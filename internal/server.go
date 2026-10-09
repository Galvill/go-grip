package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode"
	"unicode/utf8"

	chroma_html "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/chrishrb/go-grip/defaults"
)

const defaultHTMLTitle = "go-grip - markdown preview"

type Server struct {
	parser       *Parser
	boundingBox  bool
	host         string
	port         int
	browser      bool
	enableReload bool
	hub          *Hub
}

// reloadDebounce collapses the several events an editor writes per save into
// one browser reload.
const reloadDebounce = 100 * time.Millisecond

// eventsPath is the live reload stream endpoint.
const eventsPath = "/__grip/events"

// reloading reports whether live reload is on: requested and backed by a
// running hub.
func (s *Server) reloading() bool {
	return s.enableReload && s.hub != nil
}

func NewServer(host string, port int, boundingBox bool, browser bool, enableReload bool, parser *Parser) *Server {
	return &Server{
		host:         host,
		port:         port,
		boundingBox:  boundingBox,
		browser:      browser,
		enableReload: enableReload,
		parser:       parser,
	}
}

func (s *Server) Serve(file string) error {
	directory, startPath, err := ResolveTarget(file)
	if err != nil {
		return err
	}

	if s.enableReload {
		src, err := newFsnotifySource()
		if err != nil {
			fmt.Println("❌ Error starting file watcher, auto-reload disabled:", err)
			s.enableReload = false
		} else {
			s.hub = NewHub(src, reloadDebounce)
			go s.hub.Run(context.Background())
		}
	}

	handler := s.newHandler(http.Dir(directory))

	addr := fmt.Sprintf("http://%s:%d%s", s.host, s.port, startPath)

	fmt.Printf("🚀 Starting server: %s\n", addr)

	if s.browser {
		err := Open(addr)
		if err != nil {
			fmt.Println("❌ Error opening browser:", err)
		}
	}

	if s.reloading() {
		fmt.Printf("📡 Auto-reload enabled. Files will trigger browser refresh.\n")
	} else {
		fmt.Printf("🔄 Auto-reload disabled. Use F5 to manually refresh.\n")
	}
	return http.ListenAndServe(fmt.Sprintf(":%d", s.port), handler)
}

func (s *Server) newHandler(fsys http.FileSystem) http.Handler {
	root := NewRoot(fsys)
	fileServer := http.FileServer(fsys)
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(defaults.StaticFiles)))

	if s.reloading() {
		absRoot := ""
		if dir, ok := fsys.(http.Dir); ok {
			if abs, err := filepath.Abs(string(dir)); err == nil {
				absRoot = abs
			}
		}
		mux.HandleFunc(eventsPath, func(w http.ResponseWriter, r *http.Request) {
			s.serveEvents(w, r, root, absRoot)
		})
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch root.Classify(r.URL.Path) {
		case KindMarkdown:
			s.serveMarkdown(w, r, root)
			return
		case KindDir:
			s.serveFolder(w, r, root)
			return
		}

		fileServer.ServeHTTP(w, r)
	})

	if !s.reloading() {
		return mux
	}
	// While reload is on, make the browser revalidate everything, so a
	// reload never shows a heuristically cached image or stylesheet.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		mux.ServeHTTP(w, r)
	})
}

// serveEvents streams Server-Sent Events to one page: a "reload" event each
// time something that page shows has changed. The page names itself in the
// path query parameter.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request, root *Root, absRoot string) {
	ws, ok := s.watchSetFor(root, absRoot, r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		serverError(w, r, errors.New("response writer does not support streaming"))
		return
	}

	notify, cancel := s.hub.Subscribe(ws)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-notify:
			if _, err := io.WriteString(w, "event: reload\ndata: 1\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// watchSetFor computes what the page at urlPath depends on, as absolute OS
// paths under absRoot. A Markdown page depends on its file and the local
// images it references; a folder page on the folder's direct entries and the
// images its README references. urlPath is only a lookup key: a path with a
// ".." segment, or one that is neither Markdown nor a folder, is rejected.
func (s *Server) watchSetFor(root *Root, absRoot, urlPath string) (WatchSet, bool) {
	if absRoot == "" || !strings.HasPrefix(urlPath, "/") || strings.ContainsRune(urlPath, 0) {
		return WatchSet{}, false
	}
	if filepath.Separator != '/' && strings.ContainsRune(urlPath, filepath.Separator) {
		return WatchSet{}, false
	}
	for _, seg := range strings.Split(urlPath, "/") {
		if seg == ".." {
			return WatchSet{}, false
		}
	}
	clean := path.Clean(urlPath)
	own, ok := toOSPath(absRoot, clean)
	if !ok {
		return WatchSet{}, false
	}

	var ws WatchSet
	var mdPath, refDir string
	switch root.Classify(clean) {
	case KindMarkdown:
		ws.Files = append(ws.Files, own)
		mdPath, refDir = clean, path.Dir(clean)
	case KindDir:
		ws.Dirs = append(ws.Dirs, own)
		if name, ok := root.Readme(clean); ok {
			mdPath, refDir = path.Join(clean, name), clean
		}
	default:
		return WatchSet{}, false
	}

	if mdPath != "" {
		if content, err := root.ReadFile(mdPath); err == nil {
			for _, ref := range s.parser.LocalRefs(content) {
				rel, ok := resolveRef(refDir, ref)
				if !ok {
					continue
				}
				if p, ok := toOSPath(absRoot, "/"+rel); ok {
					ws.Files = append(ws.Files, p)
				}
			}
		}
	}
	return ws, true
}

// resolveRef resolves an image reference against the URL folder dir and
// returns it relative to the root, or false if it would leave the root.
func resolveRef(dir, ref string) (string, bool) {
	var rel string
	if strings.HasPrefix(ref, "/") {
		rel = path.Clean(strings.TrimLeft(ref, "/"))
	} else {
		rel = path.Join(strings.TrimLeft(dir, "/"), ref)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// toOSPath maps a cleaned URL path to an OS path under absRoot, or reports
// false if the result would not stay inside absRoot.
func toOSPath(absRoot, urlPath string) (string, bool) {
	p := filepath.Join(absRoot, filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
	rel, err := filepath.Rel(absRoot, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

func (s *Server) serveMarkdown(w http.ResponseWriter, r *http.Request, root *Root) {
	setNoCacheHeaders(w)

	content, err := root.ReadFile(r.URL.Path)
	if err != nil {
		serverError(w, r, err)
		return
	}
	htmlContent, err := s.parser.MdToHTML(content)
	if err != nil {
		serverError(w, r, err)
		return
	}

	var buf bytes.Buffer
	err = renderTemplate(&buf, htmlStruct{
		Content:      string(htmlContent),
		BoundingBox:  s.boundingBox,
		CssCodeLight: getCssCode("github"),
		CssCodeDark:  getCssCode("github-dark"),
		Title:        html.EscapeString(s.pageTitle(r.URL.Path)),
		Reload:       s.reloading(),
	})
	if err != nil {
		serverError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) serveFolder(w http.ResponseWriter, r *http.Request, root *Root) {
	dirPath := r.URL.Path
	if !strings.HasSuffix(dirPath, "/") {
		// Collapse leading slashes so the redirect can never be read as a
		// protocol-relative URL to another host.
		target := url.URL{Path: "/" + strings.TrimLeft(dirPath, "/") + "/", RawQuery: r.URL.RawQuery}
		http.Redirect(w, r, target.String(), http.StatusMovedPermanently)
		return
	}

	setNoCacheHeaders(w)
	stripCacheValidators(r)

	entries, err := root.List(dirPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		serverError(w, r, err)
		return
	}

	var readme htmltemplate.HTML
	if name, ok := root.Readme(dirPath); ok {
		content, err := root.ReadFile(path.Join(dirPath, name))
		if err != nil {
			serverError(w, r, err)
			return
		}
		rendered, err := s.parser.MdToHTML(content)
		if err != nil {
			serverError(w, r, err)
			return
		}
		// Goldmark output is trusted, as it is for Markdown pages.
		readme = htmltemplate.HTML(rendered) //nolint:gosec
	}

	listing, err := renderListing(dirPath, entries, readme, time.Now())
	if err != nil {
		serverError(w, r, err)
		return
	}

	title := defaultHTMLTitle
	if dirPath != "/" {
		title = s.pageTitle(strings.TrimSuffix(dirPath, "/"))
	}

	var buf bytes.Buffer
	err = renderTemplate(&buf, htmlStruct{
		Content:      listing,
		BoundingBox:  s.boundingBox,
		CssCodeLight: getCssCode("github"),
		CssCodeDark:  getCssCode("github-dark"),
		Title:        html.EscapeString(title),
		IsListing:    true,
		Reload:       s.reloading(),
	})
	if err != nil {
		serverError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write(buf.Bytes())
}

func serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("❌ %s %s: %v", r.Method, r.URL.Path, err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

type htmlStruct struct {
	Content      string
	BoundingBox  bool
	CssCodeLight string
	CssCodeDark  string
	Title        string
	IsListing    bool
	Reload       bool
}

func (s *Server) pageTitle(filename string) string {
	title := formatFilenameTitle(filename)
	if title == "" {
		return defaultHTMLTitle
	}
	return title
}

func formatFilenameTitle(filename string) string {
	filename = path.Base(filename)
	extension := path.Ext(filename)
	if strings.EqualFold(extension, ".md") {
		filename = strings.TrimSuffix(filename, extension)
	}

	filename = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' {
			return ' '
		}
		return r
	}, filename)

	words := strings.Fields(filename)
	for i, word := range words {
		first, size := utf8.DecodeRuneInString(word)
		words[i] = string(unicode.ToUpper(first)) + word[size:]
	}
	return strings.Join(words, " ")
}

func renderTemplate(w io.Writer, html htmlStruct) error {
	tmpl, err := template.ParseFS(defaults.Templates, "templates/layout.html")
	if err != nil {
		return err
	}
	return tmpl.Execute(w, html)
}

func getCssCode(style string) string {
	buf := new(strings.Builder)
	formatter := chroma_html.New(chroma_html.WithClasses(true))
	s := styles.Get(style)
	_ = formatter.WriteCSS(buf, s)
	return buf.String()
}

func setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func stripCacheValidators(r *http.Request) {
	r.Header.Del("If-Modified-Since")
	r.Header.Del("If-None-Match")
}

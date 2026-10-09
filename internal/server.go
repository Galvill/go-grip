package internal

import (
	"bytes"
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
	"strings"
	"text/template"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aarol/reload"
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

	var reloadMiddleware *reload.Reloader
	if s.enableReload {
		reloadMiddleware = reload.New(directory)
		reloadMiddleware.DebugLog = log.New(io.Discard, "", 0)
		// Fix WebSocket CORS issues for development
		reloadMiddleware.Upgrader.CheckOrigin = func(r *http.Request) bool {
			return true
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

	if s.enableReload {
		handler = reloadMiddleware.Handle(handler)
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

	return mux
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

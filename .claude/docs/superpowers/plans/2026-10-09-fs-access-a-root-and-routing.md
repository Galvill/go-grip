# File System Root and Routing Implementation Plan (A, #3)

**Spec:** `.claude/docs/superpowers/specs/2026-10-09-fs-access-folder-view-reload-design.md`, sections 1 and 2.
**Goal:** `Serve` resolves the right root and start page for any argument. Every request is classified by one `Root` over `http.FileSystem`, and handler errors return 500 instead of killing the process.
**Constraints:** `## Constraints` in `.claude/CLAUDE.md` and the spec's `## Constraints` apply. Additionally:
- Folders still go to `http.FileServer` in this plan. The templated folder page is plan B, so the folder-page behavior bullets in spec section 2 are not part of this unit.
- Running with no argument opens `/`, which shows the stdlib listing until B lands. The README text about that behavior changes in B, not here.
- `newHandler` keeps a signature that tests can call as `server.newHandler(http.Dir(tmpDir))` (`internal/server_test.go:22`).
**Verification:** the suite in `.claude/agents/coder.md`; per-task tests are listed under each task.

## File structure

| File | Responsibility |
|---|---|
| `internal/files.go` | `ResolveTarget`, `Kind`, `Entry`, `Root` (classify, list, read, find README) |
| `internal/files_test.go` | tests for everything in `files.go` |
| `internal/server.go` | `Serve` uses `ResolveTarget`; `newHandler` switches on `Root.Classify`; 500s; old helpers removed |
| `internal/server_test.go` | routing, traversal, error-path tests |
| `README.md` | Usage: directory argument, missing-path error |

### Task 1: `ResolveTarget`

**Files:**
- Create: `internal/files.go`
- Modify: `internal/server.go` (`Serve`, replace `:47-48` and `:63-77`)
- Test: `internal/files_test.go`

**Interfaces:**
- Consumes: `Serve(file string) error` at `internal/server.go:46`; `Open(url string) error` at `internal/open.go:8`.
- Produces:

```go
// ResolveTarget maps the CLI argument to the served directory (an OS path)
// and the URL path the browser opens first.
func ResolveTarget(arg string) (dir, startPath string, err error)
```

**Behavior** (in a temp dir containing `docs/guide.md`, `docs/my notes.md`, `notes.txt`):
- `""` → `(".", "/", nil)`.
- `"docs/guide.md"` → `("docs", "/guide.md", nil)`.
- `"docs"` and `"docs/"` → `("docs", "/", nil)`.
- `"notes.txt"` → `(".", "/notes.txt", nil)`.
- `"docs/my notes.md"` → startPath `"/my%20notes.md"`.
- `"missing.md"` → error that wraps `fs.ErrNotExist` and names `missing.md`. `go-grip missing.md` exits 1 before listening.
- `Serve` opens `http://host:port` + startPath, and `reload.New` receives `dir`.

**Tests:**
- `TestResolveTarget` table: `no argument`, `markdown file`, `directory`, `directory trailing slash`, `non-markdown file`, `name with space is escaped`, `missing path errors` (`errors.Is(err, fs.ErrNotExist)`).

**Gotchas:**
- Use `path/filepath` (`Dir`, `Base`, `Clean`) on the argument and `url.PathEscape` per URL segment. `url.JoinPath` doesn't escape `#` or `?` the way a browser needs.
- When the argument is a file directly in the working directory, `filepath.Dir` returns `"."`. Keep it as `"."`, matching today's root.

### Task 2: `Root` over `http.FileSystem`

**Files:**
- Modify: `internal/files.go`
- Test: `internal/files_test.go`

**Interfaces:**
- Consumes: `http.FileSystem` (`http.Dir`, `http.FS`).
- Produces (plans B and C consume these exactly):

```go
type Kind int

const (
	KindNotFound Kind = iota
	KindMarkdown
	KindDir
	KindOther
)

type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

func NewRoot(fsys http.FileSystem) *Root
func (r *Root) Classify(urlPath string) Kind
func (r *Root) List(urlPath string) ([]Entry, error)
func (r *Root) ReadFile(urlPath string) ([]byte, error)
func (r *Root) Readme(dirPath string) (name string, ok bool)
```

**Behavior:**
- `Classify("/a.md")`, `Classify("/B.MD")` → `KindMarkdown`. `Classify("/sub")` → `KindDir`. `Classify("/x.png")` → `KindOther`. `Classify("/nope")` → `KindNotFound`.
- A folder named `x.md/` → `KindDir`, not Markdown.
- `List("/")` on `{b.md, A.md, .hidden, zed/, Alpha/}` → `Alpha, zed, .hidden, A.md, b.md`: folders first, then case-insensitive name. Dotfiles are included.
- `Readme("/")` finds `readme.MD` → `("readme.MD", true)`. With no README → `("", false)`.
- `ReadFile("/../etc/passwd")` resolves inside the root (`http.Dir` cleans it) and never reads outside.

**Tests:**
- `TestRootClassify` table: one case per bullet above.
- `TestRootListOrder`, `TestRootListMissing` (error wraps `fs.ErrNotExist`), `TestRootReadme` table (`exact`, `lowercase`, `absent`).
- Use `http.FS(fstest.MapFS{...})`. `TestRootReadFileTraversal` uses `http.Dir(t.TempDir())`.

**Gotchas:**
- Ordering uses `strings.ToLower`, which sorts `.hidden` before letters.

### Task 3: Handler switch and 500s

**Files:**
- Modify: `internal/server.go` (`newHandler` `:97-145`, remove `readToString`, `isDirectory`, `isRegularFile`)
- Modify: `README.md` (Usage, after the "file-tree" paragraph at `:108-114`)
- Test: `internal/server_test.go`

**Interfaces:**
- Consumes: `NewRoot`, `Root.Classify`, `Root.ReadFile` (Task 2).
- Produces: `func (s *Server) newHandler(fsys http.FileSystem) http.Handler`. The same call shape as today, so existing tests compile unchanged.

**Behavior:**
- `GET /a.md` → 200, the template with rendered Markdown, no-cache headers (unchanged).
- `GET /` (a folder) → no-cache headers, validators stripped, then `http.FileServer` (unchanged).
- `GET /x.png`, `GET /nope` → `http.FileServer` (200 / 404, unchanged).
- `GET /%2e%2e/secret.md`, with `secret.md` one level above the root → 404. The body doesn't contain the secret's text.
- Unreadable Markdown (mode `0o000`) → 500 with body `Internal Server Error`. The handler keeps serving: a following `GET /a.md` → 200.
- README Usage documents `go-grip docs/` (serves that folder) and that a missing path exits with an error.

**Tests:**
- Existing `TestDirectoryListingIgnoresCacheValidators`, `TestRegularFileStillSupportsConditionalRequests`, `TestMarkdownResponsesDisableCaching`, `TestFilenameTitleResponses` pass unchanged.
- `TestHandlerTraversalRejected`, `TestHandlerReadErrorReturns500` (skip when `os.Getuid() == 0`), `TestHandlerUppercaseMarkdownExtension` (`/B.MD` renders).

**Gotchas:**
- Ordinary Markdown pages don't change, so no `parser_test.go` case is needed.

## Coverage

Spec sections: 1 (Root and start page), 2 (File system unit and routing, except folder-page rendering), Errors under 2, Testing (`files_test.go`, traversal and 500 cases).

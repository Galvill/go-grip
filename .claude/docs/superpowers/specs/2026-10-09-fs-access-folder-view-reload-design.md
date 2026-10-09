# File system access, folder view, and scoped reload — design

Date: 2026-10-09
Issues: Galvill/go-grip #3 (directory argument serves the parent), #2 (keyboard-navigable folder table), #1 (reload only for the page in view)

## Goal

Replace the ad-hoc file system handling in `internal/server.go` with one small, testable unit. Build on it to:

1. serve the right root and start page for any CLI argument (#3),
2. render folders through the go-grip template as a GitHub-style table with keyboard navigation (#2),
3. refresh a browser tab only when something that tab shows has changed (#1).

## Today

- `Serve` guesses the root with `path.Dir(file)` / `path.Base(file)` (`internal/server.go:47-48`). A directory argument serves its parent. With a trailing slash it serves the right folder but opens a URL that 404s.
- Markdown requests are detected with a regex plus `isRegularFile` (`:102-106`). Folders and everything else go to stdlib `http.FileServer` (`:135-141`), which returns an unstyled `<pre>` link list.
- Read and render errors call `log.Fatal` inside the handler (`:111`, `:116`, `:128`), so one bad file kills the server.
- Live reload is `github.com/aarol/reload` v1.2.0. It watches the whole tree recursively and broadcasts a reload to every tab on any Create or Write event. Remove and Rename never trigger a reload (`watch.go:85-92`). The script is injected into every `text/html` response.

## Decisions

| Topic | Decision |
|---|---|
| Approach | New `internal/files.go` unit over `http.FileSystem`. The handler switches on its classification. |
| Folder entries | All entries, dotfiles included. Folders first, then files, by name case-insensitively. |
| README on folder page | Rendered below the table, like GitHub. |
| No CLI argument | Opens the folder page `/` (was `/README.md`). |
| Table stack | Server-rendered HTML plus a small hand-written JS file. No React, no build step, no new dependency. |
| Markdown tab refresh | The viewed file plus the local images it references. |
| Folder tab refresh | Any entry created, removed, renamed or written in that folder (editor temp files ignored), plus its README and that README's images. |
| Reload transport | Server-Sent Events, implemented by go-grip with stdlib plus `fsnotify`. Replaces `aarol/reload`. |

## 1. Root and start page (#3)

`ResolveTarget(arg string) (dir, startPath string, err error)` in `internal/files.go` runs once in `Serve`, before listening. It replaces `server.go:47-48` and `:63-77`.

| Argument | `dir` (served root, also the watch base) | Start page |
|---|---|---|
| none | `.` | `/` (folder page) |
| `docs/guide.md` | `docs` | `/guide.md` |
| `docs` or `docs/` | `docs` | `/` |
| `notes.txt` (not Markdown) | `.` | `/notes.txt` |
| a path that doesn't exist | — | error; the CLI prints it and exits 1 |

- It uses `os.Stat` to tell a file from a folder, instead of guessing from the string.
- It splits OS paths with `path/filepath`, not `path`, so Windows separators work.
- It escapes the start URL per path segment, so names with spaces, `#` or `?` open correctly.

## 2. File system unit and routing

### `internal/files.go`

```go
type Kind int // KindNotFound, KindMarkdown, KindDir, KindOther

type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

func ResolveTarget(arg string) (dir, startPath string, err error)
func NewRoot(fsys http.FileSystem) *Root
func (r *Root) Classify(urlPath string) Kind         // one Open + Stat; Markdown = regular file with a case-insensitive .md extension
func (r *Root) List(urlPath string) ([]Entry, error) // all entries; folders first, then case-insensitive name
func (r *Root) ReadFile(urlPath string) ([]byte, error)
func (r *Root) Readme(dirPath string) (name string, ok bool) // README.md, any letter case
```

- Production passes `http.Dir(dir)`. Tests pass `http.FS(fstest.MapFS{...})`.
- Every disk access in the server goes through `Root`. This keeps the rule that serving stays inside the served directory. `readToString`, `isDirectory`, `isRegularFile` and the `.md` regex are removed.
- Symlinks behave exactly as they do with `http.Dir` today. Tightening that is out of scope.

### `newHandler` routing

| Request | Handling |
|---|---|
| `/static/…` | unchanged (embedded assets) |
| `/__grip/events` | reload stream (section 4); registered only when reload is on |
| `KindMarkdown` | rendered through `layout.html`, as today |
| `KindDir` without a trailing slash | 301 redirect to the path with a slash (same as `http.FileServer`) |
| `KindDir` | no-cache headers and cache validators stripped (as today), then the folder page (section 3) |
| `KindOther`, `KindNotFound` | `http.FileServer`, as today |

### Escaping

`layout.html` is processed with `text/template` because `Content` is trusted goldmark output. File names are not trusted. The listing is rendered from a new `defaults/templates/listing.html` processed with `html/template`, which escapes names and hrefs. The result is placed into `Content`.

### Errors

- Read or render failure: `http.Error(w, …, 500)` plus a log line. No `log.Fatal` in the handler.
- `List` failure (for example, permission denied): 500. A missing path: 404.

## 3. Folder page (#2)

### Content

1. A heading with the folder path relative to the root (`/` at the root).
2. `<table class="grip-listing">` with columns:
   - **Name:** folder or file icon, plus an `<a href>` link.
   - **Size:** human-readable; empty for folders.
   - **Modified:** relative time, with the exact timestamp in `title`.

   GitHub shows the last commit instead, but there is no git data offline, so modification time stands in.
3. A `..` row first, except at the root. Its href is `../?from=<current folder name>`.
4. The folder's README (case-insensitive `README.md`), rendered with the normal parser below the table.

The page title is the folder name, or the default title at the root. `layout.html` gets an `IsListing` field so the listing assets load only on folder pages.

### `defaults/static/js/dir-listing.js`

Hand-written plain JS in the style of `theme-switch.js`. It moves real keyboard focus between the row links.

| Key | Action |
|---|---|
| ↓ / `j` | next row; stops at the last row |
| ↑ / `k` | previous row; stops at the first row |
| Home / End | first / last row |
| Enter | opens the focused link (native browser behavior) |
| Backspace / ← | go to the parent folder; does nothing at the root |

- On load it focuses the row named by `?from=`, or else the first row. It then removes `from` from the URL with `history.replaceState`.
- It ignores keys with Ctrl, Alt or Meta held, and keys typed in form fields.
- It scrolls the focused row into view with `block: "nearest"`.
- Without JS the page still works: rows are ordinary links.

### `defaults/static/css/dir-listing.css`

- Looks like GitHub's file list: bordered rounded box, row dividers, muted Size and Modified columns, and a `:focus-within` highlight on the active row.
- Colors come from the variables in `github-markdown-{light,dark}.css`, so the existing theme toggle restyles the table with no extra code.
- The Size column is hidden on narrow screens.

## 4. Scoped live reload (#1)

### Components

- `internal/reload.go`: a `Hub` that owns one `fsnotify.Watcher`, the subscriber set, and per-folder reference counts.
- `GET /__grip/events?path=<url path>`: an SSE stream that sends `event: reload` when a change is relevant to that page.
- `defaults/static/js/live-reload.js`: included by `layout.html` when reload is on.
  - Opens an `EventSource` for `location.pathname` and calls `location.reload()` on `reload`.
  - Also reloads when the connection comes back after an error (the server restarted).
- `Parser.LocalRefs(md []byte) []string`: walks the goldmark AST and returns the destinations of `ast.Image` nodes.

### Watch set per subscriber

Computed when the page subscribes. Every reload reconnects, so the set stays current with no extra machinery.

- **Markdown page:** the page's own path, plus its local image refs. Refs are resolved relative to the page's folder, cleaned, and kept only if they stay inside the root. References with a scheme (`http:`, `https:`, `data:`, `mailto:`, …) and protocol-relative `//` references are skipped.
- **Folder page:** any event in that folder, except editor temp names (`.#*`, `*~`, `*.swp`, `*.swx`, `4913`). The folder's README and that README's image refs also count.

The `path` query value is cleaned and checked with `Root.Classify`. It is only a lookup key and is never used to open anything outside the root. Unknown or escaping paths get 400.

### Watcher

- **Folders, not the whole tree:** one non-recursive `fsnotify` watch per folder that holds at least one watched path.
- **Reference counted:** a watch is added when the first subscriber needs that folder and removed when the last one disconnects.
- **Folders, not files:** watching the folder survives editors that save by writing a temp file and renaming it over the original.
- **All event types count:** Create, Write, Remove and Rename.
- **Debounce:** 100 ms per subscriber, so one save produces one reload.
- **Disconnects:** when the request context is done, the subscriber is unregistered and its reference counts are released.

### Delivery

The script tag comes from `layout.html`. Responses are no longer rewritten.

- **Behavior change:** raw `.html` files served by `http.FileServer` no longer get a reload script.
- `--no-reload` is unchanged: no script, no endpoint, no watcher.

### Errors

- If the watcher can't add a folder (for example, the inotify watch limit is reached), it logs once per folder. The page keeps working without reload.
- If the `ResponseWriter` is not an `http.Flusher`, the endpoint returns 500 and the page works without reload.

### Dependencies

- `aarol/reload` and `gorilla/websocket` are removed. `fsnotify` becomes a direct dependency.
- `go.sum` changes invalidate `vendorHash` in `flake.nix`. No Nix is available here, so the unit's PR says so under Gate notes.

## Project rules

- Offline: the new JS, CSS and template live under `defaults/` and are embedded via `go:embed`. No CDN and no build step.
- The module path is unchanged.
- No flag is renamed or re-defaulted. The README Usage section changes in the same PR as the behavior:
  - a directory argument serves that directory,
  - no argument opens the folder page,
  - the "file-tree" wording at `README.md:108-114` describes the table.
- No unit PR touches `.claude/`.

## Testing

### `internal/files_test.go`

- `ResolveTarget` table: every row in section 1, trailing slashes, names with spaces, a missing path. Uses `t.TempDir()`.
- `Classify`, `List` ordering (folders first, case-insensitive), `Readme` letter-case matching. Uses `fstest.MapFS`.

### `internal/server_test.go`

- Folder page:
  - shows rows and the `..` row (absent at root) with `?from`,
  - escapes a hostile file name,
  - renders the README below the table,
  - redirects a folder without a trailing slash.
- `..` and percent-encoded traversal are rejected for pages and for `?path=`.
- A render error returns 500 and the server keeps serving.
- `TestDirectoryListingIgnoresCacheValidators` keeps passing.

### `internal/reload_test.go`

- The hub is fed synthetic events through an injectable event source:
  - a page is notified for its own file and its images, not for other files,
  - a folder page is notified on create, remove and rename, but not for temp names,
  - one burst of events produces one notification (debounce),
  - disconnecting releases the folder's reference count.
- One end-to-end test with real `fsnotify`: a temp folder, an atomic rename-over save, and an `httptest` SSE client that must receive `reload` within a timeout.

### `internal/parser_test.go`

- `LocalRefs` returns relative images and skips scheme and `//` URLs.

### Browser smoke

- Arrow, `j`/`k`, Home/End, Enter and Backspace navigation, including focus returning via `?from`.
- Live reload on a viewed file. No reload when an unrelated file changes.

## Delivery

Each unit is one PR into the `integration/*` branch.

1. **Unit A (#3):** `internal/files.go` (`ResolveTarget`, `Root`), the `newHandler` switch, `log.Fatal` replaced by 500. Folders still go to `http.FileServer` in this unit.
2. **Unit B (#2):** the folder page (`listing.html`, `dir-listing.js`, `dir-listing.css`, README below the table) and the README Usage text. Depends on A.
3. **Unit C (#1):** `internal/reload.go`, the SSE endpoint, `live-reload.js`, `Parser.LocalRefs`, the dependency swap with its Gate note. Depends on A, and on B for folder-page subscriptions.

## Out of scope

- Updating content in place without a full page reload (DOM patching, keeping the scroll position).
- Search, filtering, or sorting by column in the listing.
- Changing how `http.Dir` handles symlinks.
- Tracking `<img>` written as raw HTML, and CSS or other assets referenced from Markdown.
- Reloading when go-grip's own embedded assets change.

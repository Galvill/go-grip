# Scoped Live Reload Implementation Plan (C, #1)

**Spec:** `.claude/docs/superpowers/specs/2026-10-09-fs-access-folder-view-reload-design.md`, section 4.
**Goal:** a tab reloads only when its own Markdown file, an image that file references, or (for a folder page) its folder's entries change. This replaces `aarol/reload` with an SSE hub on `fsnotify`.
**Constraints:** `## Constraints` in `.claude/CLAUDE.md` and the spec's `## Constraints` apply. Additionally:
- Depends on plans A (#3) and B (#2) being on the integration branch: it consumes `Root`, `Kind` and the folder page.
- `go.mod`/`go.sum` change, so `vendorHash` in `flake.nix` is stale. Say so under `Gate notes`.
**Verification:** the suite in `.claude/agents/coder.md`; per-task tests are listed under each task.

## File structure

| File | Responsibility |
|---|---|
| `internal/parser.go` | `LocalRefs`: image destinations from the AST |
| `internal/parser_test.go` | `LocalRefs` cases |
| `internal/reload.go` | `WatchSet`, `EventSource`, `Hub` (subscribe, ref-counted folder watches, matching, debounce) |
| `internal/reload_test.go` | hub tests with a fake source; one real-fsnotify end-to-end test |
| `internal/server.go` | `/__grip/events` SSE handler, `watchSetFor`, wiring in `Serve`, `aarol/reload` removed |
| `internal/server_test.go` | SSE endpoint tests |
| `defaults/static/js/live-reload.js` | `EventSource` client |
| `defaults/templates/layout.html` | `{{if .Reload}}` script tag |
| `go.mod`, `go.sum` | drop `aarol/reload`, `gorilla/websocket`; `fsnotify` direct |

### Task 1: `Parser.LocalRefs`

**Files:**
- Modify: `internal/parser.go` (new method after `MdToHTML`)
- Test: `internal/parser_test.go`

**Interfaces:**
- Consumes: `frontmatter.Extract` (`pkg/frontmatter`), as `MdToHTML` does at `internal/parser.go:30-37`.
- Produces: `func (m Parser) LocalRefs(input []byte) []string`

**Behavior:**
- `![a](img/x.png)` → `["img/x.png"]`. `![a](/abs.png)` → `["/abs.png"]`.
- `![a](img/my%20pic.png?v=2#f)` → `["img/my pic.png"]`: unescaped, with query and fragment stripped.
- `![a](https://x/y.png)`, `![a](//cdn/y.png)`, `![a](data:image/png;base64,AA)` → `[]`.
- Duplicates are returned once, in document order. Raw HTML `<img src="z.png">` → `[]` (known limitation).

**Tests:**
- `TestLocalRefs` table: `relative`, `root relative`, `escaped with query`, `external skipped`, `protocol relative skipped`, `data skipped`, `deduplicated`, `raw html ignored`.

**Gotchas:**
- Parse with `goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough))` and `ast.Walk`. Reference-style images resolve during parsing, so `![a][r]` with `[r]: p.png` → `["p.png"]`.

### Task 2: Hub

**Files:**
- Create: `internal/reload.go`
- Test: `internal/reload_test.go`

**Interfaces:**
- Produces:

```go
type WatchSet struct {
	Files []string // absolute OS paths; any event on them matches
	Dirs  []string // absolute OS paths; any non-temp event on a direct child matches
}

type EventSource interface {
	Add(dir string) error
	Remove(dir string) error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

func NewHub(src EventSource, debounce time.Duration) *Hub
func (h *Hub) Run(ctx context.Context)
func (h *Hub) Subscribe(ws WatchSet) (notify <-chan struct{}, cancel func())
```

**Behavior:**
- The watched folders are the parent of each `Files` entry plus each `Dirs` entry. `src.Add` is called on the first subscriber of a folder, and `src.Remove` when the last one cancels.
- Event `/r/a.md` Write with Files `[/r/a.md]` → one notify. `/r/b.md` → none.
- Dirs `[/r]`: `/r/new.md` Create, `/r/old.md` Remove or Rename → notify. `/r/.#a.md`, `/r/a.md~`, `/r/.a.md.swp`, `/r/4913` → none. `/r/sub/x.md` → none (not recursive).
- Five events within 100 ms → exactly one notify. `notify` is buffered with capacity 1, and sends never block.
- A failed `src.Add` is logged once per folder, and the subscriber still gets a channel.

**Tests:**
- `TestHubMatchesFile`, `TestHubIgnoresOtherFile`, `TestHubDirEvents` table (`create`, `remove`, `rename`, `temp names ignored`, `nested ignored`), `TestHubDebounce`, `TestHubRefCountsWatches` (asserts `Add`/`Remove` calls on the fake), `TestHubAddErrorStillSubscribes`.
- `TestHubFsnotifyAtomicSave`: real `fsnotify` on `t.TempDir()`. Write `a.md.tmp`, then `os.Rename` it to `a.md`. Notify must arrive within 2 s.

**Gotchas:**
- `*fsnotify.Watcher` has `Events`/`Errors` as fields, so it needs an adapter.
- Never send on a channel while holding the hub mutex.

### Task 3: SSE endpoint, client, wiring

**Files:**
- Create: `defaults/static/js/live-reload.js`
- Modify: `internal/server.go` (`Server` gains `hub *Hub`; `Serve` replaces `:50-58` and `:88-93`; `newHandler` registers `/__grip/events` when `s.enableReload`; `htmlStruct` gains `Reload bool`)
- Modify: `defaults/templates/layout.html` (`{{if .Reload}}<script src="/static/js/live-reload.js"></script>{{end}}` in head)
- Modify: `go.mod`, `go.sum` (`go mod tidy`)
- Test: `internal/server_test.go`

**Interfaces:**
- Consumes: `Root.Classify`, `Root.Readme`, `Parser.LocalRefs`, `NewHub`, `Hub.Subscribe`.
- Produces: `func (s *Server) watchSetFor(root *Root, absRoot, urlPath string) (WatchSet, bool)`

**Behavior:**
- `GET /__grip/events?path=/docs/a.md` → 200 `text/event-stream`, an immediate `: connected\n\n`, then `event: reload\ndata: 1\n\n` after a match.
- WatchSet for `/docs/a.md` with `![](img/x.png)` → Files `[<abs>/docs/a.md, <abs>/docs/img/x.png]`. For `/docs/` with a README → Dirs `[<abs>/docs]`, Files = the README's image refs.
- `?path=/%2e%2e/x.md`, a missing path, or a `KindOther` path → 400.
- Client disconnect → `cancel` called, and the folder watch is released.
- `--no-reload`: no script tag, `/__grip/events` → 404, no watcher started.
- All responses carry `Cache-Control: no-cache` while reload is on (`aarol/reload`'s `DisableCaching` did this).
- Client: `reload` event → `location.reload()`. `onopen` after a previous `onerror` → `location.reload()`.

**Tests:**
- `TestEventsStreamsReload` (fake source; push an event, read the stream), `TestEventsRejectsBadPath` table, `TestEventsDisabledWithoutReload`, `TestWatchSetFor` table (`markdown with image`, `folder with readme`, `ref escaping root is dropped`).

**Gotchas:**
- Without blanket `no-cache`, images get heuristic caching from `Last-Modified`. A reload would then show the stale image.
- `go mod tidy` must drop `gorilla/websocket`. Check that `go.mod` lists `fsnotify` without `// indirect`.

## Coverage

Spec sections: 4 (Components, Watch set, Watcher, Delivery, Errors, Dependencies), Testing (`reload_test.go`, `LocalRefs`, reload smoke).

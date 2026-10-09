# Folder View Implementation Plan (B, #2)

**Spec:** `.claude/docs/superpowers/specs/2026-10-09-fs-access-folder-view-reload-design.md`, sections 2 (folder routing, escaping) and 3.
**Goal:** a folder request renders through `layout.html` as a GitHub-style table with keyboard navigation and the folder's README below it.
**Constraints:** `## Constraints` in `.claude/CLAUDE.md` and the spec's `## Constraints` apply. Additionally:
- Depends on plan A (#3) being on the integration branch. It consumes `Root`, `Kind`, `Entry` from `internal/files.go`.
- Hand-written JS only: no React, no build step, no new module dependency.
**Verification:** the suite in `.claude/agents/coder.md`; per-task tests are listed under each task.

## File structure

| File | Responsibility |
|---|---|
| `defaults/templates/listing.html` | `html/template` fragment for the path heading, table, and README slot |
| `internal/listing.go` | build listing data (rows, sizes, relative times) and render the fragment |
| `internal/listing_test.go` | fragment and formatting tests |
| `internal/server.go` | `KindDir` route: redirect, folder page through `layout.html` |
| `internal/server_test.go` | folder-page HTTP tests |
| `defaults/templates/layout.html` | `IsListing` block loading listing CSS/JS |
| `defaults/static/js/dir-listing.js` | keyboard navigation |
| `defaults/static/css/dir-listing.css` | GitHub file-list styling |
| `README.md` | Usage: folder page, no-argument behavior |

### Task 1: Listing fragment

**Files:**
- Create: `defaults/templates/listing.html`, `internal/listing.go`
- Test: `internal/listing_test.go`

**Interfaces:**
- Consumes: `Entry{Name, IsDir, Size, ModTime}` from `internal/files.go`; `defaults.Templates` (`defaults/embed.go`).
- Produces: `func renderListing(dirPath string, entries []Entry, readme template.HTML, now time.Time) (string, error)`. The JS and CSS (Task 3) select on this shape:

```html
<nav class="grip-listing-path">/docs/</nav>
<table class="grip-listing"><tbody>
<tr class="grip-listing-row grip-listing-up"><td class="grip-listing-name"><a href="../?from=docs">..</a></td><td></td><td></td></tr>
<tr class="grip-listing-row" data-dir="true"><td class="grip-listing-name"><svg class="octicon">…</svg><a href="sub/">sub</a></td>
  <td class="grip-listing-size"></td>
  <td class="grip-listing-mtime"><time datetime="2026-10-09T10:00:00Z" title="2026-10-09 10:00:00 UTC">3 hours ago</time></td></tr>
</tbody></table>
<article class="grip-listing-readme markdown-body">…</article>
```

**Behavior:**
- Folder hrefs end in `/`. Names are escaped per segment: `my notes.md` → `href="my%20notes.md"`.
- `<img src=x onerror=alert(1)>.md` appears as escaped text. No `<img` tag appears in the output.
- At `/` there is no `..` row. In `/a/b/` the `..` href is `../?from=b`.
- Size: `0` → `0 B`, `1536` → `1.5 KB`, `5242880` → `5.0 MB` (base 1024). Folders have an empty size cell.
- Relative time from `now`: under 1 min `just now`, `1 minute ago`, `5 hours ago`, `3 days ago`. Over 30 days it shows the date `2026-01-02`.
- An empty `readme` means no `<article>`.

**Tests:**
- `TestRenderListing` table: `root has no up row`, `nested up row carries from`, `folder href has slash`, `space escaped`, `hostile name escaped`, `readme article present`, `readme absent`.
- `TestFormatSize`, `TestFormatRelativeTime` tables with the values above.

**Gotchas:**
- The fragment must use `html/template`. `serveTemplate` uses `text/template`, which escapes nothing.
- Octicons `file-directory-fill-16` and `file-16` (primer/octicons v19, MIT) are inlined as `<svg>` in `listing.html`. Note the version in the PR body.

### Task 2: Folder route

**Files:**
- Modify: `internal/server.go` (`KindDir` branch of the `newHandler` switch; `htmlStruct` gains `IsListing bool`)
- Modify: `defaults/templates/layout.html` (head: `{{if .IsListing}}` link and script for Task 3 assets)
- Test: `internal/server_test.go`

**Interfaces:**
- Consumes: `Root.Classify`, `Root.List`, `Root.Readme`, `Root.ReadFile`; `Parser.MdToHTML`; `renderListing` (Task 1).

**Behavior:**
- `GET /sub` → 301 with `Location: /sub/`. A query string is preserved: `/sub?from=x` → `/sub/?from=x`.
- `GET /sub/` → 200 `text/html`, no-cache headers, validators stripped, `<table class="grip-listing">` inside `layout.html`, `<title>Sub</title>` (using `formatFilenameTitle`). `GET /` → default title.
- A folder with `README.md` → the rendered README inside `<article class="grip-listing-readme">` after the table.
- `List` permission error → 500. The handler keeps serving.
- The `/%2e%2e/` traversal case still gets 404.

**Tests:**
- `TestDirectoryListingIgnoresCacheValidators` still passes (it asserts the body mentions `README.md`).
- `TestFolderPageRedirectsWithoutSlash`, `TestFolderPageRendersTemplate`, `TestFolderPageRendersReadme`, `TestFolderPageListErrorReturns500` (skip when `os.Getuid() == 0`).

### Task 3: Keyboard navigation and styling

**Files:**
- Create: `defaults/static/js/dir-listing.js`, `defaults/static/css/dir-listing.css`
- Modify: `README.md` (Usage `:108-114`)

**Interfaces:**
- Consumes: the Task 1 markup (`.grip-listing-row a`, `.grip-listing-up`).

**Behavior** (browser only; covered by the Phase 7 smoke):
- On load, focus goes to the row whose link text equals `?from`, else the first row. `from` is then removed with `history.replaceState`.
- ↓/`j` and ↑/`k` move focus and stop at the ends. Home and End jump to the first or last row. Enter follows the focused link natively.
- Backspace or ← goes to the `.grip-listing-up` href. At the root they do nothing.
- Keys with Ctrl, Alt or Meta, or typed in `input`, `textarea` or `select`, are ignored. Focus scrolls with `scrollIntoView({block: "nearest"})`.
- The CSS uses only variables from `github-markdown-{light,dark}.css`: `--borderColor-default`, `--bgColor-muted`, `--fgColor-muted`. The `:focus-within` row is highlighted. Under 544px the size column is hidden.
- README Usage: `go-grip` with no argument opens the folder table with the README below. Arrow keys, Enter and Backspace navigate.

**Tests:**
- None in Go. Add `.claude/scripts/gate-fixture.md` folder-navigation steps as a separate fork-tooling follow-up, not in this unit.

**Gotchas:**
- Write it as an IIFE in the style of `defaults/static/js/theme-switch.js`. The script loads in `<head>`, so wait for `DOMContentLoaded`.

## Coverage

Spec sections: 2 (`KindDir` routing, escaping), 3 (content, `dir-listing.js`, `dir-listing.css`), Constraints (README Usage), Testing (folder-page and browser smoke items).

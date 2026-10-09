# Example: one task in the lean form

This task is written after the fact for `pkg/slug` (upstream #79, "make heading anchor id same as github"), against the code and tests that change actually shipped. It is about 240 words, and nothing the coder or reviewer needed is missing.

---

### Task 1: GitHub-compatible heading ids

**Files:**
- Create: `pkg/slug/slug.go`
- Modify: `internal/parser.go` (`MdToHTML`, the `md.Convert` call)
- Test: `pkg/slug/slug_test.go`, `internal/parser_test.go`

**Interfaces:**
- Consumes: `parser.IDs` and `parser.WithIDs` from `github.com/yuin/goldmark/parser`; `parser.WithAutoHeadingID()` already set in `MdToHTML` at `internal/parser.go`.
- Produces:

```go
func Slug(value string) string
func NewIDs() parser.IDs // Generate(value []byte, kind ast.NodeKind) []byte; Put(value []byte)
```

**Behavior:**
- `Implementation ATTACK_PATTERN` → `implementation-attack_pattern` (underscore kept).
- `What's new? (v2.0)` → `whats-new-v20` (punctuation dropped, digits kept).
- `Überschrift mit Ümlaut` → `überschrift-mit-ümlaut`; CJK kept unchanged.
- `Release 🎉 notes` → `release--notes` (emoji dropped, both spaces kept as dashes, as GitHub does).
- Third heading with the same text → `heading`, `heading-1`, `heading-2`.
- Text that slugs to empty → `heading` for headings, `id` for other kinds.
- `MdToHTML("# Hello_World")` emits `<h1 id="hello_world">`.

**Tests:**
- `TestSlug` table: `underscores are kept`, `spaces become dashes`, `punctuation is dropped`, `non-ascii letters are kept`, `cjk is kept`, `emoji are dropped`, `surrounding space is trimmed`, `only punctuation is empty`.
- `TestIDsGenerateDeduplicates`, `TestIDsGenerateFallback`, `TestIDsPut` (a `Put` id is skipped by the next `Generate`).
- `TestMdToHTML_HeadingIDsMatchGitHub`, `TestMdToHTML_DuplicateHeadingIDs` in `internal/parser_test.go`.

**Gotchas:**
- The ids must be a fresh `NewIDs()` per `MdToHTML` call, passed via `parser.NewContext(parser.WithIDs(...))`. A package-level instance leaks `-1` suffixes across page loads.
- Existing anchors change: links written against goldmark's old ids (`hello-world` for `Hello_World`) break. Say so in the PR body.

---

What makes it work:

- The fence holds only the two signatures `MdToHTML` consumes, not the body of `Slug`.
- Every Behavior bullet is one test case, and each gives the exact id, so "like GitHub" is checkable without github.com.
- The first Gotcha is something the coder can't guess from the goldmark docs, and getting it wrong passes every single-render test.

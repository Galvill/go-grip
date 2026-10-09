# <Feature> Implementation Plan

**Spec:** `.claude/docs/superpowers/specs/<date>-<name>-design.md`, sections <list>.
**Goal:** <one sentence: what exists when this plan is done>.
**Constraints:** `## Constraints` in `.claude/CLAUDE.md` and the spec's `## Constraints` apply. Additionally:
- <plan-specific constraint, one line, or delete this list>
**Verification:** the suite in `.claude/agents/coder.md`; per-task tests are listed under each task.

## File structure

| File | Responsibility |
|---|---|
| `pkg/<name>/<name>.go` | <one clause> |

### Task 1: <component name>

**Files:**
- Create: `pkg/<name>/<name>.go`, `pkg/<name>/renderer.go`
- Modify: `internal/parser.go` (extension list in `MdToHTML`, after `<extension>`)
- Test: `pkg/<name>/<name>_test.go`

**Interfaces:**
- Consumes: `MdToHTML(input []byte) ([]byte, error)` at `internal/parser.go:<line>`
- Produces: `New() goldmark.Extender`; AST node `KindThing`

```go
// Only when a later task consumes it. At most 15 lines.
type Thing struct {
	ast.BaseBlock
	Kind string
}
```

**Behavior:**
- `<markdown input>` → `<exact HTML output>`
- `<markdown input>` → `<exact HTML output>` (GitHub renders the same)

**Tests:**
- `TestThing` table: `basic block`: `> [!X]` renders `<div class="...">`.
- `TestMdToHTML_Thing` in `internal/parser_test.go`: the extension is wired and doesn't change `<existing input>`.

**Gotchas:**
- <what the coder cannot infer, or delete this heading>

### Task 2: <component name>

...

## Coverage

Spec sections: <list of the headings this plan implements>.

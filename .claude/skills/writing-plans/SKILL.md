---
name: writing-plans
description: Use when writing or revising an implementation plan in this repo from a design spec under .claude/docs/superpowers/specs, including when brainstorming or another skill hands off to writing-plans
---

# Writing Plans

## Overview

A plan is the contract a coder implements against, not the implementation. It states what each task must produce and how that is proven, in the fewest words that leave no decision open. In this repo it replaces superpowers:writing-plans.

Its readers already know the repo. The issue-filer distils tasks into issue bodies on the fork. The coder reads the issue, the plan and the spec, and its own brief in `.claude/agents/coder.md` carries the verification suite, git rules, the rendering and interface obligations and repo gotchas. The PR reviewer checks the diff against each task's Behavior and Tests lines. A plan carries only what they cannot get elsewhere: file layout, cross-task interfaces, per-task behavior contracts, test names, non-obvious gotchas.

## Where specs and plans live

Specs and plans are fork-only, like everything else under `.claude/`. They must never land at the repo root, where a unit PR would carry them upstream:

- Specs: `.claude/docs/superpowers/specs/<date>-<feature>-design.md`. When brainstorming hands off, save its spec here, not in the default `docs/superpowers/specs/`.
- Plans: `.claude/docs/superpowers/plans/<date>-<feature>-<letter>-<name>.md`, matching the letter in the spec's `## Delivery plans`.

Commit specs and plans to fork `main` as `chore(claude): ...` commits, never inside a unit PR.

## What a plan is

Copy `template.md` and fill every slot. In order:

1. **Header**: Spec path, Goal, Constraints line, Verification line.
2. **File structure**: one table, path and responsibility, one row per file created or modified.
3. **Tasks**.
4. **Coverage**: one line listing the spec sections implemented.

Size each task so it can become one issue, one unit PR, and later one upstream PR. An upstream maintainer reviews a single coherent change more readily than a bundle.

## What a task is

- **Files**: Create / Modify / Test, exact paths, line ranges or an anchor ("append to the extension list in `MdToHTML`") for Modify.
- **Interfaces**: Consumes with `file:line` for existing code, Produces with exact Go signatures. A later task's coder learns names and types only from here.
- **Behavior**: bullets. Each names a concrete input and the concrete output: Markdown in → HTML out, a request path → HTTP status and body, a flag value → a printed line or error. Each is checkable by one test case. For rendering, say what GitHub produces when that is the reference.
- **Tests**: the Go test function names, or table-case names inside one, in backticks, with the assertion when the name alone doesn't carry it. No test bodies. Say which file when the task has several.
- **Gotchas**: only what the coder cannot infer: a helper to reuse (`httptest`, an existing extension to copy the layout from), a goldmark trap (block vs inline parser priority, AST walk order, `html.WithUnsafe` interactions), markup the vendored CSS or `defaults/static/js/` selects on, a `flake.nix` `vendorHash` consequence. Omit the heading when empty.

A code fence holds a declaration another task consumes: a type, a signature, an HTML shape, a flag definition. At most 15 lines. Bodies are the coder's work.

## Budget

| Unit | Words |
|---|---|
| Task | ≤ 250 |
| Plan, excluding the file table | ≤ 3,000 |

Check with `wc -w`. Over budget means the plan holds implementation or restates the spec, or the arc needs one more plan letter.

## Constraints

`## Constraints` in `.claude/CLAUDE.md` applies to every plan, and so does the spec's own `## Constraints` section. A plan lists only constraints neither has but this plan trips over, one line each — for example "the extension must run after `highlighting` so fenced `math` blocks never reach Chroma". Repo-wide rules such as the verification suite, conventional commits, the `--browser=false` rule and "never touch `.claude/` in a unit" live in the coder brief and are not repeated.

## Concreteness

Every Behavior bullet names a value, a path, an HTML fragment, a status code or an error message. "TBD", "handle edge cases" and "similar to Task N" are plan failures. A later task that needs an earlier task's signature repeats the signature, not the body.

## Rendering and interface tasks

- A task that adds a Markdown feature creates `pkg/<name>/` in the house layout and modifies `internal/parser.go` to wire it in. It states where in the extension list it goes and why the position matters.
- A task that changes the HTML for Markdown that already renders states in Behavior whether existing documents change, and adds an `internal/parser_test.go` case for the old input.
- A task that adds or changes a flag lists `cmd/root.go` and `README.md` under Modify.
- A task that needs new client-side behavior vendors the asset under `defaults/static/` and names its version and source. No CDN.
- A task whose result only shows in a browser says so in Behavior, so the Phase 7 browser smoke covers it, and lists a `.claude/scripts/gate-fixture.md` addition as a separate fork-tooling follow-up rather than as part of the unit.

## Specs

- Every rule an implementer must obey sits under one `## Constraints` heading, so plans can reference it.
- A section that is a table has no prose restating the table.
- Decisions are one line each: the decision and its reason.
- `## Delivery plans` lists the plan split by letter, one sentence per plan.

## Common mistakes

- Pasting the test file into Tests. One line per test case names what it proves.
- Copying the constraints into the plan. Reference the section.
- A Produces line that names identifiers without signatures. The next coder needs types.
- Behavior that says "matches GitHub" without the expected output. The coder can't run github.com in a test. Write the HTML or the id.
- Skipping Gotchas because the plan feels complete. Goldmark's default heading-id generator turns `_` into `-` and drops non-ASCII letters. A plan that only says "add heading anchors" yields ids that differ from GitHub's, and links written against GitHub stop resolving. `pkg/slug` exists because of this (upstream #79).

`example.md` shows one task in this form, written for the code `pkg/slug` actually contains.

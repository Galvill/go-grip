# go-grip — fork workflow

This checkout is `Galvill/go-grip`, a fork of `chrishrb/go-grip`. go-grip is a Go CLI that renders Markdown files in the browser with GitHub's look, fully offline: `main.go` → `cmd/` (cobra flags, `--version`) → `internal/server.go` (HTTP server, live reload, file tree) and `internal/parser.go` (goldmark pipeline). Custom Markdown extensions live in `pkg/<name>/`. The HTML template and the vendored CSS/JS (GitHub CSS, Mermaid, MathJax) live under `defaults/` and are embedded with `go:embed`.

## Remotes and where things live

| | `origin` = `Galvill/go-grip` (fork) | `upstream` = `chrishrb/go-grip` |
|---|---|---|
| Issues | the backlog `/process-issues` drains | read-only for us |
| `main` | upstream's `main` plus the fork-only `.claude/` commits | the maintainer's |
| PRs | unit PRs into `integration/*`, integration → fork `main` | contribution PRs, only via `/upstream-pr` |

`gh` resolves to the fork (`gh repo set-default Galvill/go-grip`). Still pass `-R Galvill/go-grip` on every `gh` command that creates or edits something, because a fresh clone defaults to the parent repo.

## Fork-only content

Everything under `.claude/` is fork tooling. It is committed to the fork and must never appear in a PR to `chrishrb/go-grip`. That includes this file, specs, plans and scratch notes, so they go under `.claude/` too (specs and plans: `.claude/docs/superpowers/`), never at the repo root.

How it stays out of upstream PRs:

- Unit PRs never touch `.claude/`. Fork tooling changes are separate `chore(claude):` commits on fork `main`.
- An upstream contribution branch is cut from `upstream/main`, never from fork `main` or `integration/*`, and receives the unit's squash commit by cherry-pick (`/upstream-pr`).
- `.claude/hooks/guard-upstream.sh` (a PreToolUse hook) blocks `gh pr create` against upstream when the head branch's diff against `upstream/main` contains `.claude/`, blocks direct PR creation through `gh api`, blocks `git push upstream`, and blocks `gh repo sync --force`.
- Don't use GitHub's "Contribute" button on the fork. It proposes fork `main`, which contains `.claude/`.

Keeping fork `main` current with upstream is `/sync-upstream`. It merges and never resets, because a reset to upstream deletes `.claude/`.

## Constraints

Every change, plan and review obeys these. Unit work is meant to be upstreamable, so nothing here is fork-specific except the last rule.

- **Offline and self-contained.** No network access while rendering or serving. Every CSS/JS asset is vendored under `defaults/static/` and embedded via `defaults/embed.go`. No CDN links in `defaults/templates/layout.html`.
- **GitHub is the reference.** Rendering should match what github.com shows for the same Markdown. When go-grip and GitHub disagree, GitHub wins. An issue or PR that changes rendering cites GitHub's output.
- **Extensions follow the house pattern.** A new Markdown feature is a goldmark extension in `pkg/<name>/` (`ast.go`, `parser.go` or `transformer.go`, `renderer.go`, a `New()`/extender, and `<name>_test.go` with input → HTML cases). It is wired in one place, `internal/parser.go` `MdToHTML`.
- **Serving stays inside the served directory.** Files are served through `http.Dir(directory)`. A request path must never escape it.
- **The module path stays `github.com/chrishrb/go-grip`.** That includes `-ldflags -X github.com/chrishrb/go-grip/cmd.version=...` in `mise.toml`, `flake.nix` and `.github/workflows/release.yml`. Renaming it would make every change un-upstreamable.
- **CLI flags are a public interface.** Don't rename or re-default a flag in `cmd/root.go` without the README's Usage section changing in the same PR.
- **`go.sum` changes invalidate `vendorHash` in `flake.nix`.** No Nix is installed here, so a dependency PR says so under `Gate notes` rather than guessing the hash.
- **Unit PRs never touch `.claude/`.**

## Verification suite

Defined once in `.claude/agents/coder.md`. Short version: `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...`, `gofmt -l .` must print nothing, and `golangci-lint run`. The whole suite runs in about 15 s.

`golangci-lint` is v1.62 locally, while CI installs v2.7 through `mise`. If CI's lint step fails on something local lint passed, CI is right.

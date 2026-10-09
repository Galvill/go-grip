---
description: Dependency watch — collect a Go module, toolchain, vendored-asset and Actions inventory, decide which upstream changes deserve work, and file or refresh dep-watch issues on the fork via the issue-filer agent
---

# /dep-watch

You are ORCHESTRATING the dependency watch for go-grip. You collect an inventory of everything the binary is built from, then turn the relevant deltas into GitHub issues on the fork `Galvill/go-grip`. Filing happens in the `issue-filer` subagent — you decide *what* is worth an issue, it writes the body in house style. You never edit code, never open PRs, never bump anything, and never file on `chrishrb/go-grip`.

What exists:

- **Go modules**: `go.mod` + `go.sum`. Direct dependencies (goldmark and its extensions, chroma, cobra, `aarol/reload`, `yaml.v3`, testify) are compiled into every binary. `go.sum` also pins `vendorHash` in `flake.nix`.
- **Go toolchain**, pinned in four places that drift independently: the `go` directive in `go.mod`, `go` in `mise.toml` (what CI's `build` job and `mise run` use), `goversion:` in `.github/workflows/release.yml` (what release binaries are built with), and nixpkgs in `flake.lock`.
- **golangci-lint**, pinned in `mise.toml`. CI's `golangci/golangci-lint-action` runs it with `install-mode: none`.
- **Vendored web assets** under `defaults/static/`, embedded into the binary and served offline: `js/mermaid.min.js`, `js/tex-mml-chtml.js` (MathJax), and `css/github-markdown-{light,dark}.css` (github-markdown-css). No package manager tracks them. The version is whatever string is inside the file.
- **GitHub Actions** in `.github/workflows/`, pinned by tag (`@v4`), not by SHA.

## Arguments

Run directory: `$ARGUMENTS`

If empty, use `$HOME/.local/state/go-grip-dep-watch/runs/manual-$(date +%Y-%m-%d_%H%M%S)`. Create it and a `raw/` subfolder.

## Phase 0 — collect and load state

Run each as its own Bash call from the repo root, saving output under `<run-dir>/raw/`. A non-zero exit from a scanner that found something is data, not failure.

1. `go list -m -u -json all > <run-dir>/raw/modules.json` — every module with `Version`, `Update.Version` when newer, and `Indirect`. Direct dependencies are the ones without `"Indirect": true`.
2. `go run golang.org/x/vuln/cmd/govulncheck@latest -format json ./... > <run-dir>/raw/govulncheck.json`. A finding with a call trace reaches the binary; a module-only finding does not. If it can't run (no network), record `lookup failed`.
3. `curl -s 'https://go.dev/dl/?mode=json' > <run-dir>/raw/go-releases.json` — the two supported Go minors. Then `grep -n '^go ' go.mod`, `grep -n 'go = ' mise.toml`, `grep -n 'goversion' .github/workflows/release.yml` for the four pins. A release `goversion` older than the `go.mod` directive, or any pin on an unsupported minor, is a finding on its own.
4. golangci-lint: `gh api repos/golangci/golangci-lint/releases/latest --jq .tag_name` against the `mise.toml` pin.
5. Vendored assets:
   - Mermaid: `grep -oE 'version:"[0-9]+\.[0-9]+\.[0-9]+"' defaults/static/js/mermaid.min.js | head -1` vs `npm view mermaid version` (or `gh api repos/mermaid-js/mermaid/releases/latest --jq .tag_name`).
   - MathJax: `grep -oE 'setA11yOptions:\(\)=>[A-Za-z]+\}\);const [A-Za-z]+="[0-9]+\.[0-9]+\.[0-9]+"' defaults/static/js/tex-mml-chtml.js | grep -oE '[0-9]+\.[0-9]+\.[0-9]+'` (4.1.0 when this command was written) vs `npm view mathjax version`. Ignore `mathjaxVersion="..."` and `VERSION="5.0.0-beta..."` in the same file: they belong to the bundled speech-rule-engine. If the pattern misses after an update, the browser smoke's `MathJax.version` is the fallback. Note the latest version of the line in use (`npm view mathjax@4 version`) as well as the latest major.
   - github-markdown-css: no version string in the file; record the last commit that touched it (`git log -1 --format='%h %cs' -- defaults/static/css/github-markdown-light.css`) vs `npm view github-markdown-css version time.modified`.
   - Record `lookup failed` for anything you can't determine; never guess a version.
6. Actions: for each `uses: <owner>/<repo>@<ref>` in `.github/workflows/*.yml` (`grep -n 'uses:' .github/workflows/*.yml`), `gh api repos/<owner>/<repo>/releases/latest --jq .tag_name`.
7. `git rev-parse --short HEAD` and `git rev-list --count HEAD..upstream/main` (after `git fetch upstream main`) for the summary. If upstream already bumped something, that changes the advice: the fix is `/sync-upstream`, not a fork issue.

Write `<run-dir>/inventory.md` with one table: `module | direct/indirect | current | latest | bump_kind | govulncheck`, where `bump_kind` is `major`, `minor`, `patch` or `none`. Add a Toolchain table (the four Go pins, golangci-lint), an Assets table (`asset | vendored version | latest | bump_kind`), an Actions table (`action | pinned ref | latest tag | bump_kind`) and a Vulnerabilities section. A collector that failed is written as `lookup failed` — treat those as unknown, never as "no change".

Then:

1. Ensure the labels exist: `gh label list -R Galvill/go-grip --json name --jq '.[].name'`; create any missing:
   - `gh label create -R Galvill/go-grip dep-watch --color 0e8a16 --description "Filed by the dependency watch: upstream change needing a bump or adaptation"`
   - `gh label create -R Galvill/go-grip human-needed --color d93f0b --description "Agent loop paused: needs a human decision"`
   - `gh label create -R Galvill/go-grip security --color b60205 --description "Security advisory"`
2. Load the existing watch issues:
   ```
   gh issue list -R Galvill/go-grip --label dep-watch --state open --limit 200 --json number,title,body,labels
   gh issue list -R Galvill/go-grip --label dep-watch --state closed --limit 200 --json number,title,labels,closedAt
   ```
   A closed dep-watch issue carrying `wontfix` means "do not refile for this dependency at this major" — skip it silently. A closed one without `wontfix` was done; refile only if a *newer* version than the one it named has since appeared.

## Phase 1 — classify deltas

| bucket | what lands here | issue shape | labels |
|---|---|---|---|
| **toolchain** | a Go pin on an unsupported minor, or the four pins disagreeing in a way that matters (release binaries built with an older Go than `go.mod` declares); a golangci-lint major | one issue per tool | `dep-watch,human-needed` |
| **major** | a direct module whose latest is a new major (a new import path for Go modules, e.g. `chroma/v3`) | one issue per module | `dep-watch,human-needed` |
| **goldmark-group** | `yuin/goldmark` plus the extensions built on it (`goldmark-emoji`, `go.abhg.dev/goldmark/hashtag`, `go.abhg.dev/goldmark/mermaid`) — any minor or major of goldmark itself, since every `pkg/` extension implements its interfaces | one `render:` issue | `dep-watch,human-needed` for majors, `dep-watch,enhancement` otherwise |
| **assets** | a Mermaid or MathJax release newer than the vendored copy; github-markdown-css changes since the vendored copy | one issue per asset | `dep-watch,human-needed` for majors, `dep-watch,enhancement` otherwise |
| **security** | govulncheck findings with a call trace (the binary reaches the vulnerable symbol) | one issue per advisory group | `dep-watch,security,bug` |
| **batch** | all other Go module minor/patch bumps, direct and indirect | one rolling issue | `dep-watch,enhancement` |
| **actions** | any action with a newer major release | one rolling `ci:` issue | `dep-watch,enhancement` |
| **ignore** | `bump_kind` `none`; `lookup failed`; govulncheck module-only findings with no call trace; anything a closed `wontfix` issue already declined; anything `upstream/main` already contains | nothing, but list it in the run summary | — |

Judgment rules:

- `human-needed` marks bumps a person should green-light before `/process-issues` picks them up: majors, toolchain changes and the goldmark group change rendering or the build. Minor/patch batches and security fixes are drainable as filed.
- `yuin/goldmark` deserves extra care even at patch level: its CommonMark and GFM behavior is what makes go-grip look like GitHub, and the `pkg/` extensions depend on its AST and parser interfaces. `chroma` changes highlighting colors and lexers; `aarol/reload` is live reload. Call each out on its own line inside the batch issue.
- Every `go.mod`/`go.sum` change means `flake.nix` `vendorHash` must be recomputed. Every Go-module issue says so, and that it can't be done without Nix.
- Rolling batch issue titles, stable so they are refreshed instead of duplicated: `deps: Go module minor and patch bumps` and `ci: bump GitHub Actions`.
- For **major** bumps and the **goldmark group**, the issue must say which changes in the release notes matter to this repo. Pull them with `gh api repos/<owner>/<repo>/releases` and quote the load-bearing lines (renamed AST nodes, changed parser options, changed HTML output). For Mermaid and MathJax, also say what the browser smoke in `.claude/commands/process-issues.md` Phase 7 must re-check (Mermaid: diagrams render and re-theme on toggle, given `defaults/static/js/mermaid-init.js`; MathJax: inline and display math typeset, given `defaults/static/js/mathjax-options.js`). For everything else a link to the release page is enough.
- Every issue reminds the implementer that the change should be upstreamable. Mention any upstream issue or PR that already covers it (`gh pr list -R chrishrb/go-grip --state all --search <module>`).
- Cap new individual issues at 8 per run, toolchain and security first, then the goldmark group, then assets and majors ordered by how central the dependency is. Anything past the cap goes to the run summary as deferred.

## Phase 2 — dedupe

For every candidate individual issue, compare against the open dep-watch issues by dependency name in the title:

- Same dependency, open issue names the same or a newer target version → skip (report as `already tracked #N`).
- Same dependency, open issue names an older target → `gh issue comment -R Galvill/go-grip` on it with the new version and the release link; do not file a second one.
- No match → file.

For each rolling bucket: if an open issue with the stable title exists, rewrite its body with the fresh table via `gh api -X PATCH repos/Galvill/go-grip/issues/<n> -F body=@<file>` and add a one-line comment `refreshed by dep-watch run <run-dir basename>`; else file it.

## Phase 3 — file via the issue-filer agent

Write one findings file per issue to `<run-dir>/findings/<slug>.md` holding: the title (house style `<subsystem>: <imperative>`, e.g. `render: bump goldmark 1.7 to 1.8 with its extensions`, `mermaid: update vendored mermaid.min.js to 11.14`, `deps: bump chroma to v3`), every place the version appears as `file:line` from a `grep -n` you ran yourself (`go.mod`, `flake.nix` `vendorHash`, `mise.toml`, `.github/workflows/*.yml`, the vendored file), current and target versions, the release-notes URL and the quoted lines that matter, the verification commands — the suite from `.claude/agents/coder.md`, plus the integration gate `.claude/scripts/integration-gate.sh` and, for anything that changes rendering, the browser smoke in `.claude/commands/process-issues.md` Phase 7 — and the labels from the table above.

Then dispatch **one** `issue-filer` agent (Agent tool, `subagent_type: "issue-filer"`) per batch of up to 6 findings files with a prompt of this shape:

> File one GitHub issue on Galvill/go-grip per findings file listed below, from the repo root. Treat each file as prepared findings: verify every `file:line` it cites by opening the file, fix wrong refs, and follow your house style. Title and labels are given in the file; use them verbatim. Body sections: Today's behavior (the version, with the load-bearing line), The gap (what upstream changed, quoted), Proposed (bump plus what to watch for), Implementation sketch (every location), Test coverage, Out of scope, Refs. Do not check for duplicates — the caller did. Return the contract line per issue.
>
> Findings files: <absolute paths>

Verify each filed issue exists with `gh issue view <n> -R Galvill/go-grip --json labels,title` and that the labels stuck; add missing ones with `gh issue edit <n> -R Galvill/go-grip --add-label`.

## Phase 4 — report

Write `<run-dir>/summary.md` and print the same text as your final message:

```
dep-watch <date> — checkout <sha> (upstream ahead by <N>)
Filed: #N title, ...
Refreshed: #N title, ...
Commented: #N title, ...
Already tracked: ...
Already on upstream/main (run /sync-upstream): ...
Deferred past cap: ...
Ignored: <one line per ignored row, terse>
Lookups failed: <collectors that failed>
```

## Rules

- Filing ends at the URL. Never start /process-issues, never edit code, never bump anything, never open PRs. Nothing in Phase 0 writes to the repo; `go run ...govulncheck@latest` only fills the module cache.
- Issues go to `Galvill/go-grip` only.
- Only cite refs you verified this run; the inventory is a hint, the `grep -n` is the evidence.
- Read `raw/govulncheck.json` directly when a finding looks truncated; the inventory is a summary, the raw files are the record.
- If `gh` is unauthenticated or every collector failed, stop and print `failed: <why>` instead of filing partial work.

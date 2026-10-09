---
description: Drain the fork's open GitHub issue queue via parallel worktree subagents into an integration branch, gate it on the built-binary integration gate and a browser smoke, then open one integration→main PR on the fork for human review
---

# /process-issues

You are the **main orchestrator** for an issue-driven development loop on go-grip. The backlog is the open issues on the fork, `Galvill/go-grip`. Your job is to drain it using parallel coder subagents in isolated worktrees, with an independent re-verification and a code review per PR.

Everything in this loop happens on the fork. Every `gh` call passes `-R Galvill/go-grip`, and nothing here touches `chrishrb/go-grip`. Contributing a landed unit upstream is a separate, human-started step (`/upstream-pr`).

All per-issue PRs merge into a shared **integration branch**, never directly into `main`. When the queue is drained, you run the **integration gate** against the integration branch (Phase 7). Only once it is green do you open a **single integration→main PR** on the fork and stop: merging that PR is the human's job, and you never merge anything into `main` yourself. The human merges the integration PR with a **merge commit, never squash**. Each unit's squash commit then stays an individual commit on fork `main`, and `/upstream-pr` cherry-picks exactly that commit onto `upstream/main`. A squash of the integration PR would fuse every unit into one commit that can't be contributed piecemeal.

You do NOT write code yourself. You dispatch subagents and track state.

## Testing model

go-grip has no service stack: the suite runs locally in about 15 s, and tests use `httptest` and temp dirs. `.github/workflows/build.yml` (upstream's file, unchanged on the fork) runs the `build` job — `mise run build`, `mise run test`, a `gofmt -d` check and `golangci-lint` v2 — on every push and every PR. The fork has no branch protection or rulesets. The loop still changes `integration/*` only through PRs with green CI, and still updates a `BEHIND` PR before merging, because a green result from before the base moved doesn't count. So:

- **Per unit — the verification suite, run twice.** The coder runs it and writes a receipt; CI's `build` job re-runs it independently on the PR (Phase 3 step 1). CI is the evidence; the receipt is only the first filter.
- **Once, at Phase 7 — the integration gate.** It covers what the per-unit suite structurally cannot: the built binary with release ldflags, cross-compilation for every release target, the live HTTP server against a feature fixture, rendering in a real browser, cross-unit interactions, and whether each landed unit still applies cleanly to `upstream/main`.

### The verification suite (paste into every coder/remediation prompt)

No subagent may open a PR or push to an existing PR without ALL of the following green from its own worktree:

1. `go build ./...`
2. `go vet ./...`
3. `go test -race -count=1 ./...`
4. `gofmt -l .` prints nothing.
5. `golangci-lint run` (local v1.62; CI runs v2.7 — a CI-only lint failure still counts).
6. `git diff --name-only origin/<integration>...HEAD | grep '^\.claude/'` prints nothing — unit PRs never touch fork tooling.
7. **Verification receipt**: append one line per command above to `/tmp/go-grip-verify/<branch-with-slashes-as-dashes>.log` (`mkdir -p /tmp/go-grip-verify` first; truncate the file at the start of each verification pass) in the form `<unix-epoch-start> <duration-seconds> <exit-code> <command>`; end with `RESULT PASS` once everything passed, or `RESULT FAIL` if not done.

Every command runs in the FOREGROUND with a 600000ms timeout. Never start go-grip without `--browser=false` and a random high port.

## State you maintain in memory

- `integration`: the integration branch name for this run (resolved in Phase 0).
- `slots`: a list of up to **4** in-flight work units. Each slot owns one branch/PR from coder spawn through merge or escalation, and tracks the id/name of whichever agent currently holds that branch's worktree — Phase 3 addresses it directly via `SendMessage` while it is still alive, rather than spawning a fresh worktree agent that cannot check the branch out. Each slot also records the unit's **model tier** from Phase 1 and how many remediation rounds it has consumed. Four, not more: the repo is small and most units meet in `internal/parser.go`, `cmd/root.go` or `README.md`, so more parallelism mostly buys merge conflicts.
- `merged`: PRs merged into the integration branch this run.
- `escalated`: issues moved to `human-needed` this run.
- `filed`: new issues filed mid-flight this run.

Print these counters in the final summary.

## Phase 0 — Snapshot + integration branch

**0a. Resolve the integration branch.** Reuse before create, so an interrupted run resumes instead of forking:

1. `gh pr list -R Galvill/go-grip --state open --base main --json number,headRefName --jq '.[] | select(.headRefName | startswith("integration/"))'` — if an open integration→main PR exists, its head branch is `integration`, and it is **frozen**: it passed the Phase 7 gate and the human is testing exactly that branch. Stack onto it only a unit the user has named explicitly; otherwise stop and tell the user the queue is waiting on their merge — do NOT stack the rest of the backlog onto it, and do NOT fork a second integration branch. This check runs ONLY on the first Phase 0 pass of a run; a Phase 6 re-entry reuses the bound `integration` unconditionally.
2. Else, `git ls-remote --heads origin 'integration/*'` — if exactly one exists, reuse it. If several exist, surface them to the user and stop (don't guess which is live).
3. Else create it from fork main and push:
   ```
   git fetch origin main
   git push origin origin/main:refs/heads/integration/issues-<YYYY-MM-DD>
   ```
   (append `-2`, `-3`, … if the name is taken).

On the first pass, also `git fetch upstream main` and count `git rev-list --count origin/main..upstream/main`. If upstream is ahead, say so in the pooling printout ("upstream is N commits ahead of fork main; consider /sync-upstream after this run"). Don't sync mid-run.

If you're reusing an existing branch and fork `main` has advanced (`git merge-base --is-ancestor origin/main origin/<integration>` fails), sync it with a **main-sync PR**:

1. `git push origin origin/main:refs/heads/sync/main-into-<integration-slug>`.
2. `gh pr create -R Galvill/go-grip --base <integration> --head sync/main-into-<integration-slug> --title "chore: merge main into <integration>" --body "Syncs main into the integration branch."`
3. Its head is `main`'s tip, so the PR starts `BEHIND` whenever the integration branch has commits `main` lacks, which is nearly always. Run `gh api -X PUT repos/Galvill/go-grip/pulls/<n>/update-branch` straight away. Wait for CI on the new head. Before merging, confirm `gh pr view <n> -R Galvill/go-grip --json mergeStateStatus --jq .mergeStateStatus` is not `BEHIND`; if it is, update again. Then run `gh pr merge <n> -R Galvill/go-grip --merge --delete-branch`. Use a **merge commit, not a squash**, so `main`'s commits become ancestors of the integration branch.
4. If GitHub reports the PR as conflicting, dispatch a `remediator` worktree agent on the sync branch to merge `<integration>` into it and resolve. Do not resolve conflicts yourself.

Ensure the loop's labels exist on the fork; create any that are missing:

```
gh label create -R Galvill/go-grip agent-wip --color fbca04 --description "Claimed by the /process-issues loop"
gh label create -R Galvill/go-grip human-needed --color d93f0b --description "Agent loop paused: needs a human decision"
gh label create -R Galvill/go-grip design-needed --color 5319e7 --description "Parked until a design session settles it"
gh label create -R Galvill/go-grip blocked-upstream --color c5def5 --description "Waiting on an upstream release or on chrishrb/go-grip"
```

**0b. Snapshot the queue.** Run:

```
gh issue list -R Galvill/go-grip --state open --json number,title,body,labels,createdAt --limit 100
```

Filter out any issue with label `human-needed`, `agent-wip`, `design-needed`, `blocked-upstream` or `wontfix`. If the filtered list is empty AND no slots are in flight, go to **Phase 7 — Finalize**.

## Phase 1 — Cluster (autonomous pooling)

Group the filtered issues into **work units**. Pool two or more issues into a single unit when ANY of:

- Titles share a subsystem prefix and touch the same area (e.g. two `math:` issues).
- Issue bodies name overlapping files. Treat these as always-overlapping, because nearly every change to them collides: `internal/parser.go`, `cmd/root.go`, `defaults/templates/layout.html`, `README.md`, `go.mod`/`go.sum`.
- One issue is a strict subset / follow-up of another.

Keep each unit small enough to be one sensible upstream PR: a unit becomes one squash commit, and `/upstream-pr` contributes it as a unit. Don't pool unrelated features just because they share `internal/parser.go`. Sequence them instead.

Otherwise, each issue is its own unit.

Print the pooling map to the user **before** dispatching anything (no approval gate — just visibility), sorted by **smallest issue number first**:

```
Units this pass:
  unit-A: #12 + #19 (math delimiters) — large
  unit-B: #15 (alert title casing) — small
```

### Tier each unit by difficulty

Every unit gets a model tier before dispatch. The tier picks the `model` passed on the coder, remediator and reviewer `Agent` calls; the agent files carry the strong default, and the tier is the only thing that lowers it.

A unit is **small** (`model: sonnet`) only when ALL of these hold, read from the issue bodies:

- One issue, or a pool whose issues all sit in one area: a single `pkg/<name>/`, `cmd/`, `internal/` (outside the Tier 1 parts), `defaults/static/css/`, or docs.
- The body carries verified `file:line` references and a sketch of the change, so the coder follows rather than designs.
- It touches none of the **Tier 1 areas**: path resolution, routing, caching headers or live reload in `internal/server.go`; the extension list, its order or the parser/renderer options in `internal/parser.go` `MdToHTML`; heading ids in `pkg/slug/`; raw-HTML handling; `defaults/templates/layout.html` or any vendored JS under `defaults/static/js/`; `go.mod`/`go.sum`; flag names or defaults in `cmd/root.go`; build and release files (`mise.toml`, `flake.nix`, `.github/workflows/`).
- The expected diff is under roughly 300 lines, or is docs, tests or CSS only.

Everything else is **large** (`model: opus`). When in doubt it is large: a sonnet unit that needs two remediation rounds costs more than opus would have.

Review follows the unit tier, except that a small unit whose PR diff turns out to touch a Tier 1 area is reviewed on opus.

## Phase 2 — Dispatch (≤4 parallel slots)

While there are unworked units AND a free slot:

1. Take the next unit. Apply WIP labels:
   ```
   gh issue edit -R Galvill/go-grip <n1> <n2> ... --add-label agent-wip
   ```
2. Spawn a **coder subagent** in parallel (do not wait):
   - Tool: `Agent`
   - `subagent_type: "coder"` — its brief carries the suite, the invariants and the rendering and interface rules.
   - `isolation: "worktree"`
   - `model`: the unit's tier, `sonnet` for small and `opus` for large. Record it in the slot.
   - `description`: `"code #<lowest>: <short slug>"`
   - `prompt`: include all of the following:
     - Issue numbers + full bodies (paste from Phase 0 JSON).
     - Branch name to create: `issues/<lowest-number>-<short-slug>`, branched **from `origin/<integration>`** (NOT from main).
     - PR must target the integration branch on the fork: `gh pr create -R Galvill/go-grip --base <integration> ...`, body referencing issues as `Refs #<n>` (NOT `Closes #<n>` — closing keywords only fire on merges to the default branch, so `Closes` belongs in the final integration→main PR).
     - The **verification suite** above, verbatim, plus: "Before running `gh pr create`, every point must be green from your worktree. If your change only shows in a real browser or only on another OS, say so in the PR body under `Gate notes`."
     - Final instruction: "Before returning, make sure the receipt at /tmp/go-grip-verify/<branch-slug>.log ends `RESULT PASS`. When that's true and the PR is opened, return exactly: `PR=<number> BRANCH=<name>`. Do not return prose."
3. Mark the slot as **owned by this coder** until it returns. Record the coder's agent id/name — Phase 3 routes remediation back to this same agent while it is still resumable.

When a coder returns:

- If the return text starts with `HUMAN-NEEDED:` → go to Phase 4 for this slot's issues.
- If the return text matches `PR=<n> BRANCH=<b>` → proceed to Phase 3 in the same slot. CI finishes in a few minutes; if it is still pending when you get there, call `ScheduleWakeup` with `delaySeconds: 180`, `reason: "checking CI on PR #<N>"`, and prompt `"/process-issues"`.

## Phase 3 — Verify, review, remediate (sequential within the slot)

**Every remediation prompt below must include the verification suite**: reproduce, fix, get the suite green from the worktree, THEN push.

**0. Check the receipt** at `/tmp/go-grip-verify/<branch-slug>.log` (slug = branch name with `/` replaced by `-`).
   - Missing, or no final `RESULT PASS` → route to remediation (below) asking the agent to actually run the suite and write the receipt.
   - Present and `RESULT PASS` → proceed to (1). For calibration: on this repo `go build` and `go vet` take a second or two warm, `go test -race` about 5–10 s, `golangci-lint run` a few seconds, so short durations are normal; what is not normal is a missing command, a `0` exit on a command whose output the PR diff would obviously break, or a receipt older than the PR head commit.

**1. Poll CI** — the independent re-run: `gh pr checks <n> -R Galvill/go-grip`. Pending → wait for the scheduled wakeup. `gh pr checks` exits non-zero with "no checks reported" for the first seconds after a push; treat that as pending. The `build` job appears twice (push and pull_request events); both must pass.
   - A `build` failure → `gh run view <run-id> -R Galvill/go-grip --log-failed > /tmp/pr-<n>-failed.log` and route per **Remediation routing** with "CI failed on PR #<n>; failed log at /tmp/pr-<n>-failed.log. Fix the cause, re-run the suite, push, return `PR=<n> BRANCH=<b>`." Loop back to (0) when it returns. A lint failure that local v1.62 doesn't reproduce is still the PR's to fix: the log names the linter and line.
   - A failing test that passes on re-run (`gh run rerun <run-id> -R Galvill/go-grip --failed`) in a package the PR does not touch → a flake, not a blocker for this PR; file it via `gh issue create -R Galvill/go-grip` (label `bug`) once per run, append to `filed`, and proceed.
   - If CI never reports checks for the PR (Actions disabled on the fork, or down), fall back to a `test-runner` agent (`model: sonnet`): "`git -C /home/gv/Code/go-grip fetch origin <branch>`, `git -C /home/gv/Code/go-grip worktree add --detach /tmp/pr<n>-verify origin/<branch>`, from there run `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...`, `gofmt -l .`, `golangci-lint run`, then `git -C /home/gv/Code/go-grip worktree remove --force /tmp/pr<n>-verify`." Say in the final output that CI was unavailable.

**2. Title check.** There is no CI title job on the fork, so check it yourself: the title must match `^(feat|fix|docs|chore|test|refactor|perf|build|ci)(\([a-z0-9-]+\))?!?: .+`. It becomes the squash subject and, later, the upstream PR's commit, so pick the type the upstream maintainer should see: `feat` for new user-visible behavior, `fix` for a bug, `docs` for docs, `chore`/`test`/`refactor` for invisible work. Fix it with `gh api -X PATCH repos/Galvill/go-grip/pulls/<n> -f title="<type>: <subject>"`; it needs no worktree.

**3. Review.** Spawn a code-review subagent:
   - `subagent_type: "pr-reviewer"` — read-only, it pulls the diff itself with `gh pr diff`.
   - `model`: the slot's current tier. First run `gh pr diff <n> -R Galvill/go-grip --name-only`; if a small unit's diff touches a Tier 1 area, review on `opus` and mark the slot large. If the diff names any path under `.claude/`, skip the review and route straight to remediation: "Remove every change under .claude/ from this PR."
   - Prompt: "Review PR #<n> on Galvill/go-grip in /home/gv/Code/go-grip. Return JSON per your contract."

**4.** If `verdict == needs_changes`: route per **Remediation routing** with the findings JSON — "Apply these review findings, verify with the verification suite, push, return `PR=<n> BRANCH=<b>`." Loop back to (0).

**5.** If `verdict == clean` AND CI is green:
   - **Up-to-date check.** Run `gh pr view <n> -R Galvill/go-grip --json mergeStateStatus --jq .mergeStateStatus`. If it is `BEHIND`, the integration branch moved after CI ran (usually another slot merged), so the green result no longer covers what would merge. Update the branch with `gh api -X PUT repos/Galvill/go-grip/pulls/<n>/update-branch`. It merges the base into the PR branch on GitHub, needs no worktree, and triggers a fresh CI run. Then go back to (1) and poll CI on the new head. The review stands, because the PR's own diff hasn't changed. If the update fails with a merge conflict, route to remediation as for a conflicted merge below. The slot's worktree agent must `git pull` before its next push, because the branch moved on GitHub.
   - `gh pr merge <n> -R Galvill/go-grip --squash --delete-branch` — this merges into the **integration branch**. Plain merge only, never `--admin`. If it fails for conflicts, treat it like a verification failure: remediation rebases onto the integration branch, re-verifies, pushes, and the slot goes back through (0)–(3).
   - Comment on each referenced issue: `gh issue comment <issue> -R Galvill/go-grip --body "Implemented in PR #<n>; staged on <integration>. Closes via the integration PR to main."`
   - Leave `agent-wip` ON — the issues stay open until the human merges the integration PR, and the label stops the next Phase 0 pass from re-dispatching them.
   - Append the PR to `merged`. **Free the slot.**

**Remediation routing**: route a fix to the **agent currently holding the branch's worktree** — the slot's tracked coder, or the most recent remediator — via `SendMessage`, while it is still resumable. A coder's worktree locks its branch, so a new worktree agent pointed at the same branch returns `BLOCKED: branch ... already checked out`. Only spawn a fresh `remediator` (`subagent_type: "remediator"`, `isolation: "worktree"`) when `SendMessage` errors or reports the owner is no longer running. Update the slot's tracked agent whenever a fresh one takes over.

**Tier escalation.** `SendMessage` keeps the owner's model, so a small unit gets exactly one remediation round on sonnet. On a second round — a second red re-run, a second `needs_changes`, or a receipt rejected twice — `TaskStop` the owner so its worktree lock releases, spawn a fresh `remediator` with `model: opus`, and mark the slot large. A large unit never moves down.

A fresh remediator gets a lean prompt: PR number, branch, integration branch, issue numbers, the failed-report path or findings JSON, and the suite. Do not paste a summary of the previous agent's reasoning.

## Phase 4 — Escalate to `human-needed`

When a coder returns `HUMAN-NEEDED: <question>`:

1. For each issue in the slot's unit:
   ```
   gh issue edit <n> -R Galvill/go-grip --remove-label agent-wip --add-label human-needed
   gh issue comment <n> -R Galvill/go-grip --body "Agent loop paused: <question>"
   ```
2. Append the issue numbers to `escalated`. Free the slot. Do NOT open a PR.

## Phase 5 — New issues discovered mid-flight

If a subagent's return text references a newly filed issue (e.g. "filed #25"), append it to `filed`. These issues join the **next** Phase 0 snapshot — not this iteration.

## Phase 6 — Iterate

After any slot frees (via merge or escalation), go back to Phase 0. When Phase 0 finds nothing actionable and no slot is in flight, go to Phase 7.

## Phase 7 — Finalize: integration gate, then one integration→main PR

- If `merged` is empty this run AND the integration branch has no commits ahead of main (`git rev-list --count origin/main..origin/<integration>` is 0), there is nothing to hand off. Delete the integration branch if this run created it, then print the final output with `Integration PR: none`.
- Otherwise:
  1. **Integration gate — all four parts, on the integration branch, BEFORE creating or updating the integration→main PR.** This is the first time the units run together, and the first time anything exercises the built binary or a real browser. Check the landed PRs' `Gate notes` sections before starting — they are the pre-declared triage hints. None of the parts may be skipped without the user saying so.
     - Materialize the branch: `git fetch origin` then `git worktree add --detach /tmp/go-grip-gate-<integration-slug> origin/<integration>` (remove a stale worktree at that path first). Run everything below from that worktree.

     **(a) Built-binary gate.** Run `.claude/scripts/integration-gate.sh` from the gate worktree (foreground, 600000ms timeout), or dispatch a `test-runner` to run it and return its output. It runs `go mod verify`, build, vet, `go test -race`, gofmt, golangci-lint and a cross-compile for every target in `.github/workflows/release.yml`. It then builds the binary with release-style `-ldflags` and checks `--help` and `--version`. Finally it serves a temp site, `.claude/scripts/gate-fixture.md` plus the README, on a random port and probes: every fixture feature's HTML marker (frontmatter table, GitHub-style heading id, alert, task list, emoji, inline math, Mermaid block, Chroma highlighting, footnote, details, issue link, table); that the page references no external assets; no-cache headers on Markdown; the embedded static assets; raw serving of non-Markdown files; a 404; five traversal variants that must not reach a decoy file outside the served dir; and that the server survived. It prints a receipt and ends `RESULT PASS` or `RESULT FAIL`. If a unit added a feature, the gate does not know about it yet: say so in the testing focus, and file an issue to extend `gate-fixture.md` (fork tooling, so it is a `chore(claude):` change on fork `main`, not a unit).

     **(b) Cross-unit re-run.** Part (a) already ran the tests once on the combined branch. Run `go test -race -count=2 ./...` once more in the gate worktree: the combined branch is the first place units' tests meet, and a failure that shows up once in three runs is a flake to file, not noise to ignore.

     **(c) Browser smoke.** Package tests check HTML strings and can't show Mermaid SVG output, MathJax typesetting, the theme toggle, live reload or real layout. Build the gate worktree's binary (`go build -o /tmp/go-grip-gate-<slug>.bin .`), copy `.claude/scripts/gate-fixture.md` and the worktree's `README.md` into a fresh temp dir, and start `/tmp/go-grip-gate-<slug>.bin --browser=false --port <random high port>` from that dir, run in the background (the one background command in the loop). Then use the Playwright MCP tools (`mcp__plugin_playwright_playwright__browser_*`; load them with ToolSearch) to:
       - open `/README.md` and `/fixture.md` and confirm every `pre.mermaid` became an `svg`, not an error box or raw text;
       - confirm inline and display math rendered as `mjx-container`, not raw `$...$`;
       - confirm the Go code block is highlighted (`.chroma` spans with color) and the copy button appears on hover;
       - confirm the NOTE alert shows its icon and title, task-list checkboxes render disabled, and the footnote link jumps to the note;
       - toggle `details`, reload, and confirm the open state persisted (sessionStorage);
       - toggle the theme and reload — it must persist (localStorage), and Mermaid diagrams must re-render in the new theme;
       - **live reload**: append a line to `fixture.md` on disk and confirm the open page shows it within a few seconds without a manual reload;
       - open `/` and confirm the directory listing;
       - read `browser_console_messages` and treat any error as a failure.
       Screenshot any failure to the scratchpad. Kill the server when done. If the Playwright MCP is not available in this session, do NOT mark this part passed: report `browser smoke: NOT RUN` in the gate result and ask the user to run the checklist by hand before merging.

     **(d) Upstream readiness.** Informational, never blocking. For every PR in `merged`, find its squash commit on the integration branch (`gh pr view <n> -R Galvill/go-grip --json mergeCommit --jq .mergeCommit.oid`). Then, in a throwaway worktree on `upstream/main` (`git fetch upstream main`, `git worktree add --detach /tmp/go-grip-upstream-check upstream/main`), try each commit alone: `git cherry-pick --no-commit <sha>`, record `clean` or `conflicts in <files>`, then `git cherry-pick --abort` (or `git reset --hard` if nothing was staged). Also flag any commit whose `git show --name-only` lists a `.claude/` path; there should be none. Remove the worktree afterwards. The table goes into the PR body so the human knows which units `/upstream-pr` can contribute as-is.

     - **On any failure in (a)–(c)**: save the failing output to `/tmp/go-grip-gate-<integration-slug>-failed.log`, then fix it through a **gate-fix PR**. Dispatch a `remediator` worktree agent to branch `gate-fix/<integration-slug>-<k>` from `origin/<integration>`, fix, run the verification suite, and open a PR into `<integration>` titled after the fix. Take that PR through Phase 3 (CI, title, review, up-to-date check, squash merge). Never push to `<integration>` directly. After the fix lands, re-run the gate from the top of step 1 (fresh `git fetch`, fresh worktree). Loop until green, but if the SAME failure survives two remediation attempts, STOP and surface it to the user.
     - When green, remove the gate worktree (`git worktree remove --force /tmp/go-grip-gate-<integration-slug>`).
  2. **Testing focus summary.** Distill the landed PRs into a concise, tester-oriented change list — the human uses it to decide what to try by hand, so it describes observable behavior, not implementation. Build it from each landed PR's body (`gh pr view <n> -R Galvill/go-grip --json body`) plus anything the reviews or the gate surfaced; dispatch the `researcher` agent to draft it when more than a handful of PRs landed. Format:
     - Grouped by surface, highest-risk first: serving and path handling, then rendering changes (anything that changes the HTML for existing Markdown, with GitHub as the reference), then live reload, then CLI flags and output, then theme and page UI, then vendored assets, then docs last.
     - One line per behavior change, fragment style: what changed, who notices, what a tester does to see it, PR number. Changed flags, changed defaults, changed heading ids and changed rendering for existing Markdown MUST appear.
     - Invisible changes — refactors, test-only, dep bumps with no behavior change — collapse into one "no runtime change" line.
     - End with a `Suggested focus order:` line.
     - Plain markdown bullets under a `## Testing focus` heading, 15–30 lines total.
     Save it to the scratchpad; it goes verbatim into the integration PR body AND into the final output.
  3. Check for an existing open integration→main PR (same query as Phase 0a). If one exists, update its body with `gh api -X PATCH repos/Galvill/go-grip/pulls/<n> -F body=@<file>` to include this run's PRs and issues instead of opening a duplicate.
  4. Else open one:
     ```
     gh pr create -R Galvill/go-grip --base main --head <integration> --title "chore: drain issue backlog — <K> reviewed and gated PRs" --body-file <file>
     ```
     Body must contain: one line per landed PR — `- #<pr>: <title> — Refs #<issue>`; the `## Testing focus` section, verbatim, directly after that list; a `Closes #<n>` line for every resolved issue so they auto-close on merge; a `## Gate` section listing parts (a)–(c) with their result, the gate receipt from (a), and the browser-smoke checklist as run; a `## Upstream readiness` table from (d) (`PR | squash commit | cherry-picks onto upstream/main`); and a closing line `Merge with a merge commit, not squash — /upstream-pr cherry-picks the unit commits individually.`
  5. Wait for CI on the integration PR (`ScheduleWakeup`, 300s). If fork `main` has moved since the gate ran, `mergeStateStatus` is `BEHIND`. Sync `main` in with a main-sync PR as in Phase 0a, which re-runs CI, and re-run the step-1 gate on the new head. Fix any CI failure with gate-fix PRs as in step 1 until green, and if that pushes new commits, re-run the step-1 gate before considering Phase 7 done.
  6. **Do NOT merge it. Do NOT approve it. Do NOT open anything on `chrishrb/go-grip`.** Gated + open = done; the merge decision and every upstream contribution belong to the human.

## Final output

Print exactly:

```
Run complete.
Merged into <integration>: <comma list of PR numbers, or "none">
Integration gate: pass after <k> attempt(s), or "not reached", or "BLOCKED: <failure>"
Browser smoke: pass, or "NOT RUN — manual check required", or "not reached"
Integration PR: #<n> — <url> — awaiting human review, or "none"
Upstream-ready (cherry-pick clean): <comma list of PR numbers, or "none">
Escalated to human-needed: <comma list of issue numbers, or "none">
New issues filed: <comma list, or "none">
```

If the integration PR is not `none`, append: `Merge #<n> with a merge commit, not squash. After merging, contribute units with /upstream-pr <PR numbers>.`

If `escalated` is non-empty, append: `Review these with: gh issue list -R Galvill/go-grip --label human-needed`.

Then print the `## Testing focus` section verbatim, so the human can plan manual testing without opening the PR. If Phase 7 was not reached, print `Testing focus: not reached` instead.

## Troubleshooting (operator notes — not part of the loop)

- If `pr-reviewer` returns malformed JSON twice, fall back to invoking the `/code-review high` skill against the PR and parse its output.
- If two or more slots conflict, the later PRs become unmergeable at `gh pr merge` time; the Phase 3 remediation loop rebases onto the integration branch. If conflicts repeat, pool more aggressively in Phase 1 — `internal/parser.go`, `cmd/root.go` and `README.md` are the usual collision points.
- Never `cd` out of the repo root in an orchestrator Bash call: once the session's cwd sits outside the repo, every `isolation: "worktree"` dispatch fails with `Cannot create agent worktree: not in a git repository`. Use absolute paths and `git -C`; if you see that message, `cd` back to the repo root and re-dispatch.
- A fresh worktree agent returning `BLOCKED: branch ... already checked out in worktree ...` means Remediation routing was skipped: `SendMessage` the owning agent instead.
- `gh` without `-R` resolves through `gh repo set-default`. If `gh repo set-default --view` says `chrishrb/go-grip`, every unqualified command in this file would hit upstream: run `gh repo set-default Galvill/go-grip` before anything else.
- Issues intentionally stay open with `agent-wip` after their PR lands on the integration branch; they close when the human merges the integration PR.
- If a run crashes mid-flight, leftover `agent-wip` labels on issues whose PRs never landed are stale — check whether each issue's PR is on the integration branch, then clear with `gh issue edit <n> -R Galvill/go-grip --remove-label agent-wip`.
- A stale integration branch from an abandoned run (no open PR, main has moved on): delete it manually (`git push origin --delete integration/...`) — Phase 0a stops rather than guessing when several exist.
- Leftover `/tmp/pr<n>-verify`, `/tmp/go-grip-gate-*` or `/tmp/go-grip-upstream-check` worktrees from a crashed run: `git worktree remove --force <path>` then `git worktree prune`; the `branch-janitor` agent also sweeps them.
- Gate failures come in four flavors: build (a cross-compile target breaks, typically `internal/open.go` or a platform-specific import), rendering (visible only in the browser smoke: Mermaid or MathJax no longer finds the markup it expects), serving (the HTTP probes), and cross-unit (two units each green alone: both appended to the extension list in `MdToHTML` and the order now matters, or both changed the same flag). Point the remediator at the specific part's output, not just "gate failed".
- Merges are one at a time by design. Every merge into the integration branch makes the other open unit PRs `BEHIND`, and each one needs an update-branch and a fresh CI run (about a minute) before it can merge.
- If the GitHub CLI's PR-edit subcommand fails with a GraphQL "Projects (classic) is being deprecated" error, use the REST form, as this file does throughout: `gh api -X PATCH repos/Galvill/go-grip/pulls/<n> -f title="..."` or `-F body=@<file>`.

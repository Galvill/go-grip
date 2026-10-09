---
description: Bring chrishrb/go-grip's main into the fork's main with a merge (never a reset), keeping the fork-only .claude/ folder intact
---

# /sync-upstream

Fork `main` is upstream's `main` plus the fork-only `.claude/` commits plus units that haven't been contributed yet. Syncing therefore means **merging** `upstream/main` into it. Never reset, rebase or force-push fork `main`, and never run `gh repo sync --force` or click GitHub's "Discard commits". Each of those deletes `.claude/`.

## Procedure

1. `git fetch upstream main` and `git fetch origin main`. If `git rev-list --count origin/main..upstream/main` is 0, print `fork main is up to date with upstream` and stop.
2. Show what's coming: `git log --oneline origin/main..upstream/main`.
3. If an open integration→main PR exists (`gh pr list -R Galvill/go-grip --state open --base main --json number,headRefName`), say so. The sync makes it `BEHIND`, and `/process-issues` Phase 7 step 5 re-gates it. That's expected, not a reason to stop.
4. Merge in a throwaway worktree, never in the main checkout, which may hold the user's work:
   ```
   git worktree add -b sync/upstream-<YYYY-MM-DD> /tmp/go-grip-sync origin/main
   git -C /tmp/go-grip-sync merge --no-ff upstream/main -m "chore: merge upstream/main into fork main"
   ```
   On conflicts: anything under `.claude/` keeps the fork side (`git checkout --ours -- .claude`). Elsewhere the conflict is between upstream and a fork unit. If upstream merged its own version of a unit (via `/upstream-pr`), take upstream's side. Otherwise, dispatch a `remediator` with the conflicting files and both intents, or ask the user. Never resolve a non-trivial code conflict by guessing.
5. Verify from `/tmp/go-grip-sync`, foreground, 600000ms timeout: `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...`, `gofmt -l .` (prints nothing), `golangci-lint run`. Also confirm `.claude/` survived: `git -C /tmp/go-grip-sync diff --stat origin/main -- .claude` must print nothing.
6. Push the branch and open a PR on the fork, so CI runs before `main` moves:
   ```
   git -C /tmp/go-grip-sync push -u origin sync/upstream-<YYYY-MM-DD>
   gh pr create -R Galvill/go-grip --base main --head sync/upstream-<YYYY-MM-DD> --title "chore: merge upstream/main into fork main" --body "<the commit list from step 2>"
   ```
7. When CI is green, merge it with a **merge commit**: `gh pr merge <n> -R Galvill/go-grip --merge --delete-branch`. A squash would drop upstream's commit identities, and every later sync would conflict. The user invoked this command, so merging the sync PR is in scope. Ask first only if step 4 needed a non-trivial conflict resolution.
8. `git worktree remove /tmp/go-grip-sync`. If the main checkout is on `main` with a clean tree, `git pull --ff-only origin main`.

## Report

```
Synced <N> upstream commits into fork main via PR #<n> (<merged | awaiting CI | needs review: conflicts in ...>)
```

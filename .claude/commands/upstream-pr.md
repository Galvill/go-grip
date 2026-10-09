---
description: Contribute landed fork work to chrishrb/go-grip — cherry-pick a unit's squash commit onto upstream/main in a clean branch with no fork-only .claude/ content, verify it, and open the upstream PR
---

# /upstream-pr

Turn work that already landed on the fork into a pull request on `chrishrb/go-grip`. The only thing that crosses over is the unit's code. Fork tooling under `.claude/` never does.

## Arguments

`$ARGUMENTS`: one or more fork PR numbers (`#12 #15`) or commit SHAs on fork `main`. Several items become ONE upstream PR only if the user says so explicitly; otherwise each item gets its own branch and PR, which is what an upstream maintainer wants to review.

If empty, list candidates and stop: the squash commits on `origin/main` that aren't on `upstream/main`, i.e. `git log --oneline --no-merges upstream/main..origin/main -- . ':!.claude'`. Then ask which to contribute.

## Procedure, per item

Work in a throwaway worktree. Never check out or modify fork `main` or `integration/*` here.

1. **Resolve the commit.** For a fork PR: `gh pr view <n> -R Galvill/go-grip --json state,mergeCommit,title,body,baseRefName`. If it was merged into an `integration/*` branch, its `mergeCommit` is the unit's squash commit. Confirm that commit is reachable from `origin/main` (`git merge-base --is-ancestor <sha> origin/main`). If it isn't, the integration PR hasn't merged yet: stop and say so. Contributing ungated work is the user's call, not yours.
2. **Check the commit is clean.** `git show --name-only --format= <sha>`. If any path is under `.claude/`, stop: the unit broke the fork rule. Report it, and don't strip the path silently.
3. **Look for prior art upstream.** `gh pr list -R chrishrb/go-grip --state all --search "<key words from the title>"` and `gh issue list -R chrishrb/go-grip --state all --search ...`. If upstream already merged an equivalent change, stop and report it. If an upstream issue exists, the PR body says `Closes #<upstream issue>`.
4. **Branch from upstream.**
   ```
   git fetch upstream main
   git fetch origin
   git worktree add -b upstream-pr/<slug> /tmp/go-grip-upstream-<slug> upstream/main
   ```
   `<slug>` is the fork PR's branch name without its `issues/<n>-` prefix.
5. **Cherry-pick** in the worktree: `git -C /tmp/go-grip-upstream-<slug> cherry-pick -x <sha>`. On conflicts, resolve them toward upstream's current code while keeping the unit's intent. If that needs design judgment, `git cherry-pick --abort`, remove the worktree and branch, and report the conflicting files instead. Dispatching a `remediator` with the conflict is fine when the user asks.
6. **Make the commit read as upstream's.** The commit message must not mention fork issue or PR numbers, which mean nothing upstream. Amend it to the conventional title, a short body, and the `(cherry picked from ...)` line dropped: `git commit --amend`.
7. **Verify** from the worktree, foreground, 600000ms timeout: `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...`, `gofmt -l .` (prints nothing), `golangci-lint run`. Then the leak check, which must print nothing: `git -C /tmp/go-grip-upstream-<slug> diff --name-only upstream/main...HEAD | grep -E '^\.claude/'`.
8. **Push to the fork**, never to upstream: `git -C /tmp/go-grip-upstream-<slug> push -u origin upstream-pr/<slug>`.
9. **Write the PR body** for the upstream maintainer, from the fork PR's body and its issues. Include: what it does and why, from the user's point of view; how it was tested (the suite, plus any browser check that applies); `Closes #<n>` only for upstream issues. Leave out fork issue and PR numbers, `Gate notes`, verification receipts, and anything about `.claude/`, agents or the integration loop. Save it to the scratchpad.
10. **Confirm with the user before opening.** Show the title, the body, and `git diff --stat upstream/main...upstream-pr/<slug>`. This publishes to someone else's repository, so wait for an explicit yes. A yes covers this PR only.
11. **Open it**:
    ```
    gh pr create -R chrishrb/go-grip --base main --head Galvill:upstream-pr/<slug> --title "<conventional title>" --body-file <file>
    ```
    The `guard-upstream` hook re-checks the branch's diff against `upstream/main` and blocks the call if `.claude/` appears. If it blocks, fix the branch. Never work around the hook.
12. Remove the worktree (`git worktree remove /tmp/go-grip-upstream-<slug>`). Keep the branch: it backs the open PR, and `branch-janitor` deletes it after upstream merges or closes the PR.

## Report

One line per item:

```
#<fork PR> → <upstream PR url>            (opened)
#<fork PR> → upstream-pr/<slug> pushed     (awaiting your go-ahead)
#<fork PR> → skipped: <reason>
```

## Rules

- Never push to the `upstream` remote, never open a PR on upstream from fork `main` or `integration/*`, and never use `gh api` to create upstream PRs. The hook enforces all three.
- One upstream PR per unit unless the user asks otherwise.
- After an upstream merge, fork `main` picks up upstream's version of the same change through `/sync-upstream`. The duplicate content merges cleanly when upstream took the commit unchanged; when the maintainer edited it, expect a conflict there and resolve it toward upstream.

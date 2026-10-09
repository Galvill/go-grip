#!/usr/bin/env bash
# PreToolUse guard for Bash: keeps the fork-only .claude/ folder out of
# chrishrb/go-grip. Exit 2 blocks the tool call and shows stderr to Claude.
#
# Blocks:
#   - `gh pr create` targeting upstream when the head branch's diff against
#     upstream/main touches .claude/ (or when the head can't be determined)
#   - PR creation against upstream through `gh api`
#   - `git push upstream ...`
#   - `gh repo sync --force` (it resets fork main and deletes .claude/)
#
# Matching is textual; it is a seatbelt, not a sandbox.

set -uo pipefail

UPSTREAM_REPO="chrishrb/go-grip"
FORK_OWNER="Galvill"

input=$(cat)
cmd=$(jq -r '.tool_input.command // ""' <<<"$input")
cwd=$(jq -r '.cwd // "."' <<<"$input")

block() {
  echo "guard-upstream: $1" >&2
  exit 2
}

# git push upstream ...
if grep -Eq '(^|[;&|(]|\s)git(\s+-C\s+\S+)?\s+push\s+(\S+\s+)*upstream(\s|$)' <<<"$cmd"; then
  block "never push to the upstream remote. Push the branch to origin (the fork) and open the PR with /upstream-pr."
fi

if grep -Eq 'gh\s+repo\s+sync' <<<"$cmd" && grep -Eq -- '--force' <<<"$cmd"; then
  block "'gh repo sync --force' hard-resets fork main to upstream and deletes .claude/. Use /sync-upstream (a merge) instead."
fi

if grep -Eq "gh\s+api" <<<"$cmd" && grep -Eiq "repos/$UPSTREAM_REPO/pulls(\s|\"|'|$)" <<<"$cmd" &&
  grep -Eq -- '(-X|--method)\s*POST|(\s-f|\s-F|--field|--raw-field|--input)\s' <<<"$cmd"; then
  block "create upstream PRs with 'gh pr create -R $UPSTREAM_REPO --head $FORK_OWNER:<branch>' so the .claude/ check runs."
fi

grep -Eq 'gh\s+pr\s+create' <<<"$cmd" || exit 0

# Does this gh pr create target upstream? Explicit -R/--repo wins; otherwise
# gh uses the checkout's default repo.
target=$(grep -oE -- '(-R|--repo)[= ]+\S+' <<<"$cmd" | head -1 | sed -E 's/^(-R|--repo)[= ]+//; s/["'\'']//g')
if [ -z "$target" ]; then
  target=$(cd "$cwd" 2>/dev/null && gh repo set-default --view 2>/dev/null)
fi
target=${target#https://github.com/}
[ "${target,,}" = "${UPSTREAM_REPO,,}" ] || exit 0

head=$(grep -oE -- '(-H|--head)[= ]+\S+' <<<"$cmd" | head -1 | sed -E 's/^(-H|--head)[= ]+//; s/["'\'']//g')
[ -n "$head" ] || block "upstream PRs must name the head explicitly: --head $FORK_OWNER:<branch>."
branch=${head#*:}

git -C "$cwd" fetch -q upstream main 2>/dev/null || block "could not fetch upstream/main to check the diff."
git -C "$cwd" fetch -q origin "$branch" 2>/dev/null || block "branch '$branch' is not on origin; push it to the fork first."

leaked=$(git -C "$cwd" diff --name-only upstream/main...FETCH_HEAD | grep -E '^\.claude/' || true)
if [ -n "$leaked" ]; then
  block "branch '$branch' changes fork-only files relative to upstream/main:
$leaked
Cut the branch from upstream/main and cherry-pick only the unit's commit (see /upstream-pr)."
fi

exit 0

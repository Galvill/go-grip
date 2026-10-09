#!/usr/bin/env bash
# Integration gate for go-grip.
#
# Run from the root of a clean checkout of the branch under test (e.g. a
# throwaway worktree of the integration branch). It exercises the product the
# way a user gets it: module verification, the full suite with -race, lint,
# cross-compilation for every release target, then the built binary's CLI
# and a live server probed over HTTP against a feature fixture and the README.
# The server port is picked at random, so several gates can run side by side.
#
# Output: one receipt line per step, `<epoch-start> <seconds> <exit> <step>`,
# then `RESULT PASS` or `RESULT FAIL`. Exit status 0 only on PASS. A failing
# step's last 30 log lines go to stderr; full logs stay in the printed work dir.
#
# Browser rendering (Mermaid, MathJax, theme toggle, live reload) is NOT
# covered here; the orchestrator runs that as a separate Playwright smoke. See
# .claude/commands/process-issues.md, Phase 7.

set -uo pipefail

root=$(pwd)
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/go-grip-gate-XXXXXX")
fail=0
server_pid=""
url=""

cleanup() {
  if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null; fi
}
trap cleanup EXIT

# step <name> <command...>: run, time and record one gate step.
step() {
  local name=$1
  shift
  local log="$work/$(echo "$name" | tr -c 'a-zA-Z0-9\n' '-').log"
  local t0 ec
  t0=$(date +%s)
  "$@" >"$log" 2>&1
  ec=$?
  printf '%s %s %s %s\n' "$t0" "$(($(date +%s) - t0))" "$ec" "$name"
  if [ "$ec" -ne 0 ]; then
    fail=1
    echo "--- $name failed; tail of $log:" >&2
    tail -30 "$log" >&2
  fi
  return "$ec"
}

gofmt_clean() {
  local out
  out=$(gofmt -l .)
  [ -z "$out" ] || { echo "gofmt would change:"; echo "$out"; return 1; }
}

cross_compile() {
  local t
  for t in linux/amd64 linux/arm64 linux/386 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 windows/386; do
    GOOS=${t%/*} GOARCH=${t#*/} go build -o /dev/null . || { echo "build failed for $t"; return 1; }
  done
}

version_stamped() {
  local out
  out=$("$bin" --version)
  echo "$out"
  grep -q 'gate-test' <<<"$out" || { echo "--version does not report the -ldflags version"; return 1; }
}

free_port() {
  if command -v python3 >/dev/null; then
    python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1])'
  else
    shuf -i 20000-60000 -n 1
  fi
}

start_server() {
  local port
  port=$(free_port)
  (cd "$site" && "$bin" --browser=false --port "$port" >"$work/server.out" 2>&1) &
  server_pid=$!
  for _ in $(seq 50); do
    if curl -s -o /dev/null "http://127.0.0.1:$port/"; then
      url="http://127.0.0.1:$port"
      return 0
    fi
    kill -0 "$server_pid" 2>/dev/null || break
    sleep 0.2
  done
  cat "$work/server.out"
  return 1
}

# expect_http <path> <status> [grep-pattern...]: GET against the live server;
# every pattern must appear in the body.
expect_http() {
  local p=$1 want=$2 body="$work/body" code pat
  shift 2
  code=$(curl -s -o "$body" -w '%{http_code}' --path-as-is "$url$p")
  [ "$code" = "$want" ] || { echo "GET $p: want $want, got $code"; head -c 2000 "$body"; return 1; }
  for pat in "$@"; do
    grep -q -- "$pat" "$body" || { echo "GET $p: body lacks '$pat'"; return 1; }
  done
}

# traversal_refused: no request may return a file from outside the served dir.
# The decoy sits next to the served dir, so any escape would hit it.
traversal_refused() {
  local p
  for p in /../decoy.txt /%2e%2e/decoy.txt /..%2fdecoy.txt /static/../../decoy.txt /..%5cdecoy.txt; do
    curl -s -L -o "$work/body" --path-as-is "$url$p"
    if grep -q 'GATE-DECOY' "$work/body"; then echo "GET $p leaked a file outside the served dir"; return 1; fi
  done
}

no_external_assets() {
  if grep -Eo '(src|href)="https?://[^"]+"' "$work/fixture.html" | grep -v 'github.com/gate-owner/gate-repo/issues/'; then
    echo "rendered page references external assets (must be offline)"
    return 1
  fi
}

echo "gate work dir: $work"

step "go mod verify" go mod verify
step "go build" go build ./...
step "go vet" go vet ./...
step "go test -race" go test -race -count=1 ./...
step "gofmt" gofmt_clean
step "golangci-lint" golangci-lint run
step "cross-compile release targets" cross_compile

bin="$work/go-grip"
step "build binary" go build -ldflags "-s -w -X github.com/chrishrb/go-grip/cmd.version=gate-test" -o "$bin" .
step "cli --help" "$bin" --help
step "cli --version" version_stamped

# Served site: the fixture plus the repo README, in a dir whose parent holds
# a decoy the traversal check looks for.
site="$work/site/docs"
mkdir -p "$site/sub"
echo "GATE-DECOY" >"$work/site/decoy.txt"
cp "$here/gate-fixture.md" "$site/fixture.md"
cp "$root/README.md" "$site/README.md"
echo "plain text" >"$site/sub/notes.txt"
# pkg/ghissue resolves #N against the cwd's `origin` remote.
git -C "$site" init -q && git -C "$site" remote add origin https://github.com/gate-owner/gate-repo.git

if step "serve start" start_server; then
  step "GET / opens README" expect_http /README.md 200 '<title>README</title>' 'class="markdown-body"'
  step "GET / directory listing" expect_http /sub/ 200 'notes.txt'
  step "render fixture" expect_http /fixture.md 200 \
    'class="frontmatter-table"' \
    'id="hello_world-heading"' \
    'markdown-alert markdown-alert-note' \
    'class="task-list-item-checkbox"' \
    '&#x1f44d;' \
    'class="math inline"' \
    '<pre class="mermaid">' \
    '<pre class="chroma">' \
    'class="footnote-ref"' \
    '<details id="details-' \
    'href="https://github.com/gate-owner/gate-repo/issues/46" class="issue-link"' \
    '<table>'
  cp "$work/body" "$work/fixture.html"
  step "page is offline-only" no_external_assets
  step "markdown not cached" bash -c "curl -sI '$url/fixture.md' | grep -qi 'cache-control: no-store'"
  step "GET static mermaid" expect_http /static/js/mermaid.min.js 200
  step "GET static mathjax" expect_http /static/js/tex-mml-chtml.js 200
  step "GET static css" expect_http /static/css/github-markdown-light.css 200
  step "GET non-md file raw" expect_http /sub/notes.txt 200 'plain text'
  step "GET missing is 404" expect_http /nope.md 404
  step "traversal refused" traversal_refused
  step "server still alive" kill -0 "$server_pid"
fi

if [ "$fail" -eq 0 ]; then echo "RESULT PASS"; else echo "RESULT FAIL"; fi
exit "$fail"

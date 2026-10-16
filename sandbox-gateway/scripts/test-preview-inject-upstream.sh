#!/usr/bin/env bash
# Smoke for `preview-inject.sh set-upstream` against the real preview-inject
# binary. No iptables/root: the injector listens on a free port and requests go
# to it directly (in a sandbox, inbound PREVIEW_PORT is REDIRECTed there).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/sandbox/scripts/preview-inject.sh"
TMP="$(mktemp -d)"
PIDS=()
cleanup() {
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP"
}
trap cleanup EXIT

for c in go python3 curl ss; do
  command -v "$c" >/dev/null || { echo "FAIL: $c not found" >&2; exit 1; }
done

fail() { echo "FAIL: $*" >&2; exit 1; }

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

wait_port() {
  for _ in $(seq 1 50); do
    (echo >/dev/tcp/127.0.0.1/"$1") >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  fail "nothing listening on :$1"
}

# start_app <name> <port>: serve an HTML page that names the app.
start_app() {
  mkdir -p "$TMP/$1"
  printf '<html><head><title>%s</title></head><body>app-%s</body></html>\n' "$1" "$1" >"$TMP/$1/index.html"
  python3 -m http.server "$2" --bind 127.0.0.1 --directory "$TMP/$1" >/dev/null 2>&1 &
  PIDS+=("$!")
  eval "APP_PID_$1=$!"
  wait_port "$2"
}

stop_app() {
  local v="APP_PID_$1"
  kill "${!v}" 2>/dev/null || true
  wait "${!v}" 2>/dev/null || true
}

PP=$(free_port)    # stands in for PREVIEW_PORT
APP=$(free_port)   # the Agent's own dev-server port (e.g. Vite 5173)
LP=$(free_port)    # injector listen port (17980 in a sandbox)
FILE="$TMP/upstream"

echo "== build preview-inject"
(cd "$ROOT/sandbox" && go build -o "$TMP/preview-inject" ./cmd/preview-inject)
"$TMP/preview-inject" --listen "127.0.0.1:$LP" --upstream "http://127.0.0.1:$PP" \
  --upstream-file "$FILE" >"$TMP/inject.log" 2>&1 &
PIDS+=("$!")
wait_port "$LP"

set_upstream() {
  PREVIEW_DIRECT=1 PREVIEW_PORT="$PP" PREVIEW_INJECT_UPSTREAM_FILE="$FILE" \
    GRASP_RUNTIME_RUN_DIR="$TMP/run" bash "$SCRIPT" set-upstream "$1" || true
}

# expect_entry <app-name> <upstream-port>: the public entry serves that app,
# injected, and reports which port it proxied. Retries past the 1s caches.
expect_entry() {
  local body hdr
  for _ in $(seq 1 40); do
    hdr=$(curl -sS -D - -o "$TMP/body" "http://127.0.0.1:$LP/" 2>/dev/null | tr -d '\r' || true)
    body=$(cat "$TMP/body" 2>/dev/null || true)
    if grep -qi "^x-grasp-upstream: $2\$" <<<"$hdr" && grep -q "app-$1" <<<"$body"; then
      grep -q '/__grasp/preview-pick.js' <<<"$body" || fail "app-$1 via :$2 served without pick script"
      echo "ok: entry → app-$1 (:$2), pick injected"
      return 0
    fi
    sleep 0.1
  done
  echo "--- headers"; echo "$hdr"; echo "--- body"; echo "$body"; cat "$TMP/inject.log"
  fail "entry did not serve app-$1 via :$2"
}

expect_result() {
  local out="$1" want="$2"
  grep -q "^result=$want" <<<"$out" || fail "want result=$want, got: $out"
  echo "ok: $want"
}

echo "== app already on PREVIEW_PORT: served and injected"
start_app main "$PP"
expect_entry main "$PP"

echo "== no steal: PREVIEW_PORT busy keeps the existing app"
start_app vite "$APP"
expect_result "$(set_upstream "$APP")" "busy preview_port=$PP"
[ ! -e "$FILE" ] || fail "busy must not write the upstream file"
expect_entry main "$PP"

echo "== switch: app on its own port gets the public entry"
stop_app main
expect_result "$(set_upstream "$APP")" "ok preview_port=$PP upstream=$APP"
[ "$(cat "$FILE")" = "$APP" ] || fail "upstream file should hold $APP"
expect_entry vite "$APP"

echo "== stale switch: dead app port falls back to PREVIEW_PORT"
stop_app vite
start_app main "$PP"
expect_entry main "$PP"

echo "== rejected ports"
expect_result "$(set_upstream 9222)" denied
expect_result "$(set_upstream 17980)" denied
expect_result "$(set_upstream "$(free_port)")" nolisten
expect_result "$(set_upstream abc)" invalid

echo "== reset clears the switch"
start_app vite "$APP"
stop_app main
expect_result "$(set_upstream "$APP")" ok
expect_result "$(set_upstream reset)" "reset preview_port=$PP"
[ ! -e "$FILE" ] || fail "reset must remove the upstream file"

echo "== SSH-style call: PREVIEW_PORT comes from the saved runtime env"
mkdir -p "$TMP/run"
printf 'declare -x PREVIEW_DIRECT="1"\ndeclare -x PREVIEW_PORT="%s"\n' "$PP" >"$TMP/run/env"
out=$(env -u PREVIEW_PORT -u PREVIEW_DIRECT PREVIEW_INJECT_UPSTREAM_FILE="$FILE" \
  GRASP_RUNTIME_RUN_DIR="$TMP/run" bash "$SCRIPT" set-upstream "$APP" || true)
expect_result "$out" "ok preview_port=$PP upstream=$APP"
expect_entry vite "$APP"

echo "== without direct preview the subcommand is a no-op"
expect_result "$(env -u PREVIEW_PORT -u PREVIEW_DIRECT GRASP_RUNTIME_RUN_DIR="$TMP/none" bash "$SCRIPT" set-upstream "$APP" || true)" unavailable

echo "preview-inject set-upstream smoke: all passed"

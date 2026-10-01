#!/usr/bin/env bash
# Unit smoke for the runtime bundle contract: grasp-bootstrap.sh (install,
# version checks, fetch) and services.sh (start/restart --if-idle/stop).
# Needs bash, curl, python3; no Docker, no Go.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO="$(cd "$ROOT/.." && pwd)"
SCRIPTS="$ROOT/sandbox/scripts"
BOOTSTRAP="$SCRIPTS/grasp-bootstrap.sh"
SERVICES="$SCRIPTS/services.sh"
BUILD="$REPO/scripts/build-sandbox-runtime.sh"
TMP="$(mktemp -d)"
PIDS=()
cleanup() {
  local p
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  [ -f "$TMP/run/backend.pid" ] && kill "$(cat "$TMP/run/backend.pid")" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok - $*"; }

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

wait_port() {
  local i
  for i in $(seq 1 50); do
    (echo >/dev/tcp/127.0.0.1/"$1") >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  return 1
}

# --- runtime_digest must be byte-identical in the build script and bootstrap.
extract_digest() { sed -n '/^runtime_digest() {/,/^}/p' "$1"; }
[ -n "$(extract_digest "$BUILD")" ] || fail "runtime_digest not found in build script"
diff <(extract_digest "$BUILD") <(extract_digest "$BOOTSTRAP") >/dev/null ||
  fail "runtime_digest differs between build-sandbox-runtime.sh and grasp-bootstrap.sh"
pass "runtime_digest identical"
eval "$(extract_digest "$BUILD")"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) ARCH="$(uname -m)" ;;
esac

# make_bundle OUT TAG [MIN_IMAGE] [ARCH] [VERSION_OVERRIDE]
make_bundle() {
  local out="$1" tag="$2" min="${3:-1}" arch="${4:-$ARCH}" override="${5:-}" d version
  d="$(mktemp -d "$TMP/b.XXXXXX")"
  mkdir -p "$d/scripts" "$d/bin"
  printf '#!/bin/bash\necho "STARTUP-OK %s"\n' "$tag" >"$d/scripts/startup.sh"
  cp "$SERVICES" "$d/scripts/services.sh"
  printf '#!/bin/bash\necho %s\n' "$tag" >"$d/bin/backend"
  chmod 0755 "$d/scripts/startup.sh" "$d/scripts/services.sh" "$d/bin/backend"
  version="$(runtime_digest "$d")"
  [ -n "$override" ] && version="$override"
  printf 'version=%s\narch=%s\nmin_image=%s\n' "$version" "$arch" "$min" >"$d/MANIFEST"
  tar -C "$d" -czf "$out" .
  rm -rf "$d"
  echo "$version"
}

export GRASP_RUNTIME_ROOT="$TMP/opt"
export GRASP_IMAGE_LEVEL_FILE="$TMP/image-level"
echo 1 >"$GRASP_IMAGE_LEVEL_FILE"

rc=0; bash "$BOOTSTRAP" bogus 2>/dev/null || rc=$?
[ "$rc" = 2 ] || fail "usage exit=$rc"
rc=0; bash "$BOOTSTRAP" version 2>/dev/null || rc=$?
[ "$rc" = 1 ] || fail "version before install exit=$rc"
pass "usage and empty version"

# --- install-stdin, switch, keep current + previous only.
V1="$(make_bundle "$TMP/v1.tgz" one)"
V2="$(make_bundle "$TMP/v2.tgz" two)"
V3="$(make_bundle "$TMP/v3.tgz" three)"
[ "$(bash "$BOOTSTRAP" install-stdin <"$TMP/v1.tgz")" = "$V1" ] || fail "install v1"
[ "$(bash "$BOOTSTRAP" version)" = "$V1" ] || fail "version v1"
[ "$(readlink "$GRASP_RUNTIME_ROOT/current")" = "$V1" ] || fail "current -> v1"
[ "$(bash "$BOOTSTRAP" install-stdin <"$TMP/v1.tgz")" = "$V1" ] || fail "reinstall v1"
bash "$BOOTSTRAP" install-stdin <"$TMP/v2.tgz" >/dev/null
bash "$BOOTSTRAP" install-stdin <"$TMP/v3.tgz" >/dev/null
[ "$(bash "$BOOTSTRAP" version)" = "$V3" ] || fail "version v3"
[ -d "$GRASP_RUNTIME_ROOT/$V2" ] || fail "previous version should be kept"
[ ! -d "$GRASP_RUNTIME_ROOT/$V1" ] || fail "older versions should be pruned"
[ -z "$(find "$GRASP_RUNTIME_ROOT" -maxdepth 1 -name '.incoming.*')" ] || fail "incoming dirs left behind"
pass "install-stdin switches current and prunes"

# --- rejected bundles leave current untouched.
expect_reject() {
  local want="$1" file="$2" what="$3" rc=0
  bash "$BOOTSTRAP" install-stdin <"$file" >/dev/null 2>"$TMP/err" || rc=$?
  [ "$rc" = "$want" ] || fail "$what: exit=$rc want $want ($(cat "$TMP/err"))"
  [ "$(bash "$BOOTSTRAP" version)" = "$V3" ] || fail "$what changed current"
}
make_bundle "$TMP/tampered.tgz" four 1 "$ARCH" "$(printf 'f%.0s' $(seq 1 64))" >/dev/null
expect_reject 1 "$TMP/tampered.tgz" "digest mismatch"
make_bundle "$TMP/old-image.tgz" five 999 >/dev/null
expect_reject 4 "$TMP/old-image.tgz" "min_image too high"
grep -q "请重建沙箱镜像" "$TMP/err" || fail "min_image message"
make_bundle "$TMP/arch.tgz" six 1 sparc >/dev/null
expect_reject 1 "$TMP/arch.tgz" "arch mismatch"
mkdir -p "$TMP/nomani" && echo x >"$TMP/nomani/a" && tar -C "$TMP/nomani" -czf "$TMP/nomani.tgz" .
expect_reject 1 "$TMP/nomani.tgz" "missing MANIFEST"
echo "not a tgz" >"$TMP/junk.tgz"
expect_reject 1 "$TMP/junk.tgz" "not a tgz"
rm -f "$GRASP_IMAGE_LEVEL_FILE"
expect_reject 4 "$TMP/v2.tgz" "missing image level"
echo 1 >"$GRASP_IMAGE_LEVEL_FILE"
pass "invalid bundles rejected"

# --- fetch: URL required, Bearer header, exec startup.sh, token never logged.
rc=0; env -u GRASP_RUNTIME_URL bash "$BOOTSTRAP" fetch 2>"$TMP/err" || rc=$?
[ "$rc" = 1 ] && grep -q GRASP_RUNTIME_URL "$TMP/err" || fail "fetch without URL exit=$rc"

PORT="$(free_port)"
TOKEN="tok-$RANDOM-$RANDOM-secret"
V4="$(make_bundle "$TMP/v4.tgz" four)"
bash "$ROOT/scripts/runtime-server.sh" "$TMP/v4.tgz" "$PORT" "$TOKEN" 2>/dev/null &
PIDS+=($!)
wait_port "$PORT" || fail "runtime server did not start"
URL="http://127.0.0.1:$PORT/sandbox-inject/abc.tgz"

rc=0
GRASP_RUNTIME_URL="$URL" SANDBOX_INJECT_HEADERS="Authorization: Bearer wrong" GRASP_BOOTSTRAP_ATTEMPTS=1 \
  bash "$BOOTSTRAP" fetch >/dev/null 2>&1 || rc=$?
[ "$rc" = 1 ] || fail "wrong token exit=$rc"
[ "$(bash "$BOOTSTRAP" version)" = "$V3" ] || fail "failed fetch changed current"

out="$(GRASP_RUNTIME_URL="$URL" SANDBOX_INJECT_HEADERS="Authorization: Bearer $TOKEN" bash "$BOOTSTRAP" fetch 2>&1)"
grep -q "STARTUP-OK four" <<<"$out" || fail "fetch did not exec startup.sh: $out"
! grep -q "$TOKEN" <<<"$out" || fail "token leaked into fetch output"
[ "$(bash "$BOOTSTRAP" version)" = "$V4" ] || fail "fetch did not install v4"
out="$(GRASP_RUNTIME_URL="$URL" SANDBOX_INJECT_HEADERS="Authorization: Bearer $TOKEN" GRASP_BOOTSTRAP_NO_EXEC=1 bash "$BOOTSTRAP" fetch 2>&1)"
! grep -q "STARTUP-OK" <<<"$out" || fail "GRASP_BOOTSTRAP_NO_EXEC still exec'd"
pass "fetch"

# --- services.sh with a fake backend that answers /api/runtime/busy.
mkdir -p "$TMP/bin" "$TMP/share/web" "$TMP/run" "$TMP/ws"
cat >"$TMP/bin/backend" <<'EOF'
#!/bin/bash
port=""
while [ $# -gt 0 ]; do [ "$1" = -listen ] && port="${2##*:}"; shift; done
exec python3 -c '
import http.server, sys
port, busy = int(sys.argv[1]), sys.argv[2]
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = open(busy, "rb").read() if self.path == "/api/runtime/busy" else b"{}"
        self.send_response(200); self.end_headers(); self.wfile.write(body)
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", port), H).serve_forever()
' "$port" "$BUSY_FILE"
EOF
chmod +x "$TMP/bin/backend"
BPORT="$(free_port)"
export GRASP_RUNTIME_RUN_DIR="$TMP/run" GRASP_RUNTIME_BIN_DIR="$TMP/bin" GRASP_RUNTIME_SHARE_DIR="$TMP/share"
export GRASP_SERVICES_WAIT=10
# BUSY_FILE / port / workspace come from the env file, as for an SSH session.
{
  echo "declare -x BUSY_FILE=\"$TMP/busy\""
  echo "declare -x ACP_BRIDGE_PORT=\"$BPORT\""
  echo "declare -x WORKSPACE_DIR=\"$TMP/ws\""
} >"$TMP/run/env"
echo '{"busy":false}' >"$TMP/busy"
svc() { bash "$SERVICES" "$@" >>"$TMP/services.out" 2>&1; }

rc=0; svc bogus backend || rc=$?; [ "$rc" = 2 ] || fail "services usage exit=$rc"
rc=0; svc start nothing || rc=$?; [ "$rc" = 2 ] || fail "services bad name exit=$rc"
svc start backend
wait_port "$BPORT" || fail "backend did not listen"
[ "$(bash "$SERVICES" status backend)" = "backend: running" ] || fail "status running"
svc start backend
grep -q "already running" "$TMP/services.out" || fail "second start should be a no-op"
PID1="$(cat "$TMP/run/backend.pid")"

echo '{"busy":true,"reason":"turn:default"}' >"$TMP/busy"
rc=0; svc restart backend --if-idle || rc=$?
[ "$rc" = 3 ] || fail "busy restart exit=$rc"
[ "$(cat "$TMP/run/backend.pid")" = "$PID1" ] || fail "busy restart must not restart"

echo '{"busy":false,"reason":""}' >"$TMP/busy"
svc restart backend --if-idle || fail "idle restart failed: $(cat "$TMP/services.out")"
PID2="$(cat "$TMP/run/backend.pid")"
[ "$PID2" != "$PID1" ] || fail "idle restart kept the old pid"
! kill -0 "$PID1" 2>/dev/null || fail "old backend still alive"
wait_port "$BPORT" || fail "restarted backend not listening"

svc stop backend
[ "$(bash "$SERVICES" status backend)" = "backend: stopped" ] || fail "status stopped"
! kill -0 "$PID2" 2>/dev/null || fail "backend still alive after stop"
svc restart backend --if-idle || fail "restart of a stopped backend should start it"
wait_port "$BPORT" || fail "backend not listening after restart from stopped"
svc stop backend

mv "$TMP/bin/backend" "$TMP/bin/backend.off"
rc=0; svc start backend || rc=$?; [ "$rc" = 1 ] || fail "missing binary exit=$rc"
mv "$TMP/bin/backend.off" "$TMP/bin/backend"

svc restart preview-inject || fail "preview-inject without PREVIEW_DIRECT should skip"
svc start preview-inject
grep -q "PREVIEW_DIRECT not set" "$TMP/services.out" || fail "preview-inject skip message"
[ "$(bash "$SERVICES" status preview-inject)" = "preview-inject: stopped" ] || fail "preview-inject status"
[ $((8#$(stat -c %a "$TMP/run") & 8#077)) = 0 ] || fail "run dir must not be group/other accessible"
pass "services.sh"

echo "test-runtime-bootstrap: all passed"

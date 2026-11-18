#!/usr/bin/env bash
# Offline smoke for the desktop image pieces: build-apps.py unit tests and
# desktop-session.sh start/fallback with fake openbox / wmctrl / launcher.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SESSION="$ROOT/sandbox/scripts/desktop-session.sh"
SERVICES="$ROOT/sandbox/scripts/services.sh"
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s "$ROOT/sandbox/desktop" -p 'test_*.py'

WORK="$(mktemp -d)"
cleanup() {
  pkill -f "$WORK/" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

FAKE="$WORK/bin"
mkdir -p "$FAKE" "$WORK/home"
# pgrep -x reads the process name. Renaming sleep is not portable: Ubuntu's
# multi-call coreutils dispatches by argv[0] and cannot run as "openbox".
cat >"$FAKE/openbox" <<'EOF'
#!/usr/bin/env python3
import ctypes
import time

libc = ctypes.CDLL(None, use_errno=True)
if libc.prctl(15, b"openbox", 0, 0, 0) != 0:  # PR_SET_NAME
    raise OSError(ctypes.get_errno(), "cannot set fake window-manager name")
time.sleep(600)
EOF
cat >"$FAKE/wmctrl" <<'EOF'
#!/bin/bash
[ "$1" = -m ] && pgrep -x openbox >/dev/null
EOF
cat >"$FAKE/xdpyinfo" <<'EOF'
#!/bin/bash
exit 0
EOF
cat >"$FAKE/grasp-desktop" <<'EOF'
#!/bin/bash
# grasp-desktop launcher -addr HOST:PORT
exec python3 -m http.server --bind 127.0.0.1 "${3##*:}" --directory "$(dirname "$0")"
EOF
chmod +x "$FAKE"/*

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
run() {
  env -i PATH="$1:/usr/bin:/bin" HOME="$WORK/home" DISPLAY=:77 \
    GRASP_DESKTOP_RUN_DIR="$WORK/run" GRASP_DESKTOP_MARKER="$2" \
    GRASP_DESKTOP_BIN="$FAKE/grasp-desktop" GRASP_DESKTOP_LAUNCHER_PORT="$port" \
    GRASP_DESKTOP_LOG_DIR="$WORK" bash "$SESSION" start
}
state() { cat "$WORK/run/state"; }
fail() {
  echo "FAIL: $*" >&2
  for log in "$WORK"/grasp-desktop-*.log; do
    [ ! -f "$log" ] || cat "$log" >&2
  done
  exit 1
}

# No marker: browser fallback with a reason.
if run "$FAKE" "$WORK/none" >/dev/null 2>&1; then fail "started without marker"; fi
state | grep -q '"mode":"browser","shellReady":false,"fallbackReason":"not a desktop image' || fail "no-marker state: $(state)"

# Marker but no window manager: fallback names the missing piece.
touch "$WORK/marker"
mkdir -p "$WORK/nowm"
cp "$FAKE/wmctrl" "$WORK/nowm/"
if run "$WORK/nowm" "$WORK/marker" >/dev/null 2>&1; then fail "started without openbox"; fi
state | grep -q '"fallbackReason":"missing openbox"' || fail "no-openbox state: $(state)"

# Full start.
run "$FAKE" "$WORK/marker" >/dev/null 2>&1 || fail "start failed: $(state)"
state | grep -q '"mode":"desktop","shellReady":true' || fail "ready state: $(state)"
apps="$WORK/home/.local/share/applications"
for id in grasp-browser grasp-launcher; do
  grep -Fxq 'Exec=/usr/local/bin/desktop-session.sh browser' "$apps/$id.desktop" || fail "wrong $id entry"
done
test -f "$WORK/home/.config/plank/dock1/launchers/grasp-terminal.dockitem" || fail "dock items missing"
grep -q 'DefaultIM=pinyin' "$WORK/home/.config/fcitx5/profile" || fail "fcitx5 profile missing"
grep -q "^GTK_IM_MODULE=fcitx$" "$WORK/run/session.env" && grep -q "^DISPLAY=" "$WORK/run/session.env" || fail "session.env: $(cat "$WORK/run/session.env" 2>&1)"

# Idempotent: a second start reuses the running shell.
run "$FAKE" "$WORK/marker" >/dev/null 2>&1 || fail "second start failed"
[ "$(pgrep -x openbox | wc -l)" = 1 ] || fail "openbox started twice"

# A runtime update fixes existing shortcuts even when direct preview is disabled.
for id in grasp-browser grasp-launcher; do
  sed -i 's|^Exec=.*|Exec=/opt/grasp-runtime/old/scripts/desktop-session.sh browser|' "$apps/$id.desktop"
done
ln -s "$SESSION" "$FAKE/desktop-session.sh"
env -i PATH="$FAKE:/usr/bin:/bin" HOME="$WORK/home" \
  GRASP_DESKTOP_MARKER="$WORK/marker" GRASP_RUNTIME_RUN_DIR="$WORK/runtime" \
  GRASP_RUNTIME_BIN_DIR="$FAKE" bash "$SERVICES" restart preview-inject >/dev/null
for id in grasp-browser grasp-launcher; do
  grep -Fxq 'Exec=/usr/local/bin/desktop-session.sh browser' "$apps/$id.desktop" || fail "stale $id entry"
done

echo "desktop-session smoke OK"

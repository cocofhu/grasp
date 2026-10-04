#!/bin/bash
# In-sandbox VNC preview stack (Xvfb + headed Chromium + x11vnc + websockify).
# Idempotent and mutually exclusive: concurrent callers (startup.sh background +
# any exec-triggered warmup) serialize on a flock so only one process brings the
# stack up; others wait then exit 0 once CDP :9222 is ready.
set -eu

export DISPLAY="${DISPLAY:-:99}"
CDP_LOOPBACK_PORT="${CDP_LOOPBACK_PORT:-9223}"
CDP_PORT="${CDP_PORT:-9222}"
VNC_PORT="${VNC_PORT:-5900}"
WS_PORT="${WS_PORT:-6080}"
PID_DIR="${VNC_PID_DIR:-/tmp/sandbox-vnc}"
# Fixed profile so cookies and logins survive a Chromium restart.
PROFILE_DIR="${VNC_CHROME_PROFILE_DIR:-${HOME:-/root}/.grasp-preview-chrome}"
mkdir -p "$PID_DIR"

LOCK_FILE="${PID_DIR}/start.lock"
exec 9>"$LOCK_FILE"
if ! flock -w 90 9; then
  echo "vnc-preview: failed to acquire start lock within 90s" >&2
  exit 1
fi

# Re-check under the lock (covers both the fast path and waiters after a peer finished).
tab_ok() {
  local resp id
  resp=$(curl -fsS -X PUT "http://127.0.0.1:${CDP_PORT}/json/new?about:blank" 2>/dev/null) || return 1
  id=$(printf '%s' "$resp" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  if [ -n "$id" ]; then
    curl -fsS -X DELETE "http://127.0.0.1:${CDP_PORT}/json/close/${id}" >/dev/null 2>&1 || true
  fi
  return 0
}

ws_ok() {
  (echo >/dev/tcp/127.0.0.1/"${WS_PORT}") >/dev/null 2>&1
}

if curl -fsS "http://127.0.0.1:${CDP_PORT}/json/version" >/dev/null 2>&1 \
  && tab_ok && ws_ok; then
  exit 0
fi

find_chrome() {
  local bin
  bin=$(find /ms-playwright -type f -name chrome -perm -111 2>/dev/null | head -1 || true)
  if [ -n "$bin" ]; then
    printf '%s' "$bin"
    return 0
  fi
  for c in chromium chromium-browser google-chrome-stable google-chrome; do
    if command -v "$c" >/dev/null 2>&1; then
      command -v "$c"
      return 0
    fi
  done
  return 1
}

if ! curl -fsS "http://127.0.0.1:${CDP_LOOPBACK_PORT}/json/version" >/dev/null 2>&1; then
  CHROME_BIN=$(find_chrome) || {
    echo "vnc-preview: no Chromium binary found" >&2
    exit 1
  }

  if ! pgrep -f "Xvfb ${DISPLAY}" >/dev/null 2>&1; then
    # Close flock FD 9 in children so the lock is released when this script exits
    # (otherwise Chrome/socat/x11vnc inherit it and block later warmup execs).
    Xvfb "$DISPLAY" -screen 0 1920x1080x24 9>&- >/tmp/sandbox-vnc-xvfb.log 2>&1 &
    echo $! >"$PID_DIR/xvfb.pid"
    sleep 1
  fi

  # No window manager on Xvfb. Explicit position and size are the bootstrap
  # window fallback; Approving NewTab windows are resized via CDP
  # (presentDesktop: normal state, then a 0,0 1920x1080 frame, so the tab strip
  # and address bar stay visible above the content area). Do not pass
  # --start-maximized: without a WM it does not fill the screen and it races the
  # later normal-state size.
  # --disable-infobars: suppress Chrome for Testing non-interactive product info
  # bar ("…is only for automated testing…") so it never paints into the VNC
  # framebuffer and does not reserve top viewport height (Chromium CfT switch).
  # --force-device-scale-factor=1: keep the framebuffer in CSS pixels before CDP
  # pins the same ratio on the content viewport.
  mkdir -p "$PROFILE_DIR"
  # A crashed Chromium leaves a profile lock that blocks the next start.
  rm -f "$PROFILE_DIR"/Singleton{Lock,Socket,Cookie}
  "$CHROME_BIN" \
    --user-data-dir="$PROFILE_DIR" \
    --no-sandbox \
    --disable-dev-shm-usage \
    --disable-infobars \
    --remote-debugging-port="${CDP_LOOPBACK_PORT}" \
    --window-size=1920,1080 \
    --window-position=0,0 \
    --force-device-scale-factor=1 \
    --no-first-run \
    --no-default-browser-check \
    about:blank \
    9>&- >/tmp/sandbox-vnc-chrome.log 2>&1 &
  echo $! >"$PID_DIR/chrome.pid"

  for i in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${CDP_LOOPBACK_PORT}/json/version" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done

  if ! curl -fsS "http://127.0.0.1:${CDP_LOOPBACK_PORT}/json/version" >/dev/null 2>&1; then
    echo "vnc-preview: Chromium CDP not ready on :${CDP_LOOPBACK_PORT}" >&2
    exit 1
  fi
fi

if ! pgrep -f "socat TCP-LISTEN:${CDP_PORT}" >/dev/null 2>&1; then
  socat TCP-LISTEN:"${CDP_PORT}",bind=0.0.0.0,fork,reuseaddr TCP:127.0.0.1:"${CDP_LOOPBACK_PORT}" \
    9>&- >/tmp/sandbox-vnc-socat.log 2>&1 &
  echo $! >"$PID_DIR/socat.pid"
fi

if ! pgrep -f "x11vnc .* -rfbport ${VNC_PORT}" >/dev/null 2>&1; then
  x11vnc -display "$DISPLAY" -rfbport "$VNC_PORT" -shared -forever -nopw -localhost -cursor arrow \
    9>&- >/tmp/sandbox-vnc-x11vnc.log 2>&1 &
  echo $! >"$PID_DIR/x11vnc.pid"
fi

if ! pgrep -f "websockify ${WS_PORT}" >/dev/null 2>&1; then
  websockify "$WS_PORT" "localhost:${VNC_PORT}" 9>&- >/tmp/sandbox-vnc-websockify.log 2>&1 &
  echo $! >"$PID_DIR/websockify.pid"
fi

for i in $(seq 1 60); do
  cdp_ok=0
  ws_ok_flag=0
  tab_ok_flag=0
  if curl -fsS "http://127.0.0.1:${CDP_PORT}/json/version" >/dev/null 2>&1; then
    cdp_ok=1
  fi
  if ws_ok; then
    ws_ok_flag=1
  fi
  if tab_ok; then
    tab_ok_flag=1
  fi
  if [ "$cdp_ok" = 1 ] && [ "$ws_ok_flag" = 1 ] && [ "$tab_ok_flag" = 1 ]; then
    echo "vnc-preview: ready (CDP :${CDP_PORT}, websockify :${WS_PORT})"
    exit 0
  fi
  sleep 0.5
done

echo "vnc-preview: CDP :${CDP_PORT} / websockify :${WS_PORT} not reachable" >&2
exit 1

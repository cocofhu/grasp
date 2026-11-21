#!/bin/bash
# In-sandbox VNC preview stack (Xvfb + headed Chromium + x11vnc + grasp-desktop
# vnc-ws, which also streams the PulseAudio sink on /audio; websockify when the
# runtime has no grasp-desktop).
# On the desktop image (/etc/grasp-desktop) desktop-session.sh first brings up
# the desktop shell on the same display; if it fails the stack falls back to
# browser mode (fallbackReason in /run/grasp-desktop/state).
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
RUNTIME_DIR="$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/.." && pwd)"
DESKTOP_BIN="${GRASP_DESKTOP_BIN:-$RUNTIME_DIR/bin/grasp-desktop}"
# Fixed socket so Chromium, desktop apps and parec find one daemon without
# XDG_RUNTIME_DIR.
PULSE_DIR="${GRASP_PULSE_DIR:-/tmp/grasp-pulse}"
export PULSE_SERVER="unix:${PULSE_DIR}/native"
mkdir -p "$PID_DIR"

LOCK_FILE="${PID_DIR}/start.lock"
exec 9>"$LOCK_FILE"
if ! flock -w 90 9; then
  echo "vnc-preview: failed to acquire start lock within 90s" >&2
  exit 1
fi

# Re-check under the lock (covers both the fast path and waiters after a peer finished).
tab_ok() {
  # Hidden CDP targets exercise tab creation without focusing Chromium. They
  # are disposed automatically when this probe disconnects.
  timeout 5s node --input-type=module - "${CDP_PORT}" 2>/dev/null <<'PROBE'
const response = await fetch(`http://127.0.0.1:${process.argv[2]}/json/version`)
if (!response.ok) process.exit(1)
const { webSocketDebuggerUrl } = await response.json()
const socket = new WebSocket(webSocketDebuggerUrl)
process.exitCode = 1
socket.onopen = () => socket.send(JSON.stringify({
  id: 1, method: 'Target.createTarget', params: { url: 'about:blank', background: true, hidden: true },
}))
socket.onmessage = ({ data }) => {
  const reply = JSON.parse(data)
  if (reply.id !== 1) return
  if (reply.result?.targetId) process.exitCode = 0
  socket.close()
}
PROBE
}

ws_ok() {
  (echo >/dev/tcp/127.0.0.1/"${WS_PORT}") >/dev/null 2>&1
}

# Desktop sound: a null sink apps play into; vnc-ws records its monitor.
# Images without pulseaudio keep a silent desktop.
start_pulse() {
  command -v pulseaudio >/dev/null 2>&1 || return 0
  if pactl info >/dev/null 2>&1; then
    return 0
  fi
  mkdir -p "$PULSE_DIR"
  pulseaudio --daemonize=yes --exit-idle-time=-1 --disallow-exit -n \
    --load="module-native-protocol-unix socket=${PULSE_DIR}/native auth-anonymous=1" \
    --load="module-null-sink sink_name=grasp sink_properties=device.description=Grasp" \
    --load="module-always-sink" \
    9>&- >/tmp/sandbox-vnc-pulse.log 2>&1 || {
    echo "vnc-preview: pulseaudio failed to start, desktop stays silent" >&2
    return 0
  }
  pactl set-default-sink grasp >/dev/null 2>&1 || true
}

if ws_ok && tab_ok; then
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

start_pulse

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

  DESKTOP_ARGS=()
  DESKTOP_ENV=()
  START_URL=about:blank
  if [ -e "${GRASP_DESKTOP_MARKER:-/etc/grasp-desktop}" ]; then
    if [ -x /usr/local/bin/desktop-session.sh ] && /usr/local/bin/desktop-session.sh start 9>&-; then
      SHARE_DIR="$(cd "$(dirname "$(readlink -f /usr/local/bin/desktop-session.sh)")/.." && pwd)/share/desktop"
      # NtpFooter: no "customize Chrome" / extension attribution under the launch page.
      DESKTOP_ARGS=(--load-extension="$SHARE_DIR/newtab" --disable-extensions-except="$SHARE_DIR/newtab"
        --disable-features=NtpFooter)
      START_URL=chrome://newtab/
      # Same session as the desktop apps: input method, locale, D-Bus.
      session_env="${GRASP_DESKTOP_RUN_DIR:-/run/grasp-desktop}/session.env"
      [ -f "$session_env" ] && mapfile -t DESKTOP_ENV <"$session_env"
    else
      echo "vnc-preview: desktop shell unavailable, falling back to browser mode" >&2
    fi
  fi

  # Browser mode has no window manager on Xvfb. Explicit position and size are the bootstrap
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
  env "${DESKTOP_ENV[@]}" "$CHROME_BIN" \
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
    "${DESKTOP_ARGS[@]}" \
    "$START_URL" \
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

if ! ws_ok; then
  if [ -x "$DESKTOP_BIN" ]; then
    "$DESKTOP_BIN" vnc-ws -addr "0.0.0.0:${WS_PORT}" -vnc "127.0.0.1:${VNC_PORT}" \
      9>&- >/tmp/sandbox-vnc-ws.log 2>&1 &
  else
    websockify "$WS_PORT" "localhost:${VNC_PORT}" 9>&- >/tmp/sandbox-vnc-websockify.log 2>&1 &
  fi
  echo $! >"$PID_DIR/websockify.pid"
fi

for i in $(seq 1 60); do
  if ws_ok && tab_ok; then
    echo "vnc-preview: ready (CDP :${CDP_PORT}, vnc-ws :${WS_PORT})"
    exit 0
  fi
  sleep 0.5
done

echo "vnc-preview: CDP :${CDP_PORT} / vnc-ws :${WS_PORT} not reachable" >&2
exit 1

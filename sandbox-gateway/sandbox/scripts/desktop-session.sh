#!/bin/bash
# Desktop shell for the desktop image (marker /etc/grasp-desktop): Openbox +
# xcompmgr (translucent dock, window shadows) + Plank dock + fcitx5 (pinyin) +
# wallpaper + the grasp-desktop launcher on 127.0.0.1:6091, all on the preview
# stack's Xvfb display.
#
#   desktop-session.sh start    bring the shell up (idempotent); exit 0 once the
#                               window manager and launcher answer, otherwise
#                               record fallbackReason and exit 1 so
#                               vnc-preview.sh falls back to browser mode
#   desktop-session.sh browser  open a Chromium window on the new tab page, which
#                               the newtab extension fills with the launch page
#
# State for the backend and desktop MCP: $GRASP_DESKTOP_RUN_DIR/state, JSON
# {"mode":"desktop"|"browser","shellReady":bool,"fallbackReason":"..."}.
set -u

export DISPLAY="${DISPLAY:-:99}"
RUN_DIR="${GRASP_DESKTOP_RUN_DIR:-/run/grasp-desktop}"
MARKER="${GRASP_DESKTOP_MARKER:-/etc/grasp-desktop}"
LAUNCHER_PORT="${GRASP_DESKTOP_LAUNCHER_PORT:-6091}"
HOME_URL="chrome://newtab/"
PROFILE_DIR="${VNC_CHROME_PROFILE_DIR:-${HOME:-/root}/.grasp-preview-chrome}"
SELF="$(readlink -f "${BASH_SOURCE[0]}")"
RUNTIME_DIR="$(cd "$(dirname "$SELF")/.." && pwd)"
SHARE="${GRASP_DESKTOP_SHARE:-$RUNTIME_DIR/share/desktop}"
DESKTOP_BIN="${GRASP_DESKTOP_BIN:-$RUNTIME_DIR/bin/grasp-desktop}"
LOG_DIR="${GRASP_DESKTOP_LOG_DIR:-/tmp}"

export GTK_IM_MODULE=fcitx QT_IM_MODULE=fcitx XMODIFIERS=@im=fcitx SDL_IM_MODULE=fcitx
export LANG="${LANG:-zh_CN.UTF-8}"
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-${HOME:-/root}/.config}"
export XDG_DATA_HOME="${XDG_DATA_HOME:-${HOME:-/root}/.local/share}"

write_state() { # mode ready reason
  mkdir -p "$RUN_DIR"
  local reason=${3//\\/\\\\}
  reason=${reason//\"/\\\"}
  printf '{"mode":"%s","shellReady":%s,"fallbackReason":"%s"}\n' "$1" "$2" "$reason" >"$RUN_DIR/state.tmp"
  mv -f "$RUN_DIR/state.tmp" "$RUN_DIR/state"
}

fail() {
  echo "desktop-session: $1" >&2
  write_state browser false "$1"
  exit 1
}

running() { pgrep -x "$1" >/dev/null 2>&1; }

port_ok() { (echo >/dev/tcp/127.0.0.1/"$1") >/dev/null 2>&1; }

wait_for() { # tries cmd...
  local n=$1
  shift
  for _ in $(seq 1 "$n"); do
    if "$@"; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

start_bg() { # name cmd...
  local name=$1
  shift
  setsid "$@" 9>&- >"$LOG_DIR/grasp-desktop-$name.log" 2>&1 </dev/null &
}

dbus_session() {
  local envf="$RUN_DIR/dbus.env"
  if [ -f "$envf" ]; then
    # shellcheck disable=SC1090
    . "$envf"
    if [ -n "${DBUS_SESSION_BUS_PID:-}" ] && kill -0 "$DBUS_SESSION_BUS_PID" 2>/dev/null; then
      export DBUS_SESSION_BUS_ADDRESS DBUS_SESSION_BUS_PID
      return 0
    fi
  fi
  command -v dbus-launch >/dev/null 2>&1 || return 0
  dbus-launch --sh-syntax 9>&- >"$envf" 2>/dev/null || return 0
  # shellcheck disable=SC1090
  . "$envf"
  export DBUS_SESSION_BUS_ADDRESS DBUS_SESSION_BUS_PID
}

# session.env: KEY=VALUE lines that grasp-desktop applies to the apps it
# starts (desktop MCP runs outside this session, under the Agent CLI).
write_session_env() {
  local k
  for k in DISPLAY DBUS_SESSION_BUS_ADDRESS GTK_IM_MODULE QT_IM_MODULE XMODIFIERS SDL_IM_MODULE \
    LANG XDG_CONFIG_HOME XDG_DATA_HOME; do
    [ -n "${!k:-}" ] && printf '%s=%s\n' "$k" "${!k}"
  done >"$RUN_DIR/session.env.tmp" && mv -f "$RUN_DIR/session.env.tmp" "$RUN_DIR/session.env"
}

seed_config() {
  local apps="$XDG_DATA_HOME/applications" launchers="$XDG_CONFIG_HOME/plank/dock1/launchers"
  mkdir -p "$apps" "$launchers" "$XDG_CONFIG_HOME/fcitx5"
  for f in "$SHARE"/applications/*.desktop; do
    [ -f "$f" ] || continue
    # Chromium's WM_CLASS instance carries the profile dir; the dock (bamf)
    # matches on it to group the browser window under its launcher.
    sed -e "s#@DESKTOP_SESSION@#$SELF#g" -e "s#@DESKTOP_SHARE@#$SHARE#g" \
      -e "s#@BROWSER_WM_CLASS@#chromium-browser ($PROFILE_DIR)#g" "$f" >"$apps/$(basename "$f")"
  done
  if [ ! -f "$launchers/.seeded" ]; then
    for id in grasp-launcher grasp-browser grasp-terminal grasp-files; do
      [ -f "$apps/$id.desktop" ] || continue
      printf '[PlankDockItemPreferences]\nLauncher=file://%s\n' "$apps/$id.desktop" >"$launchers/$id.dockitem"
    done
    : >"$launchers/.seeded"
  fi
  mkdir -p "$XDG_DATA_HOME/plank/themes"
  rm -rf "$XDG_DATA_HOME/plank/themes/Grasp"
  cp -r "$SHARE/plank/Grasp" "$XDG_DATA_HOME/plank/themes/Grasp" 2>/dev/null || true
  [ -f "$XDG_CONFIG_HOME/fcitx5/profile" ] || cp "$SHARE/fcitx5-profile" "$XDG_CONFIG_HOME/fcitx5/profile" 2>/dev/null || true
}

wallpaper() {
  local img="$RUN_DIR/wallpaper-grasp.png"
  if [ ! -f "$img" ] && command -v convert >/dev/null 2>&1; then
    convert -size 1920x1080 xc: -sparse-color Shepards \
      '0,0 #c9bfff 700,0 #8f7bff 1920,0 #5b44e6 0,1080 #7a64f5 1920,1080 #3d2bbf 1300,700 #6550f0 300,500 #a596ff' \
      -blur 0x40 "$img" 2>/dev/null || rm -f "$img"
  fi
  if [ -f "$img" ] && command -v feh >/dev/null 2>&1; then
    feh --no-fehbg --bg-fill "$img" >/dev/null 2>&1 && return 0
  fi
  command -v xsetroot >/dev/null 2>&1 && xsetroot -solid '#6a55ec' >/dev/null 2>&1
  return 0
}

wm_ok() { wmctrl -m >/dev/null 2>&1; }

launcher_ok() { port_ok "$LAUNCHER_PORT"; }

cmd_start() {
  [ -e "$MARKER" ] || fail "not a desktop image ($MARKER missing)"
  mkdir -p "$RUN_DIR" || fail "cannot create $RUN_DIR"
  for bin in openbox wmctrl; do
    command -v "$bin" >/dev/null 2>&1 || fail "missing $bin"
  done
  [ -x "$DESKTOP_BIN" ] || fail "missing $DESKTOP_BIN"
  wait_for 40 xdpyinfo_ok || fail "display $DISPLAY not available"
  if running openbox && wm_ok && launcher_ok; then
    write_state desktop true ""
    return 0
  fi
  write_state desktop false ""
  dbus_session
  write_session_env
  seed_config
  running openbox || start_bg openbox openbox
  wait_for 40 wm_ok || fail "openbox did not start"
  wallpaper
  if ! running xcompmgr && command -v xcompmgr >/dev/null 2>&1; then
    start_bg xcompmgr xcompmgr -c -C -r 14 -o 0.28 -l -18 -t -12
  fi
  if command -v gsettings >/dev/null 2>&1; then
    local dock=net.launchpad.plank.dock.settings:/net/launchpad/plank/docks/dock1/
    gsettings set "$dock" hide-mode none >/dev/null 2>&1 || true
    gsettings set "$dock" theme Grasp >/dev/null 2>&1 || true
    gsettings set "$dock" icon-size 44 >/dev/null 2>&1 || true
  fi
  if ! running fcitx5 && command -v fcitx5 >/dev/null 2>&1; then
    start_bg fcitx5 fcitx5 --replace
  fi
  if ! running plank && command -v plank >/dev/null 2>&1; then
    start_bg plank plank
  fi
  launcher_ok || GRASP_DESKTOP_SHARE="$SHARE" start_bg launcher "$DESKTOP_BIN" launcher -addr "127.0.0.1:${LAUNCHER_PORT}"
  wait_for 40 launcher_ok || fail "launcher did not listen on :${LAUNCHER_PORT}"
  write_state desktop true ""
  echo "desktop-session: ready (openbox, launcher :${LAUNCHER_PORT})"
}

xdpyinfo_ok() {
  if command -v xdpyinfo >/dev/null 2>&1; then
    xdpyinfo >/dev/null 2>&1
  else
    [ -S "/tmp/.X11-unix/X${DISPLAY#:}" ]
  fi
}

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

cmd_browser() {
  local url="${1:-$HOME_URL}" chrome
  if ! pgrep -f -- "--user-data-dir=$PROFILE_DIR" >/dev/null 2>&1; then
    exec /usr/local/bin/vnc-preview.sh
  fi
  chrome=$(find_chrome) || {
    echo "desktop-session: no Chromium binary found" >&2
    exit 1
  }
  # Same profile: Chromium hands the request to the running instance (CDP and
  # browser MCP keep seeing one browser).
  start_bg browser "$chrome" --user-data-dir="$PROFILE_DIR" --new-window "$url"
}

case "${1:-start}" in
start) cmd_start ;;
browser) cmd_browser "${2:-}" ;;
*)
  echo "usage: desktop-session.sh start|browser [url]" >&2
  exit 2
  ;;
esac

#!/bin/bash
# Long-running Grasp services inside the sandbox (backend / ACP bridge and the
# direct-preview injector). startup.sh starts them through here, and Grasp calls
# `restart` over SSH after installing a new runtime bundle, so a new bundle
# takes over without restarting the container.
#
# Usage: services.sh <start|stop|restart|status> <backend|preview-inject> [--if-idle]
# Exit:  0 ok · 1 failed · 2 usage · 3 backend busy (with --if-idle; retry later)
set -u

RUN_DIR="${GRASP_RUNTIME_RUN_DIR:-/run/grasp-runtime}"
ENV_FILE="$RUN_DIR/env"
BIN_DIR="${GRASP_RUNTIME_BIN_DIR:-/usr/local/bin}"
SHARE_DIR="${GRASP_RUNTIME_SHARE_DIR:-/usr/local/share/backend}"
INJECT_PID_FILE="${PREVIEW_INJECT_PID_DIR:-/tmp/sandbox-preview-inject}/preview-inject.pid"

# SSH sessions do not inherit PID1's environment; startup.sh saves it here.
# Sourced at top level so `declare -x` lines stay global.
if [ -r "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  . "$ENV_FILE"
fi

truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

backend_port() { echo "${ACP_BRIDGE_PORT:-${CURSOR_ACP_PORT:-8765}}"; }

pid_alive() { [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; }

read_pid() { [ -f "$1" ] && cat "$1" 2>/dev/null; }

port_open() { (echo >/dev/tcp/127.0.0.1/"$1") >/dev/null 2>&1; }

# Restarts come from an SSH session: write to the container log and detach.
service_log_redirect() {
  if [ "${GRASP_SERVICES_INHERIT_STDIO:-}" = "1" ]; then
    return
  fi
  if [ -w /proc/1/fd/1 ]; then
    exec >>/proc/1/fd/1 2>>/proc/1/fd/2
  else
    exec >>"$RUN_DIR/services.log" 2>&1
  fi
}

stop_pid() {
  local pid="$1" i
  pid_alive "$pid" || return 0
  kill -TERM "$pid" 2>/dev/null || true
  for i in $(seq 1 100); do
    pid_alive "$pid" || return 0
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
}

start_backend() {
  local port pid_file="$RUN_DIR/backend.pid" pw
  port="$(backend_port)"
  if pid_alive "$(read_pid "$pid_file")"; then
    echo "services: backend already running"
    return 0
  fi
  if [ ! -x "$BIN_DIR/backend" ] || [ ! -d "$SHARE_DIR/web" ]; then
    echo "services: backend binary or web assets missing" >&2
    return 1
  fi
  local args=(-listen "0.0.0.0:${port}" -web "$SHARE_DIR/web" -gin-mode release)
  pw="${ACP_BRIDGE_PASSWORD:-${CURSOR_ACP_PASSWORD:-}}"
  [ -n "$pw" ] && args+=(-password "$pw")
  export ACP_BRIDGE_MODEL="${ACP_BRIDGE_MODEL:-${CURSOR_ACP_MODEL:-}}"
  (
    service_log_redirect
    cd "${WORKSPACE_DIR:-/root/workspace}" || exit 1
    exec setsid "$BIN_DIR/backend" "${args[@]}" </dev/null
  ) &
  echo $! >"$pid_file"
  echo "services: backend started (pid $!, port ${port})"
}

wait_backend() {
  local port i
  port="$(backend_port)"
  for i in $(seq 1 "${GRASP_SERVICES_WAIT:-60}"); do
    port_open "$port" && return 0
    sleep 1
  done
  echo "services: backend not listening on :${port}" >&2
  return 1
}

backend_busy() {
  local port out rc
  port="$(backend_port)"
  out="$(curl -sS -m 3 -o - -w '\n%{http_code}' "http://127.0.0.1:${port}/api/runtime/busy" 2>/dev/null)"
  rc=$?
  # Not listening: nothing to interrupt.
  [ "$rc" = 7 ] && return 1
  [ "$rc" = 0 ] || return 0
  [ "${out##*$'\n'}" = 200 ] || return 0
  case "${out%$'\n'*}" in
    *'"busy":false'*) return 1 ;;
    *) return 0 ;;
  esac
}

stop_backend() {
  stop_pid "$(read_pid "$RUN_DIR/backend.pid")"
  rm -f "$RUN_DIR/backend.pid"
}

start_preview_inject() {
  local pid_file="$RUN_DIR/preview-inject.pid"
  if ! truthy "${PREVIEW_DIRECT:-}"; then
    echo "services: PREVIEW_DIRECT not set, preview-inject skipped"
    return 0
  fi
  if pid_alive "$(read_pid "$pid_file")"; then
    echo "services: preview-inject already running"
    return 0
  fi
  (
    service_log_redirect
    exec setsid "$BIN_DIR/preview-inject.sh" </dev/null
  ) &
  echo $! >"$pid_file"
  echo "services: preview-inject started (pid $!)"
}

stop_preview_inject() {
  # TERM lets preview-inject.sh drop its iptables rules and stop the binary.
  stop_pid "$(read_pid "$RUN_DIR/preview-inject.pid")"
  stop_pid "$(read_pid "$INJECT_PID_FILE")"
  rm -f "$RUN_DIR/preview-inject.pid"
}

status_of() {
  if pid_alive "$(read_pid "$RUN_DIR/$1.pid")"; then
    echo "$1: running"
  else
    echo "$1: stopped"
  fi
}

usage() {
  echo "usage: services.sh <start|stop|restart|status> <backend|preview-inject> [--if-idle]" >&2
  exit 2
}

main() {
  local action="${1:-}" name="${2:-}" if_idle=0
  [ "${3:-}" = "--if-idle" ] && if_idle=1
  case "$name" in backend|preview-inject) ;; *) usage ;; esac
  mkdir -p "$RUN_DIR"
  chmod 700 "$RUN_DIR" 2>/dev/null || true
  case "$action" in
    start)
      if [ "$name" = backend ]; then start_backend; else start_preview_inject; fi
      ;;
    stop)
      if [ "$name" = backend ]; then stop_backend; else stop_preview_inject; fi
      ;;
    restart)
      if [ "$name" = backend ]; then
        if [ "$if_idle" = 1 ] && backend_busy; then
          echo "services: backend busy, restart deferred"
          exit 3
        fi
        stop_backend
        start_backend || exit 1
        wait_backend || exit 1
      else
        if ! truthy "${PREVIEW_DIRECT:-}"; then
          echo "services: PREVIEW_DIRECT not set, preview-inject skipped"
          exit 0
        fi
        stop_preview_inject
        start_preview_inject || exit 1
      fi
      ;;
    status) status_of "$name" ;;
    *) usage ;;
  esac
}

main "$@"

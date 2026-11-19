#!/bin/bash
# Long-running Grasp services inside the sandbox (backend / ACP bridge, the
# direct-preview injector and the E2B guest cache cap). startup.sh starts them
# through here, and Grasp calls `restart` over SSH after installing a new
# runtime bundle, so a new bundle takes over without restarting the container.
#
# Usage: services.sh <start|stop|restart|status> <backend|preview-inject|memory-cap> [--if-idle]
# Exit:  0 ok · 1 failed · 2 usage · 3 backend busy (with --if-idle; retry later)
set -u

RUN_DIR="${GRASP_RUNTIME_RUN_DIR:-/run/grasp-runtime}"
ENV_FILE="$RUN_DIR/env"
BIN_DIR="${GRASP_RUNTIME_BIN_DIR:-/usr/local/bin}"
SHARE_DIR="${GRASP_RUNTIME_SHARE_DIR:-/usr/local/share/backend}"
INJECT_PID_FILE="${PREVIEW_INJECT_PID_DIR:-/tmp/sandbox-preview-inject}/preview-inject.pid"
# Scripts that are not linked into BIN_DIR by the image live next to this one
# in the installed bundle; resolve through the /usr/local/bin symlink.
SCRIPT_DIR="$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")"

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

backend_port() { echo "${ACP_BRIDGE_PORT:-8765}"; }

pid_alive() { [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; }

read_pid() { [ -f "$1" ] && cat "$1" 2>/dev/null; }

port_open() { (echo >/dev/tcp/127.0.0.1/"$1") >/dev/null 2>&1; }

# Restarts come from an SSH session: write to the container log and detach.
# GRASP_SERVICES_LOG names a provider's collected log (E2B: bootstrap.log). A
# PID1 whose stdout is /dev/null (E2B init) would silently discard the logs.
service_log_redirect() {
  if [ "${GRASP_SERVICES_INHERIT_STDIO:-}" = "1" ]; then
    return
  fi
  if [ -n "${GRASP_SERVICES_LOG:-}" ] && touch "$GRASP_SERVICES_LOG" 2>/dev/null; then
    exec >>"$GRASP_SERVICES_LOG" 2>&1
  elif [ -w /proc/1/fd/1 ] && [ "$(readlink /proc/1/fd/1 2>/dev/null)" != /dev/null ]; then
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
  pw="${ACP_BRIDGE_PASSWORD:-}"
  [ -n "$pw" ] && args+=(-password "$pw")
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

MEMCAP_UNIT=grasp-memory-cap

# E2B guests boot systemd: it restarts memory-cap if anything kills it and
# keeps the OOM killer off it. GRASP_SERVICES_SYSTEMD=0/1 forces the choice.
memcap_systemd() {
  case "${GRASP_SERVICES_SYSTEMD:-}" in
    0) return 1 ;;
    1) return 0 ;;
  esac
  [ "$(cat /proc/1/comm 2>/dev/null)" = systemd ] && command -v systemd-run >/dev/null 2>&1
}

# The unit outlives this bundle version: point it at current/, whose old
# version directories are pruned by later runtime updates.
memcap_exec() {
  local current="${GRASP_RUNTIME_ROOT:-/opt/grasp-runtime}/current/scripts/memory-cap.sh"
  if [ "$(readlink -f "$current" 2>/dev/null)" = "$SCRIPT_DIR/memory-cap.sh" ]; then
    echo "$current"
  else
    echo "$SCRIPT_DIR/memory-cap.sh"
  fi
}

start_memory_cap() {
  local pid_file="$RUN_DIR/memory-cap.pid" v
  if [ "${E2B_SANDBOX:-}" != true ]; then
    echo "services: not an E2B VM guest, memory-cap skipped"
    return 0
  fi
  if memcap_systemd; then
    if systemctl is-active --quiet "$MEMCAP_UNIT"; then
      echo "services: memory-cap already running"
      return 0
    fi
    systemctl reset-failed "$MEMCAP_UNIT" >/dev/null 2>&1 || true
    local args=(--unit="$MEMCAP_UNIT" --collect --quiet -p Restart=always -p RestartSec=5
      -p OOMScoreAdjust=-1000 -p Nice=19 -p IOSchedulingClass=idle -E E2B_SANDBOX=true)
    for v in $(compgen -e); do
      case "$v" in GRASP_MEMORY_CAP_*) args+=(-E "$v=${!v}") ;; esac
    done
    if [ -n "${GRASP_SERVICES_LOG:-}" ]; then
      args+=(-p "StandardOutput=append:$GRASP_SERVICES_LOG" -p "StandardError=append:$GRASP_SERVICES_LOG")
    fi
    systemd-run "${args[@]}" "$(memcap_exec)" run || return 1
    echo "services: memory-cap started (systemd unit $MEMCAP_UNIT)"
    return 0
  fi
  if pid_alive "$(read_pid "$pid_file")"; then
    echo "services: memory-cap already running"
    return 0
  fi
  (
    service_log_redirect
    exec setsid "$(memcap_exec)" run </dev/null
  ) &
  echo $! >"$pid_file"
  echo "services: memory-cap started (pid $!)"
}

stop_memory_cap() {
  if memcap_systemd; then
    systemctl stop "$MEMCAP_UNIT" >/dev/null 2>&1 || true
  fi
  stop_pid "$(read_pid "$RUN_DIR/memory-cap.pid")"
  rm -f "$RUN_DIR/memory-cap.pid"
}

status_of() {
  if [ "$1" = memory-cap ] && memcap_systemd; then
    if systemctl is-active --quiet "$MEMCAP_UNIT"; then echo "$1: running"; else echo "$1: stopped"; fi
    return
  fi
  if pid_alive "$(read_pid "$RUN_DIR/$1.pid")"; then
    echo "$1: running"
  else
    echo "$1: stopped"
  fi
}

usage() {
  echo "usage: services.sh <start|stop|restart|status> <backend|preview-inject|memory-cap> [--if-idle]" >&2
  exit 2
}

main() {
  local action="${1:-}" name="${2:-}" if_idle=0
  [ "${3:-}" = "--if-idle" ] && if_idle=1
  case "$name" in backend|preview-inject|memory-cap) ;; *) usage ;; esac
  mkdir -p "$RUN_DIR"
  chmod 700 "$RUN_DIR" 2>/dev/null || true
  case "$action" in
    start)
      case "$name" in
        backend) start_backend ;;
        preview-inject) start_preview_inject ;;
        memory-cap) start_memory_cap ;;
      esac
      ;;
    stop)
      case "$name" in
        backend) stop_backend ;;
        preview-inject) stop_preview_inject ;;
        memory-cap) stop_memory_cap ;;
      esac
      ;;
    restart)
      if [ "$name" = memory-cap ]; then
        stop_memory_cap
        start_memory_cap || exit 1
      elif [ "$name" = backend ]; then
        if [ "$if_idle" = 1 ] && backend_busy; then
          echo "services: backend busy, restart deferred"
          exit 3
        fi
        stop_backend
        start_backend || exit 1
        wait_backend || exit 1
      else
        # Runtime updates restart this service while the desktop stays running.
        if [ -x "$BIN_DIR/desktop-session.sh" ]; then
          "$BIN_DIR/desktop-session.sh" refresh-launchers || echo "services: could not refresh desktop launchers" >&2
        fi
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

#!/bin/bash
# In-sandbox HTML injector for IP-direct preview (PREVIEW_DIRECT=1).
# Listens on 17980, then REDIRECTs inbound PREVIEW_PORT → 17980.
# Idempotent: concurrent callers serialize on a flock; already-healthy exits 0.
#
# grasp:set-upstream (Grasp checks for this marker before calling it)
#   preview-inject.sh set-upstream <port|reset>
# Points the injector at an app on another port while PREVIEW_PORT stays the
# public entry. Prints one line "result=<status> preview_port=<n> ...":
#   ok · reset · busy (PREVIEW_PORT already serves another app) · nolisten ·
#   denied (sandbox service port) · invalid · unavailable (no direct preview)
set -eu

truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

LISTEN_PORT="${PREVIEW_INJECT_LISTEN_PORT:-17980}"
CHAIN="${PREVIEW_INJECT_CHAIN:-APPROVING-PREVIEW}"
PID_DIR="${PREVIEW_INJECT_PID_DIR:-/tmp/sandbox-preview-inject}"
BIN="${PREVIEW_INJECT_BIN:-/usr/local/bin/preview-inject}"
LOG="${PREVIEW_INJECT_LOG:-/tmp/preview-inject.log}"
UPSTREAM_FILE="${PREVIEW_INJECT_UPSTREAM_FILE:-${PID_DIR}/upstream}"
ENV_FILE="${GRASP_RUNTIME_RUN_DIR:-/run/grasp-runtime}/env"
COMMENT="grasp-preview-inject"

preview_inject_missing_env() {
  if ! truthy "${PREVIEW_DIRECT:-}"; then
    echo "preview-inject: PREVIEW_DIRECT not set, skip"
    return 0
  fi
  if [ -n "${PREVIEW_AUTO_INJECT:-}" ] && ! truthy "${PREVIEW_AUTO_INJECT}"; then
    echo "preview-inject: PREVIEW_AUTO_INJECT off, skip"
    return 0
  fi
  if [ -z "${PREVIEW_PORT:-}" ]; then
    echo "preview-inject: PREVIEW_PORT missing, skip" >&2
    return 0
  fi
  return 1
}

# Print the iptables plan (no execution). Used by dry-run and tests.
preview_inject_print_rules() {
  cat <<EOF
# chain ${CHAIN} (nat); comment ${COMMENT}
iptables -t nat -N ${CHAIN} || iptables -t nat -F ${CHAIN}
iptables -t nat -C PREROUTING -j ${CHAIN} || iptables -t nat -A PREROUTING -j ${CHAIN}
iptables -t nat -A ${CHAIN} -p tcp --dport ${PREVIEW_PORT} -m comment --comment ${COMMENT} -j REDIRECT --to-ports ${LISTEN_PORT}
EOF
}

listen_ok() {
  (echo >/dev/tcp/127.0.0.1/"${LISTEN_PORT}") >/dev/null 2>&1
}

bin_pid() {
  if [ -f "${PID_DIR}/preview-inject.pid" ]; then
    cat "${PID_DIR}/preview-inject.pid"
  fi
}

kill_bin() {
  local pid
  pid=$(bin_pid || true)
  if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -f "${PID_DIR}/preview-inject.pid"
}

remove_rules() {
  command -v iptables >/dev/null 2>&1 || return 0
  iptables -t nat -F "$CHAIN" 2>/dev/null || true
  iptables -t nat -D PREROUTING -j "$CHAIN" 2>/dev/null || true
  iptables -t nat -X "$CHAIN" 2>/dev/null || true
}

ensure_rules() {
  command -v iptables >/dev/null 2>&1 || return 1
  iptables -t nat -N "$CHAIN" 2>/dev/null || iptables -t nat -F "$CHAIN"
  if ! iptables -t nat -C PREROUTING -j "$CHAIN" 2>/dev/null; then
    iptables -t nat -A PREROUTING -j "$CHAIN"
  fi
  iptables -t nat -A "$CHAIN" -p tcp --dport "$PREVIEW_PORT" -m comment --comment "$COMMENT" -j REDIRECT --to-ports "$LISTEN_PORT"
}

start_bin() {
  mkdir -p "$PID_DIR"
  if [ ! -x "$BIN" ]; then
    echo "preview-inject: binary not found ($BIN)" >&2
    return 1
  fi
  "$BIN" \
    --listen "0.0.0.0:${LISTEN_PORT}" \
    --upstream "http://127.0.0.1:${PREVIEW_PORT}" \
    --upstream-file "$UPSTREAM_FILE" \
    >>"$LOG" 2>&1 &
  echo $! >"${PID_DIR}/preview-inject.pid"
}

wait_listen() {
  local i
  for i in $(seq 1 50); do
    if listen_ok; then
      return 0
    fi
    sleep 0.1
  done
  echo "preview-inject: not listening on :${LISTEN_PORT}" >&2
  return 1
}

rules_ok() {
  command -v iptables >/dev/null 2>&1 || return 1
  iptables -t nat -C "$CHAIN" -p tcp --dport "$PREVIEW_PORT" -m comment --comment "$COMMENT" -j REDIRECT --to-ports "$LISTEN_PORT" 2>/dev/null
}

healthy() {
  listen_ok && rules_ok
}

supervise_loop() {
  local pid
  while true; do
    pid=$(bin_pid || true)
    if [ -z "${pid:-}" ] || ! kill -0 "$pid" 2>/dev/null; then
      echo "preview-inject: process down, dropping rules then restarting" >&2
      remove_rules || true
      start_bin || { sleep 2; continue; }
      if ! wait_listen; then
        kill_bin || true
        sleep 2
        continue
      fi
    fi
    ensure_rules || echo "preview-inject: iptables ensure failed (privileged / NET_ADMIN?)" >&2
    # Backgrounded so TERM (services.sh restart) is handled without waiting out the sleep.
    sleep 5 &
    wait $! || true
  done
}

preview_inject_main() {
  if preview_inject_missing_env; then
    exit 0
  fi
  if [ "${PREVIEW_PORT}" = "${LISTEN_PORT}" ]; then
    echo "preview-inject: PREVIEW_PORT must not equal listen ${LISTEN_PORT}" >&2
    exit 0
  fi
  if [ "${PREVIEW_INJECT_DRY_RUN:-}" = "1" ]; then
    preview_inject_print_rules
    exit 0
  fi

  mkdir -p "$PID_DIR"
  local lock="${PID_DIR}/start.lock"
  exec 9>"$lock"
  if ! flock -w 90 9; then
    echo "preview-inject: failed to acquire start lock within 90s" >&2
    exit 1
  fi

  if healthy; then
    echo "preview-inject: already healthy (listen :${LISTEN_PORT}, REDIRECT ${PREVIEW_PORT})"
    exit 0
  fi

  if ! command -v iptables >/dev/null 2>&1; then
    echo "preview-inject: iptables not found, skip" >&2
    exit 0
  fi
  if [ ! -x "$BIN" ]; then
    echo "preview-inject: ${BIN} missing, skip (rebuild sandbox image)" >&2
    exit 0
  fi

  cleanup() {
    kill_bin || true
    remove_rules || true
  }
  trap cleanup EXIT
  trap 'exit 0' INT TERM

  start_bin || { echo "preview-inject: failed to start binary" >&2; exit 0; }
  if ! wait_listen; then
    echo "preview-inject: listen failed, skip" >&2
    exit 0
  fi
  if ! ensure_rules; then
    echo "preview-inject: iptables failed (need privileged / NET_ADMIN), skip" >&2
    exit 0
  fi
  echo "preview-inject: ready (REDIRECT ${PREVIEW_PORT} → ${LISTEN_PORT})"
  exec 9>&-

  supervise_loop
}

reserved_port() {
  local r
  for r in 22 5900 6080 8744 8765 9222 "$LISTEN_PORT" "${CDP_PORT:-}" "${VNC_PORT:-}" \
    "${WS_PORT:-}" "${CODE_SERVER_PORT:-}" "${ACP_BRIDGE_PORT:-}"; do
    if [ -n "$r" ] && [ "$1" = "$r" ]; then
      return 0
    fi
  done
  return 1
}

port_listening() {
  ss -tlnH 2>/dev/null | awk '{print $4}' | grep -qE ":$1\$"
}

preview_set_upstream() {
  local want="${1:-}" pp tmp
  if ! truthy "${PREVIEW_DIRECT:-}" || [ -z "${PREVIEW_PORT:-}" ]; then
    echo "result=unavailable"
    return 0
  fi
  pp="$PREVIEW_PORT"
  if [ "$want" = reset ] || [ "$want" = "$pp" ]; then
    rm -f "$UPSTREAM_FILE"
    echo "result=reset preview_port=${pp}"
    return 0
  fi
  case "$want" in
    ''|*[!0-9]*) echo "result=invalid preview_port=${pp}"; return 2 ;;
  esac
  if [ "$want" -lt 1 ] || [ "$want" -gt 65535 ]; then
    echo "result=invalid preview_port=${pp}"
    return 2
  fi
  if reserved_port "$want"; then
    echo "result=denied preview_port=${pp}"
    return 0
  fi
  if ! port_listening "$want"; then
    echo "result=nolisten preview_port=${pp}"
    return 0
  fi
  if port_listening "$pp"; then
    echo "result=busy preview_port=${pp}"
    return 0
  fi
  mkdir -p "$(dirname "$UPSTREAM_FILE")"
  tmp="${UPSTREAM_FILE}.$$"
  printf '%s\n' "$want" >"$tmp"
  mv -f "$tmp" "$UPSTREAM_FILE"
  echo "result=ok preview_port=${pp} upstream=${want}"
}

if [ "${PREVIEW_INJECT_LIB:-}" != "1" ]; then
  case "${1:-}" in
    set-upstream)
      # SSH sessions lack PID1's environment; source at top level so the
      # saved `declare -x` lines stay global.
      if [ -z "${PREVIEW_PORT:-}" ] && [ -r "$ENV_FILE" ]; then
        # shellcheck disable=SC1090
        . "$ENV_FILE"
      fi
      preview_set_upstream "${2:-}"
      ;;
    *)
      preview_inject_main
      ;;
  esac
fi

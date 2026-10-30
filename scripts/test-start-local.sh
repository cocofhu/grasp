#!/usr/bin/env bash
# Exercise local teardown in a disposable checkout, without a Docker daemon.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
PIDS=()
cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  rm -rf "$TMP"
}
trap cleanup EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

mkdir -p "$TMP/repo/.devdata/local" "$TMP/bin"
cp "$ROOT/start.sh" "$TMP/repo/start.sh"
touch "$TMP/repo/.env"
# Restrict PATH so an installed host Docker cannot affect the missing-CLI case.
for tool in bash dirname rm; do
  ln -s "$(command -v "$tool")" "$TMP/bin/$tool"
done
export GRASP_SECRETS_KEY=test-local-teardown GRASP_DOCTOR_TOKEN=test-local-teardown GIT_COMMIT=test
export TEST_PID_FILE="$TMP/repo/.devdata/local/grasp-server.pid"
export TEST_DOCKER_LOG="$TMP/docker.log"

run_case() {
  local mode="$1" pid i
  (exec -a grasp-server sleep 60) &
  pid=$!
  PIDS+=("$pid")
  printf '%s\n' "$pid" >"$TEST_PID_FILE"
  : >"$TEST_DOCKER_LOG"
  if ! PATH="$TMP/bin" TEST_DOCKER_MODE="$mode" bash "$TMP/repo/start.sh" down >"$TMP/output" 2>&1; then
    cat "$TMP/output" >&2
    fail "down failed ($mode)"
  fi
  [[ ! -f "$TEST_PID_FILE" ]] || fail "pid file retained ($mode)"
  for ((i = 0; i < 50; i++)); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.02
  done
  if kill -0 "$pid" 2>/dev/null; then
    fail "local process still running ($mode)"
  fi
  wait "$pid" 2>/dev/null || true
  if [[ "$mode" == available ]]; then
    [[ "$(<"$TEST_DOCKER_LOG")" == $'compose.release.yaml\ndocker-compose.yml' ]] || fail "compose stacks were not stopped ($mode)"
  else
    [[ ! -s "$TEST_DOCKER_LOG" ]] || fail "compose down called without usable Docker ($mode)"
  fi
  echo "ok - local down ($mode)"
}

run_case missing-cli
cat >"$TMP/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
# Docker discovery must happen only after local teardown.
[[ ! -f "$TEST_PID_FILE" ]] || exit 99
case "$*" in
  info) [[ "$TEST_DOCKER_MODE" != daemon-unavailable ]] ;;
  "compose version") [[ "$TEST_DOCKER_MODE" != compose-unavailable ]] ;;
  "compose -f compose.release.yaml down --remove-orphans")
    printf '%s\n' compose.release.yaml >>"$TEST_DOCKER_LOG"
    ;;
  "compose -f docker-compose.yml down --remove-orphans")
    printf '%s\n' docker-compose.yml >>"$TEST_DOCKER_LOG"
    ;;
  *) exit 98 ;;
esac
MOCK
chmod +x "$TMP/bin/docker"
run_case daemon-unavailable
run_case compose-unavailable
run_case available

# Repeated teardown also succeeds after the PID file is gone.
PATH="$TMP/bin" TEST_DOCKER_MODE=daemon-unavailable bash "$TMP/repo/start.sh" down
echo "ok - repeated local down"

# A stale PID reused by an unrelated process must not be signalled.
sleep 60 &
unrelated=$!
PIDS+=("$unrelated")
printf '%s\n' "$unrelated" >"$TEST_PID_FILE"
PATH="$TMP/bin" TEST_DOCKER_MODE=daemon-unavailable bash "$TMP/repo/start.sh" --local down
[[ ! -f "$TEST_PID_FILE" ]] || fail "stale pid file retained"
kill -0 "$unrelated" 2>/dev/null || fail "unrelated process reusing the recorded PID was killed"
echo "ok - stale local pid ignored"

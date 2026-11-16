#!/usr/bin/env bash
# Local runner for the real-sandbox end-to-end tests.
#
# These are the GRASP_LIVE-gated tests in internal/runtime/: they launch a
# real approving-sandbox container via Docker, drive cursor-agent over ACP, and
# assert the declared `produces` file is harvested back. The default `go test`
# run stays credential-free on the mock provider; this script opts into the
# live path. It mirrors the CI `server:e2e` job (see .github/workflows/ci-server.yml) but talks
# to your local Docker daemon instead of a dind sidecar.
#
# Usage:
#   GRASP_CURSOR_API_KEY=crsr_xxx server/scripts/e2e.sh
#   GRASP_CURSOR_API_KEY=crsr_xxx SANDBOX_IMAGE=approving-sandbox:dev \
#     server/scripts/e2e.sh -run TestCursorLiveRunAgent
#
# Any extra args are passed straight through to `go test`.
set -euo pipefail

# Resolve the server module dir (this script lives in server/scripts/).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

fail() { echo "error: $*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || fail "go is not installed / not on PATH"
command -v docker >/dev/null 2>&1 || fail "docker is not installed / not on PATH"
docker info >/dev/null 2>&1 || fail "docker daemon is not reachable (is Docker running?)"

[ -n "${GRASP_CURSOR_API_KEY:-}" ] || fail "GRASP_CURSOR_API_KEY is required (a crsr_... Cursor API key)"

# The gateway at GRASP_SANDBOX_GATEWAY_URL owns the image (SBGW_DOCKER_IMAGE).
# SANDBOX_IMAGE only pre-pulls the image that gateway is configured with.
SANDBOX_IMAGE="${SANDBOX_IMAGE:-approving-sandbox:dev}"
export GRASP_LIVE=1

if ! docker image inspect "$SANDBOX_IMAGE" >/dev/null 2>&1; then
  echo "sandbox image '$SANDBOX_IMAGE' not present locally; attempting docker pull..."
  docker pull "$SANDBOX_IMAGE" || fail "could not find or pull sandbox image '$SANDBOX_IMAGE' (build it with: ./start.sh sandbox)"
fi

# Default test selection: the sandbox run-agent e2e. Callers can override the
# -run filter (and add -v, -timeout, etc.) via extra args.
if [ "$#" -eq 0 ]; then
  set -- -run TestCursorLiveRunAgent -v -count=1 -timeout 30m
fi

echo "running live e2e (gateway image=$SANDBOX_IMAGE) ..."
cd "$SERVER_DIR"
exec go test ./internal/runtime/ "$@"

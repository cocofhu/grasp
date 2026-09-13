#!/usr/bin/env bash
# Start Grasp from published GHCR images (default), or the local source
# stack for development.
#
# Usage:
#   ./start.sh            foreground (ensure Grasp+Gateway if missing + up)
#   ./start.sh -d          detached
#   ./start.sh logs        follow logs
#   ./start.sh down        stop and remove containers
#   ./start.sh restart     down + up -d
#   ./start.sh pull        refresh compose images + the sandbox runtime
#   ./start.sh dev         local source stack (build server/web/gateway)
#   ./start.sh dev -d      local source stack, detached
#   ./start.sh sandbox     build universal-sandbox:local (dev only)
#   ./start.sh gateway     rebuild local sandbox-gateway image (dev only)
#
# Requires Docker Compose on a Linux host (services use network_mode: host).
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
export HOST_REPO_DIR="$(pwd)"

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker not found" >&2
  exit 1
fi

if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  echo "error: docker compose / docker-compose not found" >&2
  exit 1
fi

if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "created .env from .env.example"
fi

set -a
# shellcheck disable=SC1091
source .env
set +a

# Defaults so a bare clone can start without editing .env.
: "${GRASP_PORT:=8080}"
: "${GRASP_GATEWAY_PORT:=8899}"
: "${GRASP_SANDBOX_GATEWAY_URL:=http://127.0.0.1:${GRASP_GATEWAY_PORT}}"
: "${GRASP_DEPLOYMENT_MODE:=local-demo}"
: "${GRASP_IMAGE:=ghcr.io/cocofhu/grasp:1.0.0}"
: "${SANDBOX_GATEWAY_IMAGE:=ghcr.io/cocofhu/sandbox-gateway:1.0.0}"
: "${SANDBOX_GATEWAY_API_KEY:=grasp-local-demo}"

# One published sandbox image (five CLIs inside; runtime AGENT_PROVIDER picks).
: "${SANDBOX_IMAGE:=ghcr.io/cocofhu/universal-sandbox:1.0.0}"
# Release compose must pin Grasp to GHCR; empty would fall through to
# universal-sandbox:local inside the container.
if [[ -z "${GRASP_SANDBOX_IMAGE:-}" ]]; then
  GRASP_SANDBOX_IMAGE="${SANDBOX_IMAGE}"
fi

# Demo account (admin / demo1234). Set outside the .env file so `$` in the
# bcrypt hash is not eaten by shell/compose env parsing.
if [[ -z "${GRASP_AUTH_USERS:-}" ]]; then
  GRASP_AUTH_USERS='[{"username":"admin","password_hash":"$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi","is_admin":true}]'
fi

if [[ -z "${GRASP_DOCTOR_TOKEN:-}" ]]; then
  GRASP_DOCTOR_TOKEN="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
fi

export GRASP_PORT GRASP_GATEWAY_PORT GRASP_SANDBOX_GATEWAY_URL
export GRASP_DEPLOYMENT_MODE GRASP_IMAGE SANDBOX_GATEWAY_IMAGE
export SANDBOX_IMAGE SANDBOX_GATEWAY_API_KEY GRASP_AUTH_USERS GRASP_DOCTOR_TOKEN
export GRASP_SANDBOX_IMAGE
# Grasp client uses the dedicated env name; keep it in sync with the gateway.
export GRASP_SANDBOX_GATEWAY_API_KEY="${GRASP_SANDBOX_GATEWAY_API_KEY:-$SANDBOX_GATEWAY_API_KEY}"
export SBGW_API_KEYS="${SBGW_API_KEYS:-$SANDBOX_GATEWAY_API_KEY}"

# Optional stamp for the source stack (`go run` + Dockerfile.dev ldflags).
# Unset / failed rev-parse → empty; overview badge stays hidden (allowed).
if [[ -z "${GIT_COMMIT:-}" ]] && command -v git >/dev/null 2>&1; then
  GIT_COMMIT="$(git -C "$HOST_REPO_DIR" rev-parse HEAD 2>/dev/null || true)"
fi
export GIT_COMMIT="${GIT_COMMIT:-}"

RELEASE_COMPOSE_FILE="${COMPOSE_FILE:-compose.release.yaml}"
DEV_COMPOSE_FILE="docker-compose.yml"

wait_for_url() {
  local url="$1"
  local label="$2"
  local -a probe=()
  if command -v curl >/dev/null 2>&1; then
    probe=(curl -fsS -o /dev/null "$url")
  elif command -v wget >/dev/null 2>&1; then
    probe=(wget -q -O /dev/null "$url")
  else
    echo "${label}: ${url} (no curl/wget; skip probe)"
    return 0
  fi
  echo -n "waiting ${label} ${url} "
  for _ in $(seq 1 60); do
    if "${probe[@]}" >/dev/null 2>&1; then
      echo "ok"
      return 0
    fi
    echo -n "."
    sleep 1
  done
  echo ""
  echo "warning: ${label} not ready in 60s; check: ./start.sh logs" >&2
  return 0
}

print_release_endpoints() {
  echo "—— UI/API  http://localhost:${GRASP_PORT}"
  echo "—— health  http://localhost:${GRASP_PORT}/api/health"
  echo "—— gateway http://127.0.0.1:${GRASP_GATEWAY_PORT}/healthz"
  echo "—— login   admin / demo1234  (local-demo)"
  echo "—— gateway token  ${SANDBOX_GATEWAY_API_KEY}"
  echo "—— images  ${GRASP_IMAGE}"
  echo "           ${SANDBOX_GATEWAY_IMAGE}"
  echo "—— sandbox ${GRASP_SANDBOX_IMAGE}"
}

# Sandbox runtime images are NOT compose services — compose pull never fetches them.
# Used only by `./start.sh pull`; default up/-d/restart leave the runtime to
# on-demand gateway pull.
ensure_sandbox_runtime_image() {
  local img="${GRASP_SANDBOX_IMAGE:-$SANDBOX_IMAGE}"
  [[ -n "$img" ]] || return 0
  echo "pulling sandbox runtime image ${img} (GHCR, several GB)..."
  docker pull "$img"
}

# Grasp + Gateway publish images: pull only when missing locally (g1.2).
# Does not refresh tags that are already present; use `./start.sh pull` for that.
ensure_compose_images_if_missing() {
  local images=("${GRASP_IMAGE}" "${SANDBOX_GATEWAY_IMAGE}")
  local -A seen=()
  local img
  for img in "${images[@]}"; do
    [[ -n "$img" ]] || continue
    [[ -n "${seen[$img]:-}" ]] && continue
    seen[$img]=1
    if docker image inspect "$img" >/dev/null 2>&1; then
      echo "compose image present: ${img}"
      continue
    fi
    echo "pulling missing compose image ${img}..."
    docker pull "$img"
  done
}

up_release() {
  local detach="${1:-}"
  mkdir -p .localdata/gateway .localdata/db .localdata/app-data
  # On-demand: only Grasp + Gateway when missing. Sandbox runtimes are
  # pulled later by the gateway on first sandbox create (plan g1.1 / g1.2).
  echo "ensuring Grasp + Gateway images (sandbox runtimes on demand)..."
  ensure_compose_images_if_missing
  if [[ "$detach" == "1" ]]; then
    "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" up -d
    wait_for_url "http://127.0.0.1:${GRASP_GATEWAY_PORT}/healthz" "gateway"
    wait_for_url "http://127.0.0.1:${GRASP_PORT}/api/health" "api"
    echo "started (GHCR)"
    print_release_endpoints
    echo "note: sandbox runtime pulls on first Agent use"
    echo "      warm it now: ./start.sh pull"
    echo "data: .localdata/{gateway,db,app-data} (bind mounts)"
    echo "wipe: ./start.sh down && rm -rf .localdata"
    echo "logs: ./start.sh logs   stop: ./start.sh down"
  else
    echo "starting (foreground) — UI http://localhost:${GRASP_PORT}"
    "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" up
  fi
}

ensure_dev_sandbox_image() {
  local sandbox_image="${GRASP_GATEWAY_SANDBOX_IMAGE:-universal-sandbox:local}"
  local gateway_dir="${SANDBOX_GATEWAY_DIR:-./sandbox-gateway}"
  if docker image inspect "$sandbox_image" >/dev/null 2>&1; then
    return 0
  fi
  if [[ ! -f "${gateway_dir}/sandbox/Dockerfile" ]]; then
    echo "error: missing ${gateway_dir}/sandbox/Dockerfile (needed to build ${sandbox_image})" >&2
    exit 1
  fi
  echo "building local sandbox image ${sandbox_image} (first run is slow)..."
  docker build --network=host \
    -t "$sandbox_image" \
    --build-arg AGENT_PROVIDERS="${AGENT_PROVIDERS:-cursor,claude_code,codebuddy,trae,opencode}" \
    -f "${gateway_dir}/sandbox/Dockerfile" \
    "${gateway_dir}/sandbox"
}

up_dev() {
  local detach="${1:-}"
  if [[ ! -f server/config.yaml ]]; then
    cp server/config.example.yaml server/config.yaml
    echo "created server/config.yaml from config.example.yaml"
  fi
  mkdir -p .devdata/db .devdata/sandbox-home
  export GRASP_SANDBOX_GATEWAY_URL="http://127.0.0.1:${GRASP_GATEWAY_PORT}"
  ensure_dev_sandbox_image
  if [[ "$detach" == "1" ]]; then
    "${COMPOSE[@]}" -f "$DEV_COMPOSE_FILE" up --build -d
    wait_for_url "http://127.0.0.1:${GRASP_GATEWAY_PORT}/healthz" "gateway"
    echo "started (dev/source)"
    echo "—— API http://localhost:${GRASP_PORT}/api/health  UI http://localhost:5173"
    echo "—— gateway http://127.0.0.1:${GRASP_GATEWAY_PORT}/healthz"
  else
    echo "starting dev stack (foreground) — UI http://localhost:5173"
    "${COMPOSE[@]}" -f "$DEV_COMPOSE_FILE" up --build
  fi
}

cmd="${1:-up}"
shift || true
case "$cmd" in
  up)
    up_release 0
    ;;
  -d|up-d|detach)
    up_release 1
    ;;
  pull)
    "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" pull
    ensure_sandbox_runtime_image
    ;;
  logs)
    if "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" ps -q 2>/dev/null | grep -q .; then
      "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" logs -f "$@"
    else
      "${COMPOSE[@]}" -f "$DEV_COMPOSE_FILE" logs -f "$@"
    fi
    ;;
  gw-logs)
    if "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" ps -q 2>/dev/null | grep -q .; then
      "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" logs -f gateway
    else
      "${COMPOSE[@]}" -f "$DEV_COMPOSE_FILE" logs -f gateway
    fi
    ;;
  down)
    "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" down --remove-orphans || true
    "${COMPOSE[@]}" -f "$DEV_COMPOSE_FILE" down --remove-orphans || true
    ;;
  restart)
    "${COMPOSE[@]}" -f "$RELEASE_COMPOSE_FILE" down --remove-orphans || true
    up_release 1
    ;;
  dev)
    sub="${1:-up}"
    case "$sub" in
      -d|up-d|detach) up_dev 1 ;;
      up|"") up_dev 0 ;;
      *)
        echo "usage: ./start.sh dev [-d]" >&2
        exit 1
        ;;
    esac
    ;;
  sandbox)
    gateway_dir="${SANDBOX_GATEWAY_DIR:-./sandbox-gateway}"
    sandbox_image="${GRASP_GATEWAY_SANDBOX_IMAGE:-universal-sandbox:local}"
    docker build --network=host \
      -t "$sandbox_image" \
      --build-arg AGENT_PROVIDERS="${AGENT_PROVIDERS:-cursor,claude_code,codebuddy,trae,opencode}" \
      -f "${gateway_dir}/sandbox/Dockerfile" \
      "${gateway_dir}/sandbox"
    echo "built ${sandbox_image}"
    ;;
  gateway)
    "${COMPOSE[@]}" -f "$DEV_COMPOSE_FILE" build gateway
    ;;
  build)
    echo "default start uses GHCR images (no local build)." >&2
    echo "for source builds: ./start.sh dev   or   ./start.sh sandbox" >&2
    exit 1
    ;;
  *)
    echo "unknown: $cmd" >&2
    echo "usage: ./start.sh [up|-d|pull|logs|gw-logs|down|restart|dev|sandbox|gateway]" >&2
    exit 1
    ;;
esac

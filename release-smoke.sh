#!/usr/bin/env sh
set -eu

COMPOSE_FILE="${COMPOSE_FILE:-compose.release.yaml}"
EVIDENCE_DIR="${EVIDENCE_DIR:-release-evidence}"
mkdir -p "$EVIDENCE_DIR"
LOG="$EVIDENCE_DIR/smoke.log"

require_digest() {
  name="$1"
  value="$(eval "printf '%s' \"\${$name:-}\"")"
  case "$value" in
    *@sha256:????????????????????????????????????????????????????????????????) ;;
    *)
      printf '%s must be an immutable @sha256 image reference\n' "$name" >&2
      exit 2
      ;;
  esac
}

require_digest GRASP_IMAGE
require_digest SANDBOX_GATEWAY_IMAGE
require_digest SANDBOX_IMAGE

# Smoke keeps digest-pinned global force (doctor demo uses a single sandbox image).
export GRASP_SANDBOX_IMAGE="${GRASP_SANDBOX_IMAGE:-$SANDBOX_IMAGE}"

# Internal, short-lived authentication for the loopback doctor control plane.
# It is generated automatically and deliberately omitted from smoke evidence.
GRASP_DOCTOR_TOKEN="${GRASP_DOCTOR_TOKEN:-$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')}"
export GRASP_DOCTOR_TOKEN
SANDBOX_GATEWAY_API_KEY="${SANDBOX_GATEWAY_API_KEY:-grasp-local-demo}"
export SANDBOX_GATEWAY_API_KEY
export GRASP_SANDBOX_GATEWAY_API_KEY="${GRASP_SANDBOX_GATEWAY_API_KEY:-$SANDBOX_GATEWAY_API_KEY}"
export SBGW_API_KEYS="${SBGW_API_KEYS:-$SANDBOX_GATEWAY_API_KEY}"

cleanup() {
  docker compose -f "$COMPOSE_FILE" down --volumes --remove-orphans >>"$LOG" 2>&1 || true
  rm -rf .localdata
}
trap cleanup EXIT INT TERM

{
  printf 'Grasp release smoke\n'
  printf 'started_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'kernel=%s\n' "$(uname -srmo)"
  docker version --format 'docker_client={{.Client.Version}} docker_server={{.Server.Version}}'
  docker compose version
  printf 'grasp_image=%s\n' "$GRASP_IMAGE"
  printf 'gateway_image=%s\n' "$SANDBOX_GATEWAY_IMAGE"
  printf 'sandbox_image=%s\n' "$SANDBOX_IMAGE"
} >"$LOG"

docker compose -f "$COMPOSE_FILE" config --quiet >>"$LOG" 2>&1
docker compose -f "$COMPOSE_FILE" pull >>"$LOG" 2>&1
docker compose -f "$COMPOSE_FILE" up -d --wait >>"$LOG" 2>&1
docker compose -f "$COMPOSE_FILE" exec -T grasp \
  /app/grasp doctor --run-demo --timeout 5m >>"$LOG" 2>&1
docker compose -f "$COMPOSE_FILE" ps >>"$LOG" 2>&1

printf 'completed_utc=%s\nresult=passed\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$LOG"
printf 'release smoke passed; evidence: %s\n' "$LOG"

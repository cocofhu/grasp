#!/usr/bin/env bash
# Docker-driver gateway e2e (create → inject → data-plane → reinstall → delete).
#
# Prerequisites:
#   - docker + docker compose
#   - a local sandbox image (default: universal-sandbox:e2e2, fallback: universal-sandbox:local)
#
# Usage (repo root):
#   ./scripts/e2e-docker.sh
#   SBGW_DOCKER_IMAGE=universal-sandbox:local ./scripts/e2e-docker.sh
#
# Nested DinD (pod → docker → sandbox) needs SKIP_INNER_DOCKER=1; this script
# always passes it so the e2e does not depend on inner dockerd.

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GW="${GW:-http://127.0.0.1:8080}"
IMAGE="${SBGW_DOCKER_IMAGE:-}"
if [ -z "$IMAGE" ]; then
  if docker image inspect universal-sandbox:e2e2 >/dev/null 2>&1; then
    IMAGE=universal-sandbox:e2e2
  elif docker image inspect universal-sandbox:local >/dev/null 2>&1; then
    IMAGE=universal-sandbox:local
  else
    echo "error: no sandbox image. Build one or set SBGW_DOCKER_IMAGE=..." >&2
    exit 1
  fi
fi

echo "==> sandbox image: $IMAGE"
# Compose config uses ...; tag local image to that ref so
# SBGW_DOCKER_IMAGE override is not required inside the gateway container.
REF=universal-sandbox:local
docker tag "$IMAGE" "$REF"

CLEANUP=()
cleanup() {
  set +e
  for id in "${CLEANUP[@]:-}"; do
    [ -n "$id" ] && curl -sS -m10 -X DELETE "$GW/api/v1/sandboxes/$id" >/dev/null || true
  done
  docker compose -f "$ROOT/docker-compose.yml" down -v >/dev/null 2>&1 || true
  pkill -f "e2e-inject-http.8899" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Free :8080 if a prior gateway is bound (host network).
if ss -tln 2>/dev/null | grep -q ':8080 '; then
  echo "==> killing process on :8080"
  fuser -k 8080/tcp >/dev/null 2>&1 || true
  sleep 1
fi

echo "==> docker compose up --build"
docker compose -f "$ROOT/docker-compose.yml" up --build -d
for i in $(seq 1 30); do
  if curl -sS -m3 "$GW/healthz" 2>/dev/null | grep -q '"status":"ok"'; then
    break
  fi
  sleep 1
done
curl -sS -m5 "$GW/healthz" | grep -q '"driver":"docker"'

# Inject bundle HTTP server (reachable via docker0).
INJECT_DIR="$(mktemp -d)"
mkdir -p "$INJECT_DIR/seed/rules"
printf '%s\n' '{"mcpServers":{"e2e":{"command":"echo","args":["ok"]}}}' >"$INJECT_DIR/seed/mcp.json"
echo "# e2e rule" >"$INJECT_DIR/seed/rules/e2e.md"
tar -C "$INJECT_DIR/seed" -czf "$INJECT_DIR/bundle.tgz" mcp.json rules
(
  cd "$INJECT_DIR"
  exec -a e2e-inject-http.8899 python3 -m http.server 8899 --bind 0.0.0.0
) >/tmp/e2e-inject-http.log 2>&1 &
sleep 1
GW_IP=172.17.0.1
BUNDLE_URL="http://${GW_IP}:8899/bundle.tgz"

echo "==> create sandbox (SKIP_INNER_DOCKER=1 + inject)"
RESP=$(curl -sS -m60 -X POST "$GW/api/v1/sandboxes" -H 'Content-Type: application/json' -d "{
  \"image\": \"$REF\",
  \"env\": {\"SKIP_INNER_DOCKER\": \"1\"},
  \"config\": {\"bundleUrl\": \"$BUNDLE_URL\", \"configRoot\": \"/root/.cursor\"},
  \"labels\": {\"e2e\": \"docker\"}
}")
ID=$(printf '%s' "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
CLEANUP+=("$ID")
echo "    id=$ID"

echo "==> wait running"
for i in $(seq 1 30); do
  ST=$(curl -sS -m10 "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  [ "$ST" = running ] && break
  [ "$ST" = error ] && { curl -sS "$GW/api/v1/sandboxes/$ID"; exit 1; }
  sleep 2
done
[ "$ST" = running ]

SESS=$(curl -sS -m10 "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['endpoints']['session'])")
IDE=$(curl -sS -m10 "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['endpoints']['ide'])")
echo "==> data plane session=$SESS ide=$IDE"
wait_http() {
  local url="$1" want="$2" # want: regex of acceptable codes
  local i code
  for i in $(seq 1 40); do
    code=$(curl -sS -m5 --http1.1 -o /dev/null -w "%{http_code}" "$url" 2>/dev/null || true)
    [ -z "$code" ] && code=000
    if echo "$code" | grep -Eq "$want"; then
      echo "    $url -> $code"
      return 0
    fi
    sleep 2
  done
  echo "timeout waiting $url (last=$code want=$want)" >&2
  return 1
}
wait_http "http://$SESS/" '^(200)$'
wait_http "http://$IDE/" '^(200|302)$'

echo "==> inject files (CAPA A8: content smoke, not only test -f)"
docker exec "sbx-$ID" bash -c 'test -f /root/.cursor/mcp.json && test -f /root/.cursor/rules/e2e.md'
docker exec "sbx-$ID" bash -c 'grep -q mcpServers /root/.cursor/mcp.json && grep -q e2e /root/.cursor/mcp.json'

echo "==> hosts/:port"
curl -sS -m10 -o /dev/null -w "%{http_code}" "$GW/api/v1/sandboxes/$ID/hosts/8765" | grep -q 200
curl -sS -m10 -o /dev/null -w "%{http_code}" "$GW/api/v1/sandboxes/$ID/hosts/12345" | grep -q 404

echo "==> reinstall (preserveData) must re-inject"
docker exec "sbx-$ID" bash -c 'touch /root/.cursor/MARKER'
curl -sS -m30 -X POST "$GW/api/v1/sandboxes/$ID/reinstall" \
  -H 'Content-Type: application/json' -d '{"preserveData":true}' | grep -q reinstall
for i in $(seq 1 30); do
  ST=$(curl -sS -m10 "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  [ "$ST" = running ] && break
  [ "$ST" = error ] && exit 1
  sleep 2
done
docker exec "sbx-$ID" bash -c '! test -f /root/.cursor/MARKER'
# CAPA A8: re-inject restores mcpServers content (not file-existence alone).
docker exec "sbx-$ID" bash -c 'test -f /root/.cursor/mcp.json && grep -q mcpServers /root/.cursor/mcp.json && grep -q e2e /root/.cursor/mcp.json'

echo "==> stop/start/delete"
curl -sS -m20 -X POST "$GW/api/v1/sandboxes/$ID/stop" | grep -q stopped
curl -sS -m20 -X POST "$GW/api/v1/sandboxes/$ID/start" >/dev/null
for i in $(seq 1 20); do
  ST=$(curl -sS -m10 "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  [ "$ST" = running ] && break
  sleep 2
done
curl -sS -m20 -X DELETE "$GW/api/v1/sandboxes/$ID" | grep -q destroyed
CLEANUP=()

echo "OK: docker-driver e2e passed"

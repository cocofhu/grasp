#!/usr/bin/env bash
# CI-oriented docker-driver e2e using the lightweight stub sandbox image.
# Gateway uses --network host so session readiness probes on bindIP 127.0.0.1 work
# (gateway and published sandbox ports share the Docker daemon host network).
# Under GitLab DinD the job reaches that host via the `docker` service IP.
# Usage (repo root):
#   ./scripts/e2e-ci.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

STUB_TAG="${E2E_STUB_IMAGE:-sandbox-e2e-stub:local}"
GW_TAG="${E2E_GATEWAY_IMAGE:-sandbox-gateway:local}"
GW_CFG_TAG="${E2E_GATEWAY_CFG_IMAGE:-sandbox-gateway-e2e-cfg:local}"

BRIDGE_GW="$(docker network inspect bridge -f '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true)"
BRIDGE_GW="${BRIDGE_GW:-172.17.0.1}"

if [ -n "${CI:-}" ] || [[ "${DOCKER_HOST:-}" == tcp://docker* ]]; then
  # Job container → DinD host network (where --network host containers listen).
  PUBLISH_HOST="$(getent hosts docker | awk '{print $1; exit}')"
  if [ -z "$PUBLISH_HOST" ]; then
    echo "FATAL: cannot resolve docker service host"
    exit 1
  fi
  # Publish sandbox ports on the DinD service IP (not 127.0.0.1) so the job
  # can curl them; gateway readiness still works because it shares host net.
  BIND_IP="$PUBLISH_HOST"
else
  PUBLISH_HOST="127.0.0.1"
  BIND_IP="127.0.0.1"
fi
GW="${GW:-http://${PUBLISH_HOST}:8080}"

CLEANUP_IDS=()
cleanup() {
  set +e
  for id in "${CLEANUP_IDS[@]:-}"; do
    [ -n "$id" ] && curl -sS -m10 -X DELETE "$GW/api/v1/sandboxes/$id" >/dev/null || true
  done
  docker rm -f e2e-gw e2e-inject >/dev/null 2>&1 || true
  docker ps -aq --filter name=sbx- | xargs -r docker rm -f >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Prefer a CN Docker Hub mirror inside DinD (auth.docker.io often TLS-times-out).
DOCKER_HUB_MIRROR="${DOCKER_HUB_MIRROR:-docker.m.daocloud.io}"
ALPINE_IMAGE="${ALPINE_IMAGE:-${DOCKER_HUB_MIRROR}/library/alpine:3.20}"

echo "==> build stub sandbox $STUB_TAG (base=$ALPINE_IMAGE)"
docker build --build-arg "ALPINE_IMAGE=$ALPINE_IMAGE" -t "$STUB_TAG" -f scripts/e2e-stub/Dockerfile scripts/e2e-stub/

E2E_BIN="${E2E_GATEWAY_BIN:-$ROOT/artifacts/sandbox-gateway}"
if ! docker image inspect "$GW_TAG" >/dev/null 2>&1; then
  if [ -x "$E2E_BIN" ]; then
    echo "==> build gateway $GW_TAG from prebuilt binary ($E2E_BIN) (base=$ALPINE_IMAGE)"
    docker build --build-arg "ALPINE_IMAGE=$ALPINE_IMAGE" -t "$GW_TAG" -f scripts/gateway-runtime.Dockerfile "$(dirname "$E2E_BIN")"
  else
    echo "==> build gateway $GW_TAG (full Dockerfile; set artifacts/sandbox-gateway to skip golang pull)"
    docker build -t "$GW_TAG" -f Dockerfile .
  fi
fi

CFGDIR="$(mktemp -d)"
cat >"$CFGDIR/config.yaml" <<EOF
server:
  listen: ":8080"
driver: docker
database:
  driver: sqlite
  path: /tmp/gateway-e2e.db
ports:
  session: 8765
  codeServer: 8744
  ssh: 0
  cdp: 0
  novnc: 0
  app: []
docker:
  image: ${STUB_TAG}
  bindIP: "${BIND_IP}"
  namePrefix: "sbx-"
  shmSize: "64m"
auth:
  apiKeys: []
EOF
cat >"$CFGDIR/Dockerfile" <<EOF
FROM ${GW_TAG}
COPY config.yaml /tmp/e2e-config.yaml
ENV SBGW_CONFIG=/tmp/e2e-config.yaml
EOF
echo "==> build gateway cfg image $GW_CFG_TAG"
docker build -t "$GW_CFG_TAG" "$CFGDIR" >/dev/null

echo "==> start gateway (host-network gw=${GW} bindIP=${BIND_IP})"
docker rm -f e2e-gw e2e-inject >/dev/null 2>&1 || true
docker ps -aq --filter name=sbx- | xargs -r docker rm -f >/dev/null 2>&1 || true
# Free :8080 on the daemon host if a prior run left something bound.
if [ -z "${CI:-}" ]; then
  fuser -k 8080/tcp >/dev/null 2>&1 || true
  sleep 1
fi

# Bake config into the image (DinD cannot bind-mount the job workspace).
docker run -d --name e2e-gw --network host \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e SBGW_DOCKER_IMAGE="$STUB_TAG" \
  "$GW_CFG_TAG" >/dev/null

for i in $(seq 1 40); do
  if curl -sS -m3 "$GW/healthz" 2>/dev/null | grep -q '"driver":"docker"'; then
    break
  fi
  sleep 1
done
if ! curl -sS -m5 "$GW/healthz" 2>/dev/null | grep -q docker; then
  echo "gateway did not become healthy; logs:"
  docker logs e2e-gw 2>&1 | tail -80 || true
  exit 1
fi

INJECT_DIR="$(mktemp -d)"
mkdir -p "$INJECT_DIR/seed/rules"
# CAPA A8: seed must contain mcpServers (not a hollow {"ok":true} shell).
echo '{"mcpServers":{"artifact-store":{"url":"http://example.invalid/mcp"}}}' >"$INJECT_DIR/seed/mcp.json"
echo rule >"$INJECT_DIR/seed/rules/r.md"
tar -C "$INJECT_DIR/seed" -czf "$INJECT_DIR/bundle.tgz" mcp.json rules

# Inject HTTP server on host network so sandboxes can fetch via docker bridge GW.
docker run -d --name e2e-inject --network host --entrypoint sleep \
  "$STUB_TAG" infinity >/dev/null
docker exec e2e-inject mkdir -p /www
docker cp "$INJECT_DIR/bundle.tgz" e2e-inject:/www/bundle.tgz
docker exec -d e2e-inject python3 -m http.server 8899 --bind 0.0.0.0 --directory /www
for i in $(seq 1 20); do
  code=$(curl -sS -m3 -o /dev/null -w "%{http_code}" "http://${PUBLISH_HOST}:8899/bundle.tgz" || true)
  [ "$code" = 200 ] && break
  sleep 1
done
[ "$code" = 200 ]

BUNDLE_URL="http://${BRIDGE_GW}:8899/bundle.tgz"
echo "==> create + inject ($BUNDLE_URL)"
RESP=$(curl -sS -m60 -X POST "$GW/api/v1/sandboxes" -H 'Content-Type: application/json' -d "{
  \"image\": \"$STUB_TAG\",
  \"config\": {\"bundleUrl\": \"$BUNDLE_URL\", \"configRoot\": \"/root/.cursor\"},
  \"labels\": {\"e2e\": \"ci\"}
}")
echo "    create response: $RESP"
ID=$(printf '%s' "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); assert 'id' in d, d; print(d['id'])")
CLEANUP_IDS+=("$ID")

for i in $(seq 1 40); do
  ST=$(curl -sS -m10 "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  [ "$ST" = running ] && break
  if [ "$ST" = error ]; then
    echo "sandbox entered error:"
    curl -sS "$GW/api/v1/sandboxes/$ID" || true
    docker logs "sbx-$ID" 2>&1 | tail -40 || true
    exit 1
  fi
  sleep 1
done
[ "$ST" = running ]

SESS=$(curl -sS "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['endpoints']['session'])")
SESS_URL="http://${SESS}/"
for i in $(seq 1 20); do
  code=$(curl -sS -m5 -o /dev/null -w "%{http_code}" "$SESS_URL" || true)
  [ "$code" = 200 ] && break
  sleep 1
done
if [ "$code" != 200 ]; then
  echo "session HTTP check failed: url=$SESS_URL last_code=$code"
  exit 1
fi

# CAPA A8: MCP health = file exists AND content smoke (mcpServers + artifact-store).
# Do not treat `test -f mcp.json` alone as healthy.
docker exec "sbx-$ID" bash -c 'test -f /root/.cursor/mcp.json && grep -q mcpServers /root/.cursor/mcp.json && grep -q artifact-store /root/.cursor/mcp.json'
docker exec "sbx-$ID" test -f /root/.cursor/rules/r.md

curl -sS -o /dev/null -w "%{http_code}" "$GW/api/v1/sandboxes/$ID/hosts/8765" | grep -q 200
curl -sS -o /dev/null -w "%{http_code}" "$GW/api/v1/sandboxes/$ID/hosts/12345" | grep -q 404

echo "==> reinstall re-inject"
docker exec "sbx-$ID" touch /root/.cursor/MARKER
curl -sS -m30 -X POST "$GW/api/v1/sandboxes/$ID/reinstall" \
  -H 'Content-Type: application/json' -d '{"preserveData":true}' >/dev/null
for i in $(seq 1 40); do
  ST=$(curl -sS "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  [ "$ST" = running ] && break
  sleep 1
done
docker exec "sbx-$ID" sh -c '! test -f /root/.cursor/MARKER'
# CAPA A8: re-inject must also restore mcpServers content, not merely the file.
docker exec "sbx-$ID" bash -c 'test -f /root/.cursor/mcp.json && grep -q mcpServers /root/.cursor/mcp.json && grep -q artifact-store /root/.cursor/mcp.json'

curl -sS -X POST "$GW/api/v1/sandboxes/$ID/stop" | grep -q stopped
curl -sS -X POST "$GW/api/v1/sandboxes/$ID/start" >/dev/null
for i in $(seq 1 30); do
  ST=$(curl -sS "$GW/api/v1/sandboxes/$ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
  [ "$ST" = running ] && break
  sleep 1
done
curl -sS -X DELETE "$GW/api/v1/sandboxes/$ID" | grep -q destroyed
CLEANUP_IDS=()

echo "OK: CI e2e passed"

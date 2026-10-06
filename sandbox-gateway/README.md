# sandbox-gateway (vendored)

Control plane for Grasp sandboxes. This tree lives inside the Grasp
repository so a single clone can run the full stack with Docker Compose.

- Gateway API: `gateway/`
- Universal sandbox image: `sandbox/`
- Local config: `deploy/config/config.local.yaml`

Public data-plane ports: session / ide / ssh / app. CDP `:9222` and noVNC `:6080`
stay on the container or ClusterIP network (no host/LB publish). Users reach
noVNC only through Grasp VNC WebSockets. See `SECURITY.md` and `GATEWAY.md`
in the Grasp repo root.

## Build

```bash
# Gateway control plane
docker build -t sandbox-gateway:local -f Dockerfile .

# Default sandbox image used by local compose (six CLIs, including Codex which uses a login file rather than an API key; runtime AGENT_PROVIDER)
docker build -t universal-sandbox:local \
  -f sandbox/Dockerfile sandbox
```

## Standalone (optional)

```bash
docker compose up --build -d   # from this directory; listens on :8080 by default
curl -s localhost:8080/healthz
```

From the Grasp repo root, prefer `./start.sh` or root `docker compose up --build`,
which wires gateway on `:8899` next to the Grasp server.

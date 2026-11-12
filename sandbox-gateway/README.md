# sandbox-gateway (vendored)

Control plane and optional authenticated TCP transport for Grasp sandboxes. This tree lives inside the Grasp
repository so a single clone can run the full stack with Docker Compose.

- Gateway API: `gateway/`
- Universal sandbox image: `sandbox/`
- Local config: `deploy/config/config.local.yaml`

Configuration keys, defaults, precedence and E2B setup are collected in
[Grasp and sandbox configuration (中文)](../CONFIGURATION.md). This distinguishes
gateway resources from Grasp's request overrides and from sandbox-internal
environment variables. The complete runtime variable table is in
[sandbox/README.md](sandbox/README.md#环境变量).

Public data-plane ports: session / ide / ssh / app. CDP `:9222` and noVNC `:6080`
stay on the container or ClusterIP network (no host/LB publish). Users reach
noVNC only through Grasp VNC WebSockets. See `SECURITY.md` and `GATEWAY.md`
in the Grasp repo root.

For remote Docker, Kubernetes or E2B gateways, Grasp can opt into
`sandbox.gateway_transport: gateway` without changing browser/Agent protocols.
See [named connections, profiles and migration](../GATEWAY.md#gateway-transport-and-multiple-deployments).

## Deployment guides

- [Docker](deploy/docker/README.md): production Compose, callbacks, authentication and backup.
- [Kubernetes](deploy/k8s/README.md): full single-replica manifests, RBAC, storage and ingress.
- [E2B](deploy/e2b/README.md): provider setup, private-network access and runtime delivery.

The sandbox image requires a runtime bundle supplied by Grasp. Starting the
gateway alone does not make a bare sandbox create request runnable.

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

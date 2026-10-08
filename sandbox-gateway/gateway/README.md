# Sandbox Gateway

A thin control-plane service that manages the lifecycle of sandboxes (instances
of the Phase 1 universal image in [`../sandbox`](../sandbox)) and returns the
client-reachable address for each exposed port.

The gateway does **not** proxy data-plane traffic. Public ports are direct
client-to-sandbox connections; CDP/noVNC stay internal:

- session / agent bridge (WSP/1): `8765` — **public**, session auth
- code-server (web IDE): `8744` — **public**, IDE password
- SSH (shell, exec, file transfer): `22` — **public**, `ROOT_PASSWORD` / `SSH_KEY`
- Chromium CDP: `9222` — **internal only** (container / ClusterIP)
- noVNC preview: `6080` — **internal only** (container / ClusterIP)

Users reach noVNC via Grasp `/sandbox-vnc/:id/ws`, one desktop per sandbox
(Session when Auth is on). Grasp
outside the cluster or Docker network cannot dial CDP/noVNC.

There is no gateway `exec`/`files`/`terminal` API: run commands and move files
over SSH, and open the IDE/session against the returned **public** endpoints.

## Drivers (one per deployment)

A gateway instance runs exactly one driver, selected by `driver` in the config:

- `docker` — local testing. Publishes **Public** ports only (`session`/`ide`/`ssh`/`app`)
  on a host IP (`bindIP:ephemeral`). CDP/noVNC are **not** `-p` mapped; Endpoints
  backfill them from the container IP (`Networks[name].IPAddress` when a custom
  network is set). Already-running `-p` mappings are not rewritten (TTL/Reinstall).
- `kubernetes` — production. ClusterIP Service includes Public+Internal (Listen).
  LoadBalancer Service publishes **Public** only. Endpoints merge LB IP for public
  ports and ClusterIP DNS for `cdp`/`novnc`. `ensure*` updates existing Service
  `Spec.Ports` on AlreadyExists. Driver Options carry Public/Internal so
  `ReconcileOnStartup` / `Start` / `Get` call `ConvergePublishSurface` and heal
  inventory `*-lb` after upgrade (not only Create/Reinstall). Until that
  reconcile finishes, a pre-existing LB may still expose 9222/6080.

Docker and Kubernetes are deployed separately and never run together in one
instance.

```mermaid
flowchart TB
  client["client / remote-dev / code-flow / browser"]
  client -->|"control plane REST /api/v1 (API Key)"| gw["gateway (thin control plane)"]
  gw -->|"docker run / kubectl (client-go)"| drv["one driver: docker OR kubernetes"]
  drv --> sbx["sandbox (Docker host IP / K8s LB IP)"]
  gw -. "returns per-port direct addresses" .-> client
  client ==>|"public data plane: 8765/ws · 8744 · 22 · app"| sbx
  grasp["Grasp in-cluster"] -.->|"internal dial: 9222 CDP · 6080 noVNC"| sbx
```

## Layout

```
gateway/
├── cmd/server/main.go          # config -> DB -> pick driver -> router -> serve + reconcile
├── config.example.yaml
├── internal/
│   ├── config/                 # YAML + SBGW_* overrides
│   ├── database/               # SQLite (glebarez) + AutoMigrate
│   ├── models/                 # Sandbox model (endpoints/env/labels as JSON columns)
│   ├── store/                  # GORM-backed metadata store
│   ├── driver/                 # Driver interface (lifecycle + Endpoints)
│   │   ├── docker/             # docker CLI driver
│   │   └── kubernetes/         # client-go driver + MetalLB LoadBalancer
│   ├── service/                # SandboxService: orchestrate driver+store, async finalize, reconcile
│   └── api/                    # Gin handlers + router + API-key middleware
└── docs/API.md
```

## Run (local, Docker driver)

```bash
cp config.example.yaml config.yaml
# edit config.yaml: set image.ref to your built universal-sandbox image
go run ./cmd/server -config config.yaml
```

Then:

```bash
# create
curl -s -XPOST localhost:8080/api/v1/sandboxes \
  -H 'Content-Type: application/json' \
  -d '{"env":{"AGENT_PROVIDER":"cursor"}}'

# poll until running, read endpoints
curl -s localhost:8080/api/v1/sandboxes/<id>

# connect directly (examples)
#   IDE:     open http://<ide-endpoint>
#   session: ws://<session-endpoint>/ws
#   shell:   ssh root@<ssh-host> -p <ssh-port>
```

Set `auth.apiKeys` (or `SBGW_API_KEYS=k1,k2`) to require
`Authorization: Bearer <key>` on `/api/v1/*`.

## Configuration

See [`config.example.yaml`](config.example.yaml). Every value can be overridden
by `SBGW_*` environment variables (env wins), e.g. `SBGW_DRIVER`,
`SBGW_LISTEN`, `SBGW_IMAGE`, `SBGW_API_KEYS`, `SBGW_DOCKER_BIND_IP`,
`SBGW_K8S_KUBECONFIG`, `SBGW_K8S_NAMESPACE`, `SBGW_K8S_ENABLE_LB`.

## API

See [`docs/API.md`](docs/API.md). The data-plane contract for the session port
is WSP/1, documented in [`../sandbox/docs/PROTOCOL.md`](../sandbox/docs/PROTOCOL.md).

# Sandbox Gateway API

The gateway is a thin control plane. It creates, exposes, and destroys sandboxes
(instances of the Phase 1 universal image) and returns addresses for each port.

**Public data-plane** (may be published to host / LoadBalancer): session (`8765`),
code-server (`8744`), SSH (`22`), and extra app ports. Clients connect directly;
each service has its own auth (session/IDE password, `ROOT_PASSWORD` / `SSH_KEY`).

**Internal-only** (container network / ClusterIP, **not** published to host or
external LB): Chromium CDP (`9222`) and noVNC/websockify (`6080`). These have
**no application-layer auth**. Grasp dials them in-cluster for Pick/navigate
and VNC WS proxy. Users must **not** reach them directly — use Grasp
`/sandbox-vnc/:sandboxId/ws` and `/preview-vnc/:runId/:nodeId/:port/ws`
(Session required when platform Auth is injected). Grasp running outside
the cluster or Docker network is not supported for CDP/VNC.

Docker `-p` on already-running containers is not rewritten automatically; rely
on TTL or Reinstall. Kubernetes inventory `*-lb` Services that still publish
9222/6080 are converged on gateway startup reconcile (`ReconcileOnStartup`),
`Start`, or `Reinstall` — until then old bookmarks/scans may still succeed.
After converge, internal endpoints are ClusterIP DNS, not the LB IP. Old
`host:9222` / `host:6080` bookmarks becoming unreachable is an expected
breaking change.

- Base path: `/api/v1`
- Auth: `Authorization: Bearer <apiKey>` (when `auth.apiKeys` is configured)
- Data-plane contract for `session` (`8765`) is WSP/1, documented in
  [`../../sandbox/docs/PROTOCOL.md`](../../sandbox/docs/PROTOCOL.md).

A gateway instance runs one driver: `docker` (local testing) or `kubernetes`
(production, MetalLB LoadBalancer). The API surface is identical for both; only
the returned endpoint addresses differ (Docker host IP vs. LB IP).

## Health

```
GET /healthz  ->  200 {"status":"ok","driver":"docker"}
```

No auth. Useful for readiness/liveness checks.

## Create a sandbox

```
POST /api/v1/sandboxes
Content-Type: application/json
Authorization: Bearer <key>
```

Body (all fields optional):

```json
{
  "image": "universal-sandbox:local",
  "provider": "gemini",
  "env": {
    "AGENT_PROVIDER": "cursor",
    "GIT_REPOS": "app|https://github.com/acme/app|main",
    "ROOT_PASSWORD": "toor",
    "ACP_BRIDGE_PASSWORD": "s3cret"
  },
  "labels": {"owner": "team-a"},
  "workspaceDir": "/root/workspace",
  "ports": [3000, 5173],
  "mounts": ["/host/cache:/root/.cache:rw"],
  "resources": {
    "cpuCores": 2,
    "memoryMB": 4096,
    "diskGi": 160
  },
  "config": {
    "configRoot": "/root/.cursor",
    "hostPath": "/host/agent-config",
    "bundleUrl": "https://.../config.tar.gz",
    "headers": "Authorization: Bearer <token>"
  }
}
```

- `provider` selects the agent CLI (e.g. `cursor`, `claude_code`, `codebuddy`).
  The published image is one `universal-sandbox`; the gateway injects
  `AGENT_PROVIDER` when not already set.
- `env` is the injection channel to the image (see the sandbox README for the
  full variable reference: `WORKSPACE_DIR`, `GIT_REPOS`, `AGENT_PROVIDER`,
  `VNC_PREVIEW`, `BROWSER_MCP`, `ROOT_PASSWORD`, `SSH_KEY`, etc.).
- `ports` adds application ports on top of the image defaults.
- `resources` sets per-sandbox limits (same knobs as remote-dev UI):
  - `cpuCores` — CPU limit in cores (maps to k8s `limits.cpu` / docker `--cpus`)
  - `memoryMB` — memory limit in MiB (k8s `limits.memory` / docker `--memory`)
  - `diskGi` — data PVC size in GiB (kubernetes only; docker ignores)
  - omit or `0` → gateway defaults from `kubernetes.*` in config
  - values above `maxCPUCores` / `maxMemoryMB` / `maxDataDiskGi` → `400`
- `config` seeds rules/skills/mcp before services start. Use `hostPath` for the
  Docker driver (same-host bind-mount) or `bundleUrl` (+ optional `headers`)
  which is translated to the image's `SANDBOX_INJECT` contract.
- `mounts` is docker-only.

Response `202 Accepted` as soon as the control-plane record is persisted.
Driver provisioning (k8s Namespace/Secret/PVC/Deployment/LB, or `docker run`)
and session readiness finalize run in the background — poll `GET /sandboxes/:id`
until `status` is `running` or `error`. Do not treat a slow PVC/LB as an HTTP
timeout on this POST.

`config.hostPath` is **docker-only** (same-host bind-mount). Against a remote
kubernetes gateway it is ignored; use `config.bundleUrl` (+ optional `headers`)
so the image pulls the seed archive via `SANDBOX_INJECT`.

Response body (initial `status` is usually `creating`):

```json
{
  "id": "a1b2c3d4e5f6",
  "name": "sbx-a1b2c3d4e5f6",
  "status": "creating",
  "image": "universal-sandbox:local",
  "resources": {"cpuCores": 2, "memoryMB": 4096, "diskGi": 160},
  "endpoints": {}
}
```

The gateway backfills `endpoints` and flips `status` to `running` once the
sandbox is reachable (LB IP assigned where applicable, session port accepting
connections). Poll `GET /sandboxes/:id` until `status == "running"`.

## List sandboxes

```
GET /api/v1/sandboxes
GET /api/v1/sandboxes?label=owner:team-a
GET /api/v1/sandboxes?label=owner:team-a&label=env:prod
```

Returns `{"sandboxes":[ {sandbox}, ... ]}` newest first.

- Optional repeated `label=key:value` filters; all must match (AND) against the
  sandbox `labels` map written at create time.
- Value may contain `:` (only the first `:` separates key from value).
- Invalid `label` (missing `:` or empty key) → `400`.

## Get a sandbox

```
GET /api/v1/sandboxes/:id
```

```json
{
  "id": "a1b2c3d4e5f6",
  "name": "sbx-a1b2c3d4e5f6",
  "status": "running",
  "image": "universal-sandbox:local",
  "resources": {"cpuCores": 2, "memoryMB": 4096, "diskGi": 160},
  "endpoints": {
    "session": "10.0.0.21:8765",
    "ide": "10.0.0.21:8744",
    "ssh": "10.0.0.21:22",
    "cdp": "10.88.0.12:9222",
    "novnc": "10.88.0.12:6080",
    "8765": "10.0.0.21:8765",
    "8744": "10.0.0.21:8744",
    "22": "10.0.0.21:22"
  }
}
```

`endpoints` carries friendly names and raw port keys. `session`/`ide`/`ssh`
(and app ports) are **public** host or LB addresses. `cdp`/`novnc` are
**internal** container IP or ClusterIP DNS (`<svc>.<ns>.svc.cluster.local`)
for in-cluster Grasp only — not user-facing, not an external LB IP.

Grasp's user `GetView` whitelist returns only `session`/`ide`/`ssh`.

## Lifecycle

```
POST   /api/v1/sandboxes/:id/start      # docker start / k8s scale=1
POST   /api/v1/sandboxes/:id/stop       # docker stop  / k8s scale=0 (retained)
POST   /api/v1/sandboxes/:id/reinstall  # rebuild Pod; optional PVC wipe
DELETE /api/v1/sandboxes/:id            # remove sandbox + record
GET    /api/v1/sandboxes/:id/status     # {"status":"running|stopped|not_found|..."}
```

### Reinstall

```
POST /api/v1/sandboxes/:id/reinstall
Content-Type: application/json
Authorization: Bearer <key>

{"preserveData": true}
```

Aligned with remote-dev「重装环境」:

| `preserveData` | 行为 |
|----------------|------|
| `true` | 不删 PVC（k8s）/ 匿名卷（docker），只重建容器；工作区与缓存保留 |
| `false` / 省略 | 删除数据卷后重建（工作区、docker 层等清空） |

Host 挂载的共享配置（如 `config.hostPath` → `.cursor`）不会被本接口删除。
响应 `202 Accepted`，随后异步探测就绪（同 create），轮询 `GET /sandboxes/:id` 直至 `running`。

## Single-port lookup

```
GET /api/v1/sandboxes/:id/hosts/:port  ->  {"port":8744,"address":"10.0.0.21:8744"}
```

## Status values

| status     | meaning                                                    |
|------------|------------------------------------------------------------|
| `creating` | record persisted, resource provisioning / not yet ready    |
| `running`  | ready; `endpoints` populated                               |
| `stopped`  | stopped but retained (restartable via `start`)             |
| `error`    | provisioning or reconcile failed (see `error` field)       |

## Container logs (read-only)

```
GET /api/v1/sandboxes/:id/logs
GET /api/v1/sandboxes/:id/logs?tail=5000
```

Returns the sandbox PID1 combined stdout/stderr as a synchronous JSON body
(non-follow). Used for infrastructure / boot troubleshooting — not a substitute
for the agent execution event log.

```json
{"content": "[boot] sandbox container started\n…"}
```

| Query | Default | Notes |
|-------|---------|-------|
| `tail` | `5000` | Lines from the end of the log stream |

- **Docker driver**: `docker logs --tail` (combined stdout+stderr).
- **Kubernetes driver**: client-go `Pods.GetLogs` on the sandbox pod's
  `sandbox` container (combined stdout+stderr, non-follow). Pods are located by
  labels `sandbox-gateway.io/id` + `app.kubernetes.io/managed-by=sandbox-gateway`;
  when multiple pods match, prefer `Running`, else the newest by creation time.
- Missing sandbox (store / no matching workload) → `404` or driver not-found
  mapped by handlers. Driver / API / CLI failure → `500` with `error`.
- Drivers that do not implement Logs still return `501`
  (`sandbox logs not supported by this driver`).
- **Required RBAC (shipped)**: the gateway ServiceAccount needs `get` on
  `pods/log` in the sandbox namespace. Incremental manifests are provided at
  `sandbox-gateway/deploy/k8s/` (`role-pods-log.yaml` +
  `rolebinding-pods-log.yaml`); apply them alongside existing Roles (do not
  replace a full production Role). Missing permission surfaces as a non-2xx
  error (not empty success).

## Last container exit (read-only)

```
GET /api/v1/sandboxes/:id/exit
```

Why the sandbox's main container last stopped, so callers can tell an OOM kill
from a network drop. `exit` is `null` when it has never exited (or the driver
cannot tell).

```json
{"exit": {"reason": "OOMKilled", "exitCode": 137, "oomKilled": true,
          "restarts": 1, "memoryMB": 8192, "at": "2026-10-07T22:31:04Z"}}
```

- `reason`: `OOMKilled` | `Error` | `Evicted` | `Completed` | `Exited` (kubelet's
  reason verbatim on Kubernetes).
- **Docker driver**: `docker inspect` `State.OOMKilled` / `ExitCode` /
  `FinishedAt`, `RestartCount`, `HostConfig.Memory`.
- **Kubernetes driver**: latest of the `sandbox` container's `state.terminated`
  / `lastState.terminated` across the sandbox's pods, or an evicted pod. Needs
  `list` on `pods` (already required by logs).

## What the gateway does NOT do

- No `exec` / file / terminal endpoints. Run commands and move files by
  connecting directly to the sandbox **SSH (`22`)** or the WSP/1 session.
- No reverse proxy for code-server, session, SSH, or app ports. Clients connect
  to the returned **public** endpoint addresses directly.
- CDP / noVNC are not an external data-plane. Users go through Grasp VNC
  WebSocket proxies; the gateway does not terminate those WS paths.
- No streaming / follow logs (`?follow=1` / SSE). Clients re-fetch on demand.
- No previous-container / multi-container log fan-out (single `sandbox` container).
- code-flow's former `/api/changes` is gone from the image; compute "changes"
  by running `git` over a direct SSH/exec connection.

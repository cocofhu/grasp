# sandbox-gateway contract

Grasp vendors [sandbox-gateway](sandbox-gateway/) in this repository. The
control plane creates and destroys sandboxes; the data plane uses SSH endpoints
returned by the gateway.

Source layout:

- `sandbox-gateway/gateway/` — Go control plane
- `sandbox-gateway/sandbox/` — universal sandbox image (environment only) and
  the source of the sandbox runtime bundle (scripts, `cmd/`, `internal/`, `web/`)
- `scripts/build-sandbox-runtime.sh` — builds the runtime bundle the Grasp
  server ships and serves to sandboxes
- `sandbox-gateway/deploy/config/config.local.yaml` — local compose config

## Local stack

From the repo root (Linux host with Docker Compose):

```bash
./start.sh -d          # default: published GHCR images (compose.release.yaml)
./start.sh dev -d      # source/HMR stack (docker-compose.yml)
```

Two paths, different UI ports:

| Mode | Command | Gateway | API | UI |
| --- | --- | --- | --- | --- |
| Release (default) | `./start.sh -d` | `:8899` | `:8080` | `:8080` (served with the API) |
| Dev / source | `./start.sh dev -d` | `:8899` | `:8080` | `:5173` (Vite) |

Release mode ensures Grasp + Gateway images when missing; the sandbox runtime
pulls on first create (`status=pulling`). Use `./start.sh pull` to warm it.
Dev mode may build `universal-sandbox:local` from `sandbox-gateway/sandbox`
on first run (slow).

## Minimum compatible API

| Capability | Contract |
| --- | --- |
| Health | `GET /healthz` returns 2xx |
| Create | `POST /api/v1/sandboxes` accepts image, env, labels, ports, resources, config.bundleUrl; response `202`, status `creating` then optionally `pulling` while the image downloads, then `creating` again during `docker run` / finalize |
| Get | `GET /api/v1/sandboxes/{id}` returns status and endpoints. Gateway still includes internal `cdp`/`novnc` (container/ClusterIP) for in-cluster Grasp. Grasp user `GetView` only returns `session`/`ide`/`ssh`. |
| List | `GET /api/v1/sandboxes?label=key:value` (AND) |
| Delete | `DELETE /api/v1/sandboxes/{id}` returns 2xx |
| Logs | `GET /api/v1/sandboxes/{id}/logs?tail=` returns `{content}` (PID1 stdout/stderr, non-follow). Docker (`docker logs --tail`) and kubernetes (pod `sandbox` container via client-go GetLogs) both supported. Cluster RBAC must allow `get` on `pods/log` in the sandbox namespace; the incremental Role+RoleBinding is shipped in `sandbox-gateway/deploy/k8s/` (apply alongside existing Roles — do not replace a full production Role). Drivers that still omit Logs → `501` |
| Ready | status `running` with a `session` endpoint |
| Images | one image `universal-sandbox` (six CLIs, including Codex, which uses a login file rather than an API key); runtime `AGENT_PROVIDER` selects the live CLI |
| Runtime bundle | the image holds only the environment plus `/grasp-bootstrap.sh` (entrypoint). Grasp passes `GRASP_RUNTIME_URL` (Bearer from `SANDBOX_INJECT_HEADERS`) in the create env; the bootstrap installs that bundle and runs its `startup.sh`. Without it the container exits 1. Running sandboxes are updated over SSH (`grasp-bootstrap.sh install-stdin` + `services.sh restart`). Exit 4 = the bundle needs a newer image (`/etc/grasp-image-level` < MANIFEST `min_image`). Deploy the Grasp server before a new sandbox image. |
| Data plane | SSH / session / ide may connect directly (each has its own auth). CDP `:9222` and noVNC `:6080` are **not** external data-plane: they stay on the container/cluster network. Users use `/sandbox-vnc/:id/ws`, one desktop per sandbox (Session when Auth is on; Session validity only, no sandbox/run ownership check). Grasp outside the cluster/Docker net cannot dial CDP/noVNC. Docker already-running `-p` needs TTL/Reinstall. K8s inventory `*-lb` is healed on gateway startup reconcile / Start / Reinstall; until then 9222/6080 may still be on the LB. |
| Auth | Bearer token: gateway `SBGW_API_KEYS` / client `GRASP_SANDBOX_GATEWAY_API_KEY` (compose default `grasp-local-demo` via `SANDBOX_GATEWAY_API_KEY`) |

`grasp doctor --run-demo` verifies health, create, ready, and cleanup after failure.

## Published images

Release tags may also publish digest-pinned images to `ghcr.io/cocofhu/...`.
`compose.release.yaml` still requires explicit immutable references for the
clean-Linux smoke path.

---
title: Gateway
description: sandbox-gateway contract summary; full details in GATEWAY.md.
---

Grasp schedules the generic sandbox image through the vendored **sandbox-gateway** control plane. The Web UI talks to the Grasp API; Agent nodes execute in containers via the gateway.

## Direct endpoints vs platform proxy

Legacy direct mode remains the default. With `sandbox.gateway_transport: gateway`, Docker, Kubernetes and E2B SSH, chat, IDE and browser connections can use the authenticated gateway, so Grasp can run outside the sandbox network. Browser entry points stay unchanged. This requires a gateway API key and an isolated preview origin. The default named gateway affects new sandboxes only; existing sandboxes retain their connection. See the full configuration and migration guide below.

- **Direct (already authenticated)**: `session` (session password), `ide` (IDE password), `ssh` (`ROOT_PASSWORD` or `SSH_KEY`).
- **Not direct**: CDP `:9222` and noVNC `:6080` have no app-layer auth, are not published to the host/LB, and are not shown or copied on the sandbox detail page.
- **User path**: platform proxy only — `/sandbox-vnc/:sandboxId/ws`, one desktop per sandbox showing the browser the Agent drives (Session required when platform Auth is enabled; validity only, no sandbox/run ownership check). “Open preview” opens sandbox console noVNC; it does not dial websockify.
- **Inventory window**: Docker already-running `-p` needs TTL/Reinstall. K8s inventory `*-lb` may still expose `:9222` / `:6080` until gateway startup reconcile / Start / Reinstall.

## Environment and file retention

An expired workflow debugging window stops compute while retaining the environment identity and workspace. Startup reconciliation and gateway orphan scans preserve Solo/workflow resources whose records are missing, stopped or temporarily unavailable. Operators restore control-plane records or reconcile untracked resources explicitly; physical release remains a separate deletion action.

Kubernetes stop waits for the controller to observe scaling down and for live Pods to exit. The PVC is retained and services cold-start on resume; process memory is not saved. Stop is not reported as complete while it is waiting. Observation failures or a subsequent start superseding the stop return an error so operators can check the current state and retry.

## Full documentation

- [GATEWAY.md](https://github.com/cocofhu/approving/blob/main/GATEWAY.md)

## Health checks (default local stack)

- Gateway: http://localhost:8899/healthz
- Grasp API: http://localhost:8080/api/health

## Source locations

- Control plane: `sandbox-gateway/gateway/`
- Sandbox image and scripts: `sandbox-gateway/sandbox/`, `sandbox-gateway/scripts/`

## Related

- [Configuration](../configuration/)
- [Core concepts](../../guide/concepts/)

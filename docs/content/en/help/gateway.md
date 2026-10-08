---
title: Gateway
description: sandbox-gateway contract summary; full details in GATEWAY.md.
---

Grasp schedules the generic sandbox image through the vendored **sandbox-gateway** control plane. The Web UI talks to the Grasp API; Agent nodes execute in containers via the gateway.

## Direct endpoints vs platform proxy

- **Direct (already authenticated)**: `session` (session password), `ide` (IDE password), `ssh` (`ROOT_PASSWORD` or `SSH_KEY`).
- **Not direct**: CDP `:9222` and noVNC `:6080` have no app-layer auth, are not published to the host/LB, and are not shown or copied on the sandbox detail page.
- **User path**: platform proxy only — `/sandbox-vnc/:sandboxId/ws`, one desktop per sandbox showing the browser the Agent drives (Session required when platform Auth is enabled; validity only, no sandbox/run ownership check). “Open preview” opens sandbox console noVNC; it does not dial websockify.
- **Inventory window**: Docker already-running `-p` needs TTL/Reinstall. K8s inventory `*-lb` may still expose `:9222` / `:6080` until gateway startup reconcile / Start / Reinstall.

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

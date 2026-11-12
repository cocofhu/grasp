---
title: Gateway
description: sandbox-gateway contract summary; full details in GATEWAY.md.
---

Grasp schedules the generic sandbox image through the vendored **sandbox-gateway** control plane. The Web UI talks to the Grasp API; Agent nodes execute in containers via the gateway.

## Direct endpoints vs platform proxy

Legacy direct mode remains the default. With `sandbox.gateway_transport: gateway`, Docker, Kubernetes and E2B SSH, chat, IDE and browser connections can use the authenticated gateway, so Grasp can run outside the sandbox network. This requires a gateway API key. Browsers keep using Grasp, with previews under `/preview/...`; no separate preview domain is required. Sandboxes must still reach `server.mcp_advertise` for runtime/config downloads, MCP and other callbacks. The default named gateway affects new sandboxes only; existing sandboxes retain their connection.

## Docker and Kubernetes deployment

- [Docker deployment guide](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/docker/README.md): production Compose override, accounts and keys, browser/callback URLs, private-network troubleshooting, backups and upgrades. `.env` does not forward every variable automatically; add settings to the service environment or a mounted configuration file.
- [Kubernetes guide and manifests](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/k8s/README.md): dedicated namespaces, complete RBAC, Secrets, PVCs, Deployments, Services and Ingress. Gateway transport works with ClusterIP sandboxes without a per-sandbox LoadBalancer. The older `pods/log` files are incremental patches only.

For Docker private-network access, check container DNS, host forwarding and firewall rules. For Kubernetes, check Pod DNS, CNI/NetworkPolicy and destination ACLs. Neither uses the E2B allowlist variable; verify connectivity inside the sandbox.

- **Direct (already authenticated)**: `session` (session password), `ide` (IDE password), `ssh` (`ROOT_PASSWORD` or `SSH_KEY`).
- **Not direct**: CDP `:9222` and noVNC `:6080` have no app-layer auth, are not published to the host/LB, and are not shown or copied on the sandbox detail page.
- **User path**: platform proxy only — `/sandbox-vnc/:sandboxId/ws`, one desktop per sandbox showing the browser the Agent drives (Session required when platform Auth is enabled; validity only, no sandbox/run ownership check). “Open preview” opens sandbox console noVNC; it does not dial websockify.
- **Inventory window**: Docker already-running `-p` needs TTL/Reinstall. K8s inventory `*-lb` may still expose `:9222` / `:6080` until gateway startup reconcile / Start / Reinstall.

## Kubernetes stop confirmation

Kubernetes stop waits for the controller to observe scaling down and for live Pods to exit. The PVC is retained and services cold-start on resume; process memory is not saved. Stop is not reported as complete while it is waiting. Observation failures or a subsequent start superseding the stop return an error so operators can check the current state and retry.

## E2B private-network access

E2B sandboxes restrict private-network egress by default. A gateway or host that can reach an internal service does not grant its guests the same access. Supported self-hosted E2B/Embed versions use `ALLOW_SANDBOX_INTERNAL_CIDRS` on the **provider's orchestrator**. Preserve existing entries when adding destinations. This exception applies to every sandbox on the node; it is not a Grasp or gateway setting.

See the [E2B deployment guide: private-network access](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/e2b/README.md#内网访问) for locating the configuration, a Compose example, applying changes and verifying access from inside a guest.

## Full documentation

- [GATEWAY.md](https://github.com/cocofhu/grasp/blob/main/GATEWAY.md)

## Health checks (default local stack)

- Gateway: http://localhost:8899/healthz
- Grasp API: http://localhost:8080/api/health

## Source locations

- Control plane: `sandbox-gateway/gateway/`
- Sandbox image and scripts: `sandbox-gateway/sandbox/`, `sandbox-gateway/scripts/`

## Related

- [Configuration](../configuration/)
- [Core concepts](../../guide/concepts/)

---
title: Connect a sandbox gateway
navTitle: Gateway and previews
description: Set gateway transport, choose a backend, configure preview exposure, and diagnose sandbox startup.
section: deploy
order: 30
keywords: [gateway, preview, direct, subdomain, private_endpoints, E2B, healthz, sandbox]
---

## Connect Grasp and the gateway {#connect}

1. Configure the Docker, Kubernetes, or E2B gateway with its example. The gateway owns image, template, and resource selection.
2. Set `sandbox.gateway_url` in Grasp and provide the matching gateway API key. Verify that gateway `/healthz` is ready.
3. Select `sandbox.gateway_transport`. The default `direct` requires callers to reach the relevant endpoints. `gateway` sends traffic through the authenticated gateway and suits server-only endpoints. Named connections let sessions and workflow runs use different environments.

See the [deployment reference](https://github.com/cocofhu/grasp/blob/main/CONFIGURATION.md) and [gateway contract](https://github.com/cocofhu/grasp/blob/main/GATEWAY.md) for connection and backend fields.

## Configure app previews {#preview}

| Policy | Effect |
| --- | --- |
| `auto`, default | Public direct endpoints use direct access. Private endpoints or authenticated gateway transport use subdomains when a base URL is set. Without it, the corresponding exposure option is hidden. |
| `subdomain` | Use `preview.expose.subdomain.base_url` and wildcard DNS to give each preview its own subdomain. |
| `none` | Disable this form of preview exposure. |
| `direct` | Available on named connections that use direct transport. |

For production subdomains, use HTTPS and a root domain separate from the application. Treat a preview URL as an access credential. CDP and noVNC are proxied by the platform, with the raw ports kept on the service side. For desktop IDE access, see [Connect a local IDE](/en/help/local-ide/).

## Check network reachability {#network}

Verify reachability from Grasp and from the sandbox. For Docker, check container DNS, host forwarding, and firewall rules. For Kubernetes, check pod DNS, network policies, and target ACLs. E2B applies its own private network access policy. Use the [deployment guide](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/e2b/README.md) to configure the required destinations.

## Diagnose creation and restore {#diagnose}

Start with the sandbox status in Solo or in the run detail, then check gateway logs. Missing templates, resource mismatches, failed repository access, and capacity waits can prevent readiness. Verify current settings with a new test sandbox. See [Sandbox lifecycle](/en/help/sandbox-lifecycle/) for reclaim and restore and [Troubleshooting](/en/help/troubleshooting/) for common failures.

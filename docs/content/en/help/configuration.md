---
title: Deploy and configure Grasp
navTitle: Deployment configuration
description: Choose Docker, Kubernetes, or E2B and understand where settings live, which value wins, and when changes apply.
section: deploy
order: 10
keywords: [configuration, YAML, env, .env, CONFIG_PATH, Compose, SBGW_CONFIG, production, Docker, Kubernetes, E2B]
---

## Choose where to configure {#where-to-configure}

| What you want to change | Where | When it applies |
| --- | --- | --- |
| API accounts and tool authorization | Settings → Credentials | Agents resolve bound credentials at runtime. Follow prompts to reopen affected sessions after updates. |
| Models, coding tools, and reasoning | Settings → Credentials → Model groups, then bind the group to an agent | Session selections apply on the next turn. Automated runs use the default group. |
| Git repository, clone access, and commit identity | Settings → Repositories | Credentials are resolved when a sandbox starts or restores. |
| Capacity, idle reclaim, memory, and agent inactivity | Settings → General | Applies after saving. See [General settings](/en/help/settings/) for timing by field. |
| Ports, database, users, and gateway connections | Grasp YAML or process environment | Restart the relevant service after deployment changes. |
| Images, templates, CPU, memory, storage, and networking | Gateway YAML for the backend | Restart the gateway and verify with a new sandbox. |
| Node instructions, total duration, and human gates | Workflow canvas | Save and publish for future runs. |

Grasp uses `CONFIG_PATH` to select its YAML, defaulting to `config.yaml`. The gateway uses `-config` or `SBGW_CONFIG`. A deployment uses one Grasp configuration and one gateway configuration for the selected backend.

## Choose a sandbox backend {#backends}

| Backend | Use when | Gateway configuration |
| --- | --- | --- |
| Docker | You want a local demo or single-host service | Docker image, resource caps, and networking. Sandboxes share host CPU and memory by default. |
| Kubernetes | You run sandboxes in an existing cluster | Cluster access, namespace, scheduling, storage class, and resources. Defaults per sandbox are 2 CPU, 4096 MiB, and 25 GiB. |
| E2B | You use templates and snapshots | API address and built template IDs. The actual template determines its resources. |

Use the current deployment examples for your backend. [Docker](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/docker/README.md) · [Kubernetes](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/k8s/README.md) · [E2B](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/e2b/README.md)

The gateway owns image and template selection. Release Compose maps `SANDBOX_IMAGE` to the Docker gateway image setting. Grasp-level `sandbox.image` and `GRASP_SANDBOX_IMAGE` are retired. Docker and Kubernetes environments stay available until an explicit stop, close, or cancellation. E2B idle reclaim defaults to 360 minutes. See [Sandbox lifecycle](/en/help/sandbox-lifecycle/).

## Determine the effective value {#precedence}

- Ordinary server options use **process environment → YAML → built-in default**.
- Fields available in General settings use **process environment → saved value → YAML → default**. Environment overrides lock the corresponding fields.
- Compose reads `.env` as input. Set the container's `environment` or mount a configuration file to pass values to a service.
- Grasp's memory override defaults to `0`, which follows the gateway or template. Reset a saved memory override to `0` for future sandboxes to follow the gateway.

## Tell the time limits apart {#time-limits}

| Setting | When it starts and what it controls |
| --- | --- |
| `sandbox.idle_ttl_minutes`, default 360 minutes | An E2B model turn has ended and the queue is empty. The environment can be saved and reclaimed when this interval elapses. |
| `sandbox.chat_idle_timeout_seconds`, default 1200 seconds | A running turn is producing no output or activity. It detects an unresponsive turn. |
| Canvas node duration, falling back to `sandbox.agent_node_hard_cap_hours`, default 24 hours | Caps total execution time for an agent node. |
| `sandbox.sandbox_create_timeout_seconds`, default 1200 seconds | Waits for the gateway to report sandbox readiness, including cold-start work. Coordinate with the gateway finalize timeout. |

## Credentials, access, and backups {#secrets}

Configure explicit users and `GRASP_SECRETS_KEY` for a production deployment. Keep the key with your deployment and database backups so existing credentials can be decrypted. When setting advertised URLs and proxies, check reachability separately from a browser and from a sandbox.

To offer GitHub device login, set `GRASP_GITHUB_OAUTH_CLIENT_ID` and restart. Add model credentials and repository tokens in the application. For local IDE access, follow the [SSH gateway guide](/en/help/local-ide/).

## Verify the deployment {#verify}

1. Check Grasp `/api/health` and gateway `/healthz`. Check the startup logs for the backend and configuration source.
2. Open Settings → General and confirm the effective value, source, and lock state.
3. Start a test environment with your agent and confirm its gateway, template, resources, and preview. Follow [First run](/en/guide/first-run/) for model and repository checks.
4. Use the [Configuration index](/en/help/configuration-reference/) for YAML paths, environment variables, and defaults. Gateway and runtime fields are listed in the [deployment reference](https://github.com/cocofhu/grasp/blob/main/CONFIGURATION.md).

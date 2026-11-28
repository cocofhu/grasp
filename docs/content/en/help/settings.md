---
title: General settings explained
navTitle: General settings
description: Look up the defaults, scope, effective time, and verification for the fields in General settings.
section: use
order: 50
keywords: [General, settings, capacity, idle reclaim, memory, timeout, GRASP_CHAT_IDLE_SEC, GRASP_SANDBOX_MEMORY_MB]
---

Open Settings → General. Numeric fields display the source of their current value. Environment overrides lock the relevant fields. An administrator can change deployment variables and restart the service.

## Sandbox idle reclaim {#sandbox-idle}

**Default 360 minutes. Minimum 1 minute.** Applies to automatic reclaim for E2B. Time starts after a complete model turn when the queue is empty. Opening the page, preview, or regular terminal leaves the timer as it was. An active local IDE connection through the Grasp SSH gateway defers reclaim. A new interval starts when the last connection closes.

Changes apply on future idle checks. Verify the timer and backend in Solo's sandbox information. An individual session can choose **Never auto-reclaim** or **Keep running**, depending on the surface. Release Docker and Kubernetes environments by stopping, closing, or cancelling. YAML: `sandbox.idle_ttl_minutes`.

## Maximum active sandboxes {#sandbox-capacity}

**Default 15. Minimum 1.** Shared across all purposes and backends. Creating, running, and releasing sandboxes occupy slots. Reaching the limit restricts new sandboxes and restores, and new workflow runs can queue. This value also supplies the workflow concurrency limit.

Check the usage and limit after saving. Lowering the limit allows existing work to continue and holds new requests until there is room. YAML: `sandbox.max_sandboxes`.

## Sandbox memory override {#sandbox-memory}

**Default 0, following the gateway or template.** A nonzero value of at least 1024 MiB applies to newly created sandboxes. Docker and Kubernetes values must fit their gateway limit. On E2B the value must match the selected template. The agent, builds, and browser share that environment's memory.

Create a new sandbox to verify the resources. Enter `0` to clear the override. YAML: `sandbox.sandbox_memory_mb`. The `GRASP_SANDBOX_MEMORY_MB` environment variable locks the field.

## Agent inactivity timeout {#agent-idle}

**Default 20 minutes. UI range 2–120 minutes.** With no output, CPU activity, or disk activity for this interval, the system first attempts to resume. If the agent remains inactive, it ends that turn. A change applies starting on the next turn.

Check the run details for a turn that ended because of inactivity. Canvas node duration and sandbox readiness are separate limits. YAML `sandbox.chat_idle_timeout_seconds` and environment variable `GRASP_CHAT_IDLE_SEC` use seconds.

## Workspace and branding {#workspace-brand}

| Field | Effect |
| --- | --- |
| Workspace name and description | Change the workspace's display information. |
| Unknown model display name | Changes the label in usage reports, chart legends, and run details. Up to 64 characters. Blank restores the default label. Recorded usage retains its original attribution. |
| Product name | Admins can change the name shared by the sidebar and application home. Up to 40 characters. Blank uses Grasp. |
| Home subtitle | Admins can change the subtitle on the application home page. Up to 80 characters. Blank uses the default for the current language. |

Use the [Configuration index](/en/help/configuration-reference/) for deployment keys and [Deployment and configuration](/en/help/configuration/) for gateway and backend behavior.

---
title: Troubleshoot common problems
navTitle: Troubleshooting
description: Diagnose credential, clone, capacity, preview, and configuration issues at the stage that failed.
section: support
order: 10
keywords: [401, 403, 404, clone failed, timeout, capacity, OOM, failed, model unavailable, reclaim]
---

## Identify the failing stage {#diagnose}

Open the run detail, inbox, or Solo sandbox information. Note the failing stage, original error, agent and model group, gateway backend, and whether the environment is new. Keep relevant log context and redact credentials. Start with the first failure.

| Symptom | Check first | Next step |
| --- | --- | --- |
| Unavailable model group, empty model list, or API 401 | Credential validity, endpoint, protocol, and agent binding | Refresh the account or pick another available group. Verify with a short turn. [Model groups](/en/help/agents-models/) |
| Clone 401, 403, or missing branch | URL, host, protocol, and repository access | Check the saved credential, then retry with an accessible branch. [Git access](/en/help/repositories/) |
| Sandbox remains in preparation | Image pulls, templates, bootstrap logs, and gateway health | Check the first bootstrap error and then the readiness timeout and template resources. [Gateway](/en/help/gateway/) |
| Capacity error or a queued run | Live usage, limit, and environments still releasing | Stop unused sessions or adjust capacity. Docker and Kubernetes retain slots until an explicit release. [Capacity](/en/help/settings/#sandbox-capacity) |
| Preview unavailable | Listening app, preview registration, and transport policy | Check the service from a terminal, then the exposure policy. [Preview settings](/en/help/gateway/#preview) |
| Saved setting seems ineffective | Source, lock state, and whether it applies at boot or runtime | Check precedence and verify with a new environment if appropriate. [Precedence](/en/help/configuration/#precedence) |
| Attachments missing after migration | Database and attachment storage backup versions | Restore the same recovery set and verify any custom blob path. [Backups](/en/guide/quick-start/#backup) |

## A turn stalls or ends {#timeouts}

Check whether it is waiting for a sandbox, detecting agent inactivity, hitting the node's total limit, or waiting for a human. Use the run state and reason to choose the next action. Output and CPU or I/O activity affect agent inactivity detection. If a turn has ended, save needed logs and artifacts, then retry a minimal step in an available environment. See [Time limits](/en/help/configuration/#time-limits).

## Understand reclaim and restore {#restore}

E2B can reclaim an idle environment and restore it on next use. Check whether the environment is stopped, saving, or restoring. To keep a development server running, check the keep-running setting and local IDE connection. Docker and Kubernetes behavior is listed in the [lifecycle comparison](/en/help/sandbox-lifecycle/).

For a report, include the Grasp and gateway versions, backend type, reproduction steps, and the first error. [Open an issue](https://github.com/cocofhu/grasp/issues/new)

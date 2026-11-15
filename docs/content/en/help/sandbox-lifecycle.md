# Sandbox save, release and restore

Every sandbox purpose and Docker, Kubernetes and E2B share one idle timeout and
one capacity limit in Settings. Defaults are **60 minutes** and **15 sandboxes**.
Workflow concurrency remains a separate task scheduling limit.

| Behavior | Docker | Kubernetes | E2B |
| --- | --- | --- | --- |
| Idle | No executing task, queued request or terminal/background work | Same | Same |
| Idle timeout or Stop environment | Verify a workspace archive, then remove the container | Verify a workspace archive, then remove Deployment, Pods, PVC and exclusive resources | Verify a durable memory/disk snapshot, then report paused; no duplicate file archive |
| Next use | Recreate, restore the newest successful archive, reconnect | Same | Restore the newest successful snapshot and reconnect |
| Periodic running backup | None | None | None |
| Cancel run or close session | Delete its environment and every recovery point | Same | Delete its guest and recovery points; reclaim unreferenced disk layers |

File archives cover the workspace, excluding regenerable `node_modules`,
`.venv`, `__pycache__`, `.cache` and `.pnpm-store` directories. Docker/Kubernetes
restore does not preserve process memory or temporary files outside the
workspace. E2B preserves memory, but TCP connections and external requests must
reconnect. Docker shared bind mounts are not removed. Kubernetes storage-class
and CSI reclaim policies determine physical volume deletion.

A workflow waiting for human input can release its idle environment while
remaining in the waiting state. A reply restores it before continuing. Any
active node protects a shared sandbox. Closing the browser does not establish
idleness; status polling does not count as activity. Manual Stop uses the same
save path and refuses while work or queued requests remain. Cancel is a separate
terminal operation: conversation/workflow history stays, recovery points do not.

```yaml
sandbox:
  idle_ttl_minutes: 60
  max_sandboxes: 15
solo_backup:
  keep: 1
```

The existing `solo_backup.keep` name applies to all sandbox recovery points.
It defaults to 1 and accepts larger positive counts. Pruning follows a
successful new save. Restore always selects the newest successful point, with
no version picker or silent fallback from a corrupt archive. Existing archives
are migrated with their object references preserved.

Archive or verification failure preserves the original workload. An E2B pause
response alone does not establish successful persistence. Failed uploads retain
the previous durable point and, with the supporting provider, pin the local
pending snapshot. This cannot guarantee unsaved data against guest failure,
host power loss or storage corruption.

Failed deletion keeps persistent retry intent. Shared snapshot ancestors,
templates and live readers protect referenced layers, so `keep: 1` does not mean
one physical disk file. Once the VM is confirmed released it stops consuming a
running slot, while disk cleanup remains tracked.

Upgrade migrates saved per-purpose capacities once using the larger value;
an explicit new limit wins. Old purpose-specific timeouts, maximum retention,
manual leases, periodic backup intervals and version-selection APIs are removed.
Lowering capacity does not evict existing work. Self-hosted E2B requires its
retention schema migration and matching orchestrator/API before gateway and
Grasp. Missing capabilities fail closed. Update a single provider node only in
a maintenance window without active business work, and validate with isolated
test sandboxes.

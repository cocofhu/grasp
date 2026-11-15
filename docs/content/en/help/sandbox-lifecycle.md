# Sandbox save, release and restore

Every sandbox purpose and Docker, Kubernetes and E2B share one idle timeout and
one capacity limit in Settings. Defaults are **60 minutes** and **15 sandboxes**.
Workflow concurrency remains a separate task scheduling limit.

| Behavior | Docker | Kubernetes | E2B |
| --- | --- | --- | --- |
| Idle | No executing task, queued request or terminal/background work | Same | Same |
| Idle timeout or Stop environment | Verify a workspace archive, then remove the container | Verify a workspace archive, then remove Deployment, Pods, PVC and exclusive resources | Native pause, or independent checkpoint then release for multiple points; no file archive |
| Next use | Recreate, restore the newest successful archive, reconnect | Same | Restore the newest successful snapshot and reconnect |
| Periodic running backup | None | None | None |
| Cancel run or close session | Delete its environment and every recovery point | Same | Delete its guest and recovery points; provider reclaims physical storage |

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

E2B uses native pause/connect with `keep: 1`. Larger counts use official named
snapshot APIs, release the source guest after committing the exact checkpoint,
and prune older independent checkpoints after the new save succeeds.
Restore prefers the original paused guest. Only confirmed absence allows a new
provider guest from the checkpoint committed by the latest save; the gateway ID
stays stable. Multiple points cost additional storage and save time, without
introducing periodic backups while running.

Archive or verification failure preserves the original workload. E2B follows
the provider's official pause/snapshot recovery contract; failed saves do not
prune prior points. Grasp requires no private upload or process-exit receipt.
Provider storage monitoring and GC remain deployment responsibilities, and
unsaved state is not guaranteed against host power loss or storage corruption.

Failed deletion keeps persistent retry intent. Shared snapshot ancestors,
templates and live readers protect referenced layers, so `keep: 1` does not mean
one physical disk file. Once the VM is confirmed released it stops consuming a
running slot, while disk cleanup remains tracked.

Upgrade migrates saved per-purpose capacities once using the larger value;
an explicit new limit wins. Old purpose-specific timeouts, maximum retention,
manual leases, periodic backup intervals and version-selection APIs are removed.
Lowering capacity does not evict existing work. Standard E2B requires no private
extension or custom Dashboard. Counts above one require the official independent
snapshot API and fail explicitly when unavailable. Existing enhanced self-hosted
deployments must update their storage-tracking API and migration before the
gateway switches to standard calls. Update a single provider node only in
a maintenance window without active business work, and validate with isolated
test sandboxes.

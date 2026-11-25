# Sandbox save, release and restore

Settings has one idle timeout and one capacity limit. The idle timeout applies
to E2B only and defaults to **6 hours (360 minutes)** after the last model turn.
Docker and Kubernetes sandboxes are never saved or released for idleness; their
processes keep running until Stop, closing the session or cancelling the run.
The capacity limit covers every backend and purpose and defaults to
**15 sandboxes**. Workflow concurrency remains a separate task scheduling limit.

| Behavior | Docker | Kubernetes | E2B |
| --- | --- | --- | --- |
| Idle timeout | Not reclaimed; the environment and its processes keep running | Same | Counts once the model turn ended and no request is queued; terminals, background processes, pages and the IDE do not count. Native pause when it expires |
| Stop environment | Verify a workspace archive, then remove the container | Verify a workspace archive, then remove Deployment, Pods, PVC and exclusive resources | Native pause, or independent checkpoint then release for multiple points; no file archive |
| Next use | Recreate, restore the newest successful archive, reconnect | Same | Restore the newest successful snapshot and reconnect |
| Periodic running backup | None | None | None |
| Cancel run or close session | Delete its environment and every recovery point | Same | Delete its guest and recovery points; provider reclaims physical storage |

File archives cover the workspace, excluding regenerable `node_modules`,
`.venv`, `__pycache__`, `.cache` and `.pnpm-store` directories. Docker/Kubernetes
restore does not preserve process memory or temporary files outside the
workspace. E2B preserves memory, but TCP connections and external requests must
reconnect. Docker shared bind mounts are not removed. Kubernetes storage-class
and CSI reclaim policies determine physical volume deletion.

The idle interval and Never auto-reclaim below apply to E2B only. The idle
interval starts when a complete model turn ends with no queued request,
or when a new environment becomes ready. Generating, thinking, running tools or
waiting for tool results never counts as idle, and every new turn restarts the
interval. The interval is persisted across platform restarts. Page polling,
WebSockets, IDE/preview access and terminal activity do not extend it, so dev
servers, downloads or background commands left after a turn are saved and paused
with the environment when it expires.

To keep an environment, right-click the session or run under Running in the
sidebar and check **Never auto-reclaim**. The choice is stored on the session or
run and survives restarts, recovery and rebuilds; unchecking it grants a full
interval again. It still counts toward the running limit and does not affect
manual Stop, Cancel or Close. Docker/Kubernetes environments are never
auto-reclaimed, so the menu does not show it for them.

A workflow waiting for human input can release its idle E2B environment once its
turn has ended, while remaining in the waiting state; Docker/Kubernetes
environments stay up while it waits. A reply restores it before
continuing. Any active node protects a shared sandbox. Manual Stop uses the same
save path and refuses while a turn or queued request remains. Cancel is a separate
terminal operation: conversation/workflow history stays, recovery points do not.

## E2B idle memory return

Files an E2B guest has read stay in its page cache, and its free memory
fragments, so the host hugepage pool cannot take that memory back and new
sandboxes may fail to start. Ten minutes after the last model turn, Grasp
flushes writes, drops the guest's file cache and compacts its memory so free
memory returns to the host in whole blocks. While the guest stays idle it checks
again every 30 minutes and trims only when the cache exceeds 1 GiB, including
environments marked Never auto-reclaim. No data is lost and no process is
interrupted; the next build or large project read is slower the first time.
Docker/Kubernetes containers share the host kernel and are never trimmed.

```yaml
sandbox:
  idle_ttl_minutes: 360
  memory_trim_idle_minutes: 10 # negative disables
  memory_trim_interval_minutes: 30
  memory_trim_min_cache_mb: 1024
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

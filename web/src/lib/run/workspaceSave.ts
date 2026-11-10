import type { Run, RunWorkspaceSave } from '@/lib/shared/types'
import { soloWorkflowClient } from '@/lib/api/clients/soloWorkflowClient'
import { workspaceSnapshotsClient } from '@/lib/api/clients/workspaceSnapshotsClient'

type SaveRun = Pick<Run, 'status'> & { workspaceSave?: RunWorkspaceSave }

/**
 * The status a user should see. A run held between nodes by a workspace save
 * is not "paused" by anyone: it is saving, or the save failed.
 */
export function runDisplayStatus(run: SaveRun): string {
  if (run.status !== 'paused' || !run.workspaceSave) return run.status
  return run.workspaceSave.state === 'failed' ? 'save_failed' : 'saving'
}

const reasons = new Set(['snapshot_failed', 'snapshot_cancelled', 'snapshot_expired', 'snapshot_unknown', 'binding_unavailable', 'capture_admission_unconfirmed', 'provider_outcome_unconfirmed'])
const details = new Set(['git_unsupported', 'workspace_limit', 'workspace_changed', 'workspace_unsafe', 'workspace_integrity', 'job_deadline_expired', 'execution_owner_expired'])

/** i18n keys under workspaceSnapshots.runSave explaining a failed save. */
export function workspaceSaveReasonKeys(save: RunWorkspaceSave): string[] {
  const keys: string[] = []
  if (save.detail && details.has(save.detail)) keys.push(`workspaceSnapshots.runSave.details.${save.detail}`)
  keys.push(`workspaceSnapshots.runSave.reasons.${save.reason && reasons.has(save.reason) ? save.reason : 'other'}`)
  return keys
}

/** Moves a failed save on without re-running the node; the run then continues. */
export async function retryWorkspaceSave(run: Pick<Run, 'id' | 'controlVersion'>, save: RunWorkspaceSave) {
  if (save.action === 'reconcile') await workspaceSnapshotsClient.reconcileCheckpoint(run.id, save.checkpointId)
  else await soloWorkflowClient.controlRun(run.id, true, run.controlVersion || 0)
}

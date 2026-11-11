import { beforeEach, describe, expect, it, vi } from 'vitest'

const controlRun = vi.fn()
const reconcileCheckpoint = vi.fn()
vi.mock('@/lib/api/clients/soloWorkflowClient', () => ({ soloWorkflowClient: { controlRun: (...a: unknown[]) => controlRun(...a) } }))
vi.mock('@/lib/api/clients/workspaceSnapshotsClient', () => ({ workspaceSnapshotsClient: { reconcileCheckpoint: (...a: unknown[]) => reconcileCheckpoint(...a) } }))

import { retryWorkspaceSave, runDisplayStatus, workspaceSaveReasonKeys } from './workspaceSave'

const failed = { checkpointId: 'cp', nodeId: 'a', state: 'failed' as const, reason: 'snapshot_failed', detail: 'git_unsupported', action: 'retry' as const }

describe('workspace save', () => {
  beforeEach(() => { controlRun.mockReset(); reconcileCheckpoint.mockReset() })

  it('shows saving or save failed instead of paused', () => {
    expect(runDisplayStatus({ status: 'paused', workspaceSave: { ...failed, state: 'saving' } })).toBe('saving')
    expect(runDisplayStatus({ status: 'paused', workspaceSave: failed })).toBe('save_failed')
    expect(runDisplayStatus({ status: 'paused' })).toBe('paused')
    expect(runDisplayStatus({ status: 'running', workspaceSave: failed })).toBe('running')
  })

  it('explains the cause, falling back for unknown codes', () => {
    expect(workspaceSaveReasonKeys(failed)).toEqual(['workspaceSnapshots.runSave.details.git_unsupported', 'workspaceSnapshots.runSave.reasons.snapshot_failed'])
    expect(workspaceSaveReasonKeys({ ...failed, reason: 'new_reason', detail: 'executor_failed' })).toEqual(['workspaceSnapshots.runSave.reasons.other'])
  })

  it('retries via resume and reconciles ambiguous receipts', async () => {
    await retryWorkspaceSave({ id: 'run-1', controlVersion: 4 }, failed)
    expect(controlRun).toHaveBeenCalledWith('run-1', true, 4)
    await retryWorkspaceSave({ id: 'run-1' }, { ...failed, action: 'reconcile' })
    expect(reconcileCheckpoint).toHaveBeenCalledWith('run-1', 'cp')
  })
})

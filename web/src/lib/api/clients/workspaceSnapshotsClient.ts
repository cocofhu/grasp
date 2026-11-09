import { req } from '../httpCore'

export type WorkspaceScope = { kind: 'solo' | 'run'; id: string }
export interface WorkspaceSnapshot {
  id: string
  scopeKind: string
  scopeId: string
  runId?: string
  stateRunId?: number
  nodeId?: string
  iteration?: number
  runAttempt?: number
  providerAttempt?: number
  state: string
  bytes: number
  entries: number
  verifiedAt?: string
  retainUntil?: string
  createdAt: string
}
export interface WorkspacePage<T> { items: T[]; hasMore: boolean; nextCursor?: string }
export interface WorkflowWorkspaceCheckpoint {
  id: string
  runId: string
  stateRunId: number
  nodeId: string
  iteration: number
  runAttempt: number
  providerAttempt: number
  snapshotId?: string
  fileState: string
  routeState: string
  reason?: string
  createdAt: string
  updatedAt: string
}
export interface WorkspaceCaptureRequest {
  clientRequestId: string
  sandboxId: number
  expectedBinding: string
  paths: string[]
  includeIgnored: string[]
  durationMillis: number
}
export interface WorkspaceRestoreRequest {
  clientRequestId: string
  snapshotId: string
  expectedBinding: string
  durationMillis: number
}
export interface WorkspaceRestoreReceipt {
  id: string
  snapshotId: string
  state: string
  createdAt: string
  updatedAt: string
}
export interface WorkspaceBindingView {
  available: boolean
  id?: string
  revision?: number
  fingerprint?: string
  pendingOperationId?: string
  canActivate: boolean
  canReturn: boolean
  canCapture?: boolean
  canRestore?: boolean
  reason?: string
  returnSnapshotId?: string
  retainedBindings: { id: string; revision: number; state: string }[]
  returnPoints: { id: string; bindingId: string; snapshotId: string; state: string }[]
}
export interface WorkspaceBindingRequest {
  clientRequestId: string
  action: 'activate' | 'return'
  expectedBinding: string
  restoreId?: string
  targetBindingId?: string
  returnSnapshotId: string
  durationMillis: number
}
export interface WorkspaceBindingReceipt {
  id: string
  clientRequestId?: string
  action: string
  state: string
  requiresReconciliation?: boolean
  createdAt: string
}

const solo = (id: string) => `/solo/sessions/${encodeURIComponent(id)}`
const scopeBase = (scope: WorkspaceScope) => scope.kind === 'solo' ? solo(scope.id) : `/runs/${encodeURIComponent(scope.id)}`
const snapshots = (scope: WorkspaceScope) => `${scopeBase(scope)}/workspace-snapshots`
const binding = (id: string) => `${solo(id)}/workspace/binding`
const post = (body: unknown) => ({ method: 'POST', body: JSON.stringify(body) })

export const workspaceSnapshotsClient = {
  listCheckpoints: (runId: string) => req<{ items: WorkflowWorkspaceCheckpoint[] }>(`/runs/${encodeURIComponent(runId)}/workspace-checkpoints`),
  reconcileCheckpoint: (runId: string, id: string) => req<WorkflowWorkspaceCheckpoint>(`/runs/${encodeURIComponent(runId)}/workspace-checkpoints/${encodeURIComponent(id)}/reconcile`, post({})),
  list: (scope: WorkspaceScope, cursor = '') => req<WorkspacePage<WorkspaceSnapshot>>(`${snapshots(scope)}?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  get: (scope: WorkspaceScope, id: string) => req<WorkspaceSnapshot>(`${snapshots(scope)}/${encodeURIComponent(id)}`),
  capture: (scope: WorkspaceScope, body: WorkspaceCaptureRequest) => req<WorkspaceSnapshot>(snapshots(scope), post(body)),
  resolveCapture: (scope: WorkspaceScope, id: string) => req<WorkspaceSnapshot>(`${scopeBase(scope)}/workspace-snapshot-requests/${encodeURIComponent(id)}`),
  reconcileCapture: (scope: WorkspaceScope, id: string) => req<WorkspaceSnapshot>(`${snapshots(scope)}/${encodeURIComponent(id)}/reconcile`, post({})),
  restore: (sessionId: string, body: WorkspaceRestoreRequest) => req<WorkspaceRestoreReceipt>(`${solo(sessionId)}/workspace-restores`, post(body)),
  listRestores: (sessionId: string, cursor = '') => req<WorkspacePage<WorkspaceRestoreReceipt>>(`${solo(sessionId)}/workspace-restores?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  getRestore: (sessionId: string, id: string) => req<WorkspaceRestoreReceipt>(`${solo(sessionId)}/workspace-restores/${encodeURIComponent(id)}`),
  resolveRestore: (sessionId: string, id: string) => req<WorkspaceRestoreReceipt>(`${solo(sessionId)}/workspace-restore-requests/${encodeURIComponent(id)}`),
  reconcileRestore: (sessionId: string, id: string) => req<WorkspaceRestoreReceipt>(`${solo(sessionId)}/workspace-restores/${encodeURIComponent(id)}/reconcile`, post({})),
  currentBinding: (sessionId: string) => req<WorkspaceBindingView>(binding(sessionId)),
  listBindingOperations: (sessionId: string) => req<WorkspaceBindingReceipt[]>(`${binding(sessionId)}/operations`),
  changeBinding: (sessionId: string, body: WorkspaceBindingRequest) => req<WorkspaceBindingReceipt>(`${binding(sessionId)}/operations`, post(body)),
  getBindingOperation: (sessionId: string, id: string) => req<WorkspaceBindingReceipt>(`${binding(sessionId)}/operations/${encodeURIComponent(id)}`),
  resolveBindingOperation: (sessionId: string, id: string) => req<WorkspaceBindingReceipt>(`${binding(sessionId)}/operations/by-request/${encodeURIComponent(id)}`),
  reconcileBinding: (sessionId: string, id: string) => req<WorkspaceBindingReceipt>(`${binding(sessionId)}/operations/${encodeURIComponent(id)}/reconcile`, post({})),
}

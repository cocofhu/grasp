import type { Gate, Run } from '../../shared/types'
import type { WorkflowFeature } from '@/lib/composables/useWorkflowFeature'
import { req } from '../httpCore'

export type SoloWorkflowTaskNode = { id: string; label: string; type: string; status: string; iteration: number }
export type SoloWorkflowTask = Pick<Run, 'id' | 'title' | 'workflowName' | 'status' | 'pauseRequested' | 'controlVersion'> & { nodes: SoloWorkflowTaskNode[]; pendingGate?: Gate }

export type WorkflowInstructionTarget = { runId: string; nodeId: string; iteration: number; attempt?: number; generation: string; state: string }
export type WorkflowInstruction = { id: string; iteration: number; attempt?: number; generation: string; sequence: number; actorId: string; text: string; state: string; error?: string }
export type WorkflowInstructionHistory = { target: WorkflowInstructionTarget | null; items: WorkflowInstruction[]; hasMore: boolean }
export type WorkflowInstructionRequest = { clientMessageId: string; text: string; generation: string; iteration: number; attempt?: number }

export type WorkspaceImportSource = { nodeId: string; iteration: number; attempt: number; shared: boolean; available: boolean; state: string; createdAt: string }
export type WorkspaceImportSourceRef = Pick<WorkspaceImportSource, 'nodeId' | 'iteration' | 'attempt'>
export type WorkspaceImportRepository = { path: string; commit: string; head: string; supported: boolean; dirty: boolean; untracked: boolean; files: number; bytes: number; reason?: string }
export type WorkspaceImportInventory = { version: number; repositories: WorkspaceImportRepository[]; workingChanges: boolean; nonGit: boolean; ignoredFiles: boolean }
export type WorkspaceImportReceipt = { id: string; source: WorkspaceImportSourceRef & { runId: string }; state: string; repositories: WorkspaceImportRepository[]; directory?: string; destination?: string; paths: string[]; error?: string; createdAt: string; expiresAt?: string }

export type EnvironmentReleaseReceipt = { id: string; state: string; wholeEnvironment: boolean; chatsRetained: boolean; snapshotsRetained: number; workspacesAffected: number; returnPointsRetained: number; lossConfirmationRequired: boolean; requiresReconciliation: boolean; blockers?: string[]; error?: string; createdAt: string }

const enc = encodeURIComponent
const post = (body: unknown = {}) => ({ method: 'POST', body: JSON.stringify(body) })
const solo = (id: string) => `/solo/sessions/${enc(id)}`
const imports = (sessionId: string) => `${solo(sessionId)}/workspace-imports`
const releases = (sessionId: string) => `${solo(sessionId)}/environment-releases`
const instructions = (runId: string, nodeId: string) => `/runs/${enc(runId)}/nodes/${enc(nodeId)}/instructions`

export const soloWorkflowClient = {
  listSessionWorkflows: (sessionId: string) => req<{ items: SoloWorkflowTask[]; workflows: WorkflowFeature }>(`${solo(sessionId)}/workflows`),
  controlRun: (runId: string, resume: boolean, version: number) => req(`/runs/${enc(runId)}/control/${resume ? 'resume' : 'pause'}`, post({ version })),

  instructionHistory: (runId: string, nodeId: string, before = 0) =>
    req<WorkflowInstructionHistory>(`${instructions(runId, nodeId)}?${new URLSearchParams({ before: String(before) })}`),
  sendInstruction: (runId: string, nodeId: string, body: WorkflowInstructionRequest) => req<WorkflowInstruction>(instructions(runId, nodeId), post(body)),

  importSources: (sessionId: string, runId: string) => req<WorkspaceImportSource[]>(`${imports(sessionId)}/sources?runId=${enc(runId)}`),
  listImports: (sessionId: string, runId: string) => req<WorkspaceImportReceipt[]>(`${imports(sessionId)}?runId=${enc(runId)}`),
  importInventory: (sessionId: string, runId: string, source: WorkspaceImportSourceRef) =>
    req<WorkspaceImportInventory>(`${imports(sessionId)}/inventory`, post({ runId, nodeId: source.nodeId, iteration: source.iteration, attempt: source.attempt })),
  previewImport: (sessionId: string, runId: string, source: WorkspaceImportSourceRef, repositories: { path: string; commit: string }[], clientRequestId: string) =>
    req<WorkspaceImportReceipt>(imports(sessionId), post({ runId, nodeId: source.nodeId, iteration: source.iteration, attempt: source.attempt, repositories, clientRequestId })),
  getImport: (sessionId: string, id: string) => req<WorkspaceImportReceipt>(`${imports(sessionId)}/${enc(id)}`),
  confirmImport: (sessionId: string, id: string, clientRequestId: string) =>
    req<WorkspaceImportReceipt>(`${imports(sessionId)}/${enc(id)}/confirm`, post({ confirmed: true, clientRequestId })),
  reconcileImport: (sessionId: string, id: string) => req<WorkspaceImportReceipt>(`${imports(sessionId)}/${enc(id)}/reconcile`, { method: 'POST' }),

  listReleases: (sessionId: string) => req<{ items: EnvironmentReleaseReceipt[] }>(releases(sessionId)),
  previewRelease: (sessionId: string, clientRequestId: string) => req<EnvironmentReleaseReceipt>(releases(sessionId), post({ clientRequestId })),
  getRelease: (sessionId: string, id: string) => req<EnvironmentReleaseReceipt>(`${releases(sessionId)}/${enc(id)}`),
  confirmRelease: (sessionId: string, id: string, clientRequestId: string) =>
    req<EnvironmentReleaseReceipt>(`${releases(sessionId)}/${enc(id)}/confirm`, post({ clientRequestId, confirmed: true, lossAccepted: true })),
  reconcileRelease: (sessionId: string, id: string) => req<EnvironmentReleaseReceipt>(`${releases(sessionId)}/${enc(id)}/reconcile`, { method: 'POST' }),
}

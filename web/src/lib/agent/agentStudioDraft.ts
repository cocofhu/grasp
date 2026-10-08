import type { Agent, AgentCapabilities, MCPServer } from '@/lib/api/api'
import { normalizeCapabilities } from '@/lib/workflow/agentCapabilities'
import type { GitCredentialType } from '@/lib/agent/gitCredentialType'
import {
  ACP_BACKENDS,
  normalizeRegions,
  type BackendId,
} from '@/lib/shared/regionPolicy'

export type KV = { k: string; v: string }
export type DraftMCP = {
  name: string
  transport: 'url' | 'command'
  url: string
  headers: KV[]
  command: string
  args: string
  env: KV[]
}
export type DraftFile = { path: string; content: string }
export type AgentStudioDraft = {
  name: string
  projectId: string
  acpBackend: BackendId
  openCodeCredentialId?: string
  aiCredentialId?: string
  gitCredentialId?: string
  sshHostsCredentialId?: string
  gitCredentialType?: GitCredentialType
  files: DraftFile[]
  mcp: DraftMCP[]
  env: KV[]
  layout: { configRoot: string; workspaceDir: string }
  /** null = not declared; the Agent cannot run workflow nodes until it is. */
  capabilities: AgentCapabilities | null
}

export const DEFAULT_CONFIG_ROOT = '/root/.cursor'
export const DEFAULT_WORKSPACE_DIR = '/root/workspace'

export const ARTIFACT_STORE = 'artifact-store'
export const MEMORY_STORE = 'memory-store'
export const CONTEXT_STORE = 'context-store'
export const TASK_SCHEDULER = 'task-scheduler'

export const AGENT_PLATFORM_MCPS = [
  {
    name: MEMORY_STORE,
    url: '${GRASP_MEMORY_URL}',
    token: '${GRASP_MEMORY_TOKEN}',
  },
  {
    name: CONTEXT_STORE,
    url: '${GRASP_CONTEXT_URL}',
    token: '${GRASP_CONTEXT_TOKEN}',
  },
  {
    name: TASK_SCHEDULER,
    url: '${GRASP_SCHEDULER_URL}',
    token: '${GRASP_SCHEDULER_TOKEN}',
  },
] as const

export function recToKV(rec?: Record<string, string>): KV[] {
  return Object.entries(rec || {}).map(([k, v]) => ({ k, v }))
}

export function kvToRec(kvs: KV[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const { k, v } of kvs) if (k.trim()) out[k.trim()] = v
  return out
}

export function normalizeDraftRegions(d: AgentStudioDraft): void {
  d.env = recToKV(normalizeRegions(kvToRec(d.env), d.acpBackend, 'preserve-special').env)
}

export function apiMcpToDraft(m: MCPServer): DraftMCP {
  return {
    name: m.name,
    transport: m.command ? 'command' : 'url',
    url: m.url ?? '',
    headers: recToKV(m.headers),
    command: m.command ?? '',
    args: (m.args || []).join('\n'),
    env: recToKV(m.env),
  }
}

export function draftMcpToApi(m: DraftMCP): MCPServer {
  if (m.transport === 'command') {
    return {
      name: m.name.trim(),
      command: m.command.trim(),
      args: m.args
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean),
      env: kvToRec(m.env),
    }
  }
  return { name: m.name.trim(), url: m.url.trim(), headers: kvToRec(m.headers) }
}

export function defaultConfigRootFor(backend: BackendId): string {
  return ACP_BACKENDS.find((b) => b.id === backend)?.configRoot || DEFAULT_CONFIG_ROOT
}

function cloneCapabilities(c: AgentCapabilities): AgentCapabilities {
  return {
    ...c,
    tools: [...(c.tools || [])],
    reads: [...(c.reads || [])],
    writes: (c.writes || []).map((w) => ({ ...w })),
  }
}

export function toDraft(a: Agent): AgentStudioDraft {
  return {
    name: a.name,
    projectId: a.projectId,
    acpBackend: (a.acpBackend as BackendId) || 'cursor',
    openCodeCredentialId: a.openCodeCredentialId || '',
    aiCredentialId: a.aiCredentialId || '',
    gitCredentialId: a.gitCredentialId || '',
    sshHostsCredentialId: a.sshHostsCredentialId || '',
    gitCredentialType: a.gitCredentialType,
    files: (a.files || []).map((f) => ({ path: f.path, content: f.content })),
    mcp: (a.mcp || []).map(apiMcpToDraft),
    env: recToKV(a.env),
    layout: {
      configRoot: a.layout?.configRoot || DEFAULT_CONFIG_ROOT,
      workspaceDir: a.layout?.workspaceDir || DEFAULT_WORKSPACE_DIR,
    },
    capabilities: a.capabilities ? cloneCapabilities(a.capabilities) : null,
  }
}

export function fromDraftRaw(d: AgentStudioDraft): Agent {
  return {
    name: d.name,
    projectId: d.projectId.trim(),
    acpBackend: d.acpBackend || 'cursor',
    ...(d.openCodeCredentialId?.trim() ? { openCodeCredentialId: d.openCodeCredentialId.trim() } : {}),
    ...(d.aiCredentialId?.trim() ? { aiCredentialId: d.aiCredentialId.trim() } : {}),
    ...(d.gitCredentialId?.trim() ? { gitCredentialId: d.gitCredentialId.trim() } : {}),
    ...(d.sshHostsCredentialId?.trim() ? { sshHostsCredentialId: d.sshHostsCredentialId.trim() } : {}),
    ...(d.gitCredentialType ? { gitCredentialType: d.gitCredentialType } : {}),
    files: d.files.filter((f) => f.path.trim()).map((f) => ({ path: f.path.trim(), content: f.content })),
    mcp: d.mcp.filter((m) => m.name.trim()).map(draftMcpToApi),
    env: kvToRec(d.env),
    layout: {
      configRoot: d.layout.configRoot.trim() || DEFAULT_CONFIG_ROOT,
      workspaceDir: d.layout.workspaceDir.trim() || DEFAULT_WORKSPACE_DIR,
    },
    ...(d.capabilities ? { capabilities: normalizeCapabilities(d.capabilities) } : {}),
  }
}

export function fromDraft(d: AgentStudioDraft): Agent {
  const payload = fromDraftRaw(d)
  payload.env = normalizeRegions(payload.env || {}, d.acpBackend, 'preserve-special').env
  return payload
}

/** Canonical payload JSON for originalJson / agentDirty (same serializer both sides). */
export function draftPayloadJson(d: AgentStudioDraft): string {
  return JSON.stringify(fromDraft(d))
}

/** Load API agent into a studio draft and apply region normalization before snapshotting. */
export function hydrateStudioDraft(a: Agent): AgentStudioDraft {
  const loaded = toDraft(a)
  normalizeDraftRegions(loaded)
  return loaded
}

export type PlatformPresetKind = 'artifact' | 'memory' | 'context' | 'scheduler'

export function platformPresetKind(name: string): PlatformPresetKind | null {
  const n = name.trim()
  if (n === ARTIFACT_STORE) return 'artifact'
  if (n === MEMORY_STORE) return 'memory'
  if (n === CONTEXT_STORE) return 'context'
  if (n === TASK_SCHEDULER) return 'scheduler'
  return null
}

export function isPlatformPresetName(name: string) {
  return platformPresetKind(name) !== null
}

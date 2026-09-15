import type { WFEdge, WFNode, Workflow } from '../shared/types'

const EXPORT_SCHEMA_VERSION = 1

export const AGENT_PROFILE_KEY = 'agent_profile'

function asConfig(config?: Record<string, unknown> | null): Record<string, unknown> | null {
  return config && typeof config === 'object' ? config : null
}

/** Read Agent name from agent_profile. */
export function getAgentProfile(config?: Record<string, unknown> | null): string {
  const cfg = asConfig(config)
  if (!cfg) return ''
  return typeof cfg[AGENT_PROFILE_KEY] === 'string' ? String(cfg[AGENT_PROFILE_KEY]).trim() : ''
}

/** Write agent_profile. */
export function setAgentProfile(config: Record<string, unknown>, name: string): void {
  config[AGENT_PROFILE_KEY] = String(name || '').trim()
}

export interface ExportEnvelope {
  schemaVersion: number
  exportedAt: string
  name: string
  description: string
  needsRepo: boolean
  graph: {
    nodes: WFNode[]
    edges: WFEdge[]
    variables: any[]
  }
}

export interface WorkflowGraphPayload {
  nodes: WFNode[]
  edges: WFEdge[]
  variables?: any[]
}

/** Replace illegal filename characters with underscore. */
export function sanitizeFilename(name: string): string {
  return name.replace(/[^\w\u4e00-\u9fff\- ]/g, '_') + '.json'
}

/** Strip runtime fields from nodes before export. */
function exportNodes(nodes: WFNode[]): WFNode[] {
  return nodes.map((n) => {
    const { id, type, label, position, config, checkpoint } = n
    return { id, type, label, position, config: config ? { ...config } : {}, ...(checkpoint ? { checkpoint } : {}) }
  })
}

/** Build a portable export envelope from workflow metadata and graph. */
export function buildEnvelope(
  meta: Pick<Workflow, 'name' | 'description' | 'needsRepo'>,
  graph: WorkflowGraphPayload,
): ExportEnvelope {
  const input = graph.nodes.find((n) => n.type === 'input')
  let variables = graph.variables ?? []
  if (!variables.length && input?.config?.variables) {
    variables = [...(input.config.variables as any[])]
  }
  const nodes = exportNodes(graph.nodes).map((n) => {
    const cfg = { ...(n.config || {}) }
    if (n.type !== 'input') return { ...n, config: cfg }
    delete cfg.variables
    delete cfg.inputs
    return { ...n, config: cfg }
  })
  return {
    schemaVersion: EXPORT_SCHEMA_VERSION,
    exportedAt: new Date().toISOString(),
    name: meta.name,
    description: meta.description ?? '',
    needsRepo: !!meta.needsRepo,
    graph: {
      nodes,
      edges: graph.edges.map((e) => ({ ...e })),
      variables,
    },
  }
}

/** Trigger a browser download of JSON content. */
export function downloadJson(filename: string, data: unknown) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = filename
  a.click()
  URL.revokeObjectURL(a.href)
}

/** Collect agent_profile references from agent-class nodes. */
export function collectAgentProfiles(nodes: WFNode[]): string[] {
  const agentTypes = new Set([
    'react', 'grasp', 'approve', 'preflight', 'agent', 'plan', 'implement', 'research', 'test', 'review', 'proposal', 'submit_mr', 'visual', 'app_preview',
  ])
  const out = new Set<string>()
  for (const n of nodes) {
    if (!agentTypes.has(n.type)) continue
    const sp = getAgentProfile(n.config)
    if (sp) out.add(sp)
  }
  return [...out]
}

export type AgentProfileIssue = {
  name: string
  /** missing = not found / deleted; foreign = unbound or other project */
  reason: 'missing' | 'foreign'
}

/**
 * Return agent_profile refs that are missing or not bound to the workflow project.
 * Unbound Agents (empty projectId) count as foreign.
 */
export function agentProfileIssues(
  nodes: WFNode[],
  agents: { name: string; projectId?: string }[],
  projectId?: string,
): AgentProfileIssue[] {
  const byName = new Map(agents.map((a) => [a.name, a]))
  const pid = String(projectId || '').trim()
  const out: AgentProfileIssue[] = []
  for (const name of collectAgentProfiles(nodes)) {
    const a = byName.get(name)
    if (!a) {
      out.push({ name, reason: 'missing' })
      continue
    }
    const home = String(a.projectId || '').trim()
    if (!home || !pid || home !== pid) {
      out.push({ name, reason: 'foreign' })
    }
  }
  return out
}

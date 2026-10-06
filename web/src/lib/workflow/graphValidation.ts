// Client-side workflow graph checks; mirrors models.Graph.Validate,
// nodereg.ValidateNodeTypes and the run-start capability snapshot.

import type { NodeType, WFEdge, WFNode } from '@/lib/shared/types'
import { capabilityIssueKey, validateCapabilities } from './agentCapabilities'
import type { AgentCapsLookup } from './nodeOutlets'

export const NODE_TYPES: NodeType[] = ['input', 'output', 'set_var', 'branch', 'agent', 'human_gate']

export function isKnownNodeType(type: unknown): type is NodeType {
  return NODE_TYPES.includes(type as NodeType)
}

type Translate = (key: string, params?: Record<string, unknown>) => string

export type GraphValidationOptions = {
  t: Translate
  /** Loaded Agents by name; when omitted, Agent capability checks are skipped. */
  agents?: AgentCapsLookup
}

function agentEntry(agents: AgentCapsLookup, name: string) {
  return agents instanceof Map ? agents.get(name) : agents[name]
}

/** First problem in the graph as a user-facing message, or '' when valid. */
export function workflowGraphError(
  graph: { nodes: Pick<WFNode, 'id' | 'type' | 'label' | 'config'>[]; edges: WFEdge[] },
  { t, agents }: GraphValidationOptions,
): string {
  const { nodes, edges } = graph
  for (const n of nodes) {
    if (!isKnownNodeType(n.type)) return t('pages.workflowEditor.graphErrors.unknownNodeType', { type: String(n.type) })
  }
  const inputs = nodes.filter((n) => n.type === 'input')
  const outputs = nodes.filter((n) => n.type === 'output')
  if (!inputs.length) return t('pages.workflowEditor.graphErrors.missingInput')
  if (inputs.length > 1) return t('pages.workflowEditor.graphErrors.tooManyInputs')
  if (!outputs.length) return t('pages.workflowEditor.graphErrors.missingOutput')
  const incoming = new Set(edges.map((e) => e.target))
  const outgoing = new Set(edges.map((e) => e.source))
  if (incoming.has(inputs[0].id)) return t('pages.workflowEditor.graphErrors.inputHasIncoming')
  if (outputs.some((o) => outgoing.has(o.id))) return t('pages.workflowEditor.graphErrors.outputHasOutgoing')

  const counts = new Map<string, number>()
  for (const e of edges) {
    if ((e.kind && e.kind !== 'success') || String(e.when ?? '').trim()) continue
    const key = `${e.source}\u0000${e.sourceHandle ?? ''}`
    const n = (counts.get(key) ?? 0) + 1
    counts.set(key, n)
    if (n > 1) {
      const node = nodes.find((nn) => nn.id === e.source)
      return t('pages.workflowEditor.graphErrors.duplicateSuccessFanOut', { label: node?.label || e.source })
    }
  }

  for (const n of nodes) {
    if (n.type === 'agent' && !String(n.config?.agent_profile ?? '').trim()) {
      return t('pages.workflowEditor.graphErrors.agentMissingProfile', { label: n.label || n.id })
    }
  }

  if (agents) {
    for (const n of nodes) {
      if (n.type !== 'agent') continue
      const profile = String(n.config?.agent_profile ?? '').trim()
      const agent = agentEntry(agents, profile)
      if (!agent) return t('pages.workflowEditor.graphErrors.agentNotFound', { label: n.label || n.id, name: profile })
      const issue = validateCapabilities(agent.capabilities)
      if (issue?.code === 'missing') return t('pages.workflowEditor.graphErrors.agentNoCapabilities', { name: profile })
      if (issue) {
        return t('pages.workflowEditor.graphErrors.agentInvalidCapabilities', {
          name: profile,
          reason: t(capabilityIssueKey(issue), { value: issue.value ?? '' }),
        })
      }
    }
  }
  return ''
}

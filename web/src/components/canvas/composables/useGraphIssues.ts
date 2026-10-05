import type { WFEdge, WFNode } from '@/lib/shared/types'
import { capabilityIssueKey, validateCapabilities } from '@/lib/workflow/agentCapabilities'
import { isKnownNodeType } from '@/lib/workflow/graphValidation'
import { isNodeOutlet } from '@/lib/workflow/nodeOutlets'
import { agentLookup, findAgent, normHandle, type CanvasAgent, type Translate } from './outlets'

export interface GraphIssue {
  /** Node the issue is shown on; undefined = workflow-level. */
  nodeId?: string
  message: string
}

/**
 * Every problem in the graph, attached to the node it belongs to where possible.
 * `agents === null` means Agents are still loading: Agent checks are skipped.
 */
export function collectGraphIssues(
  graph: { nodes: WFNode[]; edges: WFEdge[] },
  agents: CanvasAgent[] | null,
  t: Translate,
): GraphIssue[] {
  const { nodes, edges } = graph
  const issues: GraphIssue[] = []
  const lookup = agents ? agentLookup(agents) : undefined
  const inputs = nodes.filter((n) => n.type === 'input')
  if (!inputs.length) issues.push({ message: t('canvas.issues.missingInput') })
  if (!nodes.some((n) => n.type === 'output')) issues.push({ message: t('canvas.issues.missingOutput') })
  for (const extra of inputs.slice(1)) issues.push({ nodeId: extra.id, message: t('canvas.issues.tooManyInputs') })

  const plainCount = new Map<string, number>()
  for (const e of edges) {
    if ((e.kind && e.kind !== 'success') || String(e.when ?? '').trim()) continue
    const key = `${e.source}\u0000${normHandle(e.sourceHandle)}`
    plainCount.set(key, (plainCount.get(key) ?? 0) + 1)
  }

  for (const n of nodes) {
    const add = (key: string, named?: Record<string, unknown>) => issues.push({ nodeId: n.id, message: t(key, named) })
    if (!isKnownNodeType(n.type)) {
      add('canvas.issues.unknownType', { type: String(n.type) })
      continue
    }
    const incoming = edges.filter((e) => e.target === n.id)
    const outgoing = edges.filter((e) => e.source === n.id)
    if (n.type === 'input' && incoming.length) add('canvas.issues.inputHasIncoming')
    if (n.type === 'output' && outgoing.length) add('canvas.issues.outputHasOutgoing')
    if (n.type !== 'input' && !incoming.length) add('canvas.issues.unreachable')
    if (n.type === 'agent') {
      const profile = String(n.config?.agent_profile ?? '').trim()
      if (!profile) add('canvas.issues.noAgent')
      else if (agents) {
        const agent = findAgent(agents, profile)
        if (!agent) add('canvas.issues.agentNotFound', { name: profile })
        else {
          const issue = validateCapabilities(agent.capabilities)
          if (issue?.code === 'missing') add('canvas.issues.agentNoCaps', { name: profile })
          else if (issue) add('canvas.issues.agentBadCaps', { name: profile, reason: t(capabilityIssueKey(issue), { value: issue.value ?? '' }) })
        }
      }
      if (!String(n.config?.prompt ?? '').trim()) add('canvas.issues.noGoal')
    }
    if (lookup || n.type !== 'agent') {
      const stale = new Set<string>()
      for (const e of outgoing) {
        const h = normHandle(e.sourceHandle)
        if (!isNodeOutlet(n, h || undefined, lookup)) stale.add(h || '—')
      }
      for (const h of stale) add('canvas.issues.staleOutlet', { handle: h })
    }
    const dup = [...plainCount].filter(([k, c]) => c > 1 && k.startsWith(`${n.id}\u0000`))
    if (dup.length) add('canvas.issues.duplicateFanOut')
  }
  return issues
}

export function issuesByNode(issues: GraphIssue[]): Map<string, string[]> {
  const m = new Map<string, string[]>()
  for (const i of issues) {
    if (!i.nodeId) continue
    const list = m.get(i.nodeId) ?? []
    list.push(i.message)
    m.set(i.nodeId, list)
  }
  return m
}

import type { NodeType, WFEdge, WFNode } from '@/lib/shared/types'
import { normHandle } from './outlets'

export interface GraphLike {
  nodes: WFNode[]
  edges: WFEdge[]
}

export interface ConnectionAttempt {
  source: string
  sourceHandle?: string | null
  target: string
}

/**
 * Rejection reasons are i18n keys under canvas.rules. An outlet holds one
 * unconditional edge, so connecting it again replaces that edge (`replaces`).
 */
export type ConnectionResult = { ok: true; replaces?: string } | { ok: false; reason: string }

const OK: ConnectionResult = { ok: true }

function fail(reason: string): ConnectionResult {
  return { ok: false, reason: `canvas.rules.${reason}` }
}

export function checkConnection(graph: GraphLike, c: ConnectionAttempt): ConnectionResult {
  if (!c.source || !c.target) return fail('missingEnd')
  if (c.source === c.target) return fail('selfLoop')
  const src = graph.nodes.find((n) => n.id === c.source)
  const dst = graph.nodes.find((n) => n.id === c.target)
  if (!src || !dst) return fail('missingEnd')
  if (dst.type === 'input') return fail('inputNoIncoming')
  if (src.type === 'output') return fail('outputNoOutgoing')
  const handle = normHandle(c.sourceHandle)
  const sameOutlet = graph.edges.filter((e) => e.source === c.source && normHandle(e.sourceHandle) === handle)
  if (sameOutlet.some((e) => e.target === c.target)) return fail('duplicate')
  const taken = sameOutlet.find((e) => !String(e.when ?? '').trim() && (e.kind ?? 'success') === 'success')
  return taken ? { ok: true, replaces: taken.id } : OK
}

/** Whether a node of this type may be added (only one input per workflow). */
export function checkAddNode(graph: GraphLike, type: NodeType): ConnectionResult {
  if (type === 'input' && graph.nodes.some((n) => n.type === 'input')) return fail('singleInput')
  return OK
}

export function useConnectionRules(graph: () => GraphLike) {
  return {
    check: (c: ConnectionAttempt) => checkConnection(graph(), c),
    canAdd: (type: NodeType) => checkAddNode(graph(), type),
  }
}

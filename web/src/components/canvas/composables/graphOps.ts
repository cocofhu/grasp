import { NODE_DEFS, syncHumanGateFormDefaults } from '@/data/nodeRegistry'
import type { NodeType, WFEdge, WFNode } from '@/lib/shared/types'
import type { GraphLike } from './useConnectionRules'
import { normHandle } from './outlets'

export const GRID = 8

export function snap(v: number): number {
  return Math.round(v / GRID) * GRID
}

function rand(len: number): string {
  return Math.random().toString(36).slice(2, 2 + len).padEnd(len, '0')
}

export function newNodeId(type: string, taken: Iterable<string>): string {
  const used = new Set(taken)
  let id = ''
  do id = `${type}_${rand(4)}`
  while (used.has(id))
  return id
}

export function newEdgeId(taken: Iterable<string>): string {
  const used = new Set(taken)
  let id = ''
  do id = `e_${rand(6)}`
  while (used.has(id))
  return id
}

export function newCaseId(cases: { id?: string }[]): string {
  const used = new Set(cases.map((c) => String(c?.id ?? '')))
  let i = cases.length + 1
  while (used.has(`case_${i}`)) i++
  return `case_${i}`
}

function clone<T>(v: T): T {
  return v === undefined ? v : (JSON.parse(JSON.stringify(v)) as T)
}

/** Default config for a fresh node, normalized to the current node model. */
export function defaultConfig(type: NodeType, opts: { agentProfile?: string } = {}): Record<string, any> {
  const base = clone(NODE_DEFS[type]?.defaults ?? {}) as Record<string, any>
  switch (type) {
    case 'agent':
      return { agent_profile: opts.agentProfile ?? '', prompt: '' }
    case 'branch':
      return { cases: [{ id: 'case_1', when: '' }] }
    case 'human_gate': {
      const actions = Array.isArray(base.actions) ? base.actions : []
      base.actions = actions.map((a: any) => ({ id: a.id, label: a.label, ...(a.requireForm ? { requireForm: true } : {}) }))
      syncHumanGateFormDefaults(base)
      return base
    }
    default:
      return base
  }
}

export interface NodeSpec {
  type: NodeType
  agentProfile?: string
  label?: string
}

export function createNode(spec: NodeSpec, at: { x: number; y: number }, taken: Iterable<string>): WFNode {
  return {
    id: newNodeId(spec.type, taken),
    type: spec.type,
    label: spec.label || spec.agentProfile || spec.type,
    position: { x: snap(at.x), y: snap(at.y) },
    config: defaultConfig(spec.type, { agentProfile: spec.agentProfile }),
  }
}

export function makeEdge(
  graph: GraphLike,
  c: { source: string; sourceHandle?: string | null; target: string },
  extra: Partial<WFEdge> = {},
): WFEdge {
  const handle = normHandle(c.sourceHandle)
  const e: WFEdge = { ...extra, id: newEdgeId(graph.edges.map((x) => x.id)), source: c.source, target: c.target }
  if (handle) e.sourceHandle = handle
  else delete e.sourceHandle
  return e
}

/** Splits edge `edgeId` around `node`: source → node keeps the edge's guard, node → target is plain. */
export function insertOnEdge(graph: GraphLike, edgeId: string, node: WFNode, nodeOutlet = ''): boolean {
  const idx = graph.edges.findIndex((e) => e.id === edgeId)
  if (idx < 0) return false
  const old = graph.edges[idx]
  graph.nodes.push(node)
  const { id: _id, target: _t, ...rest } = old
  const inEdge: WFEdge = { ...rest, id: old.id, target: node.id }
  graph.edges.splice(idx, 1, inEdge)
  if (node.type !== 'output') {
    graph.edges.push(makeEdge(graph, { source: node.id, sourceHandle: nodeOutlet, target: old.target }))
  }
  return true
}

export function removeElements(graph: GraphLike, nodeIds: Iterable<string>, edgeIds: Iterable<string> = []): boolean {
  const nodes = new Set(nodeIds)
  const edges = new Set(edgeIds)
  const before = graph.nodes.length + graph.edges.length
  if (nodes.size) graph.nodes.splice(0, graph.nodes.length, ...graph.nodes.filter((n) => !nodes.has(n.id)))
  graph.edges.splice(
    0,
    graph.edges.length,
    ...graph.edges.filter((e) => !edges.has(e.id) && !nodes.has(e.source) && !nodes.has(e.target)),
  )
  return graph.nodes.length + graph.edges.length !== before
}

/** Neighbors reachable over one edge in either direction (for arrow-key navigation). */
export function neighbors(graph: GraphLike, id: string): { prev: string[]; next: string[] } {
  return {
    prev: graph.edges.filter((e) => e.target === id).map((e) => e.source),
    next: graph.edges.filter((e) => e.source === id).map((e) => e.target),
  }
}

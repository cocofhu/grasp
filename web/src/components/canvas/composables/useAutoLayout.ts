import ELK from 'elkjs/lib/elk.bundled.js'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { snap } from './graphOps'

export type Point = { x: number; y: number }
export type Size = { width: number; height: number }

export const AGENT_NODE_WIDTH = 240
export const NODE_WIDTH = 200
export const LAYOUT_DURATION_MS = 280

let elk: InstanceType<typeof ELK> | null = null
function engine() {
  return (elk ??= new ELK())
}

/** Card size before the node has been measured. */
export function estimateNodeSize(n: WFNode): Size {
  const width = n.type === 'agent' ? AGENT_NODE_WIDTH : NODE_WIDTH
  const cfg = (n.config || {}) as Record<string, any>
  const outlets =
    n.type === 'branch'
      ? (Array.isArray(cfg.cases) ? cfg.cases.length : 0) + 1
      : n.type === 'human_gate'
        ? Array.isArray(cfg.actions) ? cfg.actions.length : 0
        : 0
  const base = n.type === 'agent' ? 120 : 64
  return { width, height: base + Math.max(0, outlets) * 24 }
}

/**
 * Edges that point back along the flow (loops such as fail → implement), found by
 * a DFS that starts at the input node and follows edges in model order. They are
 * left out of the layered ranking so the main path reads left to right.
 */
export function findBackEdges(nodes: WFNode[], edges: WFEdge[]): Set<string> {
  const ids = new Set(nodes.map((n) => n.id))
  const out = new Map<string, WFEdge[]>()
  const indeg = new Map<string, number>()
  for (const id of ids) {
    out.set(id, [])
    indeg.set(id, 0)
  }
  for (const e of edges) {
    if (!ids.has(e.source) || !ids.has(e.target)) continue
    out.get(e.source)!.push(e)
    indeg.set(e.target, (indeg.get(e.target) ?? 0) + 1)
  }
  const roots = [
    ...nodes.filter((n) => n.type === 'input'),
    ...nodes.filter((n) => n.type !== 'input' && !indeg.get(n.id)),
    ...nodes,
  ].map((n) => n.id)

  const back = new Set<string>()
  const state = new Map<string, 1 | 2>()
  for (const root of roots) {
    if (state.has(root)) continue
    const stack: { id: string; i: number }[] = [{ id: root, i: 0 }]
    state.set(root, 1)
    while (stack.length) {
      const top = stack[stack.length - 1]!
      const list = out.get(top.id)!
      if (top.i >= list.length) {
        state.set(top.id, 2)
        stack.pop()
        continue
      }
      const e = list[top.i++]!
      const s = state.get(e.target)
      if (s === 1) back.add(e.id)
      else if (!s) {
        state.set(e.target, 1)
        stack.push({ id: e.target, i: 0 })
      }
    }
  }
  return back
}

/** Layered left-to-right positions (top-left corners, snapped to the grid). */
export async function computeAutoLayout(
  nodes: WFNode[],
  edges: WFEdge[],
  sizes: (n: WFNode) => Size = estimateNodeSize,
): Promise<Map<string, Point>> {
  if (!nodes.length) return new Map()
  const back = findBackEdges(nodes, edges)
  const ids = new Set(nodes.map((n) => n.id))
  const result = await engine().layout({
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.spacing.nodeNode': '48',
      'elk.layered.spacing.nodeNodeBetweenLayers': '96',
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
      'elk.layered.crossingMinimization.forceNodeModelOrder': 'true',
      'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
      'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED',
      'elk.layered.nodePlacement.favorStraightEdges': 'true',
      'elk.separateConnectedComponents': 'false',
    },
    children: nodes.map((n) => ({ id: n.id, ...sizes(n) })),
    edges: edges
      .filter((e) => !back.has(e.id) && ids.has(e.source) && ids.has(e.target) && e.source !== e.target)
      .map((e) => ({ id: e.id, sources: [e.source], targets: [e.target] })),
  })
  const pos = new Map<string, Point>()
  for (const c of result.children || []) {
    pos.set(c.id, { x: snap(c.x ?? 0), y: snap(c.y ?? 0) })
  }
  return pos
}

export function prefersReducedMotion(): boolean {
  try {
    return typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

function easeOutCubic(t: number) {
  return 1 - Math.pow(1 - t, 3)
}

/** Tweens node positions from `from` to `to`, calling `apply` each frame. Resolves at the end. */
export function animatePositions(
  from: Map<string, Point>,
  to: Map<string, Point>,
  apply: (frame: Map<string, Point>) => void,
  duration = LAYOUT_DURATION_MS,
): Promise<void> {
  if (duration <= 0 || prefersReducedMotion() || typeof requestAnimationFrame === 'undefined') {
    apply(to)
    return Promise.resolve()
  }
  return new Promise((resolve) => {
    const start = performance.now()
    const step = (now: number) => {
      const k = easeOutCubic(Math.min(1, (now - start) / duration))
      const frame = new Map<string, Point>()
      for (const [id, end] of to) {
        const a = from.get(id) ?? end
        frame.set(id, { x: a.x + (end.x - a.x) * k, y: a.y + (end.y - a.y) * k })
      }
      apply(k >= 1 ? to : frame)
      if (k < 1) requestAnimationFrame(step)
      else resolve()
    }
    requestAnimationFrame(step)
  })
}

/** True when the graph has never been placed (new / template / imported without positions). */
export function needsInitialLayout(nodes: WFNode[]): boolean {
  if (nodes.length < 2) return false
  const seen = new Set<string>()
  for (const n of nodes) {
    const p = n.position
    if (!p || !Number.isFinite(p.x) || !Number.isFinite(p.y)) return true
    const key = `${Math.round(p.x)},${Math.round(p.y)}`
    if (seen.has(key)) return true
    seen.add(key)
  }
  return false
}

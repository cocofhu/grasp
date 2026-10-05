import type { WFEdge, WFNode } from '../shared/types'

const NODE_W = 176
const GAP_X = 36
const GAP_Y = 128
const NODE_H = 68

export function isInvalidPosition(pos: { x: number; y: number } | null | undefined): boolean {
  if (pos == null) return true
  if (typeof pos.x !== 'number' || typeof pos.y !== 'number') return true
  return pos.x === 0 && pos.y === 0
}

function addEdge(adj: Map<string, string[]>, inDeg: Map<string, number>, source: string, target: string) {
  if (source === target) return
  if (!adj.has(source) || !adj.has(target)) return
  adj.get(source)!.push(target)
  inDeg.set(target, (inDeg.get(target) ?? 0) + 1)
}

function buildAdjacency(nodes: WFNode[], edges: WFEdge[]) {
  const ids = new Set(nodes.map((n) => n.id))
  const adj = new Map<string, string[]>()
  const inDeg = new Map<string, number>()
  for (const id of ids) {
    adj.set(id, [])
    inDeg.set(id, 0)
  }

  for (const e of edges) addEdge(adj, inDeg, e.source, e.target)

  return { adj, inDeg, ids }
}

function assignLayers(nodes: WFNode[], adj: Map<string, string[]>, inDeg: Map<string, number>) {
  const layer = new Map<string, number>()
  const queue: string[] = []

  for (const n of nodes) {
    if (n.type === 'input' || (inDeg.get(n.id) ?? 0) === 0) queue.push(n.id)
  }
  if (queue.length === 0 && nodes[0]) queue.push(nodes[0].id)

  const seen = new Set<string>()
  while (queue.length) {
    const id = queue.shift()!
    if (seen.has(id)) continue
    seen.add(id)
    const l = layer.get(id) ?? 0
    for (const next of adj.get(id) ?? []) {
      layer.set(next, Math.max(layer.get(next) ?? 0, l + 1))
      queue.push(next)
    }
  }

  for (const n of nodes) {
    if (!layer.has(n.id)) layer.set(n.id, 0)
  }
  return layer
}

/** Session-only layout for nodes with invalid persisted position (missing or x,y both 0). */
export function computeSessionLayout(nodes: WFNode[], edges: WFEdge[]): Map<string, { x: number; y: number }> {
  const invalid = nodes.filter((n) => isInvalidPosition(n.position))
  if (invalid.length === 0) return new Map()

  const { adj, inDeg } = buildAdjacency(nodes, edges)
  const layer = assignLayers(nodes, adj, inDeg)

  const byLayer = new Map<number, string[]>()
  for (const n of invalid) {
    const l = layer.get(n.id) ?? 0
    if (!byLayer.has(l)) byLayer.set(l, [])
    byLayer.get(l)!.push(n.id)
  }

  const valid = nodes.filter((n) => !isInvalidPosition(n.position))
  let originX = 36
  let originY = 72
  if (valid.length > 0) {
    originY = Math.min(...valid.map((n) => n.position.y))
    originX = Math.max(...valid.map((n) => n.position.x + NODE_W)) + GAP_X
  }

  const result = new Map<string, { x: number; y: number }>()
  for (const l of [...byLayer.keys()].sort((a, b) => a - b)) {
    const ids = byLayer.get(l)!
    ids.forEach((id, row) => {
      result.set(id, {
        x: originX + l * (NODE_W + GAP_X),
        y: originY + row * (NODE_H + GAP_Y),
      })
    })
  }
  return result
}

import type { WFEdge, WFNode } from '@/lib/shared/types'
import type { GraphLike } from './useConnectionRules'
import { newEdgeId, newNodeId, snap } from './graphOps'

export const PASTE_OFFSET = 32
const STORAGE_KEY = 'grasp.canvas.clipboard'

export interface ClipboardPayload {
  nodes: WFNode[]
  edges: WFEdge[]
}

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

/** The selected nodes plus the edges running between them. */
export function extractSubgraph(graph: GraphLike, nodeIds: Iterable<string>): ClipboardPayload {
  const ids = new Set(nodeIds)
  return clone({
    nodes: graph.nodes.filter((n) => ids.has(n.id)),
    edges: graph.edges.filter((e) => ids.has(e.source) && ids.has(e.target)),
  })
}

/**
 * Adds a copy of `payload` to `graph` with fresh ids. Positions are shifted by
 * `offset`, or moved so the copy's top-left lands on `at`. A copied input node is
 * dropped when the graph already has one. Returns the new node ids.
 */
export function pasteSubgraph(
  graph: GraphLike,
  payload: ClipboardPayload,
  opts: { offset?: number; at?: { x: number; y: number } } = {},
): string[] {
  const hasInput = graph.nodes.some((n) => n.type === 'input')
  const nodes = payload.nodes.filter((n) => !(hasInput && n.type === 'input'))
  if (!nodes.length) return []
  const minX = Math.min(...nodes.map((n) => n.position?.x ?? 0))
  const minY = Math.min(...nodes.map((n) => n.position?.y ?? 0))
  const dx = opts.at ? opts.at.x - minX : (opts.offset ?? PASTE_OFFSET)
  const dy = opts.at ? opts.at.y - minY : (opts.offset ?? PASTE_OFFSET)
  const idMap = new Map<string, string>()
  const taken = new Set(graph.nodes.map((n) => n.id))
  const added: WFNode[] = []
  for (const n of nodes) {
    const id = newNodeId(n.type, taken)
    taken.add(id)
    idMap.set(n.id, id)
    added.push({
      ...clone(n),
      id,
      position: { x: snap((n.position?.x ?? 0) + dx), y: snap((n.position?.y ?? 0) + dy) },
    })
  }
  const edgeIds = new Set(graph.edges.map((e) => e.id))
  const edges: WFEdge[] = []
  for (const e of payload.edges) {
    const source = idMap.get(e.source)
    const target = idMap.get(e.target)
    if (!source || !target) continue
    const id = newEdgeId(edgeIds)
    edgeIds.add(id)
    edges.push({ ...clone(e), id, source, target })
  }
  graph.nodes.push(...added)
  graph.edges.push(...edges)
  return added.map((n) => n.id)
}

/** Copy / paste / duplicate. The clipboard survives reloads and is shared across editor tabs. */
export function useCanvasClipboard(graph: () => GraphLike, storage: Storage | null = safeStorage()) {
  let memory: ClipboardPayload | null = null
  let pasteCount = 0

  function read(): ClipboardPayload | null {
    if (storage) {
      try {
        const raw = storage.getItem(STORAGE_KEY)
        if (raw) return JSON.parse(raw) as ClipboardPayload
      } catch {
        /* fall back to memory */
      }
    }
    return memory
  }

  function copy(nodeIds: string[]): boolean {
    if (!nodeIds.length) return false
    const payload = extractSubgraph(graph(), nodeIds)
    if (!payload.nodes.length) return false
    memory = payload
    pasteCount = 0
    try {
      storage?.setItem(STORAGE_KEY, JSON.stringify(payload))
    } catch {
      /* quota: memory copy still works in this tab */
    }
    return true
  }

  function paste(at?: { x: number; y: number }): string[] {
    const payload = read()
    if (!payload?.nodes.length) return []
    pasteCount++
    return pasteSubgraph(graph(), payload, at ? { at } : { offset: PASTE_OFFSET * pasteCount })
  }

  function duplicate(nodeIds: string[]): string[] {
    if (!nodeIds.length) return []
    return pasteSubgraph(graph(), extractSubgraph(graph(), nodeIds), { offset: PASTE_OFFSET })
  }

  return { copy, paste, duplicate, hasContent: () => !!read()?.nodes.length }
}

function safeStorage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

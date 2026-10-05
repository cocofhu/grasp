import { describe, expect, it } from 'vitest'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { extractSubgraph, PASTE_OFFSET, pasteSubgraph, useCanvasClipboard } from './useCanvasClipboard'

function memoryStorage(): Storage {
  const m = new Map<string, string>()
  return {
    get length() {
      return m.size
    },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, v),
  }
}

function graph() {
  return {
    nodes: [
      { id: 'in', type: 'input', label: 'In', position: { x: 0, y: 0 }, config: {} },
      { id: 'a', type: 'agent', label: 'A', position: { x: 200, y: 0 }, config: { agent_profile: 'x', prompt: 'p' } },
      { id: 'b', type: 'agent', label: 'B', position: { x: 480, y: 80 }, config: { agent_profile: 'y', prompt: '' } },
    ] as WFNode[],
    edges: [
      { id: 'e1', source: 'in', target: 'a' },
      { id: 'e2', source: 'a', target: 'b', when: 'ok' },
    ] as WFEdge[],
  }
}

describe('canvas clipboard', () => {
  it('extracts the selected nodes and only the edges between them', () => {
    const sub = extractSubgraph(graph(), ['a', 'b'])
    expect(sub.nodes.map((n) => n.id)).toEqual(['a', 'b'])
    expect(sub.edges.map((e) => e.id)).toEqual(['e2'])
  })

  it('pastes with fresh ids, remapped internal edges and an offset', () => {
    const g = graph()
    const ids = pasteSubgraph(g, extractSubgraph(g, ['a', 'b']))
    expect(ids).toHaveLength(2)
    expect(ids.every((id) => !['a', 'b'].includes(id))).toBe(true)
    const [na, nb] = ids.map((id) => g.nodes.find((n) => n.id === id)!)
    expect(na!.position).toEqual({ x: 200 + PASTE_OFFSET, y: PASTE_OFFSET })
    expect(nb!.config).toEqual({ agent_profile: 'y', prompt: '' })
    const copied = g.edges.at(-1)!
    expect(copied).toMatchObject({ source: na!.id, target: nb!.id, when: 'ok' })
    expect(copied.id).not.toBe('e2')
  })

  it('moves the copy so its top-left lands on the paste point', () => {
    const g = graph()
    const [id] = pasteSubgraph(g, extractSubgraph(g, ['a', 'b']), { at: { x: 1000, y: 1000 } })
    expect(g.nodes.find((n) => n.id === id)!.position).toEqual({ x: 1000, y: 1000 })
  })

  it('drops a copied input node when the graph already has one', () => {
    const g = graph()
    const ids = pasteSubgraph(g, extractSubgraph(g, ['in', 'a']))
    expect(ids).toHaveLength(1)
    expect(g.nodes.filter((n) => n.type === 'input')).toHaveLength(1)
    expect(g.edges).toHaveLength(2)
  })

  it('copy / paste goes through storage and cascades the offset', () => {
    const storage = memoryStorage()
    const g = graph()
    const clip = useCanvasClipboard(() => g, storage)
    expect(clip.hasContent()).toBe(false)
    expect(clip.copy([])).toBe(false)
    expect(clip.copy(['a'])).toBe(true)
    expect(clip.hasContent()).toBe(true)

    const other = graph()
    const fromOtherTab = useCanvasClipboard(() => other, storage)
    const [p1] = fromOtherTab.paste()
    const [p2] = fromOtherTab.paste()
    const pos = (id: string) => other.nodes.find((n) => n.id === id)!.position
    expect(pos(p1!)).toEqual({ x: 200 + PASTE_OFFSET, y: PASTE_OFFSET })
    expect(pos(p2!)).toEqual({ x: 200 + PASTE_OFFSET * 2, y: PASTE_OFFSET * 2 })
  })

  it('falls back to memory without storage and duplicates in place', () => {
    const g = graph()
    const clip = useCanvasClipboard(() => g, null)
    clip.copy(['b'])
    expect(clip.paste({ x: 16, y: 16 })).toHaveLength(1)
    expect(clip.duplicate(['a', 'b'])).toHaveLength(2)
    expect(clip.duplicate([])).toEqual([])
    expect(g.nodes).toHaveLength(6)
  })
})

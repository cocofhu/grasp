import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, reactive } from 'vue'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { COALESCE_MS, HISTORY_LIMIT, useCanvasHistory } from './useCanvasHistory'

function graph() {
  return reactive({
    nodes: [{ id: 'a', type: 'agent', label: 'A', position: { x: 0, y: 0 }, config: { prompt: '' } }] as WFNode[],
    edges: [] as WFEdge[],
  })
}

describe('useCanvasHistory', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('records a discrete commit as one undoable step', async () => {
    const g = graph()
    const h = useCanvasHistory(g)
    g.nodes.push({ id: 'b', type: 'output', label: 'B', position: { x: 100, y: 0 }, config: {} })
    h.commit()
    expect(h.size.value).toBe(1)
    expect(h.undo()).toBe(true)
    expect(g.nodes.map((n) => n.id)).toEqual(['a'])
    expect(h.canRedo.value).toBe(true)
    expect(h.redo()).toBe(true)
    expect(g.nodes.map((n) => n.id)).toEqual(['a', 'b'])
    h.stop()
  })

  it('keeps at most 100 steps', async () => {
    const g = graph()
    const h = useCanvasHistory(g)
    for (let i = 1; i <= HISTORY_LIMIT + 20; i++) {
      g.nodes[0]!.position = { x: i, y: 0 }
      h.commit()
    }
    expect(h.size.value).toBe(HISTORY_LIMIT)
    let undone = 0
    while (h.undo()) undone++
    expect(undone).toBe(HISTORY_LIMIT)
    expect(g.nodes[0]!.position).toEqual({ x: 20, y: 0 })
    h.stop()
  })

  it('records a whole drag as a single step on end()', async () => {
    const g = graph()
    const h = useCanvasHistory(g)
    h.begin()
    for (let i = 1; i <= 30; i++) {
      g.nodes[0]!.position = { x: i * 8, y: i * 8 }
      await nextTick()
    }
    vi.advanceTimersByTime(COALESCE_MS * 4)
    expect(h.size.value).toBe(0)
    h.end()
    expect(h.size.value).toBe(1)
    h.undo()
    expect(g.nodes[0]!.position).toEqual({ x: 0, y: 0 })
    h.stop()
  })

  it('coalesces typing into one step after a 500ms pause', async () => {
    const g = graph()
    const h = useCanvasHistory(g)
    for (const text of ['h', 'he', 'hel', 'hell', 'hello']) {
      g.nodes[0]!.config.prompt = text
      await nextTick()
      vi.advanceTimersByTime(COALESCE_MS - 100)
    }
    expect(h.size.value).toBe(0)
    vi.advanceTimersByTime(100)
    expect(h.size.value).toBe(1)

    g.nodes[0]!.config.prompt = 'hello world'
    await nextTick()
    vi.advanceTimersByTime(COALESCE_MS)
    expect(h.size.value).toBe(2)

    h.undo()
    expect(g.nodes[0]!.config.prompt).toBe('hello')
    h.undo()
    expect(g.nodes[0]!.config.prompt).toBe('')
    h.stop()
  })

  it('undo flushes pending typing first so nothing is lost', async () => {
    const g = graph()
    const h = useCanvasHistory(g)
    g.nodes[0]!.config.prompt = 'draft'
    await nextTick()
    expect(h.undo()).toBe(true)
    expect(g.nodes[0]!.config.prompt).toBe('')
    expect(h.redo()).toBe(true)
    expect(g.nodes[0]!.config.prompt).toBe('draft')
    h.stop()
  })

  it('does not record the undo itself and drops redo after a new edit', async () => {
    const g = graph()
    const h = useCanvasHistory(g)
    g.nodes[0]!.label = 'B'
    h.commit()
    h.undo()
    await nextTick()
    await Promise.resolve()
    vi.advanceTimersByTime(COALESCE_MS * 2)
    expect(h.canRedo.value).toBe(true)
    g.nodes[0]!.label = 'C'
    h.commit()
    expect(h.canRedo.value).toBe(false)
    expect(h.size.value).toBe(1)
    h.stop()
  })

  it('reset makes the current graph the only step', () => {
    const g = graph()
    const h = useCanvasHistory(g)
    g.nodes[0]!.label = 'X'
    h.commit()
    h.reset()
    expect(h.canUndo.value).toBe(false)
    expect(h.undo()).toBe(false)
    h.stop()
  })
})

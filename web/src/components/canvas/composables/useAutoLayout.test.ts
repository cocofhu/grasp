import { describe, expect, it, vi } from 'vitest'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import {
  AGENT_NODE_WIDTH,
  animatePositions,
  computeAutoLayout,
  estimateNodeSize,
  findBackEdges,
  needsInitialLayout,
  NODE_WIDTH,
} from './useAutoLayout'

const node = (id: string, type: WFNode['type'], config: Record<string, unknown> = {}): WFNode => ({
  id,
  type,
  label: id,
  position: { x: 0, y: 0 },
  config,
})

function defaultFlow() {
  const nodes = [
    node('in', 'input'),
    node('clarify', 'agent'),
    node('impl', 'agent'),
    node('test', 'agent'),
    node('out', 'output'),
  ]
  const edges: WFEdge[] = [
    { id: 'e1', source: 'in', target: 'clarify' },
    { id: 'e2', source: 'clarify', target: 'impl' },
    { id: 'e3', source: 'impl', target: 'test' },
    { id: 'e4', source: 'test', sourceHandle: 'pass', target: 'out' },
    { id: 'e5', source: 'test', sourceHandle: 'fail', target: 'impl' },
  ]
  return { nodes, edges }
}

describe('auto layout', () => {
  it('finds the fail → implement loop as the only back edge', () => {
    const { nodes, edges } = defaultFlow()
    expect([...findBackEdges(nodes, edges)]).toEqual(['e5'])
  })

  it('finds no back edges in an acyclic graph and ignores dangling edges', () => {
    const { nodes, edges } = defaultFlow()
    const dag = edges.filter((e) => e.id !== 'e5').concat({ id: 'x', source: 'in', target: 'ghost' })
    expect(findBackEdges(nodes, dag).size).toBe(0)
  })

  it('lays the main path out left to right despite the loop', async () => {
    const { nodes, edges } = defaultFlow()
    const pos = await computeAutoLayout(nodes, edges)
    const xs = ['in', 'clarify', 'impl', 'test', 'out'].map((id) => pos.get(id)!.x)
    for (let i = 1; i < xs.length; i++) expect(xs[i]!).toBeGreaterThan(xs[i - 1]!)
    for (const p of pos.values()) {
      expect(p.x % 8).toBe(0)
      expect(p.y % 8).toBe(0)
    }
  })

  it('returns an empty map for an empty graph', async () => {
    expect((await computeAutoLayout([], [])).size).toBe(0)
  })

  it('estimates sizes per node kind', () => {
    expect(estimateNodeSize(node('a', 'agent')).width).toBe(AGENT_NODE_WIDTH)
    const branch = estimateNodeSize(node('b', 'branch', { cases: [{ id: 'c1' }, { id: 'c2' }] }))
    expect(branch.width).toBe(NODE_WIDTH)
    expect(branch.height).toBeGreaterThan(estimateNodeSize(node('s', 'set_var')).height)
  })

  it('detects graphs that were never placed', () => {
    expect(needsInitialLayout([node('a', 'agent')])).toBe(false)
    expect(needsInitialLayout([node('a', 'agent'), node('b', 'agent')])).toBe(true)
    expect(needsInitialLayout([{ ...node('a', 'agent'), position: undefined as any }, node('b', 'output')])).toBe(true)
    const placed = [node('a', 'agent'), { ...node('b', 'output'), position: { x: 300, y: 0 } }]
    expect(needsInitialLayout(placed)).toBe(false)
  })

  it('applies the final positions directly without animation frames', async () => {
    const apply = vi.fn()
    const to = new Map([['a', { x: 10, y: 20 }]])
    await animatePositions(new Map([['a', { x: 0, y: 0 }]]), to, apply, 0)
    expect(apply).toHaveBeenCalledWith(to)
  })
})

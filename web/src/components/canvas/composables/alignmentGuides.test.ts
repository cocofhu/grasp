import { describe, expect, it } from 'vitest'
import { alignmentGuides } from './alignmentGuides'

const rect = (id: string, x: number, y: number, width = 200, height = 80) => ({ id, x, y, width, height })

describe('alignmentGuides', () => {
  it('reports matching edges and centers within the tolerance', () => {
    const g = alignmentGuides(rect('a', 302, 0), [rect('b', 300, 400), rect('c', 1000, 42, 200, 0)])
    expect(g.vertical).toEqual([300, 400, 500])
    expect(g.horizontal).toEqual([42])
  })

  it('ignores the dragged node itself and far-away nodes', () => {
    const g = alignmentGuides(rect('a', 0, 0), [rect('a', 0, 0), rect('b', 600, 600)])
    expect(g).toEqual({ vertical: [], horizontal: [] })
  })

  it('aligns centers of differently sized nodes', () => {
    const g = alignmentGuides(rect('a', 100, 0, 200, 80), [rect('b', 150, 300, 100, 40)])
    expect(g.vertical).toEqual([200])
  })
})

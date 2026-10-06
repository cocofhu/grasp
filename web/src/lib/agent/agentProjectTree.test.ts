import { describe, expect, it } from 'vitest'
import { toProjectTreeNodes } from './agentProjectTree'

describe('toProjectTreeNodes', () => {
  it('groups agents under their project in project order with sorted children', () => {
    const nodes = toProjectTreeNodes(
      [
        { name: 'zeta', projectId: 'p1' },
        { name: 'alpha', projectId: 'p1' },
        { name: 'beta', projectId: 'p2' },
      ],
      [
        { id: 'p2', name: 'Second' },
        { id: 'p1', name: 'First' },
      ],
    )
    expect(nodes).toEqual([
      { id: 'p2', label: 'Second', count: 1, children: [{ id: 'beta', label: 'beta', icon: 'robot' }] },
      {
        id: 'p1',
        label: 'First',
        count: 2,
        children: [
          { id: 'alpha', label: 'alpha', icon: 'robot' },
          { id: 'zeta', label: 'zeta', icon: 'robot' },
        ],
      },
    ])
  })

  it('keeps empty projects and omits agents whose project is not listed', () => {
    const nodes = toProjectTreeNodes(
      [{ name: 'orphan', projectId: 'gone' }],
      [{ id: 'p1', name: 'Empty' }],
    )
    expect(nodes).toEqual([{ id: 'p1', label: 'Empty', count: 0, children: [] }])
  })
})

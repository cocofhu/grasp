import { describe, expect, it } from 'vitest'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { checkAddNode, checkConnection, useConnectionRules } from './useConnectionRules'

function graph(edges: WFEdge[] = []) {
  return {
    nodes: [
      { id: 'in', type: 'input', label: 'In', config: {} },
      { id: 'a', type: 'agent', label: 'A', config: {} },
      { id: 'b', type: 'agent', label: 'B', config: {} },
      { id: 'out', type: 'output', label: 'Out', config: {} },
    ] as WFNode[],
    edges,
  }
}

const reason = (r: ReturnType<typeof checkConnection>) => (r.ok ? '' : r.reason)

describe('connection rules', () => {
  it('accepts a plain connection', () => {
    expect(checkConnection(graph(), { source: 'in', target: 'a' })).toEqual({ ok: true })
  })

  it('rejects missing ends, self loops and unknown nodes', () => {
    expect(reason(checkConnection(graph(), { source: '', target: 'a' }))).toBe('canvas.rules.missingEnd')
    expect(reason(checkConnection(graph(), { source: 'a', target: 'a' }))).toBe('canvas.rules.selfLoop')
    expect(reason(checkConnection(graph(), { source: 'a', target: 'ghost' }))).toBe('canvas.rules.missingEnd')
  })

  it('rejects edges into input and out of output', () => {
    expect(reason(checkConnection(graph(), { source: 'a', target: 'in' }))).toBe('canvas.rules.inputNoIncoming')
    expect(reason(checkConnection(graph(), { source: 'out', target: 'a' }))).toBe('canvas.rules.outputNoOutgoing')
  })

  it('rejects duplicates and a second unguarded edge on the same outlet', () => {
    const g = graph([{ id: 'e1', source: 'a', target: 'b' }])
    expect(reason(checkConnection(g, { source: 'a', target: 'b', sourceHandle: null }))).toBe('canvas.rules.duplicate')
    expect(reason(checkConnection(g, { source: 'a', target: 'out' }))).toBe('canvas.rules.outletTaken')
    expect(checkConnection(g, { source: 'a', sourceHandle: 'fail', target: 'out' }).ok).toBe(true)
  })

  it('allows another edge when the existing one is guarded or a rollback', () => {
    expect(checkConnection(graph([{ id: 'e1', source: 'a', target: 'b', when: 'x > 1' }]), { source: 'a', target: 'out' }).ok).toBe(true)
    expect(checkConnection(graph([{ id: 'e1', source: 'a', target: 'b', kind: 'rollback' }]), { source: 'a', target: 'out' }).ok).toBe(true)
  })

  it('allows only one input node', () => {
    expect(reason(checkAddNode(graph(), 'input'))).toBe('canvas.rules.singleInput')
    expect(checkAddNode(graph(), 'agent').ok).toBe(true)
    const rules = useConnectionRules(() => ({ nodes: [], edges: [] }))
    expect(rules.canAdd('input').ok).toBe(true)
    expect(rules.check({ source: 'x', target: 'y' }).ok).toBe(false)
  })
})

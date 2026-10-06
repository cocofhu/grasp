import { describe, expect, it } from 'vitest'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { workflowGraphError } from './graphValidation'

const t = (key: string, p?: Record<string, unknown>) => (p ? `${key}:${JSON.stringify(p)}` : key)

function graph(agentConfig: Record<string, unknown>) {
  const nodes: WFNode[] = [
    { id: 'in', type: 'input', label: '输入', config: {} },
    { id: 'a', type: 'agent', label: '实现', config: agentConfig },
    { id: 'out', type: 'output', label: '输出', config: {} },
  ]
  const edges: WFEdge[] = [
    { id: 'e1', source: 'in', target: 'a' },
    { id: 'e2', source: 'a', target: 'out' },
  ]
  return { nodes, edges }
}

describe('workflowGraphError', () => {
  it('rejects an Agent node without agent_profile even before Agents are loaded', () => {
    expect(workflowGraphError(graph({ agent_profile: '  ', prompt: 'x' }), { t })).toContain('agentMissingProfile')
    expect(workflowGraphError(graph({ prompt: 'x' }), { t, agents: {} })).toContain('agentMissingProfile')
  })

  it('accepts a profiled Agent when Agent checks are skipped', () => {
    expect(workflowGraphError(graph({ agent_profile: '实现', prompt: 'x' }), { t })).toBe('')
  })

  it('reports an unknown profile when Agents are loaded', () => {
    expect(workflowGraphError(graph({ agent_profile: '实现' }), { t, agents: {} })).toContain('agentNotFound')
  })
})

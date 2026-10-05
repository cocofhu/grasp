import { describe, expect, it } from 'vitest'
import { resolveNodeDisplayLabel, resolveNodeDisplayLabelFromNode } from './resolveNodeDisplayLabel'

const t = (key: string) => {
  const map: Record<string, string> = {
    'nodes.agent.label': 'Agent',
    'nodes.branch.label': '分支',
  }
  return map[key] ?? key
}

describe('resolveNodeDisplayLabel', () => {
  it('translates when label equals the raw registry key', () => {
    expect(resolveNodeDisplayLabel('nodes.agent.label', 'agent', t)).toBe('Agent')
  })

  it('returns custom labels unchanged', () => {
    expect(resolveNodeDisplayLabel('测试节点', 'agent', t)).toBe('测试节点')
  })

  it('falls back to nodeId when label is empty', () => {
    expect(resolveNodeDisplayLabel('', 'agent', t, { nodeId: 'agent_abc' })).toBe('agent_abc')
  })

  it('falls back to translated type name when label is empty and no nodeId', () => {
    expect(resolveNodeDisplayLabel(undefined, 'branch', t, { typeLabel: '分支' })).toBe('分支')
  })
})

describe('resolveNodeDisplayLabelFromNode', () => {
  it('delegates to resolveNodeDisplayLabel with node fields', () => {
    const node = { id: 'agent_1', type: 'agent' as const, label: 'nodes.agent.label' }
    expect(resolveNodeDisplayLabelFromNode(node, t)).toBe('Agent')
  })
})

import { describe, expect, it } from 'vitest'
import {
  buildOutputSourceOptions,
  labelForOutputTemplate,
  upstreamNodeIds,
} from './outputSourceOptions'
import type { WFEdge, WFNode } from '../shared/types'
import type { AgentCapabilities } from '@/lib/api/apiTypes'
import { AUTO_CAPS, CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS, writesCaps } from '@/test/capsFixtures'

const t = (key: string, params?: Record<string, unknown>) =>
  params?.value != null ? `${key}:${params.value}` : key

function node(id: string, type: string, label: string, config: Record<string, unknown> = {}): WFNode {
  return { id, type: type as never, label, position: { x: 0, y: 0 }, config } as WFNode
}

function agent(id: string, label: string, caps: AgentCapabilities, config: Record<string, unknown> = {}): WFNode {
  return { ...node(id, 'agent', label, config), caps } as WFNode
}

describe('outputSourceOptions', () => {
  const nodes: WFNode[] = [
    node('in', 'input', '输入'),
    agent('plan', '计划', writesCaps('plan')),
    agent('agent', '实现', AUTO_CAPS, { produces: 'out.md' }),
    node('gate', 'human_gate', '门禁'),
  ]
  const edges: WFEdge[] = [
    { id: 'e1', source: 'in', target: 'plan' },
    { id: 'e2', source: 'plan', target: 'agent' },
    { id: 'e3', source: 'agent', target: 'gate' },
  ]

  it('walks transitive upstream ids', () => {
    expect([...upstreamNodeIds('gate', edges)].sort()).toEqual(['agent', 'in', 'plan'])
    expect(upstreamNodeIds('in', edges).size).toBe(0)
  })

  it('builds structured/agent/artifact options and resolves labels', () => {
    const opts = buildOutputSourceOptions(nodes, edges, 'gate', t)
    expect(opts.some((o) => o.value.includes('nodes.plan.outputs.plan'))).toBe(true)
    expect(opts.some((o) => o.value.includes('artifact("out.md")'))).toBe(true)
    expect(opts.some((o) => o.value.includes('nodes.agent.outputs.content'))).toBe(true)

    const hit = opts[0]
    expect(labelForOutputTemplate(hit.value, nodes, edges, 'gate', t)).toBe(hit.label)
    expect(labelForOutputTemplate('{{custom}}', nodes, edges, 'gate', t)).toContain(
      'common.gateBodyLabels.custom',
    )
  })

  it('includes upstream via pass / fail outlets of a gated agent', () => {
    const graph = [
      agent('implement', '实现', IMPLEMENT_CAPS),
      agent('test', '测试评审', TEST_REVIEW_CAPS),
      node('output', 'output', '输出'),
    ]
    const realEdges: WFEdge[] = [
      { id: 'e1', source: 'implement', target: 'test' },
      { id: 'e2', source: 'test', target: 'output', sourceHandle: 'pass' },
      { id: 'e3', source: 'test', target: 'implement', sourceHandle: 'fail' },
    ]
    const upstream = upstreamNodeIds('output', realEdges)
    expect(upstream.has('test')).toBe(true)
    expect(upstream.has('implement')).toBe(true)

    const opts = buildOutputSourceOptions(graph, realEdges, 'output', t)
    expect(opts.some((o) => o.value.includes('nodes.implement.outputs.implementation_result'))).toBe(true)
    expect(opts.some((o) => o.value.includes('nodes.test.outputs.test_result'))).toBe(true)
    expect(opts.some((o) => o.value.includes('nodes.test.outputs.review'))).toBe(true)
  })

  it('includes upstream via human_gate action and branch case outlets', () => {
    const graph = [
      agent('research', '调研', writesCaps('research')),
      node('gate', 'human_gate', '门禁', { actions: [{ id: 'approve', label: '通过' }] }),
      node('branch', 'branch', '分支', { cases: [{ id: 'ok', when: 'true' }] }),
      node('output', 'output', '输出'),
    ]
    const realEdges: WFEdge[] = [
      { id: 'e1', source: 'research', target: 'gate' },
      { id: 'e2', source: 'gate', target: 'branch', sourceHandle: 'approve' },
      { id: 'e3', source: 'branch', target: 'output', sourceHandle: 'ok' },
    ]
    const opts = buildOutputSourceOptions(graph, realEdges, 'output', t)
    expect(opts.some((o) => o.value.includes('nodes.research.outputs.research'))).toBe(true)
  })

  it('dedupes option templates across parallel paths', () => {
    const graph = [
      agent('implement', '实现', IMPLEMENT_CAPS),
      agent('test', '测试评审', TEST_REVIEW_CAPS),
      node('output', 'output', '输出'),
    ]
    const realEdges: WFEdge[] = [
      { id: 'e1', source: 'implement', target: 'test' },
      { id: 'e2', source: 'implement', target: 'output' },
      { id: 'e3', source: 'test', target: 'output', sourceHandle: 'pass' },
    ]
    const opts = buildOutputSourceOptions(graph, realEdges, 'output', t)
    const implValues = opts.filter((o) =>
      o.value.includes('nodes.implement.outputs.implementation_result'),
    )
    expect(implValues).toHaveLength(1)
  })

  it('derives clarify multi-product options from declared writes', () => {
    const graph = [agent('clarify', '需求澄清', CLARIFY_CAPS), node('output', 'output', '输出')]
    const realEdges: WFEdge[] = [{ id: 'e1', source: 'clarify', target: 'output' }]
    const opts = buildOutputSourceOptions(graph, realEdges, 'output', t)
    for (const key of ['clarified_requirement', 'plan', 'research', 'proposals', 'page']) {
      expect(opts.some((o) => o.value === `{{nodes.clarify.outputs.${key}}}`)).toBe(true)
    }
  })

  it('offers nothing structured for an agent without caps', () => {
    const graph = [node('a', 'agent', 'A'), node('output', 'output', '输出')]
    const opts = buildOutputSourceOptions(graph, [{ id: 'e1', source: 'a', target: 'output' }], 'output', t)
    expect(opts.map((o) => o.value)).toEqual(['{{nodes.a.outputs.content}}'])
  })
})

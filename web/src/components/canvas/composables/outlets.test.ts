import { describe, expect, it } from 'vitest'
import type { WFNode } from '@/lib/shared/types'
import { agentLookup, capabilityFlags, capabilitySummary, findAgent, nodeOutlets, normHandle, type CanvasAgent } from './outlets'

const t = (key: string, named?: Record<string, unknown>) => (named ? `${key}(${JSON.stringify(named)})` : key)

const agents: CanvasAgent[] = [
  {
    name: 'tester',
    capabilities: { interaction: 'auto', review: true, tools: ['set_preview'], writes: [{ schema: 'test_result', required: true }] },
  },
  { name: 'helper', capabilities: { interaction: 'auto', writes: [{ schema: 'implementation_result' }] } },
  { name: 'clarifier', capabilities: { interaction: 'clarify', tools: ['ask_question'] } },
]

const node = (type: WFNode['type'], config: Record<string, unknown>): WFNode => ({ id: 'n', type, label: 'n', config })

describe('canvas outlets', () => {
  it('gives a verdict agent toned pass / fail outlets', () => {
    const out = nodeOutlets(node('agent', { agent_profile: 'tester' }), agentLookup(agents), t)
    expect(out).toEqual([
      { id: 'pass', label: 'nodes.outlets.pass', tone: 'ok' },
      { id: 'fail', label: 'nodes.outlets.fail', tone: 'err' },
    ])
  })

  it('gives a plain agent and unknown agents a single unlabeled outlet', () => {
    const lookup = agentLookup(agents)
    expect(nodeOutlets(node('agent', { agent_profile: 'helper' }), lookup, t)).toEqual([{ id: '', label: '', tone: 'default' }])
    expect(nodeOutlets(node('agent', { agent_profile: 'ghost' }), lookup, t)).toEqual([{ id: '', label: '', tone: 'default' }])
  })

  it('gives branch one outlet per case plus else, and human_gate one per action', () => {
    const lookup = agentLookup(agents)
    const branch = nodeOutlets(node('branch', { cases: [{ id: 'c1', when: 'x > 1' }, { id: 'c2', when: '' }] }), lookup, t)
    expect(branch.map((o) => [o.id, o.label])).toEqual([
      ['c1', 'x > 1'],
      ['c2', 'c2'],
      ['else', 'nodes.outlets.else'],
    ])
    const gate = nodeOutlets(node('human_gate', { actions: [{ id: 'approve', label: '通过' }, { id: 'reject' }] }), lookup, t)
    expect(gate.map((o) => [o.id, o.label])).toEqual([
      ['approve', '通过'],
      ['reject', 'reject'],
    ])
  })

  it('normalizes handles and finds agents by trimmed name', () => {
    expect(normHandle(null)).toBe('')
    expect(normHandle(undefined)).toBe('')
    expect(normHandle('pass')).toBe('pass')
    expect(findAgent(agents, ' tester ')?.name).toBe('tester')
    expect(findAgent(agents, '')).toBeUndefined()
  })

  it('derives capability icons and a one-line summary', () => {
    expect(capabilityFlags(agents[0]!.capabilities)).toEqual({ ask: false, preview: true, review: true, gate: true })
    expect(capabilityFlags(agents[2]!.capabilities).ask).toBe(true)
    expect(capabilityFlags(null)).toEqual({ ask: false, preview: false, review: false, gate: false })
    expect(capabilitySummary(null, t)).toBe('canvas.caps.undeclared')
    const s = capabilitySummary(agents[0]!.capabilities, t)
    expect(s).toContain('nodes.capabilities.interaction.auto')
    expect(s).toContain('canvas.caps.review')
    expect(s).toContain('canvas.caps.preview')
  })
})

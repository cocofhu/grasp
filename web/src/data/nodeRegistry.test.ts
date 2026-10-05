import { describe, expect, it } from 'vitest'
import manifest from './nodeManifest.generated.json'
import {
  NODE_DEFS,
  PALETTE_GROUPS,
  agentOutputDefs,
  defaultHumanGateForm,
  isPageHtmlGateBody,
  nodeColor,
  syncHumanGateFormDefaults,
} from './nodeRegistry'
import zhNodes from '@/locales/zh-CN/nodes.json'
import enNodes from '@/locales/en/nodes.json'
import { CLARIFY_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'

function lookup(obj: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((o, k) => (o && typeof o === 'object' ? (o as Record<string, unknown>)[k] : undefined), obj)
}

describe('nodeRegistry', () => {
  it('defines exactly the manifest node types', () => {
    expect(Object.keys(NODE_DEFS).sort()).toEqual(manifest.nodeTypes.map((n) => n.type).sort())
    expect(PALETTE_GROUPS.flatMap((g) => g.types).sort()).toEqual(Object.keys(NODE_DEFS).sort())
  })

  it('configures the agent node with profile, goal and timeout only', () => {
    expect(NODE_DEFS.agent.fields.map((f) => f.key)).toEqual(['agent_profile', 'prompt', 'timeout'])
    expect(NODE_DEFS.agent.defaults).toEqual({ agent_profile: '', prompt: '' })
  })

  it('branch cases carry ids for their outlets', () => {
    const cases = NODE_DEFS.branch.defaults.cases as { id: string; when: string }[]
    expect(cases.every((c) => c.id && !('goto' in c))).toBe(true)
  })

  it('derives agent outputs from declared products', () => {
    const keys = agentOutputDefs(CLARIFY_CAPS).map((o) => o.key)
    expect(keys.slice(0, 4)).toEqual(['clarified_requirement', 'clarified_requirement_json', 'plan', 'plan_json'])
    expect(keys).toContain('page')
    expect(keys).not.toContain('page_json')
    expect(keys).toContain('content')
    expect(agentOutputDefs(TEST_REVIEW_CAPS).map((o) => o.key).slice(0, 4)).toEqual([
      'test_result',
      'test_result_json',
      'review',
      'review_json',
    ])
    expect(agentOutputDefs(undefined)).toEqual(NODE_DEFS.agent.outputs)
  })

  it('every i18n key referenced by node defs exists in both locales', () => {
    const keys = new Set<string>()
    for (const def of Object.values(NODE_DEFS)) {
      for (const k of [def.label, def.desc, def.category, def.help]) if (k) keys.add(k)
      for (const f of def.fields) for (const k of [f.label, f.help, f.placeholder]) if (k) keys.add(k)
      for (const o of def.outputs) keys.add(o.desc)
    }
    for (const o of agentOutputDefs(CLARIFY_CAPS)) keys.add(o.desc)
    for (const k of keys) {
      expect(typeof lookup(zhNodes, k), k).toBe('string')
      expect(typeof lookup(enNodes, k), k).toBe('string')
    }
  })

  it('human_gate page.html body selects an empty form', () => {
    expect(isPageHtmlGateBody('{{nodes.clarify.outputs.page}}')).toBe(true)
    expect(defaultHumanGateForm('{{nodes.clarify.outputs.page}}')).toEqual([])
    expect(defaultHumanGateForm('{{nodes.clarify.outputs.research}}')).toEqual([
      { key: 'comment', label: '评审意见', required: false },
    ])
    const cfg: Record<string, unknown> = { body_template: 'page.html' }
    syncHumanGateFormDefaults(cfg)
    expect(cfg.form).toEqual([])
    const untouched: Record<string, unknown> = {}
    syncHumanGateFormDefaults(untouched)
    expect(untouched.form).toBeUndefined()
  })

  it('colors unknown types like an agent', () => {
    expect(nodeColor('agent')).toBe(nodeColor('nope'))
    expect(nodeColor('input')).not.toBe(nodeColor('agent'))
    expect(nodeColor('agent', 0.13)).toBe('rgb(var(--c-hue-1) / 0.13)')
  })
})

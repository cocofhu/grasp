// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import nodes from '@/locales/zh-CN/nodes.json'
import pages from '@/locales/zh-CN/pages.json'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { CLARIFY_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'
import type { CanvasAgent } from './composables/outlets'
import NodeInspector from './NodeInspector.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...nodes, ...pages, ...canvas } },
})
const t = i18n.global.t as (k: string, n?: Record<string, unknown>) => string

const AGENTS: CanvasAgent[] = [
  { name: '需求澄清', capabilities: CLARIFY_CAPS },
  { name: '测试评审', capabilities: TEST_REVIEW_CAPS },
]

function node(type: string, config: Record<string, any> = {}, id = type, label = type): WFNode {
  return { id, type, label, position: { x: 0, y: 0 }, config } as WFNode
}

function mountInspector(target: WFNode, opts: { allNodes?: WFNode[]; edges?: WFEdge[]; agents?: CanvasAgent[]; agentsLoaded?: boolean; focusGoalTick?: number } = {}) {
  return mount(NodeInspector, {
    props: {
      node: target,
      allNodes: opts.allNodes ?? [target],
      edges: opts.edges ?? [],
      agents: opts.agents ?? AGENTS,
      agentsLoaded: opts.agentsLoaded ?? true,
      focusGoalTick: opts.focusGoalTick,
    },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true, RouterLink: RouterLinkStub } },
  })
}

describe('NodeInspector · agent node', () => {
  it('shows only Agent, goal and timeout plus the read-only capabilities block', () => {
    const target = node('agent', { agent_profile: '测试评审', prompt: '跑测试' }, 'a1', '测试评审')
    const w = mountInspector(target)
    expect(w.find('[data-testid="agent-picker"]').text()).toContain('测试评审')
    expect((w.find('[data-testid="goal-input"]').element as HTMLTextAreaElement).value).toBe('跑测试')
    expect(w.find('[data-testid="inspector-timeout"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-agent_profile"]').exists()).toBe(false)
    expect(w.find('[data-testid="field-prompt"]').exists()).toBe(false)

    const caps = w.find('[data-testid="inspector-capabilities"]')
    expect(caps.text()).toContain(t('nodes.capabilities.interaction.auto'))
    expect(caps.text()).toContain(t('canvas.caps.review'))
    expect(caps.text()).toContain(t('nodes.capabilities.gated'))
    expect(caps.text()).toContain(t('nodes.capabilities.readsAll'))
    const link = w.findComponent(RouterLinkStub)
    expect(link.props('to')).toEqual({ path: '/agents', query: { agent: '测试评审', studioTab: 'capabilities' } })
    w.unmount()
  })

  it('renders capabilities as text chips, never the fixed-size node icon chip', () => {
    const target = node('agent', { agent_profile: '需求澄清', prompt: 'x' }, 'a1', '需求澄清')
    const w = mountInspector(target)
    const caps = w.find('[data-testid="inspector-caps-list"]')
    expect(caps.findAll('.cnode-cap')).toHaveLength(0)
    const chips = caps.findAll('.insp-chip')
    expect(chips.length).toBeGreaterThan(3)
    expect(chips.map((c) => c.text())).toContain(t('nodes.capabilities.interaction.clarify'))
    const writes = caps.findAll('.insp-row')[3]!.findAll('.insp-chip')
    expect(writes.length).toBe(CLARIFY_CAPS.writes!.length)
    expect(writes[0]!.attributes('title')).toMatch(new RegExp(`${t('nodes.capabilities.required')}|${t('nodes.capabilities.optional')}`))
    w.unmount()
  })

  it('picks an agent from the list and adopts its name as the default label', async () => {
    const target = node('agent', { agent_profile: '', prompt: '' }, 'a1', 'agent')
    const w = mountInspector(target)
    expect(w.text()).toContain(t('canvas.node.noAgent'))
    await w.find('[data-testid="agent-picker"]').trigger('click')
    const opt = w.find('[data-testid="agent-option-需求澄清"]')
    expect(opt.text()).toContain(t('nodes.capabilities.interaction.clarify'))
    await opt.trigger('click')
    expect(target.config.agent_profile).toBe('需求澄清')
    expect(target.label).toBe('需求澄清')
    expect(w.find('[data-testid="agent-picker-list"]').exists()).toBe(false)
    w.unmount()
  })

  it('keeps a custom label when switching agents', async () => {
    const target = node('agent', { agent_profile: '需求澄清', prompt: '' }, 'a1', '我的节点')
    const w = mountInspector(target)
    await w.find('[data-testid="agent-picker"]').trigger('click')
    await w.find('[data-testid="agent-picker-list"]').trigger('keydown', { key: 'ArrowDown' })
    await w.find('[data-testid="agent-picker-list"]').trigger('keydown', { key: 'Enter' })
    expect(target.config.agent_profile).toBe('测试评审')
    expect(target.label).toBe('我的节点')
    w.unmount()
  })

  it('flags an agent that no longer exists once agents are loaded', () => {
    const target = node('agent', { agent_profile: 'ghost', prompt: '' })
    expect(mountInspector(target).find('[data-testid="inspector-agent-missing"]').exists()).toBe(true)
    expect(mountInspector(target, { agentsLoaded: false }).find('[data-testid="inspector-agent-missing"]').exists()).toBe(false)
  })

  it('warns when the agent has no declared capabilities', () => {
    const target = node('agent', { agent_profile: 'bare', prompt: '' })
    const w = mountInspector(target, { agents: [{ name: 'bare' }] })
    expect(w.find('[data-testid="inspector-capabilities"]').text()).toContain(t('canvas.inspector.noCapsBody'))
  })

  it('stores timeout as whole minutes and clears invalid input', async () => {
    const target = node('agent', { agent_profile: '测试评审', prompt: '' })
    const w = mountInspector(target)
    const input = w.find('[data-testid="inspector-timeout"]')
    await input.setValue('12.6')
    expect(target.config.timeout).toBe(13)
    await input.setValue('-1')
    expect(target.config).not.toHaveProperty('timeout')
    await input.setValue('')
    expect(target.config).not.toHaveProperty('timeout')
  })

  it('stores nudge retries as 0..10 and clears empty input back to the default', async () => {
    const target = node('agent', { agent_profile: '测试评审', prompt: '', nudgeRetries: 2 })
    const w = mountInspector(target)
    const input = w.find('[data-testid="inspector-nudge-retries"]')
    expect((input.element as HTMLInputElement).value).toBe('2')
    expect(input.attributes('placeholder')).toBe(t('canvas.inspector.nudgePlaceholder'))
    await input.setValue('0')
    expect(target.config.nudgeRetries).toBe(0)
    await input.setValue('4.4')
    expect(target.config.nudgeRetries).toBe(4)
    await input.setValue('99')
    expect(target.config.nudgeRetries).toBe(10)
    await input.setValue('-1')
    expect(target.config).not.toHaveProperty('nudgeRetries')
    await input.setValue('')
    expect(target.config).not.toHaveProperty('nudgeRetries')
  })

  it('suggests variables and upstream outputs after typing {{ and inserts the token', async () => {
    const input = node('input', { variables: [{ name: 'feature', type: 'paragraph' }] }, 'in', '输入')
    const up = node('agent', { agent_profile: '需求澄清', prompt: '' }, 'clarify', '澄清')
    const target = node('agent', { agent_profile: '测试评审', prompt: '' }, 'test', '测试')
    const edges: WFEdge[] = [
      { id: 'e1', source: 'in', target: 'clarify' },
      { id: 'e2', source: 'clarify', target: 'test' },
    ]
    const w = mountInspector(target, { allNodes: [input, up, target], edges })
    const ta = w.find('[data-testid="goal-input"]')
    const el = ta.element as HTMLTextAreaElement
    el.value = '看 {{fea'
    el.setSelectionRange(el.value.length, el.value.length)
    await ta.trigger('input')
    expect(target.config.prompt).toBe('看 {{fea')
    const suggest = w.find('[data-testid="goal-suggest"]')
    expect(suggest.text()).toContain('{{vars.feature}}')
    expect(suggest.text()).not.toContain('{{nodes.clarify')
    await ta.trigger('keydown', { key: 'Enter' })
    expect(target.config.prompt).toBe('看 {{vars.feature}}')
    expect(w.find('[data-testid="goal-suggest"]').exists()).toBe(false)
    w.unmount()
  })

  it('lists upstream node outputs as goal tokens', async () => {
    const up = node('agent', { agent_profile: '需求澄清', prompt: '' }, 'clarify', '澄清')
    const target = node('agent', { agent_profile: '测试评审', prompt: '' }, 'test', '测试')
    const w = mountInspector(target, { allNodes: [up, target], edges: [{ id: 'e', source: 'clarify', target: 'test' }] })
    const ta = w.find('[data-testid="goal-input"]')
    const el = ta.element as HTMLTextAreaElement
    el.value = '{{'
    el.setSelectionRange(2, 2)
    await ta.trigger('input')
    expect(w.find('[data-testid="goal-suggest"]').text()).toContain('{{nodes.clarify.outputs.content}}')
    w.unmount()
  })

  it('focuses the goal when the focus tick changes', async () => {
    const target = node('agent', { agent_profile: '测试评审', prompt: 'abc' })
    const w = mountInspector(target)
    await w.setProps({ focusGoalTick: 1 })
    await new Promise((r) => setTimeout(r))
    expect(document.activeElement).toBe(w.find('[data-testid="goal-input"]').element)
    w.unmount()
  })

  it('renames the node and emits delete / close', async () => {
    const target = node('agent', { agent_profile: '测试评审', prompt: '' })
    const w = mountInspector(target)
    await w.find('[data-testid="inspector-name"]').setValue('新名字')
    expect(target.label).toBe('新名字')
    await w.find('[data-testid="inspector-delete"]').trigger('click')
    await w.find('[data-testid="inspector-close"]').trigger('click')
    expect(w.emitted('delete')).toHaveLength(1)
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })
})

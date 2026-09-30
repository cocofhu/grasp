// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nodes from '@/locales/zh-CN/nodes.json'
import type { WFEdge, WFNode } from '@/lib/shared/types'

const apiMocks = vi.hoisted(() => ({
  listAgents: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listAgents: apiMocks.listAgents,
    },
  }
})

import NodeInspector from './NodeInspector.vue'

function sampleNode(): WFNode {
  return {
    id: 'research',
    type: 'research',
    label: '调研',
    position: { x: 0, y: 0 },
    config: {},
  }
}

function sampleInputNode(): WFNode {
  return {
    id: 'input',
    type: 'input',
    label: '输入',
    position: { x: 0, y: 0 },
    config: { variables: [{ name: 'topic', type: 'string', value: '', ask: false }] },
  }
}

function sampleBranchNode(): WFNode {
  return {
    id: 'branch',
    type: 'branch',
    label: '分支',
    position: { x: 0, y: 0 },
    config: { cases: [{ when: 'exists("x")', goto: 'out' }] },
  }
}

function sampleGateNode(): WFNode {
  return {
    id: 'gate',
    type: 'human_gate',
    label: '门禁',
    position: { x: 0, y: 0 },
    config: { actions: [{ id: 'approve', label: '批准' }] },
  }
}

function mountInspector(node = sampleNode()) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...nodes } },
  })
  return mount(NodeInspector, {
    props: {
      node,
      allNodes: [node],
      edges: [] as WFEdge[],
    },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AppButton: { template: '<button><slot /></button>' },
        OutputSourcesEditor: { template: '<div data-testid="output-sources" />' },
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.listAgents.mockResolvedValue([{ name: 'agent-a' }])
})

describe('NodeInspector', () => {
  it('renders node title and config tab', async () => {
    const wrapper = mountInspector()
    await flushPromises()
    expect(wrapper.text()).toContain('调研')
    expect(wrapper.text()).toMatch(/配置|config/i)
    wrapper.unmount()
  })

  it('switches to help tab when clicked', async () => {
    const wrapper = mountInspector()
    await flushPromises()
    const helpBtn = wrapper.findAll('button').find((b) => b.text().includes('帮助') || b.text().includes('Help'))
    expect(helpBtn).toBeTruthy()
    await helpBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/帮助|Help|说明|文档/i)
    wrapper.unmount()
  })

  it('emits delete when delete action is triggered', async () => {
    const wrapper = mountInspector()
    await flushPromises()
    const del = wrapper.findAll('button').find((b) => b.classes().some((c) => c.includes('err')) || b.text().includes('删除'))
    expect(del).toBeTruthy()
    await del!.trigger('click')
    expect(wrapper.emitted('delete')).toBeTruthy()
    wrapper.unmount()
  })

  it('insertVar appends token into bound config field', async () => {
    const node = sampleInputNode()
    node.config.prompt = 'hello'
    const wrapper = mountInspector(node)
    await flushPromises()
    const insertBtn = wrapper.findAll('button').find((b) => b.text().includes('{{vars.topic}}') || b.attributes('title')?.includes('vars'))
    if (insertBtn) {
      await insertBtn.trigger('click')
      expect(String(node.config.prompt)).toContain('{{vars.topic}}')
    } else {
      // invoke via exposed config row if button label differs by locale
      const vm = wrapper.vm as any
      vm.insertVar?.('prompt', '{{vars.topic}}')
      expect(String(node.config.prompt)).toContain('{{vars.topic}}')
    }
    wrapper.unmount()
  })

  it('addFormField pushes a new form row on gate node', async () => {
    const node = sampleGateNode()
    node.config.form = []
    const wrapper = mountInspector(node)
    await flushPromises()
    ;(wrapper.vm as any).addFormField()
    expect(node.config.form?.length).toBe(1)
    wrapper.unmount()
  })

  it('addElseIf inserts branch case before default', async () => {
    const node = sampleBranchNode()
    node.config.cases = [{ when: 'default', goto: 'out' }]
    const wrapper = mountInspector(node)
    await flushPromises()
    ;(wrapper.vm as any).addElseIf?.()
    expect(node.config.cases?.length).toBe(2)
    expect(node.config.cases?.[0].when).not.toBe('default')
    wrapper.unmount()
  })

  it('loads agent list for agent node config without auto-filling empty agent_profile', async () => {
    const agentNode: WFNode = {
      id: 'agent',
      type: 'agent',
      label: 'Agent',
      position: { x: 0, y: 0 },
      config: { agent_profile: '' },
    }
    apiMocks.listAgents.mockResolvedValue([
      { name: 'agent-a', projectId: 'proj-1' },
      { name: 'other', projectId: 'proj-2' },
      { name: 'unbound', projectId: '' },
    ])
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages, ...nodes } },
    })
    const wrapper = mount(NodeInspector, {
      props: {
        node: agentNode,
        allNodes: [agentNode],
        edges: [] as WFEdge[],
        projectId: 'proj-1',
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          AppButton: { template: '<button><slot /></button>' },
          OutputSourcesEditor: { template: '<div data-testid="output-sources" />' },
        },
      },
    })
    await flushPromises()
    expect(apiMocks.listAgents).toHaveBeenCalled()
    // Empty config must stay empty (g1.4) — no auto-fill of agentNames[0].
    expect(agentNode.config.agent_profile).toBe('')
    const opts = wrapper.findAll('select option').map((o) => o.text())
    expect(opts.some((t) => t.includes('agent-a'))).toBe(true)
    expect(opts.some((t) => t.includes('other'))).toBe(false)
    expect(opts.some((t) => t.includes('unbound'))).toBe(false)
    wrapper.unmount()
  })

  it('keeps stale cross-project agent_profile with banner', async () => {
    const agentNode: WFNode = {
      id: 'agent',
      type: 'agent',
      label: 'Agent',
      position: { x: 0, y: 0 },
      config: { agent_profile: 'beta-runner' },
    }
    apiMocks.listAgents.mockResolvedValue([
      { name: 'agent-a', projectId: 'proj-1' },
      { name: 'beta-runner', projectId: 'proj-2' },
    ])
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages, ...nodes } },
    })
    const wrapper = mount(NodeInspector, {
      props: {
        node: agentNode,
        allNodes: [agentNode],
        edges: [] as WFEdge[],
        projectId: 'proj-1',
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          AppButton: { template: '<button><slot /></button>' },
          OutputSourcesEditor: { template: '<div data-testid="output-sources" />' },
        },
      },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="skill-profile-stale-banner"]').exists()).toBe(true)
    expect(wrapper.text()).toMatch(/非本项目|stale/)
    expect(agentNode.config.agent_profile).toBe('beta-runner')
    wrapper.unmount()
  })

  it('app_preview card shows auto_inject on by default', async () => {
    const node: WFNode = {
      id: 'preview',
      type: 'app_preview',
      label: '应用预览',
      position: { x: 0, y: 0 },
      config: { direct_preview: true },
    }
    const wrapper = mountInspector(node)
    await flushPromises()
    expect(wrapper.text()).toContain('自动注入')
    const sw = wrapper.find('[data-testid="node-switch-auto_inject"]')
    expect(sw.exists()).toBe(true)
    expect(sw.attributes('aria-checked')).toBe('true')
    await sw.trigger('click')
    expect(node.config.auto_inject).toBe(false)
    expect(sw.attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it.each(['grasp', 'approve', 'app_preview'] as const)('%s shows omitted Live enabled and persists explicit opt-out', async (type) => {
    const node: WFNode = { id: 'preview', type, label: 'Preview', position: { x: 0, y: 0 }, config: { direct_preview: true } }
    const wrapper = mountInspector(node)
    await flushPromises()
    const sw = wrapper.get('[data-testid="node-switch-live_variants"]')
    expect(sw.attributes('aria-checked')).toBe('true')
    expect(node.config.live_variants).toBeUndefined()
    await sw.trigger('click')
    expect(node.config.live_variants).toBe(false)
    expect(sw.attributes('aria-checked')).toBe('false')
    await sw.trigger('click')
    expect(node.config.live_variants).toBe(true)
    wrapper.unmount()
  })

  it('addAction appends gate action row', async () => {
    const node = sampleGateNode()
    const wrapper = mountInspector(node)
    await flushPromises()
    ;(wrapper.vm as any).addAction?.()
    expect(node.config.actions?.length).toBe(2)
    wrapper.unmount()
  })
})

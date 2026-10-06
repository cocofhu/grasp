// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import type { PmLeaderBinding } from '@/lib/shared/types'
import PmSettingsPanel from './PmSettingsPanel.vue'

const apiMocks = vi.hoisted(() => ({
  getPmLeader: vi.fn(),
  updatePmLeader: vi.fn(),
  listAgents: vi.fn(),
  getProject: vi.fn(),
  listProjectChannels: vi.fn(),
  listPmThreads: vi.fn(),
}))

const toastMocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getPmLeader: apiMocks.getPmLeader,
      updatePmLeader: apiMocks.updatePmLeader,
      listAgents: apiMocks.listAgents,
      getProject: apiMocks.getProject,
      listProjectChannels: apiMocks.listProjectChannels,
      listPmThreads: apiMocks.listPmThreads,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => toastMocks,
}))

const BINDING: PmLeaderBinding = {
  enabled: true,
  agentAvailable: true,
  agentConfigRef: 'agent-1',
  enabledMcps: ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager'],
  aclNote: 'note',
}

type ChannelFixture = {
  channel?: Record<string, unknown> | null
  secretsKeyConfigured?: boolean
}

async function mountPanel(
  binding: PmLeaderBinding = BINDING,
  channelFixture: ChannelFixture = { channel: null, secretsKeyConfigured: true },
) {
  apiMocks.getPmLeader.mockResolvedValue(binding)
  apiMocks.listAgents.mockResolvedValue([{ name: 'agent-1' }])
  apiMocks.getProject.mockResolvedValue({
    id: 'proj-1',
    name: 'Demo',
    notifyPolicy: { enabled: true, defaultEvents: ['waiting_human', 'failed'], channelIds: [] },
  })
  apiMocks.listProjectChannels.mockResolvedValue({
    items: channelFixture.channel
      ? [{
          id: 'chn-1',
          type: 'qq',
          name: String((channelFixture.channel as any).name || 'QQ'),
          enabled: !!(channelFixture.channel as any).enabled,
          projectId: 'proj-1',
          agentName: 'agent-1',
          isPrimary: true,
          enabledMcps: ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager'],
          appId: String((channelFixture.channel as any).appId || 'app'),
          appSecretSet: !!(channelFixture.channel as any).appSecretSet,
          turnTimeoutSeconds: Number((channelFixture.channel as any).turnTimeoutSeconds || 0),
          cronDeliver: !!(channelFixture.channel as any).cronDeliver,
          cronDeliverTarget: (channelFixture.channel as any).cronDeliverTarget,
          config: (channelFixture.channel as any).config || {},
          createdAt: '',
          updatedAt: '',
        }]
      : [],
    secretsKeyConfigured: channelFixture.secretsKeyConfigured ?? true,
    freeAgents: ['agent-1'],
  })
  apiMocks.updatePmLeader.mockImplementation(async (_id: string, body: Record<string, unknown>) => ({
    ...BINDING,
    ...body,
    agentAvailable: true,
    aclNote: BINDING.aclNote,
  }))

  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }, { path: '/agents', component: { template: '<div />' } }],
  })
  await router.push('/')
  const w = mount(PmSettingsPanel, {
    props: { projectId: 'proj-1' },
    global: {
      plugins: [i18n, router],
      stubs: {
        // Channel UI moved to PmChannelMultiPanel; keep leader tests focused.
        PmChannelMultiPanel: true,
      },
    },
  })
  await flushPromises()
  return w
}

function mcpSwitches(w: Awaited<ReturnType<typeof mountPanel>>) {
  return ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager'].map((id) =>
    w.get(`[aria-label="${id}"]`),
  )
}

describe('PmSettingsPanel enabledMcps', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPmThreads.mockResolvedValue({ items: [] })
  })

  it('loads enabledMcps and saves toggled selection', async () => {
    const w = await mountPanel({
      ...BINDING,
      enabledMcps: ['pm-progress'],
    })
    const mcpCodes = w.findAll('code').map((c) => c.text())
    expect(mcpCodes).toEqual(['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager'])

    // PM MCP toggles are AppSwitch (role=switch); channel section has more switches after them.
    const boxes = mcpSwitches(w)
    expect(boxes).toHaveLength(5)
    expect(boxes[0].attributes('aria-checked')).toBe('true')
    expect(boxes[1].attributes('aria-checked')).toBe('false')
    expect(boxes[2].attributes('aria-checked')).toBe('false')
    expect(boxes[3].attributes('aria-checked')).toBe('false')
    expect(boxes[4].attributes('aria-checked')).toBe('false')

    await boxes[1].trigger('click')
    const saveBtn = w.find('[data-testid="pm-leader-save"]')
    expect(saveBtn).toBeTruthy()
    await saveBtn!.trigger('click')
    await flushPromises()

    const body = apiMocks.updatePmLeader.mock.calls[0][1] as { enabledMcps: string[] }
    expect(body.enabledMcps).toEqual(['pm-progress', 'pm-workflow-read'])
  })

  it('defaults all PM mcps when binding omits enabledMcps', async () => {
    const { enabledMcps: _drop, ...rest } = BINDING
    const w = await mountPanel(rest as PmLeaderBinding)
    const boxes = mcpSwitches(w)
    expect(boxes[0].attributes('aria-checked')).toBe('true')
    expect(boxes[1].attributes('aria-checked')).toBe('true')
    expect(boxes[2].attributes('aria-checked')).toBe('true')
    expect(boxes[3].attributes('aria-checked')).toBe('true')
    expect(boxes[4].attributes('aria-checked')).toBe('true')
  })

  it('can toggle off a PM mcp and persist only the remaining ids', async () => {
    const w = await mountPanel()
    const boxes = mcpSwitches(w)
    expect(boxes[0].attributes('aria-checked')).toBe('true')
    expect(boxes[1].attributes('aria-checked')).toBe('true')
    expect(boxes[2].attributes('aria-checked')).toBe('true')
    expect(boxes[3].attributes('aria-checked')).toBe('true')
    expect(boxes[4].attributes('aria-checked')).toBe('true')

    await boxes[0].trigger('click') // uncheck pm-progress
    const saveBtn = w.find('[data-testid="pm-leader-save"]')
    await saveBtn!.trigger('click')
    await flushPromises()

    const body = apiMocks.updatePmLeader.mock.calls[0][1] as { enabledMcps: string[] }
    expect(body.enabledMcps).toEqual(['pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager'])
  })

  it('preserves explicit empty enabledMcps from the API', async () => {
    const w = await mountPanel({
      ...BINDING,
      enabledMcps: [],
    })
    const boxes = mcpSwitches(w)
    expect(boxes[0].attributes('aria-checked')).toBe('false')
    expect(boxes[1].attributes('aria-checked')).toBe('false')
    expect(boxes[2].attributes('aria-checked')).toBe('false')
    expect(boxes[3].attributes('aria-checked')).toBe('false')
    expect(boxes[4].attributes('aria-checked')).toBe('false')

    const saveBtn = w.find('[data-testid="pm-leader-save"]')
    await saveBtn!.trigger('click')
    await flushPromises()

    const body = apiMocks.updatePmLeader.mock.calls[0][1] as { enabledMcps: string[] }
    expect(body.enabledMcps).toEqual([])
  })

  it('embeds multi-channel panel host', async () => {
    const w = await mountPanel()
    // Stubbed child still mounts as PmChannelMultiPanel placeholder.
    expect(w.find('pm-channel-multi-panel-stub').exists() || w.html().includes('PmChannelMultiPanel')).toBe(
      true,
    )
  })
})

describe('PmSettingsPanel gate-auto config', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPmThreads.mockResolvedValue({ items: [] })
  })

  it('loads and saves gateAutoVar + gateAutoPrompt as text fields', async () => {
    const binding: PmLeaderBinding = {
      ...BINDING,
      gateAutoVar: 'pm_auto_gate',
      gateAutoPrompt: '优先批准低风险',
    }
    const w = await mountPanel(binding)
    const varInput = w.get('[data-testid="pm-gate-auto-var"]')
    const promptInput = w.get('[data-testid="pm-gate-auto-prompt"]')
    expect((varInput.element as HTMLInputElement).value).toBe('pm_auto_gate')
    expect((promptInput.element as HTMLTextAreaElement).value).toBe('优先批准低风险')

    await varInput.setValue('other_switch')
    await promptInput.setValue('')
    await w.get('[data-testid="pm-leader-save"]').trigger('click')
    await flushPromises()

    const updateCalls = apiMocks.updatePmLeader.mock.calls
    const body = updateCalls[updateCalls.length - 1]?.[1] as {
      gateAutoVar: string
      gateAutoPrompt: string
    }
    expect(body.gateAutoVar).toBe('other_switch')
    expect(body.gateAutoPrompt).toBe('')
  })
})

describe('PmSettingsPanel header without settingsHint', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPmThreads.mockResolvedValue({ items: [] })
  })

  it('keeps settings title + status badge and omits the removed settingsHint copy', async () => {
    const w = await mountPanel({ ...BINDING, aclNote: '' })
    const text = w.text()

    expect(w.find('h2').text()).toBe('项目管理设置')
    const badge = w.find('[role="status"]')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('已启用')
    expect(badge.text()).toContain('agent-1')

    const titleWrap = w.find('h2').element.parentElement
    expect(titleWrap?.classList.contains('max-w-[42em]')).toBe(true)
    expect(titleWrap?.classList.contains('min-w-0')).toBe(true)
    expect(text).not.toContain('无官方模板')
    expect(text).not.toContain('仅配置项目管理专用 MCP')
    expect(text).not.toContain('No official template')
    expect(text).not.toContain('pages.projectDetail.pm.settingsHint')

    expect(text).toContain('任意已登录用户可启用/换绑/停用')
    expect(text).toContain('通用记忆/上下文/调度器始终注入，不受此列表控制')
    expect(text).toContain('记忆请在 Agent Studio「数据 → 记忆」中管理')
  })

  it('still shows noAgents hint when no bindable Agent exists', async () => {
    const w = await mountPanel({ ...BINDING, enabled: false, agentConfigRef: '' })
    apiMocks.listAgents.mockResolvedValue([])
    // remount with empty agents via helper path
    apiMocks.getPmLeader.mockResolvedValue({ ...BINDING, enabled: false, agentConfigRef: '' })
    apiMocks.listAgents.mockResolvedValue([])
    await w.unmount()
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/agents', component: { template: '<div />' } },
      ],
    })
    await router.push('/')
    const w2 = mount(PmSettingsPanel, {
      props: { projectId: 'proj-1' },
      global: { plugins: [i18n, router], stubs: { PmChannelMultiPanel: true } },
    })
    await flushPromises()
    const text = w2.text()
    expect(w2.find('h2').text()).toBe('项目管理设置')
    expect(w2.find('[role="status"]').text()).toContain('Agent 不可用')
    expect(text).toContain('当前无可绑定的 Agent，请先到 Agent 配置中心创建。')
    expect(text).toContain('前往 Agent 配置中心')
    expect(text).toContain('任意已登录用户可启用/换绑/停用')
    expect(text).toContain('通用记忆/上下文/调度器始终注入，不受此列表控制')
    expect(text).not.toContain('无官方模板')
    expect(text).not.toContain('仅配置项目管理专用 MCP')
  })

})

describe('PmSettingsPanel header en locale', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPmThreads.mockResolvedValue({ items: [] })
    apiMocks.getProject.mockResolvedValue({
      id: 'proj-1',
      name: 'Demo',
      notifyPolicy: { enabled: true, defaultEvents: ['waiting_human', 'failed'], channelIds: [] },
    })
    apiMocks.listProjectChannels.mockResolvedValue({
      items: [],
      secretsKeyConfigured: true,
      freeAgents: ['agent-1'],
    })
  })

  it('English locale keeps the settings title and never shows No official template', async () => {
    apiMocks.getPmLeader.mockResolvedValue(BINDING)
    apiMocks.listAgents.mockResolvedValue([{ name: 'agent-1' }])

    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { ...enCommon, ...enPages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push('/')
    const w = mount(PmSettingsPanel, {
      props: { projectId: 'proj-1' },
      global: { plugins: [i18n, router], stubs: { PmChannelMultiPanel: true } },
    })
    await flushPromises()

    const text = w.text()
    expect(w.find('h2').text()).toBe('Project Management settings')
    expect(w.find('[role="status"]').text()).toContain('Enabled')
    expect(text).toContain('Any signed-in user may enable, rebind, or disable')
    expect(text).toContain('Shared memory/context/scheduler always inject and are not controlled here.')
    expect(text).not.toContain('No official template')
    expect(text).not.toContain('pages.projectDetail.pm.settingsHint')
  })
})

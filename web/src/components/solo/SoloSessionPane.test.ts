// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import type { SoloTurn } from '@/lib/solo/acpTurn'

const box = vi.hoisted(() => ({
  turns: undefined as undefined | import('vue').Ref<SoloTurn[]>,
  session: undefined as undefined | import('vue').Ref<{
    id: string
    title: string
    agentName: string
    status: string
    artifactRunId: string
    projectId: string
    project?: { name?: string }
    repos: Array<{ position: number; projectId: string; name: string; repoUrl: string; repoDirName: string; branch: string }>
    initialModelId?: string
    currentModelId?: string
    currentModelSet?: boolean
    reasoningEffort?: string
    reasoningEfforts?: string[]
  }>,
  status: undefined as undefined | import('vue').Ref<string>,
  link: undefined as undefined | import('vue').Ref<string>,
  currentModelId: undefined as undefined | import('vue').Ref<string>,
  currentReasoningEffort: undefined as undefined | import('vue').Ref<string>,
  modelChangePending: undefined as undefined | import('vue').Ref<boolean>,
  setCurrentReasoningEffort: undefined as undefined | ReturnType<typeof vi.fn>,
  modelSwitchNotice: undefined as undefined | import('vue').Ref<string>,
  actionError: undefined as undefined | import('vue').Ref<string>,
  setCurrentModel: undefined as undefined | ReturnType<typeof vi.fn>,
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
}))
vi.mock('@/lib/api/api', () => ({
  api: {
    runArtifacts: vi.fn(async () => []),
    getSoloEnv: vi.fn(async () => []),
    listAgents: vi.fn(async () => []),
    listModelConfigurations: vi.fn(async () => ({ items: [] })),
    listModelConnections: vi.fn(async () => ({ items: [] })),
    cliModels: vi.fn(async () => ({ models: [] })),
  },
}))
vi.mock('@/lib/solo/useSoloSessions', () => ({ useSoloSessions: () => ({ upsert: vi.fn(), remove: vi.fn() }) }))
vi.mock('@/lib/solo/useSoloChat', () => ({
  useSoloChat: () => ({
    session: box.session,
    turns: box.turns,
    queued: ref([]),
    busy: ref(false),
    phase: ref(''),
    link: box.link,
    loading: ref(false),
    loadError: ref(''),
    actionError: box.actionError,
    status: box.status,
    closed: computed(() => box.status?.value === 'closed'),
    canSend: computed(() => box.status?.value === 'starting' || box.status?.value === 'running'),
    currentModelId: box.currentModelId,
    currentReasoningEffort: box.currentReasoningEffort,
    modelChangePending: box.modelChangePending,
    setCurrentReasoningEffort: (...args: unknown[]) => box.setCurrentReasoningEffort?.(...args),
    modelSwitchNotice: box.modelSwitchNotice,
    load: vi.fn(),
    send: vi.fn(),
    setCurrentModel: (...args: unknown[]) => box.setCurrentModel?.(...args),
    removeQueued: vi.fn(),
  }),
}))

import SoloSessionPane from './SoloSessionPane.vue'
import pages from '@/locales/zh-CN/pages.json'
import { resetCliCatalog } from '@/lib/agent/cliCatalog'

const turns = (box.turns = ref<SoloTurn[]>([]))
const sessionState = (box.session = ref<NonNullable<typeof box.session>['value']>({
  id: 's1',
  title: 'Solo',
  agentName: 'Coder',
  status: 'running',
  artifactRunId: '',
  projectId: '',
  project: undefined as { name?: string } | undefined,
  repos: [] as Array<{ position: number; projectId: string; name: string; repoUrl: string; repoDirName: string; branch: string }>,
}))
const statusState = (box.status = ref('running'))
const linkState = (box.link = ref('open'))
const currentModelId = (box.currentModelId = ref(''))
const currentReasoningEffort = (box.currentReasoningEffort = ref(''))
const modelChangePending = (box.modelChangePending = ref(false))
const setCurrentReasoningEffort = (box.setCurrentReasoningEffort = vi.fn(async () => true))
const modelSwitchNotice = (box.modelSwitchNotice = ref(''))
const actionError = (box.actionError = ref(''))
const setCurrentModel = (box.setCurrentModel = vi.fn(async () => true))

function mountPane(messages: object = {}) {
  return mount(SoloSessionPane, {
    attachTo: document.body,
    props: { sessionId: 's1' },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': messages } })],
      stubs: {
        Icon: true,
        ReviewShell: { template: '<div><slot name="actions" /><slot name="sidebar" /></div>' },
        ReactArtifactStage: true,
        AgentTimeline: true,
        SoloStartupCard: true,
        SoloLimitModal: true,
        AppModal: true,
        ChatImagePreviewModal: true,
      },
    },
  })
}

function turn(id: string, role: SoloTurn['role'], text: string): SoloTurn {
  const steps: SoloTurn['steps'] = role === 'agent' ? [{ kind: 'message', text }] : []
  return { id, role, text, tools: [], steps, plan: [], streaming: false }
}

describe('SoloSessionPane', () => {
  it('renders the message rail with one mark per user turn', async () => {
    turns.value = [
      turn('u1', 'user', '修一下登录'),
      turn('a1', 'agent', '已修复'),
      turn('s1', 'system', '已重启'),
      turn('u2', 'user', '再加个测试'),
      turn('a2', 'agent', '已添加'),
    ]
    const wrapper = mountPane()
    await flushPromises()

    expect(wrapper.find('[data-testid="chat-message-rail"]').exists()).toBe(true)
    const marks = wrapper.findAll('[data-testid="chat-message-rail-mark"]')
    expect(marks).toHaveLength(2)
    expect(marks.map(mark => mark.attributes('data-message-id'))).toEqual(['u1', 'u2'])
    expect(wrapper.findAll('[data-chat-message-id]').map(el => el.attributes('data-chat-message-id'))).toEqual(['u1', 'u2'])
    wrapper.unmount()
  })

  it('lists every repository edit link on a sandbox error without an environment toggle', async () => {
    turns.value = []
    statusState.value = 'error'
    sessionState.value = {
      id: 's1',
      title: 'Solo',
      agentName: 'Coder',
      status: 'error',
      artifactRunId: '',
      projectId: '',
      repos: [
        { position: 0, projectId: 'web', name: 'webapp', repoUrl: 'https://github.com/acme/webapp.git', repoDirName: 'webapp', branch: 'main' },
        { position: 1, projectId: 'api', name: 'api', repoUrl: 'https://github.com/acme/api.git', repoDirName: 'api', branch: 'main' },
      ],
    }
    const wrapper = mountPane(pages)
    await flushPromises()

    expect(wrapper.find('[data-testid="solo-env-toggle"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('会话环境')

    const links = wrapper.findAll('[data-testid^="solo-error-edit-"]')
    expect(links.map((link) => link.attributes('href'))).toEqual([
      '/settings/projects?edit=web',
      '/settings/projects?edit=api',
    ])
    expect(links[0]?.text()).toContain('修改 webapp')
    expect(links[1]?.text()).toContain('修改 api')
    wrapper.unmount()
    statusState.value = 'running'
  })

  it('keeps the environment toggle absent while starting, running, closed, or reconnecting', async () => {
    turns.value = []
    const cases = [
      { status: 'starting', link: 'open', banner: false },
      { status: 'running', link: 'open', banner: false },
      { status: 'closed', link: 'open', banner: false },
      { status: 'running', link: 'reconnecting', banner: true },
    ]
    for (const item of cases) {
      statusState.value = item.status
      linkState.value = item.link
      sessionState.value = {
        ...sessionState.value,
        status: item.status,
      }
      const wrapper = mountPane(pages)
      await flushPromises()
      expect(wrapper.find('[data-testid="solo-env-toggle"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('会话环境')
      if (item.banner) expect(wrapper.text()).toContain('连接已断开，正在重连…')
      else expect(wrapper.text()).not.toContain('连接已断开，正在重连…')
      wrapper.unmount()
    }
    statusState.value = 'running'
    linkState.value = 'open'
  })

  it('places the model selector to the right of the attachment button', async () => {
    turns.value = []
    statusState.value = 'running'
    currentModelId.value = ''
    modelSwitchNotice.value = '当前回复仍使用原来的模型，从下一条开始使用新模型。'
    const wrapper = mountPane(pages)
    await flushPromises()

    const toolbar = wrapper.get('.chat-toolbar')
    const plus = toolbar.get('[aria-label="添加附件"]')
    const select = toolbar.get('[data-testid="solo-model-select"]')
    expect(plus.element.compareDocumentPosition(select.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(wrapper.get('[data-testid="solo-model-switch-notice"]').text()).toContain('从下一条开始使用新模型')
    expect(select.text()).toContain('当前 Agent 还没有可用模型')
    expect(select.get('button').attributes('disabled')).toBeDefined()
    wrapper.unmount()
    modelSwitchNotice.value = ''
  })

  it('shows the saved model name and uses only the active session effort capabilities', async () => {
    resetCliCatalog()
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.listAgents).mockResolvedValueOnce([{ name: 'Coder', acpBackend: 'deepseek_harness', modelBinding: { kind: 'configuration', configurationId: 'kimi-config' } }] as never)
    vi.mocked(api.listModelConfigurations).mockResolvedValueOnce({ items: [{
      id: 'kimi-config', name: 'Kimi', backend: 'deepseek_harness', state: 'active', revision: 1,
      binding: { kind: 'connection', connectionId: 'kimi-service', modelId: 'kimi-k3.1-pro' },
    }] })
    vi.mocked(api.listModelConnections).mockResolvedValueOnce({ items: [{
      id: 'kimi-service', name: 'TokenHub API', providerId: 'tokenhub', protocol: 'openai-chat-completions', baseUrl: 'https://api.example.test/v1', credentialId: 'key', auth: { scheme: 'bearer' }, state: 'active', revision: 1,
      models: [{ id: 'kimi-k3.1-pro', source: 'manual', reasoningEfforts: ['low', 'high', 'ultra'] }],
    }] })
    currentModelId.value = ''
    currentReasoningEffort.value = 'high'
    modelChangePending.value = false
    sessionState.value = { ...sessionState.value, reasoningEffort: 'high', reasoningEfforts: ['low', 'high'] }
    setCurrentReasoningEffort.mockClear()
    setCurrentModel.mockClear()
    const wrapper = mountPane(pages)
    await flushPromises()
    expect(wrapper.get('[data-testid="solo-model-select-trigger"]').text()).toContain('Kimi')
    expect(wrapper.get('[data-testid="solo-model-select-trigger"]').text()).not.toContain('kimi-k3.1-pro')
    expect(wrapper.get('[data-testid="solo-model-select-trigger"] [data-vendor-mark="kimi"] img').exists()).toBe(true)
    expect(wrapper.find('[data-testid="solo-model-select-trigger"] [data-vendor-mark="deepseek_harness"]').exists()).toBe(false)
    await wrapper.get('[data-testid="solo-model-select-trigger"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="solo-effort-panel"]')).not.toBeNull()
    expect(document.activeElement).toBe(document.querySelector('[data-testid="solo-effort-slider"]'))
    ;(document.querySelector('[data-testid="solo-effort-back"]') as HTMLButtonElement).click()
    await flushPromises()
    const modelOption = document.querySelector('[data-testid="solo-model-option-kimi-k3.1-pro"]')
    expect(modelOption?.textContent).toContain('Kimi')
    expect(modelOption?.textContent).not.toContain('kimi-k3.1-pro')
    expect(modelOption?.querySelector('[data-vendor-mark="kimi"] img')).not.toBeNull()
    ;(document.querySelector('[data-testid="solo-effort-open"]') as HTMLButtonElement).click()
    await flushPromises()
    const slider = document.querySelector<HTMLInputElement>('[data-testid="solo-effort-slider"]')!
    expect(slider.max).toBe('2')
    expect(slider.value).toBe('2')
    expect(document.activeElement).toBe(slider)
    slider.value = '1'
    slider.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(setCurrentReasoningEffort).toHaveBeenCalledWith('low')
    ;(document.querySelector('[data-testid="solo-effort-reset"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(setCurrentReasoningEffort).toHaveBeenLastCalledWith('')
    expect(setCurrentModel).not.toHaveBeenCalled()
    // A model write must not expose the previous model's levels on its target.
    modelChangePending.value = true
    await flushPromises()
    expect(wrapper.find('[data-testid="solo-model-effort-label"]').exists()).toBe(false)
    expect(document.querySelector('[data-testid="solo-effort-panel"]')).toBeNull()
    wrapper.unmount()
    sessionState.value = { ...sessionState.value, reasoningEffort: undefined, reasoningEfforts: undefined }
    currentReasoningEffort.value = ''
    modelChangePending.value = false
    resetCliCatalog()
  })

  it('hides the selector on a closed session and disables it when sending is blocked', async () => {
    turns.value = []
    statusState.value = 'closed'
    const closed = mountPane(pages)
    await flushPromises()
    expect(closed.find('[data-testid="solo-closed-bar"]').exists()).toBe(true)
    expect(closed.find('[data-testid="solo-model-select"]').exists()).toBe(false)
    closed.unmount()

    statusState.value = 'error'
    const errored = mountPane(pages)
    await flushPromises()
    const select = errored.get('[data-testid="solo-model-select"]')
    expect(select.get('button').attributes('disabled')).toBeDefined()
    errored.unmount()
    statusState.value = 'running'
  })

  it('asks to fall back when the saved model is no longer available', async () => {
    resetCliCatalog()
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.listAgents).mockResolvedValueOnce([
      { name: 'Coder', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'c1' } },
    ] as never)
    vi.mocked(api.listModelConfigurations).mockResolvedValueOnce({
      items: [{
        id: 'c1',
        name: 'cfg',
        backend: 'cursor',
        state: 'active',
        revision: 1,
        binding: { kind: 'platform', credentialId: 'k', modelId: 'primary', auxiliaryModelIds: ['gone'] },
      }],
    })
    vi.mocked(api.cliModels).mockResolvedValue({ models: [{ id: 'primary', name: 'Primary', vendor: 'cursor' }] } as never)
    currentModelId.value = 'gone'
    setCurrentModel.mockClear()
    setCurrentModel.mockResolvedValue(true)
    actionError.value = ''
    const wrapper = mountPane(pages)
    await flushPromises()
    expect(setCurrentModel).toHaveBeenCalledWith('')
    expect(actionError.value).toContain('已改回主模型')
    wrapper.unmount()
    currentModelId.value = ''
    resetCliCatalog()
  })

  it('keeps the saved model when the directory requests fail', async () => {
    resetCliCatalog()
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.listAgents).mockRejectedValueOnce(new Error('agents down'))
    vi.mocked(api.listModelConfigurations).mockRejectedValueOnce(new Error('configs down'))
    vi.mocked(api.listModelConnections).mockRejectedValueOnce(new Error('connections down'))
    currentModelId.value = 'aux2'
    setCurrentModel.mockClear()
    actionError.value = ''
    const wrapper = mountPane(pages)
    await flushPromises()
    expect(setCurrentModel).not.toHaveBeenCalled()
    expect(actionError.value).not.toContain('已改回主模型')
    expect(wrapper.text()).not.toContain('当前 Agent 还没有可用模型')
    expect(wrapper.get('[data-testid="solo-model-catalog-notice"]').text()).toContain('模型目录没有加载出来')
    expect(wrapper.get('[data-testid="solo-model-select"]').text()).toContain('aux2')
    expect(wrapper.get('[data-testid="solo-model-select"] button').attributes('disabled')).toBeDefined()
    wrapper.unmount()
    currentModelId.value = ''
    resetCliCatalog()
  })

  it('keeps a configured model when the CLI catalog request fails', async () => {
    resetCliCatalog()
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.listAgents).mockResolvedValueOnce([
      { name: 'Coder', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'c1' } },
    ] as never)
    vi.mocked(api.listModelConfigurations).mockResolvedValueOnce({
      items: [{
        id: 'c1',
        name: 'cfg',
        backend: 'cursor',
        state: 'active',
        revision: 1,
        binding: { kind: 'platform', credentialId: 'k', modelId: 'primary', auxiliaryModelIds: ['aux2'] },
      }],
    })
    vi.mocked(api.cliModels).mockRejectedValueOnce(new Error('catalog down'))
    currentModelId.value = 'aux2'
    setCurrentModel.mockClear()
    actionError.value = ''
    const wrapper = mountPane(pages)
    await flushPromises()
    expect(setCurrentModel).not.toHaveBeenCalled()
    expect(actionError.value).not.toContain('已改回主模型')
    expect(wrapper.get('[data-testid="solo-model-catalog-notice"]').text()).toContain('模型目录没有加载出来')
    expect(wrapper.get('[data-testid="solo-model-select"]').text()).toContain('aux2')
    expect(wrapper.get('[data-testid="solo-model-select"] button').attributes('disabled')).toBeDefined()
    wrapper.unmount()
    currentModelId.value = ''
    vi.mocked(api.cliModels).mockResolvedValue({ models: [] } as never)
    resetCliCatalog()
  })
})

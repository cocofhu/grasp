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
  }>,
  status: undefined as undefined | import('vue').Ref<string>,
  link: undefined as undefined | import('vue').Ref<string>,
  currentModelId: undefined as undefined | import('vue').Ref<string>,
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
const sessionState = (box.session = ref({
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

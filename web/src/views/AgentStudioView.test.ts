// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nav from '@/locales/zh-CN/nav.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import type { Agent } from '@/lib/api/api'

const mocks = vi.hoisted(() => ({
  listAgents: vi.fn(),
  saveAgent: vi.fn(),
  renameAgent: vi.fn(),
  deleteAgent: vi.fn(),
  listRunTags: vi.fn(),
  createSharedAgentTest: vi.fn(),
  exportWorkspaceAgents: vi.fn(),
}))

const breakpointMocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue') as typeof import('vue')
  return { isMobile: vue.ref(false) }
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getSharedAgentConfig: async () => ({ files: [], mcp: [], env: {}, layout: {} }),
      listCredentials: async () => ({ items: [] }),
      listModelConfigurations: async () => ({ items: [] }),
      listModelConnections: async () => ({ items: [] }),
      listToolCapabilities: async () => ({ items: [] }),
      listAgents: mocks.listAgents,
      saveAgent: mocks.saveAgent,
      renameAgent: mocks.renameAgent,
      deleteAgent: mocks.deleteAgent,
      listRunTags: mocks.listRunTags,
      createSharedAgentTest: mocks.createSharedAgentTest,
      exportWorkspaceAgents: mocks.exportWorkspaceAgents,
    },
  }
})

vi.mock('@/lib/composables/useWorkspaceContext', async () => {
  const { ref } = await import('vue')
  const workspace = ref({ name: 'Default', description: '', variables: [] })
  return { useWorkspaceContext: () => ({ workspace, ensureLoaded: vi.fn(async () => workspace.value), reload: vi.fn(async () => workspace.value) }) }
})

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: breakpointMocks.isMobile }),
}))

import AgentStudioView from './AgentStudioView.vue'

const ButtonStub = defineComponent({
  inheritAttrs: false,
  template: '<button v-bind="$attrs"><slot /></button>',
})
const CodeEditorStub = defineComponent({
  props: { modelValue: String },
  emits: ['update:modelValue'],
  template: '<textarea data-test="code-editor" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
})

function agent(region?: string): Agent {
  return {
    name: 'legacy',
    inheritModel: false,
    inheritGit: false,
    acpBackend: 'codebuddy',
    files: [],
    mcp: [],
    env: region === undefined ? {} : { GRASP_CODEBUDDY_REGION: region },
    layout: { configRoot: '/root/.codebuddy', workspaceDir: '/root/workspace' },
  }
}

async function createStudioRouter(query: Record<string, string> = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/agents', component: { render: () => h('div') } },
    ],
  })
  await router.push({ path: '/agents', query })
  await router.isReady()
  return router
}

const mountedWrappers: Array<{ unmount: () => void }> = []

function trackMount<T extends { unmount: () => void }>(wrapper: T): T {
  mountedWrappers.push(wrapper)
  return wrapper
}

function removeStudioToasts() {
  document.querySelectorAll('[data-test="studio-toast"]').forEach((el) => el.remove())
}

async function mountStudio(query: Record<string, string> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = await createStudioRouter(query)
  return trackMount(
    mount(AgentStudioView, {
      global: {
        plugins: [i18n, router],
        stubs: {
          AppButton: ButtonStub,
          Icon: true,
          AppModal: true,
          CodeEditor: CodeEditorStub,
          MarkdownSplitEditor: true,
          ExplorerContextMenu: true,
          AgentChatTester: true,
          AgentGitGuide: true,
          AgentCreateWizard: true,
          AgentSidebar: true,
          AgentDataPanel: true,
        },
      },
    }),
  )
}

async function editCustomEnvironment(wrapper: Awaited<ReturnType<typeof mountStudio>>, value: string) {
  await wrapper.findAll('button').find(button => button.text().startsWith('环境变量'))!.trigger('click')
  await wrapper.findAll('button').find(button => button.text() === '原始 JSON')!.trigger('click')
  await wrapper.get('[data-test="code-editor"]').setValue(JSON.stringify({ STUDIO_FEATURE: value }))
  await wrapper.findAll('button').find(button => button.text() === '元信息')!.trigger('click')
}

beforeEach(() => {
  removeStudioToasts()
  vi.clearAllMocks()
  breakpointMocks.isMobile.value = false
  mocks.listRunTags.mockResolvedValue({ tags: [] })
  mocks.saveAgent.mockImplementation(async (payload: Agent) => payload)
  mocks.createSharedAgentTest.mockResolvedValue({ id: 1 })
  mocks.deleteAgent.mockResolvedValue({ status: 'ok' })
})

afterEach(() => {
  while (mountedWrappers.length) {
    mountedWrappers.pop()?.unmount()
  }
  removeStudioToasts()
})

describe('AgentStudio region UI', () => {
  it('shows special CodeBuddy values without selecting a canonical site', async () => {
    mocks.listAgents.mockResolvedValue([agent('ioa')])
    const wrapper = await mountStudio()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')

    expect(wrapper.text()).toContain('现有特殊配置：ioa')
    const trigger = wrapper.get('[data-testid="agent-model-region"] [data-test="app-select-trigger"]')
    expect(trigger.text()).not.toContain('国内站')
    expect(trigger.text()).not.toContain('国际站')
  })

  it('preserves special CodeBuddy region when saving without site interaction', async () => {
    mocks.listAgents.mockResolvedValue([agent('ioa')])
    const wrapper = await mountStudio()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')

    await editCustomEnvironment(wrapper, 'preserve-region')
    await wrapper.findAll('button').find((item) => item.text() === '保存')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        env: { GRASP_CODEBUDDY_REGION: 'ioa', STUDIO_FEATURE: 'preserve-region' },
        layout: expect.objectContaining({ workspaceDir: '/root/workspace' }),
      }),
    )
  })

  it('overwrites special CodeBuddy region after explicitly selecting a site', async () => {
    mocks.listAgents.mockResolvedValue([agent('ioa')])
    const wrapper = await mountStudio()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')

    await wrapper.get('[data-testid="agent-model-region"] [data-test="app-select-trigger"]').trigger('click')
    await flushPromises()
    document.body.querySelector<HTMLButtonElement>('[data-test="app-select-option-internal"]')!.click()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(mocks.saveAgent).toHaveBeenCalledWith(
      expect.objectContaining({ env: { GRASP_CODEBUDDY_REGION: 'internal' } }),
    )
  })

  it('hydrates a missing region without false dirty; later save still writes international default', async () => {
    mocks.listAgents.mockResolvedValue([agent()])
    const wrapper = await mountStudio()
    await flushPromises()

    expect(wrapper.text()).not.toContain('未保存')
    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')
    await editCustomEnvironment(wrapper, 'hydrated-region')
    expect(wrapper.text()).toContain('未保存')
    await wrapper.findAll('button').find((item) => item.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(mocks.saveAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        env: { GRASP_CODEBUDDY_REGION: 'public', STUDIO_FEATURE: 'hydrated-region' },
        layout: expect.objectContaining({ workspaceDir: '/root/workspace' }),
      }),
    )
  })

  it('normalizes Raw JSON conflicts without overriding the ACP site selection', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudio()
    await flushPromises()
    await wrapper.get('[data-studio-tab="env"]').trigger('click')
    await wrapper.findAll('button').find((item) => item.text() === '原始 JSON')!.trigger('click')
    await wrapper.get('[data-test="code-editor"]').setValue(
      JSON.stringify({
        GRASP_CODEBUDDY_REGION: 'internal',
        GRASP_TRAE_REGION: 'cn',
        OTHER: 'ok',
      }),
    )
    await wrapper.findAll('button').find((item) => item.text() === '保存')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        env: { GRASP_CODEBUDDY_REGION: 'public', OTHER: 'ok' },
      }),
    )
  })
})

describe('AgentStudio MCP PM leader prefills', () => {
  async function openMcpTab(wrapper: Awaited<ReturnType<typeof mountStudio>>) {
    await wrapper.findAll('button').find((item) => item.text().startsWith('MCP'))!.trigger('click')
    await flushPromises()
  }

  it('keeps mcp quick-add buttons at text-[11px] including artifact store', async () => {
    mocks.listAgents.mockResolvedValue([agent()])
    const wrapper = await mountStudio()
    await flushPromises()
    await openMcpTab(wrapper)

    const selectors = ['mcp-add-artifact', 'mcp-add-memory', 'mcp-add-context', 'mcp-add-scheduler'] as const
    for (const testId of selectors) {
      const btn = wrapper.get(`[data-test="${testId}"]`)
      expect(btn.classes()).toContain('text-[11px]')
      expect(btn.classes()).toContain('px-2')
      expect(btn.classes()).toContain('py-1')
      expect(btn.classes()).not.toContain('text-xs')
      expect(btn.classes()).not.toContain('py-0.5')
    }
    expect(wrapper.get('[data-test="mcp-add-artifact"]').text()).toBe('+ 添加产物存储')
    expect(wrapper.get('[data-test="mcp-add-memory"]').text()).toBe('+ 添加长期记忆')
    expect(wrapper.get('[data-test="mcp-add-context"]').text()).toBe('+ 添加对话上下文')
    expect(wrapper.get('[data-test="mcp-add-scheduler"]').text()).toBe('+ 添加定时任务')
  })

  it('adds memory-store from the agent platform card without changing artifact-store', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        ...agent(),
        mcp: [
          {
            name: 'artifact-store',
            url: '${GRASP_ARTIFACT_URL}',
            headers: { Authorization: 'Bearer ${GRASP_ARTIFACT_TOKEN}' },
          },
        ],
      },
    ])
    const wrapper = await mountStudio()
    await flushPromises()
    await openMcpTab(wrapper)

    expect(wrapper.text()).not.toContain('Agent 通用平台 MCP')
    expect(wrapper.text()).not.toContain('GRASP_MEMORY_URL')
    expect(wrapper.text()).not.toContain('pm-progress')
    expect(wrapper.text()).not.toContain('整份 mcp.json 由你配置')
    expect(wrapper.text()).not.toContain('运行级变量(运行时替换')
    expect(wrapper.get('[data-test="mcp-help-link"]').text()).toBe('帮助')
    expect(wrapper.get('[data-test="mcp-add-memory"]').text()).toContain('添加长期记忆')
    expect(wrapper.get('[data-test="mcp-add-memory"]').classes()).toContain('text-[11px]')
    expect(wrapper.get('[data-test="mcp-add-context"]').classes()).toContain('text-[11px]')
    expect(wrapper.get('[data-test="mcp-add-scheduler"]').classes()).toContain('text-[11px]')
    expect(wrapper.find('[data-test="mcp-add-artifact"]').exists()).toBe(false)
    expect(wrapper.get('[data-mcp-name="artifact-store"] [data-test="mcp-display-name"]').text()).toBe('产物存储')
    expect(wrapper.get('[data-mcp-name="artifact-store"] [data-test="mcp-preset-key"]').text()).toBe('artifact-store')
    expect(wrapper.get('[data-mcp-name="artifact-store"] [data-test="mcp-scope-note"]').text()).toContain('本次运行隔离的产物服务')

    await wrapper.get('[data-test="mcp-add-memory"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-mcp-name="memory-store"]').exists()).toBe(true)
    expect(wrapper.get('[data-mcp-name="memory-store"] [data-test="mcp-display-name"]').text()).toBe('长期记忆')
    expect(wrapper.get('[data-mcp-name="memory-store"] [data-test="mcp-preset-key"]').text()).toBe('memory-store')
    expect(wrapper.findAll('input').filter((el) => (el.element as HTMLInputElement).value === '${GRASP_MEMORY_URL}')).toHaveLength(1)
    expect(wrapper.findAll('input').filter((el) => (el.element as HTMLInputElement).value === 'Bearer ${GRASP_MEMORY_TOKEN}')).toHaveLength(1)
    expect(wrapper.get('[data-mcp-name="memory-store"] [data-test="mcp-scope-note"]').text()).toContain('长期记忆，归属主工作区')
    expect(wrapper.find('[data-mcp-name="artifact-store"]').exists()).toBe(true)
    expect(wrapper.findAll('input').filter((el) => (el.element as HTMLInputElement).value === 'memory-store')).toHaveLength(0)
    expect(wrapper.findAll('input').filter((el) => (el.element as HTMLInputElement).value === 'artifact-store')).toHaveLength(0)
  })

  it('toasts and keeps a single memory-store when adding again', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        ...agent(),
        mcp: [
          {
            name: 'memory-store',
            url: '${GRASP_MEMORY_URL}',
            headers: { Authorization: 'Bearer ${GRASP_MEMORY_TOKEN}' },
          },
        ],
      },
    ])
    const wrapper = await mountStudio()
    await flushPromises()
    await openMcpTab(wrapper)

    const addBtn = wrapper.get('[data-test="mcp-add-memory"]')
    expect(addBtn.text()).toContain('添加长期记忆')
    await addBtn.trigger('click')
    await flushPromises()

    expect(document.body.textContent || '').toContain('已存在约定名 memory-store')
    expect(wrapper.findAll('[data-mcp-name="memory-store"]')).toHaveLength(1)
  })

  it('drops platform display after renaming memory-store in raw JSON', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        ...agent(),
        mcp: [
          {
            name: 'memory-store',
            url: '${GRASP_MEMORY_URL}',
            headers: { Authorization: 'Bearer ${GRASP_MEMORY_TOKEN}' },
          },
        ],
      },
    ])
    const wrapper = await mountStudio()
    await flushPromises()
    await openMcpTab(wrapper)

    expect(wrapper.find('[data-mcp-name="memory-store"] [data-test="mcp-scope-note"]').exists()).toBe(true)
    expect(wrapper.get('[data-mcp-name="memory-store"] [data-test="mcp-display-name"]').text()).toBe('长期记忆')
    await wrapper.findAll('button').find((item) => item.text() === '原始 JSON')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="mcp-help-link"]').exists()).toBe(false)
    await wrapper.get('[data-test="code-editor"]').setValue(
      JSON.stringify([
        {
          name: 'memory_store',
          url: '${GRASP_MEMORY_URL}',
          headers: { Authorization: 'Bearer ${GRASP_MEMORY_TOKEN}' },
        },
      ]),
    )
    await wrapper.findAll('button').find((item) => item.text() === '表单编辑')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-mcp-name="memory-store"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="mcp-scope-note"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="mcp-custom-name"]').element).toMatchObject({ value: 'memory_store' })
    expect(wrapper.find('[data-test="mcp-help-link"]').exists()).toBe(true)
  })

  it('removing memory-store keeps artifact-store intact', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        ...agent(),
        mcp: [
          {
            name: 'artifact-store',
            url: '${GRASP_ARTIFACT_URL}',
            headers: { Authorization: 'Bearer ${GRASP_ARTIFACT_TOKEN}' },
          },
          {
            name: 'memory-store',
            url: '${GRASP_MEMORY_URL}',
            headers: { Authorization: 'Bearer ${GRASP_MEMORY_TOKEN}' },
          },
        ],
      },
    ])
    const wrapper = await mountStudio()
    await flushPromises()
    await openMcpTab(wrapper)

    await wrapper.get('[data-mcp-name="memory-store"] [data-test="mcp-remove"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-mcp-name="memory-store"]').exists()).toBe(false)
    expect(wrapper.find('[data-mcp-name="artifact-store"]').exists()).toBe(true)
  })
})

describe('AgentStudio MCP config help', () => {
  const HelpAppModalStub = defineComponent({
    props: { open: Boolean, title: String, width: Number },
    emits: ['close'],
    template:
      '<div v-if="open" data-test="help-modal" :data-width="width">' +
      '<h2>{{ title }}</h2><div data-test="help-scroll"><slot /></div><slot name="footer" />' +
      '</div>',
  })

  async function mountStudioWithMcpHelp() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: HelpAppModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: true,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
  }

  async function openMcpTab(wrapper: Awaited<ReturnType<typeof mountStudioWithMcpHelp>>) {
    await wrapper.findAll('button').find((item) => item.text().startsWith('MCP'))!.trigger('click')
    await flushPromises()
  }

  it('hides variable docs until help opens on run, then agent chip shows platform copy', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        ...agent(),
        mcp: [
          {
            name: 'artifact-store',
            url: '${GRASP_ARTIFACT_URL}',
            headers: { Authorization: 'Bearer ${GRASP_ARTIFACT_TOKEN}' },
          },
        ],
      },
    ])
    const wrapper = await mountStudioWithMcpHelp()
    await flushPromises()
    await openMcpTab(wrapper)

    expect(wrapper.get('[data-test="mcp-help-link"]').text()).toBe('帮助')
    expect(wrapper.text()).not.toContain('整份 mcp.json 由你配置')
    expect(wrapper.text()).not.toContain('Agent 通用平台 MCP')
    expect(wrapper.text()).not.toContain('GRASP_MEMORY_URL')
    expect(wrapper.text()).not.toContain('pm-progress')

    await wrapper.get('[data-test="mcp-help-link"]').trigger('click')
    await flushPromises()
    const modal = wrapper.get('[data-test="help-modal"]')
    expect(modal.attributes('data-width')).toBe('640')
    expect(modal.text()).toContain('MCP 配置帮助')
    expect(modal.text()).toContain('整份 mcp.json 由你配置')
    expect(modal.text()).toContain('/root/.codebuddy/mcp.json')
    expect(modal.text()).toContain('GRASP_ARTIFACT_URL')
    expect(wrapper.find('[data-test="mcp-help-run"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="mcp-help-agent"]').exists()).toBe(false)
    expect(wrapper.get('[data-help-chip="run"]').classes().join(' ')).toContain('border-accent')

    await wrapper.get('[data-test="mcp-help-chip-agent"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="mcp-help-agent"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="mcp-help-run"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="help-modal"]').text()).toContain('GRASP_MEMORY_URL')
    expect(wrapper.get('[data-test="help-modal"]').text()).toContain('pm-progress')
    expect(wrapper.get('[data-test="help-modal"]').text()).not.toContain('+ 添加长期记忆')
  })

  it('hides help in raw JSON and keeps mcp draft after closing help', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        ...agent(),
        mcp: [
          {
            name: 'custom-draft',
            url: 'https://example.test/sse',
          },
        ],
      },
    ])
    const wrapper = await mountStudioWithMcpHelp()
    await flushPromises()
    await openMcpTab(wrapper)

    const nameInput = wrapper.get('[data-test="mcp-custom-name"]')
    await nameInput.setValue('custom-kept')
    expect((nameInput.element as HTMLInputElement).value).toBe('custom-kept')

    await wrapper.findAll('button').find((item) => item.text() === '原始 JSON')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="mcp-help-link"]').exists()).toBe(false)

    await wrapper.findAll('button').find((item) => item.text() === '表单编辑')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="mcp-help-link"]').exists()).toBe(true)

    await wrapper.get('[data-test="mcp-help-link"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="help-modal"]').exists()).toBe(true)
    await wrapper.get('[data-test="mcp-help-got-it"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="help-modal"]').exists()).toBe(false)
    expect((wrapper.get('[data-test="mcp-custom-name"]').element as HTMLInputElement).value).toBe('custom-kept')
  })
})

describe('AgentStudio list context actions', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })

  async function mountRenameStudio() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: ModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: true,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
  }

  async function openRowAction(wrapper: Awaited<ReturnType<typeof mountRenameStudio>>, name: string, action: 'rename' | 'delete') {
    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 36, clientY: 48 })
    wrapper.get(`[data-resource-key="${name}"]`).element.dispatchEvent(event)
    await flushPromises()
    expect(event.defaultPrevented).toBe(true)
    const item = document.querySelector<HTMLButtonElement>(`[data-testid="agent-row-${action}"]`)
    expect(item).toBeTruthy()
    item!.click()
    await flushPromises()
  }

  it('does not render Agent management and keeps the header name read-only', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    expect(wrapper.text()).not.toContain('Agent 管理')
    expect(wrapper.find('[data-test="manage-search"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agent-tree-manage"]').exists()).toBe(false)

    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('名称请在 Agent 管理中重命名')
    expect(wrapper.text()).not.toContain('Rename this agent in Agent management')
    expect(wrapper.get('[data-testid="agent-switcher-trigger"]').text()).toBe('legacy')
  })

  it('keeps a lock on canonical agents and does not open rename or delete for them', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agent('public'), name: '需求澄清' },
      { ...agent('public'), name: '自建' },
    ])
    const wrapper = await mountRenameStudio()
    await flushPromises()
    expect(wrapper.get('[data-resource-key="需求澄清"]').get('[data-testid="resource-lock"]').attributes('title')).toContain('不能删除或重命名')
    const locked = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 20, clientY: 20 })
    wrapper.get('[data-resource-key="需求澄清"]').element.dispatchEvent(locked)
    await flushPromises()
    expect(locked.defaultPrevented).toBe(true)
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()
    expect(wrapper.find('[data-test="modal"]').exists()).toBe(false)

    await openRowAction(wrapper, '自建', 'rename')
    expect(wrapper.text()).toContain('重命名 Agent')
    expect(wrapper.find('[data-test="modal"] input').element).toHaveProperty('value', '自建')
  })

  it('rename from the row menu shows the cascade hint and cancel does not rename', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountRenameStudio()
    await flushPromises()
    expect(wrapper.get('[data-resource-key="legacy"]').attributes('aria-current')).toBe('true')

    await openRowAction(wrapper, 'legacy', 'rename')
    expect(wrapper.get('[data-resource-key="legacy"]').attributes('aria-current')).toBe('true')
    expect(wrapper.text()).not.toContain('工作流引用不会自动更新')
    expect(wrapper.text()).toContain('将同步更新目录、工作区助手，以及工作流中的 Agent 配置引用')
    await wrapper.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    await flushPromises()
    expect(mocks.renameAgent).not.toHaveBeenCalled()
  })

  it('renaming a row that is not selected does not switch the open agent', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agent('public'), name: 'legacy' },
      { ...agent('public'), name: 'other' },
    ])
    mocks.renameAgent.mockResolvedValueOnce({ ...agent('public'), name: 'other2', updatedWorkflowCount: 0 })
    const wrapper = await mountRenameStudio()
    await flushPromises()
    expect(wrapper.get('[data-resource-key="legacy"]').attributes('aria-current')).toBe('true')

    await openRowAction(wrapper, 'other', 'rename')
    const input = wrapper.findAll('input').find((el) => (el.element as HTMLInputElement).value === 'other')!
    await input.setValue('other2')
    await wrapper.findAll('button').find((b) => b.text() === '确定')!.trigger('click')
    await flushPromises()

    expect(mocks.renameAgent).toHaveBeenCalledWith('other', 'other2')
    expect(wrapper.get('[data-resource-key="legacy"]').attributes('aria-current')).toBe('true')
    expect(wrapper.get('[data-resource-key="other2"]').attributes('aria-current')).toBeUndefined()
    expect(wrapper.get('[data-testid="agent-switcher-trigger"]').text()).toBe('legacy')
  })

  it('rename success toast shows workflow count when N>0 and omits count when N=0', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    mocks.renameAgent
      .mockResolvedValueOnce({ ...agent('public'), name: 'legacy2', updatedWorkflowCount: 3 })
      .mockResolvedValueOnce({ ...agent('public'), name: 'legacy3', updatedWorkflowCount: 0 })

    const wrapper = await mountRenameStudio()
    await flushPromises()

    const renameInput = () =>
      wrapper.findAll('input').find((el) => {
        const node = el.element as HTMLInputElement
        return node.type !== 'file' && (node.value === 'legacy' || node.value === 'legacy2' || node.value === 'legacy3')
      })!

    await openRowAction(wrapper, 'legacy', 'rename')
    await renameInput().setValue('legacy2')
    await wrapper.findAll('button').find((b) => b.text() === '确定')!.trigger('click')
    await flushPromises()
    expect(mocks.renameAgent).toHaveBeenCalledWith('legacy', 'legacy2')
    expect(document.body.textContent || '').toContain('改名成功，已更新 3 个工作流')
    expect(document.body.textContent || '').not.toContain('已更新 0 个工作流')

    await openRowAction(wrapper, 'legacy2', 'rename')
    await renameInput().setValue('legacy3')
    await wrapper.findAll('button').find((b) => b.text() === '确定')!.trigger('click')
    await flushPromises()
    expect(mocks.renameAgent).toHaveBeenCalledWith('legacy2', 'legacy3')
    const body = document.body.textContent || ''
    expect(body).toContain('改名成功')
    expect(body).not.toContain('已更新 0 个工作流')
    expect(body).not.toContain('已更新 3 个工作流')
  })

  it('delete confirm names the agent, and deleting the open agent clears selection', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agent('public'), name: 'legacy' },
      { ...agent('public'), name: 'other' },
    ])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await openRowAction(wrapper, 'other', 'delete')
    expect(wrapper.text()).toContain('other')
    expect(wrapper.text()).toContain('不可恢复')
    const confirm = wrapper.findAll('button').find((b) => b.text() === '删除')!
    expect(confirm.attributes('variant')).toBe('danger')
    await wrapper.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    await flushPromises()
    expect(mocks.deleteAgent).not.toHaveBeenCalled()
    expect(wrapper.get('[data-resource-key="other"]').exists()).toBe(true)

    await openRowAction(wrapper, 'legacy', 'delete')
    await wrapper.findAll('button').find((b) => b.text() === '删除')!.trigger('click')
    await flushPromises()
    expect(mocks.deleteAgent).toHaveBeenCalledWith('legacy')
    expect(wrapper.find('[data-resource-key="legacy"]').exists()).toBe(false)
    expect(wrapper.get('[data-resource-key="other"]').attributes('aria-current')).toBeUndefined()
    expect(wrapper.text()).toContain('未选中 Agent。')
    expect(wrapper.get('[data-testid="agent-switcher-trigger"]').text()).toBe('选择智能体')
  })
})

describe('AgentStudio mobile core path', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const MdStub = defineComponent({
    props: { modelValue: String, filePath: String, variant: String },
    emits: ['update:modelValue'],
    template:
      '<div data-test="md-editor" :data-variant="variant">' +
      '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' +
      '</div>',
  })

  function agentWithFiles(): Agent {
    return {
      ...agent('public'),
      files: [
        { path: 'AGENTS.md', content: '# hello\n' },
        { path: 'rules/system.md', content: '---\ntitle: system\n---\n\n# rule\n' },
      ],
    }
  }

  async function mountMobileStudio() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: ModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: MdStub,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
  }

  beforeEach(() => {
    breakpointMocks.isMobile.value = true
  })

  async function openSystemMd(wrapper: Awaited<ReturnType<typeof mountMobileStudio>>) {
    await wrapper.findAll('button').find((b) => b.text().includes('rules'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text().includes('system.md'))!.trigger('click')
    await flushPromises()
  }

  it('starts on files list step without side-by-side editor', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    expect(wrapper.text()).toContain('资源管理器')
    expect(wrapper.find('[data-test="md-editor"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('AGENTS.md')
    expect(wrapper.text()).toContain('rules')
  })

  it('opens file into edit step with stack markdown editor', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)

    expect(wrapper.text()).toContain('返回')
    const md = wrapper.get('[data-test="md-editor"]')
    expect(md.attributes('data-variant')).toBe('stack')
    expect(wrapper.text()).toContain('rules/system.md')
  })

  it('stays on edit step after save and shows saved state', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)

    const ta = wrapper.get('[data-test="md-editor"] textarea')
    await ta.setValue('---\ntitle: system\n---\n\n# changed\n')
    await flushPromises()
    expect(wrapper.text()).toContain('未保存')

    await wrapper.findAll('button').find((b) => b.text() === '保存')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).toHaveBeenCalled()
    expect(wrapper.text()).toContain('返回')
    expect(wrapper.find('[data-test="md-editor"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('已保存')
  })

  it('prompts save/discard/cancel when returning dirty from edit', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)
    await wrapper.get('[data-test="md-editor"] textarea').setValue('# dirty\n')
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('返回'))!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('有未保存的修改')
    expect(wrapper.text()).toContain('保存并返回')
    expect(wrapper.text()).toContain('丢弃修改')

    await wrapper.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="md-editor"]').exists()).toBe(true)
  })

  it('keeps leave confirm open when save fails', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    mocks.saveAgent.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)
    await wrapper.get('[data-test="md-editor"] textarea').setValue('# dirty\n')
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('返回'))!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('保存并返回')

    await wrapper.findAll('button').find((b) => b.text() === '保存并返回')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('有未保存的修改')
    expect(wrapper.text()).toContain('保存并返回')
    expect(wrapper.find('[data-test="md-editor"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('save failed')
  })

  it('prompts when switching tab from dirty edit step', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)
    await wrapper.get('[data-test="md-editor"] textarea').setValue('# dirty\n')
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().startsWith('MCP'))!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('有未保存的修改')
    expect(wrapper.text()).toContain('保存并继续')
    await wrapper.findAll('button').find((b) => b.text() === '丢弃修改')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('建议在桌面使用')
  })

  it('shows desktop-only tip for non-core tabs including meta', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '元信息')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('建议在桌面使用')
    expect(wrapper.text()).not.toContain('ACP 后端')
    expect(wrapper.find('[data-testid="studio-mobile-back-files"]').exists()).toBe(true)
    await wrapper.get('[data-testid="studio-mobile-back-files"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('建议在桌面使用')
  })

  it('mounts data panel on mobile instead of desktop-only tip', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '数据')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('建议在桌面使用')
    expect(wrapper.find('agent-data-panel-stub').exists()).toBe(true)
  })

  it('keeps mcp/env/capabilities desktop-only while data is allowed', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    for (const label of ['MCP', '环境变量', '能力']) {
      const btn = wrapper.findAll('button').find((b) => b.text().startsWith(label))
      expect(btn).toBeTruthy()
      await btn!.trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('建议在桌面使用')
      expect(wrapper.find('agent-data-panel-stub').exists()).toBe(false)
    }
  })

  it('mounts chat test on mobile instead of desktop-only tip (g2.3)', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '对话测试')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('建议在桌面使用')
    expect(wrapper.find('[data-testid="studio-chat-test"]').exists()).toBe(true)
    expect(wrapper.find('agent-chat-tester-stub').exists()).toBe(true)
  })

  it('deep-links to chat test with current agent profile (g2.2)', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      { ...agentWithFiles(), name: 'beta' },
    ])
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter({ agent: 'alpha', studioTab: 'test' })
    const wrapper = trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: true,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: true,
            ExplorerContextMenu: true,
            AgentChatTester: {
              props: ['profile', 'createTest'],
              template:
                '<div data-testid="studio-chat-tester-stub">{{ profile }}|{{ typeof createTest }}</div>',
            },
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
    await flushPromises()

    expect(wrapper.find('[data-testid="studio-chat-test"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="studio-chat-tester-stub"]').text()).toBe(
      'alpha|function',
    )
    // Switching left Agent remounts tester via :key="activeName" (s3).
    const viewSrc = readFileSync(fileURLToPath(import.meta.url).replace(/\.test\.ts$/, '.vue'), 'utf8')
    expect(viewSrc).toMatch(/<AgentChatTester[\s\S]*?:key="activeName"/)
  })

  it('deep-links to data sub-tab on mobile without desktop-only tip', async () => {
    mocks.listAgents.mockResolvedValue([{ ...agentWithFiles(), name: 'alpha' }])
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter({ agent: 'alpha', studioTab: 'data', sub: 'jobs' })
    const wrapper = trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: ModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: MdStub,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
    await flushPromises()

    expect(wrapper.text()).not.toContain('建议在桌面使用')
    const panel = wrapper.find('agent-data-panel-stub')
    expect(panel.exists()).toBe(true)
    expect(panel.attributes('sub-tab') || panel.attributes('subtab')).toBeTruthy()
    wrapper.unmount()
  })

  it('deep-links to capabilities on mobile with desktop-only empty state and back to Files', async () => {
    mocks.listAgents.mockResolvedValue([{ ...agentWithFiles(), name: 'alpha' }])
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter({ agent: 'alpha', studioTab: 'capabilities' })
    const wrapper = trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: ModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: MdStub,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
    await flushPromises()
    expect(wrapper.text()).toContain('建议在桌面使用')
    expect(wrapper.find('[data-testid="studio-mobile-back-files"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows switch entry and opens the sidebar sheet tree', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      { ...agentWithFiles(), name: 'beta' },
    ])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    const switchBtn = wrapper.get('[data-test="sidebar-switch"]')
    expect(switchBtn.text()).toContain('切换')
    await switchBtn.trigger('click')
    await flushPromises()

    const sheet = document.querySelector('[data-test="sidebar-sheet"]')
    expect(sheet).toBeTruthy()
    expect(sheet!.textContent).toContain('Agent')
    expect(sheet!.querySelector('[data-resource-key="alpha"]')).toBeTruthy()
    expect(sheet!.querySelector('[data-resource-key="beta"]')).toBeTruthy()
    wrapper.unmount()
  })

  function sheetAgent(name: string) {
    return document.querySelector(
      `[data-test="sidebar-sheet"] [data-resource-key="${name}"]`,
    ) as HTMLElement
  }

  it('switches agent without dirty and closes sheet', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      {
        ...agentWithFiles(),
        name: 'beta',
        files: [{ path: 'README.md', content: '# beta\n' }],
      },
    ])
    const wrapper = await mountMobileStudio()
    await flushPromises()
    expect(wrapper.text()).toContain('alpha')

    await wrapper.get('[data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = sheetAgent('beta')
    expect(betaBtn).toBeTruthy()
    betaBtn.click()
    await flushPromises()

    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeNull()
    expect(wrapper.text()).toContain('beta')
    expect(wrapper.text()).toContain('README.md')
    wrapper.unmount()
  })

  it('dirty switch shows three actions; cancel keeps sheet and dirty state', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      { ...agentWithFiles(), name: 'beta' },
    ])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)
    await wrapper.get('[data-test="md-editor"] textarea').setValue('# dirty\n')
    await flushPromises()
    expect(wrapper.text()).toContain('未保存')

    await wrapper.get('[data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = sheetAgent('beta')
    betaBtn.click()
    await flushPromises()

    expect(wrapper.text()).toContain('保存并切换')
    expect(wrapper.text()).toContain('丢弃并切换')
    expect(wrapper.text()).toContain('取消')
    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeTruthy()

    await wrapper.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    await flushPromises()

    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeTruthy()
    expect(wrapper.text()).toContain('未保存')
    expect(wrapper.text()).toContain('alpha')
    wrapper.unmount()
  })

  it('dirty switch save-and-switch closes sheet and selects target agent', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      {
        ...agentWithFiles(),
        name: 'beta',
        files: [{ path: 'README.md', content: '# beta\n' }],
      },
    ])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)
    await wrapper.get('[data-test="md-editor"] textarea').setValue('# dirty save\n')
    await flushPromises()
    expect(wrapper.text()).toContain('未保存')

    await wrapper.get('[data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = sheetAgent('beta')
    betaBtn.click()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '保存并切换')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).toHaveBeenCalled()
    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeNull()
    expect(wrapper.text()).toContain('beta')
    expect(wrapper.text()).toContain('README.md')
    expect(wrapper.text()).not.toContain('未保存')
    wrapper.unmount()
  })

  it('dirty switch discard-and-switch closes sheet and selects target agent', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      {
        ...agentWithFiles(),
        name: 'beta',
        files: [{ path: 'README.md', content: '# beta\n' }],
      },
    ])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await openSystemMd(wrapper)
    await wrapper.get('[data-test="md-editor"] textarea').setValue('# dirty discard\n')
    await flushPromises()
    expect(wrapper.text()).toContain('未保存')

    await wrapper.get('[data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = sheetAgent('beta')
    betaBtn.click()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '丢弃并切换')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).not.toHaveBeenCalled()
    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeNull()
    expect(wrapper.text()).toContain('beta')
    expect(wrapper.text()).toContain('README.md')
    wrapper.unmount()
  })

  it('sidebar sheet has no manage button and no rename or delete menu', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.get('[data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    const sheet = document.querySelector('[data-test="sidebar-sheet"]')!
    expect(sheet.querySelector('[data-test="sidebar-sheet-manage"]')).toBeNull()
    expect(sheet.querySelector('[data-test="sidebar-sheet-import"]')).toBeTruthy()
    expect(sheet.querySelector('[data-test="sidebar-sheet-create-agent"]')).toBeTruthy()
    expect(sheet.textContent).not.toContain('Agent 管理')
    const row = sheet.querySelector('[data-resource-key]')!
    row.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()
    expect(sheet.textContent).not.toContain('重命名')
    expect(sheet.textContent).not.toContain('删除')
    wrapper.unmount()
  })

  it('keeps a single more trigger visible without hover on mobile', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    expect(wrapper.findAll('[data-test="file-row-action"]').length).toBe(0)
    const mores = wrapper.findAll('[data-test="file-row-more"]')
    expect(mores.length).toBeGreaterThan(0)
    for (const btn of mores) {
      const cls = btn.classes().join(' ')
      expect(cls).toMatch(/min-h-11/)
      expect(cls).toMatch(/min-w-11/)
      expect(cls).not.toContain('opacity-0')
    }

    await wrapper.get('[data-test="file-row-more"][data-path="rules"]').trigger('click')
    await flushPromises()
    const folderMenu = document.querySelector('[data-test="explorer-more-menu"]')
    expect(folderMenu).toBeTruthy()
    const folderActions = Array.from(folderMenu!.querySelectorAll('[data-test="explorer-more-item"]')).map(
      (el) => el.getAttribute('data-action'),
    )
    expect(folderActions).toEqual(['newFile', 'newFolder', 'rename'])

    await wrapper.get('[data-test="file-row-more"][data-path="AGENTS.md"]').trigger('click')
    await flushPromises()
    const fileMenu = document.querySelector('[data-test="explorer-more-menu"]')
    expect(fileMenu).toBeTruthy()
    const fileActions = Array.from(fileMenu!.querySelectorAll('[data-test="explorer-more-item"]')).map(
      (el) => el.getAttribute('data-action'),
    )
    expect(fileActions).toEqual(['rename', 'delete'])
    wrapper.unmount()
  })

  it('closes sidebar sheet when breakpoint flips to desktop', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.get('[data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeTruthy()

    breakpointMocks.isMobile.value = false
    await nextTick()
    await flushPromises()
    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeNull()
    wrapper.unmount()
  })

  it('keeps switch and exposes import/new via sidebar sheet header', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    expect(wrapper.find('[data-test="sidebar-switch"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-studio-action-row"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('配置可复用的 Agent')
    expect(wrapper.text()).not.toContain('复制进沙箱')

    await wrapper.get('[data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeTruthy()
    expect(document.querySelector('[data-test="sidebar-sheet-import"]')).toBeTruthy()
    expect(document.querySelector('[data-test="sidebar-sheet-create-agent"]')).toBeTruthy()
    wrapper.unmount()
  })
})

describe('AgentStudio mobile chrome', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const MdStub = defineComponent({
    props: { modelValue: String, filePath: String, variant: String },
    emits: ['update:modelValue'],
    template:
      '<div data-test="md-editor" :data-variant="variant">' +
      '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' +
      '</div>',
  })

  function agentWithFiles(): Agent {
    return {
      ...agent('public'),
      files: [
        { path: 'AGENTS.md', content: '# hello\n' },
        { path: 'rules/system.md', content: '---\ntitle: system\n---\n\n# rule\n' },
      ],
    }
  }

  async function mountMobileStudio() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: ModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: MdStub,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
  }

  beforeEach(() => {
    breakpointMocks.isMobile.value = true
  })

  it('splits name bar into two rows and hides disabled saved button when clean', async () => {
    mocks.listAgents.mockResolvedValue([{ ...agentWithFiles(), name: 'Grasp代办助手' }])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    expect(wrapper.find('[data-test="studio-name-row-top"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="studio-name-row-bottom"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="agent-name"]').text()).toContain('Grasp代办助手')
    expect(wrapper.get('[data-test="sidebar-switch"]').classes().join(' ')).toMatch(/min-h-11/)
    expect(wrapper.get('[data-test="studio-export"]').classes().join(' ')).toMatch(/min-h-11/)
    expect(wrapper.find('[data-test="studio-save"]').exists()).toBe(false)
    expect(wrapper.findAll('button').filter((b) => b.text() === '已保存').length).toBe(0)
    wrapper.unmount()
  })

  it('shows unsaved chip and save on dirty two-row bar', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('rules'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text().includes('system.md'))!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="md-editor"] textarea').setValue('# dirty chrome\n')
    await flushPromises()

    expect(wrapper.get('[data-test="studio-name-row-top"]').text()).toContain('未保存')
    const save = wrapper.get('[data-test="studio-save"]')
    expect(save.text()).toContain('保存')
    expect(save.classes().join(' ')).toMatch(/min-h-11/)
    wrapper.unmount()
  })

  it('opens full name tip only when the name is truncated', async () => {
    mocks.listAgents.mockResolvedValue([{ ...agentWithFiles(), name: 'Grasp代办助手超长名称' }])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    const nameBtn = wrapper.get('[data-test="agent-name"]')
    await nameBtn.trigger('click')
    await nextTick()
    expect(document.querySelector('[data-test="agent-name-tip"]')).toBeNull()

    Object.defineProperty(nameBtn.element, 'scrollWidth', { configurable: true, get: () => 240 })
    Object.defineProperty(nameBtn.element, 'clientWidth', { configurable: true, get: () => 80 })
    await nameBtn.trigger('click')
    await nextTick()
    const tip = document.querySelector('[data-test="agent-name-tip"]')
    expect(tip).toBeTruthy()
    expect(tip!.textContent).toContain('Grasp代办助手超长名称')
    expect(tip!.textContent).toContain('完整名称')

    ;(document.querySelector('[data-test="agent-name-tip-backdrop"]') as HTMLElement).click()
    await nextTick()
    expect(document.querySelector('[data-test="agent-name-tip"]')).toBeNull()
    wrapper.unmount()
  })

  it('shows tab edge fades from scrollLeft and hides them at the ends', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    const strip = wrapper.get('[data-test="studio-tab-strip"]')
    const el = strip.element as HTMLElement
    let scrollLeft = 0
    Object.defineProperty(el, 'scrollWidth', { configurable: true, get: () => 900 })
    Object.defineProperty(el, 'clientWidth', { configurable: true, get: () => 320 })
    Object.defineProperty(el, 'scrollLeft', {
      configurable: true,
      get: () => scrollLeft,
      set: (v: number) => {
        scrollLeft = v
      },
    })

    await strip.trigger('scroll')
    await nextTick()
    expect(wrapper.find('[data-test="tab-fade-right"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="tab-fade-left"]').exists()).toBe(false)

    scrollLeft = 40
    await strip.trigger('scroll')
    await nextTick()
    expect(wrapper.find('[data-test="tab-fade-left"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="tab-fade-right"]').exists()).toBe(true)

    scrollLeft = 580
    await strip.trigger('scroll')
    await nextTick()
    expect(wrapper.find('[data-test="tab-fade-right"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="tab-fade-left"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps more menu items at least 44px and mutually exclusive with full name tip', async () => {
    mocks.listAgents.mockResolvedValue([{ ...agentWithFiles(), name: 'Grasp代办助手超长名称' }])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.get('[data-test="file-row-more"][data-path="rules"]').trigger('click')
    await flushPromises()
    const items = Array.from(document.querySelectorAll('[data-test="explorer-more-item"]'))
    expect(items.length).toBeGreaterThan(0)
    for (const item of items) {
      expect((item as HTMLElement).className).toMatch(/min-h-11/)
    }

    const nameBtn = wrapper.get('[data-test="agent-name"]')
    Object.defineProperty(nameBtn.element, 'scrollWidth', { configurable: true, get: () => 240 })
    Object.defineProperty(nameBtn.element, 'clientWidth', { configurable: true, get: () => 80 })
    await nameBtn.trigger('click')
    await nextTick()
    expect(document.querySelector('[data-test="explorer-more-menu"]')).toBeNull()
    expect(document.querySelector('[data-test="agent-name-tip"]')).toBeTruthy()
    wrapper.unmount()
  })
})

describe('AgentStudio desktop chrome unchanged', () => {
  function agentWithFiles(): Agent {
    return {
      ...agent('public'),
      files: [
        { path: 'AGENTS.md', content: '# hello\n' },
        { path: 'rules/system.md', content: '---\ntitle: system\n---\n\n# rule\n' },
      ],
    }
  }

  it('keeps single-row name bar, disabled save, and hover row actions', async () => {
    breakpointMocks.isMobile.value = false
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountStudio()
    await flushPromises()

    expect(wrapper.find('[data-test="studio-name-row-top"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="studio-name-row-bottom"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="sidebar-switch"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="file-row-more"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="tab-fade-right"]').exists()).toBe(false)
    const save = wrapper.get('[data-test="studio-save"]')
    expect(save.text()).toContain('已保存')
    expect(save.attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('[data-test="file-row-action"]').length).toBeGreaterThan(0)
    wrapper.unmount()
  })
})

describe('AgentStudio copy removal (subtitle + toolbar)', () => {
  const subtitleZh = ['配置可复用的 Agent', '复制进沙箱', '/root/.cursor']
  const subtitleEn = ['Reusable agents', 'copied to', '/root/.cursor']
  const demoShell = [
    '可复用 Agent 配置 · 组织与 Agent 配置引用',
    'clearToast',
  ]

  async function mountStudioEn() {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { ...enCommon, ...enPages } },
    })
    const router = await createStudioRouter()
    return mount(AgentStudioView, {
      global: {
        plugins: [i18n, router],
        stubs: {
          AppButton: ButtonStub,
          Icon: true,
          AppModal: true,
          CodeEditor: CodeEditorStub,
          MarkdownSplitEditor: true,
          ExplorerContextMenu: true,
          AgentChatTester: true,
          AgentGitGuide: true,
          AgentCreateWizard: true,
          AgentSidebar: {
            template:
              '<div data-test="sidebar">' +
              '<button data-testid="agent-tree-import" aria-label="Import">Import</button>' +
              '<button data-testid="agent-tree-create-agent" aria-label="New agent">New agent</button>' +
              '</div>',
          },
          AgentDataPanel: true,
        },
      },
    })
  }

  async function mountStudioWithSidebar() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return mount(AgentStudioView, {
      global: {
        plugins: [i18n, router],
        stubs: {
          AppButton: ButtonStub,
          Icon: true,
          AppModal: true,
          CodeEditor: CodeEditorStub,
          MarkdownSplitEditor: true,
          ExplorerContextMenu: true,
          AgentChatTester: true,
          AgentGitGuide: true,
          AgentCreateWizard: true,
          AgentSidebar: {
            template:
              '<div data-test="sidebar">' +
              '<button data-testid="agent-tree-import" aria-label="导入">导入</button>' +
              '<button data-testid="agent-tree-create-agent" aria-label="新建 Agent">新建 Agent</button>' +
              '</div>',
          },
          AgentDataPanel: true,
        },
      },
    })
  }

  it('hides zh subtitle, drops action row, and keeps import/new on sidebar header', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudioWithSidebar()
    await flushPromises()

    const text = wrapper.text()
    for (const s of subtitleZh) expect(text).not.toContain(s)
    for (const s of demoShell) expect(text).not.toContain(s)
    expect(text).not.toMatch(/改前|改后/)
    expect(wrapper.findAll('h1,h2').some((el) => /Agent\s*(管理|Studio)/i.test(el.text()))).toBe(false)

    expect(wrapper.find('[data-testid="agent-studio-action-row"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agent-tree-import"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-tree-create-agent"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-tree-import"]').attributes('aria-label')).toBe('导入')
    expect(wrapper.find('[data-testid="agent-tree-create-agent"]').attributes('aria-label')).toBe('新建 Agent')
    wrapper.unmount()
  })

  it('hides en subtitle and keeps Import / New agent via sidebar', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudioEn()
    await flushPromises()

    const text = wrapper.text()
    for (const s of subtitleEn) expect(text).not.toContain(s)
    expect(wrapper.find('[data-testid="agent-studio-action-row"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agent-tree-import"]').attributes('aria-label')).toBe('Import')
    expect(wrapper.find('[data-testid="agent-tree-create-agent"]').attributes('aria-label')).toBe('New agent')
    wrapper.unmount()
  })
})

describe('AgentStudio remaining hints', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const SidebarStub = defineComponent({
    emits: ['select'],
    template: '<div data-test="sidebar"></div>',
  })

  async function mountHintStudio() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return mount(AgentStudioView, {
      global: {
        plugins: [i18n, router],
        stubs: {
          AppButton: ButtonStub,
          Icon: true,
          AppModal: ModalStub,
          CodeEditor: CodeEditorStub,
          MarkdownSplitEditor: true,
          ExplorerContextMenu: true,
          AgentChatTester: true,
          AgentGitGuide: true,
          AgentCreateWizard: true,
          AgentSidebar: SidebarStub,
          AgentDataPanel: true,
        },
      },
    })
  }

  beforeEach(() => {
    mocks.listAgents.mockResolvedValue([agent('public')])
  })

  it('keeps MCP hint and data/meta tabs without the management dialog', async () => {
    const wrapper = await mountHintStudio()
    await flushPromises()

    expect(wrapper.text()).toContain('数据')
    expect(wrapper.text()).toContain('元信息')
    expect(wrapper.text()).not.toContain('可复用 Agent 配置 · 组织与 Agent 配置引用')
    expect(wrapper.text()).not.toMatch(/改前|改后/)
    expect(wrapper.text()).not.toContain('列出全部 Agent。此处「Rename」修改身份名称')
    expect(wrapper.text()).not.toContain('Agent 管理')

    await wrapper.findAll('button').find((item) => item.text().startsWith('MCP'))!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="mcp-help-link"]').text()).toBe('帮助')
    expect(wrapper.text()).not.toContain('整份 mcp.json 由你配置')
    expect(wrapper.text()).not.toContain('/root/.codebuddy/mcp.json')
    wrapper.unmount()
  })
})

describe('AgentStudio env credential help', () => {
  const HelpAppModalStub = defineComponent({
    props: { open: Boolean, title: String, width: Number },
    emits: ['close'],
    template:
      '<div v-if="open" data-test="env-help-modal" :data-width="width">' +
      '<h2>{{ title }}</h2><div data-test="env-help-scroll"><slot /></div><slot name="footer" />' +
      '</div>',
  })
  async function mountStudioWithHelp() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return mount(AgentStudioView, {
      global: {
        plugins: [i18n, router],
        stubs: {
          AppButton: ButtonStub,
          Icon: true,
          AppModal: HelpAppModalStub,
          CodeEditor: CodeEditorStub,
          MarkdownSplitEditor: true,
          ExplorerContextMenu: true,
          AgentChatTester: true,
          AgentCreateWizard: true,
          AgentSidebar: true,
          AgentDataPanel: true,
        },
      },
    })
  }

  async function openEnvTab(wrapper: Awaited<ReturnType<typeof mountStudioWithHelp>>) {
    await wrapper.findAll('button').find((item) => item.text().startsWith('环境变量'))!.trigger('click')
    await flushPromises()
  }

  it('keeps generic env help without offering raw model authorization', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudioWithHelp()
    await flushPromises()
    await openEnvTab(wrapper)

    expect(wrapper.get('[data-test="env-help-inject"]').text()).toBe('帮助')
    expect(wrapper.find('[data-test="env-help-acp"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="git-help"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="git-guide"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="git-apply-recommended"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('GRASP_CODEBUDDY_API_KEY')
    expect(wrapper.text()).not.toContain('环境变量会注入该 Agent 的沙箱容器')
    expect(wrapper.text()).not.toContain('请在下方添加对应 Key')
    expect(wrapper.text()).not.toContain('保存后写入 agent.json')

    await wrapper.get('[data-test="env-help-inject"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="env-help-modal"]')).toHaveLength(1)
    expect(wrapper.get('[data-test="env-help-modal"]').attributes('data-width')).toBe('640')
    expect(wrapper.get('[data-test="env-help-modal"]').text()).toContain('环境变量与凭据')
    expect(wrapper.get('[data-help-chip="inject"]').classes().join(' ')).toContain('border-accent')
    expect(wrapper.get('[data-test="env-help-modal"]').text()).toContain('环境变量会注入该 Agent 的沙箱容器')

    wrapper.unmount()
  })

  it('keeps unsaved env draft after closing help', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudioWithHelp()
    await flushPromises()
    await openEnvTab(wrapper)

    await wrapper.findAll('button').find((item) => item.text() === '添加环境变量')!.trigger('click')
    await flushPromises()
    const keyInput = wrapper.findAll('input').find((el) => {
      const node = el.element as HTMLInputElement
      return node.placeholder === 'KEY' && !node.readOnly && node.value === ''
    })!
    await keyInput.setValue('MY_DRAFT_KEY')
    expect((keyInput.element as HTMLInputElement).value).toBe('MY_DRAFT_KEY')

    await wrapper.get('[data-test="env-help-inject"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="env-help-modal"]').exists()).toBe(true)
    await wrapper.get('[data-test="env-help-got-it"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="env-help-modal"]').exists()).toBe(false)
    const kept = wrapper.findAll('input').find((el) => (el.element as HTMLInputElement).value === 'MY_DRAFT_KEY')
    expect(kept).toBeTruthy()
    wrapper.unmount()
  })

  it('opens generic env help when run-tags rejects (404 noise)', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    mocks.listRunTags.mockRejectedValue(new Error('not found'))
    const wrapper = await mountStudioWithHelp()
    await flushPromises()
    await openEnvTab(wrapper)

    void mocks.listRunTags().catch(() => {})
    await wrapper.get('[data-test="env-help-inject"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="env-help-modal"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="env-help-modal"]').text()).toContain('环境变量与凭据')
    expect(wrapper.get('[data-help-chip="inject"]').classes().join(' ')).toContain('border-accent')
    wrapper.unmount()
  })
})

describe('AgentStudio sidebar actions', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const SidebarStub = defineComponent({
    props: { nodes: Array, activeKey: String },
    emits: ['create-agent', 'import', 'export-workspace', 'select'],
    template:
      '<div data-test="sidebar" :data-active-key="activeKey" :data-nodes="nodes.map((n) => n.id).join(\',\')">' +
      '<button data-test="header-create" @click="$emit(\'create-agent\')">create</button>' +
      '<button data-test="header-import" @click="$emit(\'import\')">import</button>' +
      '<button data-test="header-export" @click="$emit(\'export-workspace\')">export</button>' +
      '</div>',
  })
  const WizardStub = defineComponent({
    props: { open: Boolean, existingNames: Array },
    template: '<div v-if="open" data-test="create-wizard" :data-existing-names="existingNames.join(\',\')" />',
  })

  async function mountTreeStudio() {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    return trackMount(
      mount(AgentStudioView, {
        attachTo: document.body,
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: ModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: true,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: WizardStub,
            AgentSidebar: SidebarStub,
            AgentDataPanel: true,
          },
        },
      }),
    )
  }

  beforeEach(() => {
    mocks.listAgents.mockResolvedValue([agent('public')])
  })

  it('feeds the sidebar agent nodes and the active agent key', async () => {
    const wrapper = await mountTreeStudio()
    await flushPromises()
    const sidebar = wrapper.get('[data-test="sidebar"]')
    expect(sidebar.attributes('data-nodes')).toBe('legacy')
    expect(sidebar.attributes('data-active-key')).toBe('legacy')
  })

  it('sidebar create opens the wizard with the existing agent names', async () => {
    const wrapper = await mountTreeStudio()
    await flushPromises()
    expect(wrapper.find('[data-test="create-wizard"]').exists()).toBe(false)
    await wrapper.get('[data-test="header-create"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="create-wizard"]').attributes('data-existing-names')).toBe('legacy')
  })

  it('opens the file picker directly for workspace import', async () => {
    const wrapper = await mountTreeStudio()
    await flushPromises()
    const fileInput = wrapper.get('input[type="file"][accept=".zip"]').element as HTMLInputElement
    const click = vi.spyOn(fileInput, 'click').mockImplementation(() => {})
    await wrapper.get('[data-test="header-import"]').trigger('click')
    await flushPromises()
    expect(click).toHaveBeenCalledTimes(1)
  })

  it('exports a workspace bundle after the secrets warning', async () => {
    mocks.exportWorkspaceAgents.mockResolvedValue({ blob: new Blob(['zip']), filename: 'workspace-agents.zip' })
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:x'), revokeObjectURL: vi.fn() }))
    const wrapper = await mountTreeStudio()
    await flushPromises()
    await wrapper.get('[data-test="header-export"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('导出工作区将带走密钥')
    expect(mocks.exportWorkspaceAgents).not.toHaveBeenCalled()
    await wrapper.findAll('button').find((b) => b.text() === '确认并下载')!.trigger('click')
    await flushPromises()
    expect(mocks.exportWorkspaceAgents).toHaveBeenCalledWith()
    vi.unstubAllGlobals()
  })
})

describe('AgentStudioView loading / four-state', () => {
  it('first load shows the tree skeleton, not centered 加载中…', async () => {
    let release!: (v: unknown) => void
    mocks.listAgents.mockReturnValue(new Promise((resolve) => { release = resolve }))
    const wrapper = await mountStudio()
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-studio-skeleton"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('加载中')
    release!([])
    await flushPromises()
    wrapper.unmount()
  })

  it('failure does not show empty-team CTA', async () => {
    mocks.listAgents.mockRejectedValue(Object.assign(new Error('down'), { status: 500 }))
    const wrapper = await mountStudio()
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-studio-failed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-studio-empty-team"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.text()).toContain('重试')
    expect(wrapper.text()).not.toContain('用一个入口拉起整支团队')
    wrapper.unmount()
  })

  it('empty state keeps primary CTA and secondary import (standalone)', async () => {
    mocks.listAgents.mockResolvedValue([])
    const wrapper = await mountStudio()
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-studio-action-row"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agent-studio-empty-team"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-studio-empty-create-team"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-studio-empty-import"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('创建 Agent 团队')
    expect(wrapper.text()).toContain('导入')
    wrapper.unmount()
  })

  it('embedded empty state keeps new-agent CTA and secondary import', async () => {
    mocks.listAgents.mockResolvedValue([])
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter()
    const wrapper = trackMount(
      mount(AgentStudioView, {
        props: { embedded: true },
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: true,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: true,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-studio-action-row"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agent-studio-empty-create"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-studio-empty-import"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('新建 Agent')
    expect(wrapper.text()).not.toContain('创建 Agent 团队')
    wrapper.unmount()
  })
})

describe('AgentStudioView entry assembly (g3 / Demo main path)', () => {
  it('assembles Demo tabs via independent panels and keeps route/tab wiring', () => {
    const viewSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AgentStudioView.vue'), 'utf8')
    const orchestrationSrc = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../lib/agent/useAgentStudio.ts'),
      'utf8',
    )
    const src = viewSrc + '\n' + orchestrationSrc
    for (const panel of [
      'AgentSwitcher',
      'AgentFilesPanel',
      'AgentMcpPanel',
      'AgentEnvPanel',
      'AgentCapabilitiesPanel',
      'AgentDataPanel',
      'AgentMetaPanel',
    ]) {
      expect(viewSrc).toContain(panel)
    }
    expect(src).toMatch(/STUDIO_TABS/)
    expect(src).toMatch(/requestStudioTab/)
    // Demo main path: files kept alive across tabs; other panels gated by tab + ui-fade (g3.1).
    expect(src).toMatch(/v-show="tab === 'files'"/)
    expect(src).toMatch(/name="ui-fade"/)
    expect(src).toMatch(/v-else-if="tab === 'mcp'/)
    expect(src).toMatch(/v-else-if="tab === 'env'/)
    expect(src).toMatch(/v-else-if="tab === 'capabilities'/)
    expect(src).toMatch(/tab === 'test'/)
    expect(src).toMatch(/tab === 'meta'/)
    // Chat test is a trailing Studio inner tab (g2.1 / g2.2).
    expect(src).toMatch(/const STUDIO_TABS[\s\S]*?=\s*\[[^\]]*\]/)
    expect(src).toMatch(/StudioTab[\s\S]*'test'/)
    expect(src).toMatch(/'test' as const/)
    expect(viewSrc).toContain('AgentChatTester')
    expect(viewSrc).toMatch(/data-testid="studio-chat-test"/)
    expect(viewSrc).toMatch(/createStudioChatTest|createSharedAgentTest/)
    expect(viewSrc).toMatch(/tab !== 'data' && tab !== 'test'/)
  })

  it('switches Demo main-path tabs via tab strip without changing labels', async () => {
    mocks.listAgents.mockResolvedValue([agent()])
    const wrapper = await mountStudio({ agent: 'legacy' })
    await flushPromises()
    expect(wrapper.text()).toContain('对话测试')
    const mcpBtn = wrapper.findAll('button').find((b) => /MCP/i.test(b.text()))
    expect(mcpBtn).toBeTruthy()
    await mcpBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.exists()).toBe(true)
    const envBtn = wrapper.findAll('button').find((b) => b.text().includes('环境'))
    expect(envBtn).toBeTruthy()
    await envBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/环境|MCP|文件/)
    wrapper.unmount()
  })
})

describe('AgentStudio pinned Agent 默认配置', () => {
  const SidebarStub = defineComponent({
    name: 'AgentSidebar',
    props: { nodes: Array, pinned: Array, activeKey: String },
    emits: ['select'],
    template: '<div data-test="sidebar-stub" :data-active="activeKey"><button v-for="n in [...(pinned || []), ...(nodes || [])]" :key="n.id" :data-key="n.id" @click="$emit(\'select\', n.id)">{{ n.label }}</button></div>',
  })
  const agents = (): Agent[] => [
    { ...agent('public'), name: 'alpha' },
    { ...agent('public'), name: 'beta' },
  ]

  async function mountWithDefaults(query: Record<string, string> = {}) {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...nav } } })
    const router = await createStudioRouter(query)
    const wrapper = trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: true,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: true,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentSidebar: SidebarStub,
            AgentDataPanel: true,
            WorkspaceSharedAgentPanel: { template: '<div data-testid="shared-panel-stub" />' },
          },
        },
      }),
    )
    await flushPromises()
    return { wrapper, router }
  }
  const pick = async (wrapper: Awaited<ReturnType<typeof mountWithDefaults>>['wrapper'], key: string) => {
    await wrapper.get(`[data-test="sidebar-stub"] [data-key="${key}"]`).trigger('click')
    await flushPromises()
  }
  const activeTab = (wrapper: Awaited<ReturnType<typeof mountWithDefaults>>['wrapper']) =>
    wrapper.find('[data-test="studio-tab-strip"] [aria-selected="true"]').text()

  it('lists Agent 默认配置 as the pinned first item and opens it in the same page', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountWithDefaults({ agent: 'alpha' })
    const buttons = wrapper.findAll('[data-test="sidebar-stub"] button')
    expect(buttons[0].text()).toBe('默认配置')
    await pick(wrapper, '__workspace_defaults__')
    expect(wrapper.find('[data-testid="studio-defaults-pane"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shared-panel-stub"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="sidebar-stub"]').attributes('data-active')).toBe('__workspace_defaults__')
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
  })

  it('opens the defaults pane from ?view=defaults (old /settings/shared-agent link target)', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountWithDefaults({ view: 'defaults' })
    expect(wrapper.find('[data-testid="studio-defaults-pane"]').exists()).toBe(true)
    expect(router.currentRoute.value.query.view).toBe('defaults')
  })

  it('keeps the MCP tab when switching back to the same Agent', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountWithDefaults({ agent: 'alpha', studioTab: 'mcp' })
    expect(router.currentRoute.value.query.studioTab).toBe('mcp')
    await pick(wrapper, '__workspace_defaults__')
    expect(wrapper.find('[data-testid="studio-defaults-pane"]').exists()).toBe(true)
    await pick(wrapper, 'alpha')
    expect(wrapper.find('[data-testid="studio-defaults-pane"]').exists()).toBe(false)
    expect(router.currentRoute.value.query).toMatchObject({ agent: 'alpha', studioTab: 'mcp' })
    expect(activeTab(wrapper)).toContain('MCP')
  })

  it('opens another Agent on the same tab after the defaults', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountWithDefaults({ agent: 'alpha', studioTab: 'mcp' })
    await pick(wrapper, '__workspace_defaults__')
    await pick(wrapper, 'beta')
    expect(wrapper.find('[data-testid="studio-defaults-pane"]').exists()).toBe(false)
    expect(router.currentRoute.value.query.agent).toBe('beta')
    expect(router.currentRoute.value.query.studioTab).toBe('mcp')
  })

  it('keeps the tab and data sub-tab when switching directly between Agents', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountWithDefaults({ agent: 'alpha', studioTab: 'data', sub: 'context' })
    await pick(wrapper, 'beta')
    expect(router.currentRoute.value.query).toEqual({ agent: 'beta', studioTab: 'data', sub: 'context' })
  })

  it('narrow screens reach the defaults from the switch sheet and can switch back', async () => {
    breakpointMocks.isMobile.value = true
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountWithDefaults({ view: 'defaults' })
    expect(wrapper.find('[data-testid="studio-defaults-pane"]').exists()).toBe(true)
    await wrapper.get('[data-testid="studio-defaults-pane"] [data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    const pinned = document.querySelector('[data-test="sidebar-sheet"] [data-testid="resource-list-pinned"] button') as HTMLButtonElement
    expect(pinned.textContent).toContain('默认配置')
    expect(pinned.textContent).not.toContain('Agent 默认配置')
    const beta = document.querySelector('[data-test="sidebar-sheet"] [data-resource-key="beta"]') as HTMLButtonElement
    beta.click()
    await flushPromises()
    expect(document.querySelector('[data-test="sidebar-sheet"]')).toBeNull()
    expect(wrapper.find('[data-testid="studio-defaults-pane"]').exists()).toBe(false)
    expect(router.currentRoute.value.query.agent).toBe('beta')
  })
})

describe('AgentStudio Agent 默认配置 — empty workspace, unsaved defaults, URL as the source', () => {
  const DEFAULTS = '__workspace_defaults__'
  const sharedState = { dirty: false, save: vi.fn(async () => true) }
  const SharedPanelStub = defineComponent({
    name: 'WorkspaceSharedAgentPanel',
    setup(_p, { expose }) {
      expose({ get dirty() { return sharedState.dirty }, save: () => sharedState.save() })
      return () => h('div', { 'data-testid': 'shared-panel-stub' })
    },
  })
  const SidebarStub = defineComponent({
    name: 'AgentSidebar',
    props: { nodes: Array, pinned: Array, activeKey: String },
    emits: ['select'],
    template: '<div data-test="sidebar-stub" :data-active="activeKey"><button v-for="n in [...(pinned || []), ...(nodes || [])]" :key="n.id" :data-key="n.id" @click="$emit(\'select\', n.id)">{{ n.label }}</button></div>',
  })
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const agents = (): Agent[] => [
    { ...agent('public'), name: 'alpha' },
    { ...agent('public'), name: 'beta' },
  ]

  async function mountAt(path: string) {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...nav } } })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/agents', component: { render: () => h('div') } },
        { path: '/settings/shared-agent', redirect: { path: '/agents', query: { view: 'defaults' } } },
      ],
    })
    const seen: string[] = []
    router.afterEach((to) => { seen.push(to.fullPath) })
    await router.push(path)
    await router.isReady()
    const wrapper = trackMount(
      mount(AgentStudioView, {
        global: {
          plugins: [i18n, router],
          stubs: {
            AppButton: ButtonStub,
            Icon: true,
            AppModal: ModalStub,
            CodeEditor: CodeEditorStub,
            MarkdownSplitEditor: true,
            ExplorerContextMenu: true,
            AgentChatTester: true,
            AgentGitGuide: true,
            AgentCreateWizard: true,
            CreateAgentTeamWizard: true,
            AgentSidebar: SidebarStub,
            AgentDataPanel: true,
            WorkspaceSharedAgentPanel: SharedPanelStub,
          },
        },
      }),
    )
    await flushPromises()
    return { wrapper, router, seen }
  }
  type W = Awaited<ReturnType<typeof mountAt>>['wrapper']
  const pick = async (wrapper: W, key: string) => {
    await wrapper.get(`[data-test="sidebar-stub"] [data-key="${key}"]`).trigger('click')
    await flushPromises()
  }
  const pane = (wrapper: W) => wrapper.find('[data-testid="studio-defaults-pane"]').exists()
  const modal = (wrapper: W) => wrapper.find('[data-test="modal"]')
  const clickModal = async (wrapper: W, text: string) => {
    await modal(wrapper).findAll('button').find((b) => b.text() === text)!.trigger('click')
    await flushPromises()
  }

  beforeEach(() => {
    sharedState.dirty = false
    sharedState.save.mockClear()
    sharedState.save.mockResolvedValue(true)
  })

  // B1
  it('opens Agent 默认配置 from ?view=defaults when the workspace has no Agents', async () => {
    mocks.listAgents.mockResolvedValue([])
    const { wrapper } = await mountAt('/agents?view=defaults')
    expect(pane(wrapper)).toBe(true)
    expect(wrapper.find('[data-testid="agent-studio-empty-team"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="sidebar-stub"]').attributes('data-active')).toBe(DEFAULTS)
  })

  it('old /settings/shared-agent link reaches the defaults with no Agents', async () => {
    mocks.listAgents.mockResolvedValue([])
    const { wrapper } = await mountAt('/settings/shared-agent')
    expect(pane(wrapper)).toBe(true)
  })

  it('the empty state links to Agent 默认配置', async () => {
    mocks.listAgents.mockResolvedValue([])
    const { wrapper, router } = await mountAt('/agents')
    expect(wrapper.find('[data-testid="agent-studio-empty-team"]').exists()).toBe(true)
    const entry = wrapper.get('[data-testid="agent-studio-empty-defaults"]')
    expect(entry.text()).toBe('默认配置')
    await entry.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    expect(pane(wrapper)).toBe(true)
  })

  // S1 desktop
  it('asks before leaving unsaved defaults; cancel keeps URL and pane, confirm switches', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?agent=alpha')
    await pick(wrapper, DEFAULTS)
    sharedState.dirty = true
    await pick(wrapper, 'beta')
    expect(modal(wrapper).text()).toContain('放弃未保存的修改')
    expect(modal(wrapper).text()).toContain('默认配置')
    expect(modal(wrapper).text()).not.toContain('Agent 默认配置')
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    await clickModal(wrapper, '取消')
    expect(pane(wrapper)).toBe(true)
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    await pick(wrapper, 'beta')
    await clickModal(wrapper, '放弃并切换')
    expect(pane(wrapper)).toBe(false)
    expect(router.currentRoute.value.query).toEqual({ agent: 'beta' })
  })

  it('switches without asking when the defaults are clean', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?view=defaults')
    await pick(wrapper, 'beta')
    expect(modal(wrapper).exists()).toBe(false)
    expect(router.currentRoute.value.query).toEqual({ agent: 'beta' })
  })

  // S1 narrow
  it('narrow screens offer 保存并切换 / 丢弃并切换 for unsaved defaults', async () => {
    breakpointMocks.isMobile.value = true
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?view=defaults')
    sharedState.dirty = true
    await wrapper.get('[data-testid="studio-defaults-pane"] [data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-test="sidebar-sheet"] [data-resource-key="beta"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(modal(wrapper).text()).toContain('保存并切换')
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    await clickModal(wrapper, '保存并切换')
    expect(sharedState.save).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.query).toEqual({ agent: 'beta' })
    expect(pane(wrapper)).toBe(false)
  })

  // S2
  it('dirty Agent → defaults → other Agent → cancel keeps URL and screen on the defaults', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?agent=alpha')
    await wrapper.findAll('button').find((b) => b.text() === '元信息')!.trigger('click')
    await editCustomEnvironment(wrapper, 'dirty-feature')
    await pick(wrapper, DEFAULTS)
    await pick(wrapper, 'beta')
    expect(modal(wrapper).text()).toContain('「alpha」')
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    await clickModal(wrapper, '取消')
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    expect(pane(wrapper)).toBe(true)
    expect(wrapper.get('[data-test="sidebar-stub"]').attributes('data-active')).toBe(DEFAULTS)
    // Back to the dirty Agent itself: no prompt, edit kept, tab kept.
    await pick(wrapper, 'alpha')
    expect(modal(wrapper).exists()).toBe(false)
    expect(router.currentRoute.value.query).toEqual({ agent: 'alpha', studioTab: 'meta' })
    await wrapper.findAll('button').find(button => button.text().startsWith('环境变量'))!.trigger('click')
    expect(wrapper.get('input[placeholder="value"]').element).toHaveProperty('value', 'dirty-feature')
  })

  const dirtyAlphaMeta = async (wrapper: W) => {
    await wrapper.findAll('button').find((b) => b.text() === '元信息')!.trigger('click')
    await editCustomEnvironment(wrapper, 'dirty-feature')
  }

  // Both the defaults and the Agent unsaved: the second prompt must survive closing the first.
  it('defaults and alpha both unsaved: two 放弃并切换 clicks land on beta', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?agent=alpha')
    await dirtyAlphaMeta(wrapper)
    await pick(wrapper, DEFAULTS)
    sharedState.dirty = true
    await pick(wrapper, 'beta')
    expect(modal(wrapper).text()).toContain('默认配置')
    expect(modal(wrapper).text()).not.toContain('Agent 默认配置')
    await clickModal(wrapper, '放弃并切换')
    expect(modal(wrapper).text()).toContain('「alpha」')
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    await clickModal(wrapper, '放弃并切换')
    expect(modal(wrapper).exists()).toBe(false)
    expect(router.currentRoute.value.query).toEqual({ agent: 'beta', studioTab: 'meta' })
    expect(pane(wrapper)).toBe(false)
  })

  it('narrow screens use 保存并切换 / 丢弃并切换 for the unsaved Agent prompt too', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?agent=alpha')
    await dirtyAlphaMeta(wrapper)
    await pick(wrapper, DEFAULTS)
    sharedState.dirty = true
    breakpointMocks.isMobile.value = true
    await flushPromises()
    await wrapper.get('[data-testid="studio-defaults-pane"] [data-test="sidebar-switch"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-test="sidebar-sheet"] [data-resource-key="beta"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(modal(wrapper).text()).toContain('默认配置')
    expect(modal(wrapper).text()).not.toContain('Agent 默认配置')
    await clickModal(wrapper, '丢弃并切换')
    expect(modal(wrapper).text()).toContain('「alpha」')
    expect(modal(wrapper).text()).toContain('保存并切换')
    expect(modal(wrapper).text()).not.toContain('放弃并切换')
    expect(router.currentRoute.value.query).toEqual({ view: 'defaults' })
    await clickModal(wrapper, '丢弃并切换')
    expect(modal(wrapper).exists()).toBe(false)
    expect(router.currentRoute.value.query).toEqual({ agent: 'beta', studioTab: 'meta' })
  })

  // S3 / S4: the URL alone decides
  it('pushing the old link while an Agent is open shows the defaults; Back leaves them', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?agent=alpha&studioTab=mcp')
    expect(pane(wrapper)).toBe(false)
    await router.push('/settings/shared-agent')
    await flushPromises()
    expect(pane(wrapper)).toBe(true)
    router.back()
    await flushPromises()
    await new Promise((r) => setTimeout(r, 20))
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ agent: 'alpha', studioTab: 'mcp' })
    expect(pane(wrapper)).toBe(false)
  })

  it('settings nav 「智能体」 (/agents) leaves the defaults and restores the Agent in the URL', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router } = await mountAt('/agents?view=defaults')
    expect(pane(wrapper)).toBe(true)
    await router.push('/agents')
    await flushPromises()
    expect(pane(wrapper)).toBe(false)
    expect(router.currentRoute.value.query).toEqual({ agent: 'alpha' })
  })

  it('first load of ?view=defaults never rewrites the URL to an Agent', async () => {
    mocks.listAgents.mockResolvedValue(agents())
    const { wrapper, router, seen } = await mountAt('/agents?view=defaults')
    expect(pane(wrapper)).toBe(true)
    expect(router.currentRoute.value.fullPath).toBe('/agents?view=defaults')
    expect(seen).toEqual(['/agents?view=defaults'])
  })
})

describe('Agent studio identity icon', () => {
  it('edits the name-bar mark in the draft and writes it only on save', async () => {
    mocks.listAgents.mockResolvedValue([{ ...agent(), icon: { iconId: 'book', color: 'black' } }])
    const wrapper = await mountStudio()
    await flushPromises()
    const mark = () => wrapper.get('[data-test="agent-identity-mark"]')
    expect(mark().find('svg').exists()).toBe(true)
    expect(mocks.saveAgent).not.toHaveBeenCalled()

    await mark().trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-test="agent-identity-color-blue"]')!.click()
    document.querySelector<HTMLButtonElement>('[data-test="agent-identity-icon-plane"]')!.click()
    await flushPromises()
    expect(mocks.saveAgent).not.toHaveBeenCalled()
    const save = () => wrapper.findAll('button').find((button) => button.text().trim() === '保存')
    expect(save()).toBeTruthy()
    await save()!.trigger('click')
    await flushPromises()
    expect(mocks.saveAgent).toHaveBeenCalledWith(expect.objectContaining({
      icon: { iconId: 'plane', color: 'blue' },
    }))

    mocks.saveAgent.mockClear()
    await mark().trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-test="agent-identity-clear"]')!.click()
    await flushPromises()
    expect(mark().text()).toContain('L')
    await save()!.trigger('click')
    await flushPromises()
    expect(mocks.saveAgent).toHaveBeenCalled()
    expect(mocks.saveAgent.mock.calls.at(-1)![0]).not.toHaveProperty('icon')
    wrapper.unmount()
  })

  it('opens the same mark from the narrow name bar', async () => {
    breakpointMocks.isMobile.value = true
    mocks.listAgents.mockResolvedValue([agent()])
    const wrapper = await mountStudio()
    await flushPromises()
    const mark = wrapper.get('[data-test="agent-identity-mark"]')
    expect(mark.text()).toContain('L')
    await mark.trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="agent-identity-picker"]')).toBeTruthy()
    expect(document.querySelector('[data-test="agent-identity-search"]')).toBeTruthy()
    wrapper.unmount()
  })
})

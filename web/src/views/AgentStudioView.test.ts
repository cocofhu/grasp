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
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import type { Agent } from '@/lib/api/api'

const mocks = vi.hoisted(() => ({
  listAgents: vi.fn(),
  listProjects: vi.fn(),
  saveAgent: vi.fn(),
  patchAgentProject: vi.fn(),
  getAgentsOrg: vi.fn(),
  saveAgentsOrg: vi.fn(),
  renameAgent: vi.fn(),
  listProjectRunTags: vi.fn(),
  createProjectSharedAgentTest: vi.fn(),
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
      listAgents: mocks.listAgents,
      listProjects: mocks.listProjects,
      saveAgent: mocks.saveAgent,
      patchAgentProject: mocks.patchAgentProject,
      getAgentsOrg: mocks.getAgentsOrg,
      saveAgentsOrg: mocks.saveAgentsOrg,
      renameAgent: mocks.renameAgent,
      listProjectRunTags: mocks.listProjectRunTags,
      createProjectSharedAgentTest: mocks.createProjectSharedAgentTest,
    },
  }
})

vi.mock('@/lib/composables/useProjectContext', () => ({
  useProjectContext: () => ({
    selected: { value: 'proj-default' },
    ensureHydrated: vi.fn(),
    setProject: vi.fn(),
  }),
}))

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
    projectId: 'proj-default',
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
    routes: [{ path: '/agents', component: { render: () => h('div') } }],
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
          AgentOrgSidebar: true,
          AgentDataPanel: true,
        },
      },
    }),
  )
}

beforeEach(() => {
  removeStudioToasts()
  vi.clearAllMocks()
  breakpointMocks.isMobile.value = false
  mocks.listProjects.mockResolvedValue([{ id: 'proj-default', name: 'Default' }])
  mocks.listProjectRunTags.mockResolvedValue({ tags: [] })
  mocks.saveAgent.mockImplementation(async (payload: Agent) => payload)
  mocks.patchAgentProject.mockImplementation(async (name: string, projectId: string) => ({
    status: 'saved',
    projectId,
  }))
  mocks.getAgentsOrg.mockResolvedValue({ revision: 0, groups: [], agents: {} })
  mocks.saveAgentsOrg.mockImplementation(async (org: { revision?: number }) => ({
    revision: (org.revision || 0) + 1,
    groups: [],
    agents: {},
    ...org,
  }))
  mocks.createProjectSharedAgentTest.mockResolvedValue({ id: 1 })
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
    expect(wrapper.find('[role="radiogroup"]').exists()).toBe(true)
    const siteRadios = wrapper.findAll('button[role="radio"]')
    expect(siteRadios).toHaveLength(2)
    expect(siteRadios.every((item) => item.attributes('aria-checked') === 'false')).toBe(true)
  })

  it('preserves special CodeBuddy region when saving without site interaction', async () => {
    mocks.listAgents.mockResolvedValue([agent('ioa')])
    const wrapper = await mountStudio()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')

    const workspaceInput = wrapper.findAll('input').find((item) => {
      return (item.element as HTMLInputElement).value === '/root/workspace'
    })!
    await workspaceInput.setValue('/root/workspace-edited')
    await wrapper.findAll('button').find((item) => item.text() === '保存')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        env: { GRASP_CODEBUDDY_REGION: 'ioa' },
        layout: expect.objectContaining({ workspaceDir: '/root/workspace-edited' }),
      }),
    )
  })

  it('overwrites special CodeBuddy region after explicitly selecting a site', async () => {
    mocks.listAgents.mockResolvedValue([agent('ioa')])
    const wrapper = await mountStudio()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')

    await wrapper.findAll('button').find((item) => item.text().trim().startsWith('国内站'))!.trigger('click')
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
    const workspaceInput = wrapper.findAll('input').find((item) => {
      return (item.element as HTMLInputElement).value === '/root/workspace'
    })!
    await workspaceInput.setValue('/root/workspace-hydrated')
    expect(wrapper.text()).toContain('未保存')
    await wrapper.findAll('button').find((item) => item.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(mocks.saveAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        env: { GRASP_CODEBUDDY_REGION: 'public' },
        layout: expect.objectContaining({ workspaceDir: '/root/workspace-hydrated' }),
      }),
    )
  })

  it('normalizes Raw JSON conflicts without overriding the ACP site selection', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudio()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text() === '环境变量 (0)')!.trigger('click')
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
    expect(wrapper.get('[data-mcp-name="memory-store"] [data-test="mcp-scope-note"]').text()).toContain('长期记忆，归属主项目')
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

  it('shows legacy upgrade hint for pm-leader and upgrades in place', async () => {
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
            name: 'pm-leader',
            url: '${GRASP_PM_URL}',
            headers: { Authorization: 'Bearer ${GRASP_PM_TOKEN}' },
          },
        ],
      },
    ])
    const wrapper = await mountStudio()
    await flushPromises()
    await openMcpTab(wrapper)

    expect(wrapper.text()).toContain('检测到旧约定名 pm-leader')
    expect(wrapper.find('[data-test="mcp-legacy-pm-hint"]').exists()).toBe(true)
    await wrapper.get('[data-test="mcp-upgrade-legacy"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-mcp-name="pm-leader"]').exists()).toBe(false)
    expect(wrapper.find('[data-mcp-name="memory-store"]').exists()).toBe(true)
    expect(wrapper.find('[data-mcp-name="context-store"]').exists()).toBe(true)
    expect(wrapper.find('[data-mcp-name="task-scheduler"]').exists()).toBe(true)
    expect(wrapper.find('[data-mcp-name="artifact-store"]').exists()).toBe(true)
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
            AgentOrgSidebar: true,
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

describe('AgentStudio rename entry migration', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const SidebarStub = defineComponent({
    emits: ['rename-agent', 'open-manage', 'select-agent'],
    template:
      '<div data-test="sidebar">' +
      '<button data-test="pencil" @click="$emit(\'rename-agent\', \'legacy\')">pencil</button>' +
      '<button data-test="manage" @click="$emit(\'open-manage\')">manage</button>' +
      '</div>',
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
            AgentOrgSidebar: SidebarStub,
            AgentDataPanel: true,
          },
        },
      }),
    )
  }

  it('blocks sidebar pencil rename and does not call api.renameAgent', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await wrapper.get('[data-test="pencil"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('这里不支持修改 Agent 名字')
    expect(wrapper.text()).toContain('前往 Agent 管理')
    expect(mocks.renameAgent).not.toHaveBeenCalled()
  })

  it('opens Agent management with Rename and focuses the blocked target', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await wrapper.get('[data-test="pencil"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '前往 Agent 管理')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Agent 管理')
    expect(wrapper.text()).toContain('Rename')
    const focused = wrapper.find('[data-manage-agent="legacy"]')
    expect(focused.exists()).toBe(true)
    expect(focused.classes().join(' ')).toMatch(/accent/)
    expect((wrapper.get('[data-test="manage-search"]').element as HTMLInputElement).value).toBe('')
  })

  it('filters managed agents with a case-insensitive trimmed query, count, and safe highlight', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agent('public'), name: 'Grasp Review Engineer' },
      { ...agent('public'), name: 'HarnessPlugin Reviewer' },
    ])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await wrapper.get('[data-test="manage"]').trigger('click')
    await wrapper.get('[data-test="manage-search"]').setValue('  grasp  ')
    await nextTick()

    expect(wrapper.find('[data-manage-agent="Grasp Review Engineer"]').exists()).toBe(true)
    expect(wrapper.find('[data-manage-agent="HarnessPlugin Reviewer"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="manage-search-count"]').text()).toBe('匹配 1 / 共 2')
    expect(wrapper.get('[data-manage-agent="Grasp Review Engineer"] mark').text()).toBe('Grasp')
  })

  it('shows a distinct no-match state and clears the management search from either entry point', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agent('public'), name: 'Grasp Review Engineer' },
      { ...agent('public'), name: 'HarnessPlugin Reviewer' },
    ])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await wrapper.get('[data-test="manage"]').trigger('click')
    await wrapper.get('[data-test="manage-search"]').setValue('missing')
    await nextTick()
    expect(wrapper.text()).toContain('没有匹配的 Agent')
    expect(wrapper.findAll('[data-manage-agent]')).toHaveLength(0)

    await wrapper.findAll('button').find((button) => button.text() === '清空搜索')!.trigger('click')
    await nextTick()
    expect(wrapper.findAll('[data-manage-agent]')).toHaveLength(2)

    await wrapper.get('[data-test="manage-search"]').setValue('review')
    await wrapper.get('[data-test="manage-search-clear"]').trigger('click')
    expect((wrapper.get('[data-test="manage-search"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.findAll('[data-manage-agent]')).toHaveLength(2)
  })

  it('resets management search after closing before the next open', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await wrapper.get('[data-test="manage"]').trigger('click')
    await wrapper.get('[data-test="manage-search"]').setValue('legacy')
    await wrapper.findAll('button').find((button) => button.text() === '关闭')!.trigger('click')
    await wrapper.get('[data-test="manage"]').trigger('click')

    expect((wrapper.get('[data-test="manage-search"]').element as HTMLInputElement).value).toBe('')
  })

  it('shows Metadata rename hint and keeps header name read-only', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await wrapper.findAll('button').find((item) => item.text() === '元信息')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('名称请在 Agent 管理中重命名')
    // header name is a span, not an input
    const headerName = wrapper.findAll('span').find((s) => s.text() === 'legacy' && s.classes().includes('font-medium'))
    expect(headerName).toBeTruthy()
  })

  it('management Rename opens dialog with cascade hint; cancel does not rename', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountRenameStudio()
    await flushPromises()

    await wrapper.get('[data-test="manage"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === 'Rename')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('工作流引用不会自动更新')
    expect(wrapper.text()).toContain('将同步更新目录、项目管理、组织关系，以及工作流中的 Agent 配置引用')
    await wrapper.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    await flushPromises()
    expect(mocks.renameAgent).not.toHaveBeenCalled()
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

    await wrapper.get('[data-test="manage"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === 'Rename')!.trigger('click')
    await flushPromises()
    await renameInput().setValue('legacy2')
    await wrapper.findAll('button').find((b) => b.text() === '确定')!.trigger('click')
    await flushPromises()
    expect(mocks.renameAgent).toHaveBeenCalledWith('legacy', 'legacy2')
    expect(document.body.textContent || '').toContain('改名成功，已更新 3 个工作流')
    expect(document.body.textContent || '').not.toContain('已更新 0 个工作流')

    await wrapper.get('[data-test="manage"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === 'Rename')!.trigger('click')
    await flushPromises()
    await renameInput().setValue('legacy3')
    await wrapper.findAll('button').find((b) => b.text() === '确定')!.trigger('click')
    await flushPromises()
    expect(mocks.renameAgent).toHaveBeenCalledWith('legacy2', 'legacy3')
    const body = document.body.textContent || ''
    expect(body).toContain('改名成功')
    expect(body).not.toContain('已更新 0 个工作流')
    expect(body).not.toContain('已更新 3 个工作流')
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
            AgentOrgSidebar: true,
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
      { ...agentWithFiles(), name: 'beta', projectId: 'proj-default' },
    ])
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = await createStudioRouter({ agent: 'alpha', tab: 'test' })
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
              props: ['profile', 'homeProjectId', 'createTest'],
              template:
                '<div data-testid="studio-chat-tester-stub">{{ profile }}|{{ homeProjectId }}</div>',
            },
            AgentGitGuide: true,
            AgentCreateWizard: true,
            AgentOrgSidebar: true,
            AgentDataPanel: true,
          },
        },
      }),
    )
    await flushPromises()

    expect(wrapper.find('[data-testid="studio-chat-test"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="studio-chat-tester-stub"]').text()).toBe(
      'alpha|proj-default',
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
    const router = await createStudioRouter({ agent: 'alpha', tab: 'data', sub: 'jobs' })
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
            AgentOrgSidebar: true,
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
    const router = await createStudioRouter({ agent: 'alpha', tab: 'capabilities' })
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
            AgentOrgSidebar: true,
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

  it('unbound agent shows desktop-bind empty state without goBind on mobile', async () => {
    mocks.listAgents.mockResolvedValue([
      {
        ...agentWithFiles(),
        projectId: '',
      },
    ])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '数据')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('尚未绑定主项目')
    expect(wrapper.text()).toContain('请在桌面端')
    expect(wrapper.text()).not.toContain('去绑定主项目')
    expect(wrapper.find('agent-data-panel-stub').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('建议在桌面使用')
  })

  it('shows switch entry and opens org sheet with groups/ungrouped', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      { ...agentWithFiles(), name: 'beta' },
    ])
    mocks.getAgentsOrg.mockResolvedValue({
      revision: 1,
      groups: [{ id: 'g1', name: '工程部' }],
      agents: {
        alpha: { groupIds: ['g1'] },
        beta: { groupIds: [] },
      },
    })
    const wrapper = await mountMobileStudio()
    await flushPromises()

    const switchBtn = wrapper.get('[data-test="org-switch"]')
    expect(switchBtn.text()).toContain('切换')
    await switchBtn.trigger('click')
    await flushPromises()

    const sheet = document.querySelector('[data-test="org-sheet"]')
    expect(sheet).toBeTruthy()
    expect(sheet!.textContent).toContain('Agent 组织树')
    expect(sheet!.textContent).toContain('工程部')
    expect(sheet!.textContent).toContain('未分组')
    expect(sheet!.textContent).toContain('alpha')
    expect(sheet!.textContent).not.toContain('beta')

    const ugBtn = sheet!.querySelector(
      '[data-org-kind="ungrouped-header"] button',
    ) as HTMLElement | null
    ugBtn?.click()
    await flushPromises()
    expect(sheet!.textContent).toContain('beta')

    // Agent 叶子名称主文字色契约（选中/未选中均 text-txt，非灰）
    const agentBtns = Array.from(document.querySelectorAll('[data-test="org-sheet-agent"]'))
    for (const btn of agentBtns) {
      const nameSpan = btn.querySelector('.truncate.text-txt') as HTMLElement | null
      expect(nameSpan).toBeTruthy()
      expect(nameSpan!.className).not.toMatch(/text-txt2|text-txt3/)
    }
    const alphaRow = document.querySelector('[data-org-kind="agent"][data-org-name="alpha"]')
    expect(alphaRow?.className).toContain('bg-accent-dim')
    const groupName = sheet!.querySelector('.truncate.font-medium.text-txt2')
    expect(groupName?.textContent).toContain('工程部')

    wrapper.unmount()
  })

  it('switches agent without dirty and closes sheet', async () => {
    mocks.listAgents.mockResolvedValue([
      { ...agentWithFiles(), name: 'alpha' },
      {
        ...agentWithFiles(),
        name: 'beta',
        files: [{ path: 'README.md', content: '# beta\n' }],
      },
    ])
    mocks.getAgentsOrg.mockResolvedValue({
      revision: 1,
      groups: [],
      agents: {},
    })
    const wrapper = await mountMobileStudio()
    await flushPromises()
    expect(wrapper.text()).toContain('alpha')

    await wrapper.get('[data-test="org-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = Array.from(document.querySelectorAll('[data-test="org-sheet-agent"]')).find(
      (el) => el.textContent?.includes('beta'),
    ) as HTMLElement
    expect(betaBtn).toBeTruthy()
    betaBtn.click()
    await flushPromises()

    expect(document.querySelector('[data-test="org-sheet"]')).toBeNull()
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

    await wrapper.get('[data-test="org-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = Array.from(document.querySelectorAll('[data-test="org-sheet-agent"]')).find(
      (el) => el.textContent?.includes('beta'),
    ) as HTMLElement
    betaBtn.click()
    await flushPromises()

    expect(wrapper.text()).toContain('保存并切换')
    expect(wrapper.text()).toContain('丢弃并切换')
    expect(wrapper.text()).toContain('取消')
    expect(document.querySelector('[data-test="org-sheet"]')).toBeTruthy()

    await wrapper.findAll('button').find((b) => b.text() === '取消')!.trigger('click')
    await flushPromises()

    expect(document.querySelector('[data-test="org-sheet"]')).toBeTruthy()
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

    await wrapper.get('[data-test="org-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = Array.from(document.querySelectorAll('[data-test="org-sheet-agent"]')).find(
      (el) => el.textContent?.includes('beta'),
    ) as HTMLElement
    betaBtn.click()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '保存并切换')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).toHaveBeenCalled()
    expect(document.querySelector('[data-test="org-sheet"]')).toBeNull()
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

    await wrapper.get('[data-test="org-switch"]').trigger('click')
    await flushPromises()
    const betaBtn = Array.from(document.querySelectorAll('[data-test="org-sheet-agent"]')).find(
      (el) => el.textContent?.includes('beta'),
    ) as HTMLElement
    betaBtn.click()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '丢弃并切换')!.trigger('click')
    await flushPromises()

    expect(mocks.saveAgent).not.toHaveBeenCalled()
    expect(document.querySelector('[data-test="org-sheet"]')).toBeNull()
    expect(wrapper.text()).toContain('beta')
    expect(wrapper.text()).toContain('README.md')
    wrapper.unmount()
  })

  it('org sheet manage opens existing agent manage modal', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.get('[data-test="org-switch"]').trigger('click')
    await flushPromises()
    const manageBtn = document.querySelector('[data-test="org-sheet-manage"]') as HTMLButtonElement
    expect(manageBtn).toBeTruthy()
    manageBtn.click()
    await flushPromises()

    expect(wrapper.text()).toContain('Agent 管理')
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

  it('closes org sheet when breakpoint flips to desktop', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    await wrapper.get('[data-test="org-switch"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="org-sheet"]')).toBeTruthy()

    breakpointMocks.isMobile.value = false
    await nextTick()
    await flushPromises()
    expect(document.querySelector('[data-test="org-sheet"]')).toBeNull()
    wrapper.unmount()
  })

  it('keeps switch and exposes import/new via org sheet header', async () => {
    mocks.listAgents.mockResolvedValue([agentWithFiles()])
    const wrapper = await mountMobileStudio()
    await flushPromises()

    expect(wrapper.find('[data-test="org-switch"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-studio-action-row"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('配置可复用的 Agent')
    expect(wrapper.text()).not.toContain('复制进沙箱')

    await wrapper.get('[data-test="org-switch"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="org-sheet"]')).toBeTruthy()
    expect(document.querySelector('[data-test="org-sheet-import"]')).toBeTruthy()
    expect(document.querySelector('[data-test="org-sheet-create-agent"]')).toBeTruthy()
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
            AgentOrgSidebar: true,
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
    expect(wrapper.get('[data-test="org-switch"]').classes().join(' ')).toMatch(/min-h-11/)
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
    expect(wrapper.find('[data-test="org-switch"]').exists()).toBe(false)
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
    '已拖入未分组',
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
          AgentOrgSidebar: {
            template:
              '<div data-test="sidebar">' +
              '<button data-testid="agent-org-import" aria-label="Import">Import</button>' +
              '<button data-testid="agent-org-create-agent" aria-label="New agent">New agent</button>' +
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
          AgentOrgSidebar: {
            template:
              '<div data-test="sidebar">' +
              '<button data-testid="agent-org-import" aria-label="导入">导入</button>' +
              '<button data-testid="agent-org-create-agent" aria-label="新建 Agent">新建 Agent</button>' +
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
    expect(wrapper.find('[data-testid="agent-org-import"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-org-create-agent"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-org-import"]').attributes('aria-label')).toBe('导入')
    expect(wrapper.find('[data-testid="agent-org-create-agent"]').attributes('aria-label')).toBe('新建 Agent')
    wrapper.unmount()
  })

  it('hides en subtitle and keeps Import / New agent via sidebar', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudioEn()
    await flushPromises()

    const text = wrapper.text()
    for (const s of subtitleEn) expect(text).not.toContain(s)
    expect(wrapper.find('[data-testid="agent-studio-action-row"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agent-org-import"]').attributes('aria-label')).toBe('Import')
    expect(wrapper.find('[data-testid="agent-org-create-agent"]').attributes('aria-label')).toBe('New agent')
    wrapper.unmount()
  })
})

describe('AgentStudio org toast and remaining hints', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const SidebarStub = defineComponent({
    emits: ['remove-from-group', 'move-agent', 'open-manage', 'select-agent'],
    template:
      '<div data-test="sidebar">' +
      '<button data-test="remove-from-group" @click="$emit(\'remove-from-group\', \'legacy\', \'g1\')">remove</button>' +
      '<button data-test="move-ungrouped" @click="$emit(\'move-agent\', \'legacy\', \'g1\', \'\')">ungroup</button>' +
      '<button data-test="manage" @click="$emit(\'open-manage\')">manage</button>' +
      '</div>',
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
          AgentOrgSidebar: SidebarStub,
          AgentDataPanel: true,
        },
      },
    })
  }

  beforeEach(() => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    mocks.getAgentsOrg.mockResolvedValue({
      revision: 1,
      groups: [{ id: 'g1', name: 'Dev' }],
      agents: { legacy: { groupIds: ['g1'] } },
    })
    mocks.saveAgentsOrg.mockImplementation(async (org: { revision?: number; groups?: unknown; agents?: unknown }) => ({
      revision: (org.revision || 0) + 1,
      groups: org.groups || [],
      agents: org.agents || {},
    }))
  })

  it('toasts on 移出本组 but not when dragging to ungrouped', async () => {
    const wrapper = await mountHintStudio()
    await flushPromises()

    await wrapper.get('[data-test="move-ungrouped"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent || '').not.toContain('已拖入未分组')
    expect(document.body.textContent || '').not.toContain('已移出本组并立即保存')
    expect(mocks.saveAgentsOrg).toHaveBeenCalled()

    await wrapper.get('[data-test="remove-from-group"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent || '').toContain('已移出本组并立即保存 · 实体仍在')
    expect(document.body.textContent || '').not.toContain('已拖入未分组')
    wrapper.unmount()
  })

  it('keeps MCP hint, manageIntro, and data/meta tabs', async () => {
    const wrapper = await mountHintStudio()
    await flushPromises()

    expect(wrapper.text()).toContain('数据')
    expect(wrapper.text()).toContain('元信息')
    expect(wrapper.text()).not.toContain('可复用 Agent 配置 · 组织与 Agent 配置引用')
    expect(wrapper.text()).not.toMatch(/改前|改后/)

    await wrapper.findAll('button').find((item) => item.text().startsWith('MCP'))!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="mcp-help-link"]').text()).toBe('帮助')
    expect(wrapper.text()).not.toContain('整份 mcp.json 由你配置')
    expect(wrapper.text()).not.toContain('/root/.codebuddy/mcp.json')

    await wrapper.get('[data-test="manage"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('列出全部 Agent。此处「Rename」修改身份名称')
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
  const GitGuideHelpStub = defineComponent({
    emits: ['help', 'update:credentialType'],
    template:
      '<div data-test="git-guide"><button type="button" data-test="git-help-emit" @click="$emit(\'help\', \'git\')">帮助</button></div>',
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
          AgentGitGuide: GitGuideHelpStub,
          AgentCreateWizard: true,
          AgentOrgSidebar: true,
          AgentDataPanel: true,
        },
      },
    })
  }

  async function openEnvTab(wrapper: Awaited<ReturnType<typeof mountStudioWithHelp>>) {
    await wrapper.findAll('button').find((item) => item.text().startsWith('环境变量'))!.trigger('click')
    await flushPromises()
  }

  it('moves long copy into one help modal and jumps inject / git / acp', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    const wrapper = await mountStudioWithHelp()
    await flushPromises()
    await openEnvTab(wrapper)

    expect(wrapper.get('[data-test="env-help-inject"]').text()).toBe('帮助')
    expect(wrapper.get('[data-test="env-help-acp"]').text()).toBe('帮助')
    expect(wrapper.text()).toContain('GRASP_CODEBUDDY_API_KEY')
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

    await wrapper.get('[data-test="git-help-emit"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="env-help-modal"]')).toHaveLength(1)
    expect(wrapper.get('[data-help-chip="git"]').classes().join(' ')).toContain('border-accent')
    expect(wrapper.get('[data-test="env-help-modal"]').text()).toContain('不会验证变量引用的实际值')

    await wrapper.get('[data-test="env-help-acp"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="env-help-modal"]')).toHaveLength(1)
    expect(wrapper.get('[data-help-chip="acp"]').classes().join(' ')).toContain('border-accent')
    expect(wrapper.get('[data-test="env-help-modal"]').text()).toContain('CodeBuddy ACP 鉴权')
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

  it('opens Git credential help when run-tags rejects (404 noise)', async () => {
    mocks.listAgents.mockResolvedValue([agent('public')])
    mocks.listProjectRunTags.mockRejectedValue(new Error('not found'))
    const wrapper = await mountStudioWithHelp()
    await flushPromises()
    await openEnvTab(wrapper)

    void mocks.listProjectRunTags('proj-28d13430').catch(() => {})
    await wrapper.get('[data-test="git-help-emit"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="env-help-modal"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="env-help-modal"]').text()).toContain('环境变量与凭据')
    expect(wrapper.get('[data-help-chip="git"]').classes().join(' ')).toContain('border-accent')
    wrapper.unmount()
  })
})

describe('AgentStudio group assign project', () => {
  const ModalStub = defineComponent({
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" data-test="modal"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
  })
  const SidebarStub = defineComponent({
    props: { org: Object, agents: Array, projects: Array },
    emits: ['assign-project', 'select-agent'],
    template: '<div data-test="sidebar"><button data-test="assign-g1" @click="$emit(\'assign-project\', \'g1\')">assign</button></div>',
  })

  function studioAgent(name: string, projectId: string): Agent {
    return {
      name,
      projectId,
      acpBackend: 'cursor',
      files: [{ path: 'AGENTS.md', content: `# ${name}\n` }],
      mcp: [],
      env: {},
      layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
    }
  }

  async function mountAssignStudio() {
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
            AgentOrgSidebar: SidebarStub,
            AgentDataPanel: true,
          },
        },
      }),
    )
  }

  beforeEach(() => {
    breakpointMocks.isMobile.value = false
    mocks.listProjects.mockResolvedValue([
      { id: 'github', name: 'GitHub' },
      { id: 'figma', name: 'Figma' },
    ])
    mocks.getAgentsOrg.mockResolvedValue({
      revision: 1,
      groups: [{ id: 'g1', name: '设计组' }],
      agents: {
        alice: { groupIds: ['g1'] },
        bob: { groupIds: ['g1'] },
      },
    })
  })

  it('集合内非 dirty 成功后同步草稿 projectId，不弹草稿冲突', async () => {
    mocks.listAgents.mockResolvedValue([
      studioAgent('alice', 'figma'),
      studioAgent('bob', 'github'),
    ])
    mocks.listAgents
      .mockResolvedValueOnce([
        studioAgent('alice', 'figma'),
        studioAgent('bob', 'github'),
      ])
      .mockResolvedValueOnce([
        studioAgent('alice', 'github'),
        studioAgent('bob', 'github'),
      ])
    const wrapper = await mountAssignStudio()
    await flushPromises()

    await wrapper.get('[data-test="assign-g1"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('指定项目 · 设计组')
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('bob')

    await wrapper.get('[data-test="org-assign-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('确认切换主项目')
    expect(wrapper.text()).toContain('立即生效')
    expect(wrapper.text()).not.toContain('草稿主项目冲突')

    await wrapper.get('[data-test="org-assign-cover-ok"]').trigger('click')
    await flushPromises()

    expect(mocks.patchAgentProject).toHaveBeenCalledTimes(2)
    expect(mocks.patchAgentProject).toHaveBeenNthCalledWith(1, 'alice', 'github')
    expect(mocks.patchAgentProject).toHaveBeenNthCalledWith(2, 'bob', 'github')
    expect(document.querySelector('[data-test="studio-toast"]')?.textContent).toContain('已绑定到 GitHub')
    wrapper.unmount()
  })

  it('dirty 保留草稿则整次不写 PATCH', async () => {
    mocks.listAgents.mockResolvedValue([
      studioAgent('alice', 'figma'),
      studioAgent('bob', 'github'),
    ])
    const wrapper = await mountAssignStudio()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '元信息')!.trigger('click')
    await flushPromises()
    const select = wrapper.get('[data-test="agent-project-select"]')
    await select.setValue('github')
    await flushPromises()
    // confirm single-agent draft switch modal if it appears
    const confirmSwitch = wrapper.findAll('button').find((b) => b.text().includes('切换到'))
    if (confirmSwitch) {
      await confirmSwitch.trigger('click')
      await flushPromises()
    }

    await wrapper.get('[data-test="assign-g1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="org-assign-submit"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="org-assign-cover-ok"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('草稿主项目冲突')
    await wrapper.get('[data-test="org-assign-draft-keep"]').trigger('click')
    await flushPromises()

    expect(mocks.patchAgentProject).not.toHaveBeenCalled()
    expect(document.querySelector('[data-test="studio-toast"]')?.textContent).toContain('组级指定未执行')
    wrapper.unmount()
  })

  it('部分失败 err-box 列出原因，当前 Agent fail 不同步草稿', async () => {
    mocks.listAgents.mockResolvedValue([
      studioAgent('alice', 'figma'),
      studioAgent('bob', 'github'),
    ])
    mocks.patchAgentProject.mockImplementation(async (name: string, projectId: string) => {
      if (name === 'alice') throw Object.assign(new Error('绑定不被允许（含项目级 MCP 约束）'), { status: 400 })
      return { status: 'saved', projectId }
    })
    mocks.listAgents
      .mockResolvedValueOnce([
        studioAgent('alice', 'figma'),
        studioAgent('bob', 'github'),
      ])
      .mockResolvedValueOnce([
        studioAgent('alice', 'figma'),
        studioAgent('bob', 'github'),
      ])

    const wrapper = await mountAssignStudio()
    await flushPromises()
    await wrapper.get('[data-test="assign-g1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="org-assign-submit"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="org-assign-cover-ok"]').trigger('click')
    await flushPromises()

    const failBox = wrapper.get('[data-test="org-assign-fail"]')
    expect(failBox.text()).toContain('部分成功')
    expect(failBox.text()).toContain('alice')
    expect(failBox.text()).toContain('绑定不被允许')
    expect(failBox.text()).toContain('括号按完成后实际绑定刷新')
    expect(mocks.patchAgentProject).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})

describe('AgentStudioView loading / four-state', () => {
  it('first load shows org-tree skeleton, not centered 加载中…', async () => {
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
        props: { projectId: 'proj-1', embedded: true },
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
            AgentOrgSidebar: true,
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
      'AgentOrgSidebar',
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
    expect(viewSrc).toMatch(/createStudioChatTest|createProjectSharedAgentTest/)
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

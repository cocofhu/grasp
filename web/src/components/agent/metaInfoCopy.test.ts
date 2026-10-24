// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import zhCommon from '@/locales/zh-CN/common.json'
import zhPages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import AgentMetaPanel from './AgentMetaPanel.vue'
import WorkspaceSharedAgentPanel from '@/components/workspace/WorkspaceSharedAgentPanel.vue'

const apiMocks = vi.hoisted(() => ({
  getSharedAgentConfig: vi.fn(),
  listCredentials: vi.fn(),
  listAgents: vi.fn(),
  listProjects: vi.fn(),
  putSharedAgentConfig: vi.fn(),
  getModelDefaults: vi.fn(),
  listModelConfigurations: vi.fn(), listModelConnections: vi.fn(),
  listToolCapabilities: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getSharedAgentConfig: apiMocks.getSharedAgentConfig,
      listCredentials: apiMocks.listCredentials,
      listAgents: apiMocks.listAgents,
      listProjects: apiMocks.listProjects,
      putSharedAgentConfig: apiMocks.putSharedAgentConfig,
      getModelDefaults: apiMocks.getModelDefaults,
      listModelConfigurations: apiMocks.listModelConfigurations, listModelConnections: apiMocks.listModelConnections,
      listToolCapabilities: apiMocks.listToolCapabilities,
    },
  }
})

vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: async () => [{ id: 'gpt-5', name: 'GPT-5', vendor: 'openai' }], cliCatalogFailed: () => false }))

const hiddenZh = [
  '沙箱注入布局(WSP/1)',
  '本 Agent 的配置在沙箱内的注入位置',
  '每个 Agent 配置绑定一个 ACP 后端',
  'Agent 工作目录(rules / skills / mcp.json)RW 挂载到容器内的此路径。',
  '仓库克隆与代码执行目录,作为 WORKSPACE_DIR 注入容器。',
  '派生子路径(协议固定,随 configRoot 计算)',
  '本工作区的 Agent 默认用哪个账号、跑哪个模型。',
  '这个 Agent 用哪个账号、跑哪个模型。',
  '这是本工作区的默认授权',
  '未选择时使用种类匹配的共享通用授权',
  '通用授权',
  '运行配置',
  '名称请在 Agent 管理中重命名',
  '不指定 --model，使用 CLI 默认',
  '(configRoot)',
  '(workspaceDir)',
  'claude_code',
]

const hiddenEn = [
  'Sandbox layout (WSP/1)',
  'Persisted per agent',
  'Each Agent profile binds one backend',
  'Agent workspace (rules/skills/mcp.json) RW mount path in container.',
  'Repo clone and execution dir (WORKSPACE_DIR).',
  'Derived paths (fixed by protocol from configRoot)',
  "Which account and model this workspace's Agents use by default.",
  'Which account this Agent signs in with and which model it runs.',
  'This is the workspace default.',
  'Shared credential',
  'Runtime',
  'Rename this agent in Agent management',
  'No --model; the CLI default applies',
  'configRoot',
  'workspaceDir',
  'claude_code',
]

const sharedCfg = {
  acpBackend: 'cursor',
  gitCredentialType: '',
  aiCredentialId: '',
  files: [],
  mcp: [],
  env: { ACP_BRIDGE_MODEL: 'composer-2.5' },
  layout: { configRoot: '/root/.cursor', workspaceDir: '/workspace' },
}

function i18nFor(locale: 'zh-CN' | 'en') {
  return createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...zhCommon, ...zhPages },
      en: { ...enCommon, ...enPages },
    },
  })
}

function mountShared(locale: 'zh-CN' | 'en' = 'zh-CN') {
  return mount(WorkspaceSharedAgentPanel, {
    global: {
      plugins: [i18nFor(locale)],
      stubs: {
        Icon: true,
        RouterLink: true,
        AppButton: { template: '<button v-bind="$attrs"><slot/></button>' },
        AgentFilesPanel: true,
        AgentMcpPanel: true,
        AgentEnvPanel: true,
      },
    },
  })
}

function mountMeta(locale: 'zh-CN' | 'en' = 'zh-CN', overrides: Record<string, unknown> = {}) {
  const draft = reactive({
    name: 'e2e-agent',
    acpBackend: 'cursor',
    modelBinding: { kind: 'inherit' },
    aiCredentialId: '',
    openCodeCredentialId: '',
    gitCredentialId: '',
    sshHostsCredentialId: '',
    gitCredentialType: undefined,
    files: [],
    mcp: [],
    env: [{ k: 'ACP_BRIDGE_MODEL', v: 'gpt-5' }],
    layout: { configRoot: '/root/.cursor', workspaceDir: '/workspace' },
    capabilities: null,
    ...overrides,
  })
  const wrapper = mount(AgentMetaPanel, {
    props: { draft, agentName: 'e2e-agent' },
    global: {
      plugins: [i18nFor(locale)],
      stubs: { Icon: true, RouterLink: true, AppModal: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' } },
    },
  })
  return { wrapper, draft }
}

function expectHidden(text: string, phrases: string[]) {
  for (const phrase of phrases) expect(text).not.toContain(phrase)
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.getSharedAgentConfig.mockResolvedValue(sharedCfg)
  apiMocks.listCredentials.mockResolvedValue({ items: [] })
  apiMocks.listAgents.mockResolvedValue([])
  apiMocks.listProjects.mockResolvedValue([])
  apiMocks.putSharedAgentConfig.mockImplementation(async (body: unknown) => body)
  apiMocks.getModelDefaults.mockResolvedValue({ bindingsByBackend: { cursor: { kind: 'platform', credentialId: 'cursor-key', modelId: 'gpt-5' } } })
  apiMocks.listModelConfigurations.mockResolvedValue({ items: [] })
  apiMocks.listModelConnections.mockResolvedValue({ items: [] })
  apiMocks.listToolCapabilities.mockResolvedValue({ items: [{ toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' }] })
})

describe('meta info copy cleanup', () => {
  it('hides named copy on the workspace meta tab and keeps the controls', async () => {
    const wrapper = mountShared()
    await flushPromises()
    await wrapper.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    const text = wrapper.get('[data-testid="shared-agent-meta"]').text()
    expectHidden(text, hiddenZh)
    expect(text).toContain('编码工具')
    expect(text).toContain('配置根目录')
    expect(text).toContain('工作区目录')
    expect(text).toContain('按工具设置工作区默认')
    expect(text).not.toContain('单个 Agent 已选择时用自己的')
    expect(text).not.toContain('工作区默认模型。使用同一 CLI、且未单独选模型的 Agent 会用这里的选择。')
    expect(wrapper.get('[data-testid="workspace-model-defaults"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-binding-editor"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="meta-config-root"]').element).toBeInstanceOf(HTMLInputElement)
    expect(wrapper.get('[data-testid="meta-workspace-dir"]').element).toBeInstanceOf(HTMLInputElement)
    expect(wrapper.find('[data-testid="derived-paths-table"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('派生子路径')
    const claude = wrapper.get('[data-testid="acp-backend-claude_code"]')
    expect(claude.text()).toContain('Claude Code')
    expect(claude.text()).not.toContain('claude_code')
    expect(claude.find('[data-provider-logo="claude"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="acp-backend-cursor"]').attributes('aria-checked')).toBe('true')
    for (const id of ['cursor', 'claude', 'codebuddy', 'codex', 'trae', 'opencode']) {
      expect(wrapper.find(`[data-provider-logo="${id}"]`).exists()).toBe(true)
    }
    wrapper.unmount()
  })

  it('keeps backend and layout controls separate from per-tool model defaults', async () => {
    const wrapper = mountShared()
    await flushPromises()
    await wrapper.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    const vm = wrapper.vm as unknown as {
      draft: { acpBackend: string; layout: { configRoot: string }; env: { k: string }[] }
      selectAcpBackend: (id: string) => void
    }
    vm.selectAcpBackend('claude_code')
    await flushPromises()
    expect(vm.draft.acpBackend).toBe('claude_code')
    expect(vm.draft.layout.configRoot).toBe('/root/.claude')
    expect(vm.draft.env.some((entry) => entry.k === 'ACP_BRIDGE_MODEL')).toBe(false)
    expect(wrapper.get('[data-testid="acp-backend-claude_code"]').attributes('aria-checked')).toBe('true')
    const defaultsTool = wrapper.getComponent('[data-testid="workspace-model-default-backend"]')
    expect(defaultsTool.props('modelValue')).toBe('cursor')
    await wrapper.get('[data-testid="meta-config-root"]').setValue('/custom/root')
    vm.selectAcpBackend('codex')
    await flushPromises()
    expect(vm.draft.layout.configRoot).toBe('/custom/root')
    expect(defaultsTool.props('modelValue')).toBe('cursor')
    expect(wrapper.find('[data-testid="derived-paths-table"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides the same copy on Agent Studio meta and keeps the controls', async () => {
    apiMocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', env: {} })
    const { wrapper } = mountMeta()
    await flushPromises()
    const text = wrapper.text()
    expectHidden(text, hiddenZh)
    expect(text).toContain('编码工具')
    expect(text).toContain('配置根目录')
    expect(text).toContain('工作区目录')
    expect(text).toContain('继承工作区默认')
    expect(wrapper.get('[data-testid="model-binding-inherit"]').attributes('aria-pressed')).toBe('true')
    expect(text).not.toContain('未选择时使用工作区默认授权')
    expect(text).not.toContain('工作区默认模型。使用同一 CLI、且未单独选模型的 Agent 会用这里的选择。')
    expect(wrapper.get('[data-testid="model-binding-inherited"]').text()).toContain('gpt-5')
    expect(wrapper.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-binding-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="derived-paths-table"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="acp-backend-claude_code"]').text()).toContain('Claude Code')
    expect(wrapper.get('[data-testid="acp-backend-claude_code"]').text()).not.toContain('claude_code')
    expect(wrapper.get('[data-testid="acp-backend-cursor"]').attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })

  it('uses the cleaned English labels on both meta panels', async () => {
    const shared = mountShared('en')
    await flushPromises()
    await shared.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    const sharedText = shared.get('[data-testid="shared-agent-meta"]').text()
    expectHidden(sharedText, hiddenEn)
    expect(sharedText).toContain('Coding tool')
    expect(sharedText).toContain('Config root')
    expect(sharedText).toContain('Workspace directory')
    expect(sharedText).toContain('Workspace model defaults by tool')
    expect(sharedText).not.toContain('The workspace default model.')
    expect(shared.get('[data-testid="cli-model-trigger"]').exists()).toBe(true)
    shared.unmount()

    const { wrapper } = mountMeta('en')
    await flushPromises()
    const text = wrapper.text()
    expectHidden(text, hiddenEn)
    expect(text).toContain('Coding tool')
    expect(text).toContain('Config root')
    expect(text).toContain('Workspace directory')
    expect(text).toContain('Workspace default')
    expect(wrapper.get('[data-testid="model-binding-inherit"]').attributes('aria-pressed')).toBe('true')
    expect(text).not.toContain('The workspace default model.')
    expect(wrapper.get('[data-testid="model-binding-inherited"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

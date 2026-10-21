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
  putSharedAgentConfig: vi.fn(),
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
      putSharedAgentConfig: apiMocks.putSharedAgentConfig,
    },
  }
})

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
  '工作区默认模型。使用同一 CLI、且未单独选模型的 Agent 会用这里的选择。',
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
  'The workspace default model.',
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
      stubs: { Icon: true, AppModal: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' } },
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
  apiMocks.putSharedAgentConfig.mockImplementation(async (body: unknown) => body)
})

describe('meta info copy cleanup', () => {
  it('hides named copy on the workspace meta tab and keeps the controls', async () => {
    const wrapper = mountShared()
    await flushPromises()
    await wrapper.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    const text = wrapper.get('[data-testid="shared-agent-meta"]').text()
    expectHidden(text, hiddenZh)
    expect(text).toContain('ACP 后端 (acpBackend)')
    expect(text).toContain('配置根目录')
    expect(text).toContain('工作区目录')
    expect(text).toContain('运行配置')
    expect(text).toContain('通用授权')
    expect(wrapper.get('[data-testid="cli-model-trigger"]').text()).toContain('composer-2.5')
    expect(wrapper.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="cli-model-reset"]').text()).toContain('恢复自动')
    expect(wrapper.find('[data-testid="credential-alias-picker"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="meta-config-root"]').element).toBeInstanceOf(HTMLInputElement)
    expect(wrapper.get('[data-testid="meta-workspace-dir"]').element).toBeInstanceOf(HTMLInputElement)
    const table = wrapper.get('[data-testid="derived-paths-table"]')
    expect(table.text()).toContain('mcp.json')
    expect(table.text()).not.toContain('派生子路径')
    const claude = wrapper.get('[data-testid="acp-backend-claude_code"]')
    expect(claude.text()).toContain('Claude Code')
    expect(claude.text()).not.toContain('claude_code')
    expect(claude.find('[data-provider-logo="claude"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="acp-backend-cursor"]').classes()).toContain('border-accent')
    for (const id of ['cursor', 'claude', 'codebuddy', 'codex', 'trae', 'opencode']) {
      expect(wrapper.find(`[data-provider-logo="${id}"]`).exists()).toBe(true)
    }
    wrapper.unmount()
  })

  it('keeps backend switching, region, effort, and OpenCode fields', async () => {
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
    expect(vm.draft.env.some((e) => e.k === 'ACP_BRIDGE_MODEL')).toBe(false)
    expect(wrapper.get('[data-testid="acp-backend-claude_code"]').classes()).toContain('border-accent')

    await wrapper.get('[data-testid="meta-config-root"]').setValue('/custom/root')
    vm.selectAcpBackend('codex')
    await flushPromises()
    expect(vm.draft.layout.configRoot).toBe('/custom/root')
    expect(wrapper.find('[data-test="agent-cli-effort"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('运行配置')

    vm.selectAcpBackend('codebuddy')
    await flushPromises()
    expect(wrapper.text()).toContain('服务站点')

    vm.selectAcpBackend('opencode')
    await flushPromises()
    expect(wrapper.text()).toContain('模型厂商与模型')
    expect(wrapper.find('[data-testid="credential-opencode"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="derived-paths-table"]').text()).toContain('mcp.json')
    wrapper.unmount()
  })

  it('hides the same copy on Agent Studio meta and keeps the controls', async () => {
    apiMocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', env: {} })
    const { wrapper } = mountMeta()
    await flushPromises()
    const text = wrapper.text()
    expectHidden(text, hiddenZh)
    expect(text).toContain('ACP 后端 (acpBackend)')
    expect(text).toContain('配置根目录')
    expect(text).toContain('工作区目录')
    expect(text).toContain('运行配置')
    expect(wrapper.get('[data-testid="cli-model-trigger"]').text()).toContain('gpt-5')
    expect(wrapper.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="cli-model-reset"]').text()).toContain('恢复自动')
    expect(wrapper.find('[data-testid="credential-alias-picker"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="derived-paths-table"]').text()).toContain('mcp.json')
    expect(wrapper.get('[data-testid="acp-backend-claude_code"]').text()).toContain('Claude Code')
    expect(wrapper.get('[data-testid="acp-backend-claude_code"]').text()).not.toContain('claude_code')
    expect(wrapper.get('[data-testid="acp-backend-cursor"]').classes()).toContain('border-accent')
    wrapper.unmount()
  })

  it('uses the cleaned English labels on both meta panels', async () => {
    const shared = mountShared('en')
    await flushPromises()
    await shared.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    const sharedText = shared.get('[data-testid="shared-agent-meta"]').text()
    expectHidden(sharedText, hiddenEn)
    expect(sharedText).toContain('ACP backend (acpBackend)')
    expect(sharedText).toContain('Config root')
    expect(sharedText).toContain('Workspace directory')
    expect(shared.get('[data-testid="cli-model-trigger"]').exists()).toBe(true)
    shared.unmount()

    const { wrapper } = mountMeta('en')
    await flushPromises()
    const text = wrapper.text()
    expectHidden(text, hiddenEn)
    expect(text).toContain('ACP backend (acpBackend)')
    expect(text).toContain('Config root')
    expect(text).toContain('Workspace directory')
    expect(wrapper.get('[data-testid="cli-model-trigger"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="cli-model-command"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

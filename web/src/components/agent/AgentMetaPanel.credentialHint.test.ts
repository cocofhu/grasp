// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentMetaPanel from './AgentMetaPanel.vue'

const mocks = vi.hoisted(() => ({
  listCredentials: vi.fn(),
  getSharedAgentConfig: vi.fn(),
  listModelConfigurations: vi.fn(), listModelConnections: vi.fn(),
  listToolCapabilities: vi.fn(),
  getModelDefaults: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listCredentials: mocks.listCredentials,
      getSharedAgentConfig: mocks.getSharedAgentConfig,
      listModelConfigurations: mocks.listModelConfigurations, listModelConnections: mocks.listModelConnections,
      listToolCapabilities: mocks.listToolCapabilities,
      getModelDefaults: mocks.getModelDefaults,
    },
  }
})

function mountPanel(draft: Record<string, unknown>) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentMetaPanel, {
    props: {
      draft,
      agentName: 'e2e-agent',
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, RouterLink: true, RepoAccessPicker: true, AgentCredentialBindings: true, AppModal: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' } },
    },
  })
}

function draftFor(overrides: Record<string, unknown> = {}) {
  return reactive({
    name: 'e2e-agent',
    acpBackend: 'cursor',
    modelBinding: { kind: 'inherit' },
    aiCredentialId: '',
    openCodeCredentialId: '',
    gitCredentialId: '',
    sshHostsCredentialId: '',
    gitCredentialType: 'github_https',
    files: [],
    mcp: [],
    env: [],
    layout: { configRoot: '~/.cursor', workspaceDir: '/workspace' },
    capabilities: null,
    ...overrides,
  })
}

async function selectOption(field: DOMWrapper<Element>, value: string) {
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await flushPromises()
  const option = Array.from(document.body.querySelectorAll<HTMLButtonElement>('[role="option"]'))
    .find((item) => item.dataset.test === `app-select-option-${value}`)
  expect(option).toBeTruthy()
  option!.click()
  await flushPromises()
}

describe('AgentMetaPanel credential hints', () => {
  beforeEach(() => {
    mocks.listCredentials.mockResolvedValue({ items: [] })
    mocks.listModelConfigurations.mockResolvedValue({ items: [] })
    mocks.listModelConnections.mockResolvedValue({ items: [] })
    mocks.getModelDefaults.mockResolvedValue({ bindingsByBackend: {} })
    mocks.listToolCapabilities.mockResolvedValue({ items: [{ toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' }, { toolId: 'opencode', modes: ['connection'], protocols: ['openai-chat-completions'], adapterState: 'existing' }] })
    mocks.getSharedAgentConfig.mockRejectedValue(new Error('offline'))
  })

  it('inherits by default and keeps other picker hints', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', env: {} })
    const wrapper = mountPanel(draftFor())
    await flushPromises()
    expect(wrapper.get('[data-testid="model-binding-inherit"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.text()).not.toContain('不用在这里填写')
    expect(wrapper.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="agent-git-credential-picker"]').text()).toContain('添加 Git 授权')
    expect(wrapper.find('[data-testid="credential-alias-picker"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows legacy SSH selection without migrating until the user edits Git', async () => {
    mocks.listCredentials.mockResolvedValue({ items: [{ id: 'ssh-1', name: '部署私钥', type: 'ssh', envKey: 'GIT_SSH_PRIVATE_KEY', configured: true }, { id: 'hosts-1', name: '主机校验', type: 'ssh', envKey: 'GIT_SSH_KNOWN_HOSTS', configured: true }] })
    const draft = draftFor({ gitCredentialType: 'ssh', gitCredentialId: 'ssh-1', sshHostsCredentialId: 'hosts-1' })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.get('[data-testid="git-credential-row-ssh-1"]').text()).toContain('部署私钥')
    expect(draft).not.toHaveProperty('gitBindings')
    await wrapper.get('[data-testid="git-remove-ssh-1"]').trigger('click')
    expect(draft.gitBindings).toEqual({ items: [] })
    expect(draft.gitCredentialId).toBe('')
    expect(draft.sshHostsCredentialId).toBe('')
    wrapper.unmount()
  })

  it('shows generic-only legacy Git references without migrating and permits an explicit list edit', async () => {
    mocks.listCredentials.mockResolvedValue({ items: [{ id: 'gh', name: '团队 GitHub', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true }, { id: 'gl', name: '团队 GitLab', provider: 'gitlab', envKey: 'GITLAB_TOKEN', configured: true }] })
    const draft = draftFor({ gitCredentialType: undefined, credentialBindings: { GITHUB_TOKEN: 'gh', GITLAB_TOKEN: 'gl' } })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.find('[data-testid="git-credential-row-gh"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="git-credential-row-gl"]').exists()).toBe(true)
    expect(draft).not.toHaveProperty('gitBindings')
    expect(draft.credentialBindings).toEqual({ GITHUB_TOKEN: 'gh', GITLAB_TOKEN: 'gl' })
    await wrapper.get('[data-testid="git-preferred-gl"]').trigger('click')
    expect(draft.gitBindings).toEqual({ items: [{ credentialId: 'gh' }, { credentialId: 'gl' }], preferredCredentialId: 'gl' })
    expect(draft.credentialBindings).toEqual({})
    wrapper.unmount()
  })

  it('does not claim workspace inheritance when a legacy Git disable is explicitly saved', async () => {
    mocks.listCredentials.mockResolvedValue({ items: [{ id: 'shared-gh', name: '共享 GitHub', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true }] })
    mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', env: {}, gitBindings: { items: [{ credentialId: 'shared-gh' }], preferredCredentialId: 'shared-gh' } })
    const draft = draftFor({ gitCredentialType: undefined, credentialBindings: { GITHUB_TOKEN: null } })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.get('[data-testid="git-credential-legacy"]').text()).toContain('当前使用原有授权')
    expect(wrapper.find('[data-testid="git-credential-row-shared-gh"]').exists()).toBe(false)
    expect(draft).not.toHaveProperty('gitBindings')
    expect(draft.credentialBindings).toEqual({ GITHUB_TOKEN: null })
    wrapper.unmount()
  })

  it('shows the credential list only after leaving the default config', async () => {
    mocks.listCredentials.mockResolvedValue({
      items: [
        { id: 'shared-cursor', name: '工作号', type: 'ai', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: true },
        { id: 'own-cursor', name: '个人号', type: 'ai', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: true },
      ],
    })
    mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', aiCredentialId: 'shared-cursor', env: {} })
    const inherited = mountPanel(draftFor())
    await flushPromises()
    expect(inherited.get('[data-testid="model-binding-inherit"]').attributes('aria-pressed')).toBe('true')
    expect(inherited.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(false)
    await inherited.get('[data-testid="model-binding-advanced"] summary').trigger('click')
    await inherited.get('[data-testid="model-binding-platform"]').trigger('click')
    await flushPromises()
    expect(inherited.get('[data-testid="model-binding-credential"]').exists()).toBe(true)
    expect(inherited.find('[data-testid="credential-alias-inherited"]').exists()).toBe(false)
    inherited.unmount()

    const draft = draftFor({ modelBinding: { kind: 'platform', credentialId: 'own-cursor' } })
    const own = mountPanel(draft)
    await flushPromises()
    expect(own.get('[data-testid="model-binding-inherit"]').attributes('aria-pressed')).toBe('false')
    expect(draft.modelBinding).toEqual({ kind: 'platform', credentialId: 'own-cursor' })
    expect(own.get('[data-testid="model-binding-credential"] [data-test="app-select-trigger"]').text()).toContain('个人号')
    await selectOption(own.get('[data-testid="model-binding-credential"]'), 'shared-cursor')
    expect(draft.modelBinding).toEqual({ kind: 'platform', credentialId: 'shared-cursor', modelId: undefined })
    await own.get('[data-testid="model-binding-inherit"]').trigger('click')
    await flushPromises()
    expect(draft.aiCredentialId).toBe('')
    expect(own.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(false)
    own.unmount()
  })

  it('hides the OpenCode credential hint and keeps the picker', async () => {
    const wrapper = mountPanel(draftFor({ acpBackend: 'opencode', gitCredentialType: undefined }))
    await flushPromises()
    expect(wrapper.get('[data-testid="model-binding-connection"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="model-binding-platform"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="credential-alias-picker"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('未选择时使用种类匹配的共享通用授权')
    expect(wrapper.text()).not.toContain('点一条给这个 Agent')
    wrapper.unmount()
  })
})

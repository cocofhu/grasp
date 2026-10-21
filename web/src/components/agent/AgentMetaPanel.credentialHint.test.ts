// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentMetaPanel from './AgentMetaPanel.vue'

const mocks = vi.hoisted(() => ({
  listCredentials: vi.fn(),
  getSharedAgentConfig: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listCredentials: mocks.listCredentials,
      getSharedAgentConfig: mocks.getSharedAgentConfig,
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
      stubs: { Icon: true, AppModal: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' } },
    },
  })
}

function draftFor(overrides: Record<string, unknown> = {}) {
  return reactive({
    name: 'e2e-agent',
    acpBackend: 'cursor',
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

function pickerText(wrapper: ReturnType<typeof mountPanel>, kind: string): string {
  return wrapper.get(`[data-testid="credential-alias-picker"][data-kind="${kind}"]`).text()
}

describe('AgentMetaPanel credential hints', () => {
  beforeEach(() => {
    mocks.listCredentials.mockResolvedValue({ items: [] })
    mocks.getSharedAgentConfig.mockRejectedValue(new Error('offline'))
  })

  it('inherits by default and keeps other picker hints', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', env: {} })
    const wrapper = mountPanel(draftFor())
    await flushPromises()
    expect(wrapper.get('[data-test="agent-inherit-default-switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.text()).not.toContain('不用在这里填写')
    expect(wrapper.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(false)
    const github = pickerText(wrapper, 'github')
    expect(github).toContain('清空后不会改用另一条')
    expect(github).not.toContain('共享通用授权')
    wrapper.unmount()
  })

  it('keeps the original hint on SSH key and known-hosts pickers', async () => {
    const wrapper = mountPanel(draftFor({ gitCredentialType: 'ssh' }))
    await flushPromises()
    for (const kind of ['ssh_key', 'ssh_hosts']) {
      const text = pickerText(wrapper, kind)
      expect(text).toContain('清空后不会改用另一条')
      expect(text).not.toContain('共享通用授权')
    }
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
    expect(inherited.get('[data-test="agent-inherit-default-switch"]').attributes('aria-checked')).toBe('true')
    expect(inherited.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(false)
    await inherited.get('[data-test="agent-inherit-default-switch"]').trigger('click')
    await flushPromises()
    expect(inherited.get('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(true)
    expect(inherited.find('[data-testid="credential-alias-inherited"]').exists()).toBe(false)
    inherited.unmount()

    const draft = draftFor({ aiCredentialId: 'own-cursor' })
    const own = mountPanel(draft)
    await flushPromises()
    expect(own.get('[data-test="agent-inherit-default-switch"]').attributes('aria-checked')).toBe('true')
    expect(draft.aiCredentialId).toBe('own-cursor')
    expect(own.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(false)
    await own.get('[data-test="agent-inherit-default-switch"]').trigger('click')
    await flushPromises()
    expect(own.get('[data-testid="credential-alias-option-own-cursor"]').attributes('aria-pressed')).toBe('true')
    await own.get('[data-test="agent-inherit-default-switch"]').trigger('click')
    await flushPromises()
    expect(draft.aiCredentialId).toBe('')
    expect(own.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(false)
    own.unmount()
  })

  it('hides the OpenCode credential hint and keeps the picker', async () => {
    const wrapper = mountPanel(draftFor({ acpBackend: 'opencode', gitCredentialType: undefined }))
    await flushPromises()
    expect(wrapper.find('[data-testid="credential-opencode"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="credential-alias-picker"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('未选择时使用种类匹配的共享通用授权')
    expect(wrapper.text()).not.toContain('点一条给这个 Agent')
    wrapper.unmount()
  })
})

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
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, listCredentials: mocks.listCredentials } }
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
  })

  it('hides the coding credential hint and keeps other picker hints', async () => {
    const wrapper = mountPanel(draftFor())
    await flushPromises()
    const cursor = pickerText(wrapper, 'cursor')
    expect(cursor).not.toContain('未选择时使用种类匹配的共享通用授权')
    expect(cursor).not.toContain('主标题是别名')
    expect(wrapper.find('[data-testid="credential-alias-picker"][data-kind="cursor"]').exists()).toBe(true)
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

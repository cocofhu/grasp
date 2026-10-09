// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentMetaPanel from './AgentMetaPanel.vue'

const mocks = vi.hoisted(() => ({
  getProjectCredentials: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, getProjectCredentials: mocks.getProjectCredentials } }
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
      projects: [{ id: 'p1', name: '演示' }],
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
    projectId: 'p1',
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
    mocks.getProjectCredentials.mockResolvedValue({ items: [] })
  })

  it('uses the shared fallback only on the coding credential picker', async () => {
    const wrapper = mountPanel(draftFor())
    await flushPromises()
    expect(pickerText(wrapper, 'cursor')).toContain('未选择时使用种类匹配的共享通用授权')
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

  it('keeps the shared fallback on the OpenCode coding picker', async () => {
    const wrapper = mountPanel(draftFor({ acpBackend: 'opencode', gitCredentialType: undefined }))
    await flushPromises()
    expect(wrapper.text()).toContain('未选择时使用种类匹配的共享通用授权')
    expect(wrapper.find('[data-testid="credential-alias-picker"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

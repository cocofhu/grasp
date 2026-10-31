// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { CredentialItem } from '@/lib/api/api'
import WorkspaceCredentialsPanel from './WorkspaceCredentialsPanel.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  status: vi.fn(),
  start: vi.fn(),
  poll: vi.fn(),
  cancel: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    listCredentials: mocks.list,
    createCredential: mocks.create,
    listModelConnections: vi.fn().mockResolvedValue({ items: [] }),
    listModelConfigurations: vi.fn().mockResolvedValue({ items: [] }),
    listToolCapabilities: vi.fn().mockResolvedValue({ items: [] }),
    getGitHubDeviceLogin: mocks.status,
    startGitHubDeviceLogin: mocks.start,
    pollGitHubDeviceLogin: mocks.poll,
    cancelGitHubDeviceLogin: mocks.cancel,
  },
}))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))

const github: CredentialItem = { id: 'gh-1', type: 'git', purpose: 'git', provider: 'github', host: 'https://github.com', envKey: 'GITHUB_TOKEN', name: 'Team', configured: true, masked: '••••••••', metadata: { authMethod: 'device_flow', githubLogin: 'cocofhu' } }
const enterprise: CredentialItem = { id: 'ghe-1', type: 'git', purpose: 'git', provider: 'github', host: 'https://github.example.com', envKey: 'GITHUB_TOKEN', name: 'GHE', configured: true, masked: '••••••••' }

function mountPanel(items: CredentialItem[] = []) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  mocks.list.mockResolvedValue({ items })
  return mount(WorkspaceCredentialsPanel, {
    global: { plugins: [i18n], stubs: { Icon: true, Teleport: true, RouterLink: { template: '<a><slot /></a>' } } },
  })
}

async function openGitHubDetails(wrapper: ReturnType<typeof mountPanel>) {
  await flushPromises()
  await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="workspace-add-credential"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="credential-kind-github"]').trigger('click')
  await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
  await new Promise((resolve) => setTimeout(resolve, 220))
  await flushPromises()
}

const session = { sessionId: 'ghd-1', userCode: 'WDJB-MJHT', verificationUri: 'https://github.com/login/device', expiresIn: 900, interval: 5 }

describe('WorkspaceCredentialsPanel GitHub device sign-in', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.status.mockResolvedValue({ enabled: true })
    mocks.start.mockResolvedValue(session)
    mocks.poll.mockResolvedValue({ status: 'pending', interval: 5 })
    mocks.cancel.mockResolvedValue({ status: 'cancelled' })
  })
  afterEach(() => { vi.useRealTimers() })

  it('keeps the paste-only form when the deployment has no client ID', async () => {
    mocks.status.mockResolvedValue({ enabled: false })
    const wrapper = mountPanel([github])
    await openGitHubDetails(wrapper)
    expect(wrapper.find('[data-testid="credential-auth-login"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="github-device-card"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-value"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="workspace-credential-relogin-gh-1"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('defaults to GitHub sign-in on github.com and falls back to paste for a self-hosted site', async () => {
    const wrapper = mountPanel()
    await openGitHubDetails(wrapper)
    expect(wrapper.get('[data-testid="credential-auth-login"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="github-device-card"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="workspace-credential-create-value"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-review-next"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-host-status"]').text()).toBe('官方站点')

    const host = wrapper.get('[data-testid="workspace-credential-create-host"]')
    await host.setValue('https://github.example.com:8443')
    expect((host.element as HTMLInputElement).value).toBe('github.example.com:8443')
    expect(wrapper.get('[data-testid="workspace-credential-host-status"]').text()).toBe('自建')
    const loginTab = wrapper.get('[data-testid="credential-auth-login"]')
    expect(loginTab.attributes('disabled')).toBeDefined()
    expect(loginTab.attributes('title')).toBe('设备码登录仅支持 github.com')
    expect(wrapper.get('[data-testid="credential-auth-paste"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="workspace-credential-create-value"]').exists()).toBe(true)

    await wrapper.get('[data-testid="workspace-credential-host-reset"]').trigger('click')
    expect((wrapper.get('[data-testid="workspace-credential-create-host"]').element as HTMLInputElement).value).toBe('github.com')
    expect(wrapper.get('[data-testid="credential-auth-login"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('validates the alias locally, then signs in and saves the credential', async () => {
    const wrapper = mountPanel([github])
    await openGitHubDetails(wrapper)
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-alias-error"]').text()).toContain('别名')
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('team')
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-alias-error"]').text()).toContain('Team')
    expect(mocks.start).not.toHaveBeenCalled()

    vi.useFakeTimers()
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Mine')
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledWith({ name: 'Mine' })
    expect(wrapper.get('[data-testid="github-device-code"]').text()).toBe('WDJBMJHT')
    expect(wrapper.get('[data-testid="github-device-open"]').attributes('href')).toBe('https://github.com/login/device')
    expect(wrapper.get('[data-testid="github-device-badge"]').text()).toBe('等待授权')
    expect(wrapper.get('[data-testid="workspace-credential-create-alias"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="credential-auth-paste"]').attributes('disabled')).toBeDefined()

    const saved: CredentialItem = { id: 'cred-new', type: 'git', purpose: 'git', provider: 'github', host: 'https://github.com', envKey: 'GITHUB_TOKEN', name: 'Mine', configured: true, masked: '••••••••', metadata: { authMethod: 'device_flow', githubLogin: 'octocat' } }
    mocks.poll.mockResolvedValueOnce({ status: 'authorized', credential: saved, githubLogin: 'octocat', scopes: ['read:org'], scopeWarning: true })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.get('[data-testid="github-device-login"]').text()).toBe('@octocat')
    expect(wrapper.find('[data-testid="github-device-scope-warning"]').exists()).toBe(true)
    expect(wrapper.emitted('credentialSaved')?.[0]).toEqual([saved])
    expect(mocks.success).toHaveBeenCalled()

    // A scope warning keeps the dialog open until the user confirms.
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.find('[data-testid="github-device-card"]').exists()).toBe(true)
    await wrapper.get('[data-testid="github-device-done"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="github-device-card"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-github-login-cred-new"]').text()).toBe('@octocat')
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('closing the dialog while waiting cancels the session', async () => {
    const wrapper = mountPanel()
    await openGitHubDetails(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Mine')
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('button[aria-label="关闭"]').trigger('click')
    await flushPromises()
    expect(mocks.cancel).toHaveBeenCalledWith('ghd-1')
    wrapper.unmount()
  })

  it('offers re-login only for github.com credentials and starts it with the credential id', async () => {
    const wrapper = mountPanel([github, enterprise])
    await flushPromises()
    expect(wrapper.get('[data-testid="workspace-credential-github-login-gh-1"]').text()).toBe('@cocofhu')
    expect(wrapper.find('[data-testid="workspace-credential-relogin-ghe-1"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-relogin-gh-1"]').trigger('click')
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledWith({ credentialId: 'gh-1' })
    expect(wrapper.get('[data-testid="github-device-code"]').exists()).toBe(true)
    await wrapper.get('[data-testid="workspace-credential-relogin-close"]').trigger('click')
    await flushPromises()
    expect(mocks.cancel).toHaveBeenCalledWith('ghd-1')
    wrapper.unmount()
  })

  it('shows a retry after GitHub rejects the request', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('GitHub: device_flow_disabled'), { status: 502, code: 'device_flow_disabled' }))
    const wrapper = mountPanel()
    await openGitHubDetails(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Mine')
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="github-device-failed"]').text()).toContain('Enable Device Flow')
    await wrapper.get('[data-testid="github-device-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="github-device-code"]').exists()).toBe(true)
    wrapper.unmount()
  })
})

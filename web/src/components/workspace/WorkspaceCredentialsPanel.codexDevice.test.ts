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
    getGitHubDeviceLogin: vi.fn().mockResolvedValue({ enabled: false }),
    getCodexDeviceLogin: mocks.status,
    startCodexDeviceLogin: mocks.start,
    pollCodexDeviceLogin: mocks.poll,
    cancelCodexDeviceLogin: mocks.cancel,
  },
}))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))

const codex: CredentialItem = {
  id: 'cx-1', type: 'ai', purpose: 'tool_auth', toolId: 'codex', provider: 'codex', secretKind: 'login_file', envKey: 'GRASP_CODEX_AUTH_JSON', name: 'Team Codex', configured: true, masked: '••••••••',
  metadata: { authMethod: 'device_code' },
  codexLogin: { email: 'team@example.com', lastRefresh: '2026-11-01T08:00:00Z', status: 'needs_relogin' },
}

function mountPanel(items: CredentialItem[] = [], props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  mocks.list.mockResolvedValue({ items })
  return mount(WorkspaceCredentialsPanel, {
    props,
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true, Teleport: true, RouterLink: { template: '<a><slot /></a>' } } },
  })
}

async function openCodexDetails(wrapper: ReturnType<typeof mountPanel>) {
  await flushPromises()
  await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="workspace-add-credential"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="credential-kind-codex"]').trigger('click')
  await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
  await new Promise((resolve) => setTimeout(resolve, 220))
  await flushPromises()
}

const session = { sessionId: 'cdx-1', userCode: 'ABCD-1234', verificationUri: 'https://auth.openai.com/codex/device', expiresIn: 900, interval: 5 }
const copy = pages.pages.workspace.credentials.codexDevice

describe('WorkspaceCredentialsPanel Codex device sign-in', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.status.mockResolvedValue({ enabled: true })
    mocks.start.mockResolvedValue(session)
    mocks.poll.mockResolvedValue({ status: 'pending', interval: 5 })
    mocks.cancel.mockResolvedValue({ status: 'cancelled' })
  })
  afterEach(() => { vi.useRealTimers() })

  it('shows email, renewal status and last renewal on the row without any token', async () => {
    const wrapper = mountPanel([codex])
    await flushPromises()
    expect(wrapper.get('[data-testid="workspace-credential-codex-email-cx-1"]').text()).toBe('team@example.com')
    const status = wrapper.get('[data-testid="workspace-credential-codex-status-cx-1"]').text()
    expect(status).toContain(copy.status.needs_relogin)
    expect(status).toContain('·')
    expect(wrapper.get('[data-testid="workspace-credential-row"]').text()).toContain(copy.authMethodTag)
    wrapper.unmount()
  })

  it('keeps paste only, with the refresh-token hint, when device login is off', async () => {
    mocks.status.mockResolvedValue({ enabled: false })
    const wrapper = mountPanel([codex])
    await openCodexDetails(wrapper)
    expect(wrapper.find('[data-testid="credential-auth-login"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-value"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="workspace-credential-codex-paste-hint"]').text()).toBe(copy.pasteHint)
    expect(wrapper.find('[data-testid="workspace-credential-relogin-cx-1"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('treats a failed status probe as disabled', async () => {
    mocks.status.mockRejectedValue(new Error('offline'))
    const wrapper = mountPanel([codex])
    await openCodexDetails(wrapper)
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('defaults to device sign-in, can switch to paste, and saves through the device flow', async () => {
    const wrapper = mountPanel()
    await openCodexDetails(wrapper)
    expect(wrapper.get('[data-testid="credential-auth-login"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="workspace-credential-codex-paste-hint"]').exists()).toBe(false)

    await wrapper.get('[data-testid="credential-auth-paste"]').trigger('click')
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-codex-paste-hint"]').exists()).toBe(true)
    await wrapper.get('[data-testid="credential-auth-login"]').trigger('click')

    vi.useFakeTimers()
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Mine')
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledWith({ name: 'Mine' })
    expect(wrapper.get('[data-testid="codex-device-code"]').text()).toBe('ABCD1234')
    expect(wrapper.get('[data-testid="credential-auth-paste"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="codex-device-cancel"]').exists()).toBe(true)

    const saved: CredentialItem = { ...codex, id: 'cx-new', name: 'Mine', codexLogin: undefined }
    mocks.list.mockResolvedValue({ items: [{ ...saved, codexLogin: { email: 'me@example.com', status: 'ok' } }] })
    mocks.poll.mockResolvedValueOnce({ status: 'success', credential: saved, email: 'me@example.com' })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.emitted('credentialSaved')?.[0]).toEqual([saved])
    expect(mocks.success).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="codex-device-done"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-codex-email-cx-new"]').text()).toBe('me@example.com')
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('the footer cancel stops waiting and drops the session', async () => {
    const wrapper = mountPanel()
    await openCodexDetails(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Mine')
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="codex-device-cancel"]').trigger('click')
    await flushPromises()
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('re-logs an existing Codex credential by id and refreshes its status', async () => {
    vi.useFakeTimers()
    const wrapper = mountPanel([codex])
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-relogin-cx-1"]').trigger('click')
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledWith({ credentialId: 'cx-1' })
    expect(wrapper.find('[data-testid="codex-device-code"]').exists()).toBe(true)

    mocks.list.mockResolvedValue({ items: [{ ...codex, codexLogin: { email: 'team@example.com', status: 'ok' } }] })
    mocks.poll.mockResolvedValueOnce({ status: 'success', credential: { ...codex, codexLogin: undefined }, email: 'team@example.com', updated: true })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.get('[data-testid="workspace-credential-codex-status-cx-1"]').text()).toContain(copy.status.ok)
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('closing the re-login dialog cancels the session', async () => {
    const wrapper = mountPanel([codex])
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-relogin-cx-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-relogin-close"]').trigger('click')
    await flushPromises()
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
    wrapper.unmount()
  })

  it('can switch a failed new sign-in to paste without losing the alias', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('not enabled'), { code: 'not_enabled' }))
    const wrapper = mountPanel()
    await openCodexDetails(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('My ChatGPT')
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="codex-device-paste"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="workspace-credential-create-alias"]').element).toHaveProperty('value', 'My ChatGPT')
    expect(wrapper.get('[data-testid="credential-auth-paste"]').attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(wrapper.get('[data-testid="workspace-credential-create-value"]').element)
    wrapper.unmount()
  })

  it('can replace an existing login with a pasted file after device sign-in fails', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('not enabled'), { code: 'not_enabled' }))
    const wrapper = mountPanel([codex])
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-relogin-cx-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="codex-device-paste"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-device-card"]').exists()).toBe(false)
    expect(document.activeElement).toBe(wrapper.get('[data-testid="workspace-credential-input-cx-1"]').element)
    expect(wrapper.get('[data-testid="workspace-credential-input-cx-1"]').element).toHaveProperty('value', '')
    wrapper.unmount()
  })

  it('uses the inline editor for re-login and returns to the credential list on cancel', async () => {
    const wrapper = mountPanel([codex], { inline: true, view: 'credentials' })
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-relogin-cx-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('editing')?.at(-1)).toEqual([true])
    expect(wrapper.get('[data-testid="workspace-credential-section-coding"]').isVisible()).toBe(false)
    expect(wrapper.get('[data-testid="credential-inline-editor"]').isVisible()).toBe(true)
    await wrapper.get('[data-testid="workspace-credential-relogin-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('editing')?.at(-1)).toEqual([false])
    expect(wrapper.get('[data-testid="workspace-credential-section-coding"]').isVisible()).toBe(true)
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
    wrapper.unmount()
  })
})

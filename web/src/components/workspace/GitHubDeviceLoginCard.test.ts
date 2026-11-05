// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import zhCommon from '@/locales/zh-CN/common.json'
import zhPages from '@/locales/zh-CN/pages.json'
import GitHubDeviceLoginCard from './GitHubDeviceLoginCard.vue'

const mocks = vi.hoisted(() => ({ start: vi.fn(), poll: vi.fn(), cancel: vi.fn() }))
vi.mock('@/lib/api/api', () => ({
  api: {
    startGitHubDeviceLogin: mocks.start,
    pollGitHubDeviceLogin: mocks.poll,
    cancelGitHubDeviceLogin: mocks.cancel,
  },
}))

const session = { sessionId: 'ghd-1', userCode: 'WDJB-MJHT', verificationUri: 'https://github.com/login/device', expiresIn: 125, interval: 5 }

function mountCard(locale: 'en' | 'zh-CN' = 'en', props: Record<string, unknown> = {}) {
  const messages = locale === 'en'
    ? { en: { ...enCommon, ...enPages } }
    : { 'zh-CN': { ...zhCommon, ...zhPages } }
  const i18n = createI18n({ legacy: false, locale, messages })
  return mount(GitHubDeviceLoginCard, { props: { name: 'Mine', ...props }, global: { plugins: [i18n], stubs: { Icon: true, AppSpinner: true } } })
}

function stubSecureContext(value: boolean) {
  Object.defineProperty(window, 'isSecureContext', { configurable: true, get: () => value })
}

describe('GitHubDeviceLoginCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubSecureContext(true)
    Object.defineProperty(document, 'execCommand', { configurable: true, writable: true, value: vi.fn().mockReturnValue(false) })
    mocks.start.mockResolvedValue(session)
    mocks.poll.mockResolvedValue({ status: 'pending', interval: 5 })
    mocks.cancel.mockResolvedValue({ status: 'cancelled' })
  })
  afterEach(() => { vi.useRealTimers() })

  it('shows the steps and verification code, and never the requested scopes', async () => {
    const wrapper = mountCard('zh-CN')
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    const card = wrapper.get('[data-testid="github-device-card"]')
    expect(card.text()).toContain(zhPages.pages.workspace.credentials.githubDevice.step1)
    expect(card.text()).toContain(zhPages.pages.workspace.credentials.githubDevice.step2)
    expect(wrapper.get('[data-testid="github-device-code"]').text()).toBe('WDJBMJHT')
    expect(card.text()).not.toContain('申请权限')
    expect(card.text()).not.toMatch(/\brepo\b/)
    expect(wrapper.find('[data-testid="github-device-authorized"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="github-device-failed"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('copies the full verification code without a clipboard API', async () => {
    stubSecureContext(false)
    const writeText = vi.fn()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    let copied = ''
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      writable: true,
      value: vi.fn(() => {
        copied = document.querySelector('textarea')?.value ?? ''
        return true
      }),
    })
    const wrapper = mountCard()
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="github-device-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).not.toHaveBeenCalled()
    expect(copied).toBe('WDJB-MJHT')
    expect(wrapper.get('[data-testid="github-device-copy"]').text()).toBe(enPages.pages.workspace.credentials.githubDevice.copied)
    wrapper.unmount()
  })

  it('shows copy failure only when both clipboard paths fail, then clears it for a new code', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const wrapper = mountCard()
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="github-device-copy"]').trigger('click')
    await flushPromises()
    expect(document.execCommand).toHaveBeenCalledWith('copy')
    expect(wrapper.get('[data-testid="github-device-copy"]').text()).toBe(enPages.pages.workspace.credentials.githubDevice.copyFailed)
    mocks.poll.mockResolvedValueOnce({ status: 'expired' })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    mocks.start.mockResolvedValueOnce({ ...session, userCode: 'WXYZ-5678' })
    await wrapper.get('[data-testid="github-device-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="github-device-copy"]').text()).toBe(enPages.pages.workspace.credentials.githubDevice.copyCode)
    expect(wrapper.find('[data-testid="github-device-failed"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('offers paste after a failure and does not show waiting content at the same time', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('GitHub: device_flow_disabled'), { status: 502, code: 'device_flow_disabled' }))
    const wrapper = mountCard()
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="github-device-code"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="github-device-authorized"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="github-device-failed"]').text()).toContain('Enable Device Flow')
    await wrapper.get('[data-testid="github-device-paste"]').trigger('click')
    expect(wrapper.emitted('paste')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps the missing-repo warning on the saved result', async () => {
    vi.useFakeTimers()
    const wrapper = mountCard()
    await wrapper.get('[data-testid="github-device-start"]').trigger('click')
    await flushPromises()
    mocks.poll.mockResolvedValueOnce({ status: 'authorized', credential: { id: 'c1', name: 'Mine' }, githubLogin: 'octocat', scopeWarning: true })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.find('[data-testid="github-device-code"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="github-device-failed"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="github-device-scope-warning"]').text()).toContain('repo')
    wrapper.unmount()
  })

  it('does not call the verification code a PIN in either language', () => {
    const zh = JSON.stringify(zhPages.pages.workspace.credentials.githubDevice)
    const en = JSON.stringify(enPages.pages.workspace.credentials.githubDevice)
    expect(zh).not.toMatch(/\bPIN\b/)
    expect(en).not.toMatch(/\bPIN\b/i)
    expect(zh).toContain('设备码')
    expect(zh).toContain('验证码')
    expect(en).toContain('device code')
    expect(en).toContain('verification code')
  })
})

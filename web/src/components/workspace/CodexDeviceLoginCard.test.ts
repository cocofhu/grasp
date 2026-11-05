// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/en/common.json'
import pages from '@/locales/en/pages.json'
import CodexDeviceLoginCard from './CodexDeviceLoginCard.vue'

const mocks = vi.hoisted(() => ({ start: vi.fn(), poll: vi.fn(), cancel: vi.fn() }))
vi.mock('@/lib/api/api', () => ({
  api: {
    startCodexDeviceLogin: mocks.start,
    pollCodexDeviceLogin: mocks.poll,
    cancelCodexDeviceLogin: mocks.cancel,
  },
}))

const session = { sessionId: 'cdx-1', userCode: 'ABCD-1234', verificationUri: 'https://auth.openai.com/codex/device', expiresIn: 125, interval: 5 }

function mountCard(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { ...common, ...pages } } })
  return mount(CodexDeviceLoginCard, { props: { name: 'Mine', ...props }, global: { plugins: [i18n], stubs: { Icon: true, AppSpinner: true } } })
}

function stubSecureContext(value: boolean) {
  Object.defineProperty(window, 'isSecureContext', { configurable: true, get: () => value })
}

describe('CodexDeviceLoginCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubSecureContext(true)
    Object.defineProperty(document, 'execCommand', { configurable: true, writable: true, value: vi.fn().mockReturnValue(false) })
    mocks.start.mockResolvedValue(session)
    mocks.poll.mockResolvedValue({ status: 'pending', interval: 5 })
    mocks.cancel.mockResolvedValue({ status: 'cancelled' })
  })
  afterEach(() => { vi.useRealTimers() })

  it('shows the code, copies it and reports the saved account', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const wrapper = mountCard()
    expect(wrapper.emitted('state')?.[0]).toEqual(['idle'])
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledWith({ name: 'Mine' })
    expect(wrapper.get('[data-testid="codex-device-code"]').text()).toBe('ABCD1234')
    expect(wrapper.get('[data-testid="codex-device-open"]').attributes('href')).toBe('https://auth.openai.com/codex/device')
    expect(wrapper.get('[data-testid="codex-device-remaining"]').text()).toBe('2:05')
    expect(wrapper.find('[data-testid="codex-device-badge"]').exists()).toBe(true)

    await wrapper.get('[data-testid="codex-device-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('ABCD-1234')
    expect(wrapper.get('[data-testid="codex-device-copy"]').text()).toBe(pages.pages.workspace.credentials.codexDevice.copied)
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.get('[data-testid="codex-device-copy"]').text()).toBe(pages.pages.workspace.credentials.codexDevice.copyCode)

    writeText.mockRejectedValueOnce(new Error('denied'))
    await wrapper.get('[data-testid="codex-device-copy"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-device-copy"]').text()).toBe(pages.pages.workspace.credentials.codexDevice.copyFailed)
    expect(document.execCommand).toHaveBeenCalledWith('copy')

    const credential = { id: 'cred-1', type: 'ai', name: 'Mine', configured: true }
    mocks.poll.mockResolvedValueOnce({ status: 'success', credential, email: 'dev@example.com', updated: true })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-device-email"]').text()).toBe('dev@example.com')
    expect(wrapper.get('[data-testid="codex-device-success"]').text()).toContain('Mine')
    expect(wrapper.emitted('success')?.[0]?.[0]).toMatchObject({ credential })
    wrapper.unmount()
  })

  it('copies the full code when the page is not a secure context', async () => {
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
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="codex-device-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).not.toHaveBeenCalled()
    expect(copied).toBe('ABCD-1234')
    expect(wrapper.get('[data-testid="codex-device-copy"]').text()).toBe(pages.pages.workspace.credentials.codexDevice.copied)
    wrapper.unmount()
  })

  it('never links outside auth.openai.com', async () => {
    mocks.start.mockResolvedValue({ ...session, verificationUri: 'https://evil.example.com/device' })
    const wrapper = mountCard({ autoStart: true, credentialId: 'cred-9' })
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledWith({ credentialId: 'cred-9' })
    expect(wrapper.get('[data-testid="codex-device-open"]').attributes('href')).toBe('https://auth.openai.com/codex/device')
    wrapper.unmount()
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
  })

  it('stays idle when validation fails', async () => {
    const wrapper = mountCard({ validate: () => false })
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    expect(mocks.start).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('hands an alias conflict back to the alias field', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('taken'), { status: 409, code: 'alias_taken', alias: 'Mine' }))
    const wrapper = mountCard()
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('aliasTaken')?.[0]).toEqual(['Mine'])
    expect(wrapper.find('[data-testid="codex-device-start"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('clears copy feedback when a retry issues a different code', async () => {
    vi.useFakeTimers()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) }, configurable: true })
    mocks.poll.mockResolvedValueOnce({ status: 'expired' })
    const wrapper = mountCard()
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(4500)
    await wrapper.get('[data-testid="codex-device-copy"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-device-copy"]').text()).toContain('Copy failed')
    // Expiry arrives before the two-second clipboard feedback timer finishes.
    await vi.advanceTimersByTimeAsync(500)
    mocks.start.mockResolvedValueOnce({ ...session, userCode: 'WXYZ-5678' })
    await wrapper.get('[data-testid="codex-device-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-device-code"]').text()).toBe('WXYZ5678')
    expect(wrapper.get('[data-testid="codex-device-copy"]').text()).toBe(pages.pages.workspace.credentials.codexDevice.copyCode)
    wrapper.unmount()
  })

  it.each([
    [{ status: 'expired' }, 'expired'],
    [{ status: 'denied' }, 'denied'],
    [{ status: 'not_enabled' }, 'notEnabled'],
    [{ status: 'error', error: 'exchange_failed' }, 'errors.exchange_failed'],
    [{ status: 'error', error: 'something_new' }, 'errors.unknown'],
  ])('explains the terminal result %j and offers a retry', async (reply, key) => {
    vi.useFakeTimers()
    mocks.poll.mockResolvedValueOnce(reply)
    const wrapper = mountCard()
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    const copy = pages.pages.workspace.credentials.codexDevice as Record<string, unknown>
    const expected = key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown>)[part], copy) as string
    expect(wrapper.get('[data-testid="codex-device-failed"]').text()).toContain(expected.replace('{alias}', 'Mine'))
    await wrapper.get('[data-testid="codex-device-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="codex-device-code"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows the server message for an unrecognised start failure', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('OpenAI said no'), { status: 502, code: 'weird' }))
    const wrapper = mountCard()
    await wrapper.get('[data-testid="codex-device-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-device-failed"]').text()).toContain('OpenAI said no')
    wrapper.unmount()
  })
})

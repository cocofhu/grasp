// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SoloSession } from '@/lib/api/apiTypes'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'

const api = vi.hoisted(() => ({ listSoloSessions: vi.fn(), closeSoloSession: vi.fn(), stopSoloEnvironment: vi.fn(), getSoloSession: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api }))

const session = (id: string, status: SoloSession['status'], sandboxId?: number): SoloSession => ({ id, agentName: 'Demo', title: id, status, sandboxId, busy: false, waiting: 0, restarts: 0, createdAt: '', updatedAt: '' })
const wrappers: ReturnType<typeof mount>[] = []

async function open() {
  const { useSoloSessions } = await import('@/lib/solo/useSoloSessions')
  await useSoloSessions().refresh()
  const { default: SoloLimitModal } = await import('./SoloLimitModal.vue')
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...(pages as object), ...(common as object) } } })
  const wrapper = mount(SoloLimitModal, { props: { open: true }, attachTo: document.body, global: { plugins: [i18n], directives: { 'hover-ink': {} } } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('Solo limit dialog', () => {
  beforeEach(() => {
    vi.resetModules()
    api.listSoloSessions.mockReset()
    api.closeSoloSession.mockReset()
    api.stopSoloEnvironment.mockReset()
  })
  afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount() })

  it('shows workspace-wide usage, failed sessions holding quota and the unlisted remainder', async () => {
    api.listSoloSessions.mockResolvedValue({
      sessions: [session('mine-running', 'running', 1), session('mine-failed', 'error', 2), session('mine-failed-unbound', 'error'), session('mine-closed', 'closed', 3)],
      maxSandboxes: 32,
      usedSandboxes: 32,
    })
    await open()
    const text = document.body.querySelector('[data-testid="solo-limit-modal"]')!.textContent!
    expect(text).toContain('已用 32 / 32')
    expect(text).toContain('mine-running')
    expect(text).toContain('mine-failed')
    expect(text).not.toContain('mine-failed-unbound')
    expect(text).not.toContain('mine-closed')
    expect(document.body.querySelector('[data-testid="solo-limit-others"]')!.textContent).toContain('另有 30 个')
  })

  it('closes a failed session instead of stopping its environment', async () => {
    api.listSoloSessions.mockResolvedValue({ sessions: [session('mine-failed', 'error', 2)], maxSandboxes: 1, usedSandboxes: 1 })
    api.closeSoloSession.mockResolvedValue(session('mine-failed', 'closed', 2))
    const wrapper = await open()
    const button = [...document.body.querySelectorAll('button')].find(b => b.textContent?.trim() === '关闭' && b.closest('li'))!
    button.click()
    await flushPromises()
    expect(api.closeSoloSession).toHaveBeenCalledWith('mine-failed')
    expect(api.stopSoloEnvironment).not.toHaveBeenCalled()
    expect(wrapper.emitted('freed')).toHaveLength(1)
  })
})

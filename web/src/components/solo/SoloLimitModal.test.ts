// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SoloSession } from '@/lib/api/apiTypes'
import pages from '@/locales/zh-CN/pages.json'

const api = vi.hoisted(() => ({
  listSoloSessions: vi.fn(),
  stopSoloSession: vi.fn(),
  closeSoloSession: vi.fn(),
}))
vi.mock('@/lib/api/api', () => ({ api }))

import SoloLimitModal from './SoloLimitModal.vue'
import { isSoloActive, isSoloComputing, useSoloSessions } from '@/lib/solo/useSoloSessions'

const row = (id: string, status: SoloSession['status'], over: Partial<SoloSession> = {}) =>
  ({ id, title: id, agentName: 'Coder', status, busy: false, repos: [], ...over }) as unknown as SoloSession

function mountModal() {
  return mount(SoloLimitModal, {
    props: { open: true },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': pages } })],
      stubs: {
        Icon: true,
        AppModal: { props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" /></div>' },
      },
    },
  })
}

describe('SoloLimitModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    const store = useSoloSessions()
    store.sessions.value = []
    store.usedSandboxes.value = 0
  })

  it('counts stopped sessions as open but not as holding a sandbox', () => {
    expect(['starting', 'running', 'stopping', 'stopped'].every((s) => isSoloActive({ status: s as SoloSession['status'] }))).toBe(true)
    expect(isSoloActive({ status: 'closed' })).toBe(false)
    expect(isSoloComputing({ status: 'stopping' })).toBe(true)
    expect(isSoloComputing({ status: 'stopped' })).toBe(false)
  })

  it('lists only sessions holding a sandbox and stops an idle running one', async () => {
    const store = useSoloSessions()
    store.upsert(row('a', 'running'))
    store.upsert(row('b', 'running', { busy: true }))
    store.upsert(row('c', 'stopped'))
    api.stopSoloSession.mockResolvedValueOnce(row('a', 'stopping'))
    const wrapper = mountModal()
    await flushPromises()
    const items = wrapper.findAll('li')
    expect(items.map((li) => li.text()).join('|')).not.toContain('c')
    expect(items).toHaveLength(2)
    expect(wrapper.find('[data-testid="solo-limit-stop-b"]').exists()).toBe(false)
    await wrapper.get('[data-testid="solo-limit-stop-a"]').trigger('click')
    await flushPromises()
    expect(api.stopSoloSession).toHaveBeenCalledWith('a')
    expect(store.computing.value.find((s) => s.id === 'a')?.status).toBe('stopping')
    expect(wrapper.find('[data-testid="solo-limit-stop-a"]').exists()).toBe(false)
  })

  it('shows a stop failure', async () => {
    useSoloSessions().upsert(row('a', 'running'))
    api.stopSoloSession.mockRejectedValueOnce(new Error('backup unavailable'))
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="solo-limit-stop-a"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('backup unavailable')
  })

  it('shows workspace-wide usage, failed sessions holding a sandbox and slots held by others', async () => {
    const store = useSoloSessions()
    store.upsert(row('mine', 'running'))
    store.upsert(row('broken', 'error', { sandboxId: 7 }))
    store.upsert(row('gone', 'error'))
    store.usedSandboxes.value = 4
    store.maxSandboxes.value = 4
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.text()).toContain('已用 4 / 4')
    const titles = wrapper.findAll('li').map((li) => li.text())
    expect(titles.some((t) => t.includes('broken'))).toBe(true)
    expect(titles.some((t) => t.includes('gone'))).toBe(false)
    expect(wrapper.get('[data-testid="solo-limit-others"]').text()).toContain('2')
  })
})

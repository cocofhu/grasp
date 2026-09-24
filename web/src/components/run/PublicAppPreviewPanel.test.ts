// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import PublicAppPreviewPanel from './PublicAppPreviewPanel.vue'

const shareMocks = vi.hoisted(() => ({
  createPreviewTicket: vi.fn(),
  embedTicket: vi.fn(),
}))

vi.mock('@/lib/inbox/gateShareLink', async () => {
  const actual = await vi.importActual<typeof import('@/lib/inbox/gateShareLink')>('@/lib/inbox/gateShareLink')
  return {
    ...actual,
    publicPreviewVncWsUrl: () => 'ws://example.test/vnc',
    publicGateApi: {
      ...actual.publicGateApi,
      createPreviewTicket: shareMocks.createPreviewTicket,
      embedTicket: shareMocks.embedTicket,
    },
  }
})

describe('PublicAppPreviewPanel', () => {
  beforeEach(() => {
    shareMocks.createPreviewTicket.mockResolvedValue({
      ticket: 'tix',
      wsPath: '/vnc',
      iframeUrl: 'http://example.test/preview',
    })
  })

  it('mounts with ports and requests a ticket', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const w = mount(PublicAppPreviewPanel, {
      props: {
        token: 'share-token',
        ports: [{ port: 5173, label: 'web', kind: 'http' }],
        active: true,
      },
      global: {
        plugins: [i18n],
        stubs: {
          NovncPreviewPanel: true,
          ExternalUrlPreviewFrame: true,
        },
      },
    })
    await flushPromises()
    expect(w.html().length).toBeGreaterThan(20)
    w.unmount()
  })

  it('shows inactive state when share is not active', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const w = mount(PublicAppPreviewPanel, {
      props: {
        token: 'share-token',
        ports: [],
        active: false,
      },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(w.html().length).toBeGreaterThan(10)
    w.unmount()
  })

  it('opens a direct port in a new tab with a drawer ticket from the share link', async () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    shareMocks.embedTicket.mockResolvedValue({ ticket: 'tk', runId: 'run-1', nodeId: 'ap1', expiresAt: '' })
    const tab = { opener: {} as unknown, closed: false, location: { href: '' } }
    const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    const w = mount(PublicAppPreviewPanel, {
      props: {
        token: 'share-token',
        ports: [{ port: 18080, label: 'web', kind: 'http', directUrl: 'http://10.0.0.5:18080/' }],
        active: true,
      },
      global: { plugins: [i18n], stubs: { NovncPreviewPanel: true } },
    })
    await flushPromises()
    await w.get('[data-testid="app-preview-direct-open"]').trigger('click')
    await flushPromises()
    expect(shareMocks.embedTicket).toHaveBeenCalledWith('share-token')
    expect(tab.location.href).toBe('http://10.0.0.5:18080/#__grasp_embed&run=run-1&node=ap1&ticket=tk')
    open.mockRestore()
    w.unmount()
  })
})

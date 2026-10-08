// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import PublicAppPreviewPanel from './PublicAppPreviewPanel.vue'
import { locale } from '@/lib/shared/locale'

const shareMocks = vi.hoisted(() => ({
  createPreviewTicket: vi.fn(),
  previewTicket: vi.fn(),
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
      previewTicket: shareMocks.previewTicket,
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

  it('direct port keeps noVNC and opens a new tab with a drawer ticket from the share link', async () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    shareMocks.previewTicket.mockResolvedValue({ status: 'active', ticket: 'vnc-tk', wsPath: '/vnc' })
    shareMocks.embedTicket.mockResolvedValue({ ticket: 'tk', runId: 'run-1', nodeId: 'ap1', expiresAt: '' })
    const tab = { opener: {} as unknown, closed: false, location: { href: '' } }
    const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    const w = mount(PublicAppPreviewPanel, {
      props: {
        token: 'share-token',
        ports: [{ port: 18080, label: 'web', kind: 'http', directUrl: 'http://10.0.0.5:18080/' }],
        active: true,
      },
      global: {
        plugins: [i18n],
        stubs: {
          NovncPreviewPanel: {
            props: ['wsUrl'],
            template: '<div data-testid="novnc-stub" :data-ws="wsUrl"><slot name="toolbar-extra" /></div>',
          },
        },
      },
    })
    await flushPromises()
    expect(shareMocks.previewTicket).toHaveBeenCalledWith('share-token', 0, 'vnc', expect.anything())
    expect(w.get('[data-testid="novnc-stub"]').attributes('data-ws')).toBe('ws://example.test/vnc')
    await w.get('[data-testid="app-preview-direct-open"]').trigger('click')
    await flushPromises()
    expect(shareMocks.embedTicket).toHaveBeenCalledWith('share-token')
    expect(tab.location.href).toBe(`http://10.0.0.5:18080/#__grasp_embed&run=run-1&node=ap1&ticket=tk&theme=dark&lang=${locale.value}`)
    open.mockRestore()
    w.unmount()
  })

  it('one desktop ticket per share: port tabs navigate it, and it shows before any port', async () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    shareMocks.previewTicket.mockReset()
    shareMocks.previewTicket.mockResolvedValue({ status: 'active', ticket: 'vnc-tk', wsPath: '/vnc' })
    const stub = {
      props: ['wsUrl', 'targetPort'],
      template: '<div data-testid="novnc-stub" :data-ws="wsUrl" :data-target-port="targetPort" />',
    }
    const w = mount(PublicAppPreviewPanel, {
      props: { token: 'share-token', ports: [], active: true },
      global: { plugins: [i18n], stubs: { NovncPreviewPanel: stub } },
    })
    await flushPromises()
    expect(w.get('[data-testid="novnc-stub"]').attributes('data-target-port')).toBeUndefined()

    await w.setProps({
      ports: [
        { port: 5173, label: 'web', kind: 'http' },
        { port: 8080, label: 'api', kind: 'http' },
      ],
    })
    await flushPromises()
    await w.get('[data-testid="public-gate-app-preview-port-8080"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="novnc-stub"]')).toHaveLength(1)
    expect(w.get('[data-testid="novnc-stub"]').attributes('data-target-port')).toBe('8080')
    const ports = new Set(shareMocks.previewTicket.mock.calls.map((c) => c[1]))
    expect([...ports]).toEqual([0])
    w.unmount()
  })
})

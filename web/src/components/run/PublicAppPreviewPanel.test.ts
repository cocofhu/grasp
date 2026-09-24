// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { toPreviewDocumentURL } from '@/lib/shared/previewDocumentOrigin'
import PublicAppPreviewPanel from './PublicAppPreviewPanel.vue'

const shareMocks = vi.hoisted(() => ({
  createPreviewTicket: vi.fn(),
  previewTicket: vi.fn(),
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
    shareMocks.previewTicket.mockResolvedValue({
      status: 'active',
      ticket: 'tix',
      wsPath: '/vnc',
      iframePath: '/public/gate-approvals/preview-api/tix/',
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
          DirectPreviewFrame: true,
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

  it('direct port window uses the preview-host ticket path', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const DirectStub = defineComponent({
      name: 'DirectPreviewFrame',
      props: { directUrl: String, embedUrl: String },
      template: '<div data-testid="direct-stub" :data-direct="directUrl" :data-embed="embedUrl" />',
    })
    const w = mount(PublicAppPreviewPanel, {
      props: {
        token: 'share-token',
        ports: [{ port: 18081, label: 'shop', directUrl: 'http://10.0.0.8:18081/' }],
        active: true,
      },
      global: {
        plugins: [i18n],
        stubs: {
          NovncPreviewPanel: true,
          DirectPreviewFrame: DirectStub,
          ExternalUrlPreviewFrame: true,
        },
      },
    })
    await flushPromises()
    expect(shareMocks.previewTicket).toHaveBeenCalledWith(
      'share-token',
      18081,
      'api',
      expect.any(AbortSignal),
    )
    const stub = w.get('[data-testid="public-gate-app-preview-api"]')
    expect(stub.attributes('data-embed')).toBe(
      toPreviewDocumentURL('/public/gate-approvals/preview-api/tix/'),
    )
    expect(new URL(stub.attributes('data-embed') || '').origin).not.toBe(window.location.origin)
    expect(stub.attributes('data-direct')).toBe('http://10.0.0.8:18081/')
    w.unmount()
  })
})

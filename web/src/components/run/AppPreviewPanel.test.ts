// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AppPreviewPanel from './AppPreviewPanel.vue'

vi.mock('@novnc/novnc/lib/rfb.js', () => ({
  default: class MockRFB {},
}))

const apiMocks = vi.hoisted(() => ({
  nodePreviews: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      nodePreviews: apiMocks.nodePreviews,
    },
  }
})

const NovncStub = defineComponent({
  name: 'NovncPreviewPanel',
  props: { runId: String, nodeId: String, port: Number, fill: Boolean, compact: Boolean },
  emits: ['pick'],
  template: '<div data-testid="novnc-stub" :data-port="port" />',
})

const FeedbackStub = defineComponent({
  name: 'PreviewFeedbackChat',
  props: { runId: String, nodeId: String, port: Number, compact: Boolean },
  template: '<div data-testid="feedback-stub" />',
})

function mountPanel(opts: { compact?: boolean; fill?: boolean } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AppPreviewPanel, {
    props: {
      runId: 'run-1',
      nodeId: 'preview-1',
      compact: opts.compact ?? false,
      fill: opts.fill ?? false,
    },
    global: {
      plugins: [i18n],
      stubs: { NovncPreviewPanel: NovncStub, PreviewFeedbackChat: FeedbackStub },
    },
  })
}

describe('AppPreviewPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.nodePreviews.mockResolvedValue({
      ports: [
        { port: 5173, label: '前端' },
        { port: 8080, label: 'API' },
      ],
    })
  })

  it('loads ports and renders preview tabs', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(apiMocks.nodePreviews).toHaveBeenCalledWith(
      'run-1',
      'preview-1',
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(wrapper.text()).toContain('前端')
    expect(wrapper.find('[data-testid="novnc-stub"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('switches active port tab', async () => {
    apiMocks.nodePreviews.mockResolvedValue({
      ports: [
        { port: 5173, label: '前端' },
        { port: 5174, label: '管理端' },
      ],
    })
    const wrapper = mountPanel()
    await flushPromises()
    const tabs = wrapper.findAll('button').filter((b) => /前端|管理端/.test(b.text()))
    expect(tabs.length).toBe(2)
    await tabs[1].trigger('click')
    await flushPromises()
    expect(tabs[1].classes().some((c) => c.includes('accent'))).toBe(true)
    expect(tabs[0].classes().some((c) => c.includes('accent'))).toBe(false)
    wrapper.unmount()
  })

  it('shows no ports message when list empty', async () => {
    apiMocks.nodePreviews.mockResolvedValue({ ports: [] })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.text()).toMatch(/暂无|没有|未/)
    expect(wrapper.find('[data-testid="app-preview-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('passes AbortSignal and replaces ports on node switch', async () => {
    apiMocks.nodePreviews.mockResolvedValue({
      ports: [
        { port: 5173, label: '前端' },
        { port: 5174, label: '管理端' },
      ],
    })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.text()).toContain('前端')
    expect(apiMocks.nodePreviews.mock.calls[0][2]).toEqual(
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    apiMocks.nodePreviews.mockResolvedValue({
      ports: [
        { port: 3000, label: 'NEWPORT' },
        { port: 3001, label: 'OTHER' },
      ],
    })
    await wrapper.setProps({ nodeId: 'preview-2' })
    await flushPromises()
    expect(wrapper.text()).toContain('NEWPORT')
    expect(wrapper.text()).not.toContain('前端')
    wrapper.unmount()
  })

  it('sandboxes loadError and keeps retry without e.message', async () => {
    apiMocks.nodePreviews.mockRejectedValue(new Error('internal-preview-token-leak'))
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="app-preview-load-error"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('internal-preview-token-leak')
    expect(wrapper.text()).toMatch(/失败|retry|重试/i)
    wrapper.unmount()
  })

  it('direct mode iframes directUrl and skips noVNC', async () => {
    apiMocks.nodePreviews.mockResolvedValue({
      ports: [
        {
          port: 18081,
          label: '前端',
          mode: 'direct',
          directUrl: 'http://127.0.0.1:18081/',
        },
      ],
    })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="novnc-stub"]').exists()).toBe(false)
    const frame = wrapper.get('[data-testid="app-preview-direct-frame"]')
    expect(frame.attributes('src')).toBe('http://127.0.0.1:18081/')
    expect(wrapper.find('[data-testid="direct-preview-inspect"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="direct-preview-address"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('IP 直连预览不支持取点标注')
    wrapper.unmount()
  })

  it('external url preview iframes url directly without pick controls', async () => {
    apiMocks.nodePreviews.mockResolvedValue({
      ports: [
        {
          kind: 'url',
          port: 0,
          url: 'https://staging.example.com:8443/app',
          label: 'Staging',
          healthy: true,
        },
      ],
    })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="novnc-stub"]').exists()).toBe(false)
    const frame = wrapper.get('[data-testid="app-preview-external-url-frame"]')
    expect(frame.find('iframe').attributes('src')).toBe('https://staging.example.com:8443/app')
    expect(wrapper.text()).toContain('外部 URL 直连预览')
    expect(wrapper.find('[data-testid="direct-preview-inspect"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('schedules a silent poll while the port list is empty', async () => {
    vi.useFakeTimers()
    const spy = vi.spyOn(global, 'setInterval')
    apiMocks.nodePreviews.mockResolvedValue({ ports: [] })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="app-preview-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(spy).toHaveBeenCalled()
    const callsBeforePoll = apiMocks.nodePreviews.mock.calls.length
    expect(wrapper.attributes('aria-busy')).toBe('false')
    // Advance one empty-poll tick — must not flip visible loading.
    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect(apiMocks.nodePreviews.mock.calls.length).toBeGreaterThan(callsBeforePoll)
    expect(wrapper.attributes('aria-busy')).toBe('false')
    expect(wrapper.find('[data-testid="app-preview-empty"]').exists()).toBe(true)
    wrapper.unmount()
    spy.mockRestore()
    vi.useRealTimers()
  })
})

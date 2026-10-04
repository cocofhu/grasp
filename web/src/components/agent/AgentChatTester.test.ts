// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentChatTester from './AgentChatTester.vue'

const apiMocks = vi.hoisted(() => ({
  getSandbox: vi.fn(),
  sandboxChatWsUrl: vi.fn(() => 'ws://test/sandbox/1'),
  sandboxEventLog: vi.fn(),
  createAgentTest: vi.fn(),
  destroySandbox: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getSandbox: apiMocks.getSandbox,
      sandboxChatWsUrl: apiMocks.sandboxChatWsUrl,
      sandboxEventLog: apiMocks.sandboxEventLog,
      createAgentTest: apiMocks.createAgentTest,
      destroySandbox: apiMocks.destroySandbox,
    },
  }
})

let socket: MockWebSocket | null = null

class MockWebSocket {
  static OPEN = 1
  readyState = MockWebSocket.OPEN
  onopen: (() => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  send = vi.fn()
  close = vi.fn()
  constructor() {
    socket = this
    queueMicrotask(() => this.onopen?.())
  }
}

const PreviewAppModalStub = {
  props: ['open', 'title', 'width'],
  emits: ['close'],
  template: `
    <div v-if="open" data-testid="tester-image-preview-modal">
      <div data-testid="tester-image-preview-title">{{ title }}</div>
      <button type="button" data-testid="tester-image-preview-close" @click="$emit('close')">×</button>
      <button type="button" data-testid="tester-image-preview-backdrop" @click="$emit('close')">backdrop</button>
      <slot />
    </div>
  `,
}

function mountTester() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentChatTester, {
    props: { profile: 'cursor', attachId: 1, homeProjectId: 'proj-1' },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AppButton: { template: '<button><slot /></button>' },
        ReposEditor: true,
        AcpStatusPill: true,
        AppModal: PreviewAppModalStub,
      },
    },
  })
}

describe('AgentChatTester image preview (f4)', () => {
  const originalWS = globalThis.WebSocket

  beforeEach(() => {
    vi.clearAllMocks()
    socket = null
    // @ts-expect-error test stub
    globalThis.WebSocket = MockWebSocket
    apiMocks.getSandbox.mockResolvedValue({ id: 1, status: 'running', name: 'sbx-1' })
    apiMocks.sandboxEventLog.mockResolvedValue({
      events: [
        {
          type: 'prompt_begin',
          promptText: '用这张图验证',
          imageURLs: ['data:image/png;base64,RESTOREDIMG'],
        },
      ],
    })
  })

  afterEach(() => {
    globalThis.WebSocket = originalWS
  })

  it('restored history (empty mimeType) still shows thumb and opens preview', async () => {
    const wrapper = mountTester()
    await flushPromises()
    await flushPromises()

    const thumb = wrapper.find('[data-testid="tester-history-image-thumb"]')
    expect(thumb.exists()).toBe(true)
    expect(thumb.text()).toContain('点击放大')
    expect(thumb.text()).not.toContain('不可预览')
    await thumb.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="tester-image-preview-title"]').text()).toContain('图片预览')
    expect(wrapper.find('[data-testid="tester-image-preview-img"]').attributes('src')).toBe(
      'data:image/png;base64,RESTOREDIMG',
    )
    expect(wrapper.find('[data-testid="tester-image-preview-prev"]').exists()).toBe(false)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="tester-image-preview-modal"]').exists()).toBe(true)

    await wrapper.find('[data-testid="tester-image-preview-img"]').trigger('error')
    await flushPromises()
    expect(wrapper.find('[data-testid="tester-image-preview-failed"]').text()).toContain('图片加载失败')
    await wrapper.find('[data-testid="tester-image-preview-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="tester-image-preview-modal"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('draft thumb preview closes without dropping attachment', async () => {
    const wrapper = mountTester()
    await flushPromises()
    await flushPromises()

    class MockFR {
      result = 'data:image/png;base64,DRAFTTESTER'
      onload: ((this: FileReader, ev: ProgressEvent<FileReader>) => unknown) | null = null
      readAsDataURL() {
        queueMicrotask(() => this.onload?.call(this as unknown as FileReader, {} as ProgressEvent<FileReader>))
      }
    }
    vi.stubGlobal('FileReader', MockFR)

    const fileInput = wrapper.find('input[type="file"]')
    expect(fileInput.exists()).toBe(true)
    const file = new File(['x'], '调试草稿.png', { type: 'image/png' })
    Object.defineProperty(fileInput.element, 'files', { configurable: true, value: [file] })
    await fileInput.trigger('change')
    await flushPromises()

    const draft = wrapper.find('[data-testid="tester-draft-image-thumb"]')
    expect(draft.exists()).toBe(true)
    expect(draft.text()).not.toContain('不可预览')
    await draft.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="tester-image-preview-title"]').text()).toBe('图片预览 · 调试草稿.png')
    await wrapper.find('[data-testid="tester-image-preview-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="tester-draft-image-thumb"]').exists()).toBe(true)

    vi.unstubAllGlobals()
    wrapper.unmount()
  })

  it('live turn_begin images from queue also open preview', async () => {
    apiMocks.sandboxEventLog.mockResolvedValue({ events: [] })
    const wrapper = mountTester()
    await flushPromises()
    await flushPromises()
    expect(socket).toBeTruthy()

    class MockFR {
      result = 'data:image/png;base64,LIVEQ'
      onload: ((this: FileReader, ev: ProgressEvent<FileReader>) => unknown) | null = null
      readAsDataURL() {
        queueMicrotask(() => this.onload?.call(this as unknown as FileReader, {} as ProgressEvent<FileReader>))
      }
    }
    vi.stubGlobal('FileReader', MockFR)
    const fileInput = wrapper.find('input[type="file"]')
    const file = new File(['x'], '调试截图.png', { type: 'image/png' })
    Object.defineProperty(fileInput.element, 'files', { configurable: true, value: [file] })
    await fileInput.trigger('change')
    await flushPromises()

    const textarea = wrapper.find('textarea')
    await textarea.setValue('看这张')
    const sendBtn = wrapper.findAll('button').find((b) => b.text().includes('发送'))
    expect(sendBtn).toBeTruthy()
    await sendBtn!.trigger('click')
    await flushPromises()

    const queuedId = (wrapper.vm as any).queued[0].id
    socket!.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'session', event: 'turn_begin', item: { id: queuedId, text: '看这张' } }),
      }),
    )
    await flushPromises()

    const liveThumb = wrapper.find('[data-testid="tester-history-image-thumb"]')
    expect(liveThumb.exists()).toBe(true)
    await liveThumb.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="tester-image-preview-title"]').text()).toBe('图片预览 · 调试截图.png')
    expect(wrapper.find('[data-testid="tester-image-preview-img"]').attributes('src')).toBe(
      'data:image/png;base64,LIVEQ',
    )
    vi.unstubAllGlobals()
    wrapper.unmount()
  })
})

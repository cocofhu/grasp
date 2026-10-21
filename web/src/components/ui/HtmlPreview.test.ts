// @vitest-environment happy-dom
import { defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import {
  HTML_PREVIEW_DEFAULT_TOOLBAR_PX,
  RESIZE_HEIGHT_EPSILON,
  RESIZE_MESSAGE_TYPE,
  INSPECT_CANCELED_TYPE,
  INSPECT_MESSAGE_TYPE,
  contentFitPreviewCapPx,
} from '@/lib/shared/htmlPreviewSandbox'
import { CONVERSATION_PANEL_SHOWN_EVENT } from '@/lib/chat/conversationArtifacts'
import HtmlPreview from './HtmlPreview.vue'

const FIXED_ID = 'html-preview-test-instance'

const AppModalStub = defineComponent({
  name: 'AppModal',
  props: { open: Boolean, title: String, closeOnEsc: Boolean },
  emits: ['close'],
  // Parent may pass data-testid (fallthrough); expose title via data-title for assertions.
  template:
    '<div v-if="open" data-enlarge-modal="1" :data-title="title || \'\'"><slot /></div>',
})

function mountPreview(props: Record<string, unknown> = {}, slots: Record<string, string> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(HtmlPreview, {
    props: {
      html: '<!doctype html><html><body><p>hi</p></body></html>',
      enlargeable: false,
      mode: 'default',
      fitContent: true,
      maxContentHeightVh: 60,
      ...props,
    },
    slots,
    global: {
      plugins: [i18n],
      stubs: {
        AppModal: AppModalStub,
        Icon: { template: '<span />' },
      },
    },
  })
}

async function sendResize(wrapper: ReturnType<typeof mountPreview>, height: number) {
  const iframe = wrapper.find('iframe')
  expect(iframe.exists()).toBe(true)
  const el = iframe.element as HTMLIFrameElement
  Object.defineProperty(el, 'contentWindow', {
    value: window,
    configurable: true,
  })
  window.dispatchEvent(
    new MessageEvent('message', {
      data: { type: RESIZE_MESSAGE_TYPE, id: FIXED_ID, height },
      source: window,
    }),
  )
  await nextTick()
  await flushPromises()
  // rAF batching
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
  await nextTick()
}

describe('HtmlPreview content-fit clamp', () => {
  beforeEach(() => {
    vi.stubGlobal('crypto', { randomUUID: () => FIXED_ID })
    Object.defineProperty(window, 'innerHeight', {
      configurable: true,
      writable: true,
      value: 1000,
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('clamps measured height above cap and enables scrolling=auto (deducts toolbar)', async () => {
    const wrapper = mountPreview()
    await flushPromises()

    const expectedCap =
      contentFitPreviewCapPx(1000, 60) - HTML_PREVIEW_DEFAULT_TOOLBAR_PX
    await sendResize(wrapper, 2000)

    const iframe = wrapper.find('iframe')
    expect((iframe.element as HTMLIFrameElement).style.height).toBe(`${expectedCap}px`)
    expect(iframe.attributes('scrolling')).toBe('auto')
    wrapper.unmount()
  })

  it('keeps measured height under cap and scrolling=no', async () => {
    const wrapper = mountPreview()
    await flushPromises()

    await sendResize(wrapper, 240)

    const iframe = wrapper.find('iframe')
    expect((iframe.element as HTMLIFrameElement).style.height).toBe('240px')
    expect(iframe.attributes('scrolling')).toBe('no')
    wrapper.unmount()
  })

  it('subtracts contentHeightOffsetPx from the clamp (reviewing strip)', async () => {
    const wrapper = mountPreview({ contentHeightOffsetPx: 28 })
    await flushPromises()

    const expectedCap =
      contentFitPreviewCapPx(1000, 60) - HTML_PREVIEW_DEFAULT_TOOLBAR_PX - 28
    await sendResize(wrapper, 2000)

    const iframe = wrapper.find('iframe')
    expect((iframe.element as HTMLIFrameElement).style.height).toBe(`${expectedCap}px`)
    expect(iframe.attributes('scrolling')).toBe('auto')
    wrapper.unmount()
  })

  it('inline mode clamps to full vh cap without toolbar deduction', async () => {
    const wrapper = mountPreview({ mode: 'inline', fitContent: false })
    await flushPromises()

    const expectedCap = contentFitPreviewCapPx(1000, 60)
    await sendResize(wrapper, 2000)

    const iframe = wrapper.find('iframe')
    expect((iframe.element as HTMLIFrameElement).style.height).toBe(`${expectedCap}px`)
    expect(iframe.attributes('scrolling')).toBe('auto')
    wrapper.unmount()
  })

  it('keeps iframe mounted when html content changes (silent srcdoc update)', async () => {
    const wrapper = mountPreview({
      html: '<!doctype html><html><body><p>a</p></body></html>',
    })
    await flushPromises()
    const first = wrapper.find('iframe').element
    await wrapper.setProps({
      html: '<!doctype html><html><body><p>b</p></body></html>',
    })
    await flushPromises()
    const second = wrapper.find('iframe').element
    expect(second).toBe(first)
    expect(wrapper.find('iframe').attributes('sandbox')).toBe('allow-scripts allow-forms')
    wrapper.unmount()
  })

  it('merges consecutive resize messages in one animation frame', async () => {
    const wrapper = mountPreview({ mode: 'inline', fitContent: false })
    await flushPromises()
    const iframe = wrapper.find('iframe').element as HTMLIFrameElement
    Object.defineProperty(iframe, 'contentWindow', {
      value: window,
      configurable: true,
    })
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: RESIZE_MESSAGE_TYPE, id: FIXED_ID, height: 168 },
        source: window,
      }),
    )
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: RESIZE_MESSAGE_TYPE, id: FIXED_ID, height: 248 },
        source: window,
      }),
    )
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
    await nextTick()
    expect(iframe.style.height).toBe('248px')
    wrapper.unmount()
  })

  it('ignores sub-epsilon height changes in steady state', async () => {
    const wrapper = mountPreview({ mode: 'inline', fitContent: false })
    await flushPromises()
    await sendResize(wrapper, 248)
    await new Promise((r) => setTimeout(r, 200))
    await sendResize(wrapper, 248 + RESIZE_HEIGHT_EPSILON - 1)
    const iframe = wrapper.find('iframe').element as HTMLIFrameElement
    expect(iframe.style.height).toBe('248px')
    wrapper.unmount()
  })

  it('does not reset height when html prop changes but content hash is equal', async () => {
    const html = '<!doctype html><html><body><p>same</p></body></html>'
    const wrapper = mountPreview({ mode: 'inline', fitContent: false, html })
    await flushPromises()
    await sendResize(wrapper, 240)
    const iframe = wrapper.find('iframe').element as HTMLIFrameElement
    expect(iframe.style.height).toBe('240px')
    await wrapper.setProps({ html: ['<!doctype html><html><body><p>same</p></body></html>'].join('') })
    await flushPromises()
    expect(iframe.style.height).toBe('240px')
    wrapper.unmount()
  })

  it('fillParent fills shell without scrollHeight sizing and keeps inspect bar', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      maxContentHeightVh: undefined,
      inspectable: true,
    })
    await flushPromises()

    const root = wrapper.find('[data-fill-parent="1"]')
    expect(root.exists()).toBe(true)
    expect(root.classes()).toContain('h-full')
    expect(wrapper.find('[data-testid="html-preview-inspect-bar"]').exists()).toBe(true)
    const iframe = wrapper.find('iframe')
    expect(iframe.attributes('scrolling')).toBe('auto')
    expect((iframe.element as HTMLIFrameElement).style.height).toBe('')
    expect(iframe.classes()).toContain('h-full')

    // Resize messages must not drive shell height in fillParent mode.
    await sendResize(wrapper, 2000)
    expect((iframe.element as HTMLIFrameElement).style.height).toBe('')
    wrapper.unmount()
  })

  it('default-mode fillParent keeps toolbar and does not size from scrollHeight', async () => {
    const wrapper = mountPreview({
      mode: 'default',
      fitContent: false,
      fillParent: true,
      maxContentHeightVh: undefined,
      enlargeable: true,
      inspectable: true,
    })
    await flushPromises()

    const root = wrapper.find('[data-fill-parent="1"]')
    expect(root.exists()).toBe(true)
    expect(root.classes()).toContain('h-full')
    expect(wrapper.find('[data-testid="html-preview-toolbar"]').exists()).toBe(true)
    const iframe = wrapper.find('iframe')
    expect(iframe.attributes('scrolling')).toBe('auto')
    expect((iframe.element as HTMLIFrameElement).style.height).toBe('')
    expect(iframe.classes()).toContain('h-full')

    await sendResize(wrapper, 1800)
    expect((iframe.element as HTMLIFrameElement).style.height).toBe('')
    wrapper.unmount()
  })
})

describe('HtmlPreview inline/fillParent enlarge', () => {
  beforeEach(() => {
    vi.stubGlobal('crypto', { randomUUID: () => FIXED_ID })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('groups toolbar actions before enlarge without duplicating them in the modal or remounting the preview', async () => {
    const wrapper = mountPreview({ enlargeable: true }, {
      'toolbar-actions': '<button data-testid="preview-copy">复制</button><a data-testid="preview-download" href="/page.html" download>下载</a>',
    })
    await flushPromises()
    const toolbar = wrapper.get('[data-testid="html-preview-toolbar"]')
    const copy = toolbar.get('[data-testid="preview-copy"]')
    const download = toolbar.get('[data-testid="preview-download"]')
    const enlarge = toolbar.get('[data-testid="html-preview-enlarge"]')
    const originalIframe = wrapper.get('iframe').element
    expect(copy.element.parentElement).toBe(enlarge.element.parentElement)
    expect(download.element.parentElement).toBe(enlarge.element.parentElement)
    expect(enlarge.element.parentElement).not.toBe(toolbar.element)
    expect(enlarge.element.parentElement?.parentElement).toBe(toolbar.element)
    expect(copy.element.compareDocumentPosition(enlarge.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(download.element.compareDocumentPosition(enlarge.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    await enlarge.trigger('click')
    await nextTick()
    const modal = wrapper.findComponent(AppModalStub)
    expect(modal.props('open')).toBe(true)
    expect(modal.find('iframe').exists()).toBe(true)
    expect(modal.find('[data-testid="preview-copy"]').exists()).toBe(false)
    expect(modal.find('[data-testid="preview-download"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="preview-copy"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="preview-download"]')).toHaveLength(1)
    expect(wrapper.get('iframe').element).toBe(originalIframe)

    modal.vm.$emit('close')
    await nextTick()
    await wrapper.setProps({ enlargeable: false })
    expect(wrapper.find('[data-testid="html-preview-enlarge"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="preview-copy"]').element).toBe(copy.element)
    expect(wrapper.get('[data-testid="preview-download"]').attributes('download')).toBeDefined()
    expect(wrapper.get('iframe').element).toBe(originalIframe)
    wrapper.unmount()

    const withoutActions = mountPreview({ enlargeable: true })
    expect(withoutActions.get('[data-testid="html-preview-toolbar"]').text()).toContain(common.common.htmlPreview.desktop)
    expect(withoutActions.get('[data-testid="html-preview-toolbar"]').text()).toContain(common.common.htmlPreview.mobile)
    expect(withoutActions.find('[data-testid="html-preview-enlarge"]').exists()).toBe(true)
    expect(withoutActions.find('[data-testid="preview-copy"]').exists()).toBe(false)
    withoutActions.unmount()
  })

  it('opens enlarge modal for mode=inline + fillParent + enlargeable with filename title', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      maxContentHeightVh: undefined,
      enlargeable: true,
      inspectable: true,
      modalTitle: 'page.html',
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="html-preview-inline"]').exists()).toBe(true)
    expect(wrapper.find('[data-fill-parent="1"]').exists()).toBe(true)
    const enlargeBtn = wrapper.find('[data-testid="html-preview-enlarge"]')
    expect(enlargeBtn.exists()).toBe(true)
    expect(enlargeBtn.text()).toContain('窗口放大查看')

    await enlargeBtn.trigger('click')
    await nextTick()

    // AppModal is a second root (fragment); assert via findComponent.
    const modal = wrapper.findComponent(AppModalStub)
    expect(modal.exists()).toBe(true)
    expect(modal.props('open')).toBe(true)
    expect(modal.props('title')).toBe('窗口放大查看 · page.html')
    expect(modal.props('closeOnEsc')).toBe(true)
    expect(modal.find('[data-testid="html-preview-enlarge-body"]').exists()).toBe(true)
    // Nested enlarge preview is read-only: no inspect toggle inside modal body.
    expect(
      modal.find('[data-testid="html-preview-inspect-toggle"]').exists(),
    ).toBe(false)

    // Closing enlarge must keep the outer inline shell mounted (no layout mode flip).
    ;(wrapper.vm as { closeEnlarge: () => void }).closeEnlarge()
    await nextTick()
    expect(wrapper.findComponent(AppModalStub).props('open')).toBe(false)
    expect(wrapper.find('[data-testid="html-preview-inline"]').exists()).toBe(true)
    expect(wrapper.find('[data-fill-parent="1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview-inspect-bar"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not show enlarge entry when enlargeable is false (mobile strategy)', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      enlargeable: false,
      inspectable: true,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview-enlarge"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview-inspect-bar"]').exists()).toBe(true)
    expect(wrapper.findComponent(AppModalStub).exists()).toBe(false)
    wrapper.unmount()
  })

  it('falls back to enlargeTitle when modalTitle is empty', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      enlargeable: true,
      modalTitle: '',
    })
    await flushPromises()
    await wrapper.find('[data-testid="html-preview-enlarge"]').trigger('click')
    await nextTick()
    expect(wrapper.findComponent(AppModalStub).props('title')).toBe('窗口放大查看')
    wrapper.unmount()
  })
})

describe('HtmlPreview inspect toggle', () => {
  beforeEach(() => {
    vi.stubGlobal('crypto', { randomUUID: () => FIXED_ID })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('toggles label to 取消标注 and aria-pressed round-trip', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      inspectable: true,
      enlargeable: false,
    })
    await flushPromises()
    const btn = wrapper.find('[data-testid="html-preview-inspect-toggle"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('取点标注')
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(wrapper.text()).not.toContain('取消标注')

    await btn.trigger('click')
    await nextTick()
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(btn.text()).toContain('取消标注')
    expect(btn.text()).not.toContain('取点标注')

    await btn.trigger('click')
    await nextTick()
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(btn.text()).toContain('取点标注')
    expect(wrapper.text()).not.toContain('取消标注')
    wrapper.unmount()
  })

  it('keeps inspect pressed after iframe pick (comment mode stays on)', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      inspectable: true,
      enlargeable: false,
    })
    await flushPromises()

    const iframe = wrapper.find('iframe')
    const el = iframe.element as HTMLIFrameElement
    Object.defineProperty(el, 'contentWindow', {
      value: window,
      configurable: true,
    })

    const btn = wrapper.find('[data-testid="html-preview-inspect-toggle"]')
    await btn.trigger('click')
    await nextTick()
    expect(btn.attributes('aria-pressed')).toBe('true')

    window.dispatchEvent(
      new MessageEvent('message', {
        data: {
          type: INSPECT_MESSAGE_TYPE,
          id: FIXED_ID,
          selector: 'h1.display',
          tagName: 'h1',
          imageDataUrl: '',
          bounds: { left: 10, top: 20, width: 100, height: 40 },
          currentText: 'title',
          style: { color: 'rgb(26, 26, 26)', fontSize: '118px', fontWeight: '700' },
        },
        source: window,
      }),
    )
    await nextTick()
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(wrapper.emitted('pick')?.[0]?.[0]).toMatchObject({
      selector: 'h1.display',
      tagName: 'h1',
      style: { color: 'rgb(26, 26, 26)', fontSize: '118px', fontWeight: '700' },
    })
    wrapper.unmount()
  })

  it('clears pressed state when iframe posts inspect-canceled (Esc)', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      inspectable: true,
      enlargeable: false,
    })
    await flushPromises()

    const iframe = wrapper.find('iframe')
    const el = iframe.element as HTMLIFrameElement
    Object.defineProperty(el, 'contentWindow', {
      value: window,
      configurable: true,
    })

    const btn = wrapper.find('[data-testid="html-preview-inspect-toggle"]')
    await btn.trigger('click')
    await nextTick()
    expect(btn.attributes('aria-pressed')).toBe('true')

    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: INSPECT_CANCELED_TYPE, id: FIXED_ID },
        source: window,
      }),
    )
    await nextTick()
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('false')
    wrapper.unmount()
  })
})

describe('HtmlPreview comment pin overlay', () => {
  beforeEach(() => {
    vi.stubGlobal('crypto', { randomUUID: () => FIXED_ID })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const draft = {
    selector: 'h1.display.reveal',
    initialComment: '',
    bounds: { left: 8, top: 8, width: 200, height: 40 },
    screenshotMissing: true,
    style: { color: 'rgb(26, 26, 26)', fontSize: '118px', fontWeight: '700', fontFamily: 'Bodoni Moda' },
  }

  it('shows inspect card in mode=inline + fillParent when annotateDraft is set', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fitContent: false,
      fillParent: true,
      inspectable: true,
      enlargeable: false,
      annotateDraft: draft,
      commentPins: [{ id: 'pin-1', seq: 1, bounds: draft.bounds, active: true }],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview-pin-host"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview-pin-layer"]').exists()).toBe(true)
    const card = wrapper.find('[data-testid="comment-pin-inspect-card"]')
    expect(card.exists()).toBe(true)
    expect(card.isVisible()).toBe(true)
    expect(card.text()).toContain('h1.display.reveal')
    expect(card.text()).toContain('Size')
    expect(card.text()).toContain('Color')
    expect(card.text()).toContain('Font')
    wrapper.unmount()
  })

  it('emits annotate-save from inline card after typing', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fillParent: true,
      inspectable: true,
      enlargeable: false,
      annotateDraft: draft,
    })
    await flushPromises()
    const save = wrapper.find('[data-testid="comment-pin-save"]')
    const send = wrapper.find('[data-testid="comment-pin-send-chat"]')
    expect(save.attributes('disabled')).toBeDefined()
    expect(send.attributes('disabled')).toBeDefined()

    await wrapper.find('[data-testid="comment-pin-input"]').setValue('标题加大')
    await nextTick()
    expect(save.attributes('disabled')).toBeUndefined()
    expect(send.attributes('disabled')).toBeUndefined()

    await save.trigger('click')
    expect(wrapper.emitted('annotate-save')?.[0]).toEqual(['标题加大'])
    wrapper.unmount()
  })

  it('emits annotate-send-chat from inline card', async () => {
    const wrapper = mountPreview({
      mode: 'inline',
      fillParent: true,
      inspectable: true,
      enlargeable: false,
      annotateDraft: draft,
    })
    await flushPromises()
    await wrapper.find('[data-testid="comment-pin-input"]').setValue('发给评审')
    await nextTick()
    await wrapper.find('[data-testid="comment-pin-send-chat"]').trigger('click')
    expect(wrapper.emitted('annotate-send-chat')?.[0]).toEqual(['发给评审'])
    wrapper.unmount()
  })
})

describe('HtmlPreview toolbar actions slot', () => {
  it('does not render copy or download unless a caller supplies them', async () => {
    const wrapper = mountPreview({
      mode: 'default',
      fitContent: false,
      fillParent: true,
      enlargeable: true,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-copy"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-download-raw"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview-canvas-overlay"]').exists()).toBe(false)
    const end = wrapper.get('[data-testid="html-preview-toolbar-end"]')
    expect(end.get('[data-testid="html-preview-enlarge"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps canvas overlays outside the toolbar and places caller actions before enlarge', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(HtmlPreview, {
      props: {
        html: '<!doctype html><html><body><p>hi</p></body></html>',
        mode: 'default',
        fitContent: false,
        fillParent: true,
        enlargeable: true,
      },
      slots: {
        'toolbar-actions':
          '<button type="button" data-testid="artifact-preview-copy">copy</button><button type="button" data-testid="artifact-preview-download-raw">dl</button>',
        'canvas-overlay': '<div data-testid="preview-loading">Loading preview</div>',
      },
      global: {
        plugins: [i18n],
        stubs: {
          AppModal: AppModalStub,
          Icon: { template: '<span />' },
        },
      },
    })
    await flushPromises()
    const toolbar = wrapper.get('[data-testid="html-preview-toolbar"]')
    const end = toolbar.get('[data-testid="html-preview-toolbar-end"]')
    const order = [...end.element.children].map((el) => el.getAttribute('data-testid'))
    expect(order).toEqual([
      'artifact-preview-copy',
      'artifact-preview-download-raw',
      'html-preview-enlarge',
    ])
    const host = wrapper.get('[data-testid="html-preview-pin-host"]')
    const overlay = wrapper.get('[data-testid="html-preview-canvas-overlay"]')
    expect(overlay.get('[data-testid="preview-loading"]').text()).toBe('Loading preview')
    expect(overlay.element.parentElement?.contains(host.element)).toBe(true)
    expect(overlay.element.parentElement?.contains(toolbar.element)).toBe(false)
    expect(host.element.contains(end.element)).toBe(false)
    expect(
      host.element.compareDocumentPosition(end.element) & Node.DOCUMENT_POSITION_PRECEDING,
    ).toBeTruthy()
    wrapper.unmount()
  })
})

const PAGE_A = '<!doctype html><html><body><p id="marker-a">甲</p></body></html>'
const PAGE_B = '<!doctype html><html><body><p id="marker-b">乙</p></body></html>'

function drawerProps(extra: Record<string, unknown> = {}) {
  return {
    mode: 'default' as const,
    fitContent: false,
    fillParent: true,
    maxContentHeightVh: undefined,
    enlargeable: true,
    ...extra,
  }
}

function setHostBox(wrapper: ReturnType<typeof mountPreview>, mode: 'sized' | 'hidden') {
  const el = wrapper.get('[data-testid="html-preview-pin-host"]').element as HTMLElement
  if (mode === 'hidden') {
    el.style.display = 'none'
    return el
  }
  el.style.display = 'block'
  Object.defineProperty(el, 'clientWidth', { configurable: true, get: () => 800 })
  Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => 600 })
  return el
}

async function flushPaint() {
  for (let i = 0; i < 4; i++) {
    await nextTick()
    await flushPromises()
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
  }
}

describe('HtmlPreview reloads a sized frame', () => {
  beforeEach(() => {
    vi.stubGlobal('crypto', { randomUUID: () => FIXED_ID })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reloads the current html when a hidden frame becomes sized', async () => {
    const wrapper = mountPreview(drawerProps({ html: PAGE_A, presented: false }))
    await flushPromises()
    const first = wrapper.get('iframe').element
    expect(wrapper.get('iframe').attributes('data-preview-epoch')).toBe('0')
    setHostBox(wrapper, 'sized')
    await wrapper.setProps({ presented: true })
    await flushPaint()
    const iframe = wrapper.get('iframe')
    expect(iframe.element).toBe(first)
    expect(iframe.attributes('srcdoc')).toContain('marker-a')
    expect(Number(iframe.attributes('data-preview-epoch'))).toBeGreaterThan(0)
    expect(iframe.attributes('sandbox')).toBe('allow-scripts allow-forms')
    expect(iframe.attributes('sandbox')).not.toContain('allow-same-origin')
    wrapper.unmount()
  })

  it('loads html that arrives while the frame is hidden', async () => {
    const wrapper = mountPreview(drawerProps({ html: '', presented: false }))
    await flushPromises()
    await wrapper.setProps({ html: PAGE_B })
    await flushPromises()
    setHostBox(wrapper, 'sized')
    await wrapper.setProps({ presented: true })
    await flushPaint()
    const iframe = wrapper.get('iframe')
    expect(iframe.attributes('srcdoc')).toContain('marker-b')
    expect(iframe.attributes('srcdoc')).not.toContain('marker-a')
    expect(Number(iframe.attributes('data-preview-epoch'))).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('replaces the document when html is overwritten and keeps it when html is unchanged', async () => {
    const wrapper = mountPreview(drawerProps({ html: PAGE_A, presented: true }))
    await flushPromises()
    setHostBox(wrapper, 'sized')
    window.dispatchEvent(new CustomEvent(CONVERSATION_PANEL_SHOWN_EVENT))
    await flushPaint()
    const first = wrapper.get('iframe').element
    const adopted = Number(wrapper.get('iframe').attributes('data-preview-epoch'))
    await wrapper.setProps({ html: PAGE_B })
    await flushPaint()
    const iframe = wrapper.get('iframe')
    expect(iframe.element).toBe(first)
    expect(iframe.attributes('srcdoc')).toContain('marker-b')
    expect(Number(iframe.attributes('data-preview-epoch'))).toBeGreaterThan(adopted)
    const epoch = Number(iframe.attributes('data-preview-epoch'))
    await wrapper.setProps({ html: PAGE_B })
    await flushPaint()
    expect(wrapper.get('iframe').element).toBe(first)
    expect(Number(wrapper.get('iframe').attributes('data-preview-epoch'))).toBe(epoch)
    expect(wrapper.get('iframe').attributes('sandbox')).toBe('allow-scripts allow-forms')
    wrapper.unmount()
  })

  it('repaints after an ancestor is shown again and matches the enlarge modal', async () => {
    const wrapper = mountPreview(drawerProps({ html: PAGE_A, presented: true }))
    await flushPromises()
    const host = wrapper.get('[data-testid="html-preview-pin-host"]').element as HTMLElement
    host.parentElement!.style.display = 'none'
    window.dispatchEvent(new CustomEvent(CONVERSATION_PANEL_SHOWN_EVENT))
    await flushPaint()
    expect(wrapper.get('iframe').attributes('data-preview-epoch')).toBe('0')
    host.parentElement!.style.display = 'block'
    setHostBox(wrapper, 'sized')
    window.dispatchEvent(new CustomEvent(CONVERSATION_PANEL_SHOWN_EVENT))
    await flushPaint()
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('marker-a')
    expect(Number(wrapper.get('iframe').attributes('data-preview-epoch'))).toBeGreaterThan(0)

    await wrapper.get('[data-testid="html-preview-enlarge"]').trigger('click')
    await flushPaint()
    const docs = wrapper.findAll('iframe').map((frame) => frame.attributes('srcdoc') || '')
    expect(docs.filter((doc) => doc.includes('marker-a')).length).toBeGreaterThan(1)
    for (const frame of wrapper.findAll('iframe')) {
      expect(frame.attributes('sandbox')).toBe('allow-scripts allow-forms')
    }

    await wrapper.setProps({ html: PAGE_B })
    await flushPaint()
    const next = wrapper.findAll('iframe').map((frame) => frame.attributes('srcdoc') || '')
    expect(next.every((doc) => doc.includes('marker-b'))).toBe(true)
    wrapper.unmount()
  })

  it('keeps the mobile frame on the same sandbox and current html', async () => {
    const wrapper = mountPreview(drawerProps({ html: PAGE_A, presented: true }))
    await flushPromises()
    setHostBox(wrapper, 'sized')
    window.dispatchEvent(new CustomEvent(CONVERSATION_PANEL_SHOWN_EVENT))
    await flushPaint()
    const mobile = wrapper.findAll('button').find((button) => button.text().includes('移动端'))
    expect(mobile).toBeTruthy()
    await mobile!.trigger('click')
    await flushPaint()
    const iframe = wrapper.get('iframe')
    expect(iframe.attributes('srcdoc')).toContain('marker-a')
    expect(iframe.attributes('sandbox')).toBe('allow-scripts allow-forms')
    expect(iframe.attributes('title')).toContain('移动端')
    wrapper.unmount()
  })

  it('does not reload demo thumbnails when visibility toggles', async () => {
    const wrapper = mountPreview({
      mode: 'demo',
      html: PAGE_A,
      enlargeable: false,
      fitContent: false,
      maxContentHeightVh: undefined,
    })
    await flushPromises()
    const first = wrapper.get('iframe').element
    await wrapper.setProps({ presented: false })
    await wrapper.setProps({ presented: true })
    window.dispatchEvent(new CustomEvent(CONVERSATION_PANEL_SHOWN_EVENT))
    await flushPaint()
    const iframe = wrapper.get('iframe')
    expect(iframe.element).toBe(first)
    expect(iframe.attributes('data-preview-epoch')).toBeUndefined()
    expect(iframe.attributes('srcdoc')).toContain('marker-a')
    expect(iframe.attributes('sandbox')).toBe('allow-scripts allow-forms')
    wrapper.unmount()
  })
})


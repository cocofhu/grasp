// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import NovncPreviewPanel from './NovncPreviewPanel.vue'

class MockWebSocket {
  static OPEN = 1
  readyState = MockWebSocket.OPEN
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  sent: string[] = []
  constructor(_url: string) {
    MockWebSocket.instances.push(this)
    queueMicrotask(() => {
      this.onopen?.()
      if (MockWebSocket.holdServer) return
      const frame = MockWebSocket.failWith
        ? { type: 'error', message: MockWebSocket.failWith }
        : { type: 'ready', url: MockWebSocket.readyUrl }
      this.onmessage?.({ data: JSON.stringify(frame) })
    })
  }
  static instances: MockWebSocket[] = []
  static readyUrl = 'http://localhost:5173'
  /** When set, new sockets answer with this server error instead of ready. */
  static failWith = ''
  /** When set, the socket opens and stays quiet so the pane remains connecting. */
  static holdServer = false
  static reset() {
    MockWebSocket.instances = []
    MockWebSocket.readyUrl = 'http://localhost:5173'
    MockWebSocket.failWith = ''
    MockWebSocket.holdServer = false
  }
  send(data: string) {
    this.sent.push(data)
  }
  close() {}
}

const rfbMocks = vi.hoisted(() => {
  const instances: Array<{
    scaleViewport: boolean
    scaleViewportSets: boolean[]
    resizeSession: boolean
    disconnect: () => void
  }> = []
  return {
    instances,
    /** When set, the RFB connect event is not fired, so the pane stays connecting. */
    holdConnect: false,
    reset() {
      instances.length = 0
      this.holdConnect = false
    },
    last() {
      return instances[instances.length - 1] ?? null
    },
  }
})

vi.mock('@novnc/novnc/lib/rfb.js', () => ({
  default: class MockRFB {
    _scaleViewport = false
    scaleViewportSets: boolean[] = []
    resizeSession = false
    viewOnly = false
    focusOnClick = false
    focusCount = 0
    showDotCursor = false
    background = ''
    constructor(_host: HTMLElement, _channel: unknown) {
      rfbMocks.instances.push(this)
    }
    focus() {
      this.focusCount += 1
    }
    get scaleViewport() {
      return this._scaleViewport
    }
    set scaleViewport(v: boolean) {
      this._scaleViewport = v
      this.scaleViewportSets.push(v)
    }
    addEventListener(event: string, cb: () => void) {
      if (event === 'connect' && !rfbMocks.holdConnect) queueMicrotask(cb)
    }
    disconnect() {}
  },
}))

/** Capture ResizeObserver callbacks so tests can simulate hide→show. */
const resizeObserverMocks = vi.hoisted(() => {
  const callbacks: ResizeObserverCallback[] = []
  class MockResizeObserver {
    constructor(cb: ResizeObserverCallback) {
      callbacks.push(cb)
    }
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  return {
    callbacks,
    MockResizeObserver,
    reset() {
      callbacks.length = 0
    },
    trigger() {
      for (const cb of callbacks) cb([], {} as ResizeObserver)
    },
  }
})

const apiMocks = vi.hoisted(() => ({
  sandboxVncWsUrl: vi.fn(() => 'ws://localhost/sandbox-vnc/1/ws'),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      sandboxVncWsUrl: apiMocks.sandboxVncWsUrl,
    },
  }
})

function mountNovnc(props: Record<string, unknown> = {}, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages:
      locale === 'en'
        ? { en: { ...enCommon, ...enPages } }
        : { 'zh-CN': { ...common, ...pages } },
  })
  return mount(NovncPreviewPanel, {
    props: {
      sandboxId: 1,
      fill: false,
      compact: false,
      ...props,
    },
    global: { plugins: [i18n], stubs: { Icon: true } },
    attachTo: document.body,
  })
}

function inspectButton(wrapper: ReturnType<typeof mountNovnc>) {
  return wrapper.find('[data-testid="novnc-inspect-toggle"]')
}

function controlButton(wrapper: ReturnType<typeof mountNovnc>) {
  return wrapper.get('[data-testid="novnc-control-toggle"]')
}

const SCREEN_GESTURE = ['mousedown', 'mousemove', 'mouseup', 'click'] as const

/** Child standing in for the noVNC canvas. Capture on the host must run first. */
function screenStandIn(wrapper: ReturnType<typeof mountNovnc>) {
  const host = wrapper.get('.novnc-canvas-host').element
  const child = document.createElement('div')
  child.dataset.testid = 'novnc-screen'
  host.appendChild(child)
  return child
}

function trackEvents(el: Element, types: readonly string[]) {
  const seen: string[] = []
  for (const type of types) el.addEventListener(type, () => seen.push(type))
  return seen
}

function dispatchMouse(el: Element, type: string, button = 0) {
  el.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, button, buttons: button === 0 ? 1 : 0 }))
}

/** One press-move-release, including the click or context menu that follows. */
function clickScreen(el: Element, button = 0) {
  dispatchMouse(el, 'mousedown', button)
  dispatchMouse(el, 'mousemove', button)
  dispatchMouse(el, 'mouseup', button)
  if (button === 0) {
    dispatchMouse(el, 'click', button)
    return
  }
  dispatchMouse(el, 'auxclick', button)
  el.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, button }))
}

function dispatchTouch(el: Element, type: string) {
  let ev: Event
  try {
    ev = new TouchEvent(type, { bubbles: true, cancelable: true })
  } catch {
    ev = new Event(type, { bubbles: true, cancelable: true })
  }
  el.dispatchEvent(ev)
}

function lastInspectCtrl(): { type: string; on: boolean } | null {
  const ws = MockWebSocket.instances[0]
  if (!ws) return null
  for (let i = ws.sent.length - 1; i >= 0; i--) {
    try {
      const msg = JSON.parse(ws.sent[i]) as { type?: string; on?: boolean }
      if (msg.type === 'inspect') return msg as { type: string; on: boolean }
    } catch {
      /* ignore */
    }
  }
  return null
}

describe('NovncPreviewPanel', () => {
  beforeEach(() => {
    MockWebSocket.reset()
    rfbMocks.reset()
    resizeObserverMocks.reset()
    apiMocks.sandboxVncWsUrl.mockClear()
    vi.stubGlobal('WebSocket', MockWebSocket)
    vi.stubGlobal('ResizeObserver', resizeObserverMocks.MockResizeObserver)
  })

  afterEach(() => {
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
  })

  function setPreviewBoxSize(wrapper: ReturnType<typeof mountNovnc>, w: number, h: number) {
    // Scale tracks the preview viewport (definite box). Size the host too so
    // noVNC's screen and the pane stay in lockstep in this jsdom-less setup.
    for (const sel of ['.novnc-fs-viewport', '.novnc-canvas-host']) {
      const el = wrapper.find(sel).element as HTMLElement
      Object.defineProperty(el, 'clientWidth', { configurable: true, get: () => w })
      Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => h })
    }
  }

  it('sets aria-busy while connecting', async () => {
    const wrapper = mountNovnc()
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(true)
    await flushPromises()
    wrapper.unmount()
  })

  it('warns the preview may be stuck only after the advertised first-boot window', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
    rfbMocks.holdConnect = true
    MockWebSocket.holdServer = true
    const wrapper = mountNovnc()
    try {
      await flushPromises()
      expect(wrapper.find('[data-testid="novnc-connecting-hint"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="novnc-preview-stuck"]').exists()).toBe(false)

      await vi.advanceTimersByTimeAsync(20_000)
      await flushPromises()
      expect(wrapper.find('[data-testid="novnc-preview-stuck"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="novnc-connecting-hint"]').text()).toContain('30')

      await vi.advanceTimersByTimeAsync(40_000)
      await flushPromises()
      const stuck = wrapper.find('[data-testid="novnc-preview-stuck"]')
      expect(stuck.exists()).toBe(true)
      expect(stuck.text()).toContain('可能卡死')
      expect(stuck.text()).toContain('重新连接')
      expect(wrapper.find('[data-testid="novnc-connecting-hint"]').exists()).toBe(false)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('connects preview mode and shows live toolbar without share-approval entry', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    expect(apiMocks.sandboxVncWsUrl).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toMatch(/已连接|连接中/)
    expect(inspectButton(wrapper).exists()).toBe(true)
    expect(inspectButton(wrapper).text()).toContain('取点标注')
    expect(wrapper.find('[data-testid="novnc-share-approval"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('attaching shows the Agent screen as is; only a port change navigates it', async () => {
    const gotos = () =>
      MockWebSocket.instances.flatMap((ws) =>
        ws.sent
          .map((s) => JSON.parse(s) as { type?: string; action?: string; url?: string })
          .filter((m) => m.type === 'navigate' && m.action === 'goto')
          .map((m) => m.url),
      )
    const wrapper = mountNovnc({ targetPort: 8080 })
    await flushPromises()
    expect(gotos()).toEqual([])
    await wrapper.setProps({ targetPort: 3000 })
    await flushPromises()
    expect(gotos()).toEqual(['http://127.0.0.1:3000/'])
    expect(MockWebSocket.instances).toHaveLength(1)
    wrapper.unmount()
  })

  it('switching to the port the desktop already shows does not navigate', async () => {
    MockWebSocket.readyUrl = 'http://127.0.0.1:8080/orders/7'
    const wrapper = mountNovnc({ targetPort: 5173 })
    await flushPromises()
    await wrapper.setProps({ targetPort: 8080 })
    await flushPromises()
    const gotos = MockWebSocket.instances[0].sent
      .map((s) => JSON.parse(s) as { type?: string; action?: string })
      .filter((m) => m.type === 'navigate' && m.action === 'goto')
    expect(gotos).toEqual([])
    wrapper.unmount()
  })

  it('starts watch-only; take over and return toggle remote input', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean }
    const toggle = () => wrapper.get('[data-testid="novnc-control-toggle"]')
    expect(rfb.viewOnly).toBe(true)
    expect(toggle().text()).toBe('接管')
    expect(wrapper.find('[data-testid="novnc-watch-hint"]').exists()).toBe(true)

    await toggle().trigger('click')
    expect(rfb.viewOnly).toBe(false)
    expect(toggle().text()).toBe('交还')
    expect(toggle().attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('[data-testid="novnc-control-frame"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="novnc-watch-hint"]').exists()).toBe(false)

    await toggle().trigger('click')
    expect(rfb.viewOnly).toBe(true)
    expect(wrapper.find('[data-testid="novnc-control-frame"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('inspect lifts watch-only while armed, then restores it', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean }
    await inspectButton(wrapper).trigger('click')
    expect(rfb.viewOnly).toBe(false)
    expect(wrapper.find('[data-testid="novnc-watch-hint"]').exists()).toBe(false)
    await inspectButton(wrapper).trigger('click')
    expect(rfb.viewOnly).toBe(true)
    wrapper.unmount()
  })

  it('reconnect drops take-over back to watch-only', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    await wrapper.get('[data-testid="novnc-control-toggle"]').trigger('click')
    await wrapper.setProps({ sandboxId: 2 })
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean }
    expect(rfb.viewOnly).toBe(true)
    expect(wrapper.get('[data-testid="novnc-control-toggle"]').text()).toBe('接管')
    wrapper.unmount()
  })

  it('console mode also offers take over', async () => {
    const wrapper = mountNovnc({ sandboxId: 42, console: true })
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean }
    expect(rfb.viewOnly).toBe(true)
    await wrapper.get('[data-testid="novnc-control-toggle"]').trigger('click')
    expect(rfb.viewOnly).toBe(false)
    wrapper.unmount()
  })

  it('clicking the watch-only screen takes over and swallows that gesture', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean; focusCount: number }
    const screen = screenStandIn(wrapper)
    const seen = trackEvents(screen, SCREEN_GESTURE)
    expect(wrapper.get('[data-testid="novnc-watch-hint"]').text()).toBe(
      '仅观看 · 点击画面接管，本次点击不会操作页面',
    )

    clickScreen(screen)
    await nextTick()

    expect(seen).toEqual([])
    expect(rfb.viewOnly).toBe(false)
    expect(rfb.focusCount).toBe(1)
    expect(controlButton(wrapper).text()).toBe('交还')
    expect(controlButton(wrapper).attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('[data-testid="novnc-control-frame"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="novnc-watch-hint"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('你正在操作这个页面，完成后点「交还」')

    seen.length = 0
    clickScreen(screen)
    expect(seen).toEqual(['mousedown', 'mousemove', 'mouseup', 'click'])
    expect(rfb.focusCount).toBe(1)
    wrapper.unmount()
  })

  it('right click and touch on a watch-only screen only take over', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean; focusCount: number }
    const screen = screenStandIn(wrapper)
    const seen = trackEvents(screen, [...SCREEN_GESTURE, 'auxclick', 'contextmenu', 'touchstart', 'touchend'])

    clickScreen(screen, 2)
    await nextTick()
    expect(seen).toEqual([])
    expect(rfb.viewOnly).toBe(false)
    expect(controlButton(wrapper).text()).toBe('交还')

    wrapper.unmount()

    const again = mountNovnc()
    await flushPromises()
    const rfb2 = rfbMocks.last() as unknown as { viewOnly: boolean }
    const screen2 = screenStandIn(again)
    const touches = trackEvents(screen2, ['touchstart', 'touchmove', 'touchend', 'mousedown', 'click'])
    dispatchTouch(screen2, 'touchstart')
    dispatchTouch(screen2, 'touchmove')
    dispatchTouch(screen2, 'touchend')
    dispatchTouch(screen2, 'mousedown')
    dispatchTouch(screen2, 'click')
    await nextTick()
    expect(touches).toEqual([])
    expect(rfb2.viewOnly).toBe(false)
    expect(controlButton(again).text()).toBe('交还')

    touches.length = 0
    dispatchTouch(screen2, 'touchstart')
    dispatchTouch(screen2, 'touchend')
    expect(touches).toEqual(['touchstart', 'touchend'])
    again.unmount()
  })

  it('keydown and wheel while watch-only do not take over', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean; focusCount: number }
    const screen = screenStandIn(wrapper)
    screen.dispatchEvent(new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: 40 }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a', bubbles: true, cancelable: true }))
    await nextTick()
    expect(rfb.viewOnly).toBe(true)
    expect(rfb.focusCount).toBe(0)
    expect(controlButton(wrapper).text()).toBe('接管')
    expect(wrapper.find('[data-testid="novnc-watch-hint"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('inspect clicks are not captured and do not rewrite take over', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean }
    await inspectButton(wrapper).trigger('click')
    const screen = screenStandIn(wrapper)
    const seen = trackEvents(screen, SCREEN_GESTURE)
    clickScreen(screen)
    await nextTick()
    expect(seen).toEqual(['mousedown', 'mousemove', 'mouseup', 'click'])
    expect(controlButton(wrapper).text()).toBe('接管')
    expect(controlButton(wrapper).attributes('aria-pressed')).toBe('false')
    expect(wrapper.find('[data-testid="novnc-control-frame"]').exists()).toBe(false)
    expect(rfb.viewOnly).toBe(false)

    await inspectButton(wrapper).trigger('click')
    expect(rfb.viewOnly).toBe(true)
    expect(controlButton(wrapper).text()).toBe('接管')
    wrapper.unmount()
  })

  it('cancelling inspect keeps a take over that was already on', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean }
    await controlButton(wrapper).trigger('click')
    await inspectButton(wrapper).trigger('click')
    const screen = screenStandIn(wrapper)
    clickScreen(screen)
    await inspectButton(wrapper).trigger('click')
    expect(controlButton(wrapper).text()).toBe('交还')
    expect(rfb.viewOnly).toBe(false)
    expect(wrapper.find('[data-testid="novnc-control-frame"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not take over when the screen is clicked before the session is live', async () => {
    MockWebSocket.failWith = 'down'
    const wrapper = mountNovnc()
    await flushPromises()
    const toggle = controlButton(wrapper)
    expect(toggle.text()).toBe('接管')
    expect(toggle.attributes('disabled')).toBeDefined()
    const screen = screenStandIn(wrapper)
    const seen = trackEvents(screen, SCREEN_GESTURE)
    clickScreen(screen)
    await nextTick()
    expect(seen).toEqual(['mousedown', 'mousemove', 'mouseup', 'click'])
    expect(toggle.text()).toBe('接管')
    expect(wrapper.find('[data-testid="novnc-control-frame"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('reconnect clears take over so the next screen click is swallowed again', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const screen = screenStandIn(wrapper)
    clickScreen(screen)
    await nextTick()
    expect(controlButton(wrapper).text()).toBe('交还')

    await wrapper.setProps({ sandboxId: 2 })
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean; focusCount: number }
    expect(rfb.viewOnly).toBe(true)
    expect(controlButton(wrapper).text()).toBe('接管')
    expect(wrapper.find('[data-testid="novnc-control-frame"]').exists()).toBe(false)

    const screen2 = screenStandIn(wrapper)
    const seen = trackEvents(screen2, SCREEN_GESTURE)
    clickScreen(screen2)
    await nextTick()
    expect(seen).toEqual([])
    expect(rfb.viewOnly).toBe(false)
    expect(rfb.focusCount).toBe(1)
    expect(controlButton(wrapper).text()).toBe('交还')
    wrapper.unmount()
  })

  it('console mode screen click takes over without sending that gesture', async () => {
    const wrapper = mountNovnc({ sandboxId: 42, console: true })
    await flushPromises()
    const rfb = rfbMocks.last() as unknown as { viewOnly: boolean; focusCount: number }
    const screen = screenStandIn(wrapper)
    const seen = trackEvents(screen, SCREEN_GESTURE)
    clickScreen(screen)
    await nextTick()
    expect(seen).toEqual([])
    expect(rfb.viewOnly).toBe(false)
    expect(rfb.focusCount).toBe(1)
    expect(controlButton(wrapper).text()).toBe('交还')
    seen.length = 0
    clickScreen(screen)
    expect(seen).toEqual(['mousedown', 'mousemove', 'mouseup', 'click'])
    wrapper.unmount()
  })

  it('shows the english watch-only hint', async () => {
    const wrapper = mountNovnc({}, 'en')
    await flushPromises()
    expect(wrapper.get('[data-testid="novnc-watch-hint"]').text()).toBe(
      'Watching only · Click the screen to take over. This click will not affect the page.',
    )
    wrapper.unmount()
  })

  it('renders console mode address bar', async () => {
    const wrapper = mountNovnc({ sandboxId: 42, console: true })
    await flushPromises()
    expect(apiMocks.sandboxVncWsUrl).toHaveBeenCalledWith(42)
    expect(wrapper.find('form').exists()).toBe(true)
    expect(inspectButton(wrapper).exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows inspect on console noVNC when inspectable', async () => {
    const wrapper = mountNovnc({
      sandboxId: 42,
      console: true,
      inspectable: true,
    })
    await flushPromises()
    expect(inspectButton(wrapper).exists()).toBe(true)
    expect(inspectButton(wrapper).text()).toContain('取点标注')
    wrapper.unmount()
  })

  it('uses platform WS only and never mentions websockify/6080 (g4.2)', async () => {
    const preview = mountNovnc()
    await flushPromises()
    expect(apiMocks.sandboxVncWsUrl).toHaveBeenCalledWith(1)
    expect(preview.html()).not.toMatch(/6080|websockify/i)
    preview.unmount()

    const consoleWrap = mountNovnc({ sandboxId: 42, console: true })
    await flushPromises()
    expect(apiMocks.sandboxVncWsUrl).toHaveBeenCalledWith(42)
    expect(consoleWrap.html()).not.toMatch(/6080|websockify/i)
    consoleWrap.unmount()
  })

  it('toggles inspect label to 取消标注; second click sends on:false', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const btn = inspectButton(wrapper)
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(btn.text()).toContain('取点标注')
    expect(wrapper.text()).not.toContain('取消标注')

    await btn.trigger('click')
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(btn.text()).toContain('取消标注')
    expect(btn.text()).not.toContain('取点标注')
    expect(lastInspectCtrl()).toEqual({ type: 'inspect', on: true })

    await btn.trigger('click')
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(btn.text()).toContain('取点标注')
    expect(wrapper.text()).not.toContain('取消标注')
    expect(lastInspectCtrl()).toEqual({ type: 'inspect', on: false })
    wrapper.unmount()
  })

  it('Esc while inspecting clears pressed state but keeps staged pick', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]
    expect(ws).toBeTruthy()

    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('true')

    ws!.onmessage?.({
      data: JSON.stringify({
        type: 'picked',
        pick: { selector: '#x', tagName: 'div', outerHTML: '<div id="x"></div>' },
      }),
    })
    await flushPromises()
    // pick auto-exits inspect; re-enter then Esc must keep staged
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')
    expect(wrapper.text()).toContain('#x')

    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('true')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')
    expect(wrapper.text()).toContain('#x')
    expect(lastInspectCtrl()).toEqual({ type: 'inspect', on: false })
    wrapper.unmount()
  })

  it('remote inspect-canceled clears button without clearing staged pick', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]!

    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    ws.onmessage?.({
      data: JSON.stringify({
        type: 'picked',
        pick: { selector: '#keep', tagName: 'span', outerHTML: '<span id="keep"></span>' },
      }),
    })
    await flushPromises()
    expect(wrapper.text()).toContain('#keep')

    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    const beforeLen = ws.sent.length
    ws.onmessage?.({ data: JSON.stringify({ type: 'inspect-canceled' }) })
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')
    expect(wrapper.text()).toContain('#keep')
    expect(wrapper.find('[data-testid="novnc-inline-tip"]').exists()).toBe(false)
    // Overlay.inspectModeCanceled does not leave searchForNode; echo on:false.
    expect(lastInspectCtrl()).toEqual({ type: 'inspect', on: false })
    expect(ws.sent.slice(beforeLen).some((s) => s.includes('"on":false'))).toBe(true)
    wrapper.unmount()
  })

  it('after remote cancel, toolbar click can turn inspect on then off again', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]!
    const btn = inspectButton(wrapper)

    await btn.trigger('click')
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('true')

    ws.onmessage?.({ data: JSON.stringify({ type: 'inspect-canceled' }) })
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(btn.text()).toContain('取点标注')

    await btn.trigger('click')
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(btn.text()).toContain('取消标注')
    expect(lastInspectCtrl()).toEqual({ type: 'inspect', on: true })

    await btn.trigger('click')
    await flushPromises()
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(btn.text()).toContain('取点标注')
    expect(lastInspectCtrl()).toEqual({ type: 'inspect', on: false })
    wrapper.unmount()
  })

  it('public wsUrl reconnect emits reconnect-request instead of reusing ticket', async () => {
    const before = MockWebSocket.instances.length
    const wrapper = mountNovnc({
      wsUrl: 'wss://example.test/public/gate-approvals/preview-vnc/ws?ticket=old',
      sandboxId: undefined,
    })
    await flushPromises()
    expect(apiMocks.sandboxVncWsUrl).not.toHaveBeenCalled()
    expect(MockWebSocket.instances.length).toBeGreaterThan(before)

    // Force error banner with reconnect button.
    const ws = MockWebSocket.instances[MockWebSocket.instances.length - 1]!
    ws.onmessage?.({ data: JSON.stringify({ type: 'error', message: 'ticket expired' }) })
    await flushPromises()
    const btn = wrapper.findAll('button').find((b) => b.text().includes('重新连接') || b.text().includes('reconnect'))
    expect(btn).toBeTruthy()
    const socketsBeforeClick = MockWebSocket.instances.length
    await btn!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('reconnect-request')?.length).toBe(1)
    expect(MockWebSocket.instances.length).toBe(socketsBeforeClick)
    wrapper.unmount()
  })

  it('not-ready clears sticky inspect and shows Demo intercept tip (g3.1/S2)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]!

    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('true')

    ws.onmessage?.({ data: JSON.stringify({ type: 'not-ready' }) })
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')
    const tip = wrapper.find('[data-testid="novnc-inline-tip"]')
    expect(tip.exists()).toBe(true)
    expect(tip.text()).toContain('窗口未就绪，无法可靠取点')
    expect(wrapper.find('[data-testid="novnc-pick-result"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('inspect-off-failed shows a tip after cancel', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]!

    await inspectButton(wrapper).trigger('click')
    await inspectButton(wrapper).trigger('click')
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')

    ws.onmessage?.({ data: JSON.stringify({ type: 'inspect-off-failed' }) })
    await flushPromises()
    const tip = wrapper.find('[data-testid="novnc-inline-tip"]')
    expect(tip.exists()).toBe(true)
    expect(tip.text()).toContain('页面可能仍在取点模式')
    wrapper.unmount()
  })

  it('describe-failed shows tip, clears sticky, no pick result (g3.2/S3)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]!

    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('true')

    ws.onmessage?.({ data: JSON.stringify({ type: 'describe-failed' }) })
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')
    const tip = wrapper.find('[data-testid="novnc-inline-tip"]')
    expect(tip.exists()).toBe(true)
    expect(tip.text()).toContain('未能识别该元素，请重试')
    expect(wrapper.find('[data-testid="novnc-pick-result"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('picked result bar shows selector and url then exits inspect (g3.3/S1)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]!

    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    ws.onmessage?.({
      data: JSON.stringify({
        type: 'picked',
        pick: {
          selector: 'textarea#msg',
          tagName: 'textarea',
          outerHTML: '<textarea id="msg"></textarea>',
          url: 'http://127.0.0.1:8080/',
        },
      }),
    })
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')
    expect(wrapper.find('[data-testid="novnc-inline-tip"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="novnc-pick-selector"]').text()).toBe('textarea#msg')
    expect(wrapper.find('[data-testid="novnc-pick-url"]').text()).toBe('http://127.0.0.1:8080/')
    expect(wrapper.text()).toContain('selector')
    expect(wrapper.text()).toContain('url')
    wrapper.unmount()
  })

  it('Esc while inspecting does not show failure tip (S3 cancel path)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    await inspectButton(wrapper).trigger('click')
    await flushPromises()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(inspectButton(wrapper).attributes('aria-pressed')).toBe('false')
    expect(wrapper.find('[data-testid="novnc-inline-tip"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('restoreViewport reasserts scaleViewport after host size 0→positive (g2.1/g2.2 hide→show)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last()
    expect(rfb).toBeTruthy()
    expect(resizeObserverMocks.callbacks.length).toBeGreaterThan(0)

    // Simulate v-show hide: viewport collapses to 0×0 (noVNC would autoscale to 0).
    setPreviewBoxSize(wrapper, 0, 0)
    resizeObserverMocks.trigger()
    await flushPromises()

    const setsBefore = rfb!.scaleViewportSets.length

    // Simulate tab return: viewport lays out again → restore must reassert scaleViewport.
    setPreviewBoxSize(wrapper, 800, 600)
    resizeObserverMocks.trigger()
    await flushPromises()

    expect(rfb!.scaleViewportSets.length).toBeGreaterThan(setsBefore)
    expect(rfb!.scaleViewportSets[rfb!.scaleViewportSets.length - 1]).toBe(true)
    // Must not teardown/reconnect on a live session.
    expect(MockWebSocket.instances.length).toBe(1)
    expect(wrapper.find('[data-testid="novnc-inspect-toggle"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('dead socket after hide surfaces closed/reconnect UI instead of silent black (g2.2)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const ws = MockWebSocket.instances[0]!
    expect(wrapper.text()).toMatch(/已连接|连接中/)

    setPreviewBoxSize(wrapper, 0, 0)
    resizeObserverMocks.trigger()
    await flushPromises()

    // Session dies while hidden (WS closed); readyState no longer OPEN.
    ws.readyState = 3 // CLOSED
    setPreviewBoxSize(wrapper, 800, 600)
    resizeObserverMocks.trigger()
    await flushPromises()

    expect(wrapper.find('[data-testid="novnc-closed-msg"]').text()).toContain('连接已断开')
    expect(wrapper.find('[data-testid="novnc-closed-msg"]').text()).toContain('正在重新连接')
    // Toolbar controls remain usable (g2.3).
    expect(inspectButton(wrapper).exists()).toBe(true)
    wrapper.unmount()
  })

  describe('auto reconnect', () => {
    beforeEach(() => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
    })
    afterEach(() => {
      vi.useRealTimers()
      setVisibility('visible')
    })

    function setVisibility(v: 'visible' | 'hidden') {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => v })
    }

    function serverClose(ws: MockWebSocket, reason: string) {
      ws.onmessage?.({ data: JSON.stringify({ type: 'closed', reason }) })
    }

    it('closed idle shows a localized reason and reconnects with a fresh socket', async () => {
      const wrapper = mountNovnc()
      await flushPromises()
      serverClose(MockWebSocket.instances[0]!, 'idle')
      await flushPromises()
      const msg = wrapper.find('[data-testid="novnc-closed-msg"]').text()
      expect(msg).toContain('长时间无操作已断开')
      expect(msg).not.toMatch(/\bidle\b/)
      expect(wrapper.find('[data-testid="novnc-status"]').text()).toBe('重连中…')
      expect(wrapper.find('[data-testid="novnc-reconnect"]').exists()).toBe(false)

      await vi.advanceTimersByTimeAsync(1000)
      await flushPromises()
      expect(MockWebSocket.instances).toHaveLength(2)
      expect(wrapper.find('[data-testid="novnc-status"]').text()).toBe('已连接')
      wrapper.unmount()
    })

    it('waits for the tab to become visible before reconnecting', async () => {
      const wrapper = mountNovnc()
      await flushPromises()
      setVisibility('hidden')
      serverClose(MockWebSocket.instances[0]!, 'idle')
      await vi.advanceTimersByTimeAsync(60_000)
      expect(MockWebSocket.instances).toHaveLength(1)

      setVisibility('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(MockWebSocket.instances).toHaveLength(2)
      wrapper.unmount()
    })

    it('superseded does not reconnect and keeps the manual button', async () => {
      const wrapper = mountNovnc()
      await flushPromises()
      serverClose(MockWebSocket.instances[0]!, 'superseded')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(60_000)
      expect(MockWebSocket.instances).toHaveLength(1)
      expect(wrapper.find('[data-testid="novnc-closed-msg"]').text()).toContain('已在其他窗口打开')
      expect(wrapper.find('[data-testid="novnc-reconnect"]').exists()).toBe(true)
      wrapper.unmount()
    })

    it('gives up after the retry budget and shows the manual button', async () => {
      const wrapper = mountNovnc()
      await flushPromises()
      MockWebSocket.failWith = 'sandbox gone'
      serverClose(MockWebSocket.instances[0]!, 'idle')
      for (let i = 0; i < 8; i++) await vi.advanceTimersByTimeAsync(30_000)
      expect(MockWebSocket.instances.length).toBe(1 + 6)
      expect(wrapper.find('[data-testid="novnc-reconnect"]').exists()).toBe(true)
      wrapper.unmount()
    })

    it('sends ping heartbeats only while live and visible', async () => {
      const wrapper = mountNovnc()
      await flushPromises()
      const ws = MockWebSocket.instances[0]!
      const pings = () => ws.sent.filter((d) => d === JSON.stringify({ type: 'ping' })).length

      await vi.advanceTimersByTimeAsync(60_000)
      expect(pings()).toBe(1)
      setVisibility('hidden')
      await vi.advanceTimersByTimeAsync(120_000)
      expect(pings()).toBe(1)
      wrapper.unmount()
    })
  })

  it('document visibility restore reasserts scaleViewport without reconnect (g2.2)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last()!
    setPreviewBoxSize(wrapper, 640, 480)
    const setsBefore = rfb.scaleViewportSets.length

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'visible',
    })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()

    expect(rfb.scaleViewportSets.length).toBeGreaterThan(setsBefore)
    expect(rfb.scaleViewportSets[rfb.scaleViewportSets.length - 1]).toBe(true)
    expect(MockWebSocket.instances.length).toBe(1)
    wrapper.unmount()
  })

  it('reasserts scaleViewport when a small positive viewport grows, without a new socket (g1.2/g2.1)', async () => {
    const wrapper = mountNovnc()
    await flushPromises()
    const rfb = rfbMocks.last()
    expect(rfb).toBeTruthy()
    expect(resizeObserverMocks.callbacks.length).toBeGreaterThan(0)
    const socketsBefore = MockWebSocket.instances.length

    // Connection-time box is already positive (screenshot failure: ~538×302), not 0×0.
    setPreviewBoxSize(wrapper, 538, 302)
    resizeObserverMocks.trigger()
    await flushPromises()
    const setsAfterSmall = rfb!.scaleViewportSets.length
    expect(setsAfterSmall).toBeGreaterThan(0)
    expect(rfb!.scaleViewportSets[setsAfterSmall - 1]).toBe(true)

    // Same session, pane grows to the full preview area. Must fit again.
    setPreviewBoxSize(wrapper, 1932, 1279)
    resizeObserverMocks.trigger()
    await flushPromises()

    expect(rfb!.scaleViewportSets.length).toBeGreaterThan(setsAfterSmall)
    expect(rfb!.scaleViewportSets[rfb!.scaleViewportSets.length - 1]).toBe(true)
    expect(rfb!.resizeSession).toBe(false)
    expect(MockWebSocket.instances.length).toBe(socketsBefore)
    wrapper.unmount()
  })
})

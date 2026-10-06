// @vitest-environment happy-dom
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import type { SandboxView } from '@/lib/api/api'
import { setTheme } from '@/lib/shared/theme'

const apiMocks = vi.hoisted(() => ({
  getSandbox: vi.fn(),
  listSandboxes: vi.fn(),
  sandboxLog: vi.fn(),
  sandboxIdeUrl: (_id: number) => 'about:blank#ide',
  sandboxBridgeUrl: (_id: number) => 'about:blank#acp',
  sandboxTerminalWsUrl: (id: number) => `wss://test.local/sandboxes/${id}/terminal`,
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getSandbox: apiMocks.getSandbox,
      listSandboxes: apiMocks.listSandboxes,
      sandboxLog: apiMocks.sandboxLog,
      sandboxIdeUrl: apiMocks.sandboxIdeUrl,
      sandboxBridgeUrl: apiMocks.sandboxBridgeUrl,
      sandboxTerminalWsUrl: apiMocks.sandboxTerminalWsUrl,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

const breakpointMocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue') as typeof import('vue')
  return { isMobile: vue.ref(false) }
})

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: breakpointMocks.isMobile }),
}))

vi.mock('@xterm/xterm', () => {
  class Terminal {
    cols = 80
    rows = 24
    loadAddon() {}
    open() {}
    write() {}
    dispose() {}
    onData() {
      return { dispose() {} }
    }
  }
  return { Terminal }
})

vi.mock('@xterm/addon-fit', () => {
  class FitAddon {
    fit() {}
  }
  return { FitAddon }
})

vi.mock('@xterm/xterm/css/xterm.css', () => ({}))

vi.mock('@/components/run/NovncPreviewPanel.vue', () => ({
  default: {
    name: 'NovncPreviewPanel',
    props: ['sandboxId', 'fill'],
    template: '<div data-testid="novnc-stub" />',
  },
}))

import SandboxConsoleView from './SandboxConsoleView.vue'

const MOCK_SANDBOX: SandboxView = {
  id: 42,
  name: 'sb-42',
  profile: 'default',
  purpose: 'test',
  status: 'running',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  containerStatus: 'running',
  busy: false,
  connected: true,
  hasCodeServer: true,
  hasAcp: true,
  password: 'secret-password',
  endpoints: { '5173': '127.0.0.1:5173', '8080': '127.0.0.1:8080' },
}

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  readyState = MockWebSocket.OPEN
  binaryType = 'arraybuffer'
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  constructor(_url: string) {
    queueMicrotask(() => this.onopen?.(new Event('open')))
  }
  send() {}
  close() {
    this.readyState = MockWebSocket.CLOSED
  }
}

let heldMetaResolve: ((value: SandboxView) => void) | null = null

function sandboxView(overrides?: Partial<SandboxView>): SandboxView {
  return { ...MOCK_SANDBOX, ...overrides }
}

async function resolveHeldMeta(value: SandboxView = MOCK_SANDBOX) {
  expect(heldMetaResolve).toBeTruthy()
  heldMetaResolve!(value)
  heldMetaResolve = null
  await flushPromises()
  await nextTick()
}

function clickTab(wrapper: VueWrapper, label: string) {
  const btn = wrapper.findAll('button').find((b) => b.text().includes(label))
  expect(btn).toBeTruthy()
  return btn!.trigger('click')
}

function ideLayer(wrapper: VueWrapper) {
  return wrapper.find('[data-testid="sandbox-console-ide-pane"] [data-testid="hard-load-layer"]')
}

function acpLayer(wrapper: VueWrapper) {
  return wrapper.find('[data-testid="sandbox-console-acp-pane"] [data-testid="hard-load-layer"]')
}

async function mountConsole(
  tabQuery?: string,
  opts?: { empty?: boolean; holdMeta?: boolean; sandbox?: Partial<SandboxView> },
): Promise<VueWrapper> {
  const happy = (window as unknown as { happyDOM?: { settings: { disableIframePageLoading: boolean } } }).happyDOM
  if (happy) happy.settings.disableIframePageLoading = true
  heldMetaResolve = null
  const sb = sandboxView(opts?.sandbox)
  if (opts?.holdMeta) {
    apiMocks.getSandbox.mockImplementation(
      () =>
        new Promise<SandboxView>((resolve) => {
          heldMetaResolve = resolve
        }),
    )
    apiMocks.listSandboxes.mockResolvedValue([sb])
  } else if (opts?.empty) {
    apiMocks.getSandbox.mockRejectedValue(new Error('missing'))
    apiMocks.listSandboxes.mockResolvedValue([])
  } else {
    apiMocks.getSandbox.mockResolvedValue(sb)
    apiMocks.listSandboxes.mockResolvedValue([sb])
  }
  apiMocks.sandboxLog.mockResolvedValue({ content: '', live: false, found: false })

  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/sandboxes', component: { render: () => h('div') } },
      { path: '/sandboxes/:id/console', component: SandboxConsoleView },
    ],
  })
  await router.push({
    path: '/sandboxes/42/console',
    query: tabQuery ? { tab: tabQuery } : {},
  })
  await router.isReady()

  const wrapper = mount(
    defineComponent({
      setup() {
        return () => h(RouterView)
      },
    }),
    {
      global: {
        plugins: [i18n, router],
        stubs: { Icon: true },
      },
      attachTo: document.body,
    },
  )
  await flushPromises()
  await nextTick()
  return wrapper
}

describe('SandboxConsoleView IDE/ACP lazy mount', () => {
  let consoleErrorSpy: { mockRestore: () => void }

  beforeEach(() => {
    breakpointMocks.isMobile.value = false
    vi.stubGlobal('WebSocket', MockWebSocket as unknown as typeof WebSocket)
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        disconnect() {}
        unobserve() {}
      },
    )
    const nativeError = console.error.bind(console)
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation((...args: unknown[]) => {
      const first = args[0] as { message?: string } | string
      const text = typeof first === 'string' ? first : first?.message ?? String(first)
      if (text.includes('Iframe page loading is disabled')) return
      nativeError(...args)
    })
  })

  afterEach(() => {
    consoleErrorSpy.mockRestore()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    heldMetaResolve = null
    document.body.innerHTML = ''
  })

  it('default terminal tab does not mount IDE/ACP iframes', async () => {
    const wrapper = await mountConsole()
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(false)
    expect(wrapper.find('iframe[title="ACP bridge"]').exists()).toBe(false)
    expect(ideLayer(wrapper).exists()).toBe(false)
    expect(acpLayer(wrapper).exists()).toBe(false)
    wrapper.unmount()
  })

  it('mounts IDE iframe on first IDE tab click and keeps it after switch back', async () => {
    const wrapper = await mountConsole()
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(false)

    const ideBtn = wrapper.findAll('button').find((b) => b.text().includes('IDE'))
    expect(ideBtn).toBeTruthy()
    await ideBtn!.trigger('click')
    await flushPromises()
    await nextTick()

    const ideIframe = wrapper.find('iframe[title="code-server"]')
    expect(ideIframe.exists()).toBe(true)
    expect(wrapper.find('iframe[title="ACP bridge"]').exists()).toBe(false)

    const termBtn = wrapper.findAll('button').find((b) => b.text().includes('终端'))
    expect(termBtn).toBeTruthy()
    await termBtn!.trigger('click')
    await nextTick()

    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('?tab=ide deep link mounts IDE iframe', async () => {
    const wrapper = await mountConsole('ide')
    await flushPromises()
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(true)
    expect(wrapper.find('iframe[title="ACP bridge"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('log tab failure is not empty state and keeps prior content', async () => {
    const wrapper = await mountConsole()
    apiMocks.sandboxLog.mockReset()
    apiMocks.sandboxLog.mockResolvedValueOnce({ content: 'boot ok\n', live: true, found: true })
    apiMocks.sandboxLog.mockRejectedValueOnce(new Error('docker gone'))
    const logBtn = wrapper.findAll('button').find((b) => b.text().includes('日志'))
    expect(logBtn).toBeTruthy()
    await logBtn!.trigger('click')
    await flushPromises()
    await nextTick()
    expect(wrapper.text()).toContain('boot ok')
    const refresh = wrapper.findAll('button').find((b) => b.attributes('title')?.includes('刷新') || b.attributes('title')?.includes('Refresh'))
    expect(refresh).toBeTruthy()
    await refresh!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sandbox-console-log-error"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('boot ok')
    expect(wrapper.find('[data-testid="sandbox-console-log-empty"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('?tab=acp deep link mounts ACP iframe; unknown tabs fall back to terminal', async () => {
    const acp = await mountConsole('acp')
    await flushPromises()
    expect(acp.find('iframe[title="ACP bridge"]').exists()).toBe(true)
    acp.unmount()

    const unknown = await mountConsole('acp-native')
    await flushPromises()
    expect(unknown.find('iframe[title="ACP bridge"]').exists()).toBe(false)
    expect(unknown.find('iframe[title="code-server"]').exists()).toBe(false)
    unknown.unmount()
  })

  it('syncs IDE/ACP loading copy in zh-CN and en (g1.5)', () => {
    expect(pages.pages.sandboxConsole.ideLoading).toBe('正在启动 IDE')
    expect(pages.pages.sandboxConsole.acpLoading).toBe('正在连接 ACP')
    expect(enPages.pages.sandboxConsole.ideLoading).toBe('Starting IDE')
    expect(enPages.pages.sandboxConsole.acpLoading).toBe('Connecting to ACP')
  })

  it('shows HardLoadLayer on IDE before iframe load and keeps the top bar usable (g1.1 g3.1)', async () => {
    const wrapper = await mountConsole('ide', { holdMeta: true })
    const layer = ideLayer(wrapper)
    expect(layer.exists()).toBe(true)
    expect(layer.attributes('role')).toBe('status')
    expect(layer.attributes('aria-busy')).toBe('true')
    expect(layer.get('[data-testid="hard-load-stage"]').text()).toBe('正在启动 IDE')
    expect(wrapper.find('[data-testid="sandbox-console-ide-pane"]').attributes('aria-busy')).toBe('true')
    expect(wrapper.find('[data-testid="sandbox-console-ide-unavailable"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('该沙箱镜像未提供 code-server')
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(false)

    await clickTab(wrapper, '终端')
    await nextTick()
    expect(ideLayer(wrapper).exists()).toBe(false)
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides IDE loading after iframe load and does not replay on keep-alive switch (g1.2 g3.1)', async () => {
    const wrapper = await mountConsole('ide')
    await flushPromises()
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(true)
    expect(ideLayer(wrapper).exists()).toBe(true)

    await wrapper.get('iframe[title="code-server"]').trigger('load')
    await nextTick()
    expect(ideLayer(wrapper).exists()).toBe(false)
    expect(wrapper.find('[data-testid="sandbox-console-ide-pane"]').attributes('aria-busy')).toBe('false')

    await clickTab(wrapper, '终端')
    await nextTick()
    await clickTab(wrapper, 'IDE')
    await nextTick()
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(true)
    expect(ideLayer(wrapper).exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not flash ideUnavailable while meta is pending (g1.3 g3.1)', async () => {
    const wrapper = await mountConsole('ide', { holdMeta: true })
    expect(ideLayer(wrapper).exists()).toBe(true)
    expect(wrapper.find('[data-testid="sandbox-console-ide-unavailable"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('该沙箱镜像未提供 code-server')

    await resolveHeldMeta(sandboxView({ hasCodeServer: false }))
    expect(ideLayer(wrapper).exists()).toBe(false)
    expect(wrapper.find('[data-testid="sandbox-console-ide-unavailable"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('该沙箱镜像未提供 code-server')
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows only unavailable when hasCodeServer is false (g1.3 g3.1)', async () => {
    const wrapper = await mountConsole('ide', { sandbox: { hasCodeServer: false } })
    expect(wrapper.find('[data-testid="sandbox-console-ide-unavailable"]').exists()).toBe(true)
    expect(ideLayer(wrapper).exists()).toBe(false)
    expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows boot-class stuck retry after 60s and remounts the IDE iframe (g1.4)', async () => {
    vi.useFakeTimers()
    const wrapper = await mountConsole('ide')
    await flushPromises()
    expect(ideLayer(wrapper).exists()).toBe(true)
    const iframeEl = wrapper.find('iframe[title="code-server"]').element
    vi.advanceTimersByTime(59_000)
    await nextTick()
    expect(wrapper.find('[data-testid="hard-load-stuck"]').exists()).toBe(false)
    vi.advanceTimersByTime(1_000)
    await nextTick()
    expect(wrapper.find('[data-testid="hard-load-stuck"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="hard-load-retry"]').text()).toBe('重试')

    await wrapper.get('[data-testid="hard-load-retry"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="hard-load-stuck"]').exists()).toBe(false)
    expect(ideLayer(wrapper).exists()).toBe(true)
    expect(wrapper.find('iframe[title="code-server"]').element).not.toBe(iframeEl)
    wrapper.unmount()
  })

  it('covers ACP overlay, load dismiss, unavailable mutex, retry, and independence from IDE (g2.1 g3.2)', async () => {
    vi.useFakeTimers()
    const wrapper = await mountConsole('ide')
    await flushPromises()
    await wrapper.get('iframe[title="code-server"]').trigger('load')
    await nextTick()
    expect(ideLayer(wrapper).exists()).toBe(false)

    await clickTab(wrapper, 'ACP')
    await flushPromises()
    await nextTick()
    expect(wrapper.find('iframe[title="ACP bridge"]').exists()).toBe(true)
    expect(acpLayer(wrapper).exists()).toBe(true)
    expect(acpLayer(wrapper).get('[data-testid="hard-load-stage"]').text()).toBe('正在连接 ACP')
    expect(acpLayer(wrapper).attributes('role')).toBe('status')
    expect(ideLayer(wrapper).exists()).toBe(false)

    await clickTab(wrapper, 'IDE')
    await nextTick()
    expect(ideLayer(wrapper).exists()).toBe(false)
    expect(acpLayer(wrapper).exists()).toBe(false)

    await clickTab(wrapper, 'ACP')
    await nextTick()
    expect(acpLayer(wrapper).exists()).toBe(true)

    vi.advanceTimersByTime(60_000)
    await nextTick()
    expect(wrapper.find('[data-testid="sandbox-console-acp-pane"] [data-testid="hard-load-stuck"]').exists()).toBe(true)
    const acpIframe = wrapper.find('iframe[title="ACP bridge"]').element
    await wrapper.get('[data-testid="sandbox-console-acp-pane"] [data-testid="hard-load-retry"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="sandbox-console-acp-pane"] [data-testid="hard-load-stuck"]').exists()).toBe(false)
    expect(acpLayer(wrapper).exists()).toBe(true)
    expect(wrapper.find('iframe[title="ACP bridge"]').element).not.toBe(acpIframe)

    await wrapper.get('iframe[title="ACP bridge"]').trigger('load')
    await nextTick()
    expect(acpLayer(wrapper).exists()).toBe(false)

    await clickTab(wrapper, 'IDE')
    await nextTick()
    await clickTab(wrapper, 'ACP')
    await nextTick()
    expect(acpLayer(wrapper).exists()).toBe(false)
    wrapper.unmount()
  })

  it('dismisses the ACP overlay on AgentChat ready and keeps its theme in sync', async () => {
    setTheme('dark')
    const wrapper = await mountConsole('acp')
    await flushPromises()
    const iframe = wrapper.get('iframe[title="ACP bridge"]').element as HTMLIFrameElement
    expect(iframe.getAttribute('src')).toBe('about:blank#acp#theme=dark')
    const post = vi.fn()
    const frameWin = { postMessage: post }
    Object.defineProperty(iframe, 'contentWindow', { configurable: true, get: () => frameWin })
    const ready = { type: 'grasp:agentchat-ready' }
    const origin = window.location.origin
    const send = (data: unknown, from: unknown, fromOrigin = origin) => {
      const e = new MessageEvent('message', { data, origin: fromOrigin })
      Object.defineProperty(e, 'source', { value: from })
      window.dispatchEvent(e)
    }

    send(ready, window)
    send(ready, frameWin, 'https://evil.test')
    send({ type: 'other' }, frameWin)
    await nextTick()
    expect(acpLayer(wrapper).exists()).toBe(true)

    send(ready, frameWin)
    await nextTick()
    expect(acpLayer(wrapper).exists()).toBe(false)
    expect(post).toHaveBeenLastCalledWith({ type: 'grasp-embed:theme', theme: 'dark' }, origin)

    setTheme('light')
    await nextTick()
    expect(post).toHaveBeenLastCalledWith({ type: 'grasp-embed:theme', theme: 'light' }, origin)
    expect(iframe.getAttribute('src')).toBe('about:blank#acp#theme=dark')
    wrapper.unmount()
    setTheme('dark')
  })

  it('does not flash acp unavailable while meta is pending and only shows it when hasAcp is false (g2.1 g3.2)', async () => {
    const pending = await mountConsole('acp', { holdMeta: true })
    expect(acpLayer(pending).exists()).toBe(true)
    expect(pending.find('[data-testid="sandbox-console-acp-unavailable"]').exists()).toBe(false)
    expect(pending.text()).not.toContain('该沙箱未提供 ACP 桥接服务')
    await resolveHeldMeta(sandboxView({ hasAcp: false }))
    expect(acpLayer(pending).exists()).toBe(false)
    expect(pending.find('[data-testid="sandbox-console-acp-unavailable"]').exists()).toBe(true)
    expect(pending.find('iframe[title="ACP bridge"]').exists()).toBe(false)
    pending.unmount()

    const none = await mountConsole('acp', { sandbox: { hasAcp: false } })
    expect(none.find('[data-testid="sandbox-console-acp-unavailable"]').exists()).toBe(true)
    expect(acpLayer(none).exists()).toBe(false)
    none.unmount()
  })
})

describe('SandboxConsoleView mobile desktop-recommend', () => {
  beforeEach(() => {
    breakpointMocks.isMobile.value = true
    vi.stubGlobal('WebSocket', MockWebSocket as unknown as typeof WebSocket)
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        disconnect() {}
        unobserve() {}
      },
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    document.body.innerHTML = ''
  })

  it('shows desktop recommend first and expands a read-only summary without terminal/iframe', async () => {
    const wrapper = await mountConsole()
    expect(wrapper.find('[data-testid="sandbox-console-mobile"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('推荐在桌面操作')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.find('[data-testid="novnc-stub"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('直连密码')

    await wrapper.get('[data-testid="sandbox-console-peek"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sandbox-console-summary"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('sb-42')
    expect(wrapper.text()).toContain('running')
    expect(wrapper.text()).not.toContain('secret-password')
    wrapper.unmount()
  })

  it('empty session still shows recommend plus readable empty summary', async () => {
    const wrapper = await mountConsole(undefined, { empty: true })
    expect(wrapper.text()).toContain('推荐在桌面操作')
    await wrapper.get('[data-testid="sandbox-console-peek"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('暂无会话数据')
    expect(wrapper.find('iframe').exists()).toBe(false)
    wrapper.unmount()
  })
})

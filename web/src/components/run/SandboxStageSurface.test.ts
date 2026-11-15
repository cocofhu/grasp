// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import SandboxStageSurface from './SandboxStageSurface.vue'

const written = vi.hoisted(() => ({ lines: [] as string[] }))

vi.mock('@/lib/api/api', () => ({
  api: {
    sandboxIdeUrl: (id: number) => `about:blank#ide-${id}`,
    sandboxTerminalWsUrl: (id: number) => `ws://test.local/sandboxes/${id}/terminal`,
    nodeSandboxLog: vi.fn(async () => ({ content: '', live: true, found: true })),
    sandboxLog: vi.fn(async () => ({ content: '', live: true, found: true })),
  },
}))
vi.mock('@xterm/xterm', () => {
  class Terminal {
    cols = 80
    rows = 24
    loadAddon() {}
    open() {}
    write(data: string) {
      written.lines.push(String(data))
    }
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

class FakeSocket {
  static all: FakeSocket[] = []
  static OPEN = 1
  readyState = 0
  binaryType = ''
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  constructor(public url: string) {
    FakeSocket.all.push(this)
  }
  send() {}
  close() {
    this.readyState = 3
    this.onclose?.()
  }
  drop() {
    this.readyState = 3
    this.onclose?.()
  }
}

function i18n() {
  return createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': pages } })
}

function mountSurface(props: Record<string, unknown>) {
  return mount(SandboxStageSurface, {
    props: { active: true, ...props },
    global: { plugins: [i18n()], stubs: { HardLoadLayer: true, RunSandboxPanel: true } },
  })
}

describe('SandboxStageSurface', () => {
  beforeEach(() => {
    FakeSocket.all = []
    written.lines = []
    vi.stubGlobal('WebSocket', FakeSocket)
    vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('reports a dropped terminal socket and retries the same sandbox after 5, 10 and 20 seconds', async () => {
    const wrapper = mountSurface({ kind: 'terminal', sandbox: { id: 4 } })
    await flushPromises()
    expect(FakeSocket.all).toHaveLength(1)
    FakeSocket.all[0]!.drop()
    expect(wrapper.emitted('lost')).toHaveLength(1)
    expect(written.lines.join('')).toContain(pages.pages.reactArtifactStage.terminalReconnecting)
    await vi.advanceTimersByTimeAsync(4_900)
    expect(FakeSocket.all).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(100)
    expect(FakeSocket.all).toHaveLength(2)
    expect(FakeSocket.all[1]!.url).toContain('/sandboxes/4/')

    FakeSocket.all[1]!.drop()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(FakeSocket.all).toHaveLength(3)
    FakeSocket.all[2]!.drop()
    await vi.advanceTimersByTimeAsync(20_000)
    expect(FakeSocket.all).toHaveLength(4)
    FakeSocket.all[3]!.drop()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(FakeSocket.all).toHaveLength(4)
    expect(written.lines.at(-1)).toContain(pages.pages.sandboxConsole.disconnected)
    expect(wrapper.emitted('lost')).toHaveLength(4)
    wrapper.unmount()
  })

  it('connects to a new sandbox id at once and drops the pending retry', async () => {
    const wrapper = mountSurface({ kind: 'terminal', sandbox: { id: 4 } })
    await flushPromises()
    FakeSocket.all[0]!.drop()
    await wrapper.setProps({ sandbox: { id: 5 } })
    await flushPromises()
    expect(FakeSocket.all.at(-1)!.url).toContain('/sandboxes/5/')
    const count = FakeSocket.all.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(FakeSocket.all).toHaveLength(count)
    wrapper.unmount()
  })

  it('does not report or retry when it closes the socket itself', async () => {
    const wrapper = mountSurface({ kind: 'terminal', sandbox: { id: 4 } })
    await flushPromises()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(FakeSocket.all).toHaveLength(1)
    expect(wrapper.emitted('lost')).toBeUndefined()
  })

  it('points the IDE frame at a rebuilt sandbox', async () => {
    const wrapper = mountSurface({ kind: 'ide', sandbox: { id: 4, hasCodeServer: true } })
    await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toContain('ide-4')
    const first = wrapper.get('iframe').element
    await wrapper.setProps({ sandbox: { id: 6, hasCodeServer: true } })
    expect(wrapper.get('iframe').attributes('src')).toContain('ide-6')
    expect(wrapper.get('iframe').element).not.toBe(first)
    wrapper.unmount()
  })
})

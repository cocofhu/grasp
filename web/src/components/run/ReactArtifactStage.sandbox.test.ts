// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { REACT_STAGE_TAB_NOVNC, resetStageOpenStateForTests, saveStageOpenState } from '@/lib/run/reactArtifactPreview'
import { ensureConversationDrawer, resetConversationDrawers } from '@/lib/chat/conversationDrawer'
import ReactArtifactStage from './ReactArtifactStage.vue'
import SandboxStageSurface from './SandboxStageSurface.vue'
import { api } from '@/lib/api/api'

vi.mock('@/lib/api/api', () => ({
  api: {
    artifactContent: vi.fn(async () => ({})),
    getRunNodeSandbox: vi.fn(async () => ({ id: 42 })),
    nodePreviews: vi.fn(async () => ({ ports: [] })),
    nodeSandboxLog: vi.fn(async () => ({ content: '', live: true, found: true })),
    sandboxLog: vi.fn(async () => ({ content: '', live: true, found: true })),
    sandboxIdeUrl: (id: number) => `about:blank#ide-${id}`,
    sandboxTerminalWsUrl: (id: number) => `ws://test.local/sandboxes/${id}/terminal`,
    artifactVersions: vi.fn(async () => []),
    artifactVersionContent: vi.fn(async () => ({})),
    sandboxDesktop: vi.fn(async () => ({ mode: 'browser' })),
  },
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
  }
}

const stubs = {
  Icon: true,
  HtmlPreview: true,
  ArtifactPreview: true,
  AppPreviewPanel: true,
  PublicAppPreviewPanel: true,
  NovncPreviewPanel: defineComponent({
    props: { sandboxId: Number },
    template: '<div data-testid="novnc-stub" :data-sandbox="sandboxId" />',
  }),
}

function i18n() {
  return createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })
}

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('ReactArtifactStage sandbox lifecycle', () => {
  beforeEach(() => {
    resetStageOpenStateForTests(':run-')
    resetConversationDrawers()
    FakeSocket.all = []
    vi.stubGlobal('WebSocket', FakeSocket)
    vi.mocked(api.getRunNodeSandbox).mockReset()
    vi.mocked(api.getRunNodeSandbox).mockResolvedValue({ id: 42 } as never)
    vi.mocked(api.sandboxLog).mockClear()
    vi.mocked(api.nodeSandboxLog).mockClear()
    setVisibility('visible')
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    resetStageOpenStateForTests(':run-')
    resetConversationDrawers()
    document.body.innerHTML = ''
  })

  describe('Solo paused sandbox', () => {
    function mountSolo(props: Record<string, unknown> = {}) {
      return mount(ReactArtifactStage, {
        props: { artifacts: [], runId: 'run-solo', nodeId: 'solo', sandbox: { id: 5, hasCodeServer: true }, registeredPreviews: [], soloSessionId: 's1', ...props },
        global: { plugins: [i18n()], stubs },
      })
    }

    it('adds a 电脑 card on the desktop image and drops a restored 电脑 tab on a browser image', async () => {
      const desktop = { mode: 'desktop', shellReady: true, agentActive: false, holder: 'agent', apps: [] }
      vi.mocked(api.sandboxDesktop).mockResolvedValueOnce(desktop as never)
      let wrapper = mountSolo()
      await flushPromises()
      await wrapper.get('[data-testid="react-artifact-card-computer"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="react-artifact-tab-computer"]').text()).toContain('电脑')
      expect(wrapper.get('[data-testid="react-artifact-preview-computer"] [data-testid="novnc-stub"]').attributes('data-sandbox')).toBe('5')
      wrapper.unmount()

      vi.mocked(api.sandboxDesktop).mockResolvedValueOnce(desktop as never)
      wrapper = mountSolo()
      await flushPromises()
      expect(wrapper.find('[data-testid="react-artifact-tab-computer"]').exists()).toBe(true)
      wrapper.unmount()

      wrapper = mountSolo()
      await flushPromises()
      expect(wrapper.find('[data-testid="react-artifact-card-computer"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="react-artifact-tab-computer"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="react-artifact-preview-computer"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('drops the browser card on the desktop image and moves an open browser tab to 电脑', async () => {
      const desktop = { mode: 'desktop', shellReady: true, agentActive: false, holder: 'agent', apps: [] }
      vi.mocked(api.sandboxDesktop).mockResolvedValueOnce(desktop as never)
      let wrapper = mountSolo()
      await flushPromises()
      expect(wrapper.find('[data-testid="react-artifact-card-browser"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="react-artifact-card-computer"]').exists()).toBe(true)
      wrapper.unmount()
      resetStageOpenStateForTests(':run-')

      let answer!: (v: unknown) => void
      vi.mocked(api.sandboxDesktop).mockReturnValueOnce(new Promise((r) => (answer = r)) as never)
      wrapper = mountSolo()
      await flushPromises()
      await wrapper.get('[data-testid="react-artifact-card-browser"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="react-artifact-tab-browser"]').exists()).toBe(true)
      answer(desktop)
      await flushPromises()
      expect(wrapper.find('[data-testid="react-artifact-tab-browser"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="react-artifact-preview-browser"]').exists()).toBe(false)
      expect(wrapper.get('[data-testid="react-artifact-preview-computer"]').isVisible()).toBe(true)
      wrapper.unmount()
    })

    it('shows a resume placeholder on sandbox tabs and remounts the panel after resume', async () => {
      const wrapper = mountSolo({ sandboxPaused: true })
      await flushPromises()
      await wrapper.get('[data-testid="react-artifact-card-browser"]').trigger('click')
      await flushPromises()
      const browser = wrapper.get('[data-testid="react-artifact-preview-browser"]')
      expect(browser.find('[data-testid="novnc-stub"]').exists()).toBe(false)
      expect(browser.find('[data-testid="react-artifact-browser-missing"]').exists()).toBe(false)
      expect(browser.get('[data-testid="react-artifact-sandbox-paused"]').text()).toContain(pages.pages.solo.pausedStage.title)
      await browser.get('[data-testid="react-artifact-sandbox-resume"]').trigger('click')
      expect(wrapper.emitted('resume')).toHaveLength(1)

      await wrapper.setProps({ sandboxWaking: true })
      const waking = wrapper.get('[data-testid="react-artifact-preview-browser"] [data-testid="react-artifact-sandbox-resume"]')
      expect(waking.attributes('disabled')).toBeDefined()
      expect(wrapper.get('[data-testid="react-artifact-preview-browser"]').text()).toContain(pages.pages.solo.pausedStage.waking)

      await wrapper.setProps({ sandboxPaused: false, sandboxWaking: false })
      await flushPromises()
      expect(wrapper.find('[data-testid="react-artifact-sandbox-paused"]').exists()).toBe(false)
      expect(wrapper.get('[data-testid="novnc-stub"]').attributes('data-sandbox')).toBe('5')
      wrapper.unmount()
    })

    it('opens the resources home instead of a blank stage when the remembered app tab has no preview', async () => {
      const drawer = ensureConversationDrawer('run-solo')!
      drawer.split = true
      drawer.mode = 'resources'
      saveStageOpenState('run-solo', 'solo', { openNames: [], activeTab: REACT_STAGE_TAB_NOVNC, novncOpen: true, fixedOpen: [] })
      const wrapper = mountSolo({ sandboxPaused: true })
      await flushPromises()
      expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="react-artifact-preview-novnc"]').exists()).toBe(false)
      expect((wrapper.get('[data-testid="react-artifact-grid"]').element as HTMLElement).style.display).toBe('')
      expect(wrapper.find('[data-testid="react-artifact-card-app"]').exists()).toBe(false)

      await wrapper.get('[data-testid="react-artifact-card-terminal"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="react-artifact-preview-terminal"] [data-testid="react-artifact-sandbox-paused"]').exists()).toBe(true)
      wrapper.unmount()
    })

    it('covers terminal and IDE, leaves the log tab alone and never queries the node sandbox', async () => {
      const wrapper = mountSolo({ sandboxPaused: true, sandboxLogId: 5 })
      await flushPromises()
      for (const kind of ['terminal', 'ide']) {
        await wrapper.get(`[data-testid="react-artifact-card-${kind}"]`).trigger('click')
        await flushPromises()
        expect(wrapper.get(`[data-testid="react-artifact-preview-${kind}"]`).find('[data-testid="react-artifact-sandbox-paused"]').exists()).toBe(true)
      }
      expect(FakeSocket.all).toHaveLength(0)
      expect(wrapper.find('iframe[title="code-server"]').exists()).toBe(false)
      await wrapper.get('[data-testid="react-artifact-card-log"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="react-artifact-preview-log"]').find('[data-testid="react-artifact-sandbox-paused"]').exists()).toBe(false)
      expect(api.getRunNodeSandbox).not.toHaveBeenCalled()
      expect(api.sandboxLog).toHaveBeenCalledWith(5, expect.anything())
      expect(api.nodeSandboxLog).not.toHaveBeenCalled()
      wrapper.unmount()
    })
  })

  describe('workflow node sandbox refresh', () => {
    function mountNode(props: Record<string, unknown> = {}) {
      return mount(ReactArtifactStage, {
        props: { artifacts: [], runId: 'run-refresh', nodeId: 'clarify', remoteKind: 'off', sessionBusy: false, ...props },
        global: { plugins: [i18n()], stubs },
      })
    }

    it('re-reads the sandbox when a turn starts and ends, reconnecting only on a new id', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
      const wrapper = mountNode()
      await flushPromises()
      await wrapper.get('[data-testid="react-artifact-card-browser"]').trigger('click')
      await flushPromises()
      const first = wrapper.get('[data-testid="novnc-stub"]').element
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(1)

      await wrapper.setProps({ sessionBusy: true })
      await vi.advanceTimersByTimeAsync(2_900)
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(200)
      await flushPromises()
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(2)
      // Same id: the open panel is not remounted.
      expect(wrapper.get('[data-testid="novnc-stub"]').element).toBe(first)

      vi.mocked(api.getRunNodeSandbox).mockResolvedValue({ id: 43 } as never)
      await wrapper.setProps({ sessionBusy: false })
      await flushPromises()
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(3)
      const next = wrapper.get('[data-testid="novnc-stub"]')
      expect(next.attributes('data-sandbox')).toBe('43')
      expect(next.element).not.toBe(first)
      wrapper.unmount()
    })

    it('re-reads the sandbox when a surface reports a lost connection', async () => {
      const wrapper = mountNode()
      await flushPromises()
      await wrapper.get('[data-testid="react-artifact-card-terminal"]').trigger('click')
      await flushPromises()
      expect(FakeSocket.all.at(-1)?.url).toContain('/sandboxes/42/')
      vi.mocked(api.getRunNodeSandbox).mockResolvedValue({ id: 77 } as never)
      wrapper.getComponent(SandboxStageSurface).vm.$emit('lost')
      await flushPromises()
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(2)
      expect(FakeSocket.all.at(-1)?.url).toContain('/sandboxes/77/')
      wrapper.unmount()
    })

    it('polls a visible sandbox tab without a sandbox every 5 seconds for at most 2 minutes', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
      vi.mocked(api.getRunNodeSandbox).mockResolvedValue(null)
      const wrapper = mountNode()
      await flushPromises()
      await wrapper.get('[data-testid="react-artifact-card-terminal"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="react-artifact-terminal-missing"]').exists()).toBe(true)
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(5_000)
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(2)

      setVisibility('hidden')
      await vi.advanceTimersByTimeAsync(15_000)
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(2)
      setVisibility('visible')
      await flushPromises()
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(3)

      await vi.advanceTimersByTimeAsync(130_000)
      const capped = vi.mocked(api.getRunNodeSandbox).mock.calls.length
      expect(capped).toBeLessThanOrEqual(3 + 24)
      await vi.advanceTimersByTimeAsync(60_000)
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(capped)
      wrapper.unmount()
    })

    it('stops polling once the sandbox appears and connects to it', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
      vi.mocked(api.getRunNodeSandbox).mockResolvedValue(null)
      const wrapper = mountNode()
      await flushPromises()
      await wrapper.get('[data-testid="react-artifact-card-terminal"]').trigger('click')
      await flushPromises()
      vi.mocked(api.getRunNodeSandbox).mockResolvedValue({ id: 9 } as never)
      await vi.advanceTimersByTimeAsync(5_000)
      await flushPromises()
      expect(FakeSocket.all.at(-1)?.url).toContain('/sandboxes/9/')
      const calls = vi.mocked(api.getRunNodeSandbox).mock.calls.length
      await vi.advanceTimersByTimeAsync(30_000)
      expect(api.getRunNodeSandbox).toHaveBeenCalledTimes(calls)
      wrapper.unmount()
    })

    it('leaves a host-supplied sandbox alone', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
      const wrapper = mountNode({ sandbox: { id: 3 } })
      await flushPromises()
      await wrapper.setProps({ sessionBusy: true })
      await vi.advanceTimersByTimeAsync(5_000)
      await wrapper.setProps({ sessionBusy: false })
      await flushPromises()
      expect(api.getRunNodeSandbox).not.toHaveBeenCalled()
      wrapper.unmount()
    })
  })
})

import { ref } from 'vue'
import {
  EMBED_CMD_MESSAGE,
  EMBED_CONTROL_MESSAGE,
  PAGE_CONTROL_CAP,
  loadPageControl,
  savePageControl,
  type EmbedCmdResult,
  type EmbedControlMessage,
} from '@/lib/inbox/embedChat'

export type PageControlState = 'online' | 'paused' | 'offline'

/** How long the drawer waits for the page script to announce page control before calling it outdated. */
export const PAGE_CONTROL_HELLO_MS = 3000

type Frame = Record<string, unknown>

export type PageControlOptions = {
  runId: string
  nodeId: string
  /** postMessage to the preview page. */
  post: (msg: Frame) => void
  /** Send a frame on the drawer events WebSocket; false when not connected. */
  send: (frame: Frame) => boolean
  isVisible?: () => boolean
}

function nonce(): string {
  const c = globalThis.crypto
  if (c?.randomUUID) return c.randomUUID()
  const b = new Uint8Array(16)
  c?.getRandomValues?.(b)
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('') || String(Math.random()).slice(2)
}

/**
 * Drawer side of agent page control: owns the toggle, reports it with tab
 * visibility to the server, and relays page_cmd frames to the preview page
 * (and results back). The server decides whose page a command goes to.
 */
export function usePageControl(opts: PageControlOptions) {
  const supported = ref<boolean | null>(null)
  const enabled = ref(false)
  const state = ref<PageControlState>('offline')
  /** False when another tab of the same owner holds control. */
  const active = ref(true)
  let tab = ''
  let helloTimer: ReturnType<typeof setTimeout> | undefined
  /** nonce → server command id; the page never sees server ids. */
  const inflight = new Map<string, string>()

  const visible = () => (opts.isVisible ? opts.isVisible() : document.visibilityState !== 'hidden')

  function report() {
    opts.send({ type: 'page_control', on: enabled.value && supported.value === true, visible: visible() })
  }

  function announce() {
    opts.post({ type: EMBED_CONTROL_MESSAGE, on: enabled.value })
  }

  function cancelInflight() {
    for (const n of inflight.keys()) opts.post({ type: EMBED_CMD_MESSAGE, nonce: n, action: 'cancel' })
    inflight.clear()
  }

  function setEnabled(on: boolean) {
    if (on && supported.value !== true) return
    if (enabled.value === on) return
    enabled.value = on
    savePageControl(opts.runId, opts.nodeId, tab, on)
    if (!on) cancelInflight()
    announce()
    report()
  }

  /** Call after the drawer posts grasp-embed:ready to the page. */
  function awaitHello() {
    if (supported.value !== null) return
    clearTimeout(helloTimer)
    helloTimer = setTimeout(() => {
      if (supported.value === null) supported.value = false
    }, PAGE_CONTROL_HELLO_MS)
  }

  function onPageControl(m: EmbedControlMessage) {
    if (m.stop) {
      setEnabled(false)
      return
    }
    if (!m.caps?.includes(PAGE_CONTROL_CAP)) return
    clearTimeout(helloTimer)
    supported.value = true
    tab = m.tab || ''
    enabled.value = loadPageControl(opts.runId, opts.nodeId, tab)
    announce()
    report()
  }

  function onPageResult(r: EmbedCmdResult) {
    const id = inflight.get(r.nonce)
    if (!id) return
    inflight.delete(r.nonce)
    opts.send({ type: 'page_result', id, ok: r.ok, error: r.error, note: r.note, state: r.state })
  }

  /** Handles page_* frames from the events WebSocket. */
  function onServerFrame(m: Frame) {
    switch (m.type) {
      case 'page_control_state':
        state.value = m.state === 'online' || m.state === 'paused' ? m.state : 'offline'
        active.value = m.active !== false
        return
      case 'page_cmd': {
        const id = typeof m.id === 'string' ? m.id : ''
        if (!id) return
        if (!enabled.value || supported.value !== true) {
          opts.send({ type: 'page_result', id, ok: false, error: '用户没有开启页面操作' })
          return
        }
        const n = nonce()
        inflight.set(n, id)
        opts.post({ type: EMBED_CMD_MESSAGE, nonce: n, action: m.action, args: m.args ?? {} })
        return
      }
      case 'page_cmd_cancel':
        for (const [n, id] of inflight) {
          if (id !== m.id) continue
          inflight.delete(n)
          opts.post({ type: EMBED_CMD_MESSAGE, nonce: n, action: 'cancel' })
        }
        return
    }
  }

  /** The events socket (re)connected: re-register. */
  function onEventsReady() {
    report()
  }

  /** The events socket dropped: commands in flight are the server's to resolve. */
  function onEventsClosed() {
    state.value = 'offline'
    cancelInflight()
  }

  /** Session expired / revoked: control ends with it. */
  function reset() {
    cancelInflight()
    if (enabled.value) {
      enabled.value = false
      savePageControl(opts.runId, opts.nodeId, tab, false)
      announce()
    }
    state.value = 'offline'
  }

  const onVisibility = () => report()
  document.addEventListener('visibilitychange', onVisibility)

  function dispose() {
    clearTimeout(helloTimer)
    document.removeEventListener('visibilitychange', onVisibility)
    inflight.clear()
  }

  return {
    supported,
    enabled,
    state,
    active,
    setEnabled,
    awaitHello,
    onPageControl,
    onPageResult,
    onServerFrame,
    onEventsReady,
    onEventsClosed,
    reset,
    dispose,
  }
}

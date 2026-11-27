<script setup lang="ts">
import { ref, nextTick, onMounted, onBeforeUnmount, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import { api } from '@/lib/api/api'
import { PreviewVncChannel } from '@/lib/shared/previewVncChannel'
import { createPreviewFpsCounter } from '@/lib/shared/previewFps'
import { useDesktopControl } from '@/lib/composables/useDesktopControl'
import { useDesktopAudio } from '@/lib/composables/useDesktopAudio'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import {
  VNC_HEARTBEAT_MS,
  VNC_RECONNECT_MAX_ATTEMPTS,
  closeReasonKey,
  reconnectDelayMs,
  shouldAutoReconnect,
} from '@/lib/shared/vncReconnect'
// @ts-expect-error noVNC ships without bundled types
// Pinned to exact 1.5.0 (see package.json). Official path is HTTP + x11vnc -nopw
// (None auth) + PreviewVncChannel demux. noVNC's Secure Context Log.Error is
// stripped at build time by vite-plugins/stripNovncSecureContext (localhost is
// already secure; non-localhost http:// would otherwise spam / risk breakage).
import RFB from '@novnc/novnc/lib/rfb.js'

const emit = defineEmits<{
  (e: 'pick', payload: AppPreviewPickPayload): void
  /** Element picked in inspect mode but not yet added to chat (last staged). */
  (e: 'staged-pick', payload: AppPreviewPickPayload | null): void
  /** Public ticket channel: parent must re-exchange ticket before reconnecting. */
  (e: 'reconnect-request'): void
  /** openRequest went out; the parent drops it so a remount does not open it again. */
  (e: 'open-sent'): void
}>()

const props = withDefaults(
  defineProps<{
    /** The sandbox whose desktop (the Agent's screen) this panel watches. */
    sandboxId?: number
    /** Absolute ws(s) URL of a public share ticket; overrides sandboxId. */
    wsUrl?: string
    /**
     * App port to show on the desktop. Changing it navigates the same page;
     * attaching never navigates, so a viewer first sees what the Agent shows.
     */
    targetPort?: number
    /** Console toolbar (address bar) instead of the preview toolbar (Pick, FPS). */
    console?: boolean
    fill?: boolean
    compact?: boolean
    /** Console mode: show 取点标注 (sandbox-vnc already proxies inspect/pick). */
    inspectable?: boolean
    /** Opens url in a new desktop tab; a new seq opens it again. */
    openRequest?: { url: string; seq: number } | null
    /** 电脑 view: on the desktop image, a framed screen with a control dock instead of the toolbar. */
    computer?: boolean
  }>(),
  { console: false, fill: false, compact: false, inspectable: false, computer: false },
)

const { t } = useI18n()
const fpsCounter = createPreviewFpsCounter()

const consoleMode = computed(() => props.console)
const showInspect = computed(() => !consoleMode.value || props.inspectable)

const canvasHost = ref<HTMLDivElement | null>(null)
/** Preview pane with a definite box. Scale against this, not the canvas content. */
const viewportEl = ref<HTMLDivElement | null>(null)
const rootEl = ref<HTMLDivElement | null>(null)
const isFullscreen = ref(false)
const status = ref<'connecting' | 'live' | 'closed' | 'error'>('connecting')
const rfbConnected = ref(false)
const statusMsg = ref('')
const previewStuck = ref(false)
let previewWarnTimer: ReturnType<typeof setTimeout> | null = null
// First launch is advertised as 30–60s. The server still has a 90s OpenInSandbox
// budget, so a warning before 60s fires while the desktop is starting normally.
const PREVIEW_STUCK_MS = 60_000
const inspect = ref(false)
const inspectToggleLabel = computed(() =>
  t(inspect.value ? 'pages.appPreview.novnc.cancelInspect' : 'pages.appPreview.novnc.inspect'),
)
/** Viewer has taken over mouse and keyboard; otherwise the desktop is watch-only. */
const control = ref(false)
/** Inspect needs clicks on the page, so it lifts watch-only while armed. */
const inputEnabled = computed(() => control.value || inspect.value)
const controlToggleLabel = computed(() =>
  t(control.value ? 'pages.appPreview.novnc.returnControl' : 'pages.appPreview.novnc.takeOver'),
)
/** Desktop image only (never for public share tickets): status bar and soft interlock. */
const desktop = useDesktopControl(() => (props.wsUrl ? undefined : props.sandboxId), control)
const desktopHolderLabel = computed(() =>
  t(control.value ? 'pages.appPreview.desktop.holderYou' : desktop.userHolds.value ? 'pages.appPreview.desktop.holderOther' : 'pages.appPreview.desktop.holderAgent'),
)
/** Desktop sound; public share tickets have none. */
const audio = useDesktopAudio(() => (props.wsUrl ? undefined : props.sandboxId))
const audioOn = computed(() => audio.state.value === 'on' || audio.state.value === 'connecting')
const audioLabel = computed(() => t(`pages.appPreview.novnc.audio.${audio.state.value}`))
/** 电脑 view on the desktop image: the screen is a framed card with a control dock, no browser toolbar. */
const desktopShell = computed(() => props.computer && desktop.isDesktop.value)
/** Taking over while the Agent is mid-turn fails its mouse and keyboard tools, so ask first. */
const takeoverPrompt = ref<'busy' | 'unknown' | null>(null)
let takeoverCheck: symbol | null = null
const desktopTakeoverText = computed(() => {
  if (!desktop.agentBusy.value) return ''
  return t(control.value ? 'pages.appPreview.desktop.takeoverTip' : 'pages.appPreview.desktop.takeoverHint')
})
/** 电脑 view: `ready` can arrive before noVNC paints, so keep loading until the first frame. */
const computerLoading = computed(
  () => props.computer && (status.value === 'connecting' || (status.value === 'live' && !fpsCounter.painted.value)),
)
const SCREEN_BACKGROUND = 'rgb(10,10,11)'
const picked = ref<AppPreviewPickPayload | null>(null)
const address = ref('about:blank')
/** Toolbar inline tip (Demo S2/S3): not-ready warn vs describe-failed err. */
const inlineTip = ref<{ text: string; err: boolean } | null>(null)

let rfb: InstanceType<typeof RFB> | null = null
let channel: PreviewVncChannel | null = null
let disposed = false
/** Console mode: fail fast instead of spinning until the 90s server timeout. */
let connectTimer: ReturnType<typeof setTimeout> | null = null
const CONSOLE_CONNECT_TIMEOUT_MS = 20_000

const CLIPBOARD_MAX_BYTES = 128 * 1024
const clipboardAllowed = ref(true)
const clipboardStateAvailable = ref(false)
const clipboardVisible = computed(() => !!props.sandboxId && !props.wsUrl?.trim() && clipboardAllowed.value)
const clipboardReady = computed(() =>
  clipboardVisible.value && rfbConnected.value && status.value === 'live' && control.value && !inspect.value,
)
const isApplePlatform = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
const pasteShortcut = isApplePlatform ? '⌘V' : 'Ctrl+V'
const clipboardLabel = computed(() => t(clipboardReady.value
  ? 'pages.appPreview.novnc.clipboard.shortcutHint'
  : 'pages.appPreview.novnc.clipboard.takeOverHint', { shortcut: pasteShortcut }))
const clipboardOpen = ref(false)
const clipboardDraft = ref('')
const clipboardTerminalPaste = ref(false)
const clipboardError = ref('')
const clipboardPending = ref<{ id: number; paste: boolean } | null>(null)
let clipboardRequestId = 0
let clipboardShortcutRequest: number | null = null
let clipboardTimer: ReturnType<typeof setTimeout> | undefined
let pendingBrowserPaste: boolean | null = null
let useSandboxClipboard = false
let screenFocusGeneration = 0
let clipboardWatchId: number | null = null
let clipboardCheck: {
  id: number
  text: string
  shift: boolean
  release: ReturnType<PreviewVncChannel['holdOutgoing']>
  timeout: ReturnType<typeof setTimeout>
} | null = null
type PasteModifier = 'Control' | 'Meta' | 'Shift'
const screenModifiers = new Map<string, { key: PasteModifier; location: number; suspended: boolean }>()
let forwardingScreenModifier = false

function resetScreenPaste() {
  screenFocusGeneration++
  pendingBrowserPaste = null
  useSandboxClipboard = false
  clipboardWatchId = null
  screenModifiers.clear()
  if (clipboardCheck) {
    const { release, timeout } = clipboardCheck
    clipboardCheck = null
    clearTimeout(timeout)
    release()
  }
}

function closeClipboard() {
  clipboardOpen.value = false
  clipboardDraft.value = ''
  clipboardTerminalPaste.value = false
  clipboardError.value = ''
  clipboardPending.value = null
  clipboardShortcutRequest = null
  clearTimeout(clipboardTimer)
  resetScreenPaste()
}

function openClipboard() {
  clipboardShortcutRequest = null
  clipboardError.value = ''
  clipboardOpen.value = true
}

function sendClipboard(text: string, paste: boolean, shift = false) {
  if (!clipboardReady.value || !sessionSocketAlive() || !text) return
  const key = 'pages.appPreview.novnc.clipboard'
  if (new TextEncoder().encode(text).length > CLIPBOARD_MAX_BYTES) {
    clipboardError.value = t(`${key}.tooLarge`)
    return
  }
  const id = ++clipboardRequestId
  clipboardError.value = ''
  if (clipboardOpen.value) {
    clipboardPending.value = { id, paste }
    clipboardTimer = setTimeout(() => onClipboardResult(id, false), 8_000)
  } else {
    clipboardShortcutRequest = id
  }
  sendCtrl({ type: 'clipboard', id, text, paste, shift })
}

function onClipboardResult(id: number, ok: boolean, code?: string) {
  if (!clipboardReady.value) return
  const pending = clipboardPending.value
  if (pending && pending.id === id) {
    clipboardPending.value = null
    clearTimeout(clipboardTimer)
    if (ok) {
      closeClipboard()
      void nextTick(() => {
        if (!clipboardReady.value) return
        focusRemoteScreen()
        // A manual copy stays the paste source while the viewer keeps the screen focused.
        useSandboxClipboard = !pending.paste
      })
      return
    }
  } else {
    if (clipboardShortcutRequest === null || id !== clipboardShortcutRequest) return
    clipboardShortcutRequest = null
  }
  if (!ok) clipboardError.value = t(code === 'runtime-outdated'
    ? 'pages.appPreview.novnc.clipboard.runtimeOutdated'
    : 'pages.appPreview.novnc.clipboard.failed')
}

function inFocusedClipboard(send: () => void) {
  const generation = screenFocusGeneration
  channel?.afterOutgoing(() => {
    if (generation === screenFocusGeneration && clipboardReady.value) send()
  })
}

function watchSandboxCopy() {
  if (!clipboardStateAvailable.value) return
  inFocusedClipboard(() => {
    if (useSandboxClipboard || clipboardWatchId !== null) return
    clipboardWatchId = ++clipboardRequestId
    sendCtrl({ type: 'clipboard-watch', id: clipboardWatchId })
  })
}

function pasteBrowserText(text: string, shift: boolean, shortcut: boolean) {
  inFocusedClipboard(() => {
    if (shortcut && useSandboxClipboard) {
      pasteFromSandboxClipboard(shift)
      return
    }
    const id = clipboardWatchId
    clipboardWatchId = null
    useSandboxClipboard = false
    if (!shortcut || id === null || !channel || !sessionSocketAlive()) {
      sendClipboard(text, true, shift)
      return
    }
    sendCtrl({ type: 'clipboard-check', id })
    clipboardCheck = {
      id, text, shift,
      release: channel.holdOutgoing(),
      timeout: setTimeout(() => finishClipboardCheck(id, false), 2_400),
    }
  })
}

function finishClipboardCheck(id: number, changed: boolean) {
  if (clipboardCheck?.id !== id) return
  const { text, shift, release, timeout } = clipboardCheck
  clipboardCheck = null
  clearTimeout(timeout)
  release(() => {
    if (!clipboardReady.value) return
    useSandboxClipboard = changed
    if (changed) pasteFromSandboxClipboard(shift)
    else sendClipboard(text, true, shift)
  })
}

function forwardScreenModifier(ev: KeyboardEvent, type: 'keydown' | 'keyup', key: PasteModifier, code: string, location: number) {
  forwardingScreenModifier = true
  try {
    ev.target?.dispatchEvent(new KeyboardEvent(type, {
      key, code, location,
      modifierCapsLock: ev.getModifierState('CapsLock'),
      modifierNumLock: ev.getModifierState('NumLock'),
    }))
  } finally {
    forwardingScreenModifier = false
  }
}

function restoreScreenModifiers(ev: KeyboardEvent) {
  for (const [code, modifier] of screenModifiers) {
    if (!modifier.suspended) continue
    if (!ev.getModifierState(modifier.key)) {
      screenModifiers.delete(code)
      continue
    }
    forwardScreenModifier(ev, 'keydown', modifier.key, code, modifier.location)
    modifier.suspended = false
  }
}

function pasteIfBrowserSkippedEvent() {
  if (pendingBrowserPaste === null) return
  const shift = pendingBrowserPaste
  pendingBrowserPaste = null
  // The browser can skip the paste event entirely when its clipboard is empty.
  pasteFromSandboxClipboard(shift)
}

function onScreenPasteKey(ev: KeyboardEvent) {
  if (forwardingScreenModifier || !clipboardReady.value) return
  const { key, code } = ev
  const letter = key.toLowerCase()
  const modifier = key === 'Control' || key === 'Meta' || key === 'Shift' ? key : null
  if (ev.type === 'keyup') {
    if (modifier) screenModifiers.delete(code)
    if (letter === 'v' || modifier === 'Control' || modifier === 'Meta') pasteIfBrowserSkippedEvent()
    return
  }
  if (modifier) {
    if (ev.repeat && screenModifiers.get(code)?.suspended) {
      ev.stopPropagation()
      return
    }
    if (code) screenModifiers.set(code, { key: modifier, location: ev.location, suspended: false })
    return
  }
  pasteIfBrowserSkippedEvent()
  const isLocalPaste = isApplePlatform ? ev.metaKey && !ev.ctrlKey : ev.ctrlKey && !ev.metaKey
  if (letter !== 'v' || !isLocalPaste || ev.isComposing || ev.altKey) {
    restoreScreenModifiers(ev)
    // A copy chord can also be Ctrl+C interrupting a terminal. Observe the
    // clipboard before forwarding it, and check for an actual update on paste.
    if ((letter === 'c' || letter === 'x') && ev.ctrlKey && !ev.metaKey && !ev.altKey && !ev.isComposing) {
      watchSandboxCopy()
    }
    return
  }
  // noVNC cancels keydown on the canvas. Keep the browser's default paste
  // action when obtaining text from the local clipboard.
  ev.stopPropagation()
  // Release forwarded modifiers for the Linux paste chord, including Cmd
  // mapped to Alt. Reapply keys that are still physically held before more input.
  const primaryModifier = ev.metaKey ? 'Meta' : 'Control'
  const pasteModifiers: PasteModifier[] = ev.shiftKey ? [primaryModifier, 'Shift'] : [primaryModifier]
  for (const key of pasteModifiers) {
    for (const [side, location] of [['Left', 1], ['Right', 2]] as const) {
      const code = `${key}${side}`
      const state = screenModifiers.get(code)
      if (state?.suspended) continue
      forwardScreenModifier(ev, 'keyup', key, code, location)
      if (state) state.suspended = true
    }
  }
  if (useSandboxClipboard) {
    ev.preventDefault()
    pasteFromSandboxClipboard(ev.shiftKey)
  } else {
    pendingBrowserPaste = ev.shiftKey
  }
}

function pasteFromSandboxClipboard(shift: boolean) {
  const session = rfb
  if (!session) return
  clipboardError.value = ''
  session.sendKey(0xffe3, 'ControlLeft', true)
  if (shift) session.sendKey(0xffe1, 'ShiftLeft', true)
  session.sendKey(shift ? 0x56 : 0x76, 'KeyV')
  if (shift) session.sendKey(0xffe1, 'ShiftLeft', false)
  session.sendKey(0xffe3, 'ControlLeft', false)
}

function onScreenPaste(ev: ClipboardEvent) {
  if (!clipboardReady.value) return
  ev.preventDefault()
  const pending = pendingBrowserPaste
  pendingBrowserPaste = null
  const text = ev.clipboardData?.getData('text/plain')
  if (text) pasteBrowserText(text, pending ?? false, pending !== null)
  else pasteFromSandboxClipboard(pending ?? false)
}

watch(clipboardReady, (ready) => {
  if (!ready) closeClipboard()
})

/**
 * Tab hide (v-show → display:none) collapses the preview viewport to 0×0. With
 * scaleViewport, noVNC autoscales the canvas to 0px and may not recover when
 * the tab returns. A later growth from one positive size to another must also
 * refit: noVNC skips _updateScale when the canvas host stays content-sized.
 * Observe the viewport (definite box) and reassign scaleViewport whenever that
 * box changes while still positive. g1.1 / g1.2.
 */
let hostResizeObserver: ResizeObserver | null = null
/** Last viewport size we already refit. 0 means collapsed or not yet fitted. */
let lastFitWidth = 0
let lastFitHeight = 0
let restoreRafAttempts = 0
const MAX_RESTORE_RAF_ATTEMPTS = 5

/** Auto retries already started for the current loss; reset once a session is ready. */
const reconnectAttempt = ref(0)
/** An automatic reconnect is scheduled or in flight. */
const reconnecting = ref(false)
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectWhenVisible = false
let heartbeatTimer: ReturnType<typeof setInterval> | null = null

const statusLabel = computed(() =>
  reconnecting.value
    ? t('pages.appPreview.novnc.status.reconnecting')
    : t(`pages.appPreview.novnc.status.${status.value}`),
)
const reconnectingText = computed(() =>
  t('pages.appPreview.novnc.reconnectingAttempt', {
    n: Math.min(reconnectAttempt.value + 1, VNC_RECONNECT_MAX_ATTEMPTS),
    max: VNC_RECONNECT_MAX_ATTEMPTS,
  }),
)

function clearReconnectTimer() {
  if (reconnectTimer != null) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  reconnectWhenVisible = false
}

function scheduleAutoReconnect(reason: string) {
  if (!shouldAutoReconnect(reason) || reconnectAttempt.value >= VNC_RECONNECT_MAX_ATTEMPTS) {
    reconnecting.value = false
    return
  }
  reconnecting.value = true
  clearReconnectTimer()
  if (document.visibilityState !== 'visible') {
    reconnectWhenVisible = true
    return
  }
  reconnectTimer = setTimeout(runAutoReconnect, reconnectDelayMs(reconnectAttempt.value))
}

function runAutoReconnect() {
  reconnectTimer = null
  reconnectAttempt.value++
  reconnect({ auto: true })
}

/** Live session ended: show a localized reason and retry when the policy allows. */
function onConnectionLost(reason: string) {
  if (disposed) return
  clearConnectTimer()
  clearPreviewWarnTimer()
  status.value = 'closed'
  statusMsg.value = t(`pages.appPreview.novnc.closedReason.${closeReasonKey(reason)}`)
  if (rfb) {
    try {
      rfb.disconnect()
    } catch {
      /* ignore */
    }
  }
  scheduleAutoReconnect(reason)
}

function heartbeat() {
  if (status.value === 'live' && document.visibilityState === 'visible') sendCtrl({ type: 'ping' })
}

function clearConnectTimer() {
  if (connectTimer != null) {
    clearTimeout(connectTimer)
    connectTimer = null
  }
}

function viewportBox(): { w: number; h: number } {
  const el = viewportEl.value
  return { w: el?.clientWidth ?? 0, h: el?.clientHeight ?? 0 }
}

function hostHasPositiveSize(): boolean {
  const { w, h } = viewportBox()
  return w > 0 && h > 0
}

/** True when the demux socket is still open (session may still be recoverable). */
function sessionSocketAlive(): boolean {
  return !!channel && channel.readyState === WebSocket.OPEN
}

/**
 * Force noVNC to recompute scale after the host becomes visible again.
 * Prefer in-place refresh; if the socket is already gone, leave error/closed UI
 * (existing reconnect button) rather than a silent black canvas.
 */
function restoreViewport(_reason: string) {
  if (disposed) return
  if (!hostHasPositiveSize()) {
    if (restoreRafAttempts < MAX_RESTORE_RAF_ATTEMPTS) {
      restoreRafAttempts++
      requestAnimationFrame(() => restoreViewport(_reason))
    }
    return
  }
  restoreRafAttempts = 0

  if (status.value === 'error' || status.value === 'closed') return
  if (!rfb || !sessionSocketAlive()) {
    // Socket died while hidden — surface closed UI instead of black canvas.
    if (status.value === 'live') onConnectionLost('disconnect')
    return
  }

  try {
    // Setter always invokes _updateScale even when already true.
    rfb.scaleViewport = true
  } catch {
    /* ignore */
  }
}

function onViewportResize() {
  const { w, h } = viewportBox()
  const positive = w > 0 && h > 0
  if (!positive) {
    // Next positive box, even the same pixel size as before hide, must refit.
    lastFitWidth = 0
    lastFitHeight = 0
    return
  }
  if (w === lastFitWidth && h === lastFitHeight) return
  lastFitWidth = w
  lastFitHeight = h
  restoreViewport('viewport-resize')
}

function onDocumentVisibility() {
  if (document.visibilityState === 'visible') {
    if (reconnectWhenVisible) {
      reconnectWhenVisible = false
      runAutoReconnect()
      return
    }
    restoreViewport('document-visible')
  }
}

function setupHostResizeObserver() {
  if (typeof ResizeObserver === 'undefined') return
  const el = viewportEl.value
  if (!el || hostResizeObserver) return
  const { w, h } = viewportBox()
  if (w > 0 && h > 0) {
    lastFitWidth = w
    lastFitHeight = h
  } else {
    lastFitWidth = 0
    lastFitHeight = 0
  }
  hostResizeObserver = new ResizeObserver(() => onViewportResize())
  hostResizeObserver.observe(el)
}

function teardownHostResizeObserver() {
  hostResizeObserver?.disconnect()
  hostResizeObserver = null
}

function clearPreviewWarnTimer() {
  if (previewWarnTimer != null) {
    clearTimeout(previewWarnTimer)
    previewWarnTimer = null
  }
  previewStuck.value = false
}

function sendCtrl(obj: unknown) {
  if (channel && channel.readyState === WebSocket.OPEN) {
    channel.send(JSON.stringify(obj))
  }
}

/** Held until the desktop is ready, so a link clicked while it connects still opens. */
let pendingOpenUrl = ''
function flushOpenRequest() {
  if (!pendingOpenUrl || status.value !== 'live' || !channel || channel.readyState !== WebSocket.OPEN) return
  sendCtrl({ type: 'navigate', action: 'open', url: pendingOpenUrl })
  pendingOpenUrl = ''
  emit('open-sent')
}
watch(
  () => props.openRequest?.seq,
  () => {
    if (!props.openRequest?.url) return
    pendingOpenUrl = props.openRequest.url
    flushOpenRequest()
  },
  { immediate: true },
)

function fail(m: string) {
  clearConnectTimer()
  clearPreviewWarnTimer()
  status.value = 'error'
  statusMsg.value = m
  if (reconnecting.value) scheduleAutoReconnect('disconnect')
}

function teardown() {
  disposed = true
  rfbConnected.value = false
  clipboardAllowed.value = true
  clipboardStateAvailable.value = false
  closeClipboard()
  takeoverCheck = null
  takeoverPrompt.value = null
  endTakeoverGesture()
  clearConnectTimer()
  clearPreviewWarnTimer()
  fpsCounter.detach()
  if (rfb) {
    try {
      rfb.disconnect()
    } catch {
      /* ignore */
    }
    rfb = null
  }
  if (channel) {
    channel.clearAppHandlers()
    try {
      channel.close()
    } catch {
      /* ignore */
    }
    channel = null
  }
}

function handleCtrlText(data: string) {
  let msg: any
  try {
    msg = JSON.parse(data)
  } catch {
    return
  }
  switch (msg.type) {
    case 'ready':
      clearConnectTimer()
      clearPreviewWarnTimer()
      clipboardAllowed.value = msg.clipboard !== false
      clipboardStateAvailable.value = msg.clipboardState === true
      status.value = 'live'
      reconnectAttempt.value = 0
      reconnecting.value = false
      if (typeof msg.url === 'string' && msg.url) address.value = msg.url
      flushOpenRequest()
      break
    case 'clipboard-result':
      onClipboardResult(msg.id, msg.ok === true, msg.code)
      break
    case 'clipboard-check-result':
      finishClipboardCheck(msg.id, msg.changed === true)
      break
    case 'picked':
      inlineTip.value = null
      picked.value = msg.pick
      if (typeof msg.pick?.url === 'string' && msg.pick.url) {
        address.value = msg.pick.url
      }
      // Server already left inspect mode after one-shot pick.
      clearInspect('picked', { syncRemote: false })
      if (msg.pick) emit('staged-pick', msg.pick)
      break
    case 'not-ready':
      // S2: refuse sticky inspect; show Demo intercept copy near toolbar.
      clearInspect('not-ready', { syncRemote: false })
      inlineTip.value = {
        text: t('pages.appPreview.novnc.windowNotReady'),
        err: false,
      }
      break
    case 'describe-failed':
      // S3: recognition failed — tip + clear sticky; no pick result.
      clearInspect('describe-failed', { syncRemote: false })
      inlineTip.value = {
        text: t('pages.appPreview.novnc.describeFailed'),
        err: true,
      }
      break
    case 'inspect-off-failed':
      inlineTip.value = {
        text: t('pages.appPreview.novnc.inspectOffFailed'),
        err: true,
      }
      break
    case 'inspect-canceled':
      // Remote Esc (Overlay.inspectModeCanceled) — keep staged pick; no failure tip.
      // Chromium does not leave searchForNode on this event; echo on:false so
      // overlay actually turns off. Otherwise the button looks idle while
      // inspect stays armed and the next click only re-enters.
      inlineTip.value = null
      clearInspect('remote-esc', { syncRemote: true })
      break
    case 'closed':
      onConnectionLost(typeof msg.reason === 'string' ? msg.reason : '')
      break
    case 'error':
      fail(
        msg.message ||
          (consoleMode.value
            ? t('pages.sandboxConsole.novncUnavailable')
            : t('pages.appPreview.novnc.error')),
      )
      break
  }
}

/**
 * Single exit for inspect mode (toolbar off / Esc / pick / remote cancel).
 * Never clears staged pick — Esc must keep temporarily picked annotations.
 */
function clearInspect(_reason: string, opts?: { syncRemote?: boolean }) {
  const syncRemote = opts?.syncRemote !== false
  if (!inspect.value) {
    if (syncRemote) sendCtrl({ type: 'inspect', on: false })
    return
  }
  inspect.value = false
  if (syncRemote) sendCtrl({ type: 'inspect', on: false })
}

function setInspect(on: boolean) {
  if (on) {
    inlineTip.value = null
    inspect.value = true
    sendCtrl({ type: 'inspect', on: true })
    return
  }
  inlineTip.value = null
  clearInspect('setInspect')
}

function resolveWsUrl(): string | null {
  const custom = (props.wsUrl || '').trim()
  if (custom) return custom
  if (props.sandboxId != null && props.sandboxId > 0) return api.sandboxVncWsUrl(props.sandboxId)
  return null
}

function connect() {
  teardown()
  disposed = false
  fpsCounter.reset()
  status.value = 'connecting'
  statusMsg.value = ''
  picked.value = null
  inspect.value = false
  control.value = false
  inlineTip.value = null
  if (consoleMode.value) address.value = 'about:blank'

  const host = canvasHost.value
  if (!host) return

  const wsUrl = resolveWsUrl()
  if (!wsUrl) {
    fail(
      consoleMode.value
        ? t('pages.sandboxConsole.novncUnavailable')
        : t('pages.appPreview.novnc.error'),
    )
    return
  }

  let ws: WebSocket
  try {
    ws = new WebSocket(wsUrl)
  } catch {
    fail(
      consoleMode.value
        ? t('pages.sandboxConsole.novncUnavailable')
        : t('pages.appPreview.novnc.error'),
    )
    return
  }

  channel = new PreviewVncChannel(ws)
  channel.setCtrlHandler(handleCtrlText)
  channel.setAppErrorHandler(() => {
    if (status.value === 'connecting') {
      fail(
        consoleMode.value
          ? t('pages.sandboxConsole.novncUnavailable')
          : t('pages.appPreview.novnc.error'),
      )
    }
  })
  channel.setAppCloseHandler(() => {
    if (disposed) return
    if (status.value === 'connecting') {
      fail(
        consoleMode.value
          ? t('pages.sandboxConsole.novncUnavailable')
          : t('pages.appPreview.novnc.error'),
      )
    } else if (status.value === 'live') {
      onConnectionLost('disconnect')
    }
  })

  if (consoleMode.value) {
    clearConnectTimer()
    connectTimer = setTimeout(() => {
      if (disposed || status.value !== 'connecting') return
      fail(t('pages.sandboxConsole.novncUnavailable'))
      try {
        channel?.close()
      } catch {
        /* ignore */
      }
    }, CONSOLE_CONNECT_TIMEOUT_MS)
  } else {
    clearPreviewWarnTimer()
    previewWarnTimer = setTimeout(() => {
      if (disposed || status.value !== 'connecting') return
      previewStuck.value = true
    }, PREVIEW_STUCK_MS)
  }

  // Demux: text JSON → Vue control; binary → noVNC RFB (same physical WebSocket).
  try {
    rfb = new RFB(host, channel)
    rfb.scaleViewport = true
    rfb.resizeSession = false
    rfb.viewOnly = !inputEnabled.value
    rfb.focusOnClick = true
    // Xvfb often has no cursor theme → remote cursor is fully transparent; show a
    // local fallback dot so the viewer always has a pointer.
    rfb.showDotCursor = true
    rfb.background = desktopShell.value ? 'transparent' : SCREEN_BACKGROUND

    rfb.addEventListener('connect', () => {
      rfbConnected.value = true
      clearConnectTimer()
      if (status.value === 'connecting') status.value = 'live'
      fpsCounter.attach(rfb!)
      // Layout may have settled before the socket opened; fit the current pane.
      restoreViewport('connected')
    })
    rfb.addEventListener('disconnect', () => {
      rfbConnected.value = false
      if (disposed || status.value === 'error' || status.value === 'closed') return
      if (status.value === 'live') onConnectionLost('disconnect')
      else status.value = 'closed'
    })
    rfb.addEventListener('securityfailure', () => {
      fail(
        consoleMode.value
          ? t('pages.sandboxConsole.novncUnavailable')
          : t('pages.appPreview.novnc.error'),
      )
    })
  } catch {
    fail(
      consoleMode.value
        ? t('pages.sandboxConsole.novncUnavailable')
        : t('pages.appPreview.novnc.error'),
    )
  }
}

function reconnect(opts?: { auto?: boolean }) {
  clearReconnectTimer()
  if (!opts?.auto) {
    reconnectAttempt.value = 0
    reconnecting.value = false
  }
  // Short-lived public tickets expire; reusing the first wsUrl fails after TTL.
  // Ask parent (PublicAppPreviewPanel) to exchange a fresh ticket and remount.
  if ((props.wsUrl || '').trim()) {
    emit('reconnect-request')
    return
  }
  connect()
}

function toggleInspect() {
  if (inspect.value) clearInspect('toolbar-toggle')
  else setInspect(true)
}

function clearPick() {
  picked.value = null
  emit('staged-pick', null)
  if (inspect.value) clearInspect('clearPick')
}

function nav(action: 'reload' | 'back' | 'forward') {
  sendCtrl({ type: 'navigate', action })
}

function normalizeAddress(raw: string): string {
  const u = raw.trim() || 'about:blank'
  if (u === 'about:blank') return u
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u)) return u
  return `http://${u}`
}

function openAddress() {
  const url = normalizeAddress(address.value)
  address.value = url
  sendCtrl({ type: 'navigate', action: 'goto', url })
}

function urlPort(raw: string): number | null {
  try {
    const u = new URL(raw)
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return null
    return Number(u.port || (u.protocol === 'https:' ? 443 : 80))
  } catch {
    return null
  }
}

/**
 * Click-to-take-over drops the arming gesture before it reaches noVNC.
 * noVNC 1.5 listens on its canvas for mousedown, mouseup, mousemove, click,
 * and contextmenu, and for touchstart/move/end/cancel via GestureHandler.
 * viewOnly turns off once control becomes true, so the rest of this press
 * would otherwise go out without the matching down.
 */
type TakeoverGesture = { pointer: 'mouse' | 'touch'; sawUp: boolean }

let takeoverGesture: TakeoverGesture | null = null
let takeoverGestureTimer: ReturnType<typeof setTimeout> | null = null

const TAKEOVER_WINDOW_EVENTS = [
  'mousedown',
  'mousemove',
  'mouseup',
  'click',
  'auxclick',
  'dblclick',
  'contextmenu',
  'dragstart',
  'touchstart',
  'touchmove',
  'touchend',
  'touchcancel',
] as const

function clearTakeoverGestureTimer() {
  if (takeoverGestureTimer != null) {
    clearTimeout(takeoverGestureTimer)
    takeoverGestureTimer = null
  }
}

function endTakeoverGesture() {
  clearTakeoverGestureTimer()
  if (!takeoverGesture) return
  takeoverGesture = null
  for (const type of TAKEOVER_WINDOW_EVENTS) {
    window.removeEventListener(type, onTakeoverGestureTail, true)
  }
}

function scheduleTakeoverGestureEnd(gesture: TakeoverGesture) {
  clearTakeoverGestureTimer()
  // click, contextmenu, and touch-synthesized mouse events follow pointerup
  // in the same turn, before the next task.
  takeoverGestureTimer = setTimeout(() => {
    if (takeoverGesture === gesture) endTakeoverGesture()
  }, 0)
}

function blockGestureEvent(ev: Event) {
  ev.preventDefault()
  ev.stopPropagation()
}

function focusRemoteScreen() {
  const session = rfb as { focus?: (options?: FocusOptions) => void } | null
  try {
    session?.focus?.()
  } catch {
    /* ignore */
  }
}

/** Live, watch-only, and not picking — the only moment a press should arm control. */
function canArmTakeoverFromScreen(): boolean {
  return status.value === 'live' && !control.value && !inspect.value
}

function swallowTakeoverGesture(pointer: 'mouse' | 'touch') {
  endTakeoverGesture()
  const gesture: TakeoverGesture = { pointer, sawUp: false }
  takeoverGesture = gesture
  for (const type of TAKEOVER_WINDOW_EVENTS) {
    window.addEventListener(type, onTakeoverGestureTail, { capture: true, passive: false })
  }
}

function onScreenPointerDown(ev: Event) {
  if (!canArmTakeoverFromScreen()) return
  if (ev.type !== 'mousedown' && ev.type !== 'touchstart') return
  blockGestureEvent(ev)
  swallowTakeoverGesture(ev.type === 'touchstart' ? 'touch' : 'mouse')
  void requestTakeover(true)
}

function isTrailingMouseEvent(type: string): boolean {
  return (
    type === 'mousemove' ||
    type === 'mouseup' ||
    type === 'click' ||
    type === 'auxclick' ||
    type === 'dblclick' ||
    type === 'contextmenu' ||
    type === 'dragstart'
  )
}

function onTakeoverGestureTail(ev: Event) {
  const gesture = takeoverGesture
  if (!gesture) return

  // The next real press is a new gesture and must reach the desktop.
  // A mousedown synthesized from the arming tap is not that press.
  const nextRealPress =
    (gesture.pointer === 'mouse' && ev.type === 'mousedown') ||
    (gesture.pointer === 'touch' && ev.type === 'touchstart')
  if (nextRealPress) {
    if (gesture.sawUp) {
      endTakeoverGesture()
      return
    }
    blockGestureEvent(ev)
    return
  }

  if (gesture.pointer === 'touch' && (ev.type === 'mousedown' || isTrailingMouseEvent(ev.type))) {
    blockGestureEvent(ev)
    return
  }

  if (gesture.pointer === 'mouse' && isTrailingMouseEvent(ev.type)) {
    blockGestureEvent(ev)
    if (ev.type === 'mouseup') {
      gesture.sawUp = true
      scheduleTakeoverGestureEnd(gesture)
    }
    return
  }

  if (
    gesture.pointer === 'touch' &&
    (ev.type === 'touchmove' || ev.type === 'touchend' || ev.type === 'touchcancel')
  ) {
    blockGestureEvent(ev)
    if (ev.type === 'touchend' || ev.type === 'touchcancel') {
      gesture.sawUp = true
      scheduleTakeoverGestureEnd(gesture)
    }
  }
}

function bindScreenTakeoverListeners() {
  const host = canvasHost.value
  if (!host) return
  host.addEventListener('mousedown', onScreenPointerDown, { capture: true, passive: false })
  host.addEventListener('touchstart', onScreenPointerDown, { capture: true, passive: false })
}

function unbindScreenTakeoverListeners() {
  endTakeoverGesture()
  const host = canvasHost.value
  if (!host) return
  host.removeEventListener('mousedown', onScreenPointerDown, true)
  host.removeEventListener('touchstart', onScreenPointerDown, true)
}

function toggleControl() {
  if (control.value) control.value = false
  else void requestTakeover(false)
}

async function requestTakeover(focusScreen: boolean) {
  if (status.value !== 'live' || takeoverCheck || takeoverPrompt.value) return
  if (!props.wsUrl && props.sandboxId && (props.computer || desktop.status.value?.mode !== 'browser')) {
    const request = Symbol()
    takeoverCheck = request
    const current = await desktop.refresh()
    if (takeoverCheck !== request) return
    takeoverCheck = null
    if (status.value !== 'live' || control.value) return
    if (current?.agentActive || (!current && (props.computer || desktop.isDesktop.value))) {
      takeoverPrompt.value = current ? 'busy' : 'unknown'
      return
    }
  }
  control.value = true
  if (focusScreen) focusRemoteScreen()
}

async function confirmTakeover() {
  takeoverPrompt.value = null
  if (status.value !== 'live' || control.value) return
  control.value = true
  await nextTick()
  focusRemoteScreen()
}

function cancelTakeover() {
  takeoverPrompt.value = null
}

watch(desktopShell, (on) => {
  if (!rfb) return
  try {
    rfb.background = on ? 'transparent' : SCREEN_BACKGROUND
  } catch {
    /* ignore */
  }
})

watch(inputEnabled, (on) => {
  if (!rfb) return
  try {
    rfb.viewOnly = !on
  } catch {
    /* ignore */
  }
})

function gotoTargetPort() {
  const port = props.targetPort
  if (!port || urlPort(address.value) === port) return
  address.value = `http://127.0.0.1:${port}/`
  sendCtrl({ type: 'navigate', action: 'goto', url: address.value })
}

function usePick() {
  if (picked.value) emit('pick', picked.value)
}

function onFsChange() {
  isFullscreen.value = !!document.fullscreenElement && document.fullscreenElement === rootEl.value
}

function onKeydown(ev: KeyboardEvent) {
  if (ev.key !== 'Escape' || clipboardOpen.value) return
  // Inspect Esc: exit mode only (keep staged). Non-inspect Esc clears staged pick.
  if (inspect.value) {
    ev.preventDefault()
    inlineTip.value = null
    clearInspect('host-esc')
    return
  }
  if (picked.value) {
    ev.preventDefault()
    clearPick()
  }
}

async function toggleFullscreen() {
  try {
    if (document.fullscreenElement) await document.exitFullscreen()
    else await rootEl.value?.requestFullscreen()
  } catch {
    /* ignore */
  }
}

watch(
  () => [props.sandboxId, props.wsUrl],
  () => reconnect(),
)

watch(
  () => props.targetPort,
  (next, prev) => {
    if (!next || next === prev) return
    if (picked.value) clearPick()
    else if (inspect.value) clearInspect('target-port')
    if (status.value === 'live') gotoTargetPort()
  },
)

onMounted(() => {
  document.addEventListener('fullscreenchange', onFsChange)
  document.addEventListener('visibilitychange', onDocumentVisibility)
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('blur', resetScreenPaste)
  heartbeatTimer = setInterval(heartbeat, VNC_HEARTBEAT_MS)
  connect()
  bindScreenTakeoverListeners()
  // Observe the preview viewport (definite box), not the canvas content size.
  setupHostResizeObserver()
})

onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', onFsChange)
  document.removeEventListener('visibilitychange', onDocumentVisibility)
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('blur', resetScreenPaste)
  unbindScreenTakeoverListeners()
  teardownHostResizeObserver()
  if (heartbeatTimer != null) clearInterval(heartbeatTimer)
  heartbeatTimer = null
  clearReconnectTimer()
  reconnecting.value = false
  teardown()
})
</script>

<template>
  <div
    ref="rootEl"
    class="flex min-h-0 flex-col bg-surface"
    :class="fill ? 'h-full flex-1' : ''"
    :aria-busy="status === 'connecting' ? 'true' : 'false'"
  >
    <!-- Preview toolbar: back/forward/reload + Pick + fullscreen + FPS -->
    <div
      v-if="!consoleMode && !desktopShell"
      class="relative z-20 flex shrink-0 flex-wrap items-center gap-2 border-b border-line bg-elevated px-3 py-1.5"
    >
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.back')"
        @click="nav('back')"
      >
        <Icon name="arrow-left" :size="13" />
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.forward')"
        @click="nav('forward')"
      >
        <Icon name="arrow-right" :size="13" />
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.reload')"
        @click="nav('reload')"
      >
        <Icon name="refresh" :size="13" />
      </button>
      <button
        type="button"
        class="rounded px-2 py-0.5 text-[11px] transition"
        :class="inspect ? 'bg-ok/20 text-ok' : 'text-txt2 hover:bg-overlay'"
        :title="inspectToggleLabel"
        :aria-pressed="inspect ? 'true' : 'false'"
        data-testid="novnc-inspect-toggle"
        @click="toggleInspect"
      >
        {{ inspectToggleLabel }}
      </button>
      <button
        type="button"
        class="rounded px-2 py-0.5 text-[11px] transition disabled:opacity-50"
        :class="control ? 'bg-accent/20 text-accent' : 'text-txt2 hover:bg-overlay'"
        :title="controlToggleLabel"
        :aria-pressed="control ? 'true' : 'false'"
        :disabled="status !== 'live'"
        data-testid="novnc-control-toggle"
        @click="toggleControl"
      >
        {{ controlToggleLabel }}
      </button>
      <button
        v-if="clipboardVisible"
        type="button"
        class="rounded px-1.5 py-0.5 text-txt2 hover:bg-overlay disabled:opacity-40"
        :title="clipboardLabel"
        :aria-label="t('pages.appPreview.novnc.clipboard.title')"
        :disabled="!clipboardReady"
        data-testid="novnc-clipboard-toggle"
        @click="openClipboard"
      >
        <Icon name="copy" :size="14" />
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t(isFullscreen ? 'pages.appPreview.novnc.exitFullscreen' : 'pages.appPreview.novnc.fullscreen')"
        @click="toggleFullscreen"
      >
        {{ isFullscreen ? '⤢' : '⛶' }}
      </button>
      <button
        v-if="audio.available.value"
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] transition"
        :class="audioOn ? 'bg-accent/20 text-accent' : audio.state.value === 'error' ? 'text-warn hover:bg-overlay' : 'text-txt2 hover:bg-overlay'"
        :title="audioLabel"
        :aria-label="audioLabel"
        :aria-pressed="audioOn ? 'true' : 'false'"
        data-testid="novnc-audio-toggle"
        @click="audio.toggle"
      >
        <Icon :name="audioOn ? 'volume' : 'volume-off'" :size="13" :busy="audio.state.value === 'connecting'" />
      </button>
      <slot name="toolbar-extra" />
      <span class="ml-auto flex items-center gap-2.5 text-[10px]">
        <span
          v-if="status === 'live'"
          class="group relative cursor-default tabular-nums"
          :class="fpsCounter.hasRecentFrames.value ? 'text-txt2' : 'text-txt3'"
        >
          <template v-if="fpsCounter.hasRecentFrames.value">
            <span class="font-medium">{{ fpsCounter.fps.value }}</span>
            {{ t('pages.appPreview.novnc.fps') }}
          </template>
          <template v-else>
            <span class="font-medium">—</span>
            {{ t('pages.appPreview.novnc.fps') }}
          </template>
          <span
            class="rounded-md pointer-events-none absolute right-0 top-full z-30 mt-2 hidden w-[220px] border border-line-strong bg-overlay px-2.5 py-2 text-[10px] leading-snug text-txt2 shadow-card group-hover:block"
          >
            {{ t('pages.appPreview.novnc.fpsTooltip') }}
          </span>
        </span>
        <span class="flex items-center gap-1.5">
          <span
            class="inline-block h-1.5 w-1.5 rounded-full"
            :class="{
              'bg-ok': status === 'live',
              'bg-warn animate-pulse': status === 'connecting' || reconnecting,
              'bg-err': !reconnecting && (status === 'error' || status === 'closed'),
            }"
          />
          <span class="text-txt3" data-testid="novnc-status">{{ statusLabel }}</span>
        </span>
      </span>
      <div
        v-if="inlineTip"
        class="rounded-md basis-full border px-2.5 py-1.5 text-[11px] leading-snug"
        :class="
          inlineTip.err
            ? 'border-err/40 bg-err/10 text-err'
            : 'border-warn/40 bg-warn/10 text-warn'
        "
        role="status"
        data-testid="novnc-inline-tip"
      >
        {{ inlineTip.text }}
      </div>
    </div>

    <!-- Console toolbar: back/forward/reload + address + Open (no Pick/FPS) -->
    <form
      v-else-if="!desktopShell"
      class="flex shrink-0 items-center gap-2 border-b border-line bg-elevated px-3 py-1.5"
      @submit.prevent="openAddress"
    >
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.back')"
        @click="nav('back')"
      >
        <Icon name="arrow-left" :size="13" />
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.forward')"
        @click="nav('forward')"
      >
        <Icon name="arrow-right" :size="13" />
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.reload')"
        @click="nav('reload')"
      >
        <Icon name="refresh" :size="13" />
      </button>
      <input
        v-model="address"
        type="text"
        class="min-w-0 flex-1 rounded border border-line bg-base px-2 py-1 font-mono text-[11px] text-txt outline-none focus:border-accent"
        :placeholder="t('pages.sandboxConsole.novncAddressPlaceholder')"
        spellcheck="false"
        autocomplete="off"
      />
      <button
        type="submit"
        class="shrink-0 rounded bg-accent-dim px-2.5 py-1 text-[11px] text-txt hover:bg-accent/25"
      >
        {{ t('pages.sandboxConsole.novncOpen') }}
      </button>
      <button
        type="button"
        class="shrink-0 rounded px-2 py-0.5 text-[11px] transition disabled:opacity-50"
        :class="control ? 'bg-accent/20 text-accent' : 'text-txt2 hover:bg-overlay'"
        :title="controlToggleLabel"
        :aria-pressed="control ? 'true' : 'false'"
        :disabled="status !== 'live'"
        data-testid="novnc-control-toggle"
        @click="toggleControl"
      >
        {{ controlToggleLabel }}
      </button>
      <button
        v-if="clipboardVisible"
        type="button"
        class="shrink-0 rounded px-1.5 py-0.5 text-txt2 hover:bg-overlay disabled:opacity-40"
        :title="clipboardLabel"
        :aria-label="t('pages.appPreview.novnc.clipboard.title')"
        :disabled="!clipboardReady"
        data-testid="novnc-clipboard-toggle"
        @click="openClipboard"
      >
        <Icon name="copy" :size="14" />
      </button>
      <button
        v-if="audio.available.value"
        type="button"
        class="shrink-0 rounded px-1.5 py-0.5 text-[11px] transition"
        :class="audioOn ? 'bg-accent/20 text-accent' : audio.state.value === 'error' ? 'text-warn hover:bg-overlay' : 'text-txt2 hover:bg-overlay'"
        :title="audioLabel"
        :aria-label="audioLabel"
        :aria-pressed="audioOn ? 'true' : 'false'"
        data-testid="novnc-audio-toggle"
        @click="audio.toggle"
      >
        <Icon :name="audioOn ? 'volume' : 'volume-off'" :size="13" :busy="audio.state.value === 'connecting'" />
      </button>
      <button
        v-if="inspectable"
        type="button"
        class="shrink-0 rounded px-2 py-0.5 text-[11px] transition"
        :class="inspect ? 'bg-ok/20 text-ok' : 'text-txt2 hover:bg-overlay'"
        :title="inspectToggleLabel"
        :aria-pressed="inspect ? 'true' : 'false'"
        data-testid="novnc-inspect-toggle"
        @click="toggleInspect"
      >
        {{ inspectToggleLabel }}
      </button>
    </form>

    <div
      v-if="!consoleMode && (status === 'error' || status === 'closed')"
      class="flex shrink-0 items-center gap-3 border-b border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn"
    >
      <span class="min-w-0 flex-1 truncate" data-testid="novnc-closed-msg">
        {{ statusMsg || t('pages.appPreview.novnc.error') }}
        <template v-if="reconnecting"> · {{ reconnectingText }}</template>
      </span>
      <button
        v-if="!reconnecting"
        type="button"
        class="rounded bg-overlay px-2 py-0.5 text-txt hover:bg-overlay/80"
        data-testid="novnc-reconnect"
        @click="reconnect()"
      >
        {{ t('pages.appPreview.novnc.reconnect') }}
      </button>
    </div>

    <div
      ref="viewportEl"
      class="novnc-fs-viewport relative min-h-0 overflow-hidden bg-base"
      :class="[
        fill ? 'flex-1' : compact ? 'h-[280px]' : 'h-[420px]',
        showInspect && inspect ? 'cursor-crosshair' : '',
        desktopShell ? 'is-desktop desktop-surface' : '',
        desktopShell && control ? 'is-control' : '',
      ]"
    >
      <!-- Console connecting / unavailable overlays (panel-local, no toast loop) -->
      <div
        v-if="computerLoading"
        class="desktop-surface absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 px-6 text-center"
        role="status"
        aria-live="polite"
        aria-busy="true"
        data-testid="novnc-computer-loading"
      >
        <span
          class="h-6 w-6 animate-spin rounded-full border-2 border-line-strong border-t-txt2"
          aria-hidden="true"
        />
        <span class="text-sm text-txt3">{{ t('pages.appPreview.desktop.starting') }}</span>
      </div>
      <div
        v-else-if="consoleMode && status === 'connecting'"
        class="absolute inset-0 z-10 flex items-center justify-center bg-base/90 px-6 text-center text-sm text-txt3"
        role="status"
        aria-live="polite"
        aria-busy="true"
      >
        {{ t('pages.sandboxConsole.novncConnecting') }}
      </div>
      <div
        v-else-if="consoleMode && (status === 'error' || status === 'closed')"
        class="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-base px-6 text-center"
      >
        <div class="text-sm font-medium text-txt">{{ t('pages.sandboxConsole.novncUnavailable') }}</div>
        <div class="max-w-md text-sm text-txt3">
          {{ t('pages.sandboxConsole.novncUnavailableHint') }}
        </div>
        <div
          v-if="statusMsg && statusMsg !== t('pages.sandboxConsole.novncUnavailable')"
          class="max-w-md truncate text-[11px] text-txt3/80"
        >
          {{ statusMsg }}
        </div>
        <div v-if="reconnecting" class="text-[11px] text-warn">{{ reconnectingText }}</div>
        <button
          v-else
          type="button"
          class="rounded bg-overlay px-3 py-1.5 text-xs text-txt hover:bg-overlay/80"
          @click="reconnect()"
        >
          {{ t('pages.appPreview.novnc.reconnect') }}
        </button>
      </div>
      <div
        ref="canvasHost"
        class="novnc-canvas-host absolute"
        :class="desktopShell ? 'inset-x-5 top-4 bottom-3 max-md:inset-x-2 max-md:top-2 max-md:bottom-2' : 'inset-0 h-full w-full'"
        @keydown.capture="onScreenPasteKey"
        @keyup.capture="onScreenPasteKey"
        @paste.capture="onScreenPaste"
        @focusout="resetScreenPaste"
      />
      <p
        v-if="clipboardError && !clipboardOpen"
        class="pointer-events-none absolute bottom-3 right-3 z-10 max-w-[calc(100%-1.5rem)] rounded-md border border-err/40 bg-elevated px-3 py-2 text-xs text-err shadow-card"
        role="status"
      >{{ clipboardError }}</p>
      <template v-if="status === 'live' && !desktopShell">
        <div
          v-if="control"
          class="pointer-events-none absolute inset-0 z-10 ring-2 ring-inset ring-accent"
          data-testid="novnc-control-frame"
        >
          <span
            class="absolute left-1/2 top-2 -translate-x-1/2 rounded-full bg-accent px-3 py-1 text-[11px] font-medium text-white shadow-card"
          >
            {{ t('pages.appPreview.novnc.controlBanner') }}
          </span>
        </div>
        <span
          v-else-if="!inspect"
          class="pointer-events-none absolute bottom-2 left-2 z-10 rounded-md border border-line-strong bg-overlay/95 px-2 py-1 text-[11px] text-txt2 shadow-card"
          data-testid="novnc-watch-hint"
        >
          {{ t('pages.appPreview.novnc.watchOnlyHint') }}
        </span>
      </template>

      <div
        v-if="!consoleMode && status === 'connecting'"
        class="absolute inset-0 flex flex-col items-center justify-center gap-3 px-6 text-center"
        :class="previewStuck ? '' : 'pointer-events-none'"
        role="status"
        aria-live="polite"
        aria-busy="true"
        data-testid="novnc-connecting"
      >
        <span
          class="h-6 w-6 animate-spin rounded-full border-2 border-line-strong border-t-txt2"
          aria-hidden="true"
        />
        <span class="text-sm font-medium text-txt2">{{ t('pages.appPreview.novnc.connectingTitle') }}</span>
        <span
          v-if="!previewStuck"
          class="max-w-[320px] text-xs leading-snug text-txt3"
          data-testid="novnc-connecting-hint"
        >{{ t('pages.appPreview.novnc.connectingHint') }}</span>
        <div
          v-if="previewStuck"
          class="rounded-lg pointer-events-auto max-w-[360px] border border-warn/40 bg-warn/10 px-2.5 py-2 text-[12px] text-warn"
          data-testid="novnc-preview-stuck"
        >
          <p>{{ t('pages.appPreview.novnc.maybeStuck') }}</p>
          <button
            type="button"
            class="rounded-lg mt-2 inline-flex min-h-11 items-center border border-line bg-surface px-3 text-[12px] font-medium text-txt"
            @click="reconnect()"
          >
            {{ t('pages.appPreview.novnc.reconnect') }}
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="desktopShell"
      class="desktop-surface flex shrink-0 flex-col items-center gap-2 px-4 pb-3.5 pt-1 max-md:px-2 max-md:pb-2"
      data-testid="desktop-status-bar"
    >
      <div
        v-if="inlineTip"
        class="rounded-md max-w-full border px-2.5 py-1.5 text-[11px] leading-snug"
        :class="inlineTip.err ? 'border-err/40 bg-err/10 text-err' : 'border-warn/40 bg-warn/10 text-warn'"
        role="status"
        data-testid="novnc-inline-tip"
      >
        {{ inlineTip.text }}
      </div>
      <div
        class="desktop-dock inline-flex max-w-full items-center gap-2 rounded-full border py-1 pl-3.5 pr-1 text-[12px]"
        :title="desktopTakeoverText || undefined"
      >
        <span class="relative flex h-2 w-2 shrink-0" aria-hidden="true">
          <span
            v-if="!control && desktop.agentBusy.value"
            class="absolute inline-flex h-full w-full animate-ping rounded-full bg-ok opacity-60"
          />
          <span
            class="relative inline-flex h-2 w-2 rounded-full"
            :class="control ? 'bg-accent' : desktop.agentBusy.value ? 'bg-ok' : 'bg-txt3'"
          />
        </span>
        <span class="truncate font-medium text-txt" data-testid="desktop-holder">{{ desktopHolderLabel }}</span>
        <span class="text-txt3" aria-hidden="true">·</span>
        <span class="shrink-0 text-txt3" data-testid="desktop-agent">{{
          t(desktop.agentBusy.value ? 'pages.appPreview.desktop.agentBusy' : 'pages.appPreview.desktop.agentIdle')
        }}</span>
        <span
          v-if="desktop.status.value && !desktop.status.value.shellReady"
          class="inline-flex shrink-0 items-center gap-1 text-txt3"
          data-testid="desktop-starting"
        >
          <Icon name="spinner" :size="12" :interactive="false" />{{ t('pages.appPreview.desktop.starting') }}
        </span>
        <span v-if="control && desktop.agentBusy.value" class="sr-only" data-testid="desktop-takeover-tip">
          {{ desktopTakeoverText }}
        </span>
        <span v-else-if="desktop.agentBusy.value" class="sr-only" data-testid="desktop-takeover-hint">
          {{ desktopTakeoverText }}
        </span>
        <span class="ml-1 flex shrink-0 items-center gap-0.5">
          <button
            v-if="showInspect"
            type="button"
            class="inline-flex h-7 w-7 items-center justify-center rounded-full transition"
            :class="inspect ? 'bg-ok/15 text-ok' : 'text-txt3 hover:bg-overlay hover:text-txt'"
            :title="inspectToggleLabel"
            :aria-label="inspectToggleLabel"
            :aria-pressed="inspect ? 'true' : 'false'"
            data-testid="novnc-inspect-toggle"
            @click="toggleInspect"
          >
            <Icon name="crosshair" :size="14" />
          </button>
          <slot name="toolbar-extra" />
          <button
            v-if="clipboardVisible"
            type="button"
            class="inline-flex h-7 w-7 items-center justify-center rounded-full text-txt3 transition hover:bg-overlay hover:text-txt disabled:opacity-40"
            :title="clipboardLabel"
            :aria-label="t('pages.appPreview.novnc.clipboard.title')"
            :disabled="!clipboardReady"
            data-testid="novnc-clipboard-toggle"
            @click="openClipboard"
          >
            <Icon name="copy" :size="14" />
          </button>
          <button
            v-if="audio.available.value"
            type="button"
            class="inline-flex h-7 w-7 items-center justify-center rounded-full transition"
            :class="audioOn ? 'bg-accent/15 text-accent' : audio.state.value === 'error' ? 'text-warn hover:bg-overlay' : 'text-txt3 hover:bg-overlay hover:text-txt'"
            :title="audioLabel"
            :aria-label="audioLabel"
            :aria-pressed="audioOn ? 'true' : 'false'"
            data-testid="novnc-audio-toggle"
            @click="audio.toggle"
          >
            <Icon :name="audioOn ? 'volume' : 'volume-off'" :size="14" :busy="audio.state.value === 'connecting'" />
          </button>
          <button
            type="button"
            class="inline-flex h-7 w-7 items-center justify-center rounded-full text-txt3 transition hover:bg-overlay hover:text-txt"
            :title="t(isFullscreen ? 'pages.appPreview.novnc.exitFullscreen' : 'pages.appPreview.novnc.fullscreen')"
            :aria-label="t(isFullscreen ? 'pages.appPreview.novnc.exitFullscreen' : 'pages.appPreview.novnc.fullscreen')"
            @click="toggleFullscreen"
          >
            <Icon name="expand" :size="14" />
          </button>
          <button
            type="button"
            class="ml-0.5 inline-flex h-7 items-center rounded-full px-3.5 text-[12px] font-medium transition disabled:cursor-not-allowed disabled:opacity-40"
            :class="control ? 'bg-accent text-white hover:bg-accent-hover' : 'bg-txt text-surface hover:bg-txt/85'"
            :title="controlToggleLabel"
            :aria-pressed="control ? 'true' : 'false'"
            :disabled="status !== 'live'"
            data-testid="novnc-control-toggle"
            @click="toggleControl"
          >{{ controlToggleLabel }}</button>
        </span>
      </div>
    </div>

    <div
      v-if="showInspect && picked"
      class="shrink-0 border-t border-line bg-elevated px-3 py-2"
      data-testid="novnc-pick-result"
    >
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-[11px] text-txt2">{{ t('pages.appPreview.novnc.pickedLabel') }}</span>
        <span class="text-[10px] lowercase text-txt3">{{
          t('pages.appPreview.novnc.selectorLabel')
        }}</span>
        <code class="max-w-full break-all text-[11px] text-ok" data-testid="novnc-pick-selector">{{
          picked.selector
        }}</code>
        <span class="text-[10px] lowercase text-txt3">{{ t('pages.appPreview.novnc.urlLabel') }}</span>
        <code class="max-w-full break-all text-[11px] text-info" data-testid="novnc-pick-url">{{
          picked.url || ''
        }}</code>
        <span class="ml-auto flex shrink-0 items-center gap-2">
          <button
            type="button"
            class="rounded bg-ok/15 px-2 py-1 text-[11px] font-medium text-ok hover:bg-ok/25"
            @click="usePick"
          >
            {{ t('pages.appPreview.novnc.usePick') }}
          </button>
          <button
            type="button"
            class="rounded px-2 py-1 text-[11px] text-txt2 hover:bg-overlay hover:text-txt"
            :title="t('pages.appPreview.novnc.clearPick')"
            @click="clearPick"
          >
            {{ t('pages.appPreview.novnc.clearPick') }}
          </button>
        </span>
      </div>
    </div>
    <AppModal
      :open="clipboardOpen"
      :inline="isFullscreen"
      :title="t('pages.appPreview.novnc.clipboard.title')"
      :width="480"
      :close-label="t('common.buttons.close')"
      close-on-esc
      trap-focus
      @close="closeClipboard"
    >
      <label class="block text-sm text-txt2">
        {{ t('pages.appPreview.novnc.clipboard.inputLabel') }}
        <textarea
          v-model="clipboardDraft"
          rows="5"
          class="mt-2 block w-full rounded-lg border border-line bg-base px-3 py-2 font-mono text-sm text-txt outline-none focus:border-accent"
          :readonly="!!clipboardPending"
          spellcheck="false"
          autocomplete="off"
          autofocus
          data-testid="novnc-clipboard-text"
        />
      </label>
      <label v-if="desktop.isDesktop.value" class="mt-3 flex items-center gap-2 text-sm text-txt2">
        <input v-model="clipboardTerminalPaste" type="checkbox" :disabled="!!clipboardPending" />
        {{ t('pages.appPreview.novnc.clipboard.terminalPaste') }}
      </label>
      <p v-if="clipboardError" class="mt-2 text-xs text-err" role="status">{{ clipboardError }}</p>
      <template #footer>
        <AppButton variant="ghost" :disabled="!clipboardDraft || !!clipboardPending" @click="sendClipboard(clipboardDraft, false)">
          {{ t('pages.appPreview.novnc.clipboard.copyToSandbox') }}
        </AppButton>
        <AppButton variant="primary" :disabled="!clipboardDraft || !!clipboardPending" @click="sendClipboard(clipboardDraft, true, clipboardTerminalPaste)">
          {{ t('pages.appPreview.novnc.clipboard.pasteNow') }}
        </AppButton>
      </template>
    </AppModal>
    <AppModal
      :open="takeoverPrompt !== null"
      :inline="isFullscreen"
      :title="t(takeoverPrompt === 'busy' ? 'pages.appPreview.desktop.takeoverConfirmTitle' : 'pages.appPreview.novnc.takeOver')"
      :width="420"
      :close-label="t('common.buttons.close')"
      close-on-esc
      trap-focus
      @close="cancelTakeover"
    >
      <p class="text-[13px] text-txt2" data-testid="desktop-takeover-confirm">
        {{ t('pages.appPreview.desktop.takeoverConfirmBody') }}
      </p>
      <template #footer>
        <AppButton variant="ghost" data-testid="desktop-takeover-cancel" @click="cancelTakeover">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton variant="primary" autofocus data-testid="desktop-takeover-ok" @click="confirmTakeover">
          {{ t('pages.appPreview.desktop.takeoverConfirmOk') }}
        </AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
/* Inside the artifact stage, follow its --ra-* palette; elsewhere fall back to app tokens. */
.desktop-surface {
  background: var(--ra-bg, rgb(var(--c-base)));
}
.desktop-dock {
  border-color: var(--ra-line, rgb(var(--c-line)));
  background: var(--ra-surface, rgb(var(--c-elevated)));
  box-shadow: 0 1px 2px var(--ra-shadow, rgba(0, 0, 0, 0.08)), 0 8px 24px -12px var(--ra-shadow-deep, rgba(0, 0, 0, 0.35));
}
.novnc-fs-viewport.is-desktop .novnc-canvas-host :deep(canvas) {
  border-radius: 12px;
  box-shadow:
    0 0 0 1px var(--ra-line, rgb(var(--c-line))),
    0 18px 40px -18px rgba(0, 0, 0, 0.45);
  transition: box-shadow 0.15s ease;
}
.novnc-fs-viewport.is-desktop.is-control .novnc-canvas-host :deep(canvas) {
  box-shadow:
    0 0 0 2px #7b61ff,
    0 18px 40px -18px rgba(123, 97, 255, 0.5);
}
/*
 * Chrome/Edge often fail to paint CSS cursor:url(data:…) under Element Fullscreen,
 * while noVNC still sets canvas { cursor: none | url(...) }. Force a system cursor
 * so the pointer stays visible; RFB pointer events are unaffected.
 */
div:fullscreen .novnc-canvas-host,
div:fullscreen .novnc-canvas-host :deep(*) {
  cursor: default !important;
}
div:fullscreen .novnc-fs-viewport.cursor-crosshair .novnc-canvas-host,
div:fullscreen .novnc-fs-viewport.cursor-crosshair .novnc-canvas-host :deep(*) {
  cursor: crosshair !important;
}
</style>

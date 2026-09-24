<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { PreviewVncChannel } from '@/lib/shared/previewVncChannel'
import { createPreviewFpsCounter } from '@/lib/shared/previewFps'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
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
}>()

const props = withDefaults(
  defineProps<{
    /** Preview mode: run/node/port triple (app_preview). */
    runId?: string
    nodeId?: string
    port?: number
    /**
     * Port to show on the sandbox desktop when it differs from `port`. The sandbox
     * keeps one preview tab, so sibling ports share this connection and navigate.
     */
    targetPort?: number
    /** Optional absolute ws(s) URL (public ticket channel); overrides run/node/port. */
    wsUrl?: string
    /** Console mode: sandbox-scoped WS (mutually exclusive with preview triple). */
    sandboxId?: number
    fill?: boolean
    compact?: boolean
    /** Console mode: show 取点标注 (sandbox-vnc already proxies inspect/pick). */
    inspectable?: boolean
  }>(),
  { fill: false, compact: false, inspectable: false },
)

const { t } = useI18n()
const fpsCounter = createPreviewFpsCounter()

const consoleMode = computed(() => props.sandboxId != null && props.sandboxId > 0)
const showInspect = computed(() => !consoleMode.value || props.inspectable)

const canvasHost = ref<HTMLDivElement | null>(null)
const rootEl = ref<HTMLDivElement | null>(null)
const isFullscreen = ref(false)
const status = ref<'connecting' | 'live' | 'closed' | 'error'>('connecting')
const statusMsg = ref('')
const previewStuck = ref(false)
let previewWarnTimer: ReturnType<typeof setTimeout> | null = null
const PREVIEW_STUCK_MS = 20_000
const inspect = ref(false)
const inspectToggleLabel = computed(() =>
  t(inspect.value ? 'pages.appPreview.novnc.cancelInspect' : 'pages.appPreview.novnc.inspect'),
)
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

/**
 * Tab hide (v-show → display:none) collapses the canvas host to 0×0. With
 * scaleViewport, noVNC autoscales the canvas to 0px and may not recover when
 * the tab returns. Mirror SandboxConsole's "size > 0 then fit" pattern.
 */
let hostResizeObserver: ResizeObserver | null = null
let hostWasZeroSize = true
let restoreRafAttempts = 0
const MAX_RESTORE_RAF_ATTEMPTS = 5

function clearConnectTimer() {
  if (connectTimer != null) {
    clearTimeout(connectTimer)
    connectTimer = null
  }
}

function hostHasPositiveSize(): boolean {
  const el = canvasHost.value
  return !!el && el.clientWidth > 0 && el.clientHeight > 0
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
    if (status.value === 'live') status.value = 'closed'
    return
  }

  try {
    // Setter always invokes _updateScale even when already true.
    rfb.scaleViewport = true
  } catch {
    /* ignore */
  }
}

function onHostResize() {
  const positive = hostHasPositiveSize()
  if (positive && hostWasZeroSize) {
    restoreViewport('resize-from-zero')
  }
  hostWasZeroSize = !positive
}

function onDocumentVisibility() {
  if (document.visibilityState === 'visible') {
    restoreViewport('document-visible')
  }
}

function setupHostResizeObserver() {
  if (typeof ResizeObserver === 'undefined') return
  const el = canvasHost.value
  if (!el || hostResizeObserver) return
  hostWasZeroSize = !hostHasPositiveSize()
  hostResizeObserver = new ResizeObserver(() => onHostResize())
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

function fail(m: string) {
  clearConnectTimer()
  clearPreviewWarnTimer()
  status.value = 'error'
  statusMsg.value = m
}

function teardown() {
  disposed = true
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
      status.value = 'live'
      if (typeof msg.url === 'string' && msg.url) address.value = msg.url
      if (props.targetPort && props.targetPort !== props.port) gotoTargetPort()
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
    case 'inspect-canceled':
      // Remote Esc (Overlay.inspectModeCanceled) — keep staged pick; no failure tip.
      // Chromium does not leave searchForNode on this event; echo on:false so
      // overlay actually turns off. Otherwise the button looks idle while
      // inspect stays armed and the next click only re-enters.
      inlineTip.value = null
      clearInspect('remote-esc', { syncRemote: true })
      break
    case 'closed':
      status.value = 'closed'
      statusMsg.value = msg.reason || ''
      if (rfb) {
        try {
          rfb.disconnect()
        } catch {
          /* ignore */
        }
      }
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
  if (consoleMode.value) {
    return api.sandboxVncWsUrl(props.sandboxId!)
  }
  if (!props.runId || !props.nodeId || !props.port) return null
  return api.previewVncWsUrl(props.runId, props.nodeId, props.port)
}

function connect() {
  teardown()
  disposed = false
  fpsCounter.reset()
  status.value = 'connecting'
  statusMsg.value = ''
  picked.value = null
  inspect.value = false
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
    } else if (status.value !== 'error') {
      status.value = 'closed'
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
    rfb.viewOnly = false
    rfb.focusOnClick = true
    // Xvfb often has no cursor theme → remote cursor is fully transparent; show a
    // local fallback dot so the viewer always has a pointer.
    rfb.showDotCursor = true
    rfb.background = 'rgb(10,10,11)'

    rfb.addEventListener('connect', () => {
      clearConnectTimer()
      if (status.value === 'connecting') status.value = 'live'
      fpsCounter.attach(rfb!)
    })
    rfb.addEventListener('disconnect', () => {
      if (disposed || status.value === 'error' || status.value === 'closed') return
      status.value = 'closed'
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

function reconnect() {
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

function gotoTargetPort() {
  const port = props.targetPort || props.port
  if (!port) return
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
  if (ev.key !== 'Escape') return
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
  () => [props.runId, props.nodeId, props.port, props.sandboxId, props.wsUrl],
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
  connect()
  // Observe after connect so canvasHost is in the DOM; keep across reconnects.
  setupHostResizeObserver()
})

onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', onFsChange)
  document.removeEventListener('visibilitychange', onDocumentVisibility)
  window.removeEventListener('keydown', onKeydown)
  teardownHostResizeObserver()
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
      v-if="!consoleMode"
      class="flex shrink-0 flex-wrap items-center gap-2 border-b border-line bg-elevated px-3 py-1.5"
    >
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.back')"
        @click="nav('back')"
      >
        ←
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.forward')"
        @click="nav('forward')"
      >
        →
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.reload')"
        @click="nav('reload')"
      >
        ⟳
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
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t(isFullscreen ? 'pages.appPreview.novnc.exitFullscreen' : 'pages.appPreview.novnc.fullscreen')"
        @click="toggleFullscreen"
      >
        {{ isFullscreen ? '⤢' : '⛶' }}
      </button>
      <span class="ml-auto flex items-center gap-2.5 text-[10px]">
        <span
          v-if="status === 'live'"
          class="group relative cursor-default tabular-nums"
          :class="fpsCounter.hasRecentFrames.value ? 'text-txt2' : 'text-txt3'"
          :title="t('pages.appPreview.novnc.fpsTooltip')"
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
            class="rounded-md pointer-events-none absolute bottom-full right-0 z-20 mb-2 hidden w-[220px] border border-line-strong bg-overlay px-2.5 py-2 text-[10px] leading-snug text-txt2 shadow-card group-hover:block"
          >
            {{ t('pages.appPreview.novnc.fpsTooltip') }}
          </span>
        </span>
        <span class="flex items-center gap-1.5">
          <span
            class="inline-block h-1.5 w-1.5 rounded-full"
            :class="{
              'bg-ok': status === 'live',
              'bg-warn animate-pulse': status === 'connecting',
              'bg-err': status === 'error' || status === 'closed',
            }"
          />
          <span class="text-txt3">{{ t(`pages.appPreview.novnc.status.${status}`) }}</span>
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
      v-else
      class="flex shrink-0 items-center gap-2 border-b border-line bg-elevated px-3 py-1.5"
      @submit.prevent="openAddress"
    >
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.back')"
        @click="nav('back')"
      >
        ←
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.forward')"
        @click="nav('forward')"
      >
        →
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.reload')"
        @click="nav('reload')"
      >
        ⟳
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
      <span class="min-w-0 flex-1 truncate">{{ statusMsg || t('pages.appPreview.novnc.error') }}</span>
      <button type="button" class="rounded bg-overlay px-2 py-0.5 text-txt hover:bg-overlay/80" @click="reconnect">
        {{ t('pages.appPreview.novnc.reconnect') }}
      </button>
    </div>

    <div
      class="novnc-fs-viewport relative flex min-h-0 items-center justify-center overflow-hidden bg-base"
      :class="[
        fill ? 'flex-1' : compact ? 'h-[280px]' : 'h-[420px]',
        showInspect && inspect ? 'cursor-crosshair' : '',
      ]"
    >
      <!-- Console connecting / unavailable overlays (panel-local, no toast loop) -->
      <div
        v-if="consoleMode && status === 'connecting'"
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
        <button
          type="button"
          class="rounded bg-overlay px-3 py-1.5 text-xs text-txt hover:bg-overlay/80"
          @click="reconnect"
        >
          {{ t('pages.appPreview.novnc.reconnect') }}
        </button>
      </div>
      <div ref="canvasHost" class="novnc-canvas-host h-full w-full" />

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
        <span class="max-w-[320px] text-xs leading-snug text-txt3">{{
          t('pages.appPreview.novnc.connectingHint')
        }}</span>
        <div
          v-if="previewStuck"
          class="rounded-lg pointer-events-auto max-w-[360px] border border-warn/40 bg-warn/10 px-2.5 py-2 text-[12px] text-warn"
          data-testid="novnc-preview-stuck"
        >
          <p>{{ t('pages.appPreview.novnc.maybeStuck') }}</p>
          <button
            type="button"
            class="rounded-lg mt-2 inline-flex min-h-11 items-center border border-line bg-surface px-3 text-[12px] font-medium text-txt"
            @click="reconnect"
          >
            {{ t('pages.appPreview.novnc.reconnect') }}
          </button>
        </div>
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
  </div>
</template>

<style scoped>
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

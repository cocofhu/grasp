<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import PageControlStatus from '@/components/run/PageControlStatus.vue'
import PublicGateApprovalView from '@/views/PublicGateApprovalView.vue'
import {
  EMBED_READY_MESSAGE,
  EMBED_SESSION_MESSAGE,
  clearEmbedSession,
  loadEmbedSession,
  parseEmbedCmdResult,
  parseEmbedControlMessage,
  parseEmbedPickMessage,
  parseEmbedThemeFromHash,
  parseEmbedThemeMessage,
  parseEmbedTicketFromHash,
  redeemEmbedTicket,
  saveEmbedSession,
} from '@/lib/inbox/embedChat'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { setThemeOverride } from '@/lib/shared/theme'
import { usePageControl } from '@/lib/inbox/embedPageControl'
import {
  EMBED_LIVE_ACK_MESSAGE,
  EMBED_LIVE_CAPS_MESSAGE,
  EMBED_LIVE_CMD_MESSAGE,
  EMBED_LIVE_SESSIONS_MESSAGE,
  isLiveOpen,
  parseEmbedLiveMessage,
  type LiveCmd,
  type LiveEvent,
  type LiveSession,
  type LiveStore,
  type LiveView,
} from '@/lib/inbox/liveVariants'

type ChatRef = {
  addPick?: (payload: AppPreviewPickPayload) => void
  sendEventsFrame?: (frame: Record<string, unknown>) => boolean
  sendLive?: (ev: LiveEvent) => Promise<LiveSession | null>
  loadLiveSessions?: () => Promise<{ enabled: boolean; sessions: LiveSession[] }>
  discardAllLive?: () => Promise<number>
  setLiveView?: (sid: string, view: LiveView) => void
  liveStore?: LiveStore
}

const { t } = useI18n()
const route = useRoute()
const runId = String(route.params.runId || '')
const nodeId = String(route.params.nodeId || '')

const phase = ref<'connecting' | 'ready' | 'expired' | 'network'>('connecting')
const token = ref('')
const chatRef = ref<ChatRef | null>(null)
let announced = false

const pageControl = usePageControl({
  runId,
  nodeId,
  post: (msg) => {
    if (window.parent !== window) window.parent.postMessage(msg, parentOrigin())
  },
  send: (frame) => chatRef.value?.sendEventsFrame?.(frame) ?? false,
})
const { supported: pageControlSupported, enabled: pageControlOn, state: pageControlState, active: pageControlActive } = pageControl

const liveOpenCount = computed(() => {
  const store = chatRef.value?.liveStore
  if (!store?.enabled) return 0
  return Object.values(store.sessions).filter((s) => isLiveOpen(s.state) && (s.mode !== 'steer' || s.state === 'failed')).length
})
const hasFailedSteer = computed(() => Object.values(chatRef.value?.liveStore?.sessions || {}).some((s) => s.mode === 'steer' && s.state === 'failed'))
const liveDiscarding = ref(false)
const liveNotice = ref('')

function postToPage(msg: Record<string, unknown>) {
  if (window.parent !== window) window.parent.postMessage(msg, parentOrigin())
}

/** Tell the page whether Live is on and what sessions exist (connect / reconnect). */
async function syncLive() {
  const load = chatRef.value?.loadLiveSessions
  if (!load) return
  try {
    const { enabled, sessions } = await load()
    postToPage({ type: EMBED_LIVE_CAPS_MESSAGE, enabled })
    postToPage({ type: EMBED_LIVE_SESSIONS_MESSAGE, replace: true, sessions })
  } catch {
    // Live stays off on the page until the next reconnect.
  }
}

async function forwardLive(reqId: string, ev: LiveEvent) {
  const send = chatRef.value?.sendLive
  if (!send) {
    postToPage({ type: EMBED_LIVE_ACK_MESSAGE, reqId, sid: ev.sid, ok: false, error: 'not ready' })
    return
  }
  try {
    const session = await send(ev)
    postToPage({ type: EMBED_LIVE_ACK_MESSAGE, reqId, sid: ev.sid, ok: true, session })
  } catch (e) {
    const error = e instanceof Error ? e.message : String(e)
    postToPage({ type: EMBED_LIVE_ACK_MESSAGE, reqId, sid: ev.sid, ok: false, error })
  }
}

function onLiveSession(session: LiveSession) {
  postToPage({ type: EMBED_LIVE_SESSIONS_MESSAGE, sessions: [session] })
}

function onLiveCmd(sid: string, cmd: LiveCmd, variant?: number) {
  postToPage({ type: EMBED_LIVE_CMD_MESSAGE, sid, cmd, variant })
}

async function discardAllLive() {
  const run = chatRef.value?.discardAllLive
  if (!run || liveDiscarding.value) return
  liveDiscarding.value = true
  liveNotice.value = ''
  try {
    const n = await run()
    liveNotice.value = t('pages.embedChat.live.discardAllDone', { n })
  } catch (e) {
    liveNotice.value = e instanceof Error ? e.message : String(e)
  } finally {
    liveDiscarding.value = false
  }
}

function onEventsReady() {
  pageControl.onEventsReady()
  void syncLive()
}

function takeHash(): string {
  const hash = window.location.hash
  if (hash) history.replaceState(history.state, '', `${window.location.pathname}${window.location.search}`)
  return hash
}

async function connect() {
  phase.value = 'connecting'
  const hash = takeHash()
  const theme = parseEmbedThemeFromHash(hash)
  if (theme) setThemeOverride(theme)
  const ticket = parseEmbedTicketFromHash(hash)
  if (ticket) {
    try {
      const s = await redeemEmbedTicket(ticket, runId, nodeId)
      if (s) {
        saveEmbedSession(runId, nodeId, s)
        token.value = s.token
        phase.value = 'ready'
        return
      }
    } catch {
      if (!loadEmbedSession(runId, nodeId)) {
        phase.value = 'network'
        return
      }
    }
  }
  const stored = loadEmbedSession(runId, nodeId)
  if (stored) {
    token.value = stored.token
    phase.value = 'ready'
    return
  }
  markExpired()
}

function markExpired() {
  phase.value = 'expired'
  // The page greys out Pick and Chat until it is reopened with a new ticket.
  if (window.parent !== window) window.parent.postMessage({ type: EMBED_SESSION_MESSAGE, ok: false }, parentOrigin())
}

function onStatus(status: string) {
  // The page queues picks until the chat can take them.
  if (status === 'active' && !announced && window.parent !== window) {
    announced = true
    window.parent.postMessage({ type: EMBED_READY_MESSAGE }, parentOrigin())
    pageControl.awaitHello()
    void syncLive()
  }
  if (status === 'invalid' || status === 'expired' || status === 'revoked') {
    pageControl.reset()
    clearEmbedSession(runId, nodeId)
    token.value = ''
    markExpired()
  }
}

function parentOrigin(): string {
  return window.location.ancestorOrigins?.[0] || '*'
}

function onMessage(e: MessageEvent) {
  if (window.parent === window || e.source !== window.parent) return
  const origin = window.location.ancestorOrigins?.[0]
  if (origin && e.origin !== origin) return
  const theme = parseEmbedThemeMessage(e.data)
  if (theme) {
    setThemeOverride(theme)
    return
  }
  const control = parseEmbedControlMessage(e.data)
  if (control) {
    pageControl.onPageControl(control)
    return
  }
  const result = parseEmbedCmdResult(e.data)
  if (result) {
    pageControl.onPageResult(result)
    return
  }
  const liveMsg = parseEmbedLiveMessage(e.data)
  if (liveMsg) {
    if (liveMsg.kind === 'state') chatRef.value?.setLiveView?.(liveMsg.sid, liveMsg.view)
    else void forwardLive(liveMsg.reqId, liveMsg.event)
    return
  }
  const pick = parseEmbedPickMessage(e.data)
  if (pick) chatRef.value?.addPick?.(pick)
}

onMounted(() => {
  window.addEventListener('message', onMessage)
  void connect()
})
onUnmounted(() => {
  window.removeEventListener('message', onMessage)
  pageControl.dispose()
  setThemeOverride(null)
})
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden bg-base text-txt" data-testid="embed-chat-root">
    <div
      v-if="phase === 'ready' && token && pageControlSupported !== null"
      class="shrink-0 border-b border-line px-3 py-2"
      data-page-agent-not-interactive
      data-testid="page-control-bar"
    >
      <template v-if="pageControlSupported">
        <label class="flex items-center justify-between gap-3 text-[12px] text-txt2">
          <span>{{ t('pages.embedChat.pageControl.toggle') }}</span>
          <AppSwitch
            :model-value="pageControlOn"
            :aria-label="t('pages.embedChat.pageControl.toggle')"
            data-testid="page-control-toggle"
            @update:model-value="pageControl.setEnabled"
          />
        </label>
        <p v-if="!pageControlOn" class="m-0 mt-1 text-[11px] leading-snug text-txt3">
          {{ t('pages.embedChat.pageControl.privacy') }}
        </p>
        <PageControlStatus v-else class="mt-1" :state="pageControlState" :active="pageControlActive" />
      </template>
      <p v-else class="m-0 text-[11px] leading-snug text-txt3" data-testid="page-control-unsupported">
        {{ t('pages.embedChat.pageControl.unsupported') }}
      </p>
    </div>
    <div
      v-if="phase === 'ready' && token && (liveOpenCount > 0 || liveNotice)"
      class="flex shrink-0 items-center gap-2 border-b border-line px-3 py-1.5 text-[11px] text-txt2"
      role="status"
      data-testid="live-open-bar"
    >
      <span class="min-w-0 flex-1 truncate">{{ liveOpenCount > 0 ? t('pages.embedChat.live.openSessions', { n: liveOpenCount }) : liveNotice }}</span>
      <span v-if="hasFailedSteer" class="text-txt3" data-testid="live-steer-partial">{{ t('pages.embedChat.live.steerPartial') }}</span>
      <button
        v-if="liveOpenCount > 0"
        type="button"
        class="shrink-0 rounded border border-line px-2 py-0.5 text-txt2 hover:text-err disabled:opacity-40"
        :disabled="liveDiscarding"
        data-testid="live-discard-all"
        @click="discardAllLive"
      >
        {{ t(hasFailedSteer ? 'pages.embedChat.live.cleanupAll' : 'pages.embedChat.live.discardAll') }}
      </button>
    </div>
    <PublicGateApprovalView
      v-if="phase === 'ready' && token"
      ref="chatRef"
      class="min-h-0 flex-1"
      :embed-token="token"
      @status="onStatus"
      @events-ready="onEventsReady"
      @events-closed="pageControl.onEventsClosed"
      @page-frame="pageControl.onServerFrame"
      @live-session="onLiveSession"
      @live-cmd="onLiveCmd"
    />
    <div
      v-else-if="phase === 'connecting'"
      class="flex flex-1 flex-col items-center justify-center gap-3 text-center"
      role="status"
      aria-busy="true"
      data-testid="embed-chat-connecting"
    >
      <Icon name="spinner" :size="24" class="animate-spin text-accent" aria-hidden="true" />
      <p class="text-sm text-txt3">{{ t('pages.embedChat.connecting') }}</p>
    </div>
    <div
      v-else-if="phase === 'network'"
      class="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center"
      role="alert"
      data-testid="embed-chat-network"
    >
      <Icon name="alert" :size="24" class="text-warn" />
      <p class="max-w-[36ch] text-sm text-txt2">{{ t('pages.embedChat.networkError') }}</p>
    </div>
    <div
      v-else
      class="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center"
      role="status"
      data-testid="embed-chat-expired"
    >
      <Icon name="alert" :size="24" class="text-warn" />
      <h1 class="text-base font-semibold">{{ t('pages.embedChat.expiredTitle') }}</h1>
      <p class="max-w-[36ch] text-sm text-txt3">{{ t('pages.embedChat.expiredHint') }}</p>
    </div>
  </div>
</template>

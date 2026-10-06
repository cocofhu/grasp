import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { relTime } from '@/lib/shared/format'
import { renderMarkdown } from '@/lib/shared/markdown'
import { createStreamMarkdownPreview } from '@/lib/shared/streamMarkdownPreview'
import { api } from '@/lib/api/api'
import { imgSrc } from '@/lib/shared/compositeText'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { useImageAttachments } from '@/lib/composables/useImageAttachments'
import {
  attachmentDisplayName,
  isImageAttachment,
} from '@/lib/shared/attachments'
import { useToast } from '@/lib/composables/useToast'
import {
  isPmFailKind,
  pmActiveThreadStorageKey,
  pmWsReconnectDelayMs,
  type PmFailKind,
} from '@/lib/pm/pmTurnState'
import { extractAgentMessageDelta } from '@/lib/run/acpUnpack'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import {
  channelPeerId,
  isChannelThreadUserId,
  parseChannelUserId,
} from '@/lib/pm/cronDeliverTargets'
import type { ChatMessage, ChatThread, ClarifyImage, PmLeaderBinding } from '@/lib/shared/types'

export interface UsePmLeaderChatProps {
  projectId: string
  binding: PmLeaderBinding | null
  restoreMobileChat?: boolean
  unknownModelDisplayName?: string | null
}

export type UsePmLeaderChatEmit = {
  (event: 'openSettings'): void
  (event: 'restoredMobileChat'): void
}

export function usePmLeaderChat(props: UsePmLeaderChatProps, emit: UsePmLeaderChatEmit) {
type FailKind = PmFailKind

const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()
const mobileView = ref<'threads' | 'chat'>('threads')

const threads = ref<ChatThread[]>([])
const activeId = ref('')
const messages = ref<ChatMessage[]>([])
const input = ref('')
const loading = ref(true)
const messagesLoading = ref(false)
const messagesLoadFailed = ref(false)
const finalizingRefetchFailed = ref(false)
const finalizing = ref(false)
const sending = ref(false)
const streaming = ref(false)
const streamText = ref('')
/** HTML for the live bubble — updated at most once per animation frame. */
const streamHtml = ref('')
const streamPreview = createStreamMarkdownPreview({ render: renderMarkdown })
const unsubStreamHtml = streamPreview.subscribe((html) => {
  streamHtml.value = html
})
function syncStreamText(next: string) {
  streamText.value = next
  streamPreview.setText(next)
  // Absolute snapshots (resume / seed) paint immediately; deltas stay rAF-batched.
  streamPreview.flush()
}
function appendStreamText(delta: string) {
  streamText.value += delta
  streamPreview.append(delta)
}
function clearStreamText() {
  streamText.value = ''
  streamPreview.reset()
  streamHtml.value = ''
}
/** Reconnected mid-turn: the bubble waits for replayed output. */
const resuming = ref(false)
const scroller = ref<HTMLElement | null>(null)

const STICK_THRESHOLD = 48
/** Align with approved Demo: near-top auto lazyload threshold (px). */
const TOP_THRESHOLD = 56
/** Fixed page size for PM session message windows. */
const PAGE_SIZE = 20
const stickToBottom = ref(true)
/** True when older messages remain beyond the loaded window. */
const hasMoreEarlier = ref(false)
const historyLoading = ref(false)
const historyLoadFailed = ref(false)

function isNearBottom(el: HTMLElement) {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= STICK_THRESHOLD
}

function onScrollerScroll() {
  const el = scroller.value
  if (!el) return
  stickToBottom.value = isNearBottom(el)
  if (el.scrollTop <= TOP_THRESHOLD) {
    void loadEarlier()
  }
}

function scrollBottom(force = false) {
  const el = scroller.value
  if (el && (force || stickToBottom.value)) {
    el.scrollTop = el.scrollHeight
  }
}

/** Keep already-loaded prefix; update/append by message id (never shrink to latest PAGE). */
function mergeMessagesKeepPrefix(existing: ChatMessage[], incoming: ChatMessage[]): ChatMessage[] {
  if (!incoming.length) return existing
  if (!existing.length) return incoming.slice()
  const byId = new Map(existing.map((m) => [m.id, m]))
  for (const m of incoming) {
    byId.set(m.id, m)
  }
  const seen = new Set(existing.map((m) => m.id))
  const out = existing.map((m) => byId.get(m.id)!)
  for (const m of incoming) {
    if (!seen.has(m.id)) {
      out.push(m)
      seen.add(m.id)
    }
  }
  return out
}

function resetHistoryWindowState() {
  hasMoreEarlier.value = false
  historyLoading.value = false
  historyLoadFailed.value = false
}

/** Session-only failed partial bubbles (S2); not persisted as assistant rows. */
const failedPartialByUserMsgId = ref<Record<string, string>>({})

/** Current turn's user message id (for fail/retry persistence). */
const activeUserMessageId = ref('')

const {
  attachments,
  fileInput,
  notice: attachNotice,
  onPickFiles,
  onPaste,
  removeAttachment,
  takeAttachments,
  blockSendIfOversized,
} = useImageAttachments()

const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

watch(attachNotice, (n) => {
  if (n?.kind === 'error') toast.error(n.text)
})

let ws: WebSocket | null = null
/** Thread the socket should follow; '' disables reconnect. */
let wsThreadId = ''
let wsAttempt = 0
let wsReconnectTimer: ReturnType<typeof setTimeout> | null = null
let disposed = false
/** Sandbox boot / turn phase from the server (preparing|pulling|creating|running). */
const sandboxBootStatus = ref('')
/** Discard stale listPmMessages responses after thread switch or re-load. */
let threadLoadGen = 0

const enabled = computed(() => !!props.binding?.enabled && props.binding.agentAvailable)
const turnBusy = computed(
  () => sending.value || streaming.value || finalizing.value || resuming.value,
)
const busy = computed(() => turnBusy.value || messagesLoading.value)
const canSend = computed(
  () => !busy.value && (!!input.value.trim() || attachments.value.length > 0),
)
const showStreamBubble = computed(
  () => sending.value || streaming.value || finalizing.value || resuming.value,
)
/** Main pane priority: finalizing > resuming > messagesLoading > errorEmpty > content */
const mainViewState = computed(() => {
  if (finalizing.value) return 'finalizing'
  if (resuming.value) return 'resuming'
  if (messagesLoading.value) return 'messagesLoading'
  if (messagesLoadFailed.value) return 'errorEmpty'
  return 'content'
})
const isPullingBoot = computed(
  () => (sandboxBootStatus.value || '').trim().toLowerCase() === 'pulling',
)
const busyHint = computed(() => {
  if (finalizing.value) return t('pages.projectDetail.pm.busyFinalizing')
  if (resuming.value) return t('pages.projectDetail.pm.busyResuming')
  if (isPullingBoot.value) return t('pages.projectDetail.pm.busyPulling')
  if (streaming.value && streamText.value) return t('pages.projectDetail.pm.busyStreaming')
  return t('pages.projectDetail.pm.busyWaiting')
})
const suggestions = computed(() => [
  t('pages.projectDetail.pm.suggestProgress'),
  t('pages.projectDetail.pm.suggestBlockers'),
  t('pages.projectDetail.pm.suggestRisk'),
])
const showStreamTypingDots = computed(
  () => showStreamBubble.value && !streamText.value && !finalizing.value && !resuming.value,
)

async function copyAssistantText(ev: Event) {
  const root = (ev.currentTarget as HTMLElement | null)?.closest('[data-assistant-bubble]')
  const md = root?.querySelector('.md') as HTMLElement | null
  if (!md) return
  const text = md.innerText.trim()
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    toast.success(t('pages.projectDetail.pm.copySuccess'))
  } catch {
    toast.error(t('pages.projectDetail.pm.copyFailed'))
  }
}
const showThreadsAside = computed(() => !isMobile.value || mobileView.value === 'threads')
const showChatSection = computed(() => !isMobile.value || mobileView.value === 'chat')

/** Channel synthetic user id, e.g. qq:guild:… / wecom:c2c:… / feishu:c2c:… / dingtalk:c2c:… */
function channelTypeOf(th: ChatThread | undefined | null): string {
  return parseChannelUserId(th?.userId || '')?.type || ''
}

function isChannelThread(th: ChatThread | undefined | null): boolean {
  return isChannelThreadUserId(th?.userId)
}

function channelBadgeLabel(th: ChatThread | undefined | null): string {
  switch (channelTypeOf(th)) {
    case 'wecom':
      return t('pages.projectDetail.pm.channelBadgeWecom')
    case 'feishu':
      return t('pages.projectDetail.pm.channelBadgeFeishu')
    case 'dingtalk':
      return t('pages.projectDetail.pm.channelBadgeDingTalk')
    default:
      return t('pages.projectDetail.pm.channelBadgeQQ')
  }
}

function channelBadgeClass(th: ChatThread | undefined | null): string {
  switch (channelTypeOf(th)) {
    case 'wecom':
      return 'border-accent/55 bg-accent-dim text-accent-2'
    case 'feishu':
      return 'border-cyan-400/40 bg-cyan-400/10 text-cyan-300'
    case 'dingtalk':
      return 'border-blue-400/40 bg-blue-400/10 text-blue-300'
    default:
      return 'border-accent-2/35 bg-accent/15 text-accent-2'
  }
}

function channelReadonlyTitle(th: ChatThread | undefined | null): string {
  switch (channelTypeOf(th)) {
    case 'wecom':
      return t('pages.projectDetail.pm.channelReadonlyTitleWecom')
    case 'feishu':
      return t('pages.projectDetail.pm.channelReadonlyTitleFeishu')
    case 'dingtalk':
      return t('pages.projectDetail.pm.channelReadonlyTitleDingTalk')
    default:
      return t('pages.projectDetail.pm.channelReadonlyTitle')
  }
}

function channelReadonlyHint(th: ChatThread | undefined | null): string {
  switch (channelTypeOf(th)) {
    case 'wecom':
      return t('pages.projectDetail.pm.channelReadonlyHintWecom')
    case 'feishu':
      return t('pages.projectDetail.pm.channelReadonlyHintFeishu')
    case 'dingtalk':
      return t('pages.projectDetail.pm.channelReadonlyHintDingTalk')
    default:
      return t('pages.projectDetail.pm.channelReadonlyHint')
  }
}

function threadDisplayTitle(th: ChatThread | undefined | null): string {
  if (isChannelThread(th) && th?.userId) {
    // g3.2: channel thread primary title is the synthetic identity.
    return th.userId
  }
  const title = (th?.title || '').trim()
  if (title) return title
  return t('pages.projectDetail.pm.untitled')
}

function channelSourceLine(th: ChatThread | undefined | null): string {
  const kind = channelTypeOf(th)
  const src =
    kind === 'wecom'
      ? t('pages.projectDetail.pm.channelSourceWecom')
      : kind === 'feishu'
        ? t('pages.projectDetail.pm.channelSourceFeishu')
        : kind === 'dingtalk'
          ? t('pages.projectDetail.pm.channelSourceDingTalk')
          : t('pages.projectDetail.pm.channelSourceQq')
  const peer = channelPeerId(th?.userId || '')
  if (th?.unspoken) {
    return `${src} · ${t('pages.projectDetail.pm.unspoken')}${peer ? ` · ${peer}` : ''}`
  }
  return peer ? `${src} · ${peer}` : src
}

const activeThread = computed(() => threads.value.find((x) => x.id === activeId.value))
const activeIsChannel = computed(() => isChannelThread(activeThread.value))
const activeThreadTitle = computed(() => threadDisplayTitle(activeThread.value))
const showEmptyHint = computed(
  () =>
    mainViewState.value === 'content' &&
    !activeIsChannel.value &&
    !messages.value.length &&
    !showStreamBubble.value,
)
/** Top non-button history tip (Demo four-state). */
const showHistoryTip = computed(
  () =>
    mainViewState.value === 'content' &&
    !showEmptyHint.value &&
    (messages.value.length > 0 || historyLoading.value || historyLoadFailed.value),
)
const historyTipText = computed(() => {
  if (historyLoading.value) return t('pages.projectDetail.pm.historyLoading')
  if (historyLoadFailed.value) return t('pages.projectDetail.pm.historyLoadFailed')
  if (!hasMoreEarlier.value) return t('pages.projectDetail.pm.historyReachedStart')
  return t('pages.projectDetail.pm.historyScrollUp')
})
const historyTipClass = computed(() => {
  if (historyLoading.value) return 'text-accent'
  if (historyLoadFailed.value) return 'text-err'
  return 'text-txt3'
})
const showIdleSuggestions = computed(() => {
  if (activeIsChannel.value) return false
  const msgs = messages.value
  if (!msgs.length || busy.value || showStreamBubble.value) return false
  return msgs[msgs.length - 1]?.role === 'assistant'
})

type ChannelCtx = { open: true; x: number; y: number; threadId: string }
const channelCtx = ref<ChannelCtx | null>(null)
const channelDetailOpen = ref(false)
const channelDetailTitle = ref('')
const channelDetailSource = ref('')

function closeChannelCtx() {
  channelCtx.value = null
}

function openChannelCtx(e: MouseEvent, th: ChatThread) {
  if (!isChannelThread(th)) return
  e.preventDefault()
  e.stopPropagation()
  channelCtx.value = { open: true, x: e.clientX, y: e.clientY, threadId: th.id }
}

function openChannelDetail() {
  if (!channelCtx.value) return
  const th = threads.value.find((x) => x.id === channelCtx.value!.threadId)
  channelDetailTitle.value = threadDisplayTitle(th)
  channelDetailSource.value = channelSourceLine(th)
  channelDetailOpen.value = true
  closeChannelCtx()
}

function closeChannelDetail() {
  channelDetailOpen.value = false
}

function onChannelCtxAction() {
  openChannelDetail()
}

const FAIL_KIND_KEYS: Record<FailKind, { title: string; desc: string }> = {
  connection: { title: 'failConnectionTitle', desc: 'failConnectionDesc' },
  sandbox: { title: 'failSandboxTitle', desc: 'failSandboxDesc' },
  empty: { title: 'failEmptyTitle', desc: 'failEmptyDesc' },
  unknown: { title: 'failUnknownTitle', desc: 'failUnknownDesc' },
  stopped: { title: 'failStoppedTitle', desc: 'failStoppedDesc' },
  interrupted: { title: 'failInterruptedTitle', desc: 'failInterruptedDesc' },
}

function failMeta(kind: string) {
  const k = (isPmFailKind(kind) ? kind : 'unknown') as FailKind
  const keys = FAIL_KIND_KEYS[k]
  return {
    kind: k,
    title: t(`pages.projectDetail.pm.${keys.title}`),
    desc: t(`pages.projectDetail.pm.${keys.desc}`),
  }
}

function applyRestoreMobileChat() {
  if (props.restoreMobileChat && isMobile.value && activeId.value) {
    mobileView.value = 'chat'
    emit('restoredMobileChat')
  }
}

function isFailedUser(m: ChatMessage) {
  return m.role === 'user' && m.status === 'failed'
}

/** QQ inbound download-failure notice only; other system rows stay hidden. */
function isChannelHint(m: ChatMessage) {
  return m.role === 'system' && m.source === 'channel'
}

async function loadThreads() {
  loading.value = true
  try {
    const res = await api.listPmThreads(props.projectId)
    threads.value = res.items || []
    const stored =
      typeof localStorage !== 'undefined'
        ? localStorage.getItem(pmActiveThreadStorageKey(props.projectId)) || ''
        : ''
    const preferred =
      (stored && threads.value.some((t) => t.id === stored) && stored) ||
      activeId.value ||
      (threads.value[0]?.id ?? '')
    if (!activeId.value && preferred) {
      await activateThread(preferred)
    } else if (activeId.value) {
      await loadMessages(activeId.value)
    }
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

/**
 * Bind UI to a thread. When preserveTurn is true (send-path ensure), do NOT bump
 * turnGen / turnClosed — otherwise a mid-send newThread would silently abort the turn.
 */
async function activateThread(id: string, opts?: { preserveTurn?: boolean }) {
  threadLoadGen += 1
  if (!opts?.preserveTurn) {
    resetTurnLocal()
  }
  activeId.value = id
  messages.value = []
  messagesLoadFailed.value = false
  resetHistoryWindowState()
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(pmActiveThreadStorageKey(props.projectId), id)
    }
  } catch {
    /* ignore quota */
  }
  closeWs()
  await loadMessages(id)
  if (id === activeId.value) connectThreadWs(id)
}

async function selectThread(id: string) {
  if (turnBusy.value) return
  if (id === activeId.value) {
    if (isMobile.value) mobileView.value = 'chat'
    return
  }
  await activateThread(id)
  if (isMobile.value) mobileView.value = 'chat'
}

function backToThreads() {
  if (turnBusy.value) return
  mobileView.value = 'threads'
}

/** Create + activate a thread without resetting an in-flight send/retry generation. */
async function ensureActiveThread() {
  if (activeId.value) return
  const thr = await api.createPmThread(props.projectId)
  threads.value = [thr, ...threads.value]
  await activateThread(thr.id, { preserveTurn: true })
}

async function loadMessages(tid: string) {
  const gen = ++threadLoadGen
  messagesLoadFailed.value = false
  messagesLoading.value = true
  historyLoadFailed.value = false
  historyLoading.value = false
  try {
    const res = await api.listPmMessages(props.projectId, tid, { limit: PAGE_SIZE })
    if (gen !== threadLoadGen || tid !== activeId.value) return
    messages.value = res.items || []
    hasMoreEarlier.value = !!res.hasMore
  } catch {
    if (gen !== threadLoadGen || tid !== activeId.value) return
    messagesLoadFailed.value = true
    toast.error(t('pages.projectDetail.pm.loadFailed'))
  } finally {
    if (gen === threadLoadGen && tid === activeId.value) {
      // Clear loading before scrollBottom: while messagesLoading, template shows
      // ArtifactLoadingPane and scrollHeight is not the message list.
      messagesLoading.value = false
    }
  }
  // Force stick-to-bottom only after content replaces the loading pane.
  if (gen !== threadLoadGen || tid !== activeId.value || messagesLoadFailed.value) return
  stickToBottom.value = true
  await nextTick()
  if (gen !== threadLoadGen || tid !== activeId.value) return
  scrollBottom(true)
  // Browser layout can lag one frame behind the v-if swap; re-pin after paint.
  requestAnimationFrame(() => {
    if (gen !== threadLoadGen || tid !== activeId.value) return
    scrollBottom(true)
  })
}

/**
 * Near-top lazyload: prepend up to PAGE_SIZE older messages.
 * Uses threadLoadGen so a late response cannot write into another session.
 */
async function loadEarlier() {
  if (
    historyLoading.value ||
    !hasMoreEarlier.value ||
    !activeId.value ||
    messagesLoading.value ||
    !messages.value.length
  ) {
    return
  }
  const gen = threadLoadGen
  const tid = activeId.value
  const before = messages.value[0]?.id
  if (!before) return

  historyLoading.value = true
  historyLoadFailed.value = false
  const el = scroller.value
  const prevTop = el?.scrollTop ?? 0
  const prevHeight = el?.scrollHeight ?? 0

  try {
    const res = await api.listPmMessages(props.projectId, tid, { limit: PAGE_SIZE, before })
    if (gen !== threadLoadGen || tid !== activeId.value) return
    const older = res.items || []
    if (older.length) {
      const existing = new Set(messages.value.map((m) => m.id))
      const prepend = older.filter((m) => !existing.has(m.id))
      messages.value = [...prepend, ...messages.value]
    }
    hasMoreEarlier.value = !!res.hasMore
    stickToBottom.value = false
    await nextTick()
    if (el) {
      el.scrollTop = prevTop + (el.scrollHeight - prevHeight)
    }
  } catch {
    if (gen !== threadLoadGen || tid !== activeId.value) return
    historyLoadFailed.value = true
  } finally {
    if (gen === threadLoadGen && tid === activeId.value) {
      historyLoading.value = false
    }
  }
}

async function retryLoadMessages() {
  if (!activeId.value || messagesLoading.value) return
  await loadMessages(activeId.value)
}

function setFailedPartial(userMsgId: string, partial: string) {
  const text = partial.trim()
  if (!userMsgId || !text) return
  failedPartialByUserMsgId.value = { ...failedPartialByUserMsgId.value, [userMsgId]: partial }
}

function clearFailedPartial(userMsgId: string) {
  if (!(userMsgId in failedPartialByUserMsgId.value)) return
  const next = { ...failedPartialByUserMsgId.value }
  delete next[userMsgId]
  failedPartialByUserMsgId.value = next
}

async function newThread() {
  if (!enabled.value) {
    emit('openSettings')
    return
  }
  if (turnBusy.value) return
  try {
    const thr = await api.createPmThread(props.projectId)
    threads.value = [thr, ...threads.value]
    await activateThread(thr.id)
    if (isMobile.value) mobileView.value = 'chat'
  } catch (e: any) {
    toast.error(String(e?.message || e))
  }
}

async function removeThread(id: string) {
  if (turnBusy.value) return
  const th = threads.value.find((x) => x.id === id)
  if (isChannelThread(th)) return
  try {
    await api.deletePmThread(props.projectId, id)
    threads.value = threads.value.filter((x) => x.id !== id)
    if (activeId.value === id) {
      activeId.value = ''
      messages.value = []
      resetTurnLocal()
      closeWs()
      if (threads.value[0]) await activateThread(threads.value[0].id)
    }
  } catch (e: any) {
    toast.error(String(e?.message || e))
  }
}

function clearReconnectTimer() {
  if (wsReconnectTimer) {
    clearTimeout(wsReconnectTimer)
    wsReconnectTimer = null
  }
}

/** Close the thread socket and stop following it. */
function closeWs() {
  wsThreadId = ''
  clearReconnectTimer()
  if (ws) {
    const socket = ws
    ws = null
    try {
      socket.close()
    } catch {
      /* ignore */
    }
  }
}

/**
 * Follow the thread's turn stream. The server sends a queue_state snapshot on
 * every (re)connect, so a dropped socket only reconnects — it never fails a turn.
 */
function connectThreadWs(tid: string) {
  closeWs()
  if (!tid || disposed || isChannelThread(threads.value.find((x) => x.id === tid))) return
  wsThreadId = tid
  wsAttempt = 0
  openThreadWs()
}

function openThreadWs() {
  clearReconnectTimer()
  const tid = wsThreadId
  if (!tid || disposed) return
  let socket: WebSocket
  try {
    socket = new WebSocket(api.pmThreadChatWsUrl(props.projectId, tid))
  } catch {
    scheduleReconnect()
    return
  }
  ws = socket
  socket.onmessage = (ev) => {
    if (ws !== socket || tid !== activeId.value) return
    let msg: any
    try {
      msg = JSON.parse(ev.data)
    } catch {
      return
    }
    wsAttempt = 0
    handleFrame(msg)
  }
  socket.onclose = () => {
    if (ws !== socket) return
    ws = null
    scheduleReconnect()
  }
}

function scheduleReconnect() {
  if (!wsThreadId || disposed || wsReconnectTimer) return
  if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return
  const delay = pmWsReconnectDelayMs(wsAttempt)
  wsAttempt += 1
  wsReconnectTimer = setTimeout(() => {
    wsReconnectTimer = null
    openThreadWs()
  }, delay)
}

function onVisibilityChange() {
  if (document.visibilityState !== 'visible' || !wsThreadId || ws) return
  wsAttempt = 0
  openThreadWs()
}

function handleFrame(msg: any) {
  if (msg?.type === 'acp') {
    if (msg.data) handleAcp(msg.data)
    return
  }
  if (msg?.type !== 'session') return
  switch (msg.event) {
    case 'queue_state':
      applyQueueState(msg)
      break
    case 'turn_begin':
      beginServerTurn(String(msg.userMsgId || msg.item?.id || ''))
      break
    case 'phase':
      sandboxBootStatus.value = String(msg.phase || '')
      break
    case 'turn_done':
      if (msg.interrupted) {
        void onTurnError('stopped', '')
      } else {
        void onTurnDone()
      }
      break
    case 'error': {
      const kind = (isPmFailKind(msg.failKind) ? msg.failKind : 'unknown') as FailKind
      void onTurnError(kind, String(msg.message || 'error'))
      break
    }
  }
}

/** Snapshot / queue change: busy restores the live bubble; idle settles a turn whose end we missed. */
function applyQueueState(msg: { busy?: boolean; waiting?: number; phase?: string; userMsgId?: string }) {
  if (msg.busy) {
    if (msg.phase) sandboxBootStatus.value = msg.phase
    if (msg.userMsgId && msg.userMsgId !== activeUserMessageId.value) {
      beginServerTurn(msg.userMsgId)
      resuming.value = true
    } else if (!streaming.value && !finalizing.value) {
      streaming.value = true
      resuming.value = true
    }
    return
  }
  if ((msg.waiting ?? 0) > 0) return
  sandboxBootStatus.value = ''
  if ((sending.value || streaming.value) && !finalizing.value) void onTurnDone()
}

function beginServerTurn(userMsgId: string) {
  if (userMsgId) activeUserMessageId.value = userMsgId
  clearStreamText()
  sending.value = false
  streaming.value = true
  finalizing.value = false
  resuming.value = false
  // Turn started elsewhere (IM channel, cron, another tab): pull its user message.
  if (userMsgId && !messagesLoading.value && !messages.value.some((m) => m.id === userMsgId)) {
    void refreshMessages()
  }
}

function resetTurnLocal() {
  clearStreamText()
  streaming.value = false
  sending.value = false
  resuming.value = false
  finalizing.value = false
  sandboxBootStatus.value = ''
  activeUserMessageId.value = ''
  failedPartialByUserMsgId.value = {}
}

function patchLocalMessage(mid: string, patch: Partial<ChatMessage>) {
  messages.value = messages.value.map((m) => (m.id === mid ? { ...m, ...patch } : m))
}

function upsertLocalMessage(msg: ChatMessage) {
  if (messages.value.some((m) => m.id === msg.id)) {
    patchLocalMessage(msg.id, msg)
  } else {
    messages.value = [...messages.value, msg]
  }
}

function handleAcp(raw: any) {
  if ((!streaming.value && !sending.value) || finalizing.value) return
  const delta = extractAgentMessageDelta(raw)
  if (!delta?.text) return
  sending.value = false
  streaming.value = true
  resuming.value = false
  appendStreamText(delta.text)
  void nextTick().then(() => scrollBottom())
}

function clearFinalizingStream() {
  finalizing.value = false
  finalizingRefetchFailed.value = false
  clearStreamText()
  activeUserMessageId.value = ''
  resuming.value = false
}

/** Tail refetch merged into the loaded window. */
async function refreshMessages(): Promise<boolean> {
  if (!activeId.value) return false
  const tid = activeId.value
  const gen = threadLoadGen
  const res = await api.listPmMessages(props.projectId, tid, { limit: PAGE_SIZE })
  if (gen !== threadLoadGen || tid !== activeId.value) return false
  const incoming = res.items || []
  messages.value = mergeMessagesKeepPrefix(messages.value, incoming)
  if (typeof res.hasMore === 'boolean' && messages.value.length <= PAGE_SIZE) {
    // Only trust hasMore when we have not prepended beyond the tail window.
    hasMoreEarlier.value = res.hasMore
  }
  return true
}

async function refetchAfterTurnDone() {
  if (!activeId.value) return
  finalizingRefetchFailed.value = false
  const tid = activeId.value
  const gen = threadLoadGen
  try {
    if (!(await refreshMessages())) return
    const thr = await api.listPmThreads(props.projectId)
    if (gen !== threadLoadGen || tid !== activeId.value) return
    threads.value = thr.items || []
    await nextTick()
    scrollBottom()
    clearFinalizingStream()
  } catch {
    if (gen !== threadLoadGen || tid !== activeId.value) return
    finalizingRefetchFailed.value = true
    toast.error(t('pages.projectDetail.pm.loadFailed'))
  }
}

/** Server finalized the turn (assistant appended). Refresh messages. */
async function onTurnDone() {
  if (finalizing.value) return
  streaming.value = false
  sending.value = false
  resuming.value = false
  sandboxBootStatus.value = ''
  streamPreview.flush()
  finalizing.value = true
  if (!activeId.value) {
    clearFinalizingStream()
    return
  }
  await refetchAfterTurnDone()
}

/** Server already persisted the failure on the user message; refresh then show the card. */
async function onTurnError(kind: FailKind, detail: string) {
  const mid = activeUserMessageId.value
  if (mid && kind !== 'stopped') setFailedPartial(mid, streamText.value)
  clearStreamText()
  streaming.value = false
  sending.value = false
  resuming.value = false
  finalizing.value = false
  sandboxBootStatus.value = ''
  activeUserMessageId.value = ''
  try {
    await refreshMessages()
  } catch {
    /* card falls back to the local patch below */
  }
  if (mid) {
    const user = messages.value.find((m) => m.id === mid)
    if (user && user.status !== 'failed') patchLocalMessage(mid, { status: 'failed', failKind: kind })
  }
  if (detail && kind === 'unknown') toast.error(detail)
  await nextTick()
  scrollBottom()
}

/** Queue one turn on the server: a new message (content) or a retry (retryOf). */
async function startTurn(body: { content?: string; images?: ClarifyImage[]; retryOf?: string }) {
  if (!activeId.value) throw new Error('no thread')
  const tid = activeId.value
  const res = await api.startPmTurn(props.projectId, tid, body)
  if (tid !== activeId.value) return
  upsertLocalMessage(res.message)
  if (!activeUserMessageId.value || !streaming.value) activeUserMessageId.value = res.message.id
  await nextTick()
  stickToBottom.value = true
  scrollBottom(true)
}

/**
 * @param text - when set, use as message body (suggestions) instead of input
 * @param explicitImages - when provided, use these images and do not take pending
 */
async function send(text?: string, explicitImages?: ClarifyImage[]) {
  const fromInput = text == null
  const content = (text ?? input.value).trim()
  const imgs =
    explicitImages !== undefined
      ? explicitImages.slice()
      : attachments.value.slice()
  if ((!content && imgs.length === 0) || busy.value) return
  if (activeIsChannel.value) return
  if (!enabled.value) {
    emit('openSettings')
    return
  }
  if (blockSendIfOversized(imgs)) return
  if (explicitImages === undefined) takeAttachments()
  else attachments.value = []
  // Lock busy BEFORE any await so double-click / suggestion cannot queue another turn.
  sending.value = true
  clearStreamText()
  if (fromInput) input.value = ''
  try {
    // Create thread without resetting this send (review v1).
    if (!activeId.value) await ensureActiveThread()
    await startTurn({ content, images: imgs.length ? imgs : undefined })
  } catch (e: any) {
    sending.value = false
    if (fromInput) input.value = content
    if (imgs.length) attachments.value = [...imgs, ...attachments.value]
    toast.error(String(e?.message || e))
    // A rejected turn may still have persisted (and failed) the user message.
    void refreshMessages().catch(() => {})
  }
}

async function stop() {
  if (!turnBusy.value || !activeId.value) return
  try {
    await api.cancelPmTurn(props.projectId, activeId.value)
  } catch (e: any) {
    toast.error(String(e?.message || e))
  }
}

/** Cover-this-turn retry: reuse userMessageId, do not append a new user bubble. */
async function retryTurn(userMessageId: string) {
  if (busy.value) return
  const userMsg = messages.value.find((m) => m.id === userMessageId && m.role === 'user')
  if (!userMsg) return
  sending.value = true
  clearStreamText()
  clearFailedPartial(userMessageId)
  const prev = { status: userMsg.status, failKind: userMsg.failKind }
  patchLocalMessage(userMessageId, { status: 'ok', failKind: '' })
  try {
    await startTurn({ retryOf: userMessageId })
  } catch (e: any) {
    sending.value = false
    patchLocalMessage(userMessageId, prev)
    toast.error(String(e?.message || e))
  }
}

watch(isMobile, () => {
  mobileView.value = 'threads'
})

watch(
  () => props.projectId,
  () => {
    activeId.value = ''
    messages.value = []
    messagesLoadFailed.value = false
    messagesLoading.value = false
    mobileView.value = 'threads'
    resetTurnLocal()
    void loadThreads()
  },
)

onMounted(() => {
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisibilityChange)
  void loadThreads().then(() => applyRestoreMobileChat())
})
onBeforeUnmount(() => {
  disposed = true
  if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisibilityChange)
  unsubStreamHtml()
  streamPreview.reset()
  resetTurnLocal()
  closeWs()
})

  return {
  t,
  toast,
  isMobile,
  mobileView,
  threads,
  activeId,
  messages,
  input,
  loading,
  messagesLoading,
  messagesLoadFailed,
  finalizingRefetchFailed,
  finalizing,
  sending,
  streaming,
  streamText,
  streamHtml,
  streamPreview,
  unsubStreamHtml,
  syncStreamText,
  appendStreamText,
  clearStreamText,
  resuming,
  scroller,
  STICK_THRESHOLD,
  TOP_THRESHOLD,
  PAGE_SIZE,
  stickToBottom,
  hasMoreEarlier,
  historyLoading,
  historyLoadFailed,
  isNearBottom,
  onScrollerScroll,
  scrollBottom,
  mergeMessagesKeepPrefix,
  resetHistoryWindowState,
  failedPartialByUserMsgId,
  activeUserMessageId,
  attachments,
  fileInput,
  attachNotice,
  onPickFiles,
  onPaste,
  removeAttachment,
  imagePreview,
  openChatImagePreview,
  closeChatImagePreview,
  enabled,
  turnBusy,
  busy,
  canSend,
  showStreamBubble,
  mainViewState,
  busyHint,
  isPullingBoot,
  sandboxBootStatus,
  suggestions,
  showStreamTypingDots,
  copyAssistantText,
  showThreadsAside,
  showChatSection,
  channelTypeOf,
  isChannelThread,
  channelBadgeLabel,
  channelBadgeClass,
  channelReadonlyTitle,
  channelReadonlyHint,
  threadDisplayTitle,
  channelSourceLine,
  activeThread,
  activeIsChannel,
  activeThreadTitle,
  showEmptyHint,
  showHistoryTip,
  historyTipText,
  historyTipClass,
  showIdleSuggestions,
  channelCtx,
  channelDetailOpen,
  channelDetailTitle,
  channelDetailSource,
  closeChannelCtx,
  openChannelCtx,
  openChannelDetail,
  closeChannelDetail,
  onChannelCtxAction,
  FAIL_KIND_KEYS,
  failMeta,
  applyRestoreMobileChat,
  isFailedUser,
  isChannelHint,
  loadThreads,
  activateThread,
  selectThread,
  backToThreads,
  ensureActiveThread,
  loadMessages,
  loadEarlier,
  retryLoadMessages,
  setFailedPartial,
  clearFailedPartial,
  newThread,
  removeThread,
  closeWs,
  resetTurnLocal,
  patchLocalMessage,
  connectThreadWs,
  handleFrame,
  refreshMessages,
  handleAcp,
  clearFinalizingStream,
  refetchAfterTurnDone,
  onTurnDone,
  onTurnError,
  send,
  stop,
  retryTurn,
  attachmentDisplayName,
  relTime,
  imgSrc,
  renderMarkdown,
  isImageAttachment
  }
}

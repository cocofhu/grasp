import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount, toRaw } from 'vue'
import { useI18n } from 'vue-i18n'
import { renderChatMarkdownBlocks, renderMarkdown, type MarkdownBlockCache } from '@/lib/shared/markdown'
import { createStreamMarkdownPreview } from '@/lib/shared/streamMarkdownPreview'
import { createStreamTextReveal } from '@/lib/run/streamTextReveal'
import { partsFromAcp, sameParts, sameTools, toolsFromAcp } from '@/lib/run/acpTools'
import { expandAutoPhase } from '@/lib/run/takeoverTimeline'
import { mergePersistedAndLiveTurns, persistedCompletedLiveHuman } from '@/lib/inbox/mergeClarifyLiveTurns'
import {
  ackClarifyOutboxText,
  clarifyTabReady,
  initClarifyTab,
  insertRescued,
  type RescuedPart,
  clearClarifyStash,
  readClarifyStash,
  replaceClarifyStash,
  stashClarifyDraft,
  takeClarifyOutbox,
} from '@/lib/inbox/clarifyDraftStash'
import {
  emptyFailDisplayText,
  isEmptyFailedAgent,
  isFailureAssistantText,
  isRetryableFailedAgent,
} from '@/lib/inbox/clarifyEmptyFail'
import { relTime as formatRelTime } from '@/lib/shared/format'
import { holdSidebarConfirmWindow, releaseSidebarConfirmWindow } from '@/lib/inbox/sidebarHandoff'
import {
  demoOptionsOf,
  restoreChoiceAnswers,
  selectedDemoOption,
} from '@/lib/inbox/clarifyDemo'
import type {
  AgentPart,
  AgentTool,
  ClarifyTurn,
  ClarifyImage,
  ReactQuestion,
  ReactOption,
  ReactForm,
  ReactFormField,
  ReactAnnotation,
  AcpEvent,
} from '@/lib/shared/types'
import {
  SITE_ATTACH_MAX_BYTES,
  SITE_ATTACH_MAX_MIB,
  attachmentDisplayName,
  filesFromClipboard,
  findOversizedAttachments,
  formatSelectRejectMessage,
  formatSendRejectMessage,
  isImageAttachment,
  readFilesAsAttachments,
} from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import {
  applyComposerAutoGrow,
  CLARIFY_AUTO_GROW_MAX,
  CLARIFY_AUTO_GROW_MIN,
} from '@/lib/inbox/composerAutoGrow'
import type { Ref } from 'vue'
import { useConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'
import type { ConfirmFlowDiscard } from '@/lib/inbox/confirmFlowDiscard'
import { replySpan } from '@/lib/inbox/turnSpan'
import {
  cloneAnnotations as cloneReactAnnotations,
  cloneImages as cloneClarifyImages,
  dropGhostItems,
  isAuthoritativeIdle,
  reconcileQueue,
  takeTurnBeginItem,
  type SessionFrameItem,
  type SessionQueueItem,
} from '@/lib/chat/sessionQueue'

export type ClarifyChatProps = {
  runId: string
  nodeId: string
  iteration: number
  turns?: import('@/lib/shared/types').ClarifyTurn[] | null
  done: boolean
  active?: boolean
  reviewMode?: boolean
  annotateEnabled?: boolean
  hideFinish?: boolean
  coldSession?: boolean
  finishDisabled?: boolean
  sendLabel?: string
  /** Optional adapter transport: retain the composer until the request is accepted. */
  sendRequest?: (text: string, images: ClarifyImage[], annotations: ReactAnnotation[]) => Promise<boolean>
  confirmError?: string | null
  /** Host set this after a 409 sandbox_busy so the error bar offers abort+confirm. */
  confirmCanAbort?: boolean
  seedHumanText?: string
  seedHumanImages?: ClarifyImage[]
  /** Host hand-off row; while pending, that confirm owns the 校验中 state. */
  handoff?: { phase: 'pending' | 'done' } | null
  /**
   * Row under the hand-off while this conversation follows the run.
   * auto / waiting still own the composer; ended keeps the done footer.
   */
  followTail?: { kind: 'waiting' | 'auto' | 'ended' | 'error' } | null
}

export type ClarifyChatEmit = {
  (e: 'send', text: string, images: ClarifyImage[], annotations: ReactAnnotation[]): void
  (e: 'retry-last'): void
  (e: 'finish'): void
  (e: 'finish-abort'): void
  (e: 'cancel'): void
  (e: 'queue-remove', itemId: string | undefined, index: number): void
  (e: 'queue-reorder', itemIds: string[]): void
}

export type ClarifyChatModels = {
  draft: Ref<string>
  attachments: Ref<ClarifyImage[]>
  annotations: Ref<ReactAnnotation[]>
}

/** Optional `dropped` payload of a clarify_visit_ended error (HTTP 409 / WS). */
export type ClarifyDroppedPayload = {
  clientId?: string
  text?: string
  attachments?: ClarifyImage[]
  images?: ClarifyImage[]
  annotations?: ReactAnnotation[]
}

export function useClarifyChat(
  props: ClarifyChatProps,
  emit: ClarifyChatEmit,
  models: ClarifyChatModels,
) {
const draft = models.draft
const attachments = models.attachments
const annotations = models.annotations

type QueueItem = SessionQueueItem

// active: whether the run is still in an interactive state (queued/running/
// waiting_human). When false (completed/failed/cancelled) the chat input is
// hidden — a finished run must not accept further replies.
// reviewMode: post-run ReAct review of a producer node's product (vs. the
// classic clarify dialogue). Relabels the finish action to "确认并流转";
// session UX (queue/stream/Cancel/refresh) is shared with clarify.
// annotateEnabled: show chip UI without forcing reviewMode finish semantics
// (inbox clarify binds annotations + review-mode affordances but only「发送澄清回复」).
// hideFinish: suppress finish / confirmFlow button (clarify send-only).
// confirmError: review force validation failure shown in the bottom status bar.

const showAnnotationChips = computed(() => props.reviewMode || props.annotateEnabled)

const { t: translate, locale } = useI18n()

// A takeover's opening turn carries the auto phase (turns[0].events): continue it.
const persistedTurns = computed(() => expandAutoPhase(props.turns ?? []))

const thinking = ref(false)
/** Review confirm mid-state: re-validating product (not Agent thinking). */
const validating = ref(false)
/** Success overlay host from ReviewShell (g2.1); optional in unit mounts. */
const confirmFlow = useConfirmFlowCeremony()
/** True after we ran (or parent ran) the success ceremony for the current done cycle. */
let confirmFlowPlayedForDone = false
const confirmFlowPlaying = computed(() => !!confirmFlow?.playing.value)
/**
 * Hand-off still in progress: the session is already marked done, but the
 * bottom must stay the auto-run bar (auto) or the preparing composer
 * (next chat still starting). Ended runs and gates keep the done footer.
 */
const followKeepsComposer = computed(() => {
  const kind = props.followTail?.kind
  return kind === 'auto' || kind === 'waiting'
})
/** Hold done footer until check draw finishes (g2.3), and while a follow still owns the composer. */
const showDoneChrome = computed(
  () => !!props.done && !confirmFlowPlaying.value && !followKeepsComposer.value,
)

async function playConfirmCeremony(): Promise<void> {
  confirmFlowPlayedForDone = true
  if (!confirmFlow) return
  // Same click may play from finishEarly and again from parent force — keep one play (g1.1).
  if (confirmFlow.playing.value) return
  await confirmFlow.play()
}
// SandboxChat-aligned pending-send queue (clarify + review). Items live ONLY in
// the bottom panel until turn_begin materializes transcript bubbles.
const queued = ref<QueueItem[]>([])
/** In-flight turn bubbles (human + streaming agent) before props.turns catch up. */
const liveTurns = ref<ClarifyTurn[]>([])
/** Persisted index where the first live turn begins; older same-text turns cannot dedupe it. */
let liveHistoryAnchor = 0

function anchorForNewLiveHuman(text: string): number {
  const persisted = persistedTurns.value
  const last = persisted[persisted.length - 1]
  // A snapshot may contain this turn's human but not its agent yet. Restrict
  // matching to that trailing human; a completed older pair is not this turn.
  if (last?.role === 'human' && (last.text || '').trim() === (text || '').trim()) {
    return persisted.length - 1
  }
  return persisted.length
}

/** Bridge-reported orphan turn (platform FIFO idle, sandbox still busy). */
const sandboxOrphan = ref<{ runningOpId: string; desynced: boolean } | null>(null)
/** Last queue_state said the platform FIFO is idle (not tracking a turn). */
const platformQueueIdle = ref(false)
const showClarifyEmptyHint = computed(
  () =>
    !props.reviewMode &&
    persistedTurns.value.length === 0 &&
    liveTurns.value.length === 0 &&
    queued.value.length === 0 &&
    !seedHumanTurn.value,
)

function humanMatchesSeed(t: ClarifyTurn, seed: ClarifyTurn): boolean {
  if (t.role !== 'human') return false
  const want = (seed.text || '').trim()
  if (want) return (t.text || '').trim() === want
  return (seed.images?.length || 0) > 0 && (t.images?.length || 0) > 0
}

const seedHumanTurn = computed<ClarifyTurn | null>(() => {
  const text = String(props.seedHumanText || '').trim()
  const images = props.seedHumanImages || []
  if (!text && images.length === 0) return null
  return { role: 'human', text, images, at: '' }
})

function prependSeedHuman(list: ClarifyTurn[]): ClarifyTurn[] {
  const seed = seedHumanTurn.value
  if (!seed) return list
  const seedImgs = seed.images || []
  const idx = list.findIndex((t) => humanMatchesSeed(t, seed))
  if (idx >= 0) {
    const t = list[idx]
    if (seedImgs.length && !(t.images && t.images.length)) {
      const next = list.slice()
      next[idx] = { ...t, images: seedImgs }
      return next
    }
    return list
  }
  return [seed, ...list]
}
const liveAgentIdx = ref(-1)
/** A delayed queue snapshot must not requeue the turn that just finished. */
let lastCompletedItemId: string | undefined
/**
 * Platform FIFO is idle (no waiting item, no active turn). Later ACP is the
 * turn that just lost its streaming slot — paint it, do not ask the host to
 * buffer until the next hard load. Stays false until the first idle
 * queue_state so a hard-load race still buffers.
 */
let idleNarrativeOpen = false
/** Last ACP snapshot written into the open slot, so a replay does not draw it twice. */
let lastAppliedAcpSig = ''
/**
 * paintIdleNarrative wrote a row after the streaming slot closed. A later
 * persisted snapshot is the refresh transcript: once the bridge is idle, that
 * snapshot replaces the idle row. While the bridge is still busy the row stays.
 */
let idlePainted = false
/** Persisted transcript changed after the idle row was painted. */
let persistedMovedAfterIdlePaint = false
type OutputClock = { text: string; startedAt: string; endedAt: string }
/** Page-session wall clocks, kept after the live bubble is replaced by the transcript. */
const outputClocks = ref<OutputClock[]>([])

function rememberOutputClock(row: ClarifyTurn) {
  if (row.role !== 'agent' || !row.startedAt || !row.endedAt) return
  const text = row.text || ''
  const same = outputClocks.value.find((c) => c.startedAt === row.startedAt && c.text === text)
  if (same) {
    same.endedAt = row.endedAt
    return
  }
  outputClocks.value.push({ text, startedAt: row.startedAt, endedAt: row.endedAt })
}

function markOutputStart(row: ClarifyTurn, reset = false) {
  if (reset || !row.startedAt) row.startedAt = new Date().toISOString()
  if (reset) delete row.endedAt
}

function markOutputEnd(row: ClarifyTurn) {
  if (!row.endedAt) row.endedAt = new Date().toISOString()
  rememberOutputClock(row)
}
/**
 * Coalesced per-block markdown HTML for the live streaming agent bubble.
 * Settled blocks keep their string (and DOM); only the open block re-renders.
 */
const liveStreamBlocks = ref<string[]>([])
const liveStreamHtml = computed(() => liveStreamBlocks.value.join(''))
const streamBlockCache: MarkdownBlockCache = new Map()
const streamPreview = createStreamMarkdownPreview<string[]>({
  render: (src) => renderChatMarkdownBlocks(src, streamBlockCache),
  empty: [],
})
const unsubStream = streamPreview.subscribe((blocks) => {
  liveStreamBlocks.value = blocks
})
/** Coalesced thought text (rAF) — same cadence as message preview. */
const liveThoughtText = ref('')
const thoughtPreview = createStreamMarkdownPreview({ render: (s) => s })
const unsubThought = thoughtPreview.subscribe((text) => {
  liveThoughtText.value = text
})
/**
 * Smooth catch-up reveal (Demo) → then markdown/text coalesce.
 * Vitest uses sync so existing mid-stream assertions stay stable.
 */
const syncReveal = Boolean(import.meta.env.VITEST)
const messageReveal = createStreamTextReveal({
  sync: syncReveal,
  onReveal: (text) => {
    streamPreview.setText(text)
    // Vitest: flush markdown coalesce so mid-stream assertions see text without waiting rAF.
    if (syncReveal) streamPreview.flush()
  },
})
const thoughtReveal = createStreamTextReveal({
  sync: syncReveal,
  onReveal: (text) => {
    thoughtPreview.setText(text)
    if (syncReveal) thoughtPreview.flush()
  },
})
/**
 * Manual thought expand/collapse overrides (index → open).
 * Default: open while thought-only streaming; collapsed once message starts / done.
 */
const thoughtOpenOverride = ref<Record<number, boolean>>({})
const scroller = ref<HTMLElement>()
const fileInput = ref<HTMLInputElement | null>(null)
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const overflowScroll = ref(false)
const composing = ref(false)

/** Near-bottom threshold (px), aligned with PmLeaderChat / approved Demo. */
const STICK_THRESHOLD = 48
const stickToBottom = ref(true)
/** Unread real turns while off-bottom; never counts thinking placeholder. */
const unreadCount = ref(0)
const showUnreadFab = computed(() => unreadCount.value >= 1 && !stickToBottom.value)
const unreadFabLabel = computed(() =>
  translate('pages.clarify.unreadFab', { n: unreadCount.value }),
)

const sessionBusy = computed(
  () => thinking.value || queued.value.length > 0 || liveAgentIdx.value >= 0,
)

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= STICK_THRESHOLD
}

function onScrollerScroll() {
  const el = scroller.value
  if (!el) return
  if (isNearBottom(el)) {
    stickToBottom.value = true
    unreadCount.value = 0
  } else {
    stickToBottom.value = false
  }
}

async function scrollBottom(force = false) {
  await nextTick()
  const el = scroller.value
  if (!el) return
  if (force || stickToBottom.value) {
    el.scrollTop = el.scrollHeight
    stickToBottom.value = true
    unreadCount.value = 0
  }
}

/**
 * Enter / remount / session-switch stick sequence (aligned with PmLeaderChat + Demo):
 * pin on entry (scrollBottom already awaits nextTick), then re-pin after paint
 * so tall ReAct / high content blocks that lag one frame still land at the latest message.
 * Must not be used for incremental turn updates — those stay stick-gated.
 */
async function enterStickSequence() {
  stickToBottom.value = true
  unreadCount.value = 0
  await scrollBottom()
  requestAnimationFrame(() => {
    // A message-rail click or manual scroll may have cancelled follow in the meantime.
    void scrollBottom()
  })
}

function onUnreadFabClick() {
  void scrollBottom(true)
}

const AUTO_GROW_MIN = CLARIFY_AUTO_GROW_MIN
const AUTO_GROW_MAX = CLARIFY_AUTO_GROW_MAX

let composerResizeObserver: ResizeObserver | null = null

function autoGrow() {
  const el = textareaRef.value
  if (!el) return
  overflowScroll.value = applyComposerAutoGrow(el, {
    min: AUTO_GROW_MIN,
    max: AUTO_GROW_MAX,
    emptyHint: inputPlaceholder.value,
  })
}

function onTextInput() {
  autoGrow()
}

// Timestamps are relative; without a clock "just now" never ages.
const relClock = ref(0)
const relClockTimer = setInterval(() => relClock.value++, 30_000)
function relTime(iso: string): string {
  void relClock.value
  return formatRelTime(iso)
}

onBeforeUnmount(() => {
  releaseConfirmFlowWindow()
  clearInterval(relClockTimer)
  composerResizeObserver?.disconnect()
  composerResizeObserver = null
  unsubStream()
  unsubThought()
  messageReveal.reset()
  thoughtReveal.reset()
  streamPreview.reset()
  thoughtPreview.reset()
})

/** Choice prefixes the UI and models.FormatChoiceReply emit. Both are recognized in either locale. */
const CHOICE_PREFIX_ZH = '我的选择:'
const CHOICE_PREFIX_EN = 'My choices:'
const choicePrefix = computed(() => translate('pages.clarify.choicePrefix'))
/** zh-CN form-summary prefix, recognized regardless of the viewer's locale. */
const FORM_PREFIX_ZH = '我的填写:'
const formPrefix = computed(() => translate('pages.clarify.formPrefix'))
/** Skip-envelope first line (zh / en) — not a choice reply. */
const SKIP_PREFIX_ZH = '回答已跳过'
const SKIP_USER_LABEL_ZH = '用户回复'
const SKIP_PREFIX_EN = 'Answers skipped'
const SKIP_USER_LABEL_EN = 'User reply'
const skipPrefix = computed(() => translate('pages.clarify.skipPrefix'))
const skipUserReplyLabel = computed(() => translate('pages.clarify.skipUserReplyLabel'))

function textHasChoicePrefix(text: string): boolean {
  return text.startsWith(choicePrefix.value) || text.startsWith(CHOICE_PREFIX_ZH) || text.startsWith(CHOICE_PREFIX_EN)
}

function stripChoicePrefix(text: string): string {
  if (text.startsWith(choicePrefix.value)) return text.slice(choicePrefix.value.length)
  if (text.startsWith(CHOICE_PREFIX_ZH)) return text.slice(CHOICE_PREFIX_ZH.length)
  if (text.startsWith(CHOICE_PREFIX_EN)) return text.slice(CHOICE_PREFIX_EN.length)
  return text
}

function isChoiceReply(text: string): boolean {
  return !!text && textHasChoicePrefix(text)
}

function textHasFormPrefix(text: string): boolean {
  return text.startsWith(formPrefix.value) || text.startsWith(FORM_PREFIX_ZH)
}

function stripFormPrefix(text: string): string {
  if (text.startsWith(formPrefix.value)) return text.slice(formPrefix.value.length)
  if (text.startsWith(FORM_PREFIX_ZH)) return text.slice(FORM_PREFIX_ZH.length)
  return text
}

function isFormReply(text: string): boolean {
  return !!text && textHasFormPrefix(text)
}

function textHasSkipPrefix(text: string): boolean {
  if (!text) return false
  return (
    text.startsWith(skipPrefix.value + '\n') ||
    text.startsWith(SKIP_PREFIX_ZH + '\n') ||
    text.startsWith(SKIP_PREFIX_EN + '\n')
  )
}

function isSkipReply(text: string): boolean {
  return textHasSkipPrefix(text)
}

/** Three-line skip envelope: label / user-label / original (may be empty or multiline). */
function formatSkipReply(userText: string): string {
  return `${skipPrefix.value}\n${skipUserReplyLabel.value}\n${userText}`
}

/**
 * A human turn / queue item that closes the current ask_question card:
 * structured choice, skip envelope, free text, or any attachment.
 */
function closesQuestionRound(
  text: string,
  images?: ClarifyImage[] | null,
  anns?: ReactAnnotation[] | null,
): boolean {
  if (isChoiceReply(text) || isFormReply(text) || isSkipReply(text)) return true
  if ((images?.length || 0) > 0 || (anns?.length || 0) > 0) return true
  return !!(text || '').trim()
}

/** Index of the latest agent turn that raised structured questions. */
function latestQuestionTurnIndex(turnList: ClarifyTurn[]): number {
  for (let i = turnList.length - 1; i >= 0; i--) {
    const t = turnList[i]
    if (t.role === 'agent' && t.questions?.length) return i
  }
  return -1
}

/** Index of the latest agent turn that raised ask_form forms. */
function latestFormTurnIndex(turnList: ClarifyTurn[]): number {
  for (let i = turnList.length - 1; i >= 0; i--) {
    const t = turnList[i]
    if (t.role === 'agent' && t.forms?.length) return i
  }
  return -1
}

function hasHumanReplyAfter(turnList: ClarifyTurn[], qIdx: number): boolean {
  for (let i = qIdx + 1; i < turnList.length; i++) {
    const t = turnList[i]
    if (t.role === 'human' && closesQuestionRound(t.text, t.images, t.annotations)) return true
  }
  return false
}

const latestQuestionIdx = computed(() => latestQuestionTurnIndex(persistedTurns.value))

const latestQuestions = computed<ReactQuestion[]>(() => {
  const idx = latestQuestionIdx.value
  if (idx < 0) return []
  return persistedTurns.value[idx].questions ?? []
})

/** Persisted transcript plus in-flight live bubbles (no sessionStorage echo). */
function answerTranscript(): ClarifyTurn[] {
  if (liveTurns.value.length) {
    return mergePersistedAndLiveTurns(persistedTurns.value, liveTurns.value, liveHistoryAnchor)
  }
  return persistedTurns.value
}

const latestQuestionAnswered = computed(() => {
  const list = answerTranscript()
  const qIdx = latestQuestionTurnIndex(list)
  if (qIdx < 0) return latestQuestions.value.length === 0
  if (hasHumanReplyAfter(list, qIdx)) return true
  return queued.value.some((q) => closesQuestionRound(q.text, q.images, q.annotations))
})

/** Unanswered ask_question round is live — composer send becomes skip (not default choice). */
const pendingQuestionsOpen = computed(
  () =>
    !props.done &&
    !!props.active &&
    latestQuestions.value.length > 0 &&
    !latestQuestionAnswered.value,
)

const latestFormIdx = computed(() => latestFormTurnIndex(persistedTurns.value))

const latestForms = computed<ReactForm[]>(() => {
  const idx = latestFormIdx.value
  if (idx < 0) return []
  return persistedTurns.value[idx].forms ?? []
})

const latestFormAnswered = computed(() => {
  const list = answerTranscript()
  const fIdx = latestFormTurnIndex(list)
  if (fIdx < 0) return latestForms.value.length === 0
  if (hasHumanReplyAfter(list, fIdx)) return true
  return queued.value.some((q) => closesQuestionRound(q.text, q.images, q.annotations))
})

/** Unanswered ask_form round — composer free-text send wraps as skip envelope. */
const pendingFormsOpen = computed(
  () =>
    !props.done &&
    !!props.active &&
    latestForms.value.length > 0 &&
    !latestFormAnswered.value,
)

const pendingInteractiveOpen = computed(
  () => pendingQuestionsOpen.value || pendingFormsOpen.value,
)

const inputPlaceholder = computed(() => {
  if (pendingInteractiveOpen.value) return translate('pages.clarify.skipInputPlaceholder')
  if (props.reviewMode) return translate('pages.clarify.reviewInputPlaceholder')
  return translate('pages.clarify.clarifyInputPlaceholder')
})

const displayTurns = computed<ClarifyTurn[]>(() => {
  const list = persistedTurns.value
  // Session UX: live in-flight bubbles until persisted transcript catches up.
  // Dedupe against persisted turns so mid-stream softRefresh/loadRun human does not double-render.
  if (liveTurns.value.length) {
    return prependSeedHuman(mergePersistedAndLiveTurns(list, liveTurns.value, liveHistoryAnchor))
  }
  return prependSeedHuman(list)
})

/** Start/end for each visible agent reply: its own stamps, else this page's remembered clock. */
const outputBoundsByIndex = computed(() => {
  const turns = displayTurns.value
  const pool = outputClocks.value.map((c) => ({ ...c }))
  const out = new Map<number, { startedAt: string; endedAt: string }>()
  for (let i = turns.length - 1; i >= 0; i--) {
    const t = turns[i]!
    if (t.role !== 'agent') continue
    if (t.startedAt && t.endedAt) {
      out.set(i, { startedAt: t.startedAt, endedAt: t.endedAt })
      const idx = pool.findIndex((c) => c.startedAt === t.startedAt && c.text === (t.text || ''))
      if (idx >= 0) pool.splice(idx, 1)
      continue
    }
    const text = t.text || ''
    let c = pool.length - 1
    while (c >= 0 && pool[c]!.text !== text) c--
    if (c >= 0) {
      out.set(i, { startedAt: pool[c]!.startedAt, endedAt: pool[c]!.endedAt })
      pool.splice(c, 1)
      continue
    }
    const span = replySpan(turns[i - 1], t)
    if (span) out.set(i, span)
  }
  return out
})

function outputBounds(index: number): { startedAt?: string; endedAt?: string } {
  return outputBoundsByIndex.value.get(index) || {}
}

/** Single-image lightbox for human history attachments (no gallery / Esc). */
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview: closeImagePreview } =
  useChatImagePreview()

function imagePreviewLabel(images: ClarifyImage[], index: number): string {
  const named = images[index]?.name?.trim()
  if (named) return named
  if (images.length <= 1) return translate('common.chatImage.imageFallback')
  return translate('common.chatImage.imageFallbackN', { n: index + 1 })
}

const attachNotice = ref<string | null>(null)
/** A reply the server refused (e.g. clarify_visit_ended); its text is back in the composer. */
const sendNotice = ref<string | null>(null)
const queueNotice = ref<string | null>(null)
const queueToast = ref<string | null>(null)
let queueToastTimer = 0

function hasComposerDraft(): boolean {
  return !!(draft.value.trim() || attachments.value.length > 0 || annotations.value.length > 0)
}

function truncateQueueText(text: string, max: number): string {
  const s = String(text || '')
  return s.length > max ? s.slice(0, max) + '…' : s
}

function showQueueToast(msg: string) {
  queueToast.value = msg
  clearTimeout(queueToastTimer)
  queueToastTimer = window.setTimeout(() => {
    queueToast.value = null
  }, 2200)
}

function syncQueueThinking() {
  thinking.value = liveAgentIdx.value >= 0 || queued.value.length > 0
}

function cancelQueuedItem(index: number) {
  if (index < 0 || index >= queued.value.length) return
  const removed = queued.value.splice(index, 1)[0]
  syncQueueThinking()
  const preview = truncateQueueText(removed.text || '', 18)
  showQueueToast(translate('pages.clarify.queueCancelled', { text: preview }))
  if (removed.id) emit('queue-remove', removed.id, index)
}

function reorderQueuedItems(fromIndex: number, toIndex: number) {
  if (
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= queued.value.length ||
    toIndex >= queued.value.length ||
    fromIndex === toIndex
  ) {
    return
  }
  const [moved] = queued.value.splice(fromIndex, 1)
  queued.value.splice(toIndex, 0, moved)
  showQueueToast(translate('pages.clarify.queueReordered'))
  const ids = queued.value.map((q) => q.id).filter((id): id is string => !!id)
  if (ids.length === queued.value.length && ids.length > 0) {
    emit('queue-reorder', ids)
  }
}

async function editQueuedItem(index: number) {
  if (index < 0 || index >= queued.value.length) return
  const target = queued.value[index]
  queueNotice.value = null

  // Composer already has an unsent draft (text / attachments / annotation chips):
  // re-enqueue it first so chips are never discarded to unblock edit.
  if (hasComposerDraft() && props.sendRequest) {
    if (!await sendFromComposer()) return
    // A new draft typed during acknowledgement must stay with its author.
    if (hasComposerDraft()) return
    index = queued.value.findIndex((item) => target.id ? item.id === target.id : item === target)
    if (index < 0) return
  } else if (hasComposerDraft()) {
    const t = draft.value.trim()
    const imgs = cloneClarifyImages(attachments.value)
    const anns = cloneReactAnnotations(annotations.value)
    const prepared = prepareComposerSend(t, imgs, anns)
    if (!prepared) return
    draft.value = ''
    attachments.value = []
    annotations.value = []
    attachNotice.value = null
    sendMessage(prepared.text, prepared.images, prepared.annotations, t)
    // Append keeps the target index stable.
  }

  const item = queued.value.splice(index, 1)[0]
  draft.value = item.text
  attachments.value = cloneClarifyImages(item.images)
  annotations.value = cloneReactAnnotations(item.annotations)
  attachNotice.value = null
  syncQueueThinking()
  nextTick(autoGrow)
  textareaRef.value?.focus()
  showQueueToast(translate('pages.clarify.queueEditRefilled'))
  if (item.id) emit('queue-remove', item.id, index)
}

watch(draft, () => {
  if (!hasComposerDraft()) queueNotice.value = null
})

function openImagePreview(images: ClarifyImage[], index: number) {
  const im = images[index]
  if (!im || !isImageAttachment(im)) return
  openChatImagePreview(imgSrc(im), imagePreviewLabel(images, index))
}

function turnsSemanticKey(turnList: ClarifyTurn[]): string {
  if (!turnList.length) return '0'
  const last = turnList[turnList.length - 1]
  return `${turnList.length}:${last.at}:${last.role}:${last.text}`
}

/** Later turns make a persisted live prefix historical, even if its rails differ. */
function prunePersistedLiveHistory(persisted: ClarifyTurn[]) {
  let keepFrom = 0
  let nextAnchor = liveHistoryAnchor
  while (keepFrom < liveTurns.value.length) {
    const human = liveTurns.value[keepFrom]
    const text = (human?.text || '').trim()
    if (human?.role !== 'human' || !text) break
    let persistedHuman = -1
    for (let i = nextAnchor; i < persisted.length; i++) {
      if (persisted[i].role === 'human' && (persisted[i].text || '').trim() === text) {
        persistedHuman = i
        break
      }
    }
    if (persistedHuman < 0) break
    const reply = persisted[persistedHuman + 1]
    if (
      reply?.role !== 'agent' || reply.streaming ||
      (!(reply.text || reply.thought) && !isRetryableFailedAgent(reply))
    ) break
    const nextHuman = liveTurns.value.findIndex((turn, i) => i > keepFrom && turn.role === 'human')
    const persistedAdvanced = persisted.slice(persistedHuman + 2).some((turn) => turn.role === 'human')
    if (nextHuman < 0 && !persistedAdvanced) break
    nextAnchor = persistedHuman + 2
    keepFrom = nextHuman < 0 ? liveTurns.value.length : nextHuman
  }
  if (!keepFrom) return
  if (keepFrom === liveTurns.value.length) {
    clearSettledLiveTurns()
    return
  }
  liveHistoryAnchor = nextAnchor
  liveTurns.value = liveTurns.value.slice(keepFrom)
  liveAgentIdx.value = liveAgentIdx.value >= keepFrom ? liveAgentIdx.value - keepFrom : -1
}

// Real turns streamed back from the run: drop optimistic choice echo + clear
// settled live bubbles. Scroll only when stuck to bottom.
watch(
  () => turnsSemanticKey(persistedTurns.value),
  (key, prevKey) => {
    if (prevKey !== undefined && key === prevKey) return
    if (prevKey !== undefined && idlePainted) persistedMovedAfterIdlePaint = true
    // While the sandbox is still writing, keep the open row even if a refresh
    // snapshot already has the same short reply.
    dropSettledLiveIfPersistedWins()
    void scrollBottom()
  },
)

// Exact unread from props.turns length deltas (ignores optimistic pending / thinking).
watch(
  () => persistedTurns.value.length,
  (len, prevLen) => {
    if (prevLen === undefined) return
    const delta = len - prevLen
    if (delta <= 0) {
      if (stickToBottom.value) unreadCount.value = 0
      return
    }
    if (stickToBottom.value) {
      unreadCount.value = 0
      void scrollBottom()
    } else {
      unreadCount.value += delta
    }
  },
)

// Session identity change: treat as enter — force pin + rAF second pin.
watch(
  () => `${props.runId}.${props.nodeId}.${props.iteration}`,
  () => {
    idleNarrativeOpen = false
    idlePainted = false
    persistedMovedAfterIdlePaint = false
    lastAppliedAcpSig = ''
    platformQueueIdle.value = false
    sandboxOrphan.value = null
    lastCompletedItemId = undefined
    void enterStickSequence()
  },
)

// Thinking / validating placeholders follow only while stuck to bottom; never ++unread.
watch([thinking, validating], ([th, val], [prevTh, prevVal]) => {
  if ((th && !prevTh) || (val && !prevVal)) {
    if (stickToBottom.value) void scrollBottom()
  }
})
watch(
  () => props.done,
  (d) => {
    if (d) {
      thinking.value = false
      validating.value = false
      // Overlay is owned by the click path (finishEarly / parent force). Never
      // auto-play on done/node_complete (plan g1.1) — that caused the #613 regression.
    } else {
      confirmFlowPlayedForDone = false
      confirmFlow?.reset()
    }
  },
)
// A rejected「确认并流转」must hand control back: both spinners are local state
// the server can no longer clear (the dialogue stays open, so props.done never
// flips), and leaving either on strands the user with a permanent placeholder.
watch(
  () => props.confirmError,
  (err) => {
    if (!err) return
    // The host hand-off row is still pending: that confirm owns the spinner.
    // A parallel reply (often "react already done") must not turn the button
    // back into a clickable 确认并流转 that then no-ops.
    if (props.handoff?.phase === 'pending') return
    validating.value = false
    if (queued.value.length === 0 && liveAgentIdx.value < 0) thinking.value = false
  },
)

// Hosts call this when a confirm click does not send, or when the same
// confirmError is reported again (the watch above only runs on a change).
function releaseConfirmPending() {
  validating.value = false
}

function addFiles(files: ArrayLike<File> | null | undefined) {
  if (!files) return
  const { rejected } = readFilesAsAttachments(files, {
    maxBytes: SITE_ATTACH_MAX_BYTES,
    onRead: ({ data, mimeType, name }) => attachments.value.push({ data, mimeType, name }),
  })
  if (rejected.length) {
    attachNotice.value = formatSelectRejectMessage(rejected, SITE_ATTACH_MAX_MIB)
  } else {
    attachNotice.value = null
  }
}
function onPickFiles(e: Event) {
  addFiles((e.target as HTMLInputElement).files)
  if (fileInput.value) fileInput.value.value = ''
}
function onPaste(e: ClipboardEvent) {
  const picked = filesFromClipboard(e)
  if (picked.length) {
    e.preventDefault()
    addFiles(picked)
  }
  nextTick(autoGrow)
}

watch([draft, inputPlaceholder], () => nextTick(autoGrow), { immediate: true })
onMounted(() => {
  nextTick(() => {
    autoGrow()
    const el = textareaRef.value
    if (el && typeof ResizeObserver !== 'undefined') {
      composerResizeObserver?.disconnect()
      composerResizeObserver = new ResizeObserver(() => autoGrow())
      composerResizeObserver.observe(el)
    }
  })
  // Mount with historical turns: force enter stick (parent v-if remounts on leave/re-enter).
  void enterStickSequence()
})
function removeAttachment(i: number) {
  attachments.value.splice(i, 1)
}

/** Composer text behind an optimistic row (before any skip-envelope wrap). */
const composerOrigin = new WeakMap<object, string>()

function sendMessage(
  text: string,
  imgs: ClarifyImage[] = [],
  anns: ReactAnnotation[] = [],
  composerText?: string,
) {
  // Skip envelopes keep a trailing newline so an empty third line stays present
  // (trim would collapse "回答已跳过\n用户回复\n" → two lines).
  const t = textHasSkipPrefix(text) ? text.replace(/^\s+/, '') : text.trim()
  if ((!t && imgs.length === 0 && anns.length === 0) || props.done || !props.active) return
  // Enqueue only — bubbles materialize on turn_begin (AgentChatTester / Demo).
  // Busy may still enqueue; never open a concurrent turn via optimistic pending.
  const item: QueueItem = { text: t, images: imgs, annotations: cloneReactAnnotations(anns) }
  if (composerText !== undefined) composerOrigin.set(item, composerText)
  queued.value.push(item)
  thinking.value = true
  emit('send', t, imgs, anns)
  stickToBottom.value = true
  unreadCount.value = 0
  void scrollBottom(true)
}

/**
 * Prepare composer payload: when ask_question cards are unanswered, wrap text
 * as the skip envelope and clear pre-checked options. Images/annotations stay
 * intact (never emptied by the envelope wrapper).
 */
function prepareComposerSend(
  text: string,
  imgs: ClarifyImage[],
  anns: ReactAnnotation[],
): { text: string; images: ClarifyImage[]; annotations: ReactAnnotation[] } | null {
  if ((!text && imgs.length === 0 && anns.length === 0) || props.done || !props.active) return null
  const over = findOversizedAttachments(imgs)
  if (over.length) {
    attachNotice.value = formatSendRejectMessage(
      over.map((im, i) => attachmentDisplayName(im, i)),
      SITE_ATTACH_MAX_MIB,
    )
    return null
  }
  let outText = text
  if (pendingInteractiveOpen.value) {
    outText = formatSkipReply(text)
    sel.value = {}
    other.value = {}
    otherChecked.value = {}
    step.value = 0
    formValues.value = {}
    formNotice.value = null
  }
  return { text: outText, images: imgs, annotations: anns }
}

const composerSending = ref(false)

async function sendFromComposer(): Promise<boolean> {
  if (composerSending.value) return false
  // In-flight 确认并流转 owns the primary slot. Drafts stay local until the
  // product check returns — a parallel reply would be dropped as "already done".
  if (validating.value || props.handoff?.phase === 'pending') return false
  const t = draft.value.trim()
  const imgs = cloneClarifyImages(attachments.value)
  const anns = cloneReactAnnotations(annotations.value)
  const prepared = prepareComposerSend(t, imgs, anns)
  if (!prepared) return false
  if (props.sendRequest) {
    // Keep drafts and attachment references until an acknowledgement arrives.
    // A new draft typed while awaiting the transport belongs to the next send.
    const originalDraft = draft.value
    const originalImages = attachments.value
    const originalAnnotations = annotations.value
    composerSending.value = true
    try {
      if (!await props.sendRequest(prepared.text, prepared.images, prepared.annotations)) return false
      if (draft.value === originalDraft) draft.value = ''
      if (attachments.value === originalImages && JSON.stringify(attachments.value) === JSON.stringify(imgs)) attachments.value = []
      if (annotations.value === originalAnnotations && JSON.stringify(annotations.value) === JSON.stringify(anns)) annotations.value = []
      attachNotice.value = null
      sendNotice.value = null
      if (!hasComposerDraft()) releaseStash()
    } finally {
      composerSending.value = false
    }
    return true
  }
  draft.value = ''
  attachments.value = []
  annotations.value = []
  attachNotice.value = null
  sendNotice.value = null
  releaseStash()
  // Always forward cloned images/annotations — skip only rewrites text.
  sendMessage(prepared.text, prepared.images, prepared.annotations, t)
  return true
}

function onComposerKeydown(e: KeyboardEvent) {
  if (e.key !== 'Enter' || e.shiftKey) return
  // IME: composition Enter / keyCode 229 must not send (prevents double-fire).
  if (composing.value || e.isComposing || e.keyCode === 229) return
  e.preventDefault()
  send()
}

function removeAnnotation(i: number) {
  annotations.value.splice(i, 1)
}

// --- structured questions (ask_question) ---------------------------------
// Selected option ids per question, plus optional "其他" free text.
const sel = ref<Record<string, string[]>>({})
const other = ref<Record<string, string>>({})
/** Per-question "其他" checkbox — free text only counts when checked. */
const otherChecked = ref<Record<string, boolean>>({})

// The interactive choice card is only shown for the latest unanswered agent
// question turn while the dialogue is live (not done / not mid-send).
const activeQuestions = computed<ReactQuestion[]>(() => {
  if (props.done || !props.active || thinking.value) return []
  if (latestQuestionAnswered.value) return []
  return latestQuestions.value
})

const activeForms = computed<ReactForm[]>(() => {
  if (props.done || !props.active || thinking.value) return []
  if (latestFormAnswered.value) return []
  return latestForms.value
})

/** Keyed by `${formIndex}:${fieldName}` — plaintext values (never masked). */
const formValues = ref<Record<string, string>>({})
const formNotice = ref<string | null>(null)

function formFieldKey(formIndex: number, name: string): string {
  return `${formIndex}:${name}`
}

function fieldWhy(f: ReactFormField): string {
  return (f.why || f.reason || '').trim()
}

/** Input type for ask_form: text|url only — never password. */
function formInputType(f: ReactFormField): 'text' | 'url' {
  return String(f.type || '').toLowerCase() === 'url' ? 'url' : 'text'
}

function seedFormValues(forms: ReactForm[]) {
  const next: Record<string, string> = {}
  forms.forEach((form, fi) => {
    for (const f of form.fields || []) {
      const k = formFieldKey(fi, f.name)
      next[k] = formValues.value[k] ?? (f.value || '')
    }
  })
  formValues.value = next
}

watch(
  () =>
    activeForms.value
      .map((f, i) => `${i}:${(f.fields || []).map((x) => x.name).join(',')}`)
      .join('|'),
  (key) => {
    formNotice.value = null
    if (!key) {
      formValues.value = {}
      return
    }
    seedFormValues(activeForms.value)
  },
)

function formFieldsFilled(forms: ReactForm[]): boolean {
  for (let fi = 0; fi < forms.length; fi++) {
    for (const f of forms[fi].fields || []) {
      if (!(formValues.value[formFieldKey(fi, f.name)] || '').trim()) return false
    }
  }
  return forms.some((f) => (f.fields || []).length > 0)
}

const formCanSubmit = computed(
  () => activeForms.value.length > 0 && formFieldsFilled(activeForms.value) && !thinking.value,
)

function submitForms() {
  const forms = activeForms.value
  if (!forms.length || thinking.value) return
  if (!formFieldsFilled(forms)) {
    formNotice.value = translate('pages.clarify.formEmptyNotice')
    return
  }
  formNotice.value = null
  const lines: string[] = []
  forms.forEach((form, fi) => {
    for (const f of form.fields || []) {
      const label = (f.label || f.name || '').trim() || f.name
      const val = (formValues.value[formFieldKey(fi, f.name)] || '').trim()
      lines.push(`- ${label} → ${val}`)
    }
  })
  const text = formPrefix.value + '\n' + lines.join('\n')
  formValues.value = {}
  sendMessage(text)
}

interface FormRow {
  label: string
  value: string
}
function parseFormSummary(text: string): FormRow[] | null {
  if (!text || !textHasFormPrefix(text)) return null
  const bodyText = stripFormPrefix(text)
  const rows: FormRow[] = []
  for (const raw of bodyText.split('\n')) {
    const line = raw.trim()
    if (!line.startsWith('- ')) continue
    const body = line.slice(2)
    const idx = body.indexOf('→')
    if (idx === -1) continue
    const label = body.slice(0, idx).trim()
    const value = body.slice(idx + 1).trim()
    if (label) rows.push({ label, value })
  }
  return rows.length ? rows : null
}

function isActiveTurn(i: number): boolean {
  return activeQuestions.value.length > 0 && i === latestQuestionIdx.value
}
function isActiveFormTurn(i: number): boolean {
  return activeForms.value.length > 0 && i === latestFormIdx.value
}
function isSelected(qidStr: string, oid: string): boolean {
  return (sel.value[qidStr] || []).includes(oid)
}
function toggle(q: ReactQuestion, oid: string) {
  const cur = sel.value[q.id] || []
  if (q.allowMultiple) {
    sel.value[q.id] = cur.includes(oid) ? cur.filter((x) => x !== oid) : [...cur, oid]
    return
  }
  // Single-select domain includes preset options and 「其他」. Replacing or
  // clearing a preset must drop the other checkbox and must not restore it.
  sel.value[q.id] = cur.includes(oid) ? [] : [oid]
  otherChecked.value[q.id] = false
}
function isOtherSelected(qid: string): boolean {
  return !!otherChecked.value[qid]
}

function toggleOther(q: ReactQuestion) {
  const next = !otherChecked.value[q.id]
  otherChecked.value[q.id] = next
  // Checking 「其他」 on a single-select question clears presets. Unchecking
  // leaves sel empty so the previous preset does not come back. Typing in the
  // input never checks this box; residual text stays in `other` either way.
  if (next && !q.allowMultiple) sel.value[q.id] = []
}

function otherAnswered(q: ReactQuestion): boolean {
  return !!otherChecked.value[q.id] && !!other.value[q.id]?.trim()
}

function answered(q: ReactQuestion): boolean {
  return (sel.value[q.id]?.length ?? 0) > 0 || otherAnswered(q)
}
const someAnswered = computed(() => activeQuestions.value.some(answered))

// --- recommended options -------------------------------------------------
// Auto-pick id for a question: the recommended option, or the first as a
// fallback (mirrors the backend auto_var / SelectRecommendedOption rule).
function autoPickId(q: ReactQuestion): string {
  const rec = q.options.find((o) => o.recommended)
  return rec ? rec.id : (q.options[0]?.id ?? '')
}
// Whether any active question carries an explicit recommendation — gates the
// "采用推荐" button and auto-recommend affordances.
const hasRecommended = computed(() => activeQuestions.value.some((q) => q.options.some((o) => o.recommended)))

// Fill sel with the recommended (or first) option for every active question.
function applyRecommended() {
  for (const q of activeQuestions.value) {
    const id = autoPickId(q)
    sel.value[q.id] = id ? [id] : []
    // Multi-select still fills only the one recommended (or first) preset and
    // leaves an already-checked 「其他」 alone. Single-select drops that check.
    if (!q.allowMultiple) otherChecked.value[q.id] = false
  }
}
function submitRecommended() {
  if (!activeQuestions.value.length || thinking.value) return
  applyRecommended()
  submitChoices()
}

// --- card deck (one question per card, 上一题 / 下一张) ---------------------
const step = ref(0)
// Only a genuinely new question set (different question ids) resets the deck to
// the first card. Keyed by ids — not array identity — so that background run
// polling (which rebuilds `turns` into fresh objects each tick) does NOT snap
// the user back to the first question mid-answer.
watch(
  () => activeQuestions.value.map((q) => q.id).join('|'),
  (key) => {
    step.value = 0
    if (!key) return
    // Preselect the recommended option (if any) so the user only
    // confirms; never auto-pick the first when nothing was recommended.
    // immediate: run detail and the gate mount this chat only after the
    // current question is already in props, so the id key never "changes".
    for (const q of activeQuestions.value) {
      const rec = q.options.find((o) => o.recommended)
      if (rec && !(sel.value[q.id]?.length ?? 0)) sel.value[q.id] = [rec.id]
    }
  },
  { immediate: true },
)
const curQuestion = computed<ReactQuestion | null>(() => activeQuestions.value[step.value] || null)
const isFirstCard = computed(() => step.value <= 0)
const isLastCard = computed(() => step.value >= activeQuestions.value.length - 1)

function prevCard() {
  if (step.value > 0) step.value--
}
function nextCard() {
  if (step.value < activeQuestions.value.length - 1) step.value++
}
// Pick an option. Never auto-advances — the user moves on with 下一个.
function pick(q: ReactQuestion, oid: string) {
  toggle(q, oid)
}

function submitChoices() {
  const qs = activeQuestions.value
  if (!qs.length || !someAnswered.value || thinking.value) return
  // Skipped questions are omitted; only answered ones are summarized.
  const lines = qs.filter(answered).map((q) => {
    const picked = q.options.filter((o) => (sel.value[q.id] || []).includes(o.id)).map((o) => o.label)
    const extra = otherChecked.value[q.id] ? other.value[q.id]?.trim() : ''
    if (extra) picked.push(extra)
    return `- ${q.prompt} → ${picked.join('、')}`
  })
  const text = choicePrefix.value + '\n' + lines.join('\n')
  sel.value = {}
  other.value = {}
  otherChecked.value = {}
  step.value = 0
  sendMessage(text)
}

// A submitted "我的选择:" / "My choices:" human turn is rendered as structured
// cards instead of a raw markdown bullet list. A bullet may wrap: lines that
// do not start another "- " stay in the same item, then the first "→" splits
// the prompt from answers. Returns null for any other message.
interface ChoiceRow {
  q: string
  answers: string[]
}
function parseChoiceSummary(text: string): ChoiceRow[] | null {
  if (!text || !textHasChoicePrefix(text)) return null
  const bodyText = stripChoicePrefix(text)
  const chunks: string[] = []
  let buf: string[] | null = null
  for (const raw of bodyText.split('\n')) {
    const line = raw.replace(/\r$/, '')
    const trimmed = line.trim()
    if (trimmed.startsWith('- ')) {
      if (buf) chunks.push(buf.join('\n'))
      buf = [trimmed.slice(2)]
      continue
    }
    if (buf) buf.push(line)
  }
  if (buf) chunks.push(buf.join('\n'))

  const rows: ChoiceRow[] = []
  for (const chunk of chunks) {
    const idx = chunk.indexOf('→')
    if (idx === -1) continue
    const q = chunk.slice(0, idx).trim()
    const answers = chunk
      .slice(idx + 1)
      .split('、')
      .map((a) => a.trim())
      .filter(Boolean)
    if (q) rows.push({ q, answers })
  }
  return rows.length ? rows : null
}

function choiceRowsForAgentTurn(turnIndex: number): ChoiceRow[] | null {
  for (let j = turnIndex + 1; j < displayTurns.value.length; j++) {
    const turn = displayTurns.value[j]
    if (turn.role === 'human') {
      const parsed = parseChoiceSummary(turn.text)
      if (parsed) return parsed
    }
    if (turn.role === 'agent' && turn.questions?.length) break
  }
  return null
}

function selectedLabelsForQuestion(q: ReactQuestion, rows: ChoiceRow[] | null): string[] {
  if (!rows) return []
  const row = rows.find((r) => r.q === q.prompt)
  if (!row) return []
  // Saved text still splits on every dunhao. Restore whole labels before highlighting.
  return restoreChoiceAnswers(row.answers, q.options.map((option) => option.label))
}

function selectedDemoForInteractive(q: ReactQuestion): ReactOption | null {
  return q.options.find((o) => isSelected(q.id, o.id) && !!o.demoHtml?.trim()) ?? null
}

function send() {
  sendFromComposer()
}

const confirmFlowDialogOpen = ref(false)
const confirmFlowDiscard = ref<ConfirmFlowDiscard>('none')
let pendingFlowConfirm: 'finish' | 'abort' | null = null

function canFinishAbort() {
  return !(props.done || !props.confirmCanAbort || composerSending.value ||
    (!props.coldSession && hasComposerDraft()))
}

function canFinishEarly() {
  if (props.done || (!props.active && !props.coldSession)) return false
  if (validating.value || confirmDisabled.value) return false
  return true
}

function requestFlowConfirm(kind: 'finish' | 'abort') {
  if (confirmFlowDialogOpen.value) return
  pendingFlowConfirm = kind
  // Clarify keeps a hidden cold-session draft; confirm does not discard it.
  confirmFlowDiscard.value = 'none'
  confirmFlowDialogOpen.value = true
}

function dismissConfirmFlow() {
  confirmFlowDialogOpen.value = false
  pendingFlowConfirm = null
}

function acceptConfirmFlow() {
  if (!confirmFlowDialogOpen.value || !pendingFlowConfirm) return
  const kind = pendingFlowConfirm
  confirmFlowDialogOpen.value = false
  pendingFlowConfirm = null
  if (kind === 'abort') commitFinishAbort()
  else commitFinishEarly()
}

// Clarify: force Agent wrap-up (disabled while thinking).
// Review: accept store snapshot only when ready (not thinking / queue empty).
function finishAbort() {
  if (!canFinishAbort()) return
  requestFlowConfirm('abort')
}

/** Run whose confirm window is still up. Released when that window leaves or this chat unmounts. */
let heldConfirmRunId = ''

function holdConfirmWindow() {
  heldConfirmRunId = props.runId
  holdSidebarConfirmWindow(props.runId)
}

function releaseConfirmFlowWindow() {
  const runId = heldConfirmRunId
  heldConfirmRunId = ''
  if (runId) releaseSidebarConfirmWindow(runId)
}

function commitFinishAbort() {
  if (!canFinishAbort()) return
  holdConfirmWindow()
  emit('finish-abort')
}

function finishEarly() {
  if (!canFinishEarly()) return
  requestFlowConfirm('finish')
}

function commitFinishEarly() {
  if (!canFinishEarly()) return
  // Click intent: play overlay only after the in-app confirm; parent force must not wait for done (g1.1).
  // The sidebar hold is armed before the parent hears finish, so the row cannot fly while the window is up.
  holdConfirmWindow()
  validating.value = true
  // Hand-off progress is shown inline in the transcript (no full-desk overlay).
  emit('finish')
  void scrollBottom()
}

const confirmDisabled = computed(() => {
  // Force finish / 确认并流转: only when session idle (no in-flight / queue).
  return !!props.finishDisabled || composerSending.value ||
    (!props.coldSession && hasComposerDraft()) ||
    validating.value || thinking.value || queued.value.length > 0 || liveAgentIdx.value >= 0
})

function cancelReview() {
  if (props.done) return
  const orphan = !!sandboxOrphan.value
  // Orphan banner is shown only while the platform FIFO is idle, so sessionBusy
  // is false — still emit cancel so the host aborts the sandbox turn.
  if (!sessionBusy.value && !orphan) return
  // Review: clear local queue (matches CancelReviewSession clear-queue).
  // Clarify: keep local queue; authoritative queue_state will reconcile (Demo).
  if (props.reviewMode) {
    queued.value = []
  }
  if (liveAgentIdx.value >= 0 && liveTurns.value[liveAgentIdx.value]) {
    const row = liveTurns.value[liveAgentIdx.value]
    row.streaming = false
    row.interrupted = true
    if (!row.text) row.text = '(已中断)'
    markOutputEnd(row)
  }
  liveAgentIdx.value = -1
  messageReveal.reset()
  thoughtReveal.reset()
  streamPreview.reset()
  thoughtPreview.reset()
  thinking.value = queued.value.length > 0
  if (orphan) sandboxOrphan.value = null
  emit('cancel')
  void scrollBottom()
}

/**
 * Parent calls this when reactReply(force=false) fails after optimistic enqueue,
 * so the pending-send panel and FR4 confirm gate do not stick forever.
 */
function discardLastQueued() {
  if (queued.value.length === 0) return
  queued.value.pop()
  if (queued.value.length === 0 && liveAgentIdx.value < 0) {
    thinking.value = false
  }
}

/** The composer is on screen and accepts input. */
function composerOpen(): boolean {
  return !props.done && !!props.active && !props.coldSession
}

/**
 * Put text/attachments back into the composer ahead of anything typed since
 * (never overwriting it). With `focus`, keep focus in the composer (caret
 * stays where the user was typing) unless they moved to another field.
 */
function mergeIntoComposer(
  text: string,
  imgs: ClarifyImage[],
  anns: ReactAnnotation[],
  focus: boolean,
  order: { at?: number; parts?: RescuedPart[] } = {},
) {
  const el = textareaRef.value
  const hadFocus = !!el && document.activeElement === el
  const selStart = hadFocus ? el!.selectionStart : 0
  const selEnd = hadFocus ? el!.selectionEnd : 0
  const current = draft.value
  const t = text.trim()
  let shift = 0
  if (t) {
    if (typeof order.at === 'number') {
      // Rescued message: keep rescued ones in send order, ahead of new typing.
      const r = insertRescued(current, rescuedHead, { text: t, at: order.at })
      rescuedHead = r.head
      draft.value = r.text
      shift = r.text.length - current.length
    } else if (!current.trim()) {
      draft.value = t
      shift = t.length - current.length
      rescuedHead = order.parts?.length && t.startsWith(order.parts.map((p) => p.text).join('\n')) ? order.parts : []
    } else if (!current.includes(t)) {
      const prefix = `${t}\n`
      draft.value = prefix + current
      shift = prefix.length
      rescuedHead = []
    }
  }
  const imgKey = (im: ClarifyImage) => JSON.stringify(im)
  const haveImgs = new Set(attachments.value.map(imgKey))
  const backImgs = cloneClarifyImages(imgs).filter((im) => !haveImgs.has(imgKey(im)))
  if (backImgs.length) attachments.value = [...backImgs, ...attachments.value]
  const annKey = (a: ReactAnnotation) => JSON.stringify(a)
  const haveAnns = new Set(annotations.value.map(annKey))
  const backAnns = cloneReactAnnotations(anns).filter((a) => !haveAnns.has(annKey(a)))
  if (backAnns.length) annotations.value = [...backAnns, ...annotations.value]

  void nextTick(() => {
    autoGrow()
    const box = textareaRef.value
    if (!box) return
    if (hadFocus) {
      if (document.activeElement !== box) box.focus({ preventScroll: true })
      box.setSelectionRange(selStart + shift, selEnd + shift)
      return
    }
    if (!focus) return
    const active = document.activeElement as HTMLElement | null
    // Never pull focus out of another field the user moved to.
    const elsewhere =
      !!active &&
      active !== box &&
      active !== document.body &&
      (active.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(active.tagName))
    if (elsewhere) return
    box.focus({ preventScroll: true })
    const end = box.value.length
    box.setSelectionRange(end, end)
  })
}

/**
 * True while the composer holds text rescued from a refused send (or a draft
 * kept when the run ended). The run+node stash then mirrors the composer
 * until that text is sent or cleared, so a refresh never loses it.
 */
let stashLive = false
/** Rescued messages at the head of the draft, oldest send first (S19). */
let rescuedHead: RescuedPart[] = []

/** Inline images left out of the stash for size (S21); null when none. */
const stashNotice = ref<string | null>(null)
function noteImagesDropped(n: number | undefined) {
  if (n && n > 0) stashNotice.value = translate('pages.clarify.stashImagesDropped', { n })
}

/**
 * Restore this tab's run+node stash once the composer is open again (run
 * resumed). The stash lives in this tab's sessionStorage, so only the tab
 * that sent a message ever puts it back (S18 / B4).
 */
function restoreStashedDraft() {
  if (!composerOpen()) return
  if (!clarifyTabReady()) {
    // Duplicate-tab check still running: a copied tab must not refill.
    void initClarifyTab().then(restoreStashedDraft)
    return
  }
  const s = readClarifyStash(props.runId, props.nodeId)
  if (!s) return
  stashLive = true
  mergeIntoComposer(s.text, s.images, s.annotations, false, { parts: s.parts })
  noteImagesDropped(s.imagesDropped)
}

/** Composer closed (run ended) or is going away: keep the unsent draft per run+node. */
function stashComposerDraft(): boolean {
  if (!hasComposerDraft()) return false
  const r = stashClarifyDraft(props.runId, props.nodeId, {
    text: draft.value,
    images: cloneClarifyImages(attachments.value),
    annotations: cloneReactAnnotations(annotations.value),
  })
  noteImagesDropped(r.imagesDropped)
  stashLive = true
  return true
}

/** Composer sent or emptied: the rescued text is gone on purpose. */
function releaseStash() {
  rescuedHead = []
  stashNotice.value = null
  if (!stashLive) return
  stashLive = false
  clearClarifyStash(props.runId, props.nodeId)
}

watch(
  () => [draft.value, attachments.value, annotations.value] as const,
  () => {
    if (!stashLive || !composerOpen()) return
    if (!hasComposerDraft()) {
      releaseStash()
      return
    }
    const headText = rescuedHead.map((p) => p.text).join('\n')
    if (rescuedHead.length && !draft.value.startsWith(headText)) rescuedHead = []
    replaceClarifyStash(
      props.runId,
      props.nodeId,
      {
        text: draft.value,
        images: cloneClarifyImages(attachments.value),
        annotations: cloneReactAnnotations(annotations.value),
      },
      rescuedHead,
    )
  },
  { deep: true, flush: 'post' },
)

watch(composerOpen, (open, wasOpen) => {
  if (open) {
    // Resumed: the text is back in the composer, so the
    // "resend after resume" / "draft kept" notice has done its job.
    if (!wasOpen) sendNotice.value = null
    restoreStashedDraft()
    return
  }
  // Run ended under an unsent draft: the composer disappears, the draft must not.
  // A refused-send notice already says what happened; keep it.
  if (wasOpen && !props.done && stashComposerDraft() && !sendNotice.value) {
    sendNotice.value = translate('pages.clarify.draftKept')
  }
})
onMounted(restoreStashedDraft)
onBeforeUnmount(() => {
  // Leaving while the run is closed for input (run ended): keep the draft for resume.
  if (!props.done && !props.active && !props.coldSession) stashComposerDraft()
})

/**
 * Parent (HTTP 409) or the WS error frame calls this when a clarify reply was
 * refused because the clarify visit already ended (code clarify_visit_ended):
 * the message was never sent. Drop its optimistic row and, unless resume will
 * redeliver it on its own (autoRetryOnResume), put its text, attachments and
 * annotations back: into the composer when it is open (ahead of anything typed
 * since, focus kept), else into the run+node stash restored after resume.
 * Shows `notice` instead of any chat reply.
 */
function restoreUnsentQueued(
  sent: {
    text?: string
    images?: ClarifyImage[]
    annotations?: ReactAnnotation[]
    clientId?: string
    /** Send time (outbox), so several refusals come back in send order. */
    at?: number
    /** Inline images the outbox already had to leave out. */
    imagesDropped?: number
  },
  notice: string,
  opts: { autoRetryOnResume?: boolean } = {},
) {
  // Local optimistic copy first: by id (clientId), then by text.
  let idx = -1
  if (sent.clientId) idx = queued.value.findIndex((q) => q.id === sent.clientId)
  if (idx < 0 && sent.text) {
    for (let i = queued.value.length - 1; i >= 0; i--) {
      if (!queued.value[i].id && queued.value[i].text === sent.text) {
        idx = i
        break
      }
    }
  }
  const item = idx >= 0 ? queued.value.splice(idx, 1)[0] : null
  if (queued.value.length === 0 && liveAgentIdx.value < 0) thinking.value = false
  sendNotice.value = notice
  if (opts.autoRetryOnResume) return

  const origin = item ? composerOrigin.get(toRaw(item)) : undefined
  const text = (origin ?? item?.text ?? sent.text ?? '').trim()
  const imgs = item?.images ?? sent.images ?? []
  const anns = item?.annotations ?? sent.annotations ?? []
  if (!text && !imgs.length && !anns.length) return
  // Always stash (even with the composer open): a refresh or /gates dropping
  // this item before the user resends must not lose it. Cleared on send/clear.
  const at = typeof sent.at === 'number' ? sent.at : Date.now()
  const r = stashClarifyDraft(
    props.runId,
    props.nodeId,
    {
      text,
      images: cloneClarifyImages(imgs),
      annotations: cloneReactAnnotations(anns),
    },
    { at },
  )
  // With the composer open the images are back in it; only say so when they are not.
  noteImagesDropped((composerOpen() ? 0 : r.imagesDropped) + (sent.imagesDropped ?? 0))
  stashLive = true
  if (composerOpen()) mergeIntoComposer(text, imgs, anns, true, { at })
}

/** Notice copy for a clarify_visit_ended refusal. */
function visitEndedNotice(autoRetryOnResume: boolean | undefined): string {
  return autoRetryOnResume
    ? translate('pages.clarify.visitEndedAutoRetry')
    : translate('pages.clarify.visitEndedResend')
}

/**
 * 确认并流转 succeeded: the visit is over, and the socket that would have
 * delivered turn_done was closed at the click. Drop 思考中 / 生成中 on that
 * turn. Confirm failure must not call this.
 */
function settleConfirmedTurn() {
  endLiveStreaming()
  thinking.value = false
}

/** Idle is authoritative: no live turn may keep its caret, tracked or not. */
function endLiveStreaming() {
  let flushed = false
  for (const turn of liveTurns.value) {
    if (turn.role !== 'agent' || !turn.streaming) continue
    turn.streaming = false
    markOutputEnd(turn)
    flushed = true
  }
  if (flushed) {
    streamPreview.flush()
    thoughtPreview.flush()
  }
  liveAgentIdx.value = -1
}

/**
 * Authoritative idle (waiting=0 ∧ !busy ∧ no activeItem): force-clear local
 * sticky busy — ghost queued, stop streaming on the live agent, and thinking.
 * Empty / failure agent slots stay in liveTurns as failure cards (plan g1.1);
 * do not wipe liveTurns=[] on empty.
 */
function forceAuthoritativeIdle() {
  if (queued.value.length) queued.value = []
  endLiveStreaming()
  thinking.value = false
}

/**
 * After turn_done/error: drop the completed item and ghost optimistic rows.
 * Keep distinct authoritative waiters so multi-turn queue does not briefly
 * unlock confirm before the next queue_state. Empty after filter ⇒ idle.
 */
function settleAfterTurnEnd() {
  const completedId = liveTurnOrigin?.id
  if (completedId) {
    lastCompletedItemId = completedId
    queued.value = queued.value.filter((q) => q.id !== completedId)
  }
  if (queued.value.some((q) => !q.id)) {
    queued.value = dropGhostItems(queued.value)
  }
  liveTurnOrigin = null
  thinking.value = queued.value.length > 0 || liveAgentIdx.value >= 0
}

function applySandboxOrphan(frame: {
  sandboxBusy?: boolean
  sandboxDesynced?: boolean
  sandboxRunningOpId?: string
}) {
  const busy = !!frame.sandboxBusy || !!frame.sandboxDesynced
  if (!busy) {
    const wasOrphan = !!sandboxOrphan.value
    sandboxOrphan.value = null
    if (wasOrphan) {
      endLiveStreaming()
      if (queued.value.length === 0) thinking.value = false
    }
    // The refresh snapshot may have landed while the bridge was still busy.
    // Now that it is idle, drop the idle row the snapshot did not keep.
    dropSettledLiveIfPersistedWins()
    return
  }
  sandboxOrphan.value = {
    runningOpId: String(frame.sandboxRunningOpId || ''),
    desynced: !!frame.sandboxDesynced,
  }
}

/**
 * Newest assistant bubble when it already finished normally.
 * A handoff row is skipped; a human tail or an interrupted/empty agent is not.
 */
function latestNormallyCompletedTurn(): ClarifyTurn | null {
  const list = displayTurns.value
  for (let i = list.length - 1; i >= 0; i--) {
    const t = list[i]
    if (!t || t.role !== 'agent') return null
    if (t.handoff) continue
    return showTurnCompleted(t) ? t : null
  }
  return null
}

const showSandboxOrphanBanner = computed(() => {
  if (!sandboxOrphan.value || !platformQueueIdle.value || props.done || queued.value.length > 0) {
    return false
  }
  // A live slot for this orphan turn must not hide the banner: the platform
  // already stopped tracking, and the transcript still has to keep growing.
  const completed = latestNormallyCompletedTurn()
  if (!completed) return true
  const running = sandboxOrphan.value.runningOpId
  const doneOp = completed.opId || ''
  if (running !== '' && doneOp !== '' && running === doneOp) return false
  return true
})

const sandboxOrphanOpLabel = computed(() => {
  const id = sandboxOrphan.value?.runningOpId
  return id ? ` (${id})` : ''
})

/**
 * Reconcile local pending-send panel with platform-authoritative queue_state.
 * waiting===0 clears ghost rows after remote Cancel; items[] rebuilds/trims
 * while allowing at most one local-ahead item when no live turn (HTTP ack /
 * turn_begin in flight). busy+activeItem restores live bubble after refresh.
 */
function syncIdleNarrative(
  waiting: number,
  busy?: boolean,
  activeItem?: SessionFrameItem | null,
) {
  idleNarrativeOpen = waiting === 0 && !busy && !activeItem
  platformQueueIdle.value = idleNarrativeOpen
}

/** Drop idle or settled live rows once the persisted transcript is the dialogue. */
function clearSettledLiveTurns() {
  liveTurns.value = []
  liveHistoryAnchor = 0
  liveAgentIdx.value = -1
  messageReveal.reset()
  thoughtReveal.reset()
  streamPreview.reset()
  thoughtPreview.reset()
  if (queued.value.length === 0) thinking.value = false
  idlePainted = false
  persistedMovedAfterIdlePaint = false
  lastAppliedAcpSig = ''
}

/** Live rails differ from the persisted tail, so a transcript refresh must not drop them. */
function liveNarrativeAhead(persisted: ClarifyTurn[]): boolean {
  const liveAgent = [...liveTurns.value].reverse().find((t) => t.role === 'agent')
  if (!liveAgent) return false
  const liveText = (liveAgent.text || '').trim()
  const liveThought = (liveAgent.thought || '').trim()
  if (!liveText && !liveThought && !liveAgent.tools?.length && !liveAgent.parts?.length) return false
  const last = [...persisted].reverse().find((t) => t.role === 'agent' && !t.handoff)
  if (!last) return true
  if ((last.text || '').trim() !== liveText) return true
  if ((last.thought || '').trim() !== liveThought) return true
  if (!sameTools(last.tools, liveAgent.tools)) return true
  if (!sameParts(last.parts, liveAgent.parts)) return true
  return false
}

/**
 * Drop a settled live row once the persisted transcript is the dialogue.
 * While the sandbox orphan is still writing, the open row stays.
 */
function dropSettledLiveIfPersistedWins() {
  if (sandboxOrphan.value) return
  const turnList = persistedTurns.value
  prunePersistedLiveHistory(turnList)
  const liveHumanText = liveTurns.value[0]?.role === 'human' ? liveTurns.value[0].text || '' : ''
  const persistedCaughtUp = persistedCompletedLiveHuman(turnList.slice(liveHistoryAnchor), liveHumanText)
  const ahead = liveNarrativeAhead(turnList)
  const snapshotWins =
    idlePainted &&
    persistedMovedAfterIdlePaint &&
    liveAgentIdx.value < 0 &&
    ahead
  if (
    liveTurns.value.length &&
    (snapshotWins ||
      (!ahead &&
        (persistedCaughtUp || (turnList.length > 0 && !liveTurns.value.some((t) => t.streaming)))))
  ) {
    clearSettledLiveTurns()
  }
}

function applyQueueState(
  waiting: number,
  items: SessionFrameItem[] | null,
  busy?: boolean,
  activeItem?: SessionFrameItem | null,
  sandbox?: { busy?: boolean; desynced?: boolean; runningOpId?: string },
) {
  // A queue snapshot captured before completion can arrive after turn_done.
  // Its busy bit describes the completed turn, so an otherwise empty snapshot
  // must not reopen thinking. Distinct waiting items still belong to the queue.
  if (
    lastCompletedItemId &&
    !liveTurnOrigin &&
    liveAgentIdx.value < 0 &&
    (activeItem?.id === lastCompletedItemId || items?.some((it) => it.id === lastCompletedItemId))
  ) {
    if (activeItem?.id === lastCompletedItemId) activeItem = null
    if (items) items = items.filter((it) => it.id !== lastCompletedItemId)
    if (!activeItem && (!items || items.length === 0)) return
    if (items) waiting = items.length
  }
  syncIdleNarrative(waiting, busy, activeItem ?? null)
  if (isAuthoritativeIdle(waiting, busy, activeItem)) {
    const row = liveAgentIdx.value >= 0 ? liveTurns.value[liveAgentIdx.value] : undefined
    const keep =
      !!row?.streaming &&
      sandboxStillRunning(sandbox) &&
      !orphanEchoesCompleted(sandbox?.runningOpId)
    if (keep && row) {
      if (queued.value.length) queued.value = []
      thinking.value = true
      if (
        (row.text || '').trim() ||
        (row.thought || '').trim() ||
        row.tools?.length ||
        row.parts?.length
      ) {
        idlePainted = true
      }
      return
    }
    forceAuthoritativeIdle()
    return
  }
  // A delayed waiting snapshot may still list the item that turn_begin has
  // already started. Keep that active item out of the pending queue; only a
  // distinct server id can represent a waiter, including a same-text reply.
  const activeId = (busy ? activeItem?.id : undefined) || liveTurnOrigin?.id
  if (activeId) {
    queued.value = queued.value.filter((q) => q.id !== activeId)
    if (items) items = items.filter((it) => it.id !== activeId)
  }
  if (waiting === 0 && !busy) {
    if (queued.value.length) queued.value = []
    if (liveAgentIdx.value < 0) thinking.value = false
  } else if (items) {
    queued.value = reconcileQueue(queued.value, items, liveAgentIdx.value >= 0 || !!busy)
  }
  // Refresh resume: recreate streaming agent bubble from activeItem when busy.
  // 有稳定 item id 时只检查当前锚点后的历史，旧同文完成回合不能阻止
  // queue_state→ACP（无 turn_begin）的硬加载恢复。无 id 保留旧尾部判断。
  const restoreText = activeItem?.text ?? ''
  const restoreAnchor = activeItem?.id
    ? liveTurnOrigin?.id === activeItem.id
      ? liveHistoryAnchor
      : anchorForNewLiveHuman(restoreText)
    : 0
  if (
    busy &&
    activeItem &&
    liveAgentIdx.value < 0 &&
    !persistedCompletedLiveHuman(persistedTurns.value.slice(restoreAnchor), restoreText)
  ) {
    const text = restoreText
    liveHistoryAnchor = restoreAnchor
    liveTurnOrigin = {
      id: activeItem.id,
      text,
      images: activeItem.images ?? [],
      annotations: activeItem.annotations ?? [],
      newHuman: true,
    }
    const started = new Date().toISOString()
    liveTurns.value = [
      {
        role: 'human',
        text,
        at: started,
        images: activeItem.images,
        annotations: activeItem.annotations,
      },
      {
        role: 'agent',
        text: '',
        thought: '',
        at: started,
        startedAt: started,
        streaming: true,
      },
    ]
    liveAgentIdx.value = 1
    thoughtOpenOverride.value = {}
    messageReveal.reset()
    thoughtReveal.reset()
    streamPreview.reset()
    thoughtPreview.reset()
  }
  // Authority idle / !busy: tear down streaming — keep empty/failure cards
  // (plan g1.1); never wipe liveTurns=[] for empty agents.
  if (!busy) endLiveStreaming()
  thinking.value = liveAgentIdx.value >= 0 || queued.value.length > 0 || !!busy
}

/** What the live turn opened by the latest turn_begin carried (local copy). */
let liveTurnOrigin: {
  id?: string
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
  newHuman: boolean
} | null = null

/**
 * WS error frame with code clarify_visit_ended: the run ended, so the reply
 * was dropped (at turn start, or at WS enqueue). It is not an answer — never
 * write it into an agent bubble. Remove the turn it opened and hand the text
 * back (local optimistic copy first, `dropped` as fallback).
 */
function applyVisitEndedFrame(frame: {
  autoRetryOnResume?: boolean
  clientMessageId?: string
}) {
  const auto = !!frame.autoRetryOnResume
  // Broadcast frames carry no text and no `dropped`: the reply is ours to put
  // back only when its clientMessageId is in this tab's outbox. Other viewers
  // just lose the empty live slot (no refill, no stash, no notice).
  const mine = auto ? null : takeClarifyOutbox(props.runId, props.nodeId, frame.clientMessageId)
  const idx = liveAgentIdx.value
  const agent = idx >= 0 ? liveTurns.value[idx] : null
  const origin = liveTurnOrigin
  let local: { text: string; images: ClarifyImage[]; annotations: ReactAnnotation[] } | null = null
  if (agent && !(agent.text || '').trim() && !(agent.thought || '').trim()) {
    // Drop the empty agent slot and the human bubble this turn opened.
    const human = idx > 0 ? liveTurns.value[idx - 1] : null
    const removeHuman = !!origin?.newHuman && human?.role === 'human'
    liveTurns.value.splice(removeHuman ? idx - 1 : idx, removeHuman ? 2 : 1)
    if (removeHuman && origin) local = { text: origin.text, images: origin.images, annotations: origin.annotations }
    liveAgentIdx.value = -1
    messageReveal.reset()
    thoughtReveal.reset()
    streamPreview.reset()
    thoughtPreview.reset()
  } else if (agent) {
    agent.streaming = false
    markOutputEnd(agent)
    liveAgentIdx.value = -1
  }
  if (auto) {
    // System first message: resume resends it; nothing to put back.
    restoreUnsentQueued({}, visitEndedNotice(true), { autoRetryOnResume: true })
  } else if (mine) {
    // Same text as the live bubble: its images still carry preview data.
    const sameTurn = !!local && local.text.trim() === mine.text.trim()
    restoreUnsentQueued(
      {
        text: mine.text,
        images: sameTurn && local!.images.length ? local!.images : mine.images,
        annotations: mine.annotations.length ? mine.annotations : (sameTurn ? local!.annotations : []),
        at: mine.at,
        // Same turn: the live bubble's images are complete, nothing was lost.
        imagesDropped: sameTurn && local!.images.length ? 0 : mine.imagesDropped,
      },
      visitEndedNotice(false),
    )
  }
  settleAfterTurnEnd()
}

/** Parent feeds review/clarify WS frames here (shared session protocol). */
function applyReviewFrame(frame: {
  event?: string
  nodeId?: string
  item?: SessionFrameItem
  message?: string
  interrupted?: boolean
  waiting?: number
  items?: SessionFrameItem[]
  busy?: boolean
  sandboxBusy?: boolean
  sandboxDesynced?: boolean
  sandboxRunningOpId?: string
  activeItem?: SessionFrameItem
  /** error: clarify_visit_ended (run ended; the reply was dropped, not answered). */
  code?: string
  autoRetryOnResume?: boolean
  /** Echo of the request's clientMessageId (only when the request had one). */
  clientMessageId?: string
}) {
  // Defense: ignore frames for other producer sessions on the same run.
  if (frame.nodeId && frame.nodeId !== props.nodeId) return
  switch (frame.event) {
    case 'turn_begin': {
      idleNarrativeOpen = false
      idlePainted = false
      persistedMovedAfterIdlePaint = false
      lastAppliedAcpSig = ''
      platformQueueIdle.value = false
      // Pump order: queue_state(remaining, no active) → turn_begin(item=active).
      // frame.item is server-authoritative; match-remove by id first, text fallback —
      // never blind-shift (that would bind live human to the next waiter after trim).
      const auth = frame.item
      const text = auth?.text ?? ''
      const begun = takeTurnBeginItem(queued.value, auth)
      queued.value = begun.queue
      const local = begun.taken
      const images =
        auth?.images && auth.images.length > 0 ? auth.images : local?.images
      const annotations =
        auth?.annotations && auth.annotations.length > 0
          ? auth.annotations
          : local?.annotations
      const humanText = text || local?.text || ''
      if (!liveTurns.value.length) liveHistoryAnchor = anchorForNewLiveHuman(humanText)
      liveTurnOrigin = {
        id: auth?.id,
        text: (local ? composerOrigin.get(toRaw(local)) : undefined) ?? humanText,
        // Prefer the local copy: it still has inline data/preview, while the
        // server item may already be ref-only (blob:…) after upload.
        images: local?.images?.length ? local.images : (images ?? []),
        annotations: local?.annotations?.length ? local.annotations : (annotations ?? []),
        newHuman: false,
      }
      // Cover retry: reuse trailing live human + failed/empty agent instead of duplicating.
      const last = liveTurns.value.length
      const existingHuman = last >= 2 ? liveTurns.value[last - 2] : null
      const existingAgent = last >= 2 ? liveTurns.value[last - 1] : null
      if (
        existingHuman?.role === 'human' &&
        existingAgent?.role === 'agent' &&
        (existingHuman.text || '') === humanText &&
        // An agent slot with no content yet is this turn's slot — including the
        // already-streaming one retryLastFailed / refresh-resume opened
        // optimistically, which must not be duplicated.
        (isRetryableFailedAgent(existingAgent) ||
          (!(existingAgent.text || '').trim() &&
            !(existingAgent.thought || '').trim()))
      ) {
        existingHuman.images = images ?? existingHuman.images
        existingHuman.annotations = annotations ?? existingHuman.annotations
        existingAgent.text = ''
        existingAgent.thought = ''
        delete existingAgent.tools
        delete existingAgent.parts
        existingAgent.questions = undefined
        existingAgent.interrupted = false
        existingAgent.streaming = true
        markOutputStart(existingAgent, true)
        existingAgent.at = existingAgent.startedAt || new Date().toISOString()
        liveAgentIdx.value = last - 1
      } else {
        const started = new Date().toISOString()
        liveTurnOrigin.newHuman = true
        liveTurns.value.push({
          role: 'human',
          text: humanText,
          at: started,
          images,
          annotations,
        })
        liveAgentIdx.value =
          liveTurns.value.push({
            role: 'agent',
            text: '',
            thought: '',
            at: started,
            startedAt: started,
            streaming: true,
          }) - 1
      }
      thoughtOpenOverride.value = {}
      messageReveal.reset()
      thoughtReveal.reset()
      streamPreview.reset()
      thoughtPreview.reset()
      thinking.value = true
      break
    }
    case 'turn_done': {
      // Answered: this tab's outbox copy is no longer needed.
      if (liveTurnOrigin?.text) ackClarifyOutboxText(props.runId, props.nodeId, liveTurnOrigin.text)
      if (liveAgentIdx.value >= 0 && liveTurns.value[liveAgentIdx.value]) {
        const agent = liveTurns.value[liveAgentIdx.value]
        agent.streaming = false
        agent.at = new Date().toISOString()
        if (frame.interrupted) agent.interrupted = true
        markOutputEnd(agent)
        // Reveal flush before markdown flush (plan g1.2).
        messageReveal.flush()
        thoughtReveal.flush()
        streamPreview.flush()
        thoughtPreview.flush()
      }
      liveAgentIdx.value = -1
      // Pump may omit idle queue_state; clear ghost optimistic rows only.
      // Real id waiters stay busy until authoritative idle (review v1).
      settleAfterTurnEnd()
      break
    }
    case 'error': {
      if (frame.code === 'clarify_visit_ended') {
        applyVisitEndedFrame(frame)
        break
      }
      if (liveAgentIdx.value >= 0 && liveTurns.value[liveAgentIdx.value]) {
        const agent = liveTurns.value[liveAgentIdx.value]
        agent.streaming = false
        agent.text = agent.text || frame.message || 'error'
        agent.at = new Date().toISOString()
        if (frame.interrupted) agent.interrupted = true
        markOutputEnd(agent)
        messageReveal.flush()
        thoughtReveal.flush()
        streamPreview.flush()
        thoughtPreview.flush()
      }
      liveAgentIdx.value = -1
      // Same as turn_done: ghosts only; keep real remaining waiters.
      settleAfterTurnEnd()
      break
    }
    case 'queue_state':
      applyQueueState(
        typeof frame.waiting === 'number' ? frame.waiting : 0,
        Array.isArray(frame.items) ? frame.items : null,
        frame.busy,
        frame.activeItem ?? null,
        {
          busy: frame.sandboxBusy,
          desynced: frame.sandboxDesynced,
          runningOpId: frame.sandboxRunningOpId,
        },
      )
      applySandboxOrphan(frame)
      break
  }
  void scrollBottom()
}

/**
 * A snapshot with a segment marker is one user message and several agent rows.
 * Incremental single-message updates stay on the live row.
 */
function agentSegments(events: AcpEvent[]): { thought: string; message: string }[] | null {
  let multi = false
  const segs: { thought: string; message: string }[] = [{ thought: '', message: '' }]
  let sawMessage = false
  for (const ev of events) {
    if (ev.kind === 'segment') {
      segs.push({ thought: '', message: '' })
      sawMessage = false
      multi = true
      continue
    }
    if (ev.kind === 'thought' && ev.text) {
      if (sawMessage) {
        segs.push({ thought: ev.text, message: '' })
        sawMessage = false
        multi = true
      } else {
        segs[segs.length - 1]!.thought = ev.text
      }
      continue
    }
    if (ev.kind === 'message' && ev.text) {
      if (
        ev.text.startsWith('react reply chat failed') ||
        ev.text.startsWith('review revise chat failed')
      ) {
        continue
      }
      if (sawMessage) {
        segs.push({ thought: '', message: ev.text })
        multi = true
      } else {
        segs[segs.length - 1]!.message = ev.text
      }
      sawMessage = true
    }
  }
  return multi ? segs : null
}

/** Snapshot tools and steps live on the open (last) agent row, matching the persisted transcript. */
function setRowTools(row: ClarifyTurn, tools: AgentTool[], parts?: AgentPart[]) {
  if (!sameTools(row.tools, tools)) {
    if (tools.length) row.tools = tools
    else delete row.tools
  }
  if (sameParts(row.parts, parts)) return
  if (parts?.length) row.parts = parts
  else delete row.parts
}

function applyAgentSegments(segs: { thought: string; message: string }[], tools: AgentTool[], parts?: AgentPart[]) {
  let first = liveAgentIdx.value
  while (first > 0 && liveTurns.value[first - 1]?.role === 'agent') first--
  while (liveTurns.value.length < first + segs.length) {
    const started = new Date().toISOString()
    liveTurns.value.push({
      role: 'agent',
      text: '',
      thought: '',
      at: started,
      startedAt: started,
      streaming: true,
    })
  }
  segs.forEach((seg, i) => {
    const row = liveTurns.value[first + i]
    if (!row || row.role !== 'agent') return
    const last = i === segs.length - 1
    row.thought = seg.thought
    row.text = seg.message
    row.streaming = last
    row.handoff = !last
    if (!last) markOutputEnd(row)
    setRowTools(row, last ? tools : [], last ? parts : undefined)
    if (last) {
      liveAgentIdx.value = first + i
      messageReveal.setTarget(seg.message)
      thoughtReveal.setTarget(seg.thought)
    }
  })
}

/**
 * Platform FIFO is idle and the streaming slot is gone. Write the snapshot
 * onto the closed agent row (or a new one) without reopening the slot, so the
 * orphan banner can stay while the text is visible. The row is settled: the
 * slot is closed, so it must not keep a streaming tail. Stick-gated scroll only.
 */
function paintIdleNarrative(events: AcpEvent[]): boolean {
  let idx = -1
  for (let i = liveTurns.value.length - 1; i >= 0; i--) {
    if (liveTurns.value[i]?.role === 'agent') {
      idx = i
      break
    }
  }
  let created = false
  if (idx < 0) {
    if (idleSnapshotAlreadyVisible(events)) return true
    const started = new Date().toISOString()
    idx =
      liveTurns.value.push({
        role: 'agent',
        text: '',
        thought: '',
        at: started,
        startedAt: started,
        streaming: false,
      }) - 1
    created = true
  }
  liveAgentIdx.value = idx
  const applied = applyAcpEventsToSlot(events)
  const row = liveTurns.value[idx]
  liveAgentIdx.value = -1
  if (queued.value.length === 0) thinking.value = false
  const emptyCreated =
    created &&
    row &&
    !(row.text || '').trim() &&
    !(row.thought || '').trim() &&
    !row.tools?.length &&
    !row.parts?.length &&
    liveTurns.value[liveTurns.value.length - 1] === row
  if (emptyCreated) {
    liveTurns.value.pop()
  } else {
    settleIdleNarrative()
    const hasBody = liveTurns.value.some(
      (t) =>
        t.role === 'agent' &&
        (!!(t.text || '').trim() || !!(t.thought || '').trim() || !!t.tools?.length || !!t.parts?.length),
    )
    if (hasBody) idlePainted = true
  }
  return applied
}

/** Slot is closed: show the written snapshot as a settled reply, not a live tail. */
function settleIdleNarrative() {
  for (const turn of liveTurns.value) {
    if (turn.role !== 'agent' || !turn.streaming) continue
    turn.streaming = false
    if (!turn.handoff) markOutputEnd(turn)
  }
  messageReveal.flush()
  thoughtReveal.flush()
  streamPreview.flush()
  thoughtPreview.flush()
}

/** Persisted tail already shows this cumulative snapshot — do not add an empty row. */
function idleSnapshotAlreadyVisible(events: AcpEvent[]): boolean {
  const last = [...persistedTurns.value].reverse().find((t) => t.role === 'agent' && !t.handoff)
  if (!last) return false
  let msg = ''
  let thought = ''
  for (const ev of events) {
    if (ev.kind === 'message' && ev.text) msg = ev.text
    if (ev.kind === 'thought' && ev.text) thought = ev.text
  }
  return (
    (last.text || '') === msg &&
    (last.thought || '') === thought &&
    sameTools(last.tools, toolsFromAcp(events)) &&
    sameParts(last.parts, partsFromAcp(events))
  )
}

function sandboxStillRunning(sandbox?: { busy?: boolean; desynced?: boolean }): boolean {
  return !!sandbox?.busy || !!sandbox?.desynced
}

/** Bridge is echoing a turn the transcript already finished normally. */
function orphanEchoesCompleted(runningOpId?: string): boolean {
  const running = (runningOpId || '').trim()
  if (!running) return false
  const completed = latestNormallyCompletedTurn()
  if (!completed) return false
  return (completed.opId || '') === running
}

/** Platform queue is idle, but the sandbox is still producing this turn. */
function orphanTurnStillRunning(): boolean {
  if (!sandboxOrphan.value || !platformQueueIdle.value) return false
  return !orphanEchoesCompleted(sandboxOrphan.value.runningOpId)
}

function noteOrphanPainted() {
  if (!sandboxOrphan.value) return
  const hasBody = liveTurns.value.some(
    (t) =>
      t.role === 'agent' &&
      (!!(t.text || '').trim() || !!(t.thought || '').trim() || !!t.tools?.length || !!t.parts?.length),
  )
  if (hasBody) idlePainted = true
}

/** Reopen the turn the platform stopped tracking so later frames still paint. */
function ensureOrphanSlot() {
  for (let i = liveTurns.value.length - 1; i >= 0; i--) {
    const row = liveTurns.value[i]
    if (row?.role !== 'agent') continue
    row.streaming = true
    row.interrupted = false
    delete row.endedAt
    liveAgentIdx.value = i
    thinking.value = true
    return
  }
  const persisted = persistedTurns.value
  let human: ClarifyTurn | undefined
  for (let i = persisted.length - 1; i >= 0; i--) {
    const t = persisted[i]
    if (!t) continue
    if (t.role === 'human') {
      human = t
      break
    }
    if (t.role === 'agent' && !t.handoff) break
  }
  const started = new Date().toISOString()
  const next: ClarifyTurn[] = []
  if (human) {
    next.push({
      role: 'human',
      text: human.text || '',
      at: human.at || started,
      images: human.images,
      annotations: human.annotations,
    })
  }
  next.push({
    role: 'agent',
    text: '',
    thought: '',
    at: started,
    startedAt: started,
    streaming: true,
  })
  liveTurns.value = next
  liveAgentIdx.value = next.length - 1
  thinking.value = true
  lastAppliedAcpSig = ''
  messageReveal.reset()
  thoughtReveal.reset()
  streamPreview.reset()
  thoughtPreview.reset()
}

function acpSnapshotSig(events: AcpEvent[]): string {
  let msg = ''
  let thought = ''
  const tools: string[] = []
  let parts = ''
  for (const ev of events) {
    if (ev.kind === 'message' && ev.text) msg = ev.text
    else if (ev.kind === 'thought' && ev.text) thought = ev.text
    else if (ev.kind === 'tool_call') tools.push(`${ev.title || ''}:${ev.status || ''}`)
    else if (ev.kind === 'timeline' && ev.parts) {
      parts = ev.parts.map((p) => `${p.kind}:${p.text || ''}:${p.title || ''}:${p.status || ''}`).join('|')
    }
  }
  return `${thought}\n${msg}\n${tools.join(',')}\n${parts}`
}

function snapshotAlreadyOnAgent(agent: ClarifyTurn, events: AcpEvent[]): boolean {
  let msg = ''
  let thought = ''
  for (const ev of events) {
    if (ev.kind === 'message' && ev.text) msg = ev.text
    if (ev.kind === 'thought' && ev.text) thought = ev.text
  }
  return (agent.text || '') === msg && (agent.thought || '') === thought
}

/**
 * Consume publishAcp events for the streaming agent bubble (dialogue surface).
 * Returns false when mounted but streaming slot is not ready — host must buffer
 * (hard-load / remount race; never silent-noop as applied).
 * After an idle queue_state, the missing slot is intentional: paint anyway.
 * A sandbox that is still writing after the platform stopped tracking keeps
 * the slot open, so late thought and body land on the open page.
 */
function applyAcpEvents(events: AcpEvent[] | undefined, nodeId?: string): boolean {
  if (!events?.length) return true
  // Wrong node: not a readiness failure — do not buffer for another surface.
  if (nodeId && nodeId !== props.nodeId) return true
  if (liveAgentIdx.value < 0 && orphanTurnStillRunning()) ensureOrphanSlot()
  if (liveAgentIdx.value < 0) {
    if (!idleNarrativeOpen) return false
    return paintIdleNarrative(events)
  }
  const applied = applyAcpEventsToSlot(events)
  if (applied) noteOrphanPainted()
  return applied
}

function applyAcpEventsToSlot(events: AcpEvent[]): boolean {
  const agent = liveTurns.value[liveAgentIdx.value]
  if (!agent) return false
  const sig = acpSnapshotSig(events)
  if (sig === lastAppliedAcpSig && snapshotAlreadyOnAgent(agent, events)) return true
  const tools = toolsFromAcp(events)
  const parts = partsFromAcp(events)
  const segs = agentSegments(events)
  if (segs) {
    applyAgentSegments(segs, tools, parts)
    lastAppliedAcpSig = sig
    void scrollBottom()
    return true
  }
  let msg = agent.text
  let thought = agent.thought || ''
  for (const ev of events) {
    // Plan stays out of the dialogue; tool_call rows feed the folded tool group.
    if (ev.kind === 'message' && ev.text) msg = ev.text
    if (ev.kind === 'thought' && ev.text) thought = ev.text
  }
  // Until the new prompt emits output, live snapshots still hold the previous
  // turn; message rails never clear, so accepting one would stick in this bubble.
  if (msg !== agent.text && msg.trim() && msg.trim() === lastSettledAgentText(agent)) return true
  // Keep thought / message on separate rails — never msg||thought overwrite.
  agent.thought = thought
  agent.text = msg
  setRowTools(agent, tools, parts)
  lastAppliedAcpSig = sig
  // Authority → reveal → markdown/text coalesce (not absolute snapshot → DOM).
  messageReveal.setTarget(msg)
  thoughtReveal.setTarget(thought)
  // Stick-gated only — never force-drag while user scrolled up.
  void scrollBottom()
  return true
}

function lastSettledAgentText(live: ClarifyTurn): string {
  const list = displayTurns.value
  for (let i = list.length - 1; i >= 0; i--) {
    const t = list[i]
    if (t.role !== 'agent' || t === live || t.streaming) continue
    return (t.text || '').trim()
  }
  return ''
}

/** Live agent bubble has visible message body (text or coalesced markdown). */
function agentHasMessage(t: ClarifyTurn): boolean {
  return !!(t.text || (t.streaming && liveStreamHtml.value))
}

/**
 * Streaming agent whose newest step is message text: tail shows 输出中.
 * After a tool or thought step (waiting for the model) it shows 思考中….
 */
function agentTailOutputting(t: ClarifyTurn): boolean {
  if (t.role !== 'agent' || !t.streaming) return false
  const parts = t.parts || []
  for (let i = parts.length - 1; i >= 0; i--) {
    const p = parts[i]!
    if (p.kind === 'tool') return false
    if ((p.kind === 'message' || p.kind === 'thought') && p.text?.trim()) return p.kind === 'message'
  }
  if (t.tools?.length) return false
  return agentHasMessage(t)
}

/** Display thought (revealed while this live agent is streaming). */
function agentThoughtDisplay(t: ClarifyTurn, idx: number): string {
  if (t.streaming && idx === liveAgentIdx.value) {
    return liveThoughtText.value
  }
  return t.thought || ''
}

/** Default open while thought-only; collapse once message exists (Demo). */
function isThoughtOpen(idx: number, t: ClarifyTurn): boolean {
  if (thoughtOpenOverride.value[idx] !== undefined) {
    return thoughtOpenOverride.value[idx]!
  }
  return !!(t.streaming && t.thought && !agentHasMessage(t))
}

function onThoughtToggle(idx: number, e: Event) {
  const el = e.target as HTMLDetailsElement
  thoughtOpenOverride.value = { ...thoughtOpenOverride.value, [idx]: el.open }
}

/** Normal completion footnote (not interrupted / empty-fail / error banner). */
function showTurnCompleted(t: ClarifyTurn): boolean {
  return (
    t.role === 'agent' &&
    !t.streaming &&
    !t.handoff &&
    !t.interrupted &&
    !isRetryableFailedAgent(t) &&
    !!(t.text || t.thought)
  )
}

/** Index in displayTurns of the latest retryable empty/failure agent (or -1). */
function latestRetryableFailIndex(): number {
  const list = displayTurns.value
  for (let i = list.length - 1; i >= 0; i--) {
    if (isRetryableFailedAgent(list[i])) return i
  }
  return -1
}

/**
 * Only the trailing empty/failure agent shows a clickable retry.
 * Must be the last display turn — matches backend retryLast (rejects when a
 * later human/agent already followed the empty fail).
 */
function showFailRetry(t: ClarifyTurn, idx: number): boolean {
  if (props.done || !props.active) return false
  if (!isRetryableFailedAgent(t)) return false
  const list = displayTurns.value
  if (idx !== list.length - 1) return false
  // Live recovery must preserve its operation, selected candidate and params.
  // The generic retry endpoint only replays text; use the Live card instead.
  for (let i = idx - 1; i >= 0; i--) {
    if (list[i]?.role === 'human') {
      if (list[i]?.live) return false
      break
    }
  }
  return idx === latestRetryableFailIndex()
}

const failRetryDisabled = computed(
  () => sessionBusy.value || queued.value.length > 0 || props.done,
)

/**
 * Cover-this-turn retry: emit retry-last; keep draft untouched; optimistically
 * re-open the failed agent as a streaming slot when it is still in liveTurns
 * (or seed live from the trailing persisted human + empty agent).
 */
function retryLastFailed() {
  if (failRetryDisabled.value) return
  const list = displayTurns.value
  const failIdx = latestRetryableFailIndex()
  if (failIdx < 0) return
  // Prefer the human immediately before the failed agent.
  let human: ClarifyTurn | null = null
  for (let i = failIdx - 1; i >= 0; i--) {
    if (list[i]?.role === 'human') {
      human = list[i]!
      break
    }
  }
  if (!human || human.live) return

  // Optimistic: put human + streaming agent into liveTurns (merge replaces
  // persisted empty/failure for the same human).
  liveTurns.value = [
    {
      role: 'human',
      text: human.text || '',
      at: human.at || new Date().toISOString(),
      images: human.images,
      annotations: human.annotations,
    },
    {
      role: 'agent',
      text: '',
      thought: '',
      at: new Date().toISOString(),
      startedAt: new Date().toISOString(),
      streaming: true,
    },
  ]
  liveAgentIdx.value = 1
  thinking.value = true
  thoughtOpenOverride.value = {}
  messageReveal.reset()
  thoughtReveal.reset()
  streamPreview.reset()
  thoughtPreview.reset()
  emit('retry-last')
  void scrollBottom()
}


  return {
    humanMatchesSeed,
    prependSeedHuman,
    isNearBottom,
    onScrollerScroll,
    scrollBottom,
    enterStickSequence,
    onUnreadFabClick,
    autoGrow,
    onTextInput,
    textHasChoicePrefix,
    stripChoicePrefix,
    isChoiceReply,
    textHasSkipPrefix,
    isSkipReply,
    formatSkipReply,
    closesQuestionRound,
    latestQuestionTurnIndex,
    latestFormTurnIndex,
    hasHumanReplyAfter,
    imagePreviewLabel,
    openImagePreview,
    turnsSemanticKey,
    addFiles,
    onPickFiles,
    onPaste,
    removeAttachment,
    sendMessage,
    prepareComposerSend,
    sendFromComposer,
    composerSending,
    onComposerKeydown,
    removeAnnotation,
    isActiveTurn,
    isActiveFormTurn,
    isSelected,
    toggle,
    answered,
    autoPickId,
    applyRecommended,
    submitRecommended,
    prevCard,
    nextCard,
    pick,
    submitChoices,
    parseChoiceSummary,
    submitForms,
    parseFormSummary,
    formFieldKey,
    fieldWhy,
    formInputType,
    choiceRowsForAgentTurn,
    selectedLabelsForQuestion,
    selectedDemoForInteractive,
    send,
    finishEarly,
    finishAbort,
    confirmFlowDialogOpen,
    confirmFlowDiscard,
    acceptConfirmFlow,
    dismissConfirmFlow,
    releaseConfirmFlowWindow,
    playConfirmCeremony,
    followKeepsComposer,
    showDoneChrome,
    confirmFlowPlaying,
    cancelReview,
    discardLastQueued,
    restoreUnsentQueued,
    visitEndedNotice,
    sendNotice,
    stashNotice,
    forceAuthoritativeIdle,
    settleAfterTurnEnd,
    applyQueueState,
    applyReviewFrame,
    applyAcpEvents,
    agentHasMessage,
    agentTailOutputting,
    agentThoughtDisplay,
    isThoughtOpen,
    onThoughtToggle,
    showTurnCompleted,
    isRetryableFailedAgent,
    isEmptyFailedAgent,
    isFailureAssistantText,
    emptyFailDisplayText,
    showFailRetry,
    failRetryDisabled,
    retryLastFailed,
    showAnnotationChips,
    persistedTurns,
    inputPlaceholder,
    thinking,
    validating,
    releaseConfirmPending,
    settleConfirmedTurn,
    queued,
    liveTurns,
    showClarifyEmptyHint,
    seedHumanTurn,
    liveAgentIdx,
    showSandboxOrphanBanner,
    sandboxOrphanOpLabel,
    liveStreamHtml,
    liveStreamBlocks,
    streamPreview,
    unsubStream,
    liveThoughtText,
    thoughtPreview,
    unsubThought,
    syncReveal,
    messageReveal,
    thoughtReveal,
    thoughtOpenOverride,
    scroller,
    fileInput,
    textareaRef,
    overflowScroll,
    composing,
    STICK_THRESHOLD,
    stickToBottom,
    unreadCount,
    showUnreadFab,
    unreadFabLabel,
    sessionBusy,
    AUTO_GROW_MIN,
    AUTO_GROW_MAX,
    CHOICE_PREFIX_ZH,
    choicePrefix,
    FORM_PREFIX_ZH,
    formPrefix,
    SKIP_PREFIX_ZH,
    SKIP_USER_LABEL_ZH,
    SKIP_PREFIX_EN,
    SKIP_USER_LABEL_EN,
    skipPrefix,
    skipUserReplyLabel,
    latestQuestionIdx,
    latestQuestions,
    latestQuestionAnswered,
    pendingQuestionsOpen,
    latestFormIdx,
    latestForms,
    latestFormAnswered,
    pendingFormsOpen,
    pendingInteractiveOpen,
    displayTurns,
    outputBounds,
    attachNotice,
    queueNotice,
    queueToast,
    hasComposerDraft,
    cancelQueuedItem,
    reorderQueuedItems,
    editQueuedItem,
    sel,
    other,
    otherChecked,
    isOtherSelected,
    toggleOther,
    otherAnswered,
    activeQuestions,
    someAnswered,
    hasRecommended,
    activeForms,
    formValues,
    formNotice,
    formCanSubmit,
    step,
    curQuestion,
    isFirstCard,
    isLastCard,
    confirmDisabled,
    translate,
    locale,
    imagePreview,
    openChatImagePreview,
    closeImagePreview,
    renderMarkdown,
    relTime,
    demoOptionsOf,
    selectedDemoOption,
    isSessionBusy: () => sessionBusy.value,
  }
}

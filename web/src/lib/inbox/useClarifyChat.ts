import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { renderMarkdown } from '@/lib/shared/markdown'
import { expandAutoPhase } from '@/lib/run/takeoverTimeline'
import {
  dropClarifyOutbox,
  newClientMessageId,
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
import { emptyFailDisplayText, isRetryableFailedAgent } from '@/lib/inbox/clarifyEmptyFail'
import { createReplyText, latestFormTurnIndex, latestQuestionTurnIndex, type ChoiceRow } from '@/lib/inbox/clarifyReplyText'
import { relTime as formatRelTime } from '@/lib/shared/format'
import { holdSidebarConfirmWindow, releaseSidebarConfirmWindow } from '@/lib/inbox/sidebarHandoff'
import {
  demoOptionsOf,
  restoreChoiceAnswers,
  selectedDemoOption,
} from '@/lib/inbox/clarifyDemo'
import type {
  ClarifyTurn,
  ClarifyImage,
  ReactQuestion,
  ReactOption,
  ReactForm,
  ReactFormField,
  ReactAnnotation,
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
  addOptimisticSend,
  dialogueKey,
  getDialogue,
  optimisticSends,
  rejectOptimisticSend,
} from '@/lib/chat/dialogueStore'
import { render, type DialogueQueueRow, type DialogueRow, type DialogueVersion } from '@/lib/chat/dialogueState'
import {
  isSettled, isStopping, pruneQueueOverlay, retryAttemptRows, retryTakenOver, settleRows, visibleOrphan, visibleQueue,
  type QueueOverlay,
} from '@/lib/chat/dialogueOverlay'

const CLARIFY_VISIT_ENDED = 'clarify_visit_ended'

function cloneReactAnnotations(anns?: ReactAnnotation[] | null): ReactAnnotation[] {
  return anns?.length ? anns.map((a) => ({ ...a })) : []
}

function cloneClarifyImages(imgs?: ClarifyImage[] | null): ClarifyImage[] {
  return imgs?.length ? imgs.map((im) => ({ ...im })) : []
}
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
  sendRequest?: (text: string, images: ClarifyImage[], annotations: ReactAnnotation[], clientMessageId?: string) => Promise<boolean>
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
  (e: 'send', text: string, images: ClarifyImage[], annotations: ReactAnnotation[], clientMessageId?: string): void
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

// What is drawn comes from the server dialogue (dialogueStore) for this
// visit plus this page's optimistic sends. Without a server dialogue (cold
// history, unit mounts) the host's turns are shown as they are.
const storeKey = computed(() => dialogueKey(props.runId, props.nodeId))
const serverState = computed(() => {
  const st = getDialogue(storeKey.value)
  if (!st) return null
  if (st.iteration && props.iteration && st.iteration !== props.iteration) return null
  return st
})
const view = computed(() => render(serverState.value, optimisticSends(storeKey.value)))

// Page-local overlays (dialogueOverlay.ts): each covers a click until the server state answers it.
const settledAt = ref<DialogueVersion | null>(null)
const forcedIdle = computed(() => isSettled(settledAt.value, serverState.value))
/** Retry was clicked; the failed reply shows as a new attempt until the server starts it. */
const retryPending = ref(false)
let retryPendingTimer = 0
/** The turn Stop was clicked on; it shows as stopping while the server still runs it. */
const stoppingTurn = ref<string | null>(null)

const sourceRows = computed<ClarifyTurn[]>(() => {
  const rows = serverState.value ? view.value.rows : [...(props.turns ?? []), ...view.value.rows]
  const out = forcedIdle.value ? settleRows(rows) : rows
  return retryPending.value ? retryAttemptRows(out) : out
})

function rowPhase(t: ClarifyTurn): DialogueRow['phase'] | undefined {
  return (t as Partial<DialogueRow>).phase
}

function rowId(t: ClarifyTurn | undefined): string | undefined {
  return (t as Partial<DialogueRow> | undefined)?.id
}

// A takeover's opening turn carries the auto phase (turns[0].events): continue it.
const persistedTurns = computed(() =>
  expandAutoPhase(sourceRows.value.filter((t) => !t.localPending && rowPhase(t) !== 'running' && rowPhase(t) !== 'orphan')),
)

/** Only a sandbox turn the platform no longer runs: its banner offers Stop, the chat is idle. */
const orphanOnly = computed(() => !!view.value.orphan &&
  !(serverState.value?.turns ?? []).some((t) => t.phase === 'running') && !view.value.rows.some((r) => r.localPending))
const thinking = computed(() => !props.done && !forcedIdle.value &&
  ((view.value.thinking && !orphanOnly.value) || retryPending.value))
/** Review confirm mid-state: re-validating product (not Agent thinking). */
const validating = ref(false)
/** Success overlay host from ReviewShell (g2.1); optional in unit mounts. */
const confirmFlow = useConfirmFlowCeremony()
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
  if (!confirmFlow) return
  // Same click may play from finishEarly and again from parent force — keep one play (g1.1).
  if (confirmFlow.playing.value) return
  await confirmFlow.play()
}
let conversationGeneration = 0

const queueOverlay = ref<QueueOverlay>({ hidden: [], order: null })
const queued = computed<DialogueQueueRow[]>(() => {
  let rows = view.value.queue
  // A host-sent (@send) message stays cancellable in the queue until the server lists it.
  const head = props.sendRequest ? undefined : view.value.rows.find((r) => r.localPending && r.role === 'human')
  if (head) {
    rows = [{ clientMessageId: head.clientMessageId, text: head.text, images: head.images ?? [],
      annotations: head.annotations ?? [], local: true }, ...rows]
  }
  return visibleQueue(rows, queueOverlay.value)
})
watch(
  () => view.value.queue.map((q) => q.id || '').join('|'),
  () => {
    const ids = view.value.queue.map((q) => q.id).filter((id): id is string => !!id)
    queueOverlay.value = pruneQueueOverlay(queueOverlay.value, ids)
  },
)
function hideQueueRow(id: string) {
  const o = queueOverlay.value
  if (!o.hidden.includes(id)) queueOverlay.value = { ...o, hidden: [...o.hidden, id] }
}

const showClarifyEmptyHint = computed(
  () =>
    !props.reviewMode &&
    sourceRows.value.length === 0 &&
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
const liveAgentIdx = computed(() => {
  const list = displayTurns.value
  for (let i = list.length - 1; i >= 0; i--) {
    if (list[i]!.role === 'agent' && list[i]!.streaming) return i
  }
  return -1
})
const runningTurnIds = computed(() => forcedIdle.value ? [] :
  (serverState.value?.turns ?? []).filter((t) => t.phase === 'running' || t.phase === 'orphan').map((t) => t.turnId))
const turnRunning = computed(() => runningTurnIds.value.length > 0)
const stopping = computed(() => isStopping(stoppingTurn.value, runningTurnIds.value))
watch(
  () => serverState.value,
  (st) => {
    if (!retryPending.value || !st) return
    if (retryTakenOver(st)) clearRetryPending()
  },
)

function clearRetryPending() {
  retryPending.value = false
  clearTimeout(retryPendingTimer)
}

type OutputClock = { startedAt: string; endedAt?: string }
/** This page's wall clock for replies it saw running, by row id. */
const rowClocks = ref<Record<string, OutputClock>>({})
watch(
  sourceRows,
  (rows) => {
    const now = new Date().toISOString()
    for (const r of rows) {
      const id = rowId(r)
      if (!id || r.role !== 'agent') continue
      const clock = rowClocks.value[id]
      const open = !!r.streaming || rowPhase(r) === 'running' || rowPhase(r) === 'orphan'
      if (open && !clock) rowClocks.value[id] = { startedAt: now }
      else if (!open && clock && !clock.endedAt) clock.endedAt = now
    }
  },
  { immediate: true },
)
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
  clearTimeout(retryPendingTimer)
  composerResizeObserver?.disconnect()
  composerResizeObserver = null
})

const replyText = createReplyText(translate)
const {
  textHasChoicePrefix, stripChoicePrefix, isChoiceReply, textHasSkipPrefix, isSkipReply, formatSkipReply,
  closesQuestionRound, hasHumanReplyAfter, parseChoiceSummary, parseFormSummary,
} = replyText
const choicePrefix = computed(() => replyText.choicePrefix())
const formPrefix = computed(() => replyText.formPrefix())

const latestQuestionIdx = computed(() => latestQuestionTurnIndex(persistedTurns.value))

const latestQuestions = computed<ReactQuestion[]>(() => {
  const idx = latestQuestionIdx.value
  if (idx < 0) return []
  return persistedTurns.value[idx].questions ?? []
})

/** Transcript including the running turn and this page's pending send. */
function answerTranscript(): ClarifyTurn[] {
  return sourceRows.value
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

const displayTurns = computed<ClarifyTurn[]>(() => prependSeedHuman(expandAutoPhase(sourceRows.value)))

/** Start/end for each visible agent reply: its own stamps, else this page's clock, else the turn span. */
function outputBounds(index: number): { startedAt?: string; endedAt?: string } {
  const turns = displayTurns.value
  const t = turns[index]
  if (!t || t.role !== 'agent') return {}
  if (t.startedAt && t.endedAt) return { startedAt: t.startedAt, endedAt: t.endedAt }
  const id = rowId(t)
  const clock = id ? rowClocks.value[id] : undefined
  if (clock) return { startedAt: t.startedAt || clock.startedAt, endedAt: t.endedAt || clock.endedAt }
  return replySpan(turns[index - 1], t) || {}
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

/** Take a row out of the pending-send panel: a local send is withdrawn, a server one hidden until it leaves. */
function dropQueueRow(row: DialogueQueueRow) {
  if (row.local && row.clientMessageId) {
    rejectOptimisticSend(storeKey.value, row.clientMessageId)
    return
  }
  if (row.id) hideQueueRow(row.id)
}

function cancelQueuedItem(index: number) {
  const removed = queued.value[index]
  if (!removed) return
  dropQueueRow(removed)
  const preview = truncateQueueText(removed.text || '', 18)
  showQueueToast(translate('pages.clarify.queueCancelled', { text: preview }))
  if (removed.id) emit('queue-remove', removed.id, index)
}

function reorderQueuedItems(fromIndex: number, toIndex: number) {
  const list = [...queued.value]
  if (
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= list.length ||
    toIndex >= list.length ||
    fromIndex === toIndex
  ) {
    return
  }
  const [moved] = list.splice(fromIndex, 1)
  list.splice(toIndex, 0, moved!)
  const ids = list.map((q) => q.id).filter((id): id is string => !!id)
  queueOverlay.value = { ...queueOverlay.value, order: ids }
  showQueueToast(translate('pages.clarify.queueReordered'))
  if (ids.length === list.length && ids.length > 0) {
    emit('queue-reorder', ids)
  }
}

function sameQueueRow(a: DialogueQueueRow, b: DialogueQueueRow): boolean {
  if (a.id || b.id) return a.id === b.id
  return !!a.clientMessageId && a.clientMessageId === b.clientMessageId
}

async function editQueuedItem(index: number) {
  if (index < 0 || index >= queued.value.length) return
  const target = queued.value[index]!
  queueNotice.value = null

  // Composer already has an unsent draft (text / attachments / annotation chips):
  // re-enqueue it first so chips are never discarded to unblock edit.
  if (hasComposerDraft() && props.sendRequest) {
    if (!await sendFromComposer()) return
    // A new draft typed during acknowledgement must stay with its author.
    if (hasComposerDraft()) return
    index = queued.value.findIndex((item) => sameQueueRow(item, target))
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

  const item = queued.value[index]!
  dropQueueRow(item)
  draft.value = item.text
  attachments.value = cloneClarifyImages(item.images)
  annotations.value = cloneReactAnnotations(item.annotations)
  attachNotice.value = null
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

// New rows and a growing reply follow the bottom only while stuck to it.
watch(
  () => {
    const list = displayTurns.value
    const last = list[list.length - 1]
    return `${turnsSemanticKey(list)}:${last?.thought?.length ?? 0}:${last?.parts?.length ?? 0}:${last?.tools?.length ?? 0}`
  },
  () => void scrollBottom(),
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
    conversationGeneration++
    queueOverlay.value = { hidden: [], order: null }
    composerSending.value = false
    stoppingTurn.value = null
    cancelledOrphanOp.value = null
    settledAt.value = null
    clearRetryPending()
    seenEndedTurns.clear()
    endedTurnsPrimed = false
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
      validating.value = false
      // Overlay is owned by the click path (finishEarly / parent force). Never
      // auto-play on done/node_complete (plan g1.1) — that caused the #613 regression.
    } else {
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

/** What this page sent, by clientMessageId, so a refused send can go back into the composer as typed. */
type SentOrigin = { text: string; composerText?: string; images: ClarifyImage[]; annotations: ReactAnnotation[] }
const sentOrigins = new Map<string, SentOrigin>()

function trackOptimisticSend(clientMessageId: string, origin: SentOrigin) {
  sentOrigins.set(clientMessageId, origin)
  addOptimisticSend(storeKey.value, {
    clientMessageId,
    text: origin.text,
    images: origin.images,
    annotations: origin.annotations,
    at: new Date().toISOString(),
  })
  stickToBottom.value = true
  unreadCount.value = 0
  void scrollBottom(true)
}

/** The server dialogue already lists this send (the socket can beat the HTTP answer). */
function serverHasSend(clientMessageId: string): boolean {
  const st = serverState.value
  if (!st) return false
  return st.rows.some((r) => r.clientMessageId === clientMessageId) ||
    st.turns.some((t) => t.clientMessageId === clientMessageId || t.item?.clientMessageId === clientMessageId)
}

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
  const clientMessageId = newClientMessageId()
  trackOptimisticSend(clientMessageId, { text: t, composerText, images: imgs, annotations: cloneReactAnnotations(anns) })
  emit('send', t, imgs, anns, clientMessageId)
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
    const clientMessageId = newClientMessageId()
    const generation = conversationGeneration
    const identity = [props.runId, props.nodeId, props.iteration] as const
    const ownsRequest = () => generation === conversationGeneration &&
      props.runId === identity[0] && props.nodeId === identity[1] && props.iteration === identity[2]
    try {
      const acknowledged = await props.sendRequest(prepared.text, prepared.images, prepared.annotations, clientMessageId)
      // The socket can prove acceptance even when the HTTP response was lost.
      if (!acknowledged && !serverHasSend(clientMessageId)) return false
      if (!ownsRequest()) return false
      if (draft.value === originalDraft) draft.value = ''
      if (attachments.value === originalImages && JSON.stringify(attachments.value) === JSON.stringify(imgs)) attachments.value = []
      if (annotations.value === originalAnnotations && JSON.stringify(annotations.value) === JSON.stringify(anns)) annotations.value = []
      attachNotice.value = null
      sendNotice.value = null
      if (!hasComposerDraft()) releaseStash()
      trackOptimisticSend(clientMessageId, {
        text: prepared.text, composerText: t, images: prepared.images, annotations: cloneReactAnnotations(prepared.annotations),
      })
    } finally {
      if (ownsRequest()) composerSending.value = false
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
  // Clarify: keep the queue; the server state says what is left.
  if (props.reviewMode) {
    for (const o of optimisticSends(storeKey.value)) rejectOptimisticSend(storeKey.value, o.clientMessageId)
  }
  clearRetryPending()
  if (turnRunning.value) stoppingTurn.value = runningTurnIds.value[0]!
  if (orphan) cancelledOrphanOp.value = sandboxOrphan.value!.runningOpId
  emit('cancel')
  void scrollBottom()
}

/**
 * Parent calls this when reactReply(force=false) fails after optimistic enqueue,
 * so the pending-send panel and FR4 confirm gate do not stick forever.
 */
function discardLastQueued(clientMessageId?: string) {
  const list = optimisticSends(storeKey.value)
  const target = clientMessageId ? list.find((o) => o.clientMessageId === clientMessageId) : list[list.length - 1]
  if (target) rejectOptimisticSend(storeKey.value, target.clientMessageId)
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
  conversationGeneration++
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
    clientMessageId?: string
    /** Send time (outbox), so several refusals come back in send order. */
    at?: number
    /** Inline images the outbox already had to leave out. */
    imagesDropped?: number
  },
  notice: string,
  opts: { autoRetryOnResume?: boolean } = {},
) {
  // This page's send first: by clientMessageId, then by content among its
  // optimistic copies and the server queue rows it sent.
  const pending = optimisticSends(storeKey.value)
  let cmid = sent.clientMessageId
  if (!cmid && (sent.text || sent.images?.length || sent.annotations?.length)) {
    const same = (o: { text?: string; images?: unknown[]; annotations?: unknown[] }) => (o.text ?? '') === (sent.text ?? '') &&
      (!sent.images || (o.images?.length ?? 0) === sent.images.length) &&
      (!sent.annotations || (o.annotations?.length ?? 0) === sent.annotations.length)
    cmid = [...pending].reverse().find(same)?.clientMessageId ??
      [...view.value.queue].reverse().find((q) => !!q.clientMessageId && sentOrigins.has(q.clientMessageId) && same(q))?.clientMessageId
  }
  if (!cmid && sent.clientId) hideQueueRow(sent.clientId)
  const local = cmid ? pending.find((o) => o.clientMessageId === cmid) : undefined
  const origin = cmid ? sentOrigins.get(cmid) : undefined
  if (cmid) rejectOptimisticSend(storeKey.value, cmid)
  sendNotice.value = notice
  if (opts.autoRetryOnResume) return

  const text = (origin?.composerText ?? origin?.text ?? local?.text ?? sent.text ?? '').trim()
  const imgs = origin?.images ?? local?.images ?? sent.images ?? []
  const anns = origin?.annotations ?? local?.annotations ?? sent.annotations ?? []
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
 * delivered the turn's end was closed at the click. Drop 思考中 / 生成中 on
 * what is shown now. Confirm failure must not call this.
 */
function settleConfirmedTurn() {
  const st = serverState.value
  settledAt.value = st ? { epoch: st.epoch, seq: st.seq } : { epoch: 0, seq: 0 }
  clearRetryPending()
}

/**
 * The server refused a send before it became a turn (WS request_rejected).
 * Only this tab's own send is put back.
 */
function applyRequestRejected(frame: {
  clientMessageId?: string
  code?: string
  message?: string
  autoRetryOnResume?: boolean
}) {
  const cmid = frame.clientMessageId
  const own = !!cmid && (sentOrigins.has(cmid) || optimisticSends(storeKey.value).some((o) => o.clientMessageId === cmid))
  const mine = takeClarifyOutbox(props.runId, props.nodeId, cmid, { own })
  if (!own && !mine) return
  restoreUnsentQueued({
    clientMessageId: cmid,
    text: mine?.text,
    images: mine?.images,
    annotations: mine?.annotations,
    at: mine?.at,
    imagesDropped: mine?.imagesDropped,
  }, frame.code === CLARIFY_VISIT_ENDED
    ? visitEndedNotice(frame.autoRetryOnResume)
    : frame.message || 'error', { autoRetryOnResume: frame.autoRetryOnResume })
}

// Turns that ended while this chat was open: an answered send leaves the
// outbox; a send dropped because the visit ended goes back to its author.
const seenEndedTurns = new Set<string>()
let endedTurnsPrimed = false
watch(
  () => serverState.value?.turns,
  (turns) => {
    if (!turns) return
    for (const t of turns) {
      if (t.phase !== 'ended' || seenEndedTurns.has(t.turnId)) continue
      seenEndedTurns.add(t.turnId)
      const cmid = t.clientMessageId
      if (t.outcome !== 'dropped') {
        if (cmid) dropClarifyOutbox(props.runId, props.nodeId, cmid)
        continue
      }
      if (!endedTurnsPrimed || t.code !== CLARIFY_VISIT_ENDED) continue
      if (t.autoRetryOnResume) {
        restoreUnsentQueued({}, visitEndedNotice(true), { autoRetryOnResume: true })
        continue
      }
      // Other viewers' sends carry nothing to put back here.
      const mine = takeClarifyOutbox(props.runId, props.nodeId, cmid)
      if (!mine) continue
      restoreUnsentQueued({
        clientMessageId: cmid,
        text: mine.text,
        images: mine.images,
        annotations: mine.annotations,
        at: mine.at,
        imagesDropped: mine.imagesDropped,
      }, visitEndedNotice(false))
    }
    endedTurnsPrimed = true
  },
  { immediate: true },
)

/** A sandbox turn still running after the platform stopped tracking it (server-decided). */
/** The orphan op Stop was clicked for; hidden until the server state drops it. */
const cancelledOrphanOp = ref<string | null>(null)
const sandboxOrphan = computed(() =>
  forcedIdle.value || props.done ? null : visibleOrphan(view.value.orphan, cancelledOrphanOp.value))
watch(() => view.value.orphan, (orphan) => {
  if (!orphan) cancelledOrphanOp.value = null
})

const showSandboxOrphanBanner = computed(() => !!sandboxOrphan.value && queued.value.length === 0)

const sandboxOrphanOpLabel = computed(() => {
  const id = sandboxOrphan.value?.runningOpId
  return id ? ` (${id})` : ''
})

/** Agent bubble has visible message body. */
function agentHasMessage(t: ClarifyTurn): boolean {
  return !!t.text
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

/** Thought text to show for this agent row. */
function agentThoughtDisplay(t: ClarifyTurn, _idx: number): string {
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
 * Cover-this-turn retry: emit retry-last; keep draft untouched. The failed
 * reply shows as a new attempt until the server's state takes over.
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
  retryPending.value = true
  clearTimeout(retryPendingTimer)
  retryPendingTimer = window.setTimeout(() => {
    retryPending.value = false
  }, 15_000)
  thoughtOpenOverride.value = {}
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
    isSkipReply,
    latestQuestionTurnIndex,
    hasHumanReplyAfter,
    imagePreviewLabel,
    openImagePreview,
    turnsSemanticKey,
    addFiles,
    onPickFiles,
    onPaste,
    removeAttachment,
    sendMessage,
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
    applyRequestRejected,
    sendNotice,
    stashNotice,
    agentHasMessage,
    agentTailOutputting,
    agentThoughtDisplay,
    isThoughtOpen,
    onThoughtToggle,
    showTurnCompleted,
    isRetryableFailedAgent,
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
    showClarifyEmptyHint,
    seedHumanTurn,
    liveAgentIdx,
    stopping,
    showSandboxOrphanBanner,
    sandboxOrphanOpLabel,
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
    choicePrefix,
    latestQuestionIdx,
    latestQuestions,
    latestQuestionAnswered,
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

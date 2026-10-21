<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import ChatImageThumb from '../ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '../ui/ChatImagePreviewModal.vue'
import ComposerShell from './ComposerShell.vue'
import ClarifyDemoFrame from './ClarifyDemoFrame.vue'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'
import InboxMotionMark from '@/components/inbox/InboxMotionMark.vue'
import AgentToolGroup from './AgentToolGroup.vue'
import AgentTimeline from './AgentTimeline.vue'
import StreamMarkdown from './StreamMarkdown.vue'
import AnnotationChip from './AnnotationChip.vue'
import LiveVariantCard from './LiveVariantCard.vue'
import PendingSendQueuePanel from './PendingSendQueuePanel.vue'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'
import { lastMessageText } from '@/lib/chat/messagePreview'
import { createClarifyChatAnchors } from './clarifyChatAnchors'
import { computed, inject, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { choiceEchoRows } from '@/lib/inbox/clarifyDemo'
import {
  clampDemoWindow,
  isClarifyDemoEscapeMessage,
  moveDemoWindow,
  placeClarifyDemoWindow,
  resizeDemoWindow,
  withDemoEscapeBridge,
  type DemoWindowBox,
  type DemoWindowEdge,
} from '@/lib/inbox/clarifyDemoWindow'
import { EMBED_DEMO_PREVIEW_KEY } from '@/lib/inbox/embedDemoPreview'
import { EMBED_IMAGE_PREVIEW_KEY } from '@/lib/inbox/embedImagePreview'
import { LIVE_CARD_HOST, isLiveBusy } from '@/lib/inbox/liveVariants'
import type {
  ClarifyTurn,
  ClarifyImage,
  ReactAnnotation,
  ReactQuestion,
} from '@/lib/shared/types'
import { isImageAttachment, attachmentDisplayName } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import { renderChatMarkdown } from '@/lib/shared/markdown'
import { useClarifyChat } from '@/lib/inbox/useClarifyChat'
import ConfirmFlowDialog from './ConfirmFlowDialog.vue'
import { useComposerActionMotion } from '@/lib/composables/useComposerActionMotion'

const props = withDefaults(
  defineProps<{
    runId: string
    nodeId: string
    iteration: number
    turns?: ClarifyTurn[] | null
    done: boolean
    active?: boolean
    reviewMode?: boolean
    annotateEnabled?: boolean
    hideFinish?: boolean
    /** Session history plus confirm only; input/send stay unmounted. */
    coldSession?: boolean
    /** Host-level lock for the final confirmation action. */
    finishDisabled?: boolean
    sendLabel?: string
    sendRequest?: (text: string, images: ClarifyImage[], annotations: ReactAnnotation[]) => Promise<boolean>
    confirmError?: string | null
    confirmCanAbort?: boolean
    seedHumanText?: string
    seedHumanImages?: ClarifyImage[]
    /**
     * Watching an auto node: read-only stream, input disabled, the only action
     * is Stop, which asks the host to take the node over (emit takeover).
     */
    observe?: boolean
    /** Takeover accepted (state=stopping); waiting for node_takeover waiting_human. */
    stopping?: boolean
    /** Taken-over node's task: shown as the opening User Message, above the auto-phase fold row. */
    nodeTask?: string
  }>(),
  {
    active: true,
    iteration: 1,
    reviewMode: false,
    annotateEnabled: false,
    hideFinish: false,
    coldSession: false,
    finishDisabled: false,
    confirmError: null,
    confirmCanAbort: false,
    turns: () => [],
    seedHumanText: '',
    seedHumanImages: () => [],
    observe: false,
    stopping: false,
    nodeTask: '',
  },
)

/** Live request text written for the agent; the Live card already tells the user. */
function isLiveCardText(t: ClarifyTurn): boolean {
  return !!t.live && (t.live.generated === true || /^Live · /.test(t.text || ''))
}

/** Only the preview drawer can drive candidates; elsewhere Live cards are not shown. */
const liveHost = inject(LIVE_CARD_HOST, null)

/** Human bubble text: generated Live text falls back to the user's prompt without a card. */
function humanBubbleText(t: ClarifyTurn): string {
  if (!isLiveCardText(t)) return t.text || ''
  return liveHost ? '' : t.live?.prompt || ''
}

/** turn_begin of an auto stream carries no human text; nothing to draw. */
function isBlankHumanTurn(t: ClarifyTurn): boolean {
  return t.role === 'human' && !t.text && !t.images?.length && !t.annotations?.length
}

/** A generated Live turn with nothing left to show once the card is gone. */
function isEmptyLiveTurn(t: ClarifyTurn): boolean {
  return t.role === 'human' && !liveHost && isLiveCardText(t) && !humanBubbleText(t) && !t.images?.length && !t.annotations?.length
}

const emit = defineEmits<{
  (e: 'send', text: string, images: ClarifyImage[], annotations: ReactAnnotation[]): void
  (e: 'retry-last'): void
  (e: 'finish'): void
  (e: 'finish-abort'): void
  (e: 'cancel'): void
  (e: 'takeover'): void
  (e: 'queue-remove', itemId: string | undefined, index: number): void
  (e: 'queue-reorder', itemIds: string[]): void
}>()

const draft = defineModel<string>('draft', { default: '' })
const attachments = defineModel<ClarifyImage[]>('attachments', { default: () => [] })
const annotations = defineModel<ReactAnnotation[]>('annotations', { default: () => [] })

const chat = useClarifyChat(props, emit, { draft, attachments, annotations })

defineExpose({
  applyReviewFrame: chat.applyReviewFrame,
  applyAcpEvents: chat.applyAcpEvents,
  applyQueueState: chat.applyQueueState,
  cancelReview: chat.cancelReview,
  discardLastQueued: chat.discardLastQueued,
  restoreUnsentQueued: chat.restoreUnsentQueued,
  isSessionBusy: chat.isSessionBusy,
  reorderQueuedItems: chat.reorderQueuedItems,
  playConfirmCeremony: chat.playConfirmCeremony,
})

const {
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
  latestQuestionTurnIndex,
  hasHumanReplyAfter,
  imagePreviewLabel,
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
  send,
  finishEarly,
  finishAbort,
  confirmFlowDialogOpen,
  confirmFlowDiscard,
  acceptConfirmFlow,
  dismissConfirmFlow,
  playConfirmCeremony,
  showDoneChrome,
  confirmFlowPlaying,
  cancelReview,
  discardLastQueued,
  forceAuthoritativeIdle,
  settleAfterTurnEnd,
  applyQueueState,
  applyReviewFrame,
  applyAcpEvents,
  agentHasMessage,
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
  choicePrefix,
  latestQuestionIdx,
  latestQuestions,
  latestQuestionAnswered,
  displayTurns,
  outputBounds,
  attachNotice,
  sendNotice,
  stashNotice,
  sel,
  other,
  otherChecked,
  isOtherSelected,
  toggleOther,
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
  relTime,
  demoOptionsOf,
  selectedDemoForInteractive,
  selectedDemoOption,
  queueNotice,
  queueToast,
  hasComposerDraft,
  cancelQueuedItem,
  reorderQueuedItems,
  editQueuedItem,
} = chat

/**
 * Observe (read-only auto run) view: while the agent is busy and no message
 * text has arrived, 「思考中…」 is the only busy indicator — the thought
 * summary's 生成中… badge and the bottom 「刚刚」 time wait for the message.
 * The interactive chat keeps its existing indicators.
 */
function observeQuietBusy(t: ClarifyTurn): boolean {
  return !!props.observe && t.role === 'agent' && !!t.streaming && !agentHasMessage(t)
}

const railScopeKey = computed(() => `${props.runId}:${props.nodeId}:${props.iteration}`)
const resolveMessageAnchors = createClarifyChatAnchors(`clarify-${useId().replace(/:/g, '')}`)
const messageAnchorState = computed(() => resolveMessageAnchors(railScopeKey.value, displayTurns.value))
const messageRailItems = computed(() => {
  const rows = displayTurns.value
  return rows.flatMap((turn, index) => {
    const id = messageAnchorState.value.anchors[index]
    if (!id || isEmptyLiveTurn(turn) || (props.observe && isBlankHumanTurn(turn))) return []
    let end = index + 1
    while (end < rows.length && rows[end]!.role !== 'human') end++
    let preview = ''
    for (let replyIndex = index + 1; replyIndex < end; replyIndex++) {
      const reply = rows[replyIndex]!
      const message = lastMessageText(reply.parts, reply.text)
      const text = message.trim() ? message : reply.questions?.map(question => question.prompt).join('\n') || ''
      if (text.trim()) preview = text
    }
    const title = humanBubbleText(turn) || turn.images?.map((image, i) => attachmentDisplayName(image, i)).join(', ') || translate('pages.clarify.myChoice')
    return [{ id, title, preview }]
  })
})

function onMessageNavigate() {
  stickToBottom.value = false
}

const embedImagePreview = inject(EMBED_IMAGE_PREVIEW_KEY, null)
const embedDemoPreview = inject(EMBED_DEMO_PREVIEW_KEY, null)

/** Page Harness keeps the preview on the host page. The main app opens a page-level window beside the chat. */
function openImagePreview(images: ClarifyImage[], index: number) {
  if (!embedImagePreview) {
    chat.openImagePreview(images, index)
    return
  }
  const im = images[index]
  if (!im || !isImageAttachment(im)) return
  embedImagePreview.open(imgSrc(im), imagePreviewLabel(images, index))
}

/** Clarify passes its own reply copy; review uses the shared send copy. */
const clarifySendAria = computed(() => props.sendLabel || translate('pages.reviewComposer.send'))
const composerBlocksConfirm = computed(() => composerSending.value || (!props.coldSession && hasComposerDraft()))
const showHotConfirm = computed(() => !props.hideFinish && !composerBlocksConfirm.value)
const confirmActionMotion = useComposerActionMotion()
const confirmPresence = ref<HTMLElement | null>(null)
const abortConfirmButton = ref<HTMLButtonElement | null>(null)
watch([showHotConfirm, composerBlocksConfirm], ([showing, blocked]) => {
  // Keep focus in the composer when either confirmation action disappears.
  const active = document.activeElement
  const leavingConfirm = !showing && confirmPresence.value?.contains(active)
  const leavingAbort = blocked && !!abortConfirmButton.value && active === abortConfirmButton.value
  if (leavingConfirm || leavingAbort) {
    void nextTick(() => textareaRef.value?.focus({ preventScroll: true }))
  }
}, { flush: 'pre' })

/**
 * Where each session's interactive card goes: under the first agent reply
 * after the session's newest request, or on that request when no reply came.
 * Either only shows once the agent stopped working on the session.
 */
const liveCards = computed(() => {
  const full = new Map<number, NonNullable<ClarifyTurn['live']>>()
  const latest = new Map<string, number>()
  const newest = new Map<string, number>()
  if (!liveHost) return { full, latest, newest }
  const turns = displayTurns.value
  turns.forEach((t, i) => {
    if (t.role === 'human' && t.live) latest.set(t.live.sid, i)
  })
  for (const [sid, i] of latest) newest.set(sid, i)
  for (const [sid, i] of latest) {
    const live = turns[i].live!
    const j = turns.findIndex((t, k) => k > i && t.role === 'agent')
    if (j >= 0 && live.op !== 'accept' && live.op !== 'discard' && !full.has(j)) {
      full.set(j, live)
      latest.set(sid, -1)
    }
  }
  return { full, latest, newest }
})
const liveSettled = (sid: string) => {
  const state = liveHost?.store.sessions[sid]?.state
  return !!state && !isLiveBusy(state)
}
type PreviewFocus = {
  question: ReactQuestion
  optionId: string
  readonly: boolean
  turnIndex: number
}

/** One enlarged frame. Opening or closing it does not change the draft selection. */
const previewFocus = ref<PreviewFocus | null>(null)
const chatRoot = ref<HTMLElement | null>(null)
const demoWindowEl = ref<HTMLElement | null>(null)
const demoBox = ref<DemoWindowBox | null>(null)
/** True after the title bar or an edge has actually moved this open. */
const demoUserAdjusted = ref(false)
let demoPointerCleanup: (() => void) | null = null
const demoResizeEdges: DemoWindowEdge[] = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']
const demoWindowTitleId = `clarify-demo-window-title-${useId().replace(/:/g, '')}`

function hasDemoHtml(o: { demoHtml?: string } | undefined): boolean {
  return !!o?.demoHtml?.trim()
}

function historyPreviewOption(q: ReactQuestion, turnIndex: number) {
  return selectedDemoOption(q, selectedLabelsForQuestion(q, choiceRowsForAgentTurn(turnIndex)))
}

const focusedOption = computed(() => {
  const focus = previewFocus.value
  if (!focus) return null
  const option = focus.question.options.find((o) => o.id === focus.optionId)
  return hasDemoHtml(option) ? option! : null
})

const focusedHighlighted = computed(() => {
  const focus = previewFocus.value
  const option = focusedOption.value
  if (!focus || !option) return false
  if (focus.readonly) {
    return selectedLabelsForQuestion(focus.question, choiceRowsForAgentTurn(focus.turnIndex)).includes(option.label)
  }
  return isSelected(focus.question.id, option.id)
})

const focusedTitle = computed(() => {
  const base = translate('common.htmlPreview.enlargeTitle')
  const label = focusedOption.value?.label?.trim()
  return label ? `${base} · ${label}` : base
})

/** Main-app window only. CoCo draws its own host window and must not get a second layer here. */
const showDemoWindow = computed(() => !!focusedOption.value && !embedDemoPreview)

const windowDemoHtml = computed(() => withDemoEscapeBridge(focusedOption.value?.demoHtml || ''))

const demoWindowStyle = computed(() => {
  const box = demoBox.value
  if (!box) return undefined
  return {
    left: `${box.x}px`,
    top: `${box.y}px`,
    width: `${box.w}px`,
    height: `${box.h}px`,
  }
})

function viewportSize() {
  return {
    w: window.innerWidth || document.documentElement?.clientWidth || 0,
    h: window.innerHeight || document.documentElement?.clientHeight || 0,
  }
}

function layoutDemoWindow() {
  const viewport = viewportSize()
  if (demoUserAdjusted.value && demoBox.value) {
    demoBox.value = clampDemoWindow(demoBox.value, viewport)
    return
  }
  demoBox.value = placeClarifyDemoWindow(viewport)
}

function stopDemoPointer() {
  demoPointerCleanup?.()
  demoPointerCleanup = null
}

function trackDemoPointer(pointerId: number, onMove: (event: PointerEvent) => void) {
  stopDemoPointer()
  const move = (event: PointerEvent) => {
    if (event.pointerId !== pointerId) return
    onMove(event)
  }
  const up = (event: PointerEvent) => {
    if (event.pointerId !== pointerId) return
    stopDemoPointer()
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
  window.addEventListener('pointercancel', up)
  demoPointerCleanup = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
    window.removeEventListener('pointercancel', up)
  }
}

function capturePointer(event: PointerEvent) {
  const host = event.currentTarget
  if (!(host instanceof HTMLElement) || typeof host.setPointerCapture !== 'function') return
  try {
    host.setPointerCapture(event.pointerId)
  } catch {
    /* Detached nodes and some test DOMs reject capture. */
  }
}

function onTitlePointerDown(event: PointerEvent) {
  if (event.button !== 0) return
  const target = event.target
  if (target instanceof Element && target.closest('[data-testid="clarify-demo-window-close"]')) return
  const origin = demoBox.value
  if (!origin) return
  event.preventDefault()
  capturePointer(event)
  const startX = event.clientX
  const startY = event.clientY
  trackDemoPointer(event.pointerId, (ev) => {
    const dx = ev.clientX - startX
    const dy = ev.clientY - startY
    if (dx === 0 && dy === 0) return
    demoUserAdjusted.value = true
    demoBox.value = moveDemoWindow(origin, dx, dy, viewportSize())
  })
}

function onResizePointerDown(edge: DemoWindowEdge, event: PointerEvent) {
  if (event.button !== 0) return
  const origin = demoBox.value
  if (!origin) return
  event.preventDefault()
  event.stopPropagation()
  capturePointer(event)
  const startX = event.clientX
  const startY = event.clientY
  trackDemoPointer(event.pointerId, (ev) => {
    const dx = ev.clientX - startX
    const dy = ev.clientY - startY
    if (dx === 0 && dy === 0) return
    demoUserAdjusted.value = true
    demoBox.value = resizeDemoWindow(origin, edge, dx, dy, viewportSize())
  })
}

function resizeHandleClass(edge: DemoWindowEdge): string {
  const base = 'absolute z-20 touch-none'
  switch (edge) {
    case 'n':
      return `${base} inset-x-3 top-0 h-2 cursor-ns-resize`
    case 's':
      return `${base} inset-x-3 bottom-0 h-2 cursor-ns-resize`
    case 'e':
      return `${base} inset-y-3 right-0 w-2 cursor-ew-resize`
    case 'w':
      return `${base} inset-y-3 left-0 w-2 cursor-ew-resize`
    case 'nw':
      return `${base} left-0 top-0 h-3 w-3 cursor-nwse-resize`
    case 'ne':
      return `${base} right-0 top-0 h-3 w-3 cursor-nesw-resize`
    case 'sw':
      return `${base} bottom-0 left-0 h-3 w-3 cursor-nesw-resize`
    case 'se':
      return `${base} bottom-0 right-0 h-3 w-3 cursor-nwse-resize`
  }
}

function onDemoKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' && event.key !== 'Esc') return
  event.preventDefault()
  closePreviewFocus()
}

function onDemoEscapeMessage(event: MessageEvent) {
  if (!isClarifyDemoEscapeMessage(event.data)) return
  const frame = demoWindowEl.value?.querySelector('iframe')
  if (frame && event.source !== frame.contentWindow) return
  closePreviewFocus()
}

function onDemoWindowResize() {
  if (showDemoWindow.value) layoutDemoWindow()
}

function attachDemoWindowListeners() {
  window.addEventListener('keydown', onDemoKeydown)
  window.addEventListener('message', onDemoEscapeMessage)
  window.addEventListener('resize', onDemoWindowResize)
}

function detachDemoWindowListeners() {
  window.removeEventListener('keydown', onDemoKeydown)
  window.removeEventListener('message', onDemoEscapeMessage)
  window.removeEventListener('resize', onDemoWindowResize)
}

/** Open this one frame. A later open replaces the content and keeps a user-moved box. */
function openPreviewFocus(q: ReactQuestion, optionId: string, readonly: boolean, turnIndex: number) {
  const option = q.options.find((o) => o.id === optionId)
  if (!hasDemoHtml(option)) return
  const alreadyOpen = showDemoWindow.value
  previewFocus.value = { question: q, optionId, readonly, turnIndex }
  if (embedDemoPreview) {
    embedDemoPreview.open(option!.demoHtml || '', option!.label)
    return
  }
  if (!alreadyOpen) {
    demoUserAdjusted.value = false
    layoutDemoWindow()
  }
}

function closePreviewFocus() {
  previewFocus.value = null
  embedDemoPreview?.close()
}

/** Leave the window when the deck moves, the round is submitted, or the live card goes away. */
watch(
  () => `${activeQuestions.value.map((q) => q.id).join('|')}@${step.value}@${thinking.value ? 1 : 0}`,
  () => {
    if (previewFocus.value) closePreviewFocus()
  },
)

watch(showDemoWindow, (open) => {
  if (!open) {
    stopDemoPointer()
    detachDemoWindowListeners()
    demoBox.value = null
    demoUserAdjusted.value = false
    return
  }
  if (!demoBox.value) layoutDemoWindow()
  attachDemoWindowListeners()
  void nextTick(() => {
    demoWindowEl.value?.querySelector<HTMLElement>('[data-testid="clarify-demo-window-close"]')?.focus()
  })
})

/** The newest request keeps only a loading headline until the agent stops working on it. */
onBeforeUnmount(() => {
  stopDemoPointer()
  detachDemoWindowListeners()
  if (previewFocus.value) closePreviewFocus()
})

function choiceEchoes(index: number) {
  const rows = parseChoiceSummary(displayTurns.value[index]?.text || '')
  if (!rows) return []
  let agentTurnIndex = -1
  const questions: ReactQuestion[] = []
  for (let j = index - 1; j >= 0; j--) {
    const turn = displayTurns.value[j]
    if (turn?.role === 'agent' && turn.questions?.length) {
      agentTurnIndex = j
      questions.push(...turn.questions)
      break
    }
  }
  return choiceEchoRows(rows, questions).map((echo) => ({ ...echo, agentTurnIndex }))
}

function turnColumnClass(t: ClarifyTurn, index: number): string {
  if (t.role !== 'human') return 'w-full'
  return choiceEchoes(index).some((echo) => echo.cards.length > 0) ? 'w-[88%] max-w-[88%]' : 'max-w-[88%]'
}

function enlargeChoicePreview(question: ReactQuestion | null, optionId: string, agentTurnIndex: number) {
  if (!question || agentTurnIndex < 0) return
  openPreviewFocus(question, optionId, true, agentTurnIndex)
}

function humanLiveCardLoading(live: NonNullable<ClarifyTurn['live']>, i: number) {
  if (liveCards.value.newest.get(live.sid) !== i || live.op === 'accept' || live.op === 'discard') return false
  return isLiveBusy(liveHost?.store.sessions[live.sid]?.state)
}
</script>

<template>
  <!--
    Multi-root (image preview sibling): parent class fallthrough is ignored.
    Bake min-h-0 flex-1 so page-control status + this column share shell height
    and shrink-0 confirm footer is not clipped (plan g1.3).
  -->
  <div ref="chatRoot" class="chat-thread flex h-full min-h-0 min-w-0 flex-1 flex-col" data-review-composer>
    <div class="relative flex min-h-0 flex-1 flex-col">
    <ChatMessageRail
      :items="messageRailItems"
      :scroller="scroller || null"
      :scope-key="railScopeKey"
      @navigate="onMessageNavigate"
    />
    <div
      ref="scroller"
      class="scroll-area flex-1 space-y-3 overflow-y-auto p-4 pb-14"
      data-testid="clarify-scroller"
      data-conversation-scroll
      @scroll.passive="onScrollerScroll"
    >
      <div class="clarify-transcript">
      <div
        v-if="showSandboxOrphanBanner"
        class="flex items-start gap-2 rounded-md border border-warn/40 bg-warn/10 px-2.5 py-2 text-[12px] text-txt"
        data-testid="clarify-sandbox-orphan"
        role="status"
      >
        <Icon name="alert" :size="13" class="mt-0.5 shrink-0 text-warn" />
        <p class="min-w-0 flex-1 leading-relaxed">
          {{ translate('pages.clarify.sandboxOrphanBanner', { op: sandboxOrphanOpLabel }) }}
        </p>
        <button
          type="button"
          class="shrink-0 rounded-md border border-line bg-elevated px-2 py-1 text-[11px] font-medium text-txt2 hover:border-line-strong"
          data-testid="clarify-sandbox-orphan-cancel"
          @click="cancelReview"
        >
          {{ translate('pages.clarify.sandboxOrphanCancel') }}
        </button>
      </div>
      <p
        v-if="showClarifyEmptyHint"
        class="text-[12px] leading-relaxed text-txt2"
        data-testid="clarify-approve-empty-hint"
      >
        {{ translate('pages.clarify.clarifyEmptyHint') }}
      </p>
      <!-- Taken-over node: what it was asked to do, as the conversation's first User Message (same user-bubble style). -->
      <div
        v-if="nodeTask?.trim()"
        class="clarify-turn flex justify-end"
        data-turn-role="human"
        data-testid="clarify-node-task"
      >
        <span class="sr-only">{{ translate('pages.clarify.me') }}</span>
        <div class="min-w-0 max-w-[88%]">
          <div class="chat-user-message whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{{ nodeTask.trim() }}</div>
        </div>
      </div>
      <template v-for="(t, i) in displayTurns" :key="messageAnchorState.keys[i]">
      <div
        v-if="!isEmptyLiveTurn(t) && !(observe && isBlankHumanTurn(t))"
        class="clarify-turn flex"
        :class="t.role === 'human' ? 'justify-end' : ''"
        :data-turn-role="t.role"
        :data-chat-message-id="messageAnchorState.anchors[i]"
        :data-chat-anchor="messageAnchorState.anchors[i]"
      >
        <span class="sr-only">{{ translate(t.role === 'human' ? 'pages.clarify.me' : 'pages.llmTranscript.speaker.agent') }}</span>
        <div class="min-w-0" :class="turnColumnClass(t, i)">
          <div v-if="t.images && t.images.length" class="mb-1.5 flex flex-wrap gap-1.5" :class="t.role === 'human' ? 'justify-end' : ''">
            <!-- human history: images → lightbox; non-images → filename chip -->
            <template v-if="t.role === 'human'">
              <template v-for="(im, ii) in t.images" :key="ii">
                <ChatImageThumb
                  v-if="isImageAttachment(im)"
                  mode="previewable"
                  size="md"
                  fit="contain"
                  thumb-class="rounded-md"
                  :src="imgSrc(im)"
                  :label="imagePreviewLabel(t.images, ii)"
                  test-id="clarify-history-image-thumb"
                  @preview="openImagePreview(t.images!, ii)"
                />
                <div
                  v-else
                  class="chat-file-chip rounded-md flex max-w-[200px] items-center gap-2 border border-line bg-elevated px-2 py-1.5"
                  data-testid="clarify-history-file-chip"
                  :title="imagePreviewLabel(t.images, ii)"
                >
                  <Icon name="artifact" :size="16" class="shrink-0 text-txt2" aria-hidden="true" />
                  <span class="min-w-0 truncate text-[12px] text-txt">{{ imagePreviewLabel(t.images, ii) }}</span>
                </div>
              </template>
            </template>
            <!-- agent history: locked thumbs / filename chips (FR-f7) -->
            <template v-else>
              <template v-for="(im, ii) in t.images" :key="ii">
                <ChatImageThumb
                  v-if="isImageAttachment(im)"
                  mode="locked"
                  size="md"
                  thumb-class="rounded-md"
                  :src="imgSrc(im)"
                  :label="imagePreviewLabel(t.images, ii)"
                  test-id="clarify-agent-image-thumb"
                />
                <div
                  v-else
                  class="chat-file-chip rounded-md flex max-w-[200px] items-center gap-2 border border-line bg-elevated px-2 py-1.5"
                  data-testid="clarify-agent-file-chip"
                >
                  <Icon name="artifact" :size="16" class="shrink-0 text-txt2" aria-hidden="true" />
                  <span class="min-w-0 truncate text-[12px] text-txt">{{ imagePreviewLabel(t.images, ii) }}</span>
                </div>
              </template>
            </template>
          </div>
          <!-- Live variant request card (state from the preview drawer, if any) -->
          <LiveVariantCard
            v-if="liveHost && t.role === 'human' && t.live"
            :live-ref="t.live"
            :compact="liveCards.latest.get(t.live.sid) !== i || humanLiveCardLoading(t.live, i)"
            :loading="humanLiveCardLoading(t.live, i)"
          />
          <!-- annotation chips attached to this human review turn -->
          <div v-if="t.role === 'human' && t.annotations && t.annotations.length" class="mb-1.5 flex flex-wrap gap-1.5 justify-end">
            <AnnotationChip
              v-for="(a, ai) in t.annotations"
              :key="ai"
              :ann="a"
            />
          </div>
          <!-- Submitted answers share the same quiet surface as the user's messages. -->
          <div
            v-if="t.role === 'human' && parseChoiceSummary(t.text)"
            class="chat-user-message chat-response-summary min-w-0 max-w-full"
            data-testid="clarify-choice-summary"
          >
            <div class="chat-response-heading flex items-center gap-2 text-txt2">
              <Icon name="check" :size="16" /> {{ translate('pages.clarify.myChoice') }}
            </div>
            <div class="min-w-0 max-w-full">
              <div
                v-for="(echo, ri) in choiceEchoes(i)"
                :key="ri"
                class="chat-response-row min-w-0 max-w-full"
                data-testid="clarify-choice-row"
              >
                <div
                  v-if="echo.showPrompt"
                  class="chat-response-question mb-1 min-w-0 max-w-full break-words text-txt2 [overflow-wrap:anywhere]"
                >{{ echo.prompt }}</div>
                <div
                  v-if="echo.cards.length"
                  class="flex min-w-0 flex-col gap-2.5"
                  data-testid="clarify-choice-demos"
                >
                  <ClarifyDemoFrame
                    v-for="o in echo.cards"
                    :key="o.id"
                    :label="o.label"
                    :html="o.demoHtml!"
                    highlighted
                    selected
                    @enlarge="enlargeChoicePreview(echo.question, o.id, echo.agentTurnIndex)"
                  />
                </div>
                <div
                  v-if="echo.texts.length"
                  class="chat-response-answers flex min-w-0 max-w-full flex-wrap gap-x-2 gap-y-1"
                  :class="echo.cards.length ? 'mt-2' : ''"
                >
                  <span
                    v-for="(a, ai) in echo.texts"
                    :key="ai"
                    class="min-w-0 max-w-full break-words text-txt [overflow-wrap:anywhere]"
                    data-testid="clarify-choice-answer"
                  >{{ a }}</span>
                </div>
              </div>
            </div>
          </div>
          <!-- Submitted form values remain readable in full, including passwords. -->
          <div
            v-else-if="t.role === 'human' && parseFormSummary(t.text)"
            class="chat-user-message chat-response-summary min-w-0 max-w-full"
            data-testid="clarify-form-summary"
          >
            <div class="chat-response-heading flex items-center gap-2 text-txt2">
              <Icon name="ci" :size="16" /> {{ translate('pages.clarify.myForm') }}
            </div>
            <div class="min-w-0 max-w-full">
              <div
                v-for="(row, ri) in parseFormSummary(t.text)!"
                :key="ri"
                class="chat-response-row min-w-0 max-w-full"
                data-testid="clarify-form-row"
              >
                <div class="chat-response-question mb-1 min-w-0 max-w-full break-words text-txt2 [overflow-wrap:anywhere]">{{ row.label }}</div>
                <div
                  class="chat-response-value min-w-0 max-w-full break-words text-txt [overflow-wrap:anywhere] whitespace-pre-wrap"
                  data-testid="clarify-form-value"
                >{{ row.value }}</div>
              </div>
            </div>
          </div>
          <!-- Agent busy / thought / message (Demo: four-phase + restrained done) -->
          <template v-else-if="t.role === 'agent'">
            <!-- Waiting first token: pulse + 思考中… inside bubble -->
            <div
              v-if="t.streaming && !t.thought && !agentHasMessage(t)"
              class="chat-meta inline-flex items-center gap-2.5 py-2"
              data-testid="clarify-busy-placeholder"
            >
              <InboxMotionMark kind="pulse" />
              <span>{{ translate('pages.clarify.thinkingBusy') }}</span>
            </div>
            <!-- Status: 思考中… (has thought, no message yet) -->
            <div
              v-else-if="t.streaming && t.thought && !agentHasMessage(t)"
              class="chat-meta mb-1.5 inline-flex items-center gap-2 font-normal"
              data-testid="clarify-busy-status"
            >
              <InboxMotionMark kind="pulse" />
              <span>{{ translate('pages.clarify.thinkingBusy') }}</span>
            </div>
            <!-- Status: 输出中 (shimmer) while streaming message -->
            <div
              v-else-if="t.streaming && agentHasMessage(t)"
              class="clarify-outputting chat-meta mb-1.5 font-normal"
              data-testid="clarify-busy-status"
            >
              {{ translate('pages.clarify.outputting') }}
            </div>
            <!-- Steps in arrival order (thought / tools / message), like the persisted timeline -->
            <AgentTimeline
              v-if="t.parts?.length && !isRetryableFailedAgent(t)"
              :parts="t.parts"
              :streaming="!!t.streaming"
              :completed="showTurnCompleted(t)"
              :interrupted="!!t.interrupted"
              :run-id="runId"
              :started-at="outputBounds(i).startedAt"
              :ended-at="outputBounds(i).endedAt"
              :quiet-thought="observeQuietBusy(t)"
              message-test-id="clarify-agent-message"
              @reveal="scrollBottom()"
            >
              <template #caret>
                <span class="clarify-stream-caret" data-testid="clarify-stream-caret" aria-hidden="true" />
              </template>
            </AgentTimeline>
            <template v-else>
              <!-- Thought: open while thought-only; default collapsed once message starts -->
              <details
                v-if="t.thought"
                class="chat-thought chat-meta mb-2 w-full"
                data-testid="clarify-thought"
                :open="isThoughtOpen(i, t)"
                @toggle="onThoughtToggle(i, $event)"
              >
                <summary
                  class="chat-thought-summary"
                  data-testid="clarify-thought-summary"
                >
                  <ThoughtSummaryStatus
                    :busy="!!t.streaming && !observeQuietBusy(t)"
                    :completed="showTurnCompleted(t)"
                    :interrupted="!!t.interrupted"
                  />
                  <Icon name="chevron-down" :size="16" class="chat-thought-chevron" aria-hidden="true" />
                </summary>
                <div class="chat-thought-body whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{{ agentThoughtDisplay(t, i) }}</div>
              </details>
              <AgentToolGroup v-if="t.tools?.length" :tools="t.tools" :busy="!!t.streaming" :run-id="runId" />
              <!-- Message body + streaming caret -->
              <div
                v-if="agentHasMessage(t) && !isRetryableFailedAgent(t)"
                class="chat-prose md py-2"
                data-testid="clarify-agent-message"
              >
                <StreamMarkdown v-if="t.streaming" :blocks="liveStreamBlocks" /><span
                  v-else
                  v-html="renderChatMarkdown(t.text)"
                /><span
                  v-if="t.streaming"
                  class="clarify-stream-caret"
                  data-testid="clarify-stream-caret"
                  aria-hidden="true"
                />
              </div>
              <!-- Empty / failure card + cover-retry (plan g1.1 / g1.2) -->
              <div
                v-else-if="isRetryableFailedAgent(t)"
                class="rounded-lg flex max-w-full flex-col gap-2 border border-err/35 bg-err/10 px-3 py-2.5"
                role="alert"
                data-testid="clarify-empty-fail"
              >
                <div class="flex items-start gap-2">
                  <Icon name="alert" :size="16" class="mt-0.5 shrink-0 text-err" />
                  <div class="min-w-0">
                    <div class="text-[13px] font-semibold text-err">
                      {{ translate('pages.clarify.emptyFailTitle') }}
                    </div>
                    <div
                      class="mt-0.5 break-words text-xs text-txt2 [overflow-wrap:anywhere]"
                      data-testid="clarify-empty-fail-desc"
                    >
                      {{
                        emptyFailDisplayText(
                          t,
                          translate('pages.clarify.emptyFailDesc'),
                        )
                      }}
                    </div>
                  </div>
                </div>
                <div v-if="showFailRetry(t, i)">
                  <button
                    type="button"
                    class="rounded-md border border-err/40 bg-transparent px-2.5 py-1 text-xs text-err hover:bg-err/15 disabled:opacity-50"
                    :disabled="failRetryDisabled"
                    data-testid="clarify-empty-fail-retry"
                    @click="retryLastFailed"
                  >
                    {{ translate('pages.clarify.retry') }}
                  </button>
                </div>
              </div>
            </template>
            <LiveVariantCard
              v-if="liveCards.full.has(i) && !t.streaming && liveSettled(liveCards.full.get(i)!.sid)"
              :live-ref="liveCards.full.get(i)!"
              agent
            />
          </template>
          <!-- Human free-text bubble: plain text so tags stay visible and never execute. -->
          <div
            v-else-if="humanBubbleText(t)"
            class="chat-user-message whitespace-pre-wrap break-words [overflow-wrap:anywhere]"
            data-testid="clarify-human-message"
          >{{ humanBubbleText(t) }}</div>

          <!-- Structured choice questions (ask_question). The latest agent turn
               shows an interactive card deck (one question per card); earlier
               turns render read-only for context. -->
          <template v-if="t.role === 'agent' && t.questions && t.questions.length">
            <!-- interactive card deck -->
            <div v-if="isActiveTurn(i) && curQuestion" class="mt-2">
              <div>
                <div class="clarify-request relative min-w-0">
                  <!-- progress -->
                  <div v-if="activeQuestions.length > 1" class="mb-2.5 flex items-center justify-between">
                    <div class="flex items-center gap-1">
                      <span
                        v-for="(q, qi) in activeQuestions"
                        :key="qi"
                        class="h-1.5 rounded-full transition-all"
                        :class="qi === step ? 'w-4 bg-accent' : answered(q) ? 'w-1.5 bg-accent/40' : 'w-1.5 bg-line-strong'"
                      />
                    </div>
                    <span class="text-[11px] text-txt2">{{ step + 1 }} / {{ activeQuestions.length }}</span>
                  </div>

                  <Transition name="deck" mode="out-in">
                    <div :key="step">
                      <div class="clarify-question-heading mb-3 text-[13px] font-medium text-txt">
                        <Icon name="chat" :size="16" class="shrink-0 text-txt2" />
                        <span
                          class="min-w-0 break-words leading-relaxed [overflow-wrap:anywhere]"
                          data-testid="clarify-question-prompt"
                        >{{ curQuestion.prompt }}</span>
                        <span class="clarify-question-kind text-[11px] font-normal leading-5 text-txt2">{{ curQuestion.allowMultiple ? translate('pages.clarify.multiple') : translate('pages.clarify.single') }}</span>
                      </div>
                      <div class="space-y-1.5">
                        <div v-for="o in curQuestion.options" :key="o.id" class="flex w-full items-stretch gap-1.5">
                          <button
                            type="button"
                            class="chat-choice-option flex min-w-0 flex-1 items-center gap-2 rounded-lg border border-line bg-surface px-2.5 py-2 text-left text-[12px] text-txt2 transition-colors"
                            :aria-pressed="isSelected(curQuestion.id, o.id)"
                            data-testid="clarify-option-btn"
                            @click="pick(curQuestion, o.id)"
                          >
                            <span
                              class="rounded-lg flex h-4 w-4 shrink-0 items-center justify-center border"
                              :class="[
                                curQuestion.allowMultiple ? 'rounded' : 'rounded-full',
                                isSelected(curQuestion.id, o.id) ? 'border-accent bg-accent text-white' : 'border-line-strong',
                              ]"
                            >
                              <Icon v-if="isSelected(curQuestion.id, o.id)" name="check" :size="10" />
                            </span>
                            <span
                              class="min-w-0 flex-1 break-words [overflow-wrap:anywhere]"
                              data-testid="clarify-option-label"
                            >{{ o.label }}</span>
                            <span
                              v-if="o.recommended"
                              class="chat-choice-recommendation ml-auto shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium"
                            >{{ translate('pages.clarify.recommended') }}</span>
                          </button>
                          <button
                            v-if="hasDemoHtml(o)"
                            type="button"
                            class="inline-flex shrink-0 items-center self-center rounded border border-line bg-surface px-1.5 py-0.5 text-[10px] text-txt2 transition-colors hover:text-txt"
                            data-testid="clarify-option-enlarge"
                            :aria-label="translate('pages.clarify.demoEnlarge')"
                            :title="translate('pages.clarify.demoEnlarge')"
                            @click="openPreviewFocus(curQuestion, o.id, false, i)"
                          >
                            <Icon name="expand" :size="11" aria-hidden="true" />
                          </button>
                        </div>
                      </div>
                      <div
                        class="chat-choice-other mt-1.5 flex w-full items-center gap-2 rounded-lg border border-line bg-surface px-2.5 py-2 text-left text-[12px] text-txt2 transition-colors"
                        :class="{ 'is-selected': isOtherSelected(curQuestion.id) }"
                        data-testid="clarify-other-row"
                      >
                        <button
                          type="button"
                          class="rounded-lg flex h-4 w-4 shrink-0 items-center justify-center border"
                          :class="[
                            curQuestion.allowMultiple ? 'rounded' : 'rounded-full',
                            isOtherSelected(curQuestion.id) ? 'border-accent bg-accent text-white' : 'border-line-strong',
                          ]"
                          data-testid="clarify-other-checkbox"
                          :aria-pressed="isOtherSelected(curQuestion.id)"
                          :aria-label="translate('pages.clarify.otherPlaceholder')"
                          @click="toggleOther(curQuestion)"
                        >
                          <Icon v-if="isOtherSelected(curQuestion.id)" name="check" :size="10" />
                        </button>
                        <input
                          v-model="other[curQuestion.id]"
                          type="text"
                          class="input min-h-0 min-w-0 flex-1 border-0 bg-transparent p-0 text-[12px] shadow-none focus:border-transparent focus:ring-0 [overflow-wrap:anywhere]"
                          :placeholder="translate('pages.clarify.otherPlaceholder')"
                          data-testid="clarify-other-input"
                        />
                      </div>
                      <div
                        v-if="selectedDemoForInteractive(curQuestion)"
                        class="mt-2.5"
                        data-testid="clarify-inline-demos"
                      >
                        <ClarifyDemoFrame
                          :label="selectedDemoForInteractive(curQuestion)!.label"
                          :html="selectedDemoForInteractive(curQuestion)!.demoHtml!"
                          highlighted
                          @enlarge="openPreviewFocus(curQuestion, selectedDemoForInteractive(curQuestion)!.id, false, i)"
                        />
                      </div>
                    </div>
                  </Transition>

                  <!-- actions -->
                  <div class="mt-3 flex items-center justify-between">
                    <button
                      v-if="!isFirstCard"
                      class="chat-request-back inline-flex items-center gap-0.5 rounded-md px-2 py-1 text-[12px] text-txt2 hover:text-txt"
                      @click="prevCard"
                    >
                      <Icon name="arrow-left" :size="13" /> {{ translate('pages.clarify.prev') }}
                    </button>
                    <span v-else />

                    <button
                      v-if="!isLastCard"
                      class="chat-action-secondary"
                      @click="nextCard"
                    >
                      {{ translate('pages.clarify.next') }} <Icon name="chevron-right" :size="13" />
                    </button>
                    <div v-else class="flex flex-wrap items-center gap-2">
                      <button
                        v-if="hasRecommended"
                        class="chat-action-secondary"
                        :disabled="thinking"
                        :title="translate('pages.clarify.applyRecommendedTitle')"
                        @click="submitRecommended"
                      >
                        <Icon name="check" :size="14" /> {{ translate('pages.clarify.applyRecommended') }}
                      </button>
                      <button
                        class="chat-action-confirm"
                        :disabled="!someAnswered || thinking"
                        @click="submitChoices"
                      >
                        <Icon name="chat-send" :size="16" /> {{ translate('pages.clarify.submitChoices') }}
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <!-- read-only history: prompt, chosen labels, inline previews, and per-frame enlarge -->
            <div v-else class="clarify-request clarify-request--history mt-2 min-w-0">
              <div class="mb-2 flex items-center gap-2 text-[12px] font-medium text-txt2">
                <Icon name="chat" :size="16" />
                {{ translate('pages.clarify.questionsThisRound', { n: t.questions.length }) }}
                <span class="ml-auto text-[11px] font-normal text-txt2">
                  {{ translate('pages.clarify.readonly') }}
                </span>
              </div>
              <div
                v-for="(q, qi) in t.questions"
                :key="qi"
                class="mb-3 last:mb-0"
              >
                <div class="mb-1.5 min-w-0 max-w-full text-[12px] leading-snug text-txt2 [overflow-wrap:anywhere]">
                  <span class="text-txt3">{{ qi + 1 }}.</span> {{ q.prompt }}
                  <span
                    v-for="o in q.options.filter((op) => op.recommended)"
                    :key="o.id"
                    class="chat-choice-recommendation ml-1 inline-flex max-w-full min-w-0 break-words rounded-full px-1.5 py-0.5 text-[10px] font-medium [overflow-wrap:anywhere]"
                    data-testid="clarify-recommended-chip"
                  >{{ translate('pages.clarify.recommendedLabel', { label: o.label }) }}</span>
                </div>
                <div v-if="demoOptionsOf(q).length" class="mt-1.5 space-y-1">
                  <div
                    v-for="o in demoOptionsOf(q)"
                    :key="o.id"
                    class="flex items-center gap-2 text-[12px] text-txt2"
                    data-testid="clarify-history-option"
                  >
                    <span class="min-w-0 flex-1 break-words [overflow-wrap:anywhere]">{{ o.label }}</span>
                    <span
                      v-if="selectedLabelsForQuestion(q, choiceRowsForAgentTurn(i)).includes(o.label)"
                      class="shrink-0 text-accent"
                    >✓ {{ translate('pages.clarify.demoSelected') }}</span>
                    <button
                      type="button"
                      class="inline-flex shrink-0 items-center rounded border border-line bg-surface px-1.5 py-0.5 text-[10px] text-txt2 transition-colors hover:text-txt"
                      data-testid="clarify-option-enlarge"
                      :aria-label="translate('pages.clarify.demoEnlarge')"
                      :title="translate('pages.clarify.demoEnlarge')"
                      @click="openPreviewFocus(q, o.id, true, i)"
                    >
                      <Icon name="expand" :size="11" aria-hidden="true" />
                    </button>
                  </div>
                </div>
                <div
                  v-if="historyPreviewOption(q, i)"
                  class="mt-2.5"
                  data-testid="clarify-inline-demos"
                >
                  <ClarifyDemoFrame
                    :label="historyPreviewOption(q, i)!.label"
                    :html="historyPreviewOption(q, i)!.demoHtml!"
                    highlighted
                    selected
                    @enlarge="openPreviewFocus(q, historyPreviewOption(q, i)!.id, true, i)"
                  />
                </div>
              </div>
            </div>
          </template>

          <!-- Structured forms (ask_form / preflight). Latest unanswered turn is interactive;
               earlier turns render read-only. Label only (no internal name); hover shows why. -->
          <template v-if="t.role === 'agent' && t.forms && t.forms.length">
            <div v-if="isActiveFormTurn(i)" class="mt-2" data-testid="clarify-form-card">
              <div class="clarify-request relative min-w-0">
                <div
                  v-for="(form, fi) in activeForms"
                  :key="fi"
                  class="mb-3 last:mb-0"
                >
                  <div v-if="form.title" class="mb-2 flex min-w-0 items-center gap-2 text-[13px] font-medium text-txt">
                    <Icon name="ci" :size="16" class="shrink-0 text-txt2" />
                    <span class="min-w-0 break-words [overflow-wrap:anywhere]">{{ form.title }}</span>
                  </div>
                  <div class="space-y-2.5">
                    <label
                      v-for="f in form.fields"
                      :key="f.name"
                      class="block min-w-0"
                      data-testid="clarify-form-field"
                    >
                      <span
                        class="mb-1 block break-words text-[12px] text-txt2 [overflow-wrap:anywhere]"
                        :title="fieldWhy(f) || undefined"
                      >{{ f.label }}</span>
                      <input
                        v-model="formValues[formFieldKey(fi, f.name)]"
                        :type="formInputType(f)"
                        class="input h-[34px] min-w-0 w-full rounded-lg border border-line bg-surface px-2.5 text-[13px] text-txt placeholder:text-txt3 focus:border-accent/50 focus:ring-0"
                        :placeholder="f.placeholder || ''"
                        autocomplete="off"
                        data-testid="clarify-form-input"
                      />
                    </label>
                  </div>
                </div>
                <div v-if="formNotice" class="mt-2 text-[11px] text-warn" data-testid="clarify-form-notice">
                  {{ formNotice }}
                </div>
                <div class="mt-3 flex justify-end">
                  <button
                    type="button"
                    class="chat-action-confirm"
                    :disabled="!formCanSubmit"
                    data-testid="clarify-form-submit"
                    @click="submitForms"
                  >
                    <Icon name="chat-send" :size="16" /> {{ translate('pages.clarify.submitForm') }}
                  </button>
                </div>
              </div>
            </div>
            <div
              v-else
              class="clarify-request clarify-request--history mt-2 min-w-0"
              data-testid="clarify-form-readonly"
            >
              <div class="mb-2 flex items-center gap-2 text-[12px] font-medium text-txt2">
                <Icon name="ci" :size="16" />
                {{ translate('pages.clarify.formsThisRound', { n: t.forms.length }) }}
                <span class="ml-auto text-[11px] font-normal text-txt2">
                  {{ translate('pages.clarify.readonly') }}
                </span>
              </div>
              <div v-for="(form, fi) in t.forms" :key="fi" class="mb-2 last:mb-0">
                <div v-if="form.title" class="mb-1 break-words text-[13px] text-txt2 [overflow-wrap:anywhere]">{{ form.title }}</div>
                <div
                  v-for="f in form.fields"
                  :key="f.name"
                  class="mb-1 break-words text-[12px] text-txt2 [overflow-wrap:anywhere]"
                  :title="fieldWhy(f) || undefined"
                >
                  {{ f.label }}
                </div>
              </div>
            </div>
          </template>

          <div
            v-if="t.interrupted"
            class="mt-1 inline-flex items-center gap-1 rounded border border-warn/40 bg-warn/10 px-1.5 py-0.5 text-[10px] text-warn"
            data-testid="clarify-interrupted"
          >
            interrupted
          </div>
          <!-- Keep footer time; hide bottom time when completion footnote is shown (keep_footer_hide_bottom) -->
          <div
            v-if="!showTurnCompleted(t) && !observeQuietBusy(t)"
            class="chat-meta mt-1"
            :class="t.role === 'human' ? 'text-right' : ''"
            data-testid="clarify-turn-bottom-time"
          >{{ locale && relTime(t.at) }}</div>
        </div>
      </div>
      </template>
      <div v-if="thinking && !validating && liveAgentIdx < 0" class="chat-meta flex items-center gap-2 pl-9">
        <InboxMotionMark kind="pulse" />
        {{ translate('pages.clarify.thinking') }}
      </div>
      </div>
    </div>
    <button
      v-if="showUnreadFab"
      type="button"
      data-testid="clarify-unread-fab"
      class="absolute bottom-3 left-1/2 z-10 inline-flex h-[34px] -translate-x-1/2 items-center gap-1.5 rounded-full border border-line bg-surface px-3.5 pl-3 text-[12px] font-medium text-accent transition-colors hover:border-accent/40 hover:bg-accent-dim"
      :aria-label="unreadFabLabel"
      :title="unreadFabLabel"
      @click="onUnreadFabClick"
    >
      <Icon name="chevrons-down" :size="16" class="shrink-0" />
      <span class="min-w-[0.7em] text-center tabular-nums">{{ unreadCount }}</span>
    </button>
    </div>

    <slot v-if="done || !active || coldSession" name="composer-controls" :compact="false" />

    <div v-if="showDoneChrome" class="border-t border-line p-3 text-center text-[12px] text-ok">
      <Icon name="check" :size="13" class="-mt-0.5 mr-1 inline" />{{ translate('pages.clarify.done') }}
    </div>
    <!-- Success ceremony in flight after done: keep composer mounted-off until hold→done (g2.3). -->
    <div
      v-else-if="done"
      class="border-t border-line p-3 text-center text-[12px] text-txt3"
      data-testid="clarify-confirm-ceremony"
      aria-live="polite"
    />
    <div v-else-if="!active && !coldSession" class="border-t border-line p-3 text-center text-[12px] text-txt3">
      <div v-if="sendNotice" class="rounded-md mb-2 border border-err/40 bg-err/10 px-2.5 py-1.5 text-left text-[12px] text-err" data-testid="clarify-send-notice" role="alert">
        {{ sendNotice }}
      </div>
      <div v-if="stashNotice" class="rounded-md mb-2 border border-err/40 bg-err/10 px-2.5 py-1.5 text-left text-[12px] text-err" data-testid="clarify-stash-notice" role="alert">
        {{ stashNotice }}
      </div>
      <Icon name="close" :size="13" class="-mt-0.5 mr-1 inline" />{{ translate('pages.clarify.closed') }}
    </div>
    <div v-else-if="coldSession" class="shrink-0 border-t border-line p-3" data-testid="clarify-cold-actions">
      <ComposerShell :show-chrome="false" :show-footer="true">
        <template #footer>
          <button
            type="button"
            class="chat-action-confirm h-9 shrink-0 inline-flex"
            data-testid="clarify-confirm-flow"
            :disabled="confirmDisabled"
            :title="translate('pages.clarify.confirmFlowTitle')"
            @click="finishEarly"
          >
            <Icon name="check" :size="13" />
            {{ validating ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlow') }}
          </button>
        </template>
      </ComposerShell>
    </div>
    <!-- Hot path: shrink-0 so confirm-flow stays inside fixed-height sidebar (plan g1.1). -->
    <div v-else class="shrink-0 border-t border-line p-3" data-testid="clarify-hot-actions">
      <!-- pending-send queue panel (Demo / AgentChatTester): clarify + review -->
      <PendingSendQueuePanel
        :items="queued"
        :notice="queueNotice"
        :toast="queueToast"
        @cancel="cancelQueuedItem"
        @edit="editQueuedItem"
        @reorder="reorderQueuedItems"
      />
      <div v-if="showAnnotationChips && annotations.length" class="mb-2 flex flex-wrap gap-1.5">
        <AnnotationChip
          v-for="(a, ai) in annotations"
          :key="ai"
          :ann="a"
          removable
          test-id="clarify-annotation-chip"
          @remove="removeAnnotation(ai)"
        />
      </div>
      <div v-if="sendNotice" class="rounded-md mb-2 border border-err/40 bg-err/10 px-2.5 py-1.5 text-[12px] text-err" data-testid="clarify-send-notice" role="alert">
        {{ sendNotice }}
      </div>
      <div v-if="stashNotice" class="rounded-md mb-2 border border-err/40 bg-err/10 px-2.5 py-1.5 text-[12px] text-err" data-testid="clarify-stash-notice" role="alert">
        {{ stashNotice }}
      </div>
      <div v-if="attachNotice" class="rounded-md mb-2 border border-err/40 bg-err/10 px-2.5 py-1.5 text-[12px] text-err" data-testid="clarify-attach-notice" role="alert">
        {{ attachNotice }}
      </div>
      <div v-if="attachments.length" class="mb-2 flex flex-wrap gap-1.5">
        <div v-for="(im, ii) in attachments" :key="ii" class="relative">
          <ChatImageThumb
            v-if="isImageAttachment(im)"
            mode="previewable"
            size="sm"
            thumb-class="rounded-md"
            :src="imgSrc(im)"
            :label="attachmentDisplayName(im, ii)"
            :alt="attachmentDisplayName(im, ii)"
            test-id="clarify-draft-image-thumb"
            @preview="openImagePreview(attachments, ii)"
          />
          <div
            v-else
            class="chat-file-chip rounded-lg flex h-14 max-w-[160px] items-center gap-1.5 border border-line bg-elevated px-2"
            :title="attachmentDisplayName(im, ii)"
            data-testid="clarify-pending-file-chip"
          >
            <Icon name="artifact" :size="16" class="shrink-0 text-txt2" aria-hidden="true" />
            <span class="min-w-0 truncate text-[11px] text-txt2">{{ attachmentDisplayName(im, ii) }}</span>
          </div>
          <button
            class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-err text-white"
            @click.stop="removeAttachment(ii)"
          ><Icon name="close" :size="9" /></button>
        </div>
      </div>
      <ComposerShell
        wrap-toolbar
        :show-footer="!hideFinish && reviewMode && !observe"
        box-test-id="clarify-input-row"
        toolbar-test-id="clarify-action-row"
      >
        <template #input>
          <input ref="fileInput" type="file" multiple class="hidden" @change="onPickFiles" />
          <textarea
            ref="textareaRef"
            v-model="draft"
            class="chat-input composer-hint-wrap min-h-[40px] min-w-0 w-full resize-none border-0 bg-transparent p-0 shadow-none outline-none focus:ring-0"
            :class="overflowScroll ? 'scroll-area max-h-[128px] overflow-y-auto' : 'overflow-y-hidden'"
            rows="1"
            :placeholder="observe ? translate(stopping ? 'pages.clarify.observeStoppingPlaceholder' : 'pages.clarify.observeInputPlaceholder') : inputPlaceholder"
            :disabled="observe"
            data-testid="clarify-input"
            @input="onTextInput"
            @keydown="onComposerKeydown"
            @compositionstart="composing = true"
            @compositionend="composing = false"
            @paste="onPaste"
          />
        </template>
        <template #toolbar-start>
          <button
            type="button"
            class="chat-action-button"
            :title="translate('pages.clarify.addImage')"
            :aria-label="translate('pages.clarify.addImage')"
            data-testid="clarify-attach-btn"
            :disabled="observe"
            @click="fileInput?.click()"
          >
            <Icon name="plus" :size="18" />
          </button>
          <slot name="composer-controls" :compact="true" />
        </template>
        <template v-if="observe" #toolbar-end>
          <button
            type="button"
            class="chat-action-button chat-action-stop"
            data-testid="clarify-observe-stop"
            :title="translate('pages.clarify.observeStopTitle')"
            :aria-label="translate('pages.clarify.observeStop')"
            :aria-busy="stopping ? 'true' : undefined"
            :disabled="stopping"
            @click="emit('takeover')"
          >
            <Icon :name="stopping ? 'spinner' : 'chat-stop'" :size="16" :class="stopping ? 'animate-spin motion-reduce:animate-none' : ''" />
          </button>
        </template>
        <template v-else #toolbar-end>
          <button
            v-if="sessionBusy"
            type="button"
            class="chat-action-button chat-action-stop"
            data-testid="clarify-review-cancel"
            title="Cancel"
            aria-label="Cancel"
            @click="cancelReview"
          >
            <Icon name="chat-stop" :size="16" />
          </button>
          <button
            v-if="!sessionBusy || hasComposerDraft()"
            type="button"
            class="chat-action-button chat-action-primary"
            :data-testid="sendLabel ? 'clarify-send-label' : 'clarify-send-icon'"
            :title="composerSending ? translate('common.buttons.sending') : clarifySendAria"
            :aria-label="composerSending ? translate('common.buttons.sending') : clarifySendAria"
            :aria-busy="composerSending ? 'true' : undefined"
            :disabled="composerSending || !hasComposerDraft()"
            @click="send"
          >
            <Icon :name="composerSending ? 'spinner' : 'chat-send'" :size="18" :class="composerSending ? 'animate-spin motion-reduce:animate-none' : ''" />
          </button>
          <Transition
            :css="false"
            @enter="confirmActionMotion.enter"
            @leave="confirmActionMotion.leave"
            @enter-cancelled="confirmActionMotion.cancel"
            @leave-cancelled="confirmActionMotion.cancel"
          >
            <span
              v-if="!hideFinish"
              v-show="showHotConfirm"
              ref="confirmPresence"
              class="clarify-confirm-presence"
              :inert="!showHotConfirm"
              :aria-hidden="!showHotConfirm ? 'true' : undefined"
            >
              <span class="clarify-confirm-presence__inner">
                <button
                  type="button"
                  class="chat-action-confirm clarify-confirm-integrated h-9 shrink-0 inline-flex"
                  data-testid="clarify-confirm-flow"
                  :disabled="confirmDisabled || !showHotConfirm"
                  :title="translate('pages.clarify.confirmFlowTitle')"
                  @click="finishEarly"
                >
                  <Icon name="check" :size="13" />
                  {{ validating ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlow') }}
                </button>
              </span>
            </span>
          </Transition>
        </template>
        <template v-if="reviewMode && !observe" #hint>
          <p
            class="m-0 min-w-0 text-[11px] leading-snug text-txt3 [overflow-wrap:anywhere]"
            data-testid="clarify-confirm-hint"
          >
            {{ validating ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlowHint') }}
          </p>
        </template>
      </ComposerShell>
    </div>
    <!-- Confirm error sits below the composer; shrink-0 keeps it from being clipped (plan g1.2). -->
    <div
      v-if="confirmError && !done"
      class="flex shrink-0 items-center gap-1.5 border-t border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
      data-testid="clarify-confirm-error"
      role="alert"
    >
      <Icon name="alert" :size="13" />
      <span class="min-w-0 flex-1 [overflow-wrap:anywhere]">{{ confirmError }}</span>
      <button
        v-if="confirmCanAbort && !composerBlocksConfirm"
        ref="abortConfirmButton"
        type="button"
        class="chat-action-secondary"
        data-testid="clarify-abort-and-confirm"
        @click="finishAbort"
      >
        {{ translate('pages.clarify.abortAndConfirm') }}
      </button>
    </div>
  </div>

  <Teleport to="body">
    <div
      v-if="showDemoWindow && focusedOption && previewFocus"
      ref="demoWindowEl"
      class="fixed z-50 flex flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-card"
      role="dialog"
      :aria-labelledby="demoWindowTitleId"
      data-testid="clarify-demo-window"
      :style="demoWindowStyle"
    >
      <div
        v-for="edge in demoResizeEdges"
        :key="edge"
        :class="resizeHandleClass(edge)"
        :data-testid="`clarify-demo-window-resize-${edge}`"
        @pointerdown="onResizePointerDown(edge, $event)"
      />
      <div
        class="relative z-10 flex h-14 shrink-0 cursor-grab select-none items-center gap-2 border-b border-line px-5 active:cursor-grabbing"
        data-testid="clarify-demo-window-titlebar"
        @pointerdown="onTitlePointerDown"
      >
        <div :id="demoWindowTitleId" class="min-w-0 flex-1 truncate text-[15px] font-semibold text-txt">
          {{ focusedTitle }}
        </div>
        <button
          type="button"
          class="flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded-md text-txt3 hover:bg-elevated hover:text-txt"
          data-testid="clarify-demo-window-close"
          @pointerdown.stop
          @click="closePreviewFocus"
        >
          <Icon name="close" :size="18" />
        </button>
      </div>
      <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div
          class="flex min-h-0 min-w-0 flex-1 flex-col"
          :data-preview-option="focusedOption.id"
          data-testid="clarify-preview-wall"
        >
          <ClarifyDemoFrame
            class="min-h-0 flex-1"
            :label="focusedOption.label"
            :html="windowDemoHtml"
            :highlighted="focusedHighlighted"
            :selected="previewFocus.readonly && focusedHighlighted"
            :enlargeable="false"
            expanded
          />
        </div>
      </div>
    </div>
  </Teleport>

  <!-- Human history attachment image preview (single slot; no gallery / Esc) -->
  <ChatImagePreviewModal
    :open="!!imagePreview"
    :src="imagePreview?.src || ''"
    :label="imagePreview?.label || ''"
    test-id-prefix="clarify-image-preview"
    @close="closeImagePreview"
  />
  <ConfirmFlowDialog
    :open="confirmFlowDialogOpen"
    :discard="confirmFlowDiscard"
    @cancel="dismissConfirmFlow"
    @confirm="acceptConfirmFlow"
  />
</template>

<style scoped>
[data-testid="clarify-scroller"] {
  padding: 24px 24px 56px 44px;
  scrollbar-gutter: stable;
}
.clarify-transcript { width: 100%; max-width: 760px; margin-inline: auto; }
.clarify-turn { margin-top: 28px; }
.chat-response-heading { margin-bottom: 10px; font-size: 12px; font-weight: 500; }
.chat-response-row + .chat-response-row {
  margin-top: 10px;
  border-top: 1px solid rgb(var(--c-line) / .7);
  padding-top: 10px;
}
.chat-response-question { font-size: 12px; line-height: 1.65; }
.chat-response-answers,
.chat-response-value { font-size: 14px; line-height: 1.75; }
.chat-response-answers > span + span::before {
  content: '·';
  margin-right: 8px;
  color: rgb(var(--c-txt2));
}
.clarify-request {
  max-width: 100%;
  border-left: 1px solid rgb(var(--c-line-strong) / .8);
  padding: 2px 0 2px 14px;
}
.clarify-request--history { border-color: rgb(var(--c-line)); }
.clarify-question-heading {
  display: grid;
  grid-template-columns: 16px minmax(0, 1fr);
  align-items: start;
  column-gap: 8px;
  row-gap: 6px;
}
.clarify-question-heading > .app-icon { margin-top: 3px; }
.clarify-question-kind { grid-column: 2; }
.clarify-request button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .45); outline-offset: 2px; }
[data-testid="clarify-turn-bottom-time"] { margin-top: 6px; }
[data-testid="clarify-hot-actions"],
[data-testid="clarify-cold-actions"] {
  width: 100%;
  max-width: 800px;
  margin-inline: auto;
  border-color: transparent;
}
[data-testid="clarify-attach-btn"] { border-color: transparent; background: transparent; }
@media (max-width: 767px) {
  [data-testid="clarify-scroller"] { padding: 18px 16px 56px 36px; }
  .clarify-request { padding-left: 12px; }
  [data-testid="clarify-option-btn"],
  [data-testid="clarify-other-row"],
  [data-testid="clarify-form-input"] { min-height: 44px; }
}
.clarify-confirm-integrated {
  max-width: 100%;
  padding-inline: 10px;
  font-size: 12px;
}
.clarify-confirm-presence {
  display: inline-flex;
  flex: 0 0 auto;
  min-width: 0;
  max-width: 100%;
  /* Include the adjacent 6px flex gap in the animated width. */
  margin-inline-start: -6px;
}
.clarify-confirm-presence__inner {
  display: inline-flex;
  flex: 0 0 auto;
  min-width: 0;
  max-width: 100%;
  padding-inline-start: 6px;
}

/* Placeholder must wrap to the textarea width — UA default often clips the 2nd line. */
.composer-hint-wrap::placeholder {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  overflow: visible;
  text-overflow: unset;
}
.composer-hint-wrap::-webkit-input-placeholder {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  overflow: visible;
  text-overflow: unset;
}
.composer-hint-wrap::-moz-placeholder {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  overflow: visible;
  opacity: 1;
}

/* Card-deck step transition: subtle slide + fade as one card advances. */
.deck-enter-active,
.deck-leave-active {
  transition: opacity 0.16s ease, transform 0.16s ease;
}
.deck-enter-from {
  opacity: 0;
  transform: translateX(8px);
}
.deck-leave-to {
  opacity: 0;
  transform: translateX(-8px);
}

/* A quiet phase label; the message and caret carry the streaming activity. */
.clarify-outputting {
  color: rgb(var(--c-txt3));
}

/* Streaming caret at message tail (Demo phase 3) */
.clarify-stream-caret {
  display: inline-block;
  width: 3px;
  height: .9em;
  margin-left: 4px;
  border-radius: 2px;
  vertical-align: text-bottom;
  background: rgb(var(--c-accent));
  animation: clarify-caret-blink 1.4s ease-in-out infinite;
}
@keyframes clarify-caret-blink {
  0%, 100% { opacity: .25; }
  50% { opacity: .9; }
}
@media (prefers-reduced-motion: reduce) {
  .deck-enter-active, .deck-leave-active { transition: none; }
  .deck-enter-from, .deck-leave-to { transform: none; }
  .clarify-stream-caret {
    animation: none !important;
  }
  .deck-enter-active,
  .deck-leave-active { transition: none; }
  .deck-enter-from,
  .deck-leave-to { transform: none; }
}
</style>

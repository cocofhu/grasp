<script lang="ts">
/** Host-owned inline hand-off after 确认并流转 (see useGatesInbox.activeHandoff). */
export type ClarifyHandoff = {
  phase: 'pending' | 'done'
  at: string
  target?: string
  /** Successor runs on its own. False when a person is next. Absent when unknown. */
  targetAuto?: boolean
}
/** Earlier node of the same run shown above this conversation (useGatesInbox.activeChainSegments). */
export type ClarifyPriorSegment = {
  nodeId: string
  iteration: number
  label: string
  kind: 'chat' | 'auto' | 'gate'
  status: string
  at: string
  /** Ended segments only; preferred meta time. */
  endedAt?: string
  turns: ClarifyTurn[]
  /** Gate segments (#850 follow-up): approve / reject outcome, when the backend sends one. */
  decision?: string
  /** What this node does (chain task), asked when no note was handed to it. */
  task?: string
  /** The note this node left for the next one (outputs.handoff). */
  handoff?: string
  handoffSource?: 'agent' | 'platform'
  open: boolean
  /** Consecutive auto nodes folded into one row (Linear-style grouping): their labels. */
  group?: string[]
  /** Folded auto nodes, each still addressable for its own execution record. */
  members?: { nodeId: string; iteration: number; label: string; at?: string; endedAt?: string }[]
  /** LLM process rows that belong with this visit (流转 / 等人 / 退回 / 结束). */
  notes?: { key: string; event: string; nodeLabel: string; toLabel?: string; edgeKind?: string; afterTurn?: number }[]
}
/** Follow-the-run tail after a hand-off whose next node is not waiting for the user. */
export type ClarifyFollowTail = { kind: 'waiting' | 'auto' | 'ended' | 'error'; label?: string; status?: string; at: string }
/** Downstream auto visit drawn under the hand-off while following. One entry per node and iteration. */
export type ClarifyFollowProcess = {
  nodeId: string
  iteration: number
  label?: string
  parts: AgentPart[]
  live: boolean
  startedAt?: string
  endedAt?: string
}
</script>

<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import ChatTurnAttachments from './ChatTurnAttachments.vue'
import ChatUserMessage from './ChatUserMessage.vue'
import ClarifyMessageThread from './ClarifyMessageThread.vue'
import LlmSystemRow from './llm/LlmSystemRow.vue'
import { notesAfterVisit, notesBesideTurn } from '@/lib/run/llmTranscriptThread'
import ClarifyThinkingRow from './ClarifyThinkingRow.vue'
import { nodeAsk } from './clarifyPrior'
import ChatImageThumb from '../ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '../ui/ChatImagePreviewModal.vue'
import ComposerShell from './ComposerShell.vue'
import ClarifyDemoFrame from './ClarifyDemoFrame.vue'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'
import ClarifyBootLoader from './ClarifyBootLoader.vue'
import { readSidebarHandoff, sidebarHandoffEpoch } from '@/lib/inbox/sidebarHandoff'
import { confirmFlowWindowVisible } from '@/lib/inbox/confirmFlowWindow'
import { bootKey as makeBootKey, getBootRecord } from '@/lib/run/bootRecord'
import { resolveBootDisplay } from '@/lib/run/bootDisplay'
import { emptyFailHeadline, turnProcessParts } from '@/lib/inbox/clarifyEmptyFail'
import AgentToolGroup from './AgentToolGroup.vue'
import AgentTimeline from './AgentTimeline.vue'
import ClarifyHandoffBubble from './ClarifyHandoffBubble.vue'
import StreamMarkdown from './StreamMarkdown.vue'
import AnnotationChip from './AnnotationChip.vue'
import LiveVariantCard from './LiveVariantCard.vue'
import PendingSendQueuePanel from './PendingSendQueuePanel.vue'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'
import { lastMessageText } from '@/lib/chat/messagePreview'
import { createClarifyChatAnchors, priorRailItems, revealRailTarget } from './clarifyChatAnchors'
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
  AgentPart,
  ClarifyTurn,
  ClarifyImage,
  ReactAnnotation,
  ReactQuestion,
} from '@/lib/shared/types'
import { isImageAttachment, attachmentDisplayName } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import { renderChatMarkdown } from '@/lib/shared/markdown'
import { useClarifyChat } from '@/lib/inbox/useClarifyChat'
import { hasFailedTool } from '@/lib/run/acpTools'
import { thoughtParagraphs } from '@/lib/run/thoughtParagraphs'
import ComposerPrimaryAction, { type ComposerPrimaryKind } from './ComposerPrimaryAction.vue'
import ConfirmFlowDialog from './ConfirmFlowDialog.vue'

defineOptions({ inheritAttrs: false })

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
    /** Inline hand-off row (host phase); without it the row follows this chat's own confirm click. */
    handoff?: ClarifyHandoff | null
    /** Earlier nodes of this run (continuous transcript, one fold per node). */
    priorSegments?: ClarifyPriorSegment[]
    /** This node's label, for the separator into it. */
    nodeLabel?: string
    /** Same composer, locked (following the run after a hand-off): no typing / attach / primary. */
    composerLocked?: boolean
    /**
     * Specific ended state for the done footer (run live page: waiting at a
     * gate / run finished). Same footer element; ok = the done style, muted =
     * the closed style. Absent: the generic 「交互已完成，进入下一步」.
     */
    doneNote?: { text: string; tone: 'ok' | 'muted' | 'wait' } | null
    /** Row under the hand-off while following the run. */
    followTail?: ClarifyFollowTail | null
    /** Steps of the downstream auto nodes, in visit order. Empty when not following. */
    followProcess?: ClarifyFollowProcess[] | null
    /** LLM process rows for this open visit. */
    systemNotes?: { key: string; event: string; nodeLabel: string; toLabel?: string; edgeKind?: string; afterTurn?: number }[]
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
    systemNotes: () => [],
  },
)

// This node visit's boot (connecting checklist) → one folded row above the first reply.
const bootRecordKey = computed(() => makeBootKey(props.runId, props.nodeId, props.iteration))
const bootRecord = computed(() => getBootRecord(bootRecordKey.value))
const bootPending = computed(() => !!bootRecord.value && !bootRecord.value.live)
const bootLive = computed(() => !!bootRecord.value?.live)
/**
 * Window opened while this visit had not connected. Its new handoff and agent
 * replies stay hidden until the window is actually gone.
 */
const sealBootMessages = ref(false)
/** This dialog was confirmed, as opposed to cancelled. */
const flowCommitted = ref(false)
/**
 * Latched only after the sidebar mark confirms an automatic successor.
 * A mark that is still unconfirmed is unknown, not “the next process is a person”.
 */
const enteredAuto = ref<boolean | null>(null)

function latchEnteredAuto() {
  const mark = readSidebarHandoff(props.runId)
  if (mark?.confirmedAuto) enteredAuto.value = true
}

watch(confirmFlowWindowVisible, (visible, was) => {
  if (visible && !was) sealBootMessages.value = bootPending.value
  if (!visible && was) {
    sealBootMessages.value = false
    if (!flowCommitted.value) enteredAuto.value = null
  }
})
watch(bootRecordKey, () => {
  if (confirmFlowWindowVisible.value && bootPending.value) sealBootMessages.value = true
})
watch(
  () => `${props.nodeId}\0${props.iteration}`,
  (now, prev) => {
    if (!prev || now === prev || confirmFlowWindowVisible.value) return
    flowCommitted.value = false
    enteredAuto.value = null
  },
)
watch(sidebarHandoffEpoch, () => {
  if (!flowCommitted.value) return
  latchEnteredAuto()
})

const nextAuto = computed<boolean | null>(() => {
  const tail = props.followTail?.kind
  if (tail === 'auto') return true
  if (tail === 'waiting' || tail === 'ended' || tail === 'error') return false
  if (typeof props.handoff?.targetAuto === 'boolean') return props.handoff.targetAuto
  if (!flowCommitted.value) return null
  // False is reserved for an explicit non-automatic successor. Unknown stays null.
  return enteredAuto.value === true ? true : null
})

const bootView = computed(() => resolveBootDisplay({
  windowVisible: confirmFlowWindowVisible.value,
  seal: sealBootMessages.value,
  committed: flowCommitted.value,
  nextAuto: nextAuto.value,
  hasBoot: !!bootRecord.value,
  live: bootLive.value,
}))
/** Folded「环境已就绪」, or merged into the divider when earlier nodes are on screen. */
const bootSummary = computed(() => bootView.value.showReady)
/** In-progress checklist. Hidden while the confirm window is still painted. */
const bootStarting = computed(() => bootView.value.showStarting)
/** This boot's handoff bubble and agent replies wait until「连接 Agent」. */
const holdBootMessages = computed(() => bootView.value.holdMessages)
/** A reply placeholder with nothing in it yet (思考中). */
function isWaitingAgentTurn(t: ClarifyTurn): boolean {
  return t.role === 'agent' && !!t.streaming && !String(t.text || '').trim() && !t.parts?.length && !t.tools?.length
}
/** After a hand-off the boot summary merges into the 已流转到 separator. */
const bootOnDivider = computed(() => !!props.priorSegments?.length)
const dividerTitle = computed(() =>
  props.nodeLabel ? translate('pages.clarify.handoffDoneTo', { name: props.nodeLabel }) : translate('pages.clarify.handoffDone'),
)
const bootSummaryAt = computed(() => displayTurns.value.findIndex((x) => x.role === 'agent'))

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
  releaseConfirmPending: chat.releaseConfirmPending,
  settleConfirmedTurn: chat.settleConfirmedTurn,
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
  followKeepsComposer,
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
 * Inline hand-off row at the end of the transcript, replacing the full-desk
 * 已确认并流转 overlay. The host (inbox) owns the phase when it passes
 * `handoff`; otherwise (run detail) it follows this chat: pending from the
 * confirm click until `done`, then done.
 */
const localHandoffAt = ref<string | null>(null)
watch(
  () => props.done,
  (d) => {
    if (d && validating.value) localHandoffAt.value = new Date().toISOString()
    if (!d) localHandoffAt.value = null
  },
  { flush: 'sync' },
)
const handoffRow = computed<ClarifyHandoff | null>(() => {
  if (props.handoff) return props.handoff
  if (validating.value) return { phase: 'pending', at: '' }
  if (localHandoffAt.value) return { phase: 'done', at: localHandoffAt.value }
  return null
})
watch(() => handoffRow.value?.phase, (ph) => { if (ph) void scrollBottom() })
// Host hand-off settled: the composer drops「校验中…」and stays the same composer
// (the host disables it while following the run, enables it on the next node).
watch(() => props.handoff?.phase, (ph) => { if (ph === 'done') validating.value = false })

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
const messageRailItems = computed(() => [
  ...priorRailItems(props.priorSegments, { fallbackTitle: translate('pages.clarify.myChoice'), bubbleText: humanBubbleText }),
  ...currentRailItems.value,
])
const currentRailItems = computed(() => {
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

function onMessageNavigate(id: string) {
  stickToBottom.value = false
  revealRailTarget(scroller.value, id)
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
const abortConfirmButton = ref<HTMLButtonElement | null>(null)
watch(composerBlocksConfirm, (blocked) => {
  // Keep focus in the composer when the abort-and-confirm action disappears.
  if (blocked && !!abortConfirmButton.value && document.activeElement === abortConfirmButton.value) {
    void nextTick(() => textareaRef.value?.focus({ preventScroll: true }))
  }
}, { flush: 'pre' })

/**
 * 校验中 / host handoff.pending: keep the primary on「校验中…」and refuse a
 * second confirm. Input and attachments stay usable — only observe and a true
 * follow lock (#850) disable the textarea / 「+」.
 */
const confirmBusy = computed(() => validating.value || props.handoff?.phase === 'pending')
/** Existing observe bar: run-live observe, or this chat following an auto node. */
const observeComposer = computed(() => !!props.observe || props.followTail?.kind === 'auto')
const composerInputLocked = computed(() => observeComposer.value || !!props.composerLocked)

/**
 * The composer shows exactly one primary action (fix-post-launch/button-states.md):
 * draft or send in flight → 发送; session busy → 停止;
 * finishable → 确认并流转; otherwise a disabled 发送.
 * While confirmBusy, the slot stays 确认并流转 even if the user starts a draft.
 */
const primaryKind = computed<ComposerPrimaryKind>(() => {
  if (confirmBusy.value) return 'confirm'
  if (composerSending.value || hasComposerDraft()) return 'send'
  if (sessionBusy.value) return 'stop'
  if (!props.hideFinish) return 'confirm'
  return 'send'
})
const primaryDisabled = computed(() => {
  if (primaryKind.value === 'stop') return false
  // Following the run after a hand-off (#850): send and confirm stay locked
  // until the run's next node waits for the user. 停止 is never locked.
  if (props.composerLocked) return true
  if (confirmBusy.value) return true
  if (primaryKind.value === 'confirm') return confirmDisabled.value
  return composerSending.value || !hasComposerDraft()
})
const primaryLabel = computed(() => {
  if (primaryKind.value === 'stop') return translate('common.buttons.stop')
  if (primaryKind.value === 'confirm') {
    return confirmBusy.value ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlow')
  }
  return composerSending.value ? translate('common.buttons.sending') : clarifySendAria.value
})
const primaryTestid = computed(() => {
  if (primaryKind.value === 'stop') return 'clarify-review-cancel'
  if (primaryKind.value === 'confirm') return 'clarify-confirm-flow'
  return props.sendLabel ? 'clarify-send-label' : 'clarify-send-icon'
})
function onPrimary(kind: ComposerPrimaryKind) {
  if (kind === 'stop') cancelReview()
  else if (props.composerLocked || confirmBusy.value) return
  else if (kind === 'confirm') finishEarly()
  else send()
}

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

function historyOptionPicked(q: ReactQuestion, turnIndex: number, label: string) {
  return selectedLabelsForQuestion(q, choiceRowsForAgentTurn(turnIndex)).includes(label)
}

/** Accessible name keeps the option copy and the existing enlarge copy. Neither word is painted. */
function historyPreviewAria(label: string, selected: boolean) {
  const enlarge = translate('pages.clarify.demoEnlarge')
  if (!selected) return `${label}，${enlarge}`
  return `${label}，${translate('pages.clarify.demoSelected')}，${enlarge}`
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

type FollowTailView = { busy: boolean; completed: boolean; interrupted: boolean; title: string; busyLabel: string; doneLabel: string; interruptedLabel: string; busyIcon: string }
/**
 * Tail row after a hand-off. Title = what the run is doing; right label =
 * what happens next (where the conversation continues), so the disabled
 * composer never reads as broken.
 */
const followProcessRows = computed(() => (props.followProcess || []).filter((seg) => seg.live || seg.parts.length))
function followProcessOutputting(parts: AgentPart[]): boolean {
  for (let i = parts.length - 1; i >= 0; i--) {
    const part = parts[i]!
    if (part.kind === 'tool') return false
    if ((part.kind === 'message' || part.kind === 'thought') && part.text?.trim()) return part.kind === 'message'
  }
  return false
}
const followTailView = computed<FollowTailView | null>(() => {
  const ft = props.followTail
  if (!ft) return null
  const at = relTime(ft.at)
  const base = { busy: false, completed: false, interrupted: false, busyLabel: '', doneLabel: at, interruptedLabel: at, busyIcon: 'arc-spinner' }
  if (ft.kind === 'auto') {
    return { ...base, busy: true, busyIcon: 'breath-dot', title: ft.label ? translate('pages.clarify.followAutoTo', { name: ft.label }) : translate('pages.clarify.followAuto'), busyLabel: translate('pages.clarify.followAutoBadge') }
  }
  if (ft.kind === 'waiting') {
    return { ...base, busy: true, title: ft.label ? translate('pages.clarify.followWaitingTo', { name: ft.label }) : translate('pages.clarify.followWaiting'), busyLabel: translate('pages.clarify.followWaitingBadge') }
  }
  if (ft.kind === 'error') {
    return { ...base, busy: true, title: translate('pages.clarify.followError'), busyLabel: translate('pages.clarify.followErrorBadge') }
  }
  if (ft.status === 'failed') return { ...base, interrupted: true, title: translate('pages.clarify.followEndedFailed') }
  if (ft.status === 'cancelled') return { ...base, interrupted: true, title: translate('pages.clarify.followEndedCancelled') }
  return { ...base, completed: true, title: translate('pages.clarify.followEnded') }
})
const currentAsk = computed(() => nodeAsk(props.priorSegments || [], (props.priorSegments || []).length, props.nodeTask, props.nodeId))
/** Ready row sits in this transcript (not on the divider), so the handoff goes under it. */
const askUnderReady = computed(() =>
  !!currentAsk.value && !holdBootMessages.value && bootSummary.value && !bootOnDivider.value,
)
const showAskAbove = computed(() => !!currentAsk.value && !holdBootMessages.value && !askUnderReady.value)

function onFlowConfirm() {
  flowCommitted.value = true
  acceptConfirmFlow()
  latchEnteredAuto()
}

function onFlowDismiss() {
  if (!flowCommitted.value) {
    enteredAuto.value = null
  }
  dismissConfirmFlow()
}

function showTurn(t: ClarifyTurn): boolean {
  if (isEmptyLiveTurn(t) || (props.observe && isBlankHumanTurn(t))) return false
  if (holdBootMessages.value && t.role === 'agent') return false
  if (bootStarting.value && isWaitingAgentTurn(t)) return false
  return true
}


/** A legacy (timeline-less) reply with a failed tool call keeps its completion footnote. */
function turnHasFailedTool(t: ClarifyTurn): boolean {
  if (t.parts?.length) return hasFailedTool(t.parts)
  return (t.tools || []).some((tool) => (tool.status || '').toLowerCase() === 'failed')
}

function humanLiveCardLoading(live: NonNullable<ClarifyTurn['live']>, i: number) {
  if (liveCards.value.newest.get(live.sid) !== i || live.op === 'accept' || live.op === 'discard') return false
  return isLiveBusy(liveHost?.store.sessions[live.sid]?.state)
}
</script>

<template>
  <!--
    Forward parent attributes to the chat column, alongside the image preview.
    Keep min-h-0 flex-1 so page-control status + this column share shell height
    and shrink-0 confirm footer is not clipped (plan g1.3).
  -->
  <div ref="chatRoot" class="chat-thread flex h-full min-h-0 min-w-0 flex-1 flex-col" data-review-composer v-bind="$attrs">
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
      <ClarifyMessageThread
        v-if="priorSegments && priorSegments.length"
        priors-only
        :run-id="runId"
        :prior-segments="priorSegments"
        :node-id="nodeId"
        :node-label="nodeLabel"
        :node-task="nodeTask"
        :boot-summary="bootSummary"
        :boot-record-key="bootRecordKey"
        :divider-title="dividerTitle"
        :parse-choice-summary="parseChoiceSummary"
        :parse-form-summary="parseFormSummary"
      />
      <!--
        Below the separator: the note the upstream node handed this one, else
        what this node is doing (the chain segment's / taken-over node's task),
        as its first User Message.
      -->
      <ClarifyHandoffBubble v-if="showAskAbove && currentAsk" class="clarify-turn" :ask="currentAsk" />
      <ClarifyBootLoader v-if="bootStarting" phase="starting" variant="inline" :boot-key="bootRecordKey" />
      <LlmSystemRow
        v-for="note in notesBesideTurn(systemNotes, -1, displayTurns.length)"
        :key="note.key"
        :event="note.event"
        :node-label="note.nodeLabel"
        :to-label="note.toLabel"
        :edge-kind="note.edgeKind"
      />
      <template v-for="(t, i) in displayTurns" :key="messageAnchorState.keys[i]">
      <ClarifyBootLoader
        v-if="bootSummary && !bootOnDivider && i === bootSummaryAt"
        phase="done"
        variant="inline"
        :boot-key="bootRecordKey"
      />
      <ClarifyHandoffBubble
        v-if="askUnderReady && currentAsk && bootSummaryAt >= 0 && i === bootSummaryAt"
        class="clarify-turn"
        :ask="currentAsk"
      />
      <div
        v-if="showTurn(t)"
        class="clarify-turn flex"
        :class="t.role === 'human' ? 'justify-end' : ''"
        :data-turn-role="t.role"
        :data-chat-message-id="messageAnchorState.anchors[i]"
        :data-chat-anchor="messageAnchorState.anchors[i]"
      >
        <span class="sr-only">{{ translate(t.role === 'human' ? 'pages.clarify.me' : 'pages.llmTranscript.speaker.agent') }}</span>
        <div class="min-w-0" :class="turnColumnClass(t, i)">
          <ChatTurnAttachments
            v-if="t.images && t.images.length"
            :images="t.images"
            :role="t.role"
            @preview="openImagePreview(t.images!, $event)"
          />
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
              <Icon name="form" :size="16" /> {{ translate('pages.clarify.myForm') }}
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
            <!-- Waiting first token: lamp + 思考中… inside bubble -->
            <ClarifyThinkingRow v-if="t.streaming && !t.thought && !agentHasMessage(t)" />
            <!-- Steps in arrival order (thought / tools / message), like the persisted timeline -->
            <AgentTimeline
              v-if="turnProcessParts(t).length"
              :parts="turnProcessParts(t)"
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
                class="chat-thought chat-meta w-full"
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
                  <Icon name="chevron-down" :size="12" class="chat-thought-chevron" aria-hidden="true" />
                </summary>
                <div class="chat-thought-body break-words [overflow-wrap:anywhere]">
                  <p v-for="(para, pi) in thoughtParagraphs(agentThoughtDisplay(t, i))" :key="pi">{{ para }}</p>
                </div>
              </details>
              <AgentToolGroup v-if="t.tools?.length && !isRetryableFailedAgent(t)" :tools="t.tools" :busy="!!t.streaming" :run-id="runId" />
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
            </template>
            <!-- Failure sits under this turn's own steps, not under the capped tool list. -->
            <div
              v-if="isRetryableFailedAgent(t)"
              class="chat-meta mt-1 flex min-w-0 items-start gap-2 py-1"
              role="alert"
              data-testid="clarify-empty-fail"
            >
              <Icon name="alert" :size="16" class="mt-0.5 shrink-0 text-err" />
              <div class="min-w-0 flex-1">
                <div class="text-[13px] font-medium text-txt" data-testid="clarify-empty-fail-title">
                  {{
                    emptyFailHeadline(
                      t,
                      translate('pages.clarify.emptyFailTitle'),
                      translate('pages.clarify.replyFailTitle'),
                    )
                  }}
                </div>
                <div
                  class="mt-0.5 break-words text-[12px] text-txt3 [overflow-wrap:anywhere]"
                  data-testid="clarify-empty-fail-desc"
                >
                  {{
                    emptyFailDisplayText(
                      t,
                      translate('pages.clarify.emptyFailDesc'),
                    )
                  }}
                </div>
                <button
                  v-if="showFailRetry(t, i)"
                  type="button"
                  class="mt-1 text-[12px] font-medium text-txt2 hover:text-txt disabled:opacity-50"
                  :disabled="failRetryDisabled"
                  data-testid="clarify-empty-fail-retry"
                  @click="retryLastFailed"
                >
                  {{ translate('pages.clarify.retry') }}
                </button>
              </div>
            </div>
            <LiveVariantCard
              v-if="liveCards.full.has(i) && !t.streaming && liveSettled(liveCards.full.get(i)!.sid)"
              :live-ref="liveCards.full.get(i)!"
              agent
            />
            <!--
              Legacy replies without a timeline do not fold, so a failed one keeps
              the pre-#843 completion footnote: 已完成 + when it ended.
            -->
            <div
              v-if="showTurnCompleted(t) && !turnProcessParts(t).length && turnHasFailedTool(t)"
              class="chat-meta mt-1.5 flex items-center justify-between gap-2"
              data-testid="clarify-turn-completed"
            >
              <span class="text-txt2">{{ translate('pages.clarify.turnCompleted') }}</span>
              <span>{{ locale && relTime(t.at) }}</span>
            </div>
          </template>
          <!-- Human free-text bubble: chat markdown so tags stay visible and never execute. -->
          <ChatUserMessage
            v-else-if="humanBubbleText(t)"
            test-id="clarify-human-message"
            :text="humanBubbleText(t)"
          />

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
                <div v-if="demoOptionsOf(q).length" class="mt-1.5 flex flex-col gap-1.5">
                  <div
                    v-for="o in demoOptionsOf(q)"
                    :key="o.id"
                    data-testid="clarify-history-option"
                  >
                    <button
                      type="button"
                      class="clarify-history-preview"
                      :class="{ 'is-selected': historyOptionPicked(q, i, o.label) }"
                      data-testid="clarify-option-enlarge"
                      :aria-label="historyPreviewAria(o.label, historyOptionPicked(q, i, o.label))"
                      @click="openPreviewFocus(q, o.id, true, i)"
                    >
                      <svg class="clarify-history-thumb" viewBox="0 0 36 26" aria-hidden="true">
                        <rect x="0.6" y="0.6" width="34.8" height="24.8" rx="4" />
                        <path d="M5 7h10" />
                        <path d="M5 12h22" />
                        <path d="M5 16.5h16" />
                      </svg>
                      <span class="clarify-history-preview-label">{{ o.label }}</span>
                      <span
                        v-if="historyOptionPicked(q, i, o.label)"
                        class="clarify-history-preview-picked"
                      ><Icon name="check" :size="11" :interactive="false" class="inline align-middle" /> {{ translate('pages.clarify.demoSelected') }}</span>
                      <span class="clarify-history-preview-go" aria-hidden="true">
                        <Icon name="preview-expand" :size="14" />
                      </span>
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
                    <Icon name="form" :size="16" class="shrink-0 text-txt2" />
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
                <Icon name="form" :size="16" />
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
          <!--
            Turn still running: the live status sits at the end of the reply (where
            the next step will appear), never only at the top. Same lamp +
            思考中… / plain 输出中 styles as before.
          -->
          <ClarifyThinkingRow
            v-if="t.role === 'agent' && t.streaming && (t.thought || agentHasMessage(t) || t.parts?.length)"
            variant="status"
            :outputting="agentTailOutputting(t)"
          />
          <!-- No timestamp until the turn has ended. -->
          <div
            v-else-if="!showTurnCompleted(t) && !(t.role === 'agent' && t.streaming)"
            class="chat-meta mt-1"
            :class="t.role === 'human' ? 'text-right' : ''"
            data-testid="clarify-turn-bottom-time"
          >{{ locale && relTime(t.at) }}</div>
        </div>
      </div>
        <LlmSystemRow
          v-for="note in notesBesideTurn(systemNotes, i, displayTurns.length)"
          :key="note.key"
          :event="note.event"
          :node-label="note.nodeLabel"
          :to-label="note.toLabel"
          :edge-kind="note.edgeKind"
        />
      </template>
      <LlmSystemRow
        v-for="note in notesAfterVisit(systemNotes, displayTurns.length)"
        :key="note.key"
        :event="note.event"
        :node-label="note.nodeLabel"
        :to-label="note.toLabel"
        :edge-kind="note.edgeKind"
      />
      <ClarifyBootLoader
        v-if="bootSummary && !bootOnDivider && bootSummaryAt < 0"
        phase="done"
        variant="inline"
        :boot-key="bootRecordKey"
      />
      <ClarifyHandoffBubble
        v-if="askUnderReady && currentAsk && bootSummaryAt < 0"
        class="clarify-turn"
        :ask="currentAsk"
      />
      <!-- Inline hand-off (方案 B): one more step row in the「思考过程」style — ⟳ 流转到下一节点 · 校验中… → ✓ 已确认并流转 · 已完成. -->
      <Transition name="clarify-row">
      <div
        v-if="handoffRow"
        class="clarify-handoff-step chat-meta mt-2 flex w-full min-w-0"
        data-testid="clarify-handoff"
        :data-state="handoffRow.phase"
        role="status"
      >
        <ThoughtSummaryStatus
          :busy="handoffRow.phase === 'pending'"
          :completed="handoffRow.phase === 'done'"
          :title="handoffRow.phase === 'pending'
            ? (handoffRow.target ? translate('pages.clarify.handoffStepTo', { name: handoffRow.target }) : translate('pages.clarify.handoffStep'))
            : (handoffRow.target ? translate('pages.clarify.handoffDoneTo', { name: handoffRow.target }) : translate('pages.clarify.handoffDone'))"
          :busy-label="translate('pages.clarify.validating')"
        />
      </div>
      </Transition>
      <Transition name="clarify-row">
        <div
          v-if="handoffRow && followTail && followTailView"
          :key="followTail.kind + ':' + (followTail.label || '') + ':' + (followTail.status || '')"
          class="clarify-handoff-step chat-meta flex w-full min-w-0"
          data-testid="clarify-follow-tail"
          :data-kind="followTail.kind"
          role="status"
          aria-live="polite"
        >
          <ThoughtSummaryStatus
            :busy="followTailView.busy"
            :completed="followTailView.completed"
            :interrupted="followTailView.interrupted"
            :title="followTailView.title"
            :busy-label="followTailView.busyLabel"
            :done-label="followTailView.doneLabel"
            :interrupted-label="followTailView.interruptedLabel"
            :busy-icon="followTailView.busyIcon"
          />
        </div>
      </Transition>
      <div
        v-if="handoffRow && followTail && followProcessRows.length && !holdBootMessages"
        class="clarify-follow-process mt-1 flex w-full min-w-0 flex-col"
        data-testid="clarify-follow-process"
      >
        <div
          v-for="seg in followProcessRows"
          :key="seg.nodeId + ':' + seg.iteration"
          data-testid="clarify-follow-process-node"
          :data-node-id="seg.nodeId"
          :data-live="seg.live ? 'true' : 'false'"
        >
          <AgentTimeline
            v-if="seg.parts.length"
            :parts="seg.parts"
            :streaming="seg.live"
            :completed="!seg.live"
            :run-id="runId"
            :started-at="seg.startedAt"
            :ended-at="seg.endedAt"
            message-test-id="clarify-follow-agent-message"
          />
          <ClarifyThinkingRow
            v-if="seg.live"
            variant="status"
            :outputting="followProcessOutputting(seg.parts)"
          />
        </div>
      </div>
      <div v-if="thinking && !validating && liveAgentIdx < 0 && !bootPending && !bootStarting" class="chat-meta grasp-status-host flex items-center gap-2 pl-9">
        <Icon name="breath-dot" busy :size="16" class="shrink-0 text-accent-2" aria-hidden="true" />
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

    <div
      v-if="showDoneChrome && doneNote?.text"
      class="border-t border-line p-3 text-center text-[12px]"
      :class="doneNote.tone === 'ok' ? 'text-ok' : 'text-txt3'"
      data-testid="clarify-done-note"
      :data-tone="doneNote.tone"
    >
      <Icon v-if="doneNote.tone !== 'wait'" :name="doneNote.tone === 'ok' ? 'check' : 'close'" :size="13" class="-mt-0.5 mr-1 inline" />{{ doneNote.text }}
    </div>
    <div v-else-if="showDoneChrome" class="border-t border-line p-3 text-center text-[12px] text-ok">
      <Icon name="check" :size="13" class="-mt-0.5 mr-1 inline" />{{ translate('pages.clarify.done') }}
    </div>
    <!-- Success ceremony in flight after done: keep composer mounted-off until hold→done (g2.3). -->
    <div
      v-else-if="done && !followKeepsComposer"
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
            class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full pending-attach-remove"
            @click.stop="removeAttachment(ii)"
          ><Icon name="close" :size="9" /></button>
        </div>
      </div>
      <ComposerShell
        wrap-toolbar
        :show-footer="!hideFinish && reviewMode && !observeComposer"
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
            :placeholder="observeComposer ? translate(stopping ? 'pages.clarify.observeStoppingPlaceholder' : 'pages.clarify.observeInputPlaceholder') : inputPlaceholder"
            :disabled="composerInputLocked"
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
            :disabled="composerInputLocked"
            @click="fileInput?.click()"
          >
            <Icon name="plus" :size="18" />
          </button>
          <slot name="composer-controls" :compact="true" />
        </template>
        <template v-if="observeComposer" #toolbar-end>
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
            <Icon :name="stopping ? 'spinner' : 'chat-stop'" :size="16" />
          </button>
        </template>
        <template v-else #toolbar-end>
          <ComposerPrimaryAction
            :kind="primaryKind"
            :disabled="primaryDisabled"
            :busy="primaryKind === 'send' && composerSending"
            :label="primaryLabel"
            :title="primaryKind === 'confirm' ? translate('pages.clarify.confirmFlowTitle') : undefined"
            :testid="primaryTestid"
            @click="onPrimary"
          />
        </template>
        <template v-if="reviewMode && !observeComposer" #hint>
          <p
            class="m-0 min-w-0 text-[11px] leading-snug text-txt3 [overflow-wrap:anywhere]"
            data-testid="clarify-confirm-hint"
          >
            {{ confirmBusy ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlowHint') }}
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
    @cancel="onFlowDismiss"
    @confirm="onFlowConfirm"
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
.clarify-history-preview {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-height: 38px;
  padding: 6px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 10px;
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt2));
  font: inherit;
  font-size: 12px;
  line-height: 1.45;
  text-align: left;
  cursor: pointer;
  transition: border-color var(--dur-ui), background var(--dur-ui);
}
.clarify-history-preview:hover {
  border-color: rgb(var(--c-line-strong));
  background: rgb(var(--c-base));
}
.clarify-history-preview.is-selected {
  border-color: rgb(var(--c-accent) / .4);
  background: rgb(var(--c-accent) / .04);
}
.clarify-history-preview.is-selected:hover {
  border-color: rgb(var(--c-accent) / .65);
  background: rgb(var(--c-accent) / .04);
}
.clarify-history-preview:hover .clarify-history-preview-go {
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent-dim));
}
.clarify-history-thumb {
  display: block;
  width: 36px;
  height: 26px;
  flex: none;
}
.clarify-history-thumb rect {
  fill: rgb(var(--c-base));
  stroke: rgb(var(--c-line));
}
.clarify-history-preview.is-selected .clarify-history-thumb rect {
  fill: rgb(var(--c-surface));
  stroke: rgb(var(--c-line-strong));
}
.clarify-history-thumb path {
  fill: none;
  stroke: rgb(var(--c-elevated));
  stroke-width: 1.4;
  stroke-linecap: round;
}
.clarify-history-thumb path:first-of-type { stroke: rgb(var(--c-txt3)); }
.clarify-history-preview-label {
  min-width: 0;
  flex: 1;
  overflow-wrap: anywhere;
}
.clarify-history-preview-picked {
  flex: none;
  color: rgb(var(--c-accent));
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}
.clarify-history-preview-go {
  display: inline-flex;
  width: 26px;
  height: 26px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 7px;
  color: rgb(var(--c-txt2));
  transition: color var(--dur-ui), background var(--dur-ui);
}
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
/* Keep the line's height. Only the glyphs appear on hover, so the bubble does not shift. */
@media (hover: hover) and (pointer: fine) {
  .clarify-turn [data-testid="clarify-turn-bottom-time"] {
    opacity: 0;
    transition: opacity .15s ease;
  }
  .clarify-turn:hover [data-testid="clarify-turn-bottom-time"],
  .clarify-turn:focus-within [data-testid="clarify-turn-bottom-time"] {
    opacity: 1;
  }
}
@media (hover: hover) and (pointer: fine) and (prefers-reduced-motion: reduce) {
  .clarify-turn [data-testid="clarify-turn-bottom-time"] { transition: none; }
}
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
@media (max-width: 767px), (pointer: coarse) {
  .clarify-history-preview { min-height: 44px; }
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
.clarify-handoff-step { padding: 0 8px; margin-inline: -8px; width: calc(100% + 16px); }
/* Prior summary and the hand-off rows share one check column. */
.clarify-status-row { margin-inline: -8px; padding: 0 8px; width: calc(100% + 16px); }
/* History ends here: a hairline (same token as markdown <hr>) then the current node. */
.clarify-segment-divider { margin-top: 6px; padding-top: 10px; border-top: 1px solid rgb(var(--c-line) / .7); border-radius: 0; }
/* New in-stream status rows settle in quietly (no slide on reduced motion). */
.clarify-row-enter-active { transition: opacity .18s ease-out, transform .18s ease-out; }
.clarify-row-leave-active { transition: opacity .12s ease-in; }
.clarify-row-enter-from { opacity: 0; transform: translateY(3px); }
.clarify-row-leave-to { opacity: 0; }
/* Earlier nodes read as context: weaker than the live conversation. */
.clarify-prior-body { opacity: .72; }
.clarify-prior-body:hover { opacity: 1; }
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
  .clarify-history-preview,
  .clarify-history-preview-go { transition: none; }
  .clarify-row-enter-active, .clarify-row-leave-active { transition: none; }
  .clarify-row-enter-from { transform: none; }
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

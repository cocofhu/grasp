<script setup lang="ts">
import type { RestoreUnsent } from '@/lib/inbox/clarifyVisitEnded'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import ClarifyChat, { type ClarifyFollowProcess, type ClarifyFollowTail, type ClarifyHandoff, type ClarifyPriorSegment } from './ClarifyChat.vue'
import ComposerShell from './ComposerShell.vue'
import ParagraphInput from '../ui/ParagraphInput.vue'
import GateReactStreamPanel from './GateReactStreamPanel.vue'
import PendingSendQueuePanel, { type PendingQueueRow } from './PendingSendQueuePanel.vue'
import type { AgentTool, ClarifyTurn, ClarifyImage, ReactAnnotation, AcpEvent } from '@/lib/shared/types'
import AnnotationChip from './AnnotationChip.vue'
import PageControlStatus from './PageControlStatus.vue'
import type { GateReactTurn } from './gateApproval/gateApprovalContext'
import type { PageControlState } from '@/lib/inbox/embedPageControl'
import VisitModelSelect from './VisitModelSelect.vue'
import { visitModelSelectVisible } from '@/lib/run/visitModel'

/**
 * Thin mode wrapper around ClarifyChat / a gate-local composer.
 * - clarify: chips + attachments +「发送澄清回复」(classic react hides finish; Grasp shows 确认并流转)
 * - review: ClarifyChat chips + attachments + send +「确认并流转」
 * - gate: local composer with the same review semantics —「发送」+「确认并流转」
 *   (no 打回修改 / 通过并流转). Send → GateReactRevise; confirm → ResumeGate(approve/pass).
 *   Cold session (coldSession=true): no ReAct/hot hints; input/send unmounted; confirm only.
 */
const props = withDefaults(
  defineProps<{
    mode: 'clarify' | 'review' | 'gate'
    sendRequest?: (text: string, images: ClarifyImage[], annotations: ReactAnnotation[], clientMessageId?: string) => Promise<boolean>
    runId?: string
    nodeId?: string
    iteration?: number
    turns?: ClarifyTurn[]
    done?: boolean
    active?: boolean
    /** Gate: hot ReAct send/revise available (unmount send when false / cold). */
    canReject?: boolean
    /** Gate: show confirm action (unmount when false; do not use disabled-only). */
    canPass?: boolean
    /** Gate: send/revise in flight. */
    rejecting?: boolean
    rejectError?: string | null
    /** Gate: cold session — hide in-place edit UI; confirm remains. */
    coldSession?: boolean
    textOnly?: boolean
    /** Gate: disable confirm (e.g. open PreviewIssues). */
    passDisabled?: boolean
    /**
     * Gate: when true, send may fire without draft/attachments/annotations
     * (e.g. PreviewIssues n_open≥1 — issues already recorded elsewhere).
     */
    rejectAllowEmpty?: boolean
    passTitle?: string
    passLabel?: string
    rejectLabel?: string
    /** Review confirm failure (bottom status bar via ClarifyChat). */
    confirmError?: string | null
    confirmCanAbort?: boolean
    /** Home-chat first bubble before transcript lands. */
    seedHumanText?: string
    seedHumanImages?: ClarifyImage[]
    /**
     * Gate sandbox-aligned session UX (same surface as GateApproval mobile-fill /
     * content-fit): pending-send queue, streaming agent text, Cancel.
     */
    queued?: PendingQueueRow[]
    queueNotice?: string | null
    queueToast?: string | null
    thinking?: boolean
    streamText?: string
    /** ACP thought rail (separate from streamText). */
    streamThought?: string
    /** ACP tool_call rows of the current turn (name + status). */
    streamTools?: AgentTool[]
    interrupted?: boolean
    /** ISO when turn completed normally — drives restrained「已完成」footnote. */
    streamCompletedAt?: string | null
    /** Accepted user line for the gate transcript, before a frame arrives. */
    liveHumanText?: string
    gateTurns?: GateReactTurn[]
    /** This user's preview-page control state; unset hides the line. */
    pageControl?: PageControlState
    /** Read-only auto stream; Stop emits takeover (ClarifyChat observe). */
    observe?: boolean
    stopping?: boolean
    /** Inline hand-off row after 确认并流转 (host-owned phase). */
    handoff?: ClarifyHandoff | null
    /** Continuous run transcript (earlier nodes) + follow-the-run tail. */
    priorSegments?: ClarifyPriorSegment[]
    nodeLabel?: string
    /** Node task (opening User Message): takeover task or the chain segment's task. */
    nodeTask?: string
    composerLocked?: boolean
    /** Specific ended state for the done footer (see ClarifyChat). */
    doneNote?: { text: string; tone: 'ok' | 'muted' | 'wait' } | null
    followTail?: ClarifyFollowTail | null
    followProcess?: ClarifyFollowProcess[] | null
    /** Member composer may switch this visit's model. */
    modelSwitch?: boolean
    agentName?: string
    modelNodeId?: string
    modelIteration?: number
    currentConfigurationId?: string
    currentModelId?: string
    currentModelSet?: boolean
    reasoningEffort?: string
    modelNotice?: string
  }>(),
  {
    iteration: 1,
    turns: () => [],
    done: false,
    active: true,
    canReject: true,
    canPass: true,
    rejecting: false,
    rejectError: null,
    coldSession: false,
    textOnly: false,
    passDisabled: false,
    rejectAllowEmpty: false,
    passLabel: '',
    rejectLabel: '',
    confirmError: null,
    confirmCanAbort: false,
    seedHumanText: '',
    seedHumanImages: () => [],
    queued: () => [],
    queueNotice: null,
    queueToast: null,
    thinking: false,
    streamText: '',
    streamThought: '',
    streamTools: () => [],
    interrupted: false,
    streamCompletedAt: null,
    liveHumanText: '',
    observe: false,
    stopping: false,
  },
)

const emit = defineEmits<{
  (e: 'send', text: string, images: ClarifyImage[], annotations: ReactAnnotation[], clientMessageId?: string): void
  (e: 'retry-last'): void
  (e: 'finish'): void
  (e: 'finish-abort'): void
  (e: 'cancel'): void
  (e: 'takeover'): void
  (e: 'queue-remove', itemId: string | undefined, index: number): void
  (e: 'queue-reorder', itemIds: string[]): void
  (e: 'queue-edit', index: number): void
  (e: 'queue-cancel-item', index: number): void
  (e: 'queue-reorder-indexes', fromIndex: number, toIndex: number): void
}>()

const paragraphRef = ref<{ pickFiles?: () => void } | null>(null)

const chatRef = ref<{
  applyReviewFrame: (frame: any) => void
  applyAcpEvents: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
  applyQueueState: (
    waiting: number,
    items: any[] | null,
    busy?: boolean,
    activeItem?: any | null,
  ) => void
  cancelReview: () => void
  discardLastQueued: (clientMessageId?: string) => void
  releaseConfirmPending?: () => void
  settleConfirmedTurn?: () => void
  restoreUnsentQueued: RestoreUnsent
  isSessionBusy?: () => boolean
  playConfirmCeremony?: () => Promise<void>
} | null>(null)

defineExpose({
  /** Returns false when ClarifyChat is not mounted yet (hard-load race). */
  applyReviewFrame: (frame: any): boolean => {
    if (!chatRef.value?.applyReviewFrame) return false
    chatRef.value.applyReviewFrame(frame)
    return true
  },
  /**
   * Pass through ClarifyChat apply result — false when slot not ready
   * (must not return true merely because chatRef exists).
   */
  applyAcpEvents: (events: AcpEvent[] | undefined, nodeId?: string): boolean => {
    if (!chatRef.value?.applyAcpEvents) return false
    return chatRef.value.applyAcpEvents(events, nodeId) !== false
  },
  applyQueueState: (
    waiting: number,
    items: any[] | null,
    busy?: boolean,
    activeItem?: any | null,
  ) => chatRef.value?.applyQueueState?.(waiting, items, busy, activeItem),
  cancelReview: () => chatRef.value?.cancelReview(),
  discardLastQueued: (clientMessageId?: string) => chatRef.value?.discardLastQueued(clientMessageId),
  releaseConfirmPending: () => chatRef.value?.releaseConfirmPending?.(),
  settleConfirmedTurn: () => chatRef.value?.settleConfirmedTurn?.(),
  restoreUnsentQueued: (...args: Parameters<RestoreUnsent>) =>
    chatRef.value?.restoreUnsentQueued(...args),
  isChatReady: () => !!chatRef.value,
  /**
   * Platform session busy from ClarifyChat (thinking / queued / live slot).
   * Hosts gate soft-refresh on this — must not be missing through the composer (g1.3).
   */
  isSessionBusy: () => !!chatRef.value?.isSessionBusy?.(),
  playConfirmCeremony: () => chatRef.value?.playConfirmCeremony?.() ?? Promise.resolve(),
})

const { t } = useI18n()

const draft = defineModel<string>('draft', { default: '' })
const attachments = defineModel<ClarifyImage[]>('attachments', { default: () => [] })
const annotations = defineModel<ReactAnnotation[]>('annotations', { default: () => [] })

const canSubmitGate = computed(() => {
  if (props.rejecting) return false
  if (props.rejectAllowEmpty) return true
  return (
    draft.value.trim().length > 0 ||
    attachments.value.length > 0 ||
    annotations.value.length > 0
  )
})

const showGateCancel = computed(
  () => props.thinking || (props.queued?.length ?? 0) > 0,
)

/** Content that can be sent, including the existing empty-send allowance. */
const gateHasSendable = computed(
  () =>
    props.rejectAllowEmpty ||
    draft.value.trim().length > 0 ||
    attachments.value.length > 0 ||
    annotations.value.length > 0,
)

/** While busy with nothing to send, the stop icon occupies the send slot. */
const showGateSend = computed(() => {
  if (!props.canReject) return false
  if (showGateCancel.value && !gateHasSendable.value) return false
  return true
})

const gateQueued = computed<PendingQueueRow[]>(() =>
  (props.queued ?? []).map((q) => ({
    id: q.id,
    text: q.text,
    images: q.images ?? [],
    annotations: q.annotations ?? [],
  })),
)

/**
 * Footer hint (hot path only — cold path unmounts input/send and omits this hint):
 * - rejectAllowEmpty (n_open≥1): open issues already recorded; draft optional for send
 * - canReject (hot send): normal draft threshold
 * - else (confirm-only hot): submit feedback before send
 */
const gateFooterHint = computed(() => {
  if (props.rejectAllowEmpty) return t('pages.reviewComposer.openIssuesSendHint')
  if (props.canReject) return t('pages.reviewComposer.thresholdHint')
  return t('pages.gateApproval.helpReviseDetailNoIssuesNoForm')
})

const sendButtonLabel = computed(
  () => props.rejectLabel || t('pages.reviewComposer.send'),
)
const confirmButtonLabel = computed(
  () => props.passLabel || t('pages.clarify.confirmFlow'),
)

function removeAnnotation(i: number) {
  annotations.value.splice(i, 1)
}

function onSend() {
  if (!canSubmitGate.value || !props.canReject) return
  emit('send', draft.value, attachments.value, annotations.value)
}
function onConfirm() {
  if (!props.canPass || props.passDisabled) return
  emit('finish')
}
const showGateModel = computed(() => props.mode === 'gate' && visitModelSelectVisible({
  modelSwitch: props.modelSwitch && !!props.agentName,
  done: props.done,
  active: props.active,
  coldSession: props.coldSession,
  observe: props.observe,
  composerLocked: props.composerLocked,
}))
</script>

<template>
  <!-- Clarify / review: reuse ClarifyChat (chip + image + threshold already wired). -->
  <div
    v-if="mode === 'clarify' || mode === 'review'"
    class="chat-thread flex h-full min-h-0 min-w-0 flex-col"
    data-testid="review-composer-shell"
  >
    <div v-if="pageControl" class="shrink-0 border-b border-line px-3 py-1.5">
      <PageControlStatus :state="pageControl" />
    </div>
    <ClarifyChat
      ref="chatRef"
      class="min-h-0 flex-1"
      :run-id="runId || ''"
      :node-id="nodeId || ''"
      :iteration="iteration"
      v-model:draft="draft"
      v-model:attachments="attachments"
      v-model:annotations="annotations"
      :turns="turns ?? []"
      :done="done"
      :active="active"
      :cold-session="coldSession"
      :finish-disabled="passDisabled"
      :review-mode="mode === 'review'"
      :annotate-enabled="mode === 'clarify' || mode === 'review'"
      :hide-finish="!canPass"
      :seed-human-text="seedHumanText"
      :seed-human-images="seedHumanImages"
      :send-request="sendRequest"
      :send-label="mode === 'clarify' ? t('pages.reviewComposer.sendClarify') : undefined"
      :confirm-error="confirmError"
      :confirm-can-abort="confirmCanAbort"
      :observe="observe"
      :node-task="nodeTask"
      :stopping="stopping"
      :handoff="handoff ?? null"
      :prior-segments="priorSegments ?? []"
      :node-label="nodeLabel"
      :composer-locked="!!composerLocked"
      :model-switch="modelSwitch"
      :agent-name="agentName"
      :model-node-id="modelNodeId || nodeId"
      :model-iteration="modelIteration || iteration"
      :current-configuration-id="currentConfigurationId"
      :current-model-id="currentModelId"
      :current-model-set="currentModelSet"
      :reasoning-effort="reasoningEffort"
      :model-notice="modelNotice"
      :done-note="doneNote ?? null"
      :follow-tail="followTail ?? null"
      :follow-process="followProcess ?? null"
      @takeover="emit('takeover')"
      @send="(text, images, anns, clientMessageId) => emit('send', text, images, anns, clientMessageId)"
      @retry-last="emit('retry-last')"
      @finish="emit('finish')"
      @finish-abort="emit('finish-abort')"
      @cancel="emit('cancel')"
      @queue-remove="(itemId, index) => emit('queue-remove', itemId, index)"
      @queue-reorder="(itemIds) => emit('queue-reorder', itemIds)"
    >
      <template v-if="$slots['composer-controls']" #composer-controls="slotProps"><slot name="composer-controls" v-bind="slotProps" /></template>
    </ClarifyChat>
  </div>

  <!-- Gate: local composer with review-semantics sticky actions (send + confirm). -->
  <div
    v-else
    class="chat-thread flex h-full min-h-0 min-w-0 flex-col"
    data-testid="review-composer-gate"
    data-review-composer
  >
    <div class="review-gate-transcript scroll-area min-h-0 flex-1 overflow-y-auto px-4 py-5" data-conversation-scroll>
      <div class="mx-auto w-full max-w-[760px]">
      <slot name="gate-body">
        <p v-if="!coldSession" class="chat-meta">
          {{ t('pages.reviewComposer.gateHint') }}
        </p>
      </slot>
      <GateReactStreamPanel
        v-if="!coldSession"
        :run-id="runId"
        :thinking="thinking"
        :stream-text="streamText"
        :stream-thought="streamThought"
        :stream-tools="streamTools"
        :interrupted="interrupted"
        :completed-at="streamCompletedAt"
        :user-text="liveHumanText"
        :turns="gateTurns"
      />
      </div>
    </div>
    <div class="shrink-0 border-t border-line p-3" data-testid="review-composer-actions">
      <template v-if="!coldSession">
        <div v-if="annotations.length" class="mb-2 flex flex-wrap gap-1.5">
          <AnnotationChip
            v-for="(a, ai) in annotations"
            :key="ai"
            :ann="a"
            removable
            test-id="review-annotation-chip"
            @remove="removeAnnotation(ai)"
          />
        </div>
        <div v-if="rejectError" class="mb-1.5 text-[11px] text-err">{{ rejectError }}</div>
      </template>
      <ComposerShell :show-chrome="!coldSession" :show-footer="true" wrap-toolbar>
        <template v-if="!coldSession" #input>
          <ParagraphInput
            ref="paragraphRef"
            class="chat-input"
            v-model:text="draft"
            v-model:images="attachments"
            embedded
            :text-only="textOnly"
            :disabled="rejecting || !canReject"
            :placeholder="t('pages.gateApproval.reactRevise.placeholder')"
          />
        </template>
        <template v-if="!coldSession" #toolbar-start>
          <button
            v-if="!textOnly"
            type="button"
            class="chat-action-button"
            data-testid="paragraph-input-attach"
            :disabled="rejecting || !canReject"
            :title="t('common.paragraphInput.addImage')"
            :aria-label="t('common.paragraphInput.addImage')"
            @click="paragraphRef?.pickFiles?.()"
          >
            <Icon name="plus" :size="18" />
          </button>
          <VisitModelSelect
            v-if="showGateModel"
            :run-id="runId || ''"
            :node-id="modelNodeId || nodeId || ''"
            :iteration="modelIteration || iteration"
            :agent-name="agentName || ''"
            :current-configuration-id="currentConfigurationId"
            :current-model-id="currentModelId"
            :current-model-set="currentModelSet"
            :reasoning-effort="reasoningEffort"
            :model-notice="modelNotice"
          />
        </template>
        <template v-if="!coldSession" #toolbar-end>
          <button
            v-if="showGateCancel"
            type="button"
            class="chat-action-button chat-action-stop"
            data-testid="gate-react-cancel"
            title="Cancel"
            aria-label="Cancel"
            @click="emit('cancel')"
          >
            <Icon name="chat-stop" :size="16" />
          </button>
          <button
            v-if="showGateSend"
            type="button"
            class="chat-action-button chat-action-primary"
            data-testid="review-composer-send"
            :title="sendButtonLabel"
            :aria-label="sendButtonLabel"
            :disabled="!canSubmitGate"
            @click="onSend"
          >
            <Icon name="chat-send" :size="18" />
          </button>
        </template>
        <template v-if="!coldSession" #hint>
          <p class="chat-meta m-0 min-w-0 leading-snug [overflow-wrap:anywhere]" data-testid="review-composer-footer-hint">
            {{ gateFooterHint }}
          </p>
        </template>
        <template #footer>
          <button
            v-if="canPass"
            type="button"
            class="chat-action-confirm inline-flex h-9 shrink-0"
            data-testid="review-composer-pass"
            :disabled="passDisabled"
            :title="passTitle"
            @click="onConfirm"
          >
            <Icon name="check" :size="14" />
            {{ confirmButtonLabel }}
          </button>
        </template>
      </ComposerShell>
      <template v-if="!coldSession">
        <PendingSendQueuePanel
          v-if="gateQueued.length"
          panel-test-id="gate-react-queue"
          :items="gateQueued"
          :notice="queueNotice"
          :toast="queueToast"
          @cancel="(i) => emit('queue-cancel-item', i)"
          @edit="(i) => emit('queue-edit', i)"
          @reorder="(from, to) => emit('queue-reorder-indexes', from, to)"
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
/* typing-dots / outputting / caret live in GateReactStreamPanel */
[data-testid="review-composer-actions"] {
  width: 100%;
  max-width: 800px;
  margin-inline: auto;
  border-top-color: transparent;
}
</style>

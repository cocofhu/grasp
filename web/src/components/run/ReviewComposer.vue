<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import ClarifyChat from './ClarifyChat.vue'
import ComposerShell from './ComposerShell.vue'
import ParagraphInput from '../ui/ParagraphInput.vue'
import GateReactStreamPanel from './GateReactStreamPanel.vue'
import PendingSendQueuePanel, { type PendingQueueRow } from './PendingSendQueuePanel.vue'
import type { ClarifyTurn, ClarifyImage, ReactAnnotation, AcpEvent } from '@/lib/shared/types'
import { isGrasp } from '@/lib/shared/clarifyInteractive'
import AnnotationChip from './AnnotationChip.vue'
import PageControlStatus from './PageControlStatus.vue'
import type { PageControlState } from '@/lib/inbox/embedPageControl'

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
    runId?: string
    nodeId?: string
    iteration?: number
    turns?: ClarifyTurn[]
    nodeType?: string
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
    /** Adapter-only override for react nodes that own a final decision action. */
    forceConfirm?: boolean
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
    interrupted?: boolean
    /** ISO when turn completed normally — drives restrained「已完成」footnote. */
    streamCompletedAt?: string | null
    /** This user's preview-page control state; unset hides the line. */
    pageControl?: PageControlState
  }>(),
  {
    iteration: 1,
    turns: () => [],
    nodeType: '',
    done: false,
    active: true,
    canReject: true,
    canPass: true,
    rejecting: false,
    rejectError: null,
    coldSession: false,
    textOnly: false,
    passDisabled: false,
    forceConfirm: false,
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
    interrupted: false,
    streamCompletedAt: null,
  },
)

const emit = defineEmits<{
  (e: 'send', text: string, images: ClarifyImage[], annotations: ReactAnnotation[]): void
  (e: 'retry-last'): void
  (e: 'finish'): void
  (e: 'finish-abort'): void
  (e: 'cancel'): void
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
  discardLastQueued: () => void
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
  discardLastQueued: () => chatRef.value?.discardLastQueued(),
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
</script>

<template>
  <!-- Clarify / review: reuse ClarifyChat (chip + image + threshold already wired). -->
  <div
    v-if="mode === 'clarify' || mode === 'review'"
    class="flex h-full min-h-0 flex-col"
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
      :node-type="nodeType"
      :done="done"
      :active="active"
      :cold-session="coldSession"
      :finish-disabled="passDisabled"
      :force-confirm-flow="forceConfirm"
      :review-mode="mode === 'review'"
      :annotate-enabled="mode === 'clarify' || mode === 'review'"
      :hide-finish="!canPass || (mode === 'clarify' && !isGrasp(nodeType) && !forceConfirm)"
      :seed-human-text="seedHumanText"
      :seed-human-images="seedHumanImages"
      :send-label="mode === 'clarify' ? t('pages.reviewComposer.sendClarify') : undefined"
      :confirm-error="confirmError"
      :confirm-can-abort="confirmCanAbort"
      @send="(text, images, anns) => emit('send', text, images, anns)"
      @retry-last="emit('retry-last')"
      @finish="emit('finish')"
      @finish-abort="emit('finish-abort')"
      @cancel="emit('cancel')"
      @queue-remove="(itemId, index) => emit('queue-remove', itemId, index)"
      @queue-reorder="(itemIds) => emit('queue-reorder', itemIds)"
    />
  </div>

  <!-- Gate: local composer with review-semantics sticky actions (send + confirm). -->
  <div
    v-else
    class="flex h-full min-h-0 flex-col"
    data-testid="review-composer-gate"
    data-review-composer
  >
    <div class="min-h-0 flex-1 overflow-y-auto px-3 py-2 text-[12px] text-txt3">
      <slot name="gate-body">
        <template v-if="!coldSession">
          {{ t('pages.reviewComposer.gateHint') }}
        </template>
      </slot>
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
      <ComposerShell :show-chrome="!coldSession" :show-footer="true">
        <template v-if="!coldSession" #input>
          <ParagraphInput
            ref="paragraphRef"
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
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-line text-txt2 hover:border-line-strong disabled:opacity-50"
            data-testid="paragraph-input-attach"
            :disabled="rejecting || !canReject"
            :title="t('common.paragraphInput.addImage')"
            @click="paragraphRef?.pickFiles?.()"
          >
            <Icon name="paperclip" :size="16" />
          </button>
        </template>
        <template v-if="!coldSession" #toolbar-end>
          <button
            v-if="showGateCancel"
            type="button"
            class="inline-flex h-[30px] shrink-0 items-center gap-1 rounded-md border border-line bg-elevated px-2.5 text-xs font-semibold text-txt2"
            data-testid="gate-react-cancel"
            title="Cancel"
            @click="emit('cancel')"
          >
            Cancel
          </button>
          <button
            v-if="canReject"
            type="button"
            class="inline-flex h-[30px] shrink-0 items-center gap-1 rounded-md bg-accent px-2.5 text-xs font-semibold text-white hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50"
            data-testid="review-composer-send"
            :disabled="!canSubmitGate"
            @click="onSend"
          >
            <Icon name="arrow-left" :size="14" />
            {{ rejecting ? t('pages.gateApproval.reactRevise.sending') : sendButtonLabel }}
          </button>
        </template>
        <template v-if="!coldSession" #hint>
          <p class="m-0 min-w-0 text-[11px] leading-snug [overflow-wrap:anywhere]" data-testid="review-composer-footer-hint">
            {{ gateFooterHint }}
          </p>
        </template>
        <template #footer>
          <button
            v-if="canPass"
            type="button"
            class="inline-flex h-9 shrink-0 items-center gap-1 rounded-md bg-ok px-3.5 text-sm font-medium text-white hover:bg-ok/90 disabled:cursor-not-allowed disabled:opacity-50"
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
        <GateReactStreamPanel
          :thinking="thinking"
          :stream-text="streamText"
          :stream-thought="streamThought"
          :interrupted="interrupted"
          :completed-at="streamCompletedAt"
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
/* typing-dots / outputting / caret live in GateReactStreamPanel */
</style>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGateApprovalCtx } from './gateApprovalContext'
import Icon from '../../ui/Icon.vue'
import ParagraphInput from '../../ui/ParagraphInput.vue'
import ComposerShell from '../ComposerShell.vue'
import GateReactStreamPanel from '../GateReactStreamPanel.vue'
import PendingSendQueuePanel from '../PendingSendQueuePanel.vue'

const props = defineProps<{
  /** mobile: always touch-sized; content-fit: min-height only when isMobile */
  layout: 'mobile' | 'content-fit'
}>()

const { t } = useI18n()
const { s } = useGateApprovalCtx()

const paragraphRef = ref<{ pickFiles?: () => void } | null>(null)

/** The mobile drawer shares its height with help copy and the review history. */
const compact = computed(() => props.layout === 'mobile' || s.isMobile)

const showCancel = computed(
  () => s.canReactRevise && (s.reactThinking || s.reactQueued.length > 0),
)

const sendDisabled = computed(
  () => s.reactSending || (!s.hotRejectAllowEmpty && !s.canSubmitReact),
)

/** Draft, attachment, annotation, element shot, or the existing empty-send allowance. */
const hotHasSendable = computed(
  () =>
    s.hotRejectAllowEmpty ||
    s.reactText.trim().length > 0 ||
    s.reactImages.length > 0 ||
    s.reactAnnotations.length > 0 ||
    !!s.pickedElementImage?.data,
)

/** While busy with nothing to send, the stop icon occupies the send slot. */
const showSend = computed(() => {
  if (!s.showHotReject) return false
  if (showCancel.value && !hotHasSendable.value) return false
  return true
})
</script>

<template>
  <div v-if="s.reactError" class="mb-1.5 text-[11px] text-err">{{ s.reactError }}</div>
  <ComposerShell :compact="compact">
    <template #input>
      <ParagraphInput
        ref="paragraphRef"
        v-model:text="s.reactText"
        v-model:images="s.reactImages"
        embedded
        :compact="compact"
        :disabled="s.reactSending"
        :placeholder="t('pages.gateApproval.reactRevise.placeholder')"
      />
    </template>
    <template #toolbar-start>
      <button
        type="button"
        class="flex shrink-0 items-center justify-center rounded-md border border-line text-txt2 hover:border-line-strong disabled:opacity-50"
        :class="compact ? 'h-8 w-8' : 'h-10 w-10'"
        data-testid="paragraph-input-attach"
        :disabled="s.reactSending"
        :title="t('common.paragraphInput.addImage')"
        @click="paragraphRef?.pickFiles?.()"
      >
        <Icon name="paperclip" :size="16" />
      </button>
    </template>
    <template #toolbar-end>
      <button
        v-if="showCancel"
        type="button"
        class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-md border border-line bg-elevated text-txt2 hover:border-line-strong"
        data-testid="gate-react-cancel"
        title="Cancel"
        aria-label="Cancel"
        @click="s.cancelReactRevise"
      >
        <Icon name="stop" :size="12" />
      </button>
      <button
        v-if="showSend"
        type="button"
        class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-md bg-accent text-white hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50"
        data-testid="review-composer-send"
        :title="s.composerRejectLabel"
        :aria-label="s.composerRejectLabel"
        :disabled="sendDisabled"
        @click="s.sendHotReject"
      >
        <Icon name="send" :size="14" />
      </button>
    </template>
    <template #footer>
      <button
        type="button"
        class="inline-flex h-9 shrink-0 items-center justify-center rounded-md border border-line px-3 text-sm font-medium text-txt2 transition hover:bg-elevated disabled:cursor-not-allowed disabled:opacity-50"
        data-testid="review-record-issue"
        :disabled="!s.canRecordIssue"
        @click="s.recordFeedbackIssue"
      >
        {{ t('pages.gateApproval.reviewFeedback.record') }}
      </button>
      <button
        v-if="s.showHotPass"
        type="button"
        class="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md bg-ok px-3.5 text-sm font-medium text-white hover:bg-ok/90 disabled:cursor-not-allowed disabled:opacity-50"
        data-testid="review-composer-pass"
        :disabled="s.composerPassDisabled"
        :title="s.passAction ? s.actionButtonTitle(s.passAction.id) : ''"
        @click="s.onComposerPass"
      >
        <Icon name="check" :size="14" />
        {{
          s.actionSubmitting && s.resolved === s.passAction?.id
            ? t('pages.clarify.validating')
            : t('pages.clarify.confirmFlow')
        }}
      </button>
    </template>
  </ComposerShell>
  <PendingSendQueuePanel
    v-if="s.reactQueued.length"
    panel-test-id="gate-react-queue"
    :items="s.reactQueued"
    :notice="s.reactQueueNotice"
    :toast="s.reactQueueToast"
    @cancel="s.cancelReactQueuedItem"
    @edit="s.editReactQueuedItem"
    @reorder="s.reorderReactQueuedItems"
  />
  <GateReactStreamPanel
    :thinking="s.reactThinking"
    :stream-text="s.reactStreamText"
    :stream-thought="s.reactStreamThought"
    :stream-tools="s.reactStreamTools"
    :interrupted="s.reactInterrupted"
    :completed-at="s.reactStreamCompletedAt"
  />
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useAttrs } from 'vue'
import { useGateApprovalCtx } from './gateApprovalContext'
import ReviewComposer from '../ReviewComposer.vue'

defineOptions({ inheritAttrs: false })

const attrs = useAttrs()
const { t } = useI18n()
const { s } = useGateApprovalCtx()
</script>

<template>
  <ReviewComposer
    v-bind="attrs"
    mode="gate"
    v-model:draft="s.reactText"
    v-model:attachments="s.reactImages"
    v-model:annotations="s.reactAnnotations"
    :can-reject="s.showHotReject"
    :can-pass="s.showHotPass"
    :reject-allow-empty="s.hotRejectAllowEmpty"
    :rejecting="s.reactSending"
    :reject-error="s.reactError"
    :pass-disabled="s.composerPassDisabled"
    :pass-title="s.passAction ? s.actionButtonTitle(s.passAction.id) : ''"
    :pass-label="t('pages.clarify.confirmFlow')"
    :reject-label="s.composerRejectLabel"
    :queued="s.reactQueued"
    :queue-notice="s.reactQueueNotice"
    :queue-toast="s.reactQueueToast"
    :thinking="s.reactThinking"
    :stream-text="s.reactStreamText"
    :stream-thought="s.reactStreamThought"
    :stream-tools="s.reactStreamTools"
    :interrupted="s.reactInterrupted"
    :stream-completed-at="s.reactStreamCompletedAt"
    @send="s.onComposerReject"
    @finish="s.onComposerPass"
    @cancel="s.cancelReactRevise"
    @queue-edit="s.editReactQueuedItem"
    @queue-cancel-item="s.cancelReactQueuedItem"
    @queue-reorder-indexes="s.reorderReactQueuedItems"
  />
</template>

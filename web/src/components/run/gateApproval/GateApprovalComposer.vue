<script setup lang="ts">
import { computed, useAttrs } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGateApprovalCtx } from './gateApprovalContext'
import ReviewComposer from '../ReviewComposer.vue'

defineOptions({ inheritAttrs: false })

const attrs = useAttrs()
const { t } = useI18n()
const { s } = useGateApprovalCtx()
const producer = computed(() => (s.run?.nodes || []).find(node => node.id === s.gate.reactUpstreamNodeId))
const agentName = computed(() => String(producer.value?.config?.agent_profile || '').trim())
const producerConversation = computed(() => s.run?.clarifyByNode?.[s.gate.reactUpstreamNodeId || ''])
</script>

<template>
  <ReviewComposer
    v-bind="attrs"
    mode="gate"
    :run-id="s.gate.runId"
    :node-id="s.gate.nodeId"
    :iteration="s.gate.iteration ?? 1"
    :model-switch="!!agentName && !!s.gate.reactUpstreamNodeId"
    :agent-name="agentName"
    :model-node-id="s.gate.reactUpstreamNodeId"
    :model-iteration="s.gate.upstreamIteration || producerConversation?.iteration || 1"
    :current-configuration-id="producerConversation?.currentConfigurationId"
    :current-model-id="producerConversation?.currentModelId"
    :current-model-set="producerConversation?.currentModelSet"
    :reasoning-effort="producerConversation?.reasoningEffort"
    :model-notice="producerConversation?.modelNotice"
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
    :gate-turns="s.reactTranscriptTurns"
    @send="s.onComposerReject"
    @finish="s.onComposerPass"
    @cancel="s.cancelReactRevise"
    @queue-edit="s.editReactQueuedItem"
    @queue-cancel-item="s.cancelReactQueuedItem"
    @queue-reorder-indexes="s.reorderReactQueuedItems"
  />
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConversationWorkspace from '@/components/chat/ConversationWorkspace.vue'
import ConfirmFlowOverlay from '@/components/run/ConfirmFlowOverlay.vue'
import { provideConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'
import { provideConversationArtifacts } from '@/lib/chat/conversationArtifacts'
import { stageLinksFor } from '@/lib/run/stageLinks'

const props = withDefaults(defineProps<{
  /** Run + node + iteration (or public scene); resets a previous panel selection. */
  scopeKey?: string
  runId?: string
  /** GateApproval owns the success ceremony outside this shell. */
  hostConfirmFlow?: boolean
  /** Embedded page already hosts the product; only render its conversation. */
  chatOnly?: boolean
}>(), { scopeKey: 'review-conversation', hostConfirmFlow: true, chatOnly: false })

const { t } = useI18n()
const panelOpen = ref(false)
const confirmFlow = props.hostConfirmFlow ? provideConfirmFlowCeremony() : null
const confirmFlowPhase = confirmFlow?.phase
const confirmFlowReduce = confirmFlow?.reduceMotion
const confirmFlowToken = confirmFlow?.playToken

watch(() => props.scopeKey, () => { panelOpen.value = false }, { flush: 'sync' })
watch(() => props.chatOnly, (chatOnly) => { if (chatOnly) panelOpen.value = false })

provideConversationArtifacts({
  async open(target) {
    if (props.chatOnly || !props.runId || (target.runId && target.runId !== props.runId)) return
    const scope = props.scopeKey
    panelOpen.value = true
    await nextTick()
    if (scope !== props.scopeKey) return
    const stage = stageLinksFor(props.runId)
    if (target.preview) stage?.openPreview()
    else if (target.name) stage?.openArtifact(target.name)
  },
})

defineExpose({
  playConfirmCeremony: () => confirmFlow?.play() ?? Promise.resolve(),
  confirmFlowCeremony: confirmFlow,
})
</script>

<template>
  <div class="relative flex h-full min-h-0 min-w-0 w-full flex-1 overflow-hidden" data-testid="review-shell">
    <ConversationWorkspace
      v-model:open="panelOpen"
      :title="t('pages.runDetail.tabs.artifacts')"
      :scope-key="scopeKey"
      :panel-enabled="!chatOnly"
      :show-toolbar="!chatOnly"
      class="min-h-0 min-w-0 flex-1"
    >
      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden" data-testid="review-shell-sidebar">
        <slot name="sidebar" />
      </div>
      <template #panel>
        <section v-if="!chatOnly" class="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden" data-testid="review-shell-stage">
          <slot name="stage" />
        </section>
      </template>
      <template v-if="$slots.actions" #actions><slot name="actions" /></template>
    </ConversationWorkspace>
    <ConfirmFlowOverlay
      v-if="hostConfirmFlow && confirmFlowPhase"
      :phase="confirmFlowPhase"
      :reduce-motion="confirmFlowReduce"
      :play-token="confirmFlowToken"
    />
  </div>
</template>

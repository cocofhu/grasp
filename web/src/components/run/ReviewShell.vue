<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConversationWorkspace from '@/components/chat/ConversationWorkspace.vue'
import ConfirmFlowOverlay from '@/components/run/ConfirmFlowOverlay.vue'
import { provideConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'
import { ensureConversationDrawer, readArtifactPanelOpen, readRememberedPanelMode, writeArtifactPanelOpen } from '@/lib/chat/conversationDrawer'
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
  /** Conversation hosts the sandbox stage: resource and artifact icons share one drawer. */
  splitDrawer?: boolean
  /** Workspace shells with an artifact panel read and write the remembered drawer switch. */
  rememberArtifactPanel?: boolean
  /** Chat the remembered switch belongs to. Defaults to the run. */
  panelMemoryKey?: string
  /** Passed through to the conversation toolbar. Omit to keep 「对话」. */
  runName?: string
}>(), { scopeKey: 'review-conversation', hostConfirmFlow: true, chatOnly: false, splitDrawer: false, rememberArtifactPanel: false })

const { t } = useI18n()
const remembersPanel = () => props.rememberArtifactPanel && !props.chatOnly
const memoryKey = () => props.panelMemoryKey || props.runId
function rememberedPanel(): { open: boolean; mode: 'resources' | 'artifacts' | null } {
  const open = remembersPanel() && readArtifactPanelOpen(memoryKey())
  return { open, mode: open && props.splitDrawer ? readRememberedPanelMode(memoryKey()) : null }
}
const initialPanel = rememberedPanel()
const panelOpen = ref(initialPanel.open)
const panelMode = ref<'resources' | 'artifacts' | null>(initialPanel.mode)
const confirmFlow = props.hostConfirmFlow ? provideConfirmFlowCeremony() : null
const confirmFlowPhase = confirmFlow?.phase
const confirmFlowReduce = confirmFlow?.reduceMotion
const confirmFlowToken = confirmFlow?.playToken
const splitToolbar = () => props.splitDrawer && !props.chatOnly
let applyingSession = false

function applyRememberedPanel() {
  const next = rememberedPanel()
  applyingSession = true
  panelOpen.value = next.open
  panelMode.value = next.mode
  applyingSession = false
}

watch(() => props.scopeKey, () => {
  if (remembersPanel()) applyRememberedPanel()
  else {
    panelOpen.value = false
    panelMode.value = null
  }
  const entry = ensureConversationDrawer(props.runId)
  if (entry && splitToolbar()) entry.resetEpoch += 1
}, { flush: 'sync' })
watch(() => props.chatOnly, (chatOnly) => {
  if (!chatOnly) return
  applyingSession = true
  panelOpen.value = false
  panelMode.value = null
  applyingSession = false
})
watch([panelOpen, panelMode], ([open, mode], [prevOpen]) => {
  if (applyingSession || !remembersPanel()) return
  // A shell without split homes keeps the home a split shell last remembered.
  if (open) writeArtifactPanelOpen(true, props.splitDrawer ? (mode === 'resources' ? 'resources' : 'artifacts') : undefined, memoryKey())
  else if (prevOpen) writeArtifactPanelOpen(false, undefined, memoryKey())
}, { flush: 'sync' })
watch(
  () => [props.runId, props.splitDrawer, props.chatOnly, panelOpen.value, panelMode.value] as const,
  () => {
    const entry = ensureConversationDrawer(props.runId)
    if (!entry) return
    entry.split = splitToolbar()
    entry.mode = panelOpen.value ? (panelMode.value ?? 'artifacts') : null
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  const entry = ensureConversationDrawer(props.runId)
  if (!entry) return
  entry.split = false
  entry.mode = null
})

provideConversationArtifacts({
  async open(target) {
    if (props.chatOnly || !props.runId || (target.runId && target.runId !== props.runId)) return
    const scope = props.scopeKey
    if (splitToolbar()) panelMode.value = target.preview ? 'resources' : 'artifacts'
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
      v-model:mode="panelMode"
      :split-modes="splitDrawer && !chatOnly"
      :retain-open="rememberArtifactPanel && !chatOnly"
      :title="t('pages.runDetail.tabs.artifacts')"
      :run-name="runName"
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
      <template v-if="$slots.leading" #leading><slot name="leading" /></template>
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

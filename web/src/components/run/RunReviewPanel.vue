<script setup lang="ts">
import type { RestoreUnsent } from '@/lib/inbox/clarifyVisitEnded'
import type { ReferenceInput } from '@/lib/api/apiTypes'
import { takeoverTask } from '@/lib/run/takeoverTimeline'
/**
 * Run 详情「复审」面板壳：ReviewShell + 产物舞台 + ReviewComposer。
 */
import { computed, ref } from 'vue'
import { useConversationToolbarRunName } from '@/lib/inbox/conversationToolbarTitle'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ReviewComposer from '@/components/run/ReviewComposer.vue'
import type { ClarifyHandoff } from '@/components/run/ClarifyChat.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ReactConnectingState from '@/components/run/ReactConnectingState.vue'
import type {
  ClarifyImage,
  ClarifyTurn,
  NodeRun,
  NodeRunStatus,
  ReactAnnotation,
  Run,
  WFNode,
} from '@/lib/shared/types'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { isAppPreviewRemoteNode } from '@/lib/run/reactArtifactPreview'

const props = defineProps<{
  mobile: boolean
  node: WFNode
  nodeRun: NodeRun
  run: Run
  clarify: {
    nodeId: string
    iteration?: number
    turns: ClarifyTurn[]
    done: boolean
    previewArtifact?: string
    takeover?: boolean
    task?: string
    currentConfigurationId?: string
    currentModelId?: string
    currentModelSet?: boolean
    reasoningEffort?: string
    modelNotice?: string
  } | null
  draft: string
  attachments: ClarifyImage[]
  annotations: ReactAnnotation[]
  inputActive: boolean
  confirmError?: string | null
  confirmCanAbort?: boolean
  selStatus?: NodeRunStatus | string | null
  /** Same boot-gate three-state as the clarify panel. */
  handoff?: ClarifyHandoff | null
}>()

const emit = defineEmits<{
  'update:draft': [v: string]
  'update:attachments': [v: ClarifyImage[]]
  'update:annotations': [v: ReactAnnotation[]]
  send: [text: string, images: ClarifyImage[], annotations: ReactAnnotation[], clientMessageId?: string, references?: ReferenceInput[]]
  'retry-last': []
  finish: []
  'finish-abort': []
  cancel: []
  'queue-remove': [itemId: string | undefined, index: number]
  'queue-reorder': [itemIds: string[]]
  pick: [payload: AppPreviewPickPayload]
  stagedPick: [payload: AppPreviewPickPayload | null]
}>()

const reviewShellRef = ref<{
  playConfirmCeremony?: () => Promise<void>
} | null>(null)

const reviewChatRef = ref<{
  applyRequestRejected?: (frame: any) => void
  discardLastQueued?: (clientMessageId?: string) => void
  releaseConfirmPending?: () => void
  restoreUnsentQueued?: RestoreUnsent
  isSessionBusy?: () => boolean
  isChatReady?: () => boolean
  playConfirmCeremony?: () => Promise<void>
} | null>(null)

const remoteKind = computed(() => (isAppPreviewRemoteNode(props.node) ? 'app' : 'off'))
const conversationRunName = useConversationToolbarRunName(() => ({
  run: props.run,
  fallback: { runId: props.run.id, runTitle: props.run.title, workflowName: props.run.workflowName },
}))

defineExpose({
  applyRequestRejected: (frame: any) => reviewChatRef.value?.applyRequestRejected?.(frame),
  discardLastQueued: (clientMessageId?: string) => reviewChatRef.value?.discardLastQueued?.(clientMessageId),
  releaseConfirmPending: () => reviewChatRef.value?.releaseConfirmPending?.(),
  restoreUnsentQueued: (...args: Parameters<RestoreUnsent>) =>
    reviewChatRef.value?.restoreUnsentQueued?.(...args),
  isSessionBusy: () => !!reviewChatRef.value?.isSessionBusy?.(),
  isChatReady: () => !!reviewChatRef.value?.isChatReady?.(),
  playConfirmCeremony: async () => {
    // Prefer ReviewShell host — chat inject often misses slotted provide (plan g1.2).
    void reviewChatRef.value?.playConfirmCeremony?.()
    if (reviewShellRef.value?.playConfirmCeremony) {
      await reviewShellRef.value.playConfirmCeremony()
      return
    }
    await reviewChatRef.value?.playConfirmCeremony?.()
  },
})
</script>

<template>
  <ReviewShell
    :scope-key="`run-review:${run.id}:${clarify?.nodeId || node.id}:${clarify?.iteration ?? 1}`"
    :run-id="run.id"
    :run-name="conversationRunName"
    ref="reviewShellRef"
    class="h-full min-h-0"
    split-drawer
    remember-artifact-panel
  >
    <template #stage>
      <ReactConnectingState v-if="!clarify" mode="stage" hide-home-tab />
      <ReactArtifactStage
        v-else
        :artifacts="run.artifacts || []"
        :preview-artifact="clarify?.previewArtifact"
        :run-id="run.id"
        :run="run"
        :node-id="node.id"
        :node="node"
        :annotatable="inputActive"
        :remote-kind="remoteKind"
        probe-registered-preview
        @pick="emit('pick', $event)"
        @staged-pick="emit('stagedPick', $event)"
      />
    </template>
    <template #sidebar>
      <ReactConnectingState v-if="!clarify" />
      <ReviewComposer
        v-else
        ref="reviewChatRef"
        mode="review"
        :run-id="run.id"
        :node-id="clarify.nodeId"
        :iteration="clarify.iteration ?? 1"
        :draft="draft"
        :attachments="attachments"
        :annotations="annotations"
        :turns="clarify.turns ?? []"
        :done="clarify.done"
        :node-task="takeoverTask(clarify)"
        :active="inputActive"
        :model-switch="!!String(node.config?.agent_profile || '').trim()"
        :agent-name="String(node.config?.agent_profile || '')"
        :current-configuration-id="clarify?.currentConfigurationId"
        :current-model-id="clarify?.currentModelId"
        :current-model-set="clarify?.currentModelSet"
        :reasoning-effort="clarify?.reasoningEffort"
        :model-notice="clarify?.modelNotice"
        :confirm-error="confirmError"
        :confirm-can-abort="confirmCanAbort"
        :handoff="handoff ?? null"
        @update:draft="emit('update:draft', $event)"
        @update:attachments="emit('update:attachments', $event)"
        @update:annotations="emit('update:annotations', $event)"
        @send="(text: string, images: ClarifyImage[], anns: ReactAnnotation[], clientMessageId?: string, references?: ReferenceInput[]) => emit('send', text, images, anns, clientMessageId, ...(references?.length ? [references] : []))"
        @retry-last="emit('retry-last')"
        @finish="emit('finish')"
        @finish-abort="emit('finish-abort')"
        @cancel="emit('cancel')"
        @queue-remove="(itemId, index) => emit('queue-remove', itemId, index)"
        @queue-reorder="(itemIds) => emit('queue-reorder', itemIds)"
      />
    </template>
  </ReviewShell>
</template>

<script setup lang="ts">
/**
 * Run 详情「复审」面板壳：ReviewShell + 产物舞台 + ReviewComposer。
 */
import { computed, ref } from 'vue'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ReviewComposer from '@/components/run/ReviewComposer.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ReactConnectingState from '@/components/run/ReactConnectingState.vue'
import {
  REVIEW_SIDEBAR,
  REVIEW_SHELL_WIDTH_KEY_REVIEW,
} from '@/lib/inbox/reviewLayoutBudget'
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
  } | null
  draft: string
  attachments: ClarifyImage[]
  annotations: ReactAnnotation[]
  inputActive: boolean
  confirmError?: string | null
  selStatus?: NodeRunStatus | string | null
}>()

const emit = defineEmits<{
  'update:draft': [v: string]
  'update:attachments': [v: ClarifyImage[]]
  'update:annotations': [v: ReactAnnotation[]]
  send: [text: string, images: ClarifyImage[], annotations: ReactAnnotation[]]
  'retry-last': []
  finish: []
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
  applyReviewFrame?: (frame: any) => boolean | void
  applyAcpEvents?: (events: any[] | undefined, nodeId?: string) => boolean | void
  discardLastQueued?: () => void
  isSessionBusy?: () => boolean
  isChatReady?: () => boolean
  playConfirmCeremony?: () => Promise<void>
} | null>(null)

const remoteKind = computed(() => (props.node.type === 'app_preview' ? 'app' : 'off'))

defineExpose({
  applyReviewFrame: (frame: any) => reviewChatRef.value?.applyReviewFrame?.(frame),
  applyAcpEvents: (events: any[] | undefined, nodeId?: string) =>
    reviewChatRef.value?.applyAcpEvents?.(events, nodeId),
  discardLastQueued: () => reviewChatRef.value?.discardLastQueued?.(),
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
    ref="reviewShellRef"
    class="h-full min-h-0"
    :mobile="mobile"
    :sidebar-width="REVIEW_SIDEBAR"
    :storage-key="REVIEW_SHELL_WIDTH_KEY_REVIEW"
    card-panes
  >
    <template #stage>
      <ReactConnectingState v-if="!clarify" mode="stage" />
      <ReactArtifactStage
        v-else
        :artifacts="run.artifacts || []"
        :preview-artifact="clarify?.previewArtifact"
        :run-id="run.id"
        :run="run"
        :node-id="node.id"
        :node-type="node.type"
        :annotatable="inputActive"
        :remote-kind="remoteKind"
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
        :active="inputActive"
        :confirm-error="confirmError"
        @update:draft="emit('update:draft', $event)"
        @update:attachments="emit('update:attachments', $event)"
        @update:annotations="emit('update:annotations', $event)"
        @send="(text: string, images: ClarifyImage[], anns: ReactAnnotation[]) => emit('send', text, images, anns)"
        @retry-last="emit('retry-last')"
        @finish="emit('finish')"
        @cancel="emit('cancel')"
        @queue-remove="(itemId, index) => emit('queue-remove', itemId, index)"
        @queue-reorder="(itemIds) => emit('queue-reorder', itemIds)"
      />
    </template>
  </ReviewShell>
</template>

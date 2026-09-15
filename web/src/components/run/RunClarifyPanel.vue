<script setup lang="ts">
import { isGrasp } from '@/lib/shared/clarifyInteractive'
/**
 * Run 详情「澄清」面板壳：OpenDesign 产物舞台 + ReAct 聊天。
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import ClarifyChat from '@/components/run/ClarifyChat.vue'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ReactConnectingState from '@/components/run/ReactConnectingState.vue'
import {
  REVIEW_SIDEBAR,
  REVIEW_SHELL_WIDTH_KEY_CLARIFY,
} from '@/lib/inbox/reviewLayoutBudget'
import { addClarifyAnnotation } from '@/lib/inbox/useClarifyDraft'
import { useToast } from '@/lib/composables/useToast'
import { previewPickLabel, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import type {
  ClarifyImage,
  ClarifyTurn,
  NodeRunStatus,
  ReactAnnotation,
  Run,
} from '@/lib/shared/types'

const props = defineProps<{
  sandboxFailed: boolean
  nodeLabel: string
  nodeId: string
  nodeError?: string | null
  clarify: {
    nodeId: string
    iteration?: number
    turns: ClarifyTurn[]
    done: boolean
    previewArtifact?: string
  } | null
  runId: string
  run?: Run | null
  mobile?: boolean
  draft: string
  attachments: ClarifyImage[]
  inputActive: boolean
  selStatus?: NodeRunStatus | string | null
  /** Rejected「确认并流转」reason, surfaced so the chat can release its spinner. */
  confirmError?: string | null
}>()

const emit = defineEmits<{
  'update:draft': [v: string]
  'update:attachments': [v: ClarifyImage[]]
  send: [text: string, images: ClarifyImage[], annotations: ReactAnnotation[]]
  'retry-last': []
  finish: []
  cancel: []
  'queue-remove': [itemId: string | undefined, index: number]
  'queue-reorder': [itemIds: string[]]
}>()

const annotations = defineModel<ReactAnnotation[]>('annotations', { default: () => [] })

const { t } = useI18n()
const toast = useToast()

const reviewChatRef = ref<{
  applyReviewFrame?: (frame: any) => boolean | void
  applyAcpEvents?: (events: any[] | undefined, nodeId?: string) => boolean | void
  discardLastQueued?: () => void
  isSessionBusy?: () => boolean
  isChatReady?: () => boolean
} | null>(null)

const artifacts = computed(() => props.run?.artifacts || [])
const previewArtifact = computed(() => props.clarify?.previewArtifact || '')
const nodeType = computed(() => props.run?.nodes?.find((n) => n.id === props.nodeId)?.type || '')
// Approve defaults to off; ReactArtifactStage silently probes previews and upgrades to app when registered.
const remoteKind = computed(() => (isGrasp(nodeType.value) ? 'off' : 'sandbox'))

function onRemotePick(payload: AppPreviewPickPayload) {
  if (!props.inputActive) return
  const url = (payload.url || '').trim()
  const result = addClarifyAnnotation(props.runId, props.nodeId, {
    selector: payload.selector,
    url: url || undefined,
    label: previewPickLabel(url, payload.selector, payload.tagName),
  })
  if (result === 'duplicate') toast.warn(t('pages.reviewComposer.alreadyAdded'))
}

defineExpose({
  applyReviewFrame: (frame: any) => reviewChatRef.value?.applyReviewFrame?.(frame),
  applyAcpEvents: (events: any[] | undefined, nodeId?: string) =>
    reviewChatRef.value?.applyAcpEvents?.(events, nodeId),
  discardLastQueued: () => reviewChatRef.value?.discardLastQueued?.(),
  isSessionBusy: () => !!reviewChatRef.value?.isSessionBusy?.(),
  isChatReady: () => !!reviewChatRef.value?.isChatReady?.(),
})
</script>

<template>
  <div v-if="sandboxFailed" class="scroll-area flex h-full flex-col overflow-y-auto p-4">
    <div class="mb-3 flex items-center gap-2.5">
      <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-n-clarify/15 text-n-clarify">
        <Icon name="chat" :size="16" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="text-[14px] font-semibold text-txt [overflow-wrap:anywhere]">{{ nodeLabel }}</div>
        <div class="text-[11px] text-txt3 [overflow-wrap:anywhere]">{{ t('pages.runDetail.clarifyFailed.subtitle', { id: nodeId }) }}</div>
      </div>
      <StatusPill status="failed" />
    </div>
    <div class="rounded-lg mb-3 border border-err/40 bg-err/5 p-3.5">
      <div class="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-err">
        <Icon name="alert" :size="14" />
        {{ t('pages.runDetail.clarifyFailed.errorTitle') }}
      </div>
      <pre class="min-w-0 max-w-full overflow-x-auto whitespace-pre font-mono text-[11px] leading-relaxed text-txt2">{{ nodeError }}</pre>
    </div>
    <p class="text-[11px] text-txt3">{{ t('pages.runDetail.clarifyFailed.hint') }}</p>
  </div>
  <ReviewShell
    v-else
    class="h-full min-h-0"
    :mobile="mobile"
    :sidebar-width="REVIEW_SIDEBAR"
    :storage-key="REVIEW_SHELL_WIDTH_KEY_CLARIFY"
  >
    <template #stage>
      <ReactConnectingState v-if="!clarify" mode="stage" />
      <ReactArtifactStage
        v-else
        :artifacts="artifacts"
        :preview-artifact="previewArtifact"
        :run-id="runId"
        :run="run || undefined"
        :node-id="nodeId"
        :node-type="nodeType"
        :annotatable="inputActive"
        :remote-kind="remoteKind"
        @pick="onRemotePick"
      />
    </template>
    <template #sidebar>
      <ReactConnectingState v-if="!clarify" :show-confirm="false" />
      <ClarifyChat
        v-else
        ref="reviewChatRef"
        :run-id="runId"
        :node-id="clarify.nodeId"
        :iteration="clarify.iteration ?? 1"
        :draft="draft"
        :attachments="attachments"
        :turns="clarify.turns ?? []"
        :node-type="nodeType"
        :done="clarify.done"
        :active="inputActive"
        :confirm-error="confirmError"
        annotate-enabled
        v-model:annotations="annotations"
        @update:draft="emit('update:draft', $event)"
        @update:attachments="emit('update:attachments', $event)"
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

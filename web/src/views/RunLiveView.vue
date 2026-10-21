<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ReviewComposer from '@/components/run/ReviewComposer.vue'
import ArtifactLoadingPane from '@/components/run/ArtifactLoadingPane.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import AppInlineError from '@/components/ui/AppInlineError.vue'
import { findGraphNode } from '@/lib/shared/clarifyInteractive'
import { useRunLive, type LiveChatSurface } from '@/lib/run/useRunLive'
import { useI18n } from 'vue-i18n'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

/**
 * Live view of a run, opened from the sidebar「最近」row. Same surface as the
 * inbox chat (ReviewShell + ReviewComposer/ClarifyChat). An auto node streams
 * read-only (the only action is Stop → takeover). The page never navigates by
 * itself: when the node finishes, the conversation continues in place with the
 * run's next node (earlier nodes folded above, a separator, the node's task as
 * the first User Message), streaming it if auto or as a chat you can answer
 * and 确认并流转 right here. A gate or the end of the run ends it in place.
 */
const route = useRoute()
const { t } = useI18n()
const runId = computed(() => String(route.params.id || ''))
const requestedNode = computed(() => {
  const q = route.query.node
  return String((Array.isArray(q) ? q[0] : q) || '')
})
const chatRef = ref<LiveChatSurface | null>(null)
const {
  run,
  loading,
  loadError,
  nodeId,
  iteration,
  stopping,
  ended,
  endState,
  task,
  mode,
  chatKind,
  nodeLabel,
  priorSegments,
  turns,
  handoff,
  composerLocked,
  confirmError,
  takeover,
  send,
  confirm,
  reload,
} = useRunLive(runId, requestedNode, chatRef)
/**
 * Where the run is when the page ended in place, in the app's existing words:
 * a gate waiting 「X」人工审批 · 待审批 (the gate row title + the 待审批 label),
 * or the run's end 运行已完成 / 运行失败 / 运行已取消 (the /gates follow row).
 */
const doneNote = computed(() => {
  const e = endState.value
  if (!e) return null
  if (e.kind === 'gate') return { text: `${t('pages.clarify.segmentGate', { name: e.label })} · ${t('nav.pendingApprovals')}`, tone: 'wait' as const }
  const st = e.status.toLowerCase()
  if (st === 'failed') return { text: t('pages.clarify.followEndedFailed'), tone: 'muted' as const }
  if (st === 'cancelled' || st === 'canceled') return { text: t('pages.clarify.followEndedCancelled'), tone: 'muted' as const }
  return { text: t('pages.clarify.followEnded'), tone: 'ok' as const }
})
const draft = ref('')
const attachments = ref<ClarifyImage[]>([])
const annotations = ref<ReactAnnotation[]>([])
function onSend(text: string, images: ClarifyImage[], anns: ReactAnnotation[]) {
  draft.value = ''
  attachments.value = []
  annotations.value = []
  void send(text, images, anns)
}
const stageNode = computed(() => findGraphNode(run.value?.nodes, nodeId.value) || null)
const surfaceKey = computed(() => `${runId.value}:${nodeId.value}:${iteration.value}`)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col" data-testid="run-live-view">
    <div class="flex min-h-0 flex-1 items-stretch">
      <div
        class="relative flex h-full min-h-0 min-w-0 flex-1 flex-col"
        data-testid="run-live-detail"
        :data-run-id="runId"
        :data-node-id="nodeId"
      >
        <div class="relative flex h-full min-h-0 w-full flex-col overflow-hidden bg-surface">
          <div
            v-if="loadError"
            class="card flex min-h-0 flex-1 flex-col items-stretch justify-center overflow-auto p-4"
          >
            <AppInlineError :title="t('pages.runLive.loadFailed')" :message="''" retry-testid="run-live-retry" @retry="reload" />
          </div>
          <div v-else class="flex min-h-0 flex-1 flex-col overflow-hidden">
            <ArtifactLoadingPane v-if="loading && !nodeId" message-key="pages.runLive.loading" />
            <ReviewShell
              v-else
              :key="surfaceKey"
              :scope-key="`live:${surfaceKey}`"
              :run-id="runId"
              class="min-h-0 flex-1"
              :host-confirm-flow="false"
            >
              <template #stage>
                <ReactArtifactStage
                  :artifacts="run?.artifacts || []"
                  :run-id="runId"
                  :run="run || undefined"
                  :node-id="nodeId"
                  :node="stageNode"
                />
              </template>
              <template #sidebar>
                <ReviewComposer
                  ref="chatRef"
                  :mode="mode === 'chat' ? chatKind : 'review'"
                  :run-id="runId"
                  :node-id="nodeId"
                  :iteration="iteration"
                  v-model:draft="draft"
                  v-model:attachments="attachments"
                  v-model:annotations="annotations"
                  :turns="turns"
                  :done="ended"
                  :active="true"
                  :can-pass="mode === 'chat'"
                  :observe="mode === 'observe'"
                  :stopping="stopping"
                  :node-task="task"
                  :prior-segments="priorSegments"
                  :node-label="nodeLabel || nodeId"
                  :handoff="handoff"
                  :composer-locked="composerLocked"
                  :confirm-error="confirmError"
                  :done-note="doneNote"
                  @takeover="takeover"
                  @send="onSend"
                  @finish="confirm"
                />
              </template>
            </ReviewShell>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

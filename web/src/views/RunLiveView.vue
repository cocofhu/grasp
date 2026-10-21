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

/**
 * Read-only stream of an auto node, opened from the sidebar「最近」row. Same
 * surface as the inbox chat (ReviewShell + ReviewComposer/ClarifyChat); the
 * composer is disabled and its only action is Stop (takeover). It opens with
 * the node task (server currentNode.task / node events task) when sent, and
 * stays on this page once the node finishes (shown as done, no navigation).
 */
const route = useRoute()
const { t } = useI18n()
const runId = computed(() => String(route.params.id || ''))
const requestedNode = computed(() => {
  const q = route.query.node
  return String((Array.isArray(q) ? q[0] : q) || '')
})
const chatRef = ref<LiveChatSurface | null>(null)
const { run, loading, loadError, nodeId, iteration, stopping, ended, task, takeover, reload } = useRunLive(runId, requestedNode, chatRef)
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
                  mode="review"
                  :run-id="runId"
                  :node-id="nodeId"
                  :iteration="iteration"
                  :turns="[]"
                  :done="ended"
                  :active="true"
                  :can-pass="false"
                  observe
                  :stopping="stopping"
                  :node-task="task"
                  @takeover="takeover"
                />
              </template>
            </ReviewShell>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

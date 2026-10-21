<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import { NODE_DEFS, nodeColor } from '@/data/nodeRegistry'
import { resolveNodeDisplayLabelFromNode } from '@/lib/run/resolveNodeDisplayLabel'
import type { NodeRunStatus, Run } from '@/lib/shared/types'

const props = defineProps<{
  run: Run | null | undefined
  currentNodeId?: string
}>()

const { t } = useI18n()

const DOT: Record<string, string> = {
  running: 'bg-info',
  waiting_human: 'bg-warn',
  completed: 'bg-ok',
  skipped: 'bg-txt3',
  failed: 'bg-err',
  cancelled: 'bg-txt3',
  pending: 'bg-line',
}

function latestStatus(nodeId: string): NodeRunStatus {
  const run = props.run
  if (!run) return 'pending'
  return run.nodeRuns?.[nodeId]?.status || run.nodeExecutions?.[nodeId]?.at(-1)?.status || 'pending'
}

/** Empty status maps are "not loaded yet", not a run where every node is pending. */
function hasRecordedStatus(run: Run | null | undefined): boolean {
  if (!run) return false
  if (Object.values(run.nodeRuns || {}).some((row) => row?.status)) return true
  return Object.values(run.nodeExecutions || {}).some((list) => list?.some((row) => row?.status))
}

function iconOf(type: string) {
  return NODE_DEFS[type as keyof typeof NODE_DEFS]?.icon || 'agent'
}

const rows = computed(() => {
  const nodes = props.run?.nodes
  if (!nodes?.length || !hasRecordedStatus(props.run)) return []
  const current = String(props.currentNodeId || '').trim()
  return nodes.map((node) => ({
    id: node.id,
    label: resolveNodeDisplayLabelFromNode(node, t),
    type: node.type,
    status: latestStatus(node.id),
    current: !!current && node.id === current,
    icon: iconOf(node.type),
    color: nodeColor(node.type),
    tint: nodeColor(node.type, 0.16),
  }))
})
</script>

<template>
  <section v-if="rows.length" class="stage-workflow" data-testid="stage-workflow">
    <p class="mb-2 text-[12px] font-medium text-txt3" data-testid="stage-workflow-heading">
      {{ t('pages.reactArtifactStage.workflowHeading') }}
    </p>
    <ol class="relative m-0 min-w-0 max-w-full list-none space-y-2 border-l border-line pl-6" :aria-label="t('pages.reactArtifactStage.workflowHeading')">
      <li v-for="row in rows" :key="row.id" class="relative min-w-0 max-w-full">
        <span
          class="stage-workflow-dot"
          :class="DOT[row.status] || DOT.pending"
          data-testid="stage-workflow-dot"
          :data-status="row.status"
          aria-hidden="true"
        />
        <div
          class="flex min-w-0 cursor-default items-start gap-2.5 rounded-xl border bg-surface px-3 py-2.5"
          :class="row.current ? 'border-accent/60 ring-1 ring-accent/40' : 'border-line'"
          data-testid="stage-workflow-node"
          :data-node-id="row.id"
          :data-current="row.current ? 'true' : 'false'"
          :data-status="row.status"
        >
          <span
            class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg"
            :style="{ background: row.tint, color: row.color }"
            data-testid="stage-workflow-mark"
          >
            <Icon :name="row.icon" :size="15" />
          </span>
          <div
            class="min-w-0 flex-1 pt-px text-[13px] font-semibold text-txt [overflow-wrap:anywhere]"
            data-testid="stage-workflow-label"
          >
            {{ row.label }}
            <span
              v-if="row.current"
              class="ml-1.5 inline-block rounded-full bg-accent-dim px-1.5 align-middle text-[10px] font-bold leading-4 text-accent-2"
              data-testid="stage-workflow-current"
            >{{ t('pages.reactArtifactStage.currentMark') }}</span>
          </div>
          <StatusPill :status="row.status" size="sm" class="mt-0.5 shrink-0" />
        </div>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.stage-workflow {
  max-width: 100%;
  margin-bottom: 16px;
  overflow-x: hidden;
}
.stage-workflow-dot {
  position: absolute;
  left: -31px;
  top: 18px;
  width: 10px;
  height: 10px;
  border-radius: 999px;
  box-shadow: 0 0 0 4px var(--ra-bg, rgb(var(--c-base)));
}
</style>

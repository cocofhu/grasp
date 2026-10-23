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
    <p class="stage-workflow-heading mb-2 px-1 text-[12px] font-medium" data-testid="stage-workflow-heading">
      {{ t('pages.reactArtifactStage.workflowHeading') }}
    </p>
    <ol class="stage-workflow-list" :aria-label="t('pages.reactArtifactStage.workflowHeading')">
      <li
        v-for="row in rows"
        :key="row.id"
        class="stage-workflow-step"
        :class="{ 'is-done': row.status === 'completed' }"
        :aria-current="row.current ? 'step' : undefined"
      >
        <div
          class="stage-workflow-node"
          :class="{ 'is-current': row.current, 'is-pending': row.status === 'pending' && !row.current }"
          data-testid="stage-workflow-node"
          :data-node-id="row.id"
          :data-current="row.current ? 'true' : 'false'"
          :data-status="row.status"
        >
          <span
            class="stage-workflow-mark"
            :style="{ background: row.tint, color: row.color }"
            data-testid="stage-workflow-mark"
          >
            <Icon :name="row.icon" :size="14" />
          </span>
          <div
            class="stage-workflow-label min-w-0 flex-1 text-[13px] leading-5 [overflow-wrap:anywhere]"
            data-testid="stage-workflow-label"
          >
            {{ row.label }}
            <span
              v-if="row.current"
              class="stage-workflow-current ml-1.5 inline-block rounded-[4px] px-[5px] py-[3px] align-middle text-[10px] font-semibold leading-none"
              data-testid="stage-workflow-current"
            >{{ t('pages.reactArtifactStage.currentMark') }}</span>
          </div>
          <StatusPill :status="row.status" size="sm" class="shrink-0" />
        </div>
      </li>
    </ol>
  </section>
</template>

<style scoped>
/* Inherits the artifact stage tokens (--ra-*) so the list matches the resource and artifact cards; fallbacks cover standalone use. */
.stage-workflow {
  --wf-surface: var(--ra-surface, rgb(var(--c-surface)));
  --wf-line: var(--ra-line, rgb(var(--c-line)));
  --wf-ink: var(--ra-ink, rgb(var(--c-txt)));
  --wf-muted: var(--ra-muted, rgb(var(--c-txt3)));
  --wf-accent: var(--ra-accent, rgb(var(--c-accent)));
  --wf-green: var(--ra-green, rgb(var(--c-ok)));
  max-width: 100%;
  margin-bottom: 16px;
}
.stage-workflow-heading { color: var(--wf-muted); }
.stage-workflow-list {
  min-width: 0;
  max-width: 100%;
  margin: 0;
  padding: 4px;
  list-style: none;
  border: 1px solid var(--wf-line);
  border-radius: 8px;
  background: var(--wf-surface);
  box-shadow: inset 0 1px 0 var(--ra-specular, transparent), 0 2px 3px var(--ra-shadow, transparent);
}
/* The node marks are the timeline: a short connector joins each mark to the next one. */
.stage-workflow-step {
  position: relative;
  min-width: 0;
}
.stage-workflow-step:not(:last-child)::after {
  content: '';
  position: absolute;
  left: 22px;
  top: 34px;
  bottom: -6px;
  width: 2px;
  border-radius: 1px;
  background: var(--wf-line);
}
.stage-workflow-step.is-done:not(:last-child)::after { background: color-mix(in srgb, var(--wf-green) 45%, transparent); }
.stage-workflow-node {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
  cursor: default;
  padding: 8px 10px;
  border-radius: 6px;
}
.stage-workflow-mark {
  display: inline-flex;
  width: 24px;
  height: 24px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
}
.stage-workflow-label {
  font-weight: 500;
  color: var(--wf-ink);
}
.stage-workflow-node.is-current {
  background: color-mix(in srgb, var(--wf-accent) 8%, transparent);
}
/* Same chip as the cards' kind badge. */
.stage-workflow-current {
  border: 1px solid color-mix(in srgb, var(--wf-accent) 9%, transparent);
  color: var(--wf-accent);
  background: color-mix(in srgb, var(--wf-accent) 9%, var(--wf-surface));
}
.stage-workflow-node.is-pending .stage-workflow-label { color: var(--wf-muted); font-weight: 400; }
.stage-workflow-node.is-pending .stage-workflow-mark { opacity: .55; }
</style>

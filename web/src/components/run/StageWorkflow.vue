<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { resolveNodeDisplayLabelFromNode } from '@/lib/run/resolveNodeDisplayLabel'
import type { Run } from '@/lib/shared/types'

const props = defineProps<{
  run: Run | null | undefined
  currentNodeId?: string
}>()

const { t } = useI18n()

const STATUS_LABELS = new Set([
  'running',
  'queued',
  'pending',
  'waiting_human',
  'completed',
  'success',
  'failed',
  'cancelled',
  'cancelling',
  'skipped',
])

function latestStatus(nodeId: string): string {
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

function padPosition(value: number, total: number): string {
  const width = Math.max(2, String(Math.max(total, 0)).length)
  return String(value).padStart(width, '0')
}

function glyphFor(status: string): { name: string; busy: boolean; tone: 'run' | 'hold' | 'err' | 'ok' | 'quiet' } {
  if (status === 'running') return { name: 'arc-spinner', busy: true, tone: 'run' }
  if (status === 'waiting_human') return { name: 'user-hold', busy: false, tone: 'hold' }
  if (status === 'failed') return { name: 'alert', busy: false, tone: 'err' }
  if (status === 'completed' || status === 'success') return { name: 'check-draw', busy: false, tone: 'ok' }
  return { name: 'quiet-dot', busy: false, tone: 'quiet' }
}

const rows = computed(() => {
  const nodes = props.run?.nodes
  if (!nodes?.length || !hasRecordedStatus(props.run)) return []
  const current = String(props.currentNodeId || '').trim()
  const total = nodes.length
  return nodes.map((node, index) => {
    const status = latestStatus(node.id)
    const isCurrent = !!current && node.id === current
    const done = status === 'completed' || status === 'success'
    const glyph = glyphFor(status)
    const showIndex = !isCurrent && !done
    const labelKey = STATUS_LABELS.has(status) ? status : 'pending'
    return {
      id: node.id,
      label: resolveNodeDisplayLabelFromNode(node, t),
      status,
      current: isCurrent,
      done,
      glyph,
      showIndex,
      indexText: padPosition(index + 1, total),
      indexTone: status === 'failed' ? 'err' as const : 'quiet' as const,
      quietName: !isCurrent && glyph.tone === 'quiet',
      statusLabel: t(`common.status.${labelKey}`),
    }
  })
})

const progressLabel = computed(() => t('pages.reactArtifactStage.progressCount', {
  done: rows.value.filter((row) => row.done).length,
  total: rows.value.length,
}))

const indexLabel = computed(() => {
  const total = rows.value.length
  const position = rows.value.findIndex((row) => row.current)
  if (position < 0 || !total) return ''
  return `${padPosition(position + 1, total)} / ${padPosition(total, total)}`
})
</script>

<template>
  <section v-if="rows.length" class="stage-workflow" data-testid="stage-workflow">
    <div class="stage-workflow-clip">
      <div class="stage-workflow-head">
        <p class="stage-workflow-heading" data-testid="stage-workflow-heading">
          {{ t('pages.reactArtifactStage.workflowHeading') }}
        </p>
        <span class="stage-workflow-progress" data-testid="stage-workflow-progress">{{ progressLabel }}</span>
      </div>
      <p v-if="indexLabel" class="stage-workflow-index" data-testid="stage-workflow-index">{{ indexLabel }}</p>
      <ol class="stage-workflow-list" :aria-label="t('pages.reactArtifactStage.workflowHeading')">
        <li
          v-for="(row, index) in rows"
          :key="row.id"
          class="stage-workflow-step cursor-default"
          :class="{ 'is-done': row.done }"
          :style="{ '--wf-i': `${index}` }"
          data-icon-motion-host
        >
          <div
            class="stage-workflow-node"
            :class="{ 'is-current': row.current, 'is-quiet': row.quietName }"
            data-testid="stage-workflow-node"
            :data-node-id="row.id"
            :data-current="row.current ? 'true' : 'false'"
            :data-status="row.status"
            :aria-current="row.current ? 'step' : undefined"
          >
            <span
              class="stage-workflow-mark"
              :class="[row.showIndex ? 'is-index' : 'is-icon', `is-${row.showIndex ? row.indexTone : row.glyph.tone}`]"
              data-testid="stage-workflow-mark"
              :data-mark="row.showIndex ? 'index' : 'icon'"
              :data-glyph="row.showIndex ? undefined : row.glyph.name"
              :data-digits="row.showIndex ? String(row.indexText.length) : undefined"
            >
              <Icon v-if="!row.showIndex" :name="row.glyph.name" :size="14" :busy="row.glyph.busy" />
              <template v-else>{{ row.indexText }}</template>
            </span>
            <div
              class="stage-workflow-label min-w-0 flex-1 text-[13px] leading-5 [overflow-wrap:anywhere]"
              data-testid="stage-workflow-label"
            >
              {{ row.label }}
              <span
                v-if="row.current"
                class="stage-workflow-current"
                data-testid="stage-workflow-current"
              >{{ t('pages.reactArtifactStage.currentMark') }}</span>
            </div>
            <span
              class="stage-workflow-status"
              :class="`is-${row.glyph.tone}`"
              data-testid="stage-workflow-status"
            >
              <Icon :name="row.glyph.name" :size="14" :busy="row.glyph.busy" />
              {{ row.statusLabel }}
            </span>
          </div>
        </li>
      </ol>
    </div>
  </section>
</template>

<style scoped>
/* Inherits the artifact stage tokens (--ra-*) so the list matches the resource and artifact cards; fallbacks cover standalone use. */
.stage-workflow {
  --wf-surface: var(--ra-surface, rgb(var(--c-surface)));
  --wf-elevated: rgb(var(--c-elevated));
  --wf-line: var(--ra-line, rgb(var(--c-line)));
  --wf-ink: var(--ra-ink, rgb(var(--c-txt)));
  --wf-muted: var(--ra-muted, rgb(var(--c-txt3)));
  --wf-ok: rgb(var(--c-ok));
  --wf-warn: rgb(var(--c-warn));
  --wf-err: rgb(var(--c-err));
  /* Same indigo as the tool-running arc, not the artifact brand purple. */
  --wf-run: rgb(var(--c-accent-2));
  display: grid;
  grid-template-rows: 1fr;
  max-width: 100%;
  margin-bottom: 16px;
  /* Plays once per mount. Status updates patch the same nodes and do not restart it. */
  animation: stage-workflow-expand 380ms cubic-bezier(.22, 1, .36, 1) both;
}
.stage-workflow-clip { min-height: 0; overflow: hidden; }
.stage-workflow-head {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin: 2px 4px 8px;
}
.stage-workflow-heading {
  min-width: 0;
  flex: 1;
  margin: 0;
  color: var(--wf-ink);
  font-size: 13px;
  font-weight: 500;
  line-height: 20px;
}
.stage-workflow-progress,
.stage-workflow-index {
  color: var(--wf-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  line-height: 20px;
  white-space: nowrap;
}
.stage-workflow-progress { flex: none; }
.stage-workflow-index {
  margin: 0 4px 10px;
  text-align: right;
}
.stage-workflow-list {
  min-width: 0;
  max-width: 100%;
  margin: 0;
  padding: 0;
  list-style: none;
  border: 0;
  background: transparent;
  box-shadow: none;
}
.stage-workflow-step {
  position: relative;
  min-width: 0;
  animation: stage-workflow-step-in 220ms cubic-bezier(.22, 1, .36, 1) both;
  animation-delay: calc(min(var(--wf-i), 5) * 28ms);
}
.stage-workflow-step,
.stage-workflow-step * { cursor: default; }
/* The line hangs from this step's node to the next one, including the current step. */
.stage-workflow-step:not(:last-child)::after {
  content: '';
  position: absolute;
  left: 20px;
  top: 33px;
  bottom: -13px;
  z-index: 0;
  width: 2px;
  border-radius: 1px;
  background: var(--wf-line);
}
.stage-workflow-step.is-done:not(:last-child)::after {
  background: color-mix(in srgb, var(--wf-ok) 72%, transparent);
}
.stage-workflow-node {
  display: flex;
  min-width: 0;
  min-height: 46px;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 10px;
  border-radius: 8px;
  background: transparent;
  box-shadow: none;
}
.stage-workflow-node.is-current { background: color-mix(in srgb, var(--wf-ink) 6%, transparent); }
/* Hover replays the icon only. The row fill, shadow, and label stay put. */
.stage-workflow-node:hover,
.stage-workflow-node.is-current:hover {
  box-shadow: none;
}
.stage-workflow-node:hover { background: transparent; }
.stage-workflow-node.is-current:hover { background: color-mix(in srgb, var(--wf-ink) 6%, transparent); }
.stage-workflow-mark {
  position: relative;
  z-index: 1;
  display: inline-flex;
  width: 22px;
  height: 22px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
}
.stage-workflow-mark.is-index {
  color: var(--wf-muted);
  background: var(--wf-elevated);
  font-size: 10px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}
.stage-workflow-mark.is-index[data-digits="3"],
.stage-workflow-mark.is-index[data-digits="4"] { font-size: 8px; }
.stage-workflow-mark.is-index.is-err {
  color: var(--wf-err);
  background: color-mix(in srgb, var(--wf-err) 12%, var(--wf-elevated));
}
.stage-workflow-mark.is-icon.is-ok {
  color: light-dark(#fff, var(--wf-ok));
  background: light-dark(var(--wf-ok), color-mix(in srgb, var(--wf-ok) 22%, var(--wf-elevated)));
}
.stage-workflow-mark.is-icon.is-run {
  color: var(--wf-run);
  background: color-mix(in srgb, var(--wf-run) 14%, var(--wf-surface));
}
.stage-workflow-mark.is-icon.is-hold {
  color: var(--wf-warn);
  background: color-mix(in srgb, var(--wf-warn) 14%, var(--wf-surface));
}
.stage-workflow-mark.is-icon.is-err {
  color: var(--wf-err);
  background: color-mix(in srgb, var(--wf-err) 12%, var(--wf-surface));
}
.stage-workflow-mark.is-icon.is-quiet {
  color: var(--wf-muted);
  background: var(--wf-elevated);
}
.stage-workflow-label {
  padding-top: 1px;
  font-weight: 500;
  color: var(--wf-ink);
}
.stage-workflow-node.is-quiet .stage-workflow-label { color: var(--wf-muted); font-weight: 400; }
.stage-workflow-current {
  margin-left: 6px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--wf-muted);
  font-size: 12px;
  font-weight: 400;
  font-style: normal;
  line-height: 20px;
}
.stage-workflow-status {
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 6px;
  margin-left: auto;
  padding-top: 3px;
  font-size: 12px;
  font-weight: 500;
  line-height: 16px;
  white-space: nowrap;
}
.stage-workflow-status.is-run { color: var(--wf-run); }
.stage-workflow-status.is-hold { color: var(--wf-warn); }
.stage-workflow-status.is-err { color: var(--wf-err); }
.stage-workflow-status.is-ok { color: var(--wf-ok); }
.stage-workflow-status.is-quiet { color: var(--wf-muted); }
@keyframes stage-workflow-expand {
  from { opacity: 0; grid-template-rows: 0fr; }
  to { opacity: 1; grid-template-rows: 1fr; }
}
@keyframes stage-workflow-step-in {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: none; }
}
@media (prefers-reduced-motion: reduce) {
  .stage-workflow,
  .stage-workflow-step {
    animation: none;
    opacity: 1;
    transform: none;
  }
  .stage-workflow { grid-template-rows: 1fr; }
}
</style>

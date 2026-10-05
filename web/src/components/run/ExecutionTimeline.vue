<script setup lang="ts">
import { computed, ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import StatusPill from '../ui/StatusPill.vue'
import VarValueDisplay from '../ui/VarValueDisplay.vue'
import TruncatedTextTooltip from '../ui/TruncatedTextTooltip.vue'
import { fmtTime, fmtDuration } from '@/lib/shared/format'
import { formatVarChip, formatVarValue } from '@/lib/shared/compositeText'
import { NODE_DEFS, nodeColor } from '@/data/nodeRegistry'
import { resolveNodeDisplayLabel } from '@/lib/run/resolveNodeDisplayLabel'
import { compareTimelineOrder, resolveProcessDuration, resolveRunWallSec } from '@/lib/run/runStats'
import {
  fmtTokenCount,
  summarizeTimelineUsage,
  tokenUsageTotal,
} from '@/lib/run/tokenUsage'
import type { NodeRun, NodeRunStatus, Run, TokenUsage, WFNode } from '@/lib/shared/types'

const props = withDefaults(
  defineProps<{
    run: Run
    nodes: WFNode[]
    selectedNodeId: string | null
    // Array index of the selected execution within its node's history
    // (aligns with RunDetailView's selExecIdx / selIterIdx).
    selectedExecIdx: number
    /** When false, rows are not clickable and never show selection highlight (stats mode). */
    interactive?: boolean
    /** Optional shared clock from parent (stats mode); falls back to local 1s tick. */
    nowMs?: number
    /**
     * Bumped by parent (e.g. return-to-timeline) to re-scroll the selected
     * item into view even when selection itself did not change.
     */
    ensureVisibleToken?: number
  }>(),
  { interactive: true, ensureVisibleToken: 0 },
)
const emit = defineEmits<{ (e: 'select', nodeId: string, idx: number): void }>()

const rootEl = ref<HTMLElement | null>(null)

function scrollSelectedIntoView() {
  if (!props.interactive || !props.selectedNodeId || props.selectedExecIdx < 0) return
  const key = `${props.selectedNodeId}:${props.selectedExecIdx}`
  const nodes = rootEl.value?.querySelectorAll('[data-testid="timeline-item"]')
  if (!nodes?.length) return
  const el = Array.from(nodes).find((n) => n.getAttribute('data-item-key') === key) as
    | HTMLElement
    | undefined
  el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
}

watch(
  () => [props.selectedNodeId, props.selectedExecIdx, props.ensureVisibleToken] as const,
  async () => {
    await nextTick()
    scrollSelectedIntoView()
  },
)

const { t, locale } = useI18n()

/** Currently expanded var chip key (`nodeId:idx:varName`); null = none. */
const expandedKey = ref<string | null>(null)

function varExpandKey(it: TimelineItem, k: string) {
  return `${it.nodeId}:${it.idxInNode}:${k}`
}

function toggleVarExpand(it: TimelineItem, k: string, ev?: Event) {
  ev?.stopPropagation()
  const key = varExpandKey(it, k)
  expandedKey.value = expandedKey.value === key ? null : key
}

function expandedVar(it: TimelineItem): { k: string; v: unknown } | null {
  if (!expandedKey.value) return null
  const prefix = `${it.nodeId}:${it.idxInNode}:`
  if (!expandedKey.value.startsWith(prefix)) return null
  const k = expandedKey.value.slice(prefix.length)
  return it.vars.find((e) => e.k === k) ?? null
}

function fmtVarFull(v: unknown): string {
  if (v != null && typeof v === 'object') {
    try {
      return JSON.stringify(v, null, 2)
    } catch {
      return String(v)
    }
  }
  return formatVarValue(v)
}

interface TimelineItem {
  nodeId: string
  idxInNode: number
  iteration: number
  status: NodeRunStatus
  startedAt?: string
  durationSec?: number
  label: string
  type: string
  vars: { k: string; v: unknown }[]
  mcp: { tool: string; isError: boolean; tip: string }[]
  /** null/undefined = not reported; present (incl. zeros) = reported. */
  usage?: TokenUsage | null
}

// Kept for chip title tooltips (plain text).
function fmtVar(v: any): string {
  return formatVarChip(v)
}

function varEntries(snap?: Record<string, any>): { k: string; v: unknown }[] {
  if (!snap) return []
  return Object.keys(snap)
    .sort()
    .map((k) => ({ k, v: snap[k] }))
}

// Map an execution's MCP tool calls into compact chips (tool name + ok/err),
// with a hover tip showing truncated in/out for quick debugging.
function mcpEntries(calls?: NodeRun['mcpCalls']): { tool: string; isError: boolean; tip: string }[] {
  if (!calls) return []
  return calls.map((c) => ({
    tool: c.tool,
    isError: !!c.isError,
    tip: `${c.tool}\n${t('pages.liveLog.args')} ${c.args || t('common.emptyParen')}\n${t('pages.liveLog.result')} ${c.result || t('common.emptyParen')}`,
  }))
}

const nodeById = computed<Record<string, WFNode>>(() => {
  const m: Record<string, WFNode> = {}
  for (const n of props.nodes) m[n.id] = n
  return m
})

// Flatten every node's execution history into a single chronological list. The
// backend keeps one StateRun row per execution (loop-back / gate revise / retry
// each add a row), so this reads as a linear "what happened, in order" trace.
// Prefer nodeExecutions; fall back to nodeRuns for live nodes missing history
// (same boundary as runStats.flattenProcesses).
const items = computed<TimelineItem[]>(() => {
  void locale.value
  const execs = props.run.nodeExecutions || {}
  const nodeRuns = props.run.nodeRuns || {}
  const out: TimelineItem[] = []
  const ids = new Set([...Object.keys(execs), ...Object.keys(nodeRuns)])
  for (const nodeId of ids) {
    const list = execs[nodeId]?.length
      ? execs[nodeId]!
      : nodeRuns[nodeId]
        ? [nodeRuns[nodeId]!]
        : []
    const node = nodeById.value[nodeId]
    list.forEach((ex: NodeRun, idx: number) => {
      out.push({
        nodeId,
        idxInNode: idx,
        iteration: ex.iteration || idx + 1,
        status: ex.status,
        startedAt: ex.startedAt,
        durationSec: ex.durationSec,
        label: node
          ? resolveNodeDisplayLabel(node.label, node.type, t, { nodeId })
          : nodeId,
        type: node?.type || 'agent',
        vars: varEntries(ex.varsSnapshot),
        mcp: mcpEntries(ex.mcpCalls),
        usage: ex.usage,
      })
    })
  }
  // Sort by start time; executions without a timestamp (not yet started) sink to
  // the bottom so the observed order stays chronological. Shared with runStats.
  out.sort(compareTimelineOrder)
  return out
})

// Live-ticking elapsed time for active executions. Prefer parent-shared nowMs
// (stats mode) so timeline and ExecutionStatsPanel stay in sync.
const localNowMs = ref(Date.now())
let clock: number | undefined
const hasActiveItems = computed(() =>
  items.value.some((it) => it.status === 'running' || it.status === 'waiting_human'),
)
const runLive = computed(
  () =>
    props.run.status === 'running' ||
    props.run.status === 'waiting_human' ||
    props.run.status === 'queued',
)
const tickMs = computed(() => props.nowMs ?? localNowMs.value)
onMounted(() => {
  clock = window.setInterval(() => {
    if (props.nowMs != null) return
    if (hasActiveItems.value || runLive.value) localNowMs.value = Date.now()
  }, 1000)
})
onUnmounted(() => {
  if (clock) window.clearInterval(clock)
})

function displayDuration(it: TimelineItem): number | null {
  if (it.status === 'running' || it.status === 'waiting_human') {
    if (!it.startedAt) return null
    return resolveProcessDuration(it, tickMs.value)
  }
  if (it.status === 'completed' || it.status === 'failed' || it.status === 'skipped' || it.status === 'cancelled') {
    return it.durationSec != null ? it.durationSec : null
  }
  return null
}

const wallSec = computed(() => resolveRunWallSec(props.run, tickMs.value))
const usageSummary = computed(() => summarizeTimelineUsage(items.value, wallSec.value))

function iconOf(type: string) {
  return NODE_DEFS[type as keyof typeof NODE_DEFS]?.icon || 'agent'
}
function isSelected(it: TimelineItem) {
  if (!props.interactive) return false
  return it.nodeId === props.selectedNodeId && it.idxInNode === props.selectedExecIdx
}

function onSelect(it: TimelineItem) {
  if (!props.interactive) return
  emit('select', it.nodeId, it.idxInNode)
}
// A colored dot on the connector: mirrors the execution's status so the run
// reads at a glance (running/done/failed/paused).
const DOT: Record<string, string> = {
  running: 'bg-info',
  waiting_human: 'bg-warn',
  completed: 'bg-ok',
  skipped: 'bg-txt3',
  failed: 'bg-err',
  pending: 'bg-line',
}
</script>

<template>
  <div ref="rootEl" class="flex h-full min-w-0 w-full max-w-full flex-col bg-base">
    <div class="shrink-0 border-b border-line px-4 py-2.5 text-[12px] text-txt3">
      {{ t('pages.executionTimeline.header', { n: items.length }) }}
    </div>
    <div v-if="!items.length" class="flex flex-1 items-center justify-center text-[12px] text-txt3">
      {{ t('pages.executionTimeline.empty') }}
    </div>
    <template v-else>
      <!-- Footer lives in scroll flow (no shrink-0 sticky) so Run 汇总 cannot cut cards. -->
      <div class="scroll-area safe-area-bottom min-h-0 min-w-0 w-full max-w-full flex-1 overflow-x-clip overflow-y-auto px-5 py-4">
        <ol class="relative min-w-0 max-w-full space-y-2.5 border-l border-line pl-6">
          <li
            v-for="it in items"
            :key="`${it.nodeId}:${it.idxInNode}`"
            class="relative min-w-0 max-w-full"
            data-testid="timeline-item"
            :data-item-key="`${it.nodeId}:${it.idxInNode}`"
            :data-selected="isSelected(it) ? 'true' : 'false'"
          >
            <span
              class="absolute -left-[31px] top-4 h-2.5 w-2.5 rounded-full ring-4 ring-base"
              :class="DOT[it.status] || DOT.pending"
            />
            <div
              class="w-full min-w-0 overflow-hidden rounded-lg border bg-surface px-3 py-2.5 text-left transition-colors"
              :class="[
                isSelected(it) ? 'border-accent/60 ring-1 ring-accent/40' : 'border-line',
                interactive ? 'cursor-pointer hover:bg-elevated' : 'cursor-default',
              ]"
              @click="onSelect(it)"
            >
              <div class="flex min-w-0 items-center gap-2.5">
                <span
                  class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md"
                  :style="{ background: nodeColor(it.type as any, 0.13), color: nodeColor(it.type as any) }"
                >
                  <Icon :name="iconOf(it.type)" :size="16" />
                </span>
                <div class="min-w-0 flex-1 overflow-hidden">
                  <div class="flex items-center gap-1.5">
                    <TruncatedTextTooltip
                      :text="it.label"
                      class="min-w-0 truncate text-[13px] font-semibold text-txt"
                      data-testid="timeline-node-label"
                    />
                    <span
                      v-if="it.iteration > 1"
                      class="shrink-0 rounded-full border border-warn/40 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
                      :title="t('pages.executionTimeline.iterationTitle')"
                      >{{ t('common.iterationN', { n: it.iteration }) }}</span
                    >
                  </div>
                  <div
                    class="mt-0.5 flex flex-wrap items-center gap-x-3.5 gap-y-0.5 text-[11px] text-txt3"
                    data-testid="timeline-meta"
                  >
                    <span v-if="displayDuration(it) != null">
                      {{ t('pages.executionTimeline.duration', { time: fmtDuration(displayDuration(it)!) }) }}
                    </span>
                    <span
                      v-if="it.usage != null"
                      class="font-mono tabular-nums text-txt2"
                      data-testid="timeline-tokens"
                    >
                      tokens <span class="text-txt">{{ fmtTokenCount(tokenUsageTotal(it.usage)) }}</span>
                    </span>
                    <span v-else class="font-mono text-txt3" data-testid="timeline-tokens">
                      {{ t('pages.executionTimeline.tokensEmpty') }}
                    </span>
                  </div>
                  <div
                    v-if="it.usage != null"
                    class="mt-1.5 flex max-w-full flex-wrap gap-1 overflow-hidden"
                    data-testid="timeline-token-parts"
                  >
                    <span
                      class="inline-flex items-center gap-1 rounded border border-line bg-elevated px-1.5 py-px text-[10px] leading-4 text-txt2"
                    >
                      {{ t('pages.executionTimeline.partInput') }}
                      <b class="font-mono font-semibold text-txt">{{ fmtTokenCount(it.usage.inputTokens || 0) }}</b>
                    </span>
                    <span
                      class="inline-flex items-center gap-1 rounded border border-line bg-elevated px-1.5 py-px text-[10px] leading-4 text-txt2"
                    >
                      {{ t('pages.executionTimeline.partOutput') }}
                      <b class="font-mono font-semibold text-txt">{{ fmtTokenCount(it.usage.outputTokens || 0) }}</b>
                    </span>
                    <span
                      class="inline-flex items-center gap-1 rounded border border-line bg-elevated px-1.5 py-px text-[10px] leading-4 text-txt2"
                    >
                      {{ t('pages.executionTimeline.partCacheRead') }}
                      <b class="font-mono font-semibold text-txt">{{ fmtTokenCount(it.usage.cacheReadTokens || 0) }}</b>
                    </span>
                    <span
                      class="inline-flex items-center gap-1 rounded border border-line bg-elevated px-1.5 py-px text-[10px] leading-4 text-txt2"
                    >
                      {{ t('pages.executionTimeline.partCacheWrite') }}
                      <b class="font-mono font-semibold text-txt">{{ fmtTokenCount(it.usage.cacheWriteTokens || 0) }}</b>
                    </span>
                  </div>
                  <div v-if="it.vars.length" class="mt-1.5 min-w-0">
                    <div class="flex max-w-full flex-wrap gap-1 overflow-hidden">
                      <button
                        v-for="e in it.vars"
                        :key="e.k"
                        type="button"
                        data-testid="timeline-variable-chip"
                        class="inline-flex max-w-full min-w-0 items-center gap-1 overflow-hidden rounded border border-line bg-elevated px-1.5 py-px font-mono text-[10px] leading-4"
                        :class="
                          expandedKey === varExpandKey(it, e.k)
                            ? 'cursor-pointer border-accent/50 ring-1 ring-accent/30'
                            : 'cursor-pointer active:bg-surface'
                        "
                        @click="toggleVarExpand(it, e.k, $event)"
                      >
                        <span class="shrink-0 text-accent-2">{{ e.k }}</span>
                        <span class="shrink-0 text-txt3">=</span>
                        <TruncatedTextTooltip
                          :text="`${e.k} = ${fmtVar(e.v)}`"
                          :focusable="false"
                          focus-parent
                          measure-child
                          data-testid="timeline-variable-value"
                          class="min-w-0 flex-1 overflow-hidden truncate text-txt2"
                        >
                          <VarValueDisplay :value="e.v" compact class="min-w-0 max-w-full overflow-hidden truncate" />
                        </TruncatedTextTooltip>
                      </button>
                    </div>
                    <pre
                      v-if="expandedVar(it)"
                      class="scroll-area mt-1.5 max-h-40 min-w-0 max-w-full overflow-x-auto overflow-y-auto whitespace-pre rounded border border-line bg-elevated px-2 py-1.5 font-mono text-[10px] leading-4 text-txt2"
                    >{{ expandedVar(it)!.k }} = {{ fmtVarFull(expandedVar(it)!.v) }}</pre>
                  </div>
                  <div v-if="it.mcp.length" class="mt-1.5 flex max-w-full flex-wrap gap-1 overflow-hidden">
                    <span
                      v-for="(m, mi) in it.mcp"
                      :key="mi"
                      class="inline-flex max-w-full items-center gap-1 overflow-hidden rounded border px-1.5 py-px font-mono text-[10px] leading-4"
                      :class="m.isError ? 'border-err/50 bg-err/10 text-err' : 'border-info/40 bg-info/10 text-info'"
                    >
                      <Icon name="terminal" :size="10" />
                      <TruncatedTextTooltip :text="m.tip" class="min-w-0 truncate">{{ m.tool }}</TruncatedTextTooltip>
                      <span>{{ m.isError ? '✗' : '✓' }}</span>
                    </span>
                  </div>
                </div>
                <div class="flex shrink-0 flex-col items-end gap-1">
                  <StatusPill :status="it.status" size="sm" />
                  <span v-if="it.startedAt" class="text-[10px] text-txt3">{{ fmtTime(it.startedAt) }}</span>
                </div>
              </div>
            </div>
          </li>
        </ol>
        <div
          class="rounded-lg mt-3 border border-line bg-elevated px-4 py-3"
          data-testid="timeline-footer"
        >
          <div class="flex flex-wrap items-center justify-between gap-2.5">
            <div class="text-[11px] text-txt3">
              {{ t('pages.executionTimeline.footerLabel') }}
            </div>
            <div class="flex flex-wrap items-end gap-x-5 gap-y-1">
              <div class="grid gap-0.5" data-testid="timeline-total-tokens">
                <span class="text-[10px] tracking-wide text-txt3">{{ t('pages.executionTimeline.totalTokens') }}</span>
                <strong
                  class="font-mono text-[15px] font-bold tabular-nums tracking-tight"
                  :class="usageSummary.totalTokens == null ? 'font-semibold text-txt3' : 'text-txt'"
                >
                  {{
                    usageSummary.totalTokens == null
                      ? t('pages.executionTimeline.dash')
                      : fmtTokenCount(usageSummary.totalTokens)
                  }}
                </strong>
              </div>
              <div class="grid gap-0.5" data-testid="timeline-token-rate">
                <span class="text-[10px] tracking-wide text-txt3">{{ t('pages.executionTimeline.tokenRate') }}</span>
                <strong
                  class="font-mono text-[15px] font-bold tabular-nums tracking-tight"
                  :class="usageSummary.tokenRate == null ? 'font-semibold text-txt3' : 'text-txt'"
                >
                  {{ usageSummary.tokenRate ?? t('pages.executionTimeline.dash') }}
                </strong>
              </div>
              <div class="grid gap-0.5" data-testid="timeline-wall-clock">
                <span class="text-[10px] tracking-wide text-txt3">{{ t('pages.executionTimeline.wallClock') }}</span>
                <strong
                  class="font-mono text-[15px] tabular-nums tracking-tight"
                  :class="runLive ? 'font-bold text-txt' : 'font-semibold text-txt3'"
                >
                  {{ fmtDuration(wallSec) }}
                </strong>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

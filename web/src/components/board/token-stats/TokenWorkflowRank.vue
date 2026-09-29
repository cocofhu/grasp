<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { registerECharts } from '@/components/charts/echartsSetup'
import { rankTrackColor, statsTooltip } from '@/components/charts/chartTheme'
import type { TokenStatsWorkflow } from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'

registerECharts()

const WF_BAR_GRADIENT = {
  type: 'linear' as const,
  x: 0,
  y: 0,
  x2: 1,
  y2: 0,
  colorStops: [
    { offset: 0, color: '#6d5cff' },
    { offset: 1, color: '#9b8cff' },
  ],
}

const OTHER_BAR_GRADIENT = {
  type: 'linear' as const,
  x: 0,
  y: 0,
  x2: 1,
  y2: 0,
  colorStops: [
    { offset: 0, color: '#94a3b8' },
    { offset: 1, color: '#cbd5e1' },
  ],
}

const props = defineProps<{
  workflows: TokenStatsWorkflow[]
}>()

const { t } = useI18n()

const maxTotal = computed(() => Math.max(1, ...props.workflows.map((w) => w.total || 0)))

function isPM(w: TokenStatsWorkflow): boolean {
  return w.kind === 'pm' || (!w.other && w.name === 'PM' && !w.workflowId)
}

function isOther(w: TokenStatsWorkflow): boolean {
  return !!w.other || w.kind === 'other'
}

function displayName(w: TokenStatsWorkflow): string {
  if (isOther(w)) return t('pages.board.tokenStats.other')
  if (isPM(w)) return t('pages.board.tokenStats.pm')
  return w.name || w.workflowId || '—'
}

function badgeLabel(w: TokenStatsWorkflow, i: number): string {
  if (isOther(w)) return '·'
  let n = 0
  for (let j = 0; j <= i; j++) {
    const row = props.workflows[j]
    if (row && !isOther(row)) n += 1
  }
  return String(n)
}

function badgeClass(w: TokenStatsWorkflow, i: number): string {
  if (isOther(w)) return 'w-[22px] bg-elevated text-txt3'
  const n = Number(badgeLabel(w, i))
  return n <= 3
    ? 'w-[22px] bg-accent-dim text-accent-2'
    : 'w-[22px] bg-elevated text-txt3'
}

function barColor(w: TokenStatsWorkflow) {
  return isOther(w) ? OTHER_BAR_GRADIENT : WF_BAR_GRADIENT
}

function rowChartOption(w: TokenStatsWorkflow) {
  return {
    animation: false,
    grid: { left: 0, right: 0, top: 0, bottom: 0 },
    xAxis: { type: 'value', show: false, max: maxTotal.value },
    yAxis: { type: 'category', data: [''], show: false },
    tooltip: statsTooltip({
      trigger: 'item',
      formatter: () => `${displayName(w)}: ${fmtCompactTokenCount(w.total)}`,
    }),
    series: [
      {
        type: 'bar',
        data: [w.total || 0],
        barWidth: 8,
        itemStyle: { color: barColor(w), borderRadius: [0, 999, 999, 0] },
        showBackground: true,
        backgroundStyle: { color: rankTrackColor() },
      },
    ],
  }
}

const rowOptions = computed(() => props.workflows.map((w) => rowChartOption(w)))

defineExpose({ rowOptions, maxTotal, barColor })
</script>

<template>
  <ul data-testid="token-rank-list" class="token-rank relative m-0 grid list-none gap-2 p-0">
    <li
      v-for="(w, i) in workflows"
      :key="(w.kind || '') + '-' + (w.workflowId || w.name) + '-' + i"
      class="grid grid-cols-[22px_1fr_auto] items-center gap-2 px-0.5 py-1"
      :class="[
        isOther(w) ? 'token-rank-other' : '',
        isPM(w) ? 'token-rank-pm' : '',
      ]"
      :data-kind="w.kind || (isPM(w) ? 'pm' : isOther(w) ? 'other' : 'workflow')"
    >
      <span
        class="grid h-[22px] place-items-center rounded-md text-[11px] font-bold"
        :class="badgeClass(w, i)"
      >
        {{ badgeLabel(w, i) }}
      </span>
      <div class="min-w-0">
        <div class="truncate text-xs font-medium text-txt">{{ displayName(w) }}</div>
        <div class="mt-1 h-2 overflow-visible" data-testid="token-rank-bar">
          <VChart :option="rowOptions[i]" autoresize class="h-full w-full" />
        </div>
      </div>
      <span
        class="whitespace-nowrap text-xs tabular-nums text-txt3"
        :title="fmtTokenCount(w.total)"
        data-testid="token-rank-value"
      >{{ fmtCompactTokenCount(w.total) }}</span>
    </li>
  </ul>
</template>

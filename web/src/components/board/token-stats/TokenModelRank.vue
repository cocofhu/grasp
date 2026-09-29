<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { registerECharts } from '@/components/charts/echartsSetup'
import { rankTrackColor, statsTooltip } from '@/components/charts/chartTheme'
import type { TokenStatsModel } from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount, shouldShowUnknownVisual } from '@/lib/run/tokenUsage'
import UnknownModelBadge from '@/components/ui/UnknownModelBadge.vue'
import { colorForModel } from './tokenModelColors'

registerECharts()

const props = defineProps<{
  models: TokenStatsModel[]
}>()

const { t } = useI18n()
const maxTotal = computed(() => Math.max(1, ...props.models.map((m) => m.total || 0)))

function displayName(m: TokenStatsModel): string {
  if (m.other) return t('pages.board.tokenStats.modelOther')
  return m.name || m.modelKey || '—'
}

function barWidthPct(m: TokenStatsModel): number {
  return Math.max(0, Math.min(100, ((m.total || 0) / maxTotal.value) * 100))
}

function rowChartOption(m: TokenStatsModel, i: number) {
  const color = colorForModel(m, i)
  return {
    animation: false,
    grid: { left: 0, right: 0, top: 0, bottom: 0 },
    xAxis: { type: 'value', show: false, max: maxTotal.value },
    yAxis: { type: 'category', data: [''], show: false },
    tooltip: statsTooltip({
      trigger: 'item',
      formatter: () => fmtCompactTokenCount(m.total),
    }),
    series: [
      {
        type: 'bar',
        data: [m.total || 0],
        barWidth: 8,
        itemStyle: { color, borderRadius: [0, 8, 8, 0] },
        showBackground: true,
        backgroundStyle: { color: rankTrackColor() },
      },
    ],
  }
}

const rowOptions = computed(() => props.models.map((m, i) => rowChartOption(m, i)))

defineExpose({ rowOptions, maxTotal })
</script>

<template>
  <ul data-testid="token-model-rank" class="m-0 grid list-none gap-2 p-0">
    <li
      v-for="(m, i) in models"
      :key="(m.modelKey || m.name) + '-' + i"
      class="grid grid-cols-[1fr_auto] items-center gap-2"
      :data-unknown="m.unknown ? '1' : '0'"
      :data-other="m.other ? '1' : '0'"
      :data-filled="m.filled ? '1' : '0'"
    >
      <div class="min-w-0">
        <div
          class="flex min-w-0 items-center gap-1.5 text-xs font-medium"
          :class="shouldShowUnknownVisual(m.unknown, displayName(m)) ? 'text-txt3' : m.filled ? 'text-ok' : 'text-txt'"
          :title="displayName(m)"
        >
          <span class="min-w-0 truncate">{{ displayName(m) }}</span>
          <UnknownModelBadge v-if="shouldShowUnknownVisual(m.unknown, displayName(m))" />
        </div>
        <div class="relative mt-1 h-2 overflow-hidden" data-testid="token-model-rank-bar">
          <div
            class="h-full"
            :style="{ width: `${barWidthPct(m)}%`, backgroundColor: colorForModel(m, i) }"
          />
          <VChart :option="rowOptions[i]" autoresize class="absolute inset-0 w-full" />
        </div>
      </div>
      <span
        class="whitespace-nowrap text-xs tabular-nums text-txt3"
        :title="fmtTokenCount(m.total)"
      >{{ fmtCompactTokenCount(m.total) }}</span>
    </li>
    <li v-if="!models.length" class="text-xs text-txt3">{{ t('pages.board.tokenStats.emptyModelRankHint') }}</li>
  </ul>
</template>

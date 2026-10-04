<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { registerECharts } from '@/components/charts/echartsSetup'
import {
  TREND_CHART_GRID,
  axisTooltip,
  chartTone,
  fmtCompactAxis,
  statsAxis,
  statsLegend,
} from '@/components/charts/chartTheme'
import type { TokenStatsBucket } from '@/lib/shared/types'
import { fmtCompactTokenCount } from '@/lib/run/tokenUsage'
import { TOKEN_LEDGER_SOURCE_COLORS } from '@/components/token-analytics/tokenAnalyticsShared'
import { sourceLabel } from '@/components/token-analytics/tokenAnalyticsCharts'
import {
  TOKEN_SOURCE_COLORS,
  echartsTooltipPosition,
  formatBucketLabel,
  placeTrendTooltipAfter,
  trendCategoryIndexAtX,
} from './tokenStatsShared'

registerECharts()

const props = defineProps<{
  trend: TokenStatsBucket[]
  bucketWidth: string
}>()

const { t } = useI18n()

const CHART_HEIGHT_PX = 200
const WF_COLOR = TOKEN_SOURCE_COLORS.workflow
const PM_COLOR = TOKEN_SOURCE_COLORS.pm
const STUDIO_COLOR = TOKEN_LEDGER_SOURCE_COLORS.studio

type SourceKey = 'workflow' | 'pm' | 'studio'
const SOURCE_ORDER: SourceKey[] = ['workflow', 'pm', 'studio']

function sourceValue(b: TokenStatsBucket, key: SourceKey): number {
  if (key === 'workflow') return b.workflowTotal || 0
  if (key === 'pm') return b.pmTotal || 0
  return b.studioTotal || 0
}

/** Sources that are entirely zero under the current filter are omitted (no zero line). */
const activeSources = computed(() =>
  SOURCE_ORDER.filter((key) => props.trend.some((b) => sourceValue(b, key) > 0)),
)

const tone = computed(() => chartTone())

const wrapRef = ref<HTMLElement | null>(null)
const tipEl = ref<HTMLElement | null>(null)
const tip = ref({ show: false, x: 0, y: 0, idx: -1 })
const tipBucket = computed(() => (tip.value.idx >= 0 ? props.trend[tip.value.idx] : null))

function hideTip() {
  tip.value = { ...tip.value, show: false, idx: -1 }
}

function onWrapMouseMove(e: MouseEvent) {
  const wrap = wrapRef.value
  if (!wrap || !props.trend.length) return
  const rect = wrap.getBoundingClientRect()
  const idx = trendCategoryIndexAtX(e.clientX - rect.left, rect.width, props.trend.length)
  const pos = placeTrendTooltipAfter({
    caretX: e.clientX,
    caretY: e.clientY,
    tipW: tipEl.value?.offsetWidth || 168,
    tipH: tipEl.value?.offsetHeight || 72,
  })
  tip.value = { show: true, x: pos.left, y: pos.top, idx }
}

watch(
  () => props.trend,
  () => hideTip(),
)

function sourceSeriesStyle(key: SourceKey) {
  if (key === 'pm') {
    return {
      lineStyle: { color: PM_COLOR, width: 1.8, type: [5, 4] as unknown as 'dashed' },
      areaStyle: { color: 'rgba(109, 92, 255, 0.14)' },
      symbol: 'circle' as const,
      symbolSize: 7,
      showSymbol: true,
      itemStyle: { color: '#fff', borderColor: PM_COLOR, borderWidth: 2 },
    }
  }
  if (key === 'studio') {
    return {
      lineStyle: { color: STUDIO_COLOR, width: 1.8 },
      areaStyle: { color: 'rgba(34, 166, 179, 0.28)' },
      showSymbol: false,
      itemStyle: { color: STUDIO_COLOR },
    }
  }
  return {
    lineStyle: { color: WF_COLOR, width: 1.8 },
    areaStyle: { color: 'rgba(109, 92, 255, 0.28)' },
    showSymbol: false,
  }
}

const chartOption = computed(() => {
  const labels = props.trend.map((b) => formatBucketLabel(b.bucket, props.bucketWidth))
  const sources = activeSources.value
  const axis = statsAxis()
  return {
    grid: TREND_CHART_GRID,
    tooltip: axisTooltip({
      // Option keeps shared chrome + className for unit-test contract.
      // DOM tooltip is the Teleport node (v-if) so Playwright count goes to 0 on leave.
      show: false,
      className: 'token-stats-echarts-tooltip',
      triggerOn: 'mousemove',
      axisPointer: { type: 'line' as const, snap: true, animation: false },
      position: (
        point: number[],
        _params: unknown,
        _dom: unknown,
        _rect: unknown,
        size: { contentSize: number[] },
      ) => {
        const box = wrapRef.value?.getBoundingClientRect() ?? null
        return echartsTooltipPosition(point, size.contentSize, box)
      },
      formatter: (params: unknown) => {
        const items = (Array.isArray(params) ? params : [params]) as { dataIndex?: number }[]
        const idx = items[0]?.dataIndex ?? 0
        const b = props.trend[idx]
        if (!b) return ''
        const label = formatBucketLabel(b.bucket, props.bucketWidth)
        const total = fmtCompactTokenCount(b.total)
        const rows = sources
          .map(
            (key) =>
              `<div data-tip-row="${key}">${sourceLabel(t, key)} ${fmtCompactTokenCount(sourceValue(b, key))}</div>`,
          )
          .join('')
        return `<div>${label} · ${total}</div>${rows}`
      },
    }),
    legend: {
      ...statsLegend(),
      data: sources.map((key) => sourceLabel(t, key)),
    },
    xAxis: {
      type: 'category',
      data: labels,
      ...axis,
      splitLine: { show: false },
      axisLabel: { ...axis.axisLabel, maxInterval: Math.ceil(labels.length / 8) },
    },
    yAxis: {
      type: 'value',
      ...axis,
      axisLabel: { ...axis.axisLabel, formatter: (v: number) => fmtCompactAxis(v) },
    },
    series: sources.map((key) => ({
      type: 'line' as const,
      name: sourceLabel(t, key),
      stack: 'source',
      data: props.trend.map((b) => sourceValue(b, key)),
      clip: false,
      smooth: true,
      ...sourceSeriesStyle(key),
    })),
  }
})

const chartData = computed(() => ({
  labels: props.trend.map((b) => formatBucketLabel(b.bucket, props.bucketWidth)),
  datasets: activeSources.value.map((key) => ({
    label: key,
    data: props.trend.map((b) => sourceValue(b, key)),
    borderColor: key === 'studio' ? STUDIO_COLOR : key === 'pm' ? PM_COLOR : WF_COLOR,
    ...(key === 'pm' ? { borderDash: [5, 4] } : {}),
  })),
}))

defineExpose({ chartOption, chartData, hideTip })
</script>

<template>
  <div
    ref="wrapRef"
    data-testid="token-trend-wrap"
    class="token-trend-wrap relative w-full min-w-0 overflow-x-clip overflow-y-visible"
    :style="{ height: `${CHART_HEIGHT_PX}px` }"
    role="img"
    :aria-label="t('pages.board.tokenStats.trendTitle')"
    @mousemove.capture="onWrapMouseMove"
    @mouseleave="hideTip"
  >
    <div data-testid="token-trend-chart" class="h-full w-full overflow-visible">
      <VChart :option="chartOption" autoresize class="h-full w-full" />
    </div>
  </div>
  <Teleport to="body">
    <div
      v-if="tip.show && tipBucket"
      ref="tipEl"
      class="token-stats-echarts-tooltip token-trend-tooltip pointer-events-none fixed z-[1000] min-w-[140px] px-2.5 py-2 text-[11px]"
      :style="{
        left: tip.x + 'px',
        top: tip.y + 'px',
        backgroundColor: tone.tooltipBg,
        border: `1px solid ${tone.tooltipBorder}`,
        color: tone.tooltipText,
        borderRadius: '12px',
      }"
    >
      <div>
        {{ formatBucketLabel(tipBucket.bucket, bucketWidth) }}
        · {{ fmtCompactTokenCount(tipBucket.total) }}
      </div>
      <div v-for="key in activeSources" :key="key" :data-tip-row="key">
        {{ sourceLabel(t, key) }} {{ fmtCompactTokenCount(sourceValue(tipBucket, key)) }}
      </div>
    </div>
  </Teleport>
</template>

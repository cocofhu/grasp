<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type {
  GlobalTokenStatsNamedBucket,
  ProjectTokenStats,
  TokenStatsWindow,
} from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'
import TokenTrendChart from './TokenTrendChart.vue'
import TokenDonutChart from './TokenDonutChart.vue'
import TokenWorkflowRank from './TokenWorkflowRank.vue'
import TokenModelComposition from './TokenModelComposition.vue'
import TokenModelRank from './TokenModelRank.vue'
import { clientTimezoneParams } from './tokenStatsShared'
import {
  fmtCost,
  fmtDeltaPct,
  fmtPct,
} from '@/components/token-analytics/tokenAnalyticsShared'
import {
  phaseLabel,
  sourceLabel,
  statusLabel,
} from '@/components/token-analytics/tokenAnalyticsCharts'

const props = defineProps<{
  projectId: string
  /** Carried from usage stats (`?window=&from=&to=`). Direct opens stay on 30d. */
  initialWindow?: string
  initialFrom?: string
  initialTo?: string
  initialGranularity?: string
}>()

const { t } = useI18n()

const WINDOWS: TokenStatsWindow[] = ['24h', '7d', '30d', '90d', 'all']
const SOURCES = ['all', 'workflow', 'pm', 'studio'] as const

type BoardWindow = TokenStatsWindow | 'custom'
type BoardStats = ProjectTokenStats & {
  kpi?: {
    total: number
    prevTotal?: number | null
    deltaPct?: number | null
    inputTokens: number
    outputTokens: number
    cacheReadTokens: number
    cacheWriteTokens: number
    failedTotal?: number
    runCount?: number
    modelCount?: number
    cacheHitRate?: number
    cost?: number
    costDeltaPct?: number | null
  }
  currency?: string
  sources?: GlobalTokenStatsNamedBucket[]
  statuses?: GlobalTokenStatsNamedBucket[]
  phases?: GlobalTokenStatsNamedBucket[]
  nodeTypes?: GlobalTokenStatsNamedBucket[]
  topRuns?: { runId: string; title?: string; total: number }[]
  filterOptions?: {
    models?: { key: string; name: string }[]
    workflows?: { key: string; name: string }[]
    nodeTypes?: { key: string; name: string }[]
  }
  projects?: unknown[]
  heatmap?: unknown
}

const windowSel = ref<BoardWindow>('30d')
const rangeFrom = ref('')
const rangeTo = ref('')
const granularity = ref<'' | 'hour' | 'day' | 'week'>('')
const sourceSel = ref<(typeof SOURCES)[number]>('all')
const statusSel = ref('')
const workflowId = ref('')
const modelKey = ref('')
const nodeType = ref('')
const loading = ref(true)
const failed = ref(false)
const data = ref<BoardStats | null>(null)
const filterOptions = ref<NonNullable<BoardStats['filterOptions']>>({})

let abort: AbortController | null = null
let generation = 0

function dateInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function applyInitialRange() {
  const w = props.initialWindow || ''
  if (props.initialFrom) {
    windowSel.value = 'custom'
    rangeFrom.value = props.initialFrom
    rangeTo.value = props.initialTo || props.initialFrom
  } else if (w === '24h' || w === '7d' || w === '30d' || w === '90d' || w === 'all') {
    windowSel.value = w
    rangeFrom.value = ''
    rangeTo.value = ''
  }
  if (props.initialGranularity === 'hour' || props.initialGranularity === 'day' || props.initialGranularity === 'week') {
    granularity.value = props.initialGranularity
  }
}

applyInitialRange()

const windowLabel = computed(() => {
  if (windowSel.value === 'custom') {
    const from = rangeFrom.value
    const to = rangeTo.value || from
    return from ? `${from} – ${to}` : t('pages.tokenAnalytics.windowCustom')
  }
  return t(`pages.board.tokenStats.windows.${windowSel.value}`)
})

const grainLabel = computed(() => {
  if (!data.value) return ''
  if (data.value.bucketWidth === 'hour') return t('pages.board.tokenStats.grainHour')
  if (data.value.bucketWidth === 'week') return t('pages.board.tokenStats.grainWeek')
  return t('pages.board.tokenStats.grainDay')
})

const isEmpty = computed(() => !!data.value?.empty)

const modelCompModels = computed(() => {
  const d = data.value
  if (!d) return []
  if (d.modelComposition && d.modelComposition.length) return d.modelComposition
  return d.modelRanking || []
})

const modelCompTotal = computed(() => modelCompModels.value.reduce((s, m) => s + (m.total || 0), 0))

function positiveNamed(rows?: GlobalTokenStatsNamedBucket[]) {
  return (rows || []).filter((r) => (r.total || 0) > 0)
}

const deltaLabel = computed(() => fmtDeltaPct(data.value?.kpi?.deltaPct) ?? t('pages.tokenAnalytics.noPrev'))
const costDeltaLabel = computed(() => fmtDeltaPct(data.value?.kpi?.costDeltaPct))

async function load() {
  const gen = ++generation
  abort?.abort()
  abort = new AbortController()
  loading.value = true
  failed.value = false
  // Clear previous window data so UI never shows stale charts while loading.
  data.value = null

  const tz = clientTimezoneParams()
  try {
    const res = await api.getProjectTokenStats(
      props.projectId,
      {
        window: windowSel.value,
        from: windowSel.value === 'custom' ? rangeFrom.value : undefined,
        to: windowSel.value === 'custom' ? rangeTo.value || rangeFrom.value : undefined,
        granularity: granularity.value || undefined,
        timezone: tz.timezone,
        utcOffsetMinutes: tz.utcOffsetMinutes,
        source: sourceSel.value,
        status: statusSel.value || undefined,
        workflowId: workflowId.value || undefined,
        modelKey: modelKey.value || undefined,
        nodeType: nodeType.value || undefined,
      },
      { signal: abort.signal },
    )
    if (gen !== generation) return
    data.value = res
    if (res.filterOptions) filterOptions.value = res.filterOptions
    failed.value = false
  } catch (e: unknown) {
    if (gen !== generation) return
    if ((e as { name?: string })?.name === 'AbortError') return
    failed.value = true
    data.value = null
  } finally {
    if (gen === generation) loading.value = false
  }
}

function selectWindow(w: TokenStatsWindow) {
  if (windowSel.value === w && !failed.value && data.value) return
  windowSel.value = w
  rangeFrom.value = ''
  rangeTo.value = ''
  void load()
}

function selectCustomWindow() {
  if (windowSel.value === 'custom' && rangeFrom.value) return
  const end = new Date()
  const start = new Date(end)
  start.setDate(end.getDate() - 6)
  rangeFrom.value = dateInputValue(start)
  rangeTo.value = dateInputValue(end)
  windowSel.value = 'custom'
  void load()
}

function setRange(field: 'from' | 'to', value: string) {
  if (field === 'from') rangeFrom.value = value
  else rangeTo.value = value
  if (rangeFrom.value && rangeTo.value && rangeFrom.value > rangeTo.value) {
    if (field === 'from') rangeTo.value = rangeFrom.value
    else rangeFrom.value = rangeTo.value
  }
  windowSel.value = 'custom'
  if (!rangeFrom.value) return
  void load()
}

function setSource(s: (typeof SOURCES)[number]) {
  if (sourceSel.value === s) return
  sourceSel.value = s
  void load()
}

function setSelect(key: 'status' | 'workflow' | 'model' | 'node' | 'grain', value: string) {
  if (key === 'status') statusSel.value = value
  else if (key === 'workflow') workflowId.value = value
  else if (key === 'model') modelKey.value = value
  else if (key === 'node') nodeType.value = value
  else granularity.value = value as '' | 'hour' | 'day' | 'week'
  void load()
}

function retry() {
  void load()
}

watch(
  () => props.projectId,
  () => {
    windowSel.value = '30d'
    rangeFrom.value = ''
    rangeTo.value = ''
    granularity.value = ''
    sourceSel.value = 'all'
    statusSel.value = ''
    workflowId.value = ''
    modelKey.value = ''
    nodeType.value = ''
    void load()
  },
)

onMounted(() => {
  void load()
})

onUnmounted(() => {
  generation += 1
  abort?.abort()
})
</script>

<template>
  <section
    data-testid="token-stats-panel"
    class="token-stats-panel mb-4 min-w-0 overflow-x-clip"
    aria-labelledby="token-stats-heading"
  >
    <div
      data-testid="token-stats-head"
      class="mb-3 flex flex-col items-stretch gap-3 md:flex-row md:flex-wrap md:items-center md:justify-between"
    >
      <h3 id="token-stats-heading" class="m-0 flex items-center gap-2 text-sm font-semibold text-txt">
        {{ t('pages.board.tokenStats.title') }}
        <span
          data-testid="token-stats-window-badge"
          class="rounded-full bg-accent-dim px-2 py-0.5 text-[11px] font-semibold text-accent-2"
        >
          {{ windowLabel }}
        </span>
      </h3>
      <div class="flex flex-wrap items-center gap-2">
        <div
          class="flex flex-wrap gap-2 rounded-[10px] bg-elevated p-0.5"
          role="tablist"
          :aria-label="t('pages.board.tokenStats.windowAria')"
          data-testid="token-stats-windows"
        >
          <button
            v-for="w in WINDOWS"
            :key="w"
            type="button"
            role="tab"
            class="min-h-11 rounded-lg border-0 px-3 py-2.5 text-sm transition md:min-h-0 md:px-2.5 md:py-1.5 md:text-xs"
            :class="
              windowSel === w
                ? 'bg-surface font-semibold text-txt shadow-sm'
                : 'bg-transparent text-txt3 hover:text-txt2'
            "
            :aria-selected="windowSel === w"
            :data-testid="`token-stats-window-${w}`"
            @click="selectWindow(w)"
          >
            {{ t(`pages.board.tokenStats.windows.${w}`) }}
          </button>
        </div>
        <button
          type="button"
          class="min-h-11 rounded-lg border border-line px-3 py-2 text-xs md:min-h-0"
          :class="windowSel === 'custom' ? 'bg-surface font-semibold text-txt' : 'text-txt3'"
          data-testid="token-stats-window-custom"
          @click="selectCustomWindow"
        >
          {{ t('pages.tokenAnalytics.windowCustom') }}
        </button>
        <select
          :value="granularity"
          class="rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
          :aria-label="t('pages.tokenAnalytics.granularity.aria')"
          data-testid="token-stats-granularity"
          @change="setSelect('grain', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">{{ t('pages.tokenAnalytics.granularity.auto') }}</option>
          <option value="hour">{{ t('pages.tokenAnalytics.granularity.hour') }}</option>
          <option value="day">{{ t('pages.tokenAnalytics.granularity.day') }}</option>
          <option value="week">{{ t('pages.tokenAnalytics.granularity.week') }}</option>
        </select>
      </div>
    </div>

    <div v-if="windowSel === 'custom'" class="mb-3 flex flex-wrap gap-2" data-testid="token-stats-range">
      <label class="flex items-center gap-1 text-xs text-txt3">
        {{ t('pages.tokenAnalytics.rangeFrom') }}
        <input
          type="date"
          class="rounded border border-line bg-surface px-2 py-1 text-xs text-txt"
          data-testid="token-stats-range-from"
          :value="rangeFrom"
          @change="setRange('from', ($event.target as HTMLInputElement).value)"
        />
      </label>
      <label class="flex items-center gap-1 text-xs text-txt3">
        {{ t('pages.tokenAnalytics.rangeTo') }}
        <input
          type="date"
          class="rounded border border-line bg-surface px-2 py-1 text-xs text-txt"
          data-testid="token-stats-range-to"
          :value="rangeTo"
          @change="setRange('to', ($event.target as HTMLInputElement).value)"
        />
      </label>
    </div>

    <div class="mb-3 flex flex-wrap items-center gap-2" data-testid="token-stats-filters">
      <button
        v-for="s in SOURCES"
        :key="s"
        type="button"
        class="rounded-md px-2.5 py-1 text-xs"
        :class="sourceSel === s ? 'bg-accent-dim font-semibold text-accent-2' : 'text-txt3'"
        :data-testid="`token-stats-source-${s}`"
        @click="setSource(s)"
      >
        {{ s === 'all' ? t('pages.tokenAnalytics.sourceAll') : sourceLabel(t, s) }}
      </button>
      <select
        :value="workflowId"
        class="rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
        data-testid="token-stats-filter-workflow"
        @change="setSelect('workflow', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">{{ t('pages.tokenAnalytics.workflowAll') }}</option>
        <option v-for="w in filterOptions.workflows || []" :key="w.key" :value="w.key">{{ w.name }}</option>
      </select>
      <select
        :value="modelKey"
        class="rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
        data-testid="token-stats-filter-model"
        @change="setSelect('model', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">{{ t('pages.tokenAnalytics.modelAll') }}</option>
        <option v-for="m in filterOptions.models || []" :key="m.key" :value="m.key">{{ m.name }}</option>
      </select>
      <select
        :value="nodeType"
        class="rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
        data-testid="token-stats-filter-node"
        @change="setSelect('node', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">{{ t('pages.tokenAnalytics.nodeTypeAll') }}</option>
        <option v-for="n in filterOptions.nodeTypes || []" :key="n.key" :value="n.key">{{ n.name }}</option>
      </select>
      <select
        :value="statusSel"
        class="rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
        data-testid="token-stats-filter-status"
        @change="setSelect('status', ($event.target as HTMLSelectElement).value)"
      >
        <option value="">{{ t('pages.tokenAnalytics.statusAll') }}</option>
        <option value="ok">{{ statusLabel(t, 'ok') }}</option>
        <option value="failed">{{ statusLabel(t, 'failed') }}</option>
        <option value="cancelled">{{ statusLabel(t, 'cancelled') }}</option>
      </select>
    </div>

    <!-- Loading: whole panel placeholder, no stale charts -->
    <div
      v-if="loading"
      data-testid="token-stats-loading"
      class="flex min-h-[220px] items-center justify-center rounded-lg border border-line bg-surface text-sm text-txt3"
    >
      {{ t('pages.board.tokenStats.loading') }}
    </div>

    <!-- Failure: unified panel error + retry -->
    <div
      v-else-if="failed"
      data-testid="token-stats-error"
      class="flex min-h-[180px] flex-col items-center justify-center gap-3 rounded-lg border border-err/40 bg-err/10 px-4 py-6 text-center"
    >
      <p class="m-0 text-sm text-err">{{ t('pages.board.tokenStats.loadFailed') }}</p>
      <button
        type="button"
        class="rounded-md border border-err/40 px-3 py-1.5 text-xs text-err hover:bg-err/10"
        data-testid="token-stats-retry"
        @click="retry"
      >
        {{ t('pages.board.tokenStats.retry') }}
      </button>
    </div>

    <!-- Empty: no reported usage in window -->
    <div
      v-else-if="isEmpty"
      data-testid="token-stats-empty"
      class="grid gap-3"
    >
      <div class="relative min-h-[200px] min-w-0 overflow-x-clip rounded-lg border border-line bg-surface p-3.5">
        <div class="mb-2 flex items-baseline justify-between gap-2">
          <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.trendTitle') }}</h4>
          <span class="text-[11px] text-txt3">{{ grainLabel }}</span>
        </div>
        <div class="flex min-h-[160px] flex-col items-center justify-center gap-1.5 text-center">
          <strong class="text-[13px] text-txt">{{ t('pages.board.tokenStats.emptyTrendTitle') }}</strong>
          <span class="max-w-[28ch] text-xs text-txt3">{{ t('pages.board.tokenStats.emptyTrendHint') }}</span>
        </div>
      </div>
      <div class="grid min-w-0 grid-cols-1 gap-3 md:grid-cols-2">
        <div class="relative min-h-[200px] min-w-0 rounded-lg border border-line bg-surface p-3.5">
          <div class="mb-2 flex items-baseline justify-between gap-2">
            <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.compositionTitle') }}</h4>
          </div>
          <div class="flex min-h-[160px] flex-col items-center justify-center gap-1.5 text-center">
            <strong class="text-[13px] text-txt">{{ t('pages.board.tokenStats.emptyCompTitle') }}</strong>
            <span class="max-w-[28ch] text-xs text-txt3">{{ t('pages.board.tokenStats.emptyCompHint') }}</span>
          </div>
        </div>
        <div class="relative min-h-[200px] min-w-0 rounded-lg border border-line bg-surface p-3.5">
          <div class="mb-2 flex flex-col gap-1">
            <div class="flex items-baseline justify-between gap-2">
              <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.rankTitle') }}</h4>
              <span class="text-[11px] text-txt3">{{ t('pages.board.tokenStats.rankSub') }}</span>
            </div>
          </div>
          <div class="flex min-h-[160px] flex-col items-center justify-center gap-1.5 text-center">
            <strong class="text-[13px] text-txt">{{ t('pages.board.tokenStats.emptyRankTitle') }}</strong>
            <span class="max-w-[28ch] text-xs text-txt3">{{ t('pages.board.tokenStats.emptyRankHint') }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Ready charts. Cross-project share, project table, and model×project stay on the usage page. -->
    <div v-else-if="data" data-testid="token-stats-charts" class="grid gap-3">
      <div
        v-if="data.kpi"
        class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4"
        data-testid="token-stats-kpis"
      >
        <div class="rounded-lg border border-line bg-surface p-3" data-testid="token-stats-kpi-total">
          <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiTotal') }}</div>
          <div class="mt-1 text-lg font-bold tabular-nums" :title="fmtTokenCount(data.kpi.total)">{{ fmtCompactTokenCount(data.kpi.total) }}</div>
          <div class="mt-0.5 text-[11px] text-txt3">{{ deltaLabel }}</div>
        </div>
        <div class="rounded-lg border border-line bg-surface p-3" data-testid="token-stats-kpi-inout">
          <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiInOutCache') }}</div>
          <div class="mt-1 text-sm tabular-nums">
            {{ fmtCompactTokenCount(data.kpi.inputTokens) }} / {{ fmtCompactTokenCount(data.kpi.outputTokens) }}
          </div>
        </div>
        <div class="rounded-lg border border-line bg-surface p-3" data-testid="token-stats-kpi-cache">
          <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.cacheHit') }}</div>
          <div class="mt-1 text-lg font-bold tabular-nums">{{ fmtPct(data.kpi.cacheHitRate) }}</div>
        </div>
        <div class="rounded-lg border border-line bg-surface p-3" data-testid="token-stats-kpi-cost">
          <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiCost') }}</div>
          <div class="mt-1 text-lg font-bold tabular-nums">{{ (data.kpi.cost || 0) > 0 ? fmtCost(data.kpi.cost, data.currency) : t('pages.tokenAnalytics.kpiCostUnpriced') }}</div>
          <div v-if="costDeltaLabel" class="mt-0.5 text-[11px] text-txt3">{{ costDeltaLabel }}</div>
        </div>
        <div class="rounded-lg border border-line bg-surface p-3" data-testid="token-stats-kpi-failed">
          <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiFailed') }}</div>
          <div class="mt-1 text-lg font-bold tabular-nums">{{ fmtCompactTokenCount(data.kpi.failedTotal || 0) }}</div>
        </div>
        <div class="rounded-lg border border-line bg-surface p-3" data-testid="token-stats-kpi-runs">
          <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.tables.colRuns') }}</div>
          <div class="mt-1 text-lg font-bold tabular-nums">{{ data.kpi.runCount ?? 0 }}</div>
        </div>
        <div class="rounded-lg border border-line bg-surface p-3" data-testid="token-stats-kpi-models">
          <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.tables.colModel') }}</div>
          <div class="mt-1 text-lg font-bold tabular-nums">{{ data.kpi.modelCount ?? 0 }}</div>
        </div>
      </div>

      <div
        class="min-w-0 overflow-x-clip rounded-lg border border-line bg-surface p-3.5"
        data-testid="token-stats-trend-card"
      >
        <div class="mb-2 flex items-baseline justify-between gap-2">
          <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.trendTitle') }}</h4>
          <span class="text-[11px] text-txt3">{{ grainLabel }}</span>
        </div>
        <TokenTrendChart :trend="data.trend" :bucket-width="data.bucketWidth" />
      </div>
      <div class="grid min-w-0 grid-cols-1 gap-3 md:grid-cols-2">
        <div class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-model-comp-card">
          <div class="mb-2 flex items-baseline justify-between gap-2">
            <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.modelCompositionTitle') }}</h4>
            <span class="text-[11px] text-txt3">
              {{ t('pages.board.tokenStats.compTotal', { n: fmtCompactTokenCount(modelCompTotal) }) }}
            </span>
          </div>
          <TokenModelComposition :models="modelCompModels" />
        </div>
        <div class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-model-rank-card">
          <div class="mb-2 flex items-baseline justify-between gap-2" data-testid="token-stats-model-rank-head">
            <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.modelRankTitle') }}</h4>
            <span class="text-[11px] text-txt3">{{ t('pages.board.tokenStats.modelRankSub') }}</span>
          </div>
          <TokenModelRank :models="data.modelRanking || []" />
        </div>
      </div>
      <div class="grid min-w-0 grid-cols-1 gap-3 md:grid-cols-2">
        <div class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-comp-card">
          <div class="mb-2 flex items-baseline justify-between gap-2">
            <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.compositionTitle') }}</h4>
            <span class="text-[11px] text-txt3">
              {{ t('pages.board.tokenStats.compTotal', { n: fmtCompactTokenCount(data.composition.total) }) }}
            </span>
          </div>
          <TokenDonutChart :composition="data.composition" />
        </div>
        <div class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-rank-card">
          <div class="mb-2 flex flex-col gap-1">
            <div class="flex items-baseline justify-between gap-2">
              <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.board.tokenStats.rankTitle') }}</h4>
              <span class="text-[11px] text-txt3">{{ t('pages.board.tokenStats.rankSub') }}</span>
            </div>
            <p class="m-0 text-[11px] leading-snug text-txt3">{{ t('pages.board.tokenStats.rankHint') }}</p>
          </div>
          <TokenWorkflowRank :workflows="data.workflows" />
        </div>
      </div>

      <div v-if="positiveNamed(data.sources).length" class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-source-card">
        <h4 class="m-0 mb-2 text-[13px] font-semibold text-txt">{{ t('pages.tokenAnalytics.charts.sourcePie') }}</h4>
        <ul class="m-0 grid gap-1 p-0">
          <li v-for="row in positiveNamed(data.sources)" :key="row.key || row.name" class="flex justify-between text-xs">
            <span>{{ sourceLabel(t, row.key || row.name) }}</span>
            <span class="tabular-nums">{{ fmtCompactTokenCount(row.total) }}</span>
          </li>
        </ul>
      </div>
      <div v-if="positiveNamed(data.nodeTypes).length" class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-node-card">
        <h4 class="m-0 mb-2 text-[13px] font-semibold text-txt">{{ t('pages.tokenAnalytics.charts.nodePie') }}</h4>
        <ul class="m-0 grid gap-1 p-0">
          <li v-for="row in positiveNamed(data.nodeTypes)" :key="row.key || row.name" class="flex justify-between text-xs">
            <span>{{ row.name }}</span>
            <span class="tabular-nums">{{ fmtCompactTokenCount(row.total) }}</span>
          </li>
        </ul>
      </div>
      <div v-if="positiveNamed(data.statuses).length" class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-status-card">
        <h4 class="m-0 mb-2 text-[13px] font-semibold text-txt">{{ t('pages.tokenAnalytics.charts.statusPie') }}</h4>
        <ul class="m-0 grid gap-1 p-0">
          <li v-for="row in positiveNamed(data.statuses)" :key="row.key || row.name" class="flex justify-between text-xs">
            <span>{{ statusLabel(t, row.key || row.name) }}</span>
            <span class="tabular-nums">{{ fmtCompactTokenCount(row.total) }}</span>
          </li>
        </ul>
      </div>
      <div v-if="positiveNamed(data.phases).length" class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-phase-card">
        <h4 class="m-0 mb-2 text-[13px] font-semibold text-txt">{{ t('pages.tokenAnalytics.charts.phasePie') }}</h4>
        <ul class="m-0 grid gap-1 p-0">
          <li v-for="row in positiveNamed(data.phases)" :key="row.key || row.name" class="flex justify-between text-xs">
            <span>{{ phaseLabel(t, row.key || row.name) }}</span>
            <span class="tabular-nums">{{ fmtCompactTokenCount(row.total) }}</span>
          </li>
        </ul>
      </div>
      <div v-if="(data.topRuns || []).length" class="min-w-0 rounded-lg border border-line bg-surface p-3.5" data-testid="token-stats-runs-card">
        <h4 class="m-0 mb-2 text-[13px] font-semibold text-txt">{{ t('pages.tokenAnalytics.tables.runs') }}</h4>
        <ul class="m-0 grid gap-1 p-0">
          <li v-for="row in data.topRuns" :key="row.runId" class="flex justify-between text-xs">
            <span class="truncate">{{ row.title || row.runId }}</span>
            <span class="tabular-nums">{{ fmtCompactTokenCount(row.total) }}</span>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>

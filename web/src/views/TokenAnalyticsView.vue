<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { registerECharts } from '@/components/charts/echartsSetup'
import { api } from '@/lib/api/api'
import type {
  GlobalTokenStats,
  GlobalTokenStatsProjectRow,
  TokenStatsModel,
  TokenStatsWindow,
  TokenStatsWorkflow,
  GlobalTokenStatsNamedBucket,
} from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'
import { displayRunTitle } from '@/lib/run/runTitle'
import { truncateText } from '@/lib/shared/format'
import { TOKEN_PART_COLORS, TOKEN_PART_KEYS } from '@/components/board/token-stats/tokenStatsShared'
import { useToast } from '@/lib/composables/useToast'
import { useAuth } from '@/lib/composables/useAuth'
import TokenChartCard from '@/components/token-analytics/TokenChartCard.vue'
import TokenDrillDownModal from '@/components/token-analytics/TokenDrillDownModal.vue'
import TokenPricingModal from '@/components/token-analytics/TokenPricingModal.vue'
import {
  TOKEN_LEDGER_PHASE_COLORS,
  TOKEN_LEDGER_SOURCE_COLORS,
  TOKEN_LEDGER_STATUS_COLORS,
  activeFilterCount,
  defaultTokenStatsFilters,
  downloadCsv,
  filtersToParams,
  fmtCost,
  fmtDeltaPct,
  fmtPct,
  paletteColor,
  toCsv,
  type DrillTarget,
  type TokenSourceFilter,
  type TokenStatsFilters,
} from '@/components/token-analytics/tokenAnalyticsShared'
import {
  areaChartOption,
  bucketPart,
  costBarOption,
  drillFromChartEvent,
  drillPieOption,
  modelProjectHeatmapOption,
  partLabel,
  phaseLabel,
  sourceLabel,
  stackedBarOption,
  statusLabel,
  treemapOption,
  trendChartOption,
  weekHourHeatmapOption,
  type AreaMode,
  type CostBarRow,
  type DrillPieSlice,
  type StackedBarRow,
  type TrendMode,
} from '@/components/token-analytics/tokenAnalyticsCharts'
registerECharts()

const { t } = useI18n()
const router = useRouter()
const toast = useToast()
const { user } = useAuth()
const isAdmin = computed(() => !!user.value?.isAdmin)

const WINDOWS: TokenStatsWindow[] = ['24h', '7d', '30d', '90d', 'all']
const SOURCES: TokenSourceFilter[] = ['all', 'workflow', 'pm', 'studio']

const filters = ref<TokenStatsFilters>(defaultTokenStatsFilters())
const lineMode = ref<TrendMode>('total')
const areaMode = ref<AreaMode>('source')
type BarDimension = 'project' | 'workflow' | 'model' | 'nodeType'
const BAR_DIMENSIONS: BarDimension[] = ['project', 'workflow', 'model', 'nodeType']
const barDimension = ref<BarDimension>('project')
type CostDimension = 'model' | 'project' | 'workflow'
const costDimension = ref<CostDimension>('model')
const loading = ref(true)
const failed = ref(false)
const data = ref<GlobalTokenStats | null>(null)

const drillOpen = ref(false)
const drillPath = ref<DrillTarget[]>([])
const pricingOpen = ref(false)

let abort: AbortController | null = null
let generation = 0

const isEmpty = computed(() => !!data.value?.empty)
const currency = computed(() => data.value?.currency)
const filterCount = computed(() => activeFilterCount(filters.value))

const deltaLabel = computed(() => fmtDeltaPct(data.value?.kpi.deltaPct) ?? t('pages.tokenAnalytics.noPrev'))
const deltaClass = computed(() => {
  const d = data.value?.kpi.deltaPct
  if (d == null) return 'text-txt3'
  return d >= 0 ? 'text-emerald-600' : 'text-amber-600'
})
const costDeltaLabel = computed(() => fmtDeltaPct(data.value?.kpi.costDeltaPct))

const kpiCostPriced = computed(() => (data.value?.kpi.cost || 0) > 0)
const failedShare = computed(() => {
  const k = data.value?.kpi
  return k && k.total > 0 ? (k.failedTotal || 0) / k.total : 0
})

function dateInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function selectWindow(w: TokenStatsWindow) {
  filters.value = { ...filters.value, window: w, from: '', to: '' }
}

function selectCustomWindow() {
  if (filters.value.window === 'custom') return
  const end = new Date()
  const start = new Date(end)
  start.setDate(end.getDate() - 6)
  filters.value = { ...filters.value, window: 'custom', from: dateInputValue(start), to: dateInputValue(end) }
}

function setRange(field: 'from' | 'to', value: string) {
  const next = { ...filters.value, [field]: value }
  if (next.from && next.to && next.from > next.to) {
    if (field === 'from') next.to = next.from
    else next.from = next.to
  }
  filters.value = next
}

function setFilter<K extends keyof TokenStatsFilters>(key: K, value: TokenStatsFilters[K]) {
  filters.value = { ...filters.value, [key]: value }
}

function clearFilters() {
  const f = defaultTokenStatsFilters()
  filters.value = { ...f, window: filters.value.window, from: filters.value.from, to: filters.value.to, granularity: filters.value.granularity }
}

// ---------- Pies ----------

function namedSlices(
  rows: GlobalTokenStatsNamedBucket[] | undefined,
  label: (k: string) => string,
  colors: Record<string, string>,
  dim: 'source' | 'status' | null,
): DrillPieSlice[] {
  return (rows ?? []).filter((r) => r.total > 0).map((r, i) => {
    const key = r.key || r.name
    return {
      name: label(key),
      value: r.total,
      key,
      color: colors[key] || paletteColor(i),
      drill: dim ? { dim, key, name: label(key) } : undefined,
    }
  })
}

const pieSlices = computed(() => {
  const d = data.value
  if (!d || d.empty) return { source: [], comp: [], proj: [], model: [], node: [], wf: [], status: [], phase: [] }
  const c = d.composition
  const sources = d.sources?.length
    ? d.sources
    : [
        { key: 'workflow', name: 'workflow', total: d.kpi.workflowTotal },
        { key: 'pm', name: 'pm', total: d.kpi.pmTotal },
        { key: 'studio', name: 'studio', total: d.kpi.studioTotal || 0 },
      ]
  return {
    source: namedSlices(sources, (k) => sourceLabel(t, k), TOKEN_LEDGER_SOURCE_COLORS, 'source'),
    comp: TOKEN_PART_KEYS.map((key) => ({
      name: partLabel(t, key),
      value: bucketPart(c, key),
      key,
      color: TOKEN_PART_COLORS[key],
    })).filter((x) => x.value > 0),
    proj: d.projects.slice(0, 10).map((p, i) => ({
      name: p.name,
      value: p.total,
      key: p.projectId,
      color: paletteColor(i),
      drill: p.projectId ? { dim: 'project' as const, key: p.projectId, name: p.name } : undefined,
    })),
    model: d.modelRanking.filter((m) => !m.other).slice(0, 10).map((m, i) => ({
      name: m.name,
      value: m.total,
      key: m.modelKey,
      color: paletteColor(i),
      drill: m.modelKey ? { dim: 'model' as const, key: m.modelKey, name: m.name } : undefined,
    })),
    node: d.nodeTypes.map((n, i) => ({
      name: n.name,
      value: n.total,
      key: n.key,
      color: paletteColor(i),
      drill: n.key && n.key !== 'unknown' && !n.other ? { dim: 'nodeType' as const, key: n.key, name: n.name } : undefined,
    })),
    wf: d.workflows.filter((w) => !w.other && w.kind === 'workflow').slice(0, 8).map((w, i) => ({
      name: w.name,
      value: w.total,
      key: w.workflowId,
      color: paletteColor(i),
      drill: w.workflowId ? { dim: 'workflow' as const, key: w.workflowId, name: w.name } : undefined,
    })),
    status: namedSlices(d.statuses, (k) => statusLabel(t, k), TOKEN_LEDGER_STATUS_COLORS, 'status'),
    phase: namedSlices(d.phases, (k) => phaseLabel(t, k), TOKEN_LEDGER_PHASE_COLORS, null),
  }
})

type PieKey = keyof typeof pieSlices.value
const TOP_PIES: Array<{ key: PieKey; label: string; donut: boolean }> = [
  { key: 'source', label: 'sourcePie', donut: true },
  { key: 'comp', label: 'compPie', donut: false },
  { key: 'proj', label: 'projPie', donut: true },
  { key: 'model', label: 'modelPie', donut: true },
]
const DETAIL_PIES: Array<{ key: PieKey; label: string; donut: boolean }> = [
  { key: 'node', label: 'nodePie', donut: true },
  { key: 'wf', label: 'wfPie', donut: false },
  { key: 'status', label: 'statusPie', donut: true },
  { key: 'phase', label: 'phasePie', donut: true },
]
const pieOptions = computed(() => {
  const out = {} as Record<PieKey, ReturnType<typeof drillPieOption>>
  for (const cfg of [...TOP_PIES, ...DETAIL_PIES]) out[cfg.key] = drillPieOption(pieSlices.value[cfg.key], cfg.donut)
  return out
})

// ---------- Bars ----------

function partsOf(row: { inputTokens?: number; outputTokens?: number; cacheReadTokens?: number; cacheWriteTokens?: number }) {
  return {
    input: row.inputTokens || 0,
    output: row.outputTokens || 0,
    cacheRead: row.cacheReadTokens || 0,
    cacheWrite: row.cacheWriteTokens || 0,
  }
}

function workflowDrill(w: TokenStatsWorkflow): DrillTarget | undefined {
  if (w.other) return undefined
  if (w.kind === 'pm') return { dim: 'source', key: 'pm', name: sourceLabel(t, 'pm') }
  return w.workflowId ? { dim: 'workflow', key: w.workflowId, name: w.name } : undefined
}

function normalizeBarRows(dimension: BarDimension): StackedBarRow[] {
  const d = data.value
  if (!d) return []
  if (dimension === 'project') {
    return d.projects.slice(0, 10).map((p: GlobalTokenStatsProjectRow) => ({
      name: p.name,
      ...partsOf(p),
      other: false,
      drill: p.projectId ? { dim: 'project', key: p.projectId, name: p.name } : undefined,
    }))
  }
  if (dimension === 'workflow') {
    return d.workflows.map((w) => ({ name: w.name, ...partsOf(w), other: !!w.other, drill: workflowDrill(w) }))
  }
  if (dimension === 'model') {
    return d.modelRanking.map((m: TokenStatsModel) => ({
      name: m.name,
      ...partsOf(m),
      other: !!m.other,
      drill: m.modelKey && !m.other ? { dim: 'model', key: m.modelKey, name: m.name } : undefined,
    }))
  }
  return d.nodeTypes.map((n) => ({
    name: n.name,
    ...partsOf(n),
    other: !!n.other,
    drill: n.key && n.key !== 'unknown' && !n.other ? { dim: 'nodeType', key: n.key, name: n.name } : undefined,
  }))
}

const barRowsByDimension = computed<Record<BarDimension, StackedBarRow[]>>(() => ({
  project: normalizeBarRows('project'),
  workflow: normalizeBarRows('workflow'),
  model: normalizeBarRows('model'),
  nodeType: normalizeBarRows('nodeType'),
}))

const barDimensionEnabled = computed<Record<BarDimension, boolean>>(() => ({
  project: barRowsByDimension.value.project.length >= 2,
  workflow: barRowsByDimension.value.workflow.length >= 2,
  model: barRowsByDimension.value.model.length >= 2,
  nodeType: barRowsByDimension.value.nodeType.length >= 2,
}))

const hasComparableBarDimension = computed(() => BAR_DIMENSIONS.some((dim) => barDimensionEnabled.value[dim]))

function reconcileBarDimension() {
  if (barDimensionEnabled.value[barDimension.value]) return
  const fallback = BAR_DIMENSIONS.find((dim) => barDimensionEnabled.value[dim])
  if (fallback) barDimension.value = fallback
}

function selectBarDimension(dimension: string) {
  const dim = dimension as BarDimension
  if (barDimensionEnabled.value[dim]) barDimension.value = dim
}

const barOption = computed(() => stackedBarOption(barRowsByDimension.value[barDimension.value], t))

const costRows = computed<CostBarRow[]>(() => {
  const d = data.value
  if (!d) return []
  if (costDimension.value === 'model') {
    return d.modelRanking.filter((m) => !m.other).map((m) => ({
      name: m.name,
      cost: m.cost || 0,
      total: m.total,
      drill: m.modelKey ? { dim: 'model', key: m.modelKey, name: m.name } : undefined,
    }))
  }
  if (costDimension.value === 'project') {
    return d.projects.map((p) => ({
      name: p.name,
      cost: p.cost || 0,
      total: p.total,
      drill: p.projectId ? { dim: 'project', key: p.projectId, name: p.name } : undefined,
    }))
  }
  return d.workflows.filter((w) => !w.other).map((w) => ({ name: w.name, cost: w.cost || 0, total: w.total, drill: workflowDrill(w) }))
})
const costOption = computed(() => (kpiCostPriced.value ? costBarOption(costRows.value, currency.value, t) : null))

// ---------- Trend / area / heat / tree ----------

const lineOption = computed(() => trendChartOption(data.value, lineMode.value, t))
const areaOption = computed(() => areaChartOption(data.value, areaMode.value, t))
const heatOption = computed(() => modelProjectHeatmapOption(data.value))
const weekHourOption = computed(() => weekHourHeatmapOption(data.value?.weekHour, t))
const treeOption = computed(() => treemapOption(data.value?.tree, currency.value, t))

const lineModes = computed(() =>
  (['total', 'project', 'model', 'cost'] as const).map((m) => ({
    id: m,
    label: t(`pages.tokenAnalytics.lineModes.${m}`),
    disabled: m === 'cost' && !kpiCostPriced.value && !(data.value?.kpi.cacheReadTokens),
  })),
)
const areaModes = computed(() => (['source', 'comp', 'status'] as const).map((m) => ({ id: m, label: t(`pages.tokenAnalytics.areaModes.${m}`) })))
const barModes = computed(() =>
  BAR_DIMENSIONS.map((dim) => ({
    id: dim,
    label: t(`pages.tokenAnalytics.barDimensions.${dim}`),
    disabled: !barDimensionEnabled.value[dim],
    testId: `token-analytics-bar-dimension-${dim}`,
  })),
)
const costModes = computed(() =>
  (['model', 'project', 'workflow'] as const).map((dim) => ({ id: dim, label: t(`pages.tokenAnalytics.barDimensions.${dim}`) })),
)

// ---------- Load ----------

async function load() {
  const gen = ++generation
  abort?.abort()
  abort = new AbortController()
  loading.value = true
  failed.value = false
  try {
    const res = await api.getGlobalTokenStats(filtersToParams(filters.value), { signal: abort.signal })
    if (gen !== generation) return
    data.value = res
    reconcileBarDimension()
    if (lineMode.value === 'cost' && !(res.kpi.cost || 0) && !res.kpi.cacheReadTokens) lineMode.value = 'total'
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

function retry() {
  void load()
}

// ---------- Drill ----------

function openDrill(path: DrillTarget[]) {
  if (!path.length) return
  drillPath.value = path
  drillOpen.value = true
}

function onChartClick(params: unknown) {
  const path = drillFromChartEvent(params)
  if (path) openDrill(path)
}

function onDrillApply(next: TokenStatsFilters) {
  drillOpen.value = false
  filters.value = { ...next }
  toast.show(t('pages.tokenAnalytics.filterApplied', { name: drillPath.value.map((p) => p.name).join(' / ') }))
}

function goBoard(projectId: string) {
  void router.push({ path: `/projects/${projectId}`, query: { tab: 'board' } })
}

function goRun(runId: string) {
  void router.push(`/runs/${runId}`)
}

// ---------- Tables ----------

const modelRows = computed(() => (data.value?.modelRanking ?? []).filter((m) => m.total > 0))

function share(total: number): string {
  const all = data.value?.kpi.total || 0
  return all > 0 ? fmtPct(total / all) : '—'
}

function exportProjects() {
  const rows = (data.value?.projects ?? []).map((p) => [
    p.name, p.projectId, p.total, p.inputTokens, p.outputTokens, p.cacheReadTokens || 0, p.cacheWriteTokens || 0, p.runCount || 0, p.cost || 0,
    p.deltaPct ?? '',
  ])
  downloadCsv('token-usage-projects.csv', toCsv(['project', 'project_id', 'total', 'input', 'output', 'cache_read', 'cache_write', 'runs', 'cost', 'delta_pct'], rows))
}

function exportModels() {
  const rows = modelRows.value.map((m) => [
    m.name, m.modelKey || '', m.total, m.inputTokens || 0, m.outputTokens || 0, m.cacheReadTokens || 0, m.cacheWriteTokens || 0, m.cost || 0,
  ])
  downloadCsv('token-usage-models.csv', toCsv(['model', 'model_key', 'total', 'input', 'output', 'cache_read', 'cache_write', 'cost'], rows))
}

function exportRuns() {
  const rows = (data.value?.topRuns ?? []).map((r) => [
    runTitleReadable(r.title), r.runId, r.projectName, r.workflowName, r.modelName || r.modelKey, r.status || '', r.nodeCount || 0,
    r.total, r.inputTokens || 0, r.outputTokens || 0, r.cacheReadTokens || 0, r.cacheWriteTokens || 0, r.cost || 0, r.firstAt || '',
  ])
  downloadCsv(
    'token-usage-runs.csv',
    toCsv(['run', 'run_id', 'project', 'workflow', 'model', 'status', 'nodes', 'total', 'input', 'output', 'cache_read', 'cache_write', 'cost', 'first_at'], rows),
  )
}

const RUN_TITLE_MAX_DISPLAY = 60

/** Session-only widths for the "highest-usage runs" table (g1.2). Refresh restores defaults. */
type RunsColKey = 'run' | 'project' | 'model' | 'total' | 'cost'

const RUNS_COL_DEFS = [
  { key: 'run' as const, labelKey: 'pages.tokenAnalytics.tables.colRun', align: 'left' as const, defaultWidth: 220, minWidth: 80 },
  { key: 'project' as const, labelKey: 'pages.tokenAnalytics.tables.colProject', align: 'left' as const, defaultWidth: 140, minWidth: 64 },
  { key: 'model' as const, labelKey: 'pages.tokenAnalytics.tables.colModel', align: 'left' as const, defaultWidth: 140, minWidth: 64 },
  { key: 'total' as const, labelKey: 'pages.tokenAnalytics.tables.colTotal', align: 'right' as const, defaultWidth: 88, minWidth: 56 },
  { key: 'cost' as const, labelKey: 'pages.tokenAnalytics.tables.colCost', align: 'right' as const, defaultWidth: 80, minWidth: 56 },
] as const

const RUNS_COL_MIN: Record<RunsColKey, number> = Object.fromEntries(RUNS_COL_DEFS.map((c) => [c.key, c.minWidth])) as Record<RunsColKey, number>

const runsColWidths = ref<Record<RunsColKey, number>>(
  Object.fromEntries(RUNS_COL_DEFS.map((c) => [c.key, c.defaultWidth])) as Record<RunsColKey, number>,
)
const runsColDragging = ref<RunsColKey | null>(null)
let runsColDragStartX = 0
let runsColDragStartW = 0

function clampRunsColWidth(key: RunsColKey, width: number): number {
  return Math.max(RUNS_COL_MIN[key], width)
}

function runsColSashLabel(key: RunsColKey): string {
  const def = RUNS_COL_DEFS.find((c) => c.key === key)!
  return t('pages.tokenAnalytics.tables.resizeCol', { col: t(def.labelKey) })
}

function onRunsColSashPointerDown(key: RunsColKey, e: PointerEvent) {
  runsColDragging.value = key
  runsColDragStartX = e.clientX
  runsColDragStartW = runsColWidths.value[key]
  const el = e.currentTarget as HTMLElement
  if (typeof el.setPointerCapture === 'function') el.setPointerCapture(e.pointerId)
  e.preventDefault()
  e.stopPropagation()
}

function onRunsColSashPointerMove(e: PointerEvent) {
  const key = runsColDragging.value
  if (!key) return
  runsColWidths.value[key] = clampRunsColWidth(key, runsColDragStartW + (e.clientX - runsColDragStartX))
  e.preventDefault()
}

function onRunsColSashPointerUp() {
  runsColDragging.value = null
}

function runTitleReadable(raw: string): string {
  return displayRunTitle(raw).replace(/\s+/g, ' ').trim()
}

function runTitleDisplay(raw: string): string {
  return truncateText(runTitleReadable(raw), RUN_TITLE_MAX_DISPLAY)
}

function runTitleTooltip(raw: string): string | undefined {
  const readable = runTitleReadable(raw)
  return readable.length > RUN_TITLE_MAX_DISPLAY ? readable : undefined
}

const pricingKnownModels = computed(() => {
  const keys = new Set<string>()
  for (const m of data.value?.filterOptions.models ?? []) if (m.key) keys.add(m.key)
  for (const m of data.value?.unpricedModels ?? []) if (m) keys.add(m)
  return [...keys].sort()
})

onMounted(() => {
  void load()
})

onBeforeUnmount(() => {
  abort?.abort()
  runsColDragging.value = null
})

watch(() => JSON.stringify(filters.value), () => void load())
</script>

<template>
  <div class="flex h-full min-h-0 flex-col" data-testid="token-analytics-page">
    <div class="token-analytics-main min-h-0 flex-1 overflow-auto px-5 py-4 pb-14">
      <div class="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 class="m-0 flex items-center gap-2 text-[22px] font-semibold">
            {{ t('pages.tokenAnalytics.title') }}
            <span
              v-if="loading && data"
              class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-accent/30 border-t-accent"
              data-testid="token-analytics-refreshing"
              :aria-label="t('pages.tokenAnalytics.loading')"
            />
          </h1>
          <p class="mt-1 text-xs text-txt3" v-html="t('pages.tokenAnalytics.subtitle')" />
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <div
            class="flex gap-1 rounded-lg bg-elevated p-1"
            role="group"
            :aria-label="t('pages.board.tokenStats.windowAria')"
            data-testid="token-analytics-window"
          >
            <button
              v-for="w in WINDOWS"
              :key="w"
              type="button"
              class="rounded px-2.5 py-1.5 text-xs"
              :class="filters.window === w ? 'bg-surface font-semibold text-txt shadow-sm' : 'text-txt3'"
              :data-testid="`token-analytics-window-${w}`"
              @click="selectWindow(w)"
            >
              {{ t(`pages.board.tokenStats.windows.${w}`) }}
            </button>
          </div>
          <div class="flex items-center gap-1 rounded-lg bg-elevated p-1">
            <button
              type="button"
              class="rounded px-2.5 py-1.5 text-xs"
              :class="filters.window === 'custom' ? 'bg-surface font-semibold text-txt shadow-sm' : 'text-txt3'"
              data-testid="token-analytics-window-custom"
              @click="selectCustomWindow"
            >
              {{ t('pages.tokenAnalytics.windowCustom') }}
            </button>
            <template v-if="filters.window === 'custom'">
              <input
                type="date"
                class="rounded border border-line bg-surface px-1.5 py-1 text-xs text-txt2"
                :value="filters.from"
                :max="filters.to || undefined"
                :aria-label="t('pages.tokenAnalytics.rangeFrom')"
                data-testid="token-analytics-range-from"
                @change="setRange('from', ($event.target as HTMLInputElement).value)"
              />
              <span class="text-xs text-txt3">–</span>
              <input
                type="date"
                class="rounded border border-line bg-surface px-1.5 py-1 text-xs text-txt2"
                :value="filters.to"
                :min="filters.from || undefined"
                :aria-label="t('pages.tokenAnalytics.rangeTo')"
                data-testid="token-analytics-range-to"
                @change="setRange('to', ($event.target as HTMLInputElement).value)"
              />
            </template>
          </div>
          <select
            class="rounded-lg border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
            :value="filters.granularity"
            :aria-label="t('pages.tokenAnalytics.granularity.aria')"
            data-testid="token-analytics-granularity"
            @change="setFilter('granularity', ($event.target as HTMLSelectElement).value as TokenStatsFilters['granularity'])"
          >
            <option value="">{{ t('pages.tokenAnalytics.granularity.auto') }}</option>
            <option value="hour">{{ t('pages.tokenAnalytics.granularity.hour') }}</option>
            <option value="day">{{ t('pages.tokenAnalytics.granularity.day') }}</option>
            <option value="week">{{ t('pages.tokenAnalytics.granularity.week') }}</option>
          </select>
          <button
            type="button"
            class="rounded-lg border border-line bg-surface px-2.5 py-1.5 text-xs text-txt2 hover:bg-elevated"
            data-testid="token-analytics-pricing"
            @click="pricingOpen = true"
          >
            {{ t('pages.tokenAnalytics.pricing.button') }}
          </button>
          <button
            type="button"
            class="rounded-lg border border-line bg-surface px-2.5 py-1.5 text-xs text-txt2 hover:bg-elevated"
            data-testid="token-analytics-refresh"
            @click="retry"
          >
            {{ t('pages.tokenAnalytics.refresh') }}
          </button>
        </div>
      </div>

      <div class="mb-3 flex flex-wrap items-center gap-2" data-testid="token-analytics-filters">
        <button
          v-for="s in SOURCES"
          :key="s"
          type="button"
          class="chip border px-2.5 py-1.5 text-xs"
          :class="filters.source === s ? 'border-accent/40 bg-accent-dim font-semibold text-accent-2' : 'border-line bg-surface text-txt2'"
          :data-testid="`token-analytics-source-${s}`"
          @click="setFilter('source', s)"
        >
          {{ s === 'all' ? t('pages.tokenAnalytics.sourceAll') : s === 'workflow' ? t('pages.tokenAnalytics.sourceWorkflow') : s === 'pm' ? t('pages.tokenAnalytics.sourcePm') : t('pages.tokenAnalytics.sourceStudio') }}
        </button>
        <select
          :value="filters.projectId"
          class="max-w-[180px] rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
          data-testid="token-analytics-filter-project"
          @change="setFilter('projectId', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">{{ t('pages.tokenAnalytics.projectAll') }}</option>
          <option v-for="p in data?.filterOptions.projects || []" :key="p.key" :value="p.key">{{ p.name }}</option>
        </select>
        <select
          :value="filters.modelKey"
          class="max-w-[180px] rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
          data-testid="token-analytics-filter-model"
          @change="setFilter('modelKey', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">{{ t('pages.tokenAnalytics.modelAll') }}</option>
          <option v-for="m in data?.filterOptions.models || []" :key="m.key" :value="m.key">{{ m.name }}</option>
        </select>
        <select
          :value="filters.workflowId"
          class="max-w-[180px] rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
          data-testid="token-analytics-filter-workflow"
          @change="setFilter('workflowId', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">{{ t('pages.tokenAnalytics.workflowAll') }}</option>
          <option v-for="w in data?.filterOptions.workflows || []" :key="w.key" :value="w.key">{{ w.name }}</option>
        </select>
        <select
          :value="filters.nodeType"
          class="max-w-[160px] rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
          data-testid="token-analytics-filter-node-type"
          @change="setFilter('nodeType', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">{{ t('pages.tokenAnalytics.nodeTypeAll') }}</option>
          <option v-for="n in data?.filterOptions.nodeTypes || []" :key="n.key" :value="n.key">{{ n.name }}</option>
        </select>
        <select
          :value="filters.status"
          class="rounded border border-line bg-surface px-2 py-1.5 text-xs text-txt2"
          data-testid="token-analytics-filter-status"
          @change="setFilter('status', ($event.target as HTMLSelectElement).value as TokenStatsFilters['status'])"
        >
          <option value="">{{ t('pages.tokenAnalytics.statusAll') }}</option>
          <option v-for="s in (['ok', 'failed', 'cancelled'] as const)" :key="s" :value="s">{{ statusLabel(t, s) }}</option>
        </select>
        <span
          v-if="filters.runId"
          class="chip flex items-center gap-1 border border-accent/40 bg-accent-dim px-2.5 py-1.5 text-xs text-accent-2"
          data-testid="token-analytics-filter-run"
        >
          {{ t('pages.tokenAnalytics.drill.dims.run') }}：{{ filters.runId.slice(0, 8) }}
          <button type="button" :aria-label="t('pages.tokenAnalytics.clearFilters')" @click="setFilter('runId', '')">×</button>
        </span>
        <button
          type="button"
          class="rounded border border-line bg-surface px-2.5 py-1.5 text-xs text-txt2"
          :class="filterCount ? '' : 'opacity-60'"
          data-testid="token-analytics-clear"
          @click="clearFilters"
        >
          {{ t('pages.tokenAnalytics.clearFilters') }}<span v-if="filterCount"> ({{ filterCount }})</span>
        </button>
      </div>

      <div v-if="loading && !data" data-testid="token-analytics-loading" :aria-label="t('pages.tokenAnalytics.loading')">
        <div class="mb-3 grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6">
          <div v-for="i in 6" :key="i" class="token-skeleton h-[92px] rounded-xl" />
        </div>
        <div class="token-skeleton mb-3 h-[280px] rounded-xl" />
        <div class="grid grid-cols-1 gap-2.5 lg:grid-cols-2">
          <div class="token-skeleton h-[220px] rounded-xl" />
          <div class="token-skeleton h-[220px] rounded-xl" />
        </div>
      </div>
      <div v-else-if="failed" class="py-16 text-center" data-testid="token-analytics-error">
        <p class="text-sm text-txt3">{{ t('pages.tokenAnalytics.loadFailed') }}</p>
        <button type="button" class="mt-2 text-sm text-accent-2" @click="retry">{{ t('pages.tokenAnalytics.retry') }}</button>
      </div>
      <div v-else-if="isEmpty" class="py-16 text-center" data-testid="token-analytics-empty">
        <p class="font-medium">{{ t('pages.tokenAnalytics.emptyTitle') }}</p>
        <p class="mt-1 text-sm text-txt3">{{ t('pages.tokenAnalytics.emptyHint') }}</p>
      </div>
      <div v-else-if="data" class="transition-opacity" :class="loading ? 'opacity-70' : ''">
        <section id="overview" class="mb-3 grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6" data-testid="token-analytics-kpis">
          <div
            class="rounded-xl border border-line bg-surface p-3.5 shadow-sm"
            data-testid="token-analytics-kpi-total"
            :data-token-count="data.kpi.total"
          >
            <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiTotal') }}</div>
            <div class="mt-1 text-[22px] font-bold tabular-nums" :title="fmtTokenCount(data.kpi.total)">{{ fmtCompactTokenCount(data.kpi.total) }}</div>
            <div class="mt-1 text-[11px]" :class="deltaClass">{{ deltaLabel }}</div>
          </div>
          <div
            class="token-analytics-kpi-merge relative rounded-xl border border-line bg-surface p-3.5 shadow-sm outline-none"
            tabindex="0"
            data-testid="token-analytics-kpi-merge"
          >
            <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiInOutCache') }}</div>
            <div class="mt-2.5 flex justify-between gap-3 text-[13px] text-txt2">
              <span>{{ t('pages.executionTimeline.partInput') }}</span>
              <b
                class="text-base font-bold tabular-nums text-txt"
                data-testid="token-analytics-kpi-input"
                :data-token-count="data.kpi.inputTokens + data.kpi.cacheReadTokens + data.kpi.cacheWriteTokens"
              >
                {{ fmtCompactTokenCount(data.kpi.inputTokens + data.kpi.cacheReadTokens + data.kpi.cacheWriteTokens) }}
              </b>
            </div>
            <div class="mt-2.5 flex justify-between gap-3 text-[13px] text-txt2">
              <span>{{ t('pages.executionTimeline.partOutput') }}</span>
              <b
                class="text-base font-bold tabular-nums text-txt"
                data-testid="token-analytics-kpi-output"
                :data-token-count="data.kpi.outputTokens"
              >
                {{ fmtCompactTokenCount(data.kpi.outputTokens) }}
              </b>
            </div>
            <div
              class="token-analytics-kpi-tip absolute left-3.5 top-[calc(100%-8px)] z-10 hidden min-w-[200px] rounded-lg border border-line bg-elevated p-2.5 text-xs shadow-md"
              data-testid="token-analytics-kpi-detail"
            >
              <div class="flex justify-between gap-4 py-0.5">
                <span>{{ t('pages.executionTimeline.partInput') }}</span>
                <span class="tabular-nums">{{ fmtTokenCount(data.kpi.inputTokens) }}</span>
              </div>
              <div class="flex justify-between gap-4 py-0.5">
                <span>{{ t('pages.executionTimeline.partOutput') }}</span>
                <span class="tabular-nums">{{ fmtTokenCount(data.kpi.outputTokens) }}</span>
              </div>
              <div class="flex justify-between gap-4 py-0.5">
                <span>{{ t('pages.executionTimeline.partCacheRead') }}</span>
                <span class="tabular-nums">{{ fmtTokenCount(data.kpi.cacheReadTokens) }}</span>
              </div>
              <div class="flex justify-between gap-4 py-0.5">
                <span>{{ t('pages.executionTimeline.partCacheWrite') }}</span>
                <span class="tabular-nums">{{ fmtTokenCount(data.kpi.cacheWriteTokens) }}</span>
              </div>
            </div>
          </div>
          <div class="rounded-xl border border-line bg-surface p-3.5 shadow-sm" data-testid="token-analytics-kpi-scope">
            <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiScope') }}</div>
            <div class="mt-1 text-[22px] font-bold">{{ t('pages.tokenAnalytics.kpiProjects', { n: data.kpi.projectCount }) }}</div>
            <div class="mt-1 text-[11px] text-txt3">
              {{ t('pages.tokenAnalytics.kpiRuns', { n: data.kpi.runCount }) }} ·
              {{ t('pages.tokenAnalytics.kpiModels', { n: data.kpi.modelCount }) }}
            </div>
            <div v-if="data.kpi.avgPerRun" class="mt-0.5 text-[11px] text-txt3">
              {{ t('pages.tokenAnalytics.kpiAvgPerRun', { n: fmtCompactTokenCount(Math.round(data.kpi.avgPerRun)) }) }}
            </div>
          </div>
          <div class="rounded-xl border border-line bg-surface p-3.5 shadow-sm" data-testid="token-analytics-kpi-cost">
            <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiCost') }}</div>
            <template v-if="kpiCostPriced">
              <div class="mt-1 text-[22px] font-bold tabular-nums">{{ fmtCost(data.kpi.cost, currency) }}</div>
              <div class="mt-1 text-[11px]" :class="costDeltaLabel ? ((data.kpi.costDeltaPct || 0) >= 0 ? 'text-emerald-600' : 'text-amber-600') : 'text-txt3'">
                {{ costDeltaLabel ?? t('pages.tokenAnalytics.noPrev') }}
              </div>
              <button
                v-if="data.unpricedModels?.length"
                type="button"
                class="mt-0.5 text-[11px] text-warn hover:underline"
                @click="pricingOpen = true"
              >{{ t('pages.tokenAnalytics.kpiCostUnpricedModels', { n: data.unpricedModels.length }) }}</button>
            </template>
            <template v-else>
              <div class="mt-1 text-[15px] font-semibold text-txt3">{{ t('pages.tokenAnalytics.kpiCostUnpriced') }}</div>
              <button type="button" class="mt-1 text-[11px] text-accent-2 hover:underline" @click="pricingOpen = true">
                {{ t('pages.tokenAnalytics.kpiCostConfigure') }}
              </button>
            </template>
          </div>
          <div
            class="rounded-xl border border-line bg-surface p-3.5 shadow-sm"
            data-testid="token-analytics-kpi-cache"
            :title="`${fmtTokenCount(data.kpi.cacheReadTokens)} / ${fmtTokenCount(data.kpi.cacheWriteTokens)}`"
          >
            <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.cacheHit') }}</div>
            <div class="mt-1 text-[22px] font-bold tabular-nums">{{ fmtPct(data.kpi.cacheHitRate) }}</div>
            <div class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-elevated">
              <div class="h-full rounded-full bg-[#22a6b3]" :style="{ width: `${Math.min(100, (data.kpi.cacheHitRate || 0) * 100)}%` }" />
            </div>
            <div class="mt-1 text-[11px] text-txt3">
              {{ t('pages.tokenAnalytics.kpiCacheDetail', { read: fmtCompactTokenCount(data.kpi.cacheReadTokens), write: fmtCompactTokenCount(data.kpi.cacheWriteTokens) }) }}
            </div>
          </div>
          <button
            type="button"
            class="rounded-xl border border-line bg-surface p-3.5 text-left shadow-sm hover:border-err/50"
            data-testid="token-analytics-kpi-failed"
            :disabled="!data.kpi.failedTotal"
            @click="openDrill([{ dim: 'status', key: 'failed', name: statusLabel(t, 'failed') }])"
          >
            <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiFailed') }}</div>
            <div class="mt-1 text-[22px] font-bold tabular-nums" :class="data.kpi.failedTotal ? 'text-err' : ''">
              {{ fmtCompactTokenCount(data.kpi.failedTotal || 0) }}
            </div>
            <div class="mt-1 text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiFailedShare', { pct: fmtPct(failedShare) }) }}</div>
          </button>
        </section>

        <TokenChartCard
          id="lines"
          class="mb-3"
          data-testid="token-analytics-lines"
          :title="t('pages.tokenAnalytics.charts.lines')"
          :hint="t('pages.tokenAnalytics.charts.linesHint')"
          :modes="lineModes"
          :mode="lineMode"
          @update:mode="lineMode = $event as TrendMode"
        >
          <div class="token-analytics-plot mt-2 h-[260px] overflow-visible" data-testid="token-analytics-plot-lines">
            <VChart v-if="lineOption" :option="lineOption" autoresize class="h-full w-full" @click="onChartClick" />
          </div>
        </TokenChartCard>

        <TokenChartCard
          id="pies"
          class="mb-3"
          data-testid="token-analytics-pies"
          :title="t('pages.tokenAnalytics.charts.pies')"
          :hint="t('pages.tokenAnalytics.charts.piesHint')"
        >
          <div class="mt-2 grid grid-cols-1 gap-2.5 sm:grid-cols-2 xl:grid-cols-4">
            <div v-for="cfg in TOP_PIES" :key="cfg.key" class="text-center">
              <VChart
                v-if="pieOptions[cfg.key]"
                :option="pieOptions[cfg.key]!"
                autoresize
                class="mx-auto h-[168px] w-full"
                @click="onChartClick"
              />
              <p class="m-0 mt-1.5 text-[13px] font-semibold">{{ t(`pages.tokenAnalytics.charts.${cfg.label}`) }}</p>
            </div>
          </div>
        </TokenChartCard>

        <div class="mb-3 grid grid-cols-1 gap-2.5" :class="costOption && hasComparableBarDimension ? 'xl:grid-cols-[3fr_2fr]' : ''">
          <TokenChartCard
            v-if="hasComparableBarDimension"
            id="bars"
            data-testid="token-analytics-bars"
            :title="t('pages.tokenAnalytics.charts.bars')"
            :hint="t(`pages.tokenAnalytics.charts.barsHints.${barDimension}`)"
            :modes="barModes"
            :mode="barDimension"
            @update:mode="selectBarDimension"
          >
            <div class="token-analytics-plot mt-2 h-[240px] overflow-visible" data-testid="token-analytics-plot-bars">
              <VChart v-if="barOption" :option="barOption" autoresize class="h-full w-full" @click="onChartClick" />
            </div>
          </TokenChartCard>
          <TokenChartCard
            v-if="costOption"
            data-testid="token-analytics-cost"
            :title="t('pages.tokenAnalytics.charts.costRank')"
            :hint="t('pages.tokenAnalytics.charts.costRankHint')"
            :modes="costModes"
            :mode="costDimension"
            @update:mode="costDimension = $event as CostDimension"
          >
            <div class="token-analytics-plot mt-2 h-[240px] overflow-visible" data-testid="token-analytics-plot-cost">
              <VChart :option="costOption" autoresize class="h-full w-full" @click="onChartClick" />
            </div>
          </TokenChartCard>
        </div>

        <TokenChartCard
          id="area"
          class="mb-3"
          data-testid="token-analytics-area"
          :title="t('pages.tokenAnalytics.charts.area')"
          :hint="t('pages.tokenAnalytics.charts.areaHint')"
          :modes="areaModes"
          :mode="areaMode"
          @update:mode="areaMode = $event as AreaMode"
        >
          <div class="token-analytics-plot mt-2 h-[220px] overflow-visible" data-testid="token-analytics-plot-area">
            <VChart v-if="areaOption" :option="areaOption" autoresize class="h-full w-full" @click="onChartClick" />
          </div>
        </TokenChartCard>

        <div class="mb-3 grid grid-cols-1 gap-2.5 xl:grid-cols-2">
          <TokenChartCard id="heat" data-testid="token-analytics-heat" :title="t('pages.tokenAnalytics.charts.heat')" :hint="t('pages.tokenAnalytics.charts.heatHint')">
            <div class="token-analytics-plot mt-2 h-[260px] overflow-visible" data-testid="token-analytics-plot-heat">
              <VChart v-if="heatOption" :option="heatOption" autoresize class="h-full w-full" @click="onChartClick" />
            </div>
          </TokenChartCard>
          <TokenChartCard data-testid="token-analytics-weekhour" :title="t('pages.tokenAnalytics.charts.weekHour')" :hint="t('pages.tokenAnalytics.charts.weekHourHint')">
            <div class="token-analytics-plot mt-2 h-[260px] overflow-visible" data-testid="token-analytics-plot-weekhour">
              <VChart v-if="weekHourOption" :option="weekHourOption" autoresize class="h-full w-full" />
              <p v-else class="py-20 text-center text-xs text-txt3">{{ t('pages.tokenAnalytics.noData') }}</p>
            </div>
          </TokenChartCard>
        </div>

        <TokenChartCard
          id="nodeWf"
          class="mb-3"
          data-testid="token-analytics-node-wf"
          :title="t('pages.tokenAnalytics.charts.nodeWf')"
          :hint="t('pages.tokenAnalytics.charts.piesHint')"
        >
          <div class="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-4">
            <div v-for="cfg in DETAIL_PIES" :key="cfg.key">
              <VChart v-if="pieOptions[cfg.key]" :option="pieOptions[cfg.key]!" autoresize class="h-[168px] w-full" @click="onChartClick" />
              <p v-else class="flex h-[168px] items-center justify-center text-xs text-txt3">{{ t('pages.tokenAnalytics.noData') }}</p>
              <p class="m-0 text-center text-[13px] font-semibold">{{ t(`pages.tokenAnalytics.charts.${cfg.label}`) }}</p>
            </div>
          </div>
        </TokenChartCard>

        <TokenChartCard
          v-if="treeOption"
          class="mb-3"
          data-testid="token-analytics-treemap"
          :title="t('pages.tokenAnalytics.charts.treemap')"
          :hint="t('pages.tokenAnalytics.charts.treemapHint')"
        >
          <div class="token-analytics-plot mt-2 h-[320px] overflow-visible" data-testid="token-analytics-plot-treemap">
            <VChart :option="treeOption" autoresize class="h-full w-full" @click="onChartClick" />
          </div>
        </TokenChartCard>

        <section id="tables" class="grid grid-cols-1 gap-2.5 xl:grid-cols-2">
          <TokenChartCard class="max-h-[340px] min-h-0" data-testid="token-analytics-projects-table" :title="t('pages.tokenAnalytics.tables.projects')">
            <template #actions>
              <button type="button" class="text-[11px] text-txt3 hover:text-accent-2" data-testid="token-analytics-export-projects" @click="exportProjects">
                {{ t('pages.tokenAnalytics.exportCsv') }}
              </button>
            </template>
            <div class="token-analytics-table-scroll mt-2 min-h-0 flex-1 overflow-auto">
              <table class="w-full border-collapse text-xs">
                <thead class="sticky top-0 bg-surface">
                  <tr class="text-txt3">
                    <th class="border-b border-line py-1.5 text-left font-semibold">{{ t('pages.tokenAnalytics.tables.colProject') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colTotal') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colInput') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colOutput') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colRuns') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colCost') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colDelta') }}</th>
                    <th class="w-10 border-b border-line" />
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="p in data.projects" :key="p.projectId" class="hover:bg-accent-dim/40">
                    <td class="max-w-[200px] truncate border-b border-line/60 py-1.5">
                      <button v-if="p.projectId && !p.deleted" type="button" class="text-accent-2" @click="goBoard(p.projectId)">{{ p.name }}</button>
                      <span v-else class="text-txt3">{{ p.name }}<span v-if="p.deleted"> · {{ t('pages.tokenAnalytics.deleted') }}</span></span>
                    </td>
                    <td class="border-b border-line/60 py-1.5 text-right font-semibold tabular-nums" :title="fmtTokenCount(p.total)">{{ fmtCompactTokenCount(p.total) }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">{{ fmtCompactTokenCount(p.inputTokens) }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">{{ fmtCompactTokenCount(p.outputTokens) }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">{{ p.runCount ?? '—' }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">{{ p.cost ? fmtCost(p.cost, currency) : '—' }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">
                      {{ p.deltaPct != null ? `${p.deltaPct >= 0 ? '+' : ''}${p.deltaPct.toFixed(1)}%` : t('pages.tokenAnalytics.noPrev') }}
                    </td>
                    <td class="border-b border-line/60 py-1.5 text-right">
                      <button
                        v-if="p.projectId"
                        type="button"
                        class="text-[11px] text-txt3 hover:text-accent-2"
                        :data-testid="`token-analytics-project-detail-${p.projectId}`"
                        @click="openDrill([{ dim: 'project', key: p.projectId, name: p.name }])"
                      >{{ t('pages.tokenAnalytics.tables.details') }}</button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </TokenChartCard>

          <TokenChartCard class="max-h-[340px] min-h-0" data-testid="token-analytics-models-table" :title="t('pages.tokenAnalytics.tables.models')">
            <template #actions>
              <button type="button" class="text-[11px] text-txt3 hover:text-accent-2" data-testid="token-analytics-export-models" @click="exportModels">
                {{ t('pages.tokenAnalytics.exportCsv') }}
              </button>
            </template>
            <div class="token-analytics-table-scroll mt-2 min-h-0 flex-1 overflow-auto">
              <table class="w-full border-collapse text-xs">
                <thead class="sticky top-0 bg-surface">
                  <tr class="text-txt3">
                    <th class="border-b border-line py-1.5 text-left font-semibold">{{ t('pages.tokenAnalytics.tables.colModel') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colTotal') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colShare') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colInput') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colOutput') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colCache') }}</th>
                    <th class="border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colCost') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="m in modelRows" :key="m.modelKey || m.name" class="hover:bg-accent-dim/40">
                    <td class="max-w-[200px] truncate border-b border-line/60 py-1.5">
                      <button
                        v-if="m.modelKey && !m.other"
                        type="button"
                        class="truncate hover:text-accent-2 hover:underline"
                        :class="m.unknown ? 'text-txt3' : ''"
                        @click="openDrill([{ dim: 'model', key: m.modelKey!, name: m.name }])"
                      >{{ m.name }}</button>
                      <span v-else class="text-txt3">{{ m.name }}</span>
                    </td>
                    <td class="border-b border-line/60 py-1.5 text-right font-semibold tabular-nums" :title="fmtTokenCount(m.total)">{{ fmtCompactTokenCount(m.total) }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums text-txt3">{{ share(m.total) }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">{{ fmtCompactTokenCount(m.inputTokens || 0) }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">{{ fmtCompactTokenCount(m.outputTokens || 0) }}</td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">
                      {{ fmtCompactTokenCount(m.cacheReadTokens || 0) }} / {{ fmtCompactTokenCount(m.cacheWriteTokens || 0) }}
                    </td>
                    <td class="border-b border-line/60 py-1.5 text-right tabular-nums">{{ m.cost ? fmtCost(m.cost, currency) : '—' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </TokenChartCard>

          <div class="flex max-h-[340px] min-h-0 flex-col rounded-xl border border-line bg-surface p-3.5 shadow-sm xl:col-span-2" data-testid="token-analytics-runs-table">
            <div class="mb-2 flex shrink-0 items-center justify-between gap-2">
              <h2 class="m-0 text-sm font-semibold">{{ t('pages.tokenAnalytics.tables.runs') }}</h2>
              <button type="button" class="text-[11px] text-txt3 hover:text-accent-2" data-testid="token-analytics-export-runs" @click="exportRuns">
                {{ t('pages.tokenAnalytics.exportCsv') }}
              </button>
            </div>
            <div class="token-analytics-table-scroll min-h-0 flex-1 overflow-auto">
              <!-- plan coverage: g1.1 / g1.2 / g1.3 — header sashes, session col widths, truncate without misfire -->
              <table
                class="token-analytics-runs-grid min-w-full border-collapse text-xs"
                :class="runsColDragging ? 'select-none' : ''"
                data-testid="token-analytics-runs-grid"
              >
                <colgroup>
                  <col
                    v-for="col in RUNS_COL_DEFS"
                    :key="col.key"
                    :data-testid="`token-analytics-runs-col-${col.key}`"
                    :style="{ width: `${runsColWidths[col.key]}px` }"
                  />
                  <col style="width: 56px" />
                </colgroup>
                <thead>
                  <tr class="text-txt3">
                    <th
                      v-for="col in RUNS_COL_DEFS"
                      :key="col.key"
                      class="relative select-none border-b border-line py-1.5 font-semibold"
                      :class="col.align === 'right' ? 'text-right' : 'text-left'"
                    >
                      {{ t(col.labelKey) }}
                      <div
                        class="token-analytics-runs-col-sash absolute top-0 z-[2] h-full w-1.5 cursor-col-resize touch-none hover:bg-accent"
                        :class="runsColDragging === col.key ? 'bg-accent' : ''"
                        role="separator"
                        aria-orientation="vertical"
                        :aria-valuemin="col.minWidth"
                        :aria-valuenow="runsColWidths[col.key]"
                        :aria-label="runsColSashLabel(col.key)"
                        :title="runsColSashLabel(col.key)"
                        :data-testid="`token-analytics-runs-col-sash-${col.key}`"
                        @pointerdown="onRunsColSashPointerDown(col.key, $event)"
                        @pointermove="onRunsColSashPointerMove"
                        @pointerup="onRunsColSashPointerUp"
                        @pointercancel="onRunsColSashPointerUp"
                      />
                    </th>
                    <th class="border-b border-line" />
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="r in data.topRuns" :key="r.runId" class="hover:bg-accent-dim/40">
                    <td class="overflow-hidden text-ellipsis whitespace-nowrap border-b border-line/60 py-1.5">
                      <button
                        type="button"
                        class="block w-full max-w-full truncate text-left text-accent-2"
                        :title="runTitleTooltip(r.title)"
                        @click="goRun(r.runId)"
                      >{{ runTitleDisplay(r.title) }}</button>
                    </td>
                    <td class="overflow-hidden text-ellipsis whitespace-nowrap border-b border-line/60 py-1.5">{{ r.projectName }}</td>
                    <td class="overflow-hidden text-ellipsis whitespace-nowrap border-b border-line/60 py-1.5">{{ r.modelName || r.modelKey }}</td>
                    <td class="overflow-hidden whitespace-nowrap border-b border-line/60 py-1.5 text-right tabular-nums" :title="fmtTokenCount(r.total)">
                      {{ fmtCompactTokenCount(r.total) }}
                    </td>
                    <td class="overflow-hidden whitespace-nowrap border-b border-line/60 py-1.5 text-right tabular-nums">
                      {{ r.cost ? fmtCost(r.cost, currency) : '—' }}
                    </td>
                    <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right">
                      <button
                        type="button"
                        class="text-[11px] text-txt3 hover:text-accent-2"
                        :data-testid="`token-analytics-run-detail-${r.runId}`"
                        @click="openDrill([{ dim: 'run', key: r.runId, name: runTitleDisplay(r.title) }])"
                      >{{ t('pages.tokenAnalytics.tables.details') }}</button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>
      </div>
    </div>

    <TokenDrillDownModal
      :open="drillOpen"
      :base="filters"
      :path="drillPath"
      @close="drillOpen = false"
      @apply="onDrillApply"
    />
    <TokenPricingModal
      :open="pricingOpen"
      :can-edit="isAdmin"
      :known-models="pricingKnownModels"
      @close="pricingOpen = false"
      @saved="retry"
    />
  </div>
</template>

<style scoped>
.token-analytics-main,
.token-analytics-table-scroll {
  scrollbar-width: thin;
  scrollbar-color: rgb(var(--c-line)) rgb(var(--c-base));
}
.token-analytics-main::-webkit-scrollbar,
.token-analytics-table-scroll::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}
.token-analytics-main::-webkit-scrollbar-thumb,
.token-analytics-table-scroll::-webkit-scrollbar-thumb {
  background: rgb(var(--c-line));
}
.token-analytics-main::-webkit-scrollbar-track,
.token-analytics-table-scroll::-webkit-scrollbar-track {
  background: rgb(var(--c-base));
}
.token-analytics-kpi-merge:hover,
.token-analytics-kpi-merge:focus-within {
  border-color: #a1a1aa;
}
.token-analytics-kpi-merge:hover .token-analytics-kpi-tip,
.token-analytics-kpi-merge:focus .token-analytics-kpi-tip,
.token-analytics-kpi-merge:focus-within .token-analytics-kpi-tip {
  display: block;
}
.token-analytics-runs-grid {
  table-layout: fixed;
  width: max-content;
}
.token-analytics-runs-col-sash {
  right: -3px;
}
.token-skeleton {
  background: linear-gradient(90deg, rgb(var(--c-elevated)) 25%, rgb(var(--c-line) / 0.6) 50%, rgb(var(--c-elevated)) 75%);
  background-size: 200% 100%;
  animation: token-skeleton-shimmer 1.2s ease-in-out infinite;
}
@keyframes token-skeleton-shimmer {
  from { background-position: 200% 0; }
  to { background-position: -200% 0; }
}
</style>

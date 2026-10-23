<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, onUpdated, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { registerECharts } from '@/components/charts/echartsSetup'
import { api } from '@/lib/api/api'
import type {
  GlobalTokenStats,
  TokenStatsModel,
  TokenStatsWindow,
  TokenStatsWorkflow,
  GlobalTokenStatsNamedBucket,
} from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'
import { displayRunTitle } from '@/lib/run/runTitle'
import { truncateText } from '@/lib/shared/format'
import { TOKEN_PART_COLORS, TOKEN_PART_KEYS } from '@/components/token-analytics/tokenStatsShared'
import { useToast } from '@/lib/composables/useToast'
import { useAuth } from '@/lib/composables/useAuth'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import Icon from '@/components/ui/Icon.vue'
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
type BarDimension = 'workflow' | 'model' | 'nodeType' | 'source' | 'status' | 'phase' | 'run'
const BAR_DIMENSIONS: BarDimension[] = ['workflow', 'model', 'nodeType', 'source', 'status', 'phase', 'run']
const barDimension = ref<BarDimension>('workflow')
type CostDimension = 'model' | 'workflow' | 'source' | 'nodeType' | 'status' | 'phase'
const COST_DIMENSIONS: CostDimension[] = ['model', 'workflow', 'source', 'nodeType', 'status', 'phase']
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

const granularityOptions = computed(() => [
  { value: '', label: t('pages.tokenAnalytics.granularity.auto') },
  ...(['hour', 'day', 'week'] as const).map((g) => ({ value: g, label: t(`pages.tokenAnalytics.granularity.${g}`) })),
])
const modelOptions = computed(() => [
  { value: '', label: t('pages.tokenAnalytics.modelAll') },
  ...(data.value?.filterOptions.models ?? []).map((m) => ({ value: m.key, label: m.name })),
])
const workflowOptions = computed(() => [
  { value: '', label: t('pages.tokenAnalytics.workflowAll') },
  ...(data.value?.filterOptions.workflows ?? []).map((w) => ({ value: w.key, label: w.name })),
])
const nodeTypeOptions = computed(() => [
  { value: '', label: t('pages.tokenAnalytics.nodeTypeAll') },
  ...(data.value?.filterOptions.nodeTypes ?? []).map((n) => ({ value: n.key, label: n.name })),
])
const statusOptions = computed(() => [
  { value: '', label: t('pages.tokenAnalytics.statusAll') },
  ...(['ok', 'failed', 'cancelled'] as const).map((s) => ({ value: s, label: statusLabel(t, s) })),
])
const phaseOptions = computed(() => [
  { value: '', label: t('pages.tokenAnalytics.phaseAll') },
  ...(['production', 'interactive', 'chat'] as const).map((p) => ({ value: p, label: phaseLabel(t, p) })),
])

function setFilter<K extends keyof TokenStatsFilters>(key: K, value: TokenStatsFilters[K]) {
  filters.value = { ...filters.value, [key]: value }
}

function clearFilters() {
  const f = defaultTokenStatsFilters()
  filters.value = { ...f, window: filters.value.window, from: filters.value.from, to: filters.value.to, granularity: filters.value.granularity }
}

function isUnnamedModel(model: TokenStatsModel): boolean {
  return !!model.unknown && (!model.name.trim() || ['未知/未分桶', '未知模型', 'unknown model'].includes(model.name.trim().toLowerCase()))
}
function modelLabel(model: TokenStatsModel): string {
  return isUnnamedModel(model) ? t('pages.tokenAnalytics.unknownModel') : model.name
}

// ---------- Pies ----------

function namedSlices(
  rows: GlobalTokenStatsNamedBucket[] | undefined,
  label: (k: string) => string,
  colors: Record<string, string>,
  dim: 'source' | 'status' | 'phase' | null,
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
  if (!d || d.empty) return { source: [], comp: [], model: [], node: [], wf: [], status: [], phase: [] }
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
    model: d.modelRanking.filter((m) => !m.other).slice(0, 10).map((m, i) => ({
      name: modelLabel(m),
      value: m.total,
      key: m.modelKey,
      color: isUnnamedModel(m) ? '#71717a' : paletteColor(i),
      drill: m.modelKey ? { dim: 'model' as const, key: m.modelKey, name: modelLabel(m) } : undefined,
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
    phase: namedSlices(d.phases, (k) => phaseLabel(t, k), TOKEN_LEDGER_PHASE_COLORS, 'phase'),
  }
})

type PieKey = keyof typeof pieSlices.value
const TOP_PIES: Array<{ key: PieKey; label: string; donut: boolean }> = [
  { key: 'source', label: 'sourcePie', donut: true },
  { key: 'comp', label: 'compPie', donut: false },
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

function namedBarRows(
  rows: GlobalTokenStatsNamedBucket[] | undefined,
  label: (key: string) => string,
  dim: DrillTarget['dim'] | null,
): StackedBarRow[] {
  return (rows ?? []).filter((r) => r.total > 0).map((r) => {
    const key = r.key || r.name
    const name = label(key)
    return {
      name,
      ...partsOf(r),
      other: !!r.other,
      drill: dim && key && !r.other ? { dim, key, name } : undefined,
    }
  })
}

function normalizeBarRows(dimension: BarDimension): StackedBarRow[] {
  const d = data.value
  if (!d) return []
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
  if (dimension === 'nodeType') {
    return d.nodeTypes.map((n) => ({
      name: n.name,
      ...partsOf(n),
      other: !!n.other,
      drill: n.key && n.key !== 'unknown' && !n.other ? { dim: 'nodeType', key: n.key, name: n.name } : undefined,
    }))
  }
  if (dimension === 'source') return namedBarRows(d.sources, (k) => sourceLabel(t, k), 'source')
  if (dimension === 'status') return namedBarRows(d.statuses, (k) => statusLabel(t, k), 'status')
  if (dimension === 'phase') return namedBarRows(d.phases, (k) => phaseLabel(t, k), 'phase')
  return (d.topRuns ?? []).slice(0, 10).map((r) => ({
    name: r.title || r.runId,
    ...partsOf(r),
    other: false,
    drill: r.runId ? { dim: 'run' as const, key: r.runId, name: r.title || r.runId } : undefined,
  }))
}

const barRowsByDimension = computed(() => {
  const out = {} as Record<BarDimension, StackedBarRow[]>
  for (const dim of BAR_DIMENSIONS) out[dim] = normalizeBarRows(dim)
  return out
})

const barDimensionEnabled = computed(() => {
  const out = {} as Record<BarDimension, boolean>
  for (const dim of BAR_DIMENSIONS) out[dim] = barRowsByDimension.value[dim].length >= 2
  return out
})

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

function namedCostRows(
  rows: GlobalTokenStatsNamedBucket[] | undefined,
  label: (key: string) => string,
  dim: DrillTarget['dim'] | null,
): CostBarRow[] {
  return (rows ?? []).filter((r) => (r.cost || 0) > 0).map((r) => {
    const key = r.key || r.name
    const name = label(key)
    return {
      name,
      cost: r.cost || 0,
      total: r.total,
      drill: dim && key && !r.other ? { dim, key, name } : undefined,
    }
  })
}

function costRowsFor(dim: CostDimension): CostBarRow[] {
  const d = data.value
  if (!d) return []
  if (dim === 'model') {
    return d.modelRanking.filter((m) => !m.other && (m.cost || 0) > 0).map((m) => ({
      name: m.name,
      cost: m.cost || 0,
      total: m.total,
      drill: m.modelKey ? { dim: 'model' as const, key: m.modelKey, name: m.name } : undefined,
    }))
  }
  if (dim === 'workflow') {
    return d.workflows.filter((w) => !w.other && (w.cost || 0) > 0).map((w) => ({ name: w.name, cost: w.cost || 0, total: w.total, drill: workflowDrill(w) }))
  }
  if (dim === 'source') return namedCostRows(d.sources, (k) => sourceLabel(t, k), 'source')
  if (dim === 'nodeType') return namedCostRows(d.nodeTypes, (k) => k, 'nodeType')
  if (dim === 'status') return namedCostRows(d.statuses, (k) => statusLabel(t, k), 'status')
  return namedCostRows(d.phases, (k) => phaseLabel(t, k), 'phase')
}

const costDimensionEnabled = computed(() => {
  const out = {} as Record<CostDimension, boolean>
  for (const dim of COST_DIMENSIONS) out[dim] = costRowsFor(dim).length > 0
  return out
})

function reconcileCostDimension() {
  if (costDimensionEnabled.value[costDimension.value]) return
  const fallback = COST_DIMENSIONS.find((dim) => costDimensionEnabled.value[dim])
  if (fallback) costDimension.value = fallback
}

const costOption = computed(() => {
  if (!costDimensionEnabled.value[costDimension.value]) return null
  return costBarOption(costRowsFor(costDimension.value), currency.value, t)
})

// ---------- Trend / area / heat / tree ----------

const lineOption = computed(() => trendChartOption(data.value, lineMode.value, t))
const areaOption = computed(() => areaChartOption(data.value, areaMode.value, t))
const weekHourOption = computed(() => weekHourHeatmapOption(data.value?.weekHour, t))
const treeOption = computed(() => treemapOption(data.value?.tree, currency.value, t))

const lineModes = computed(() =>
  (['total', 'model', 'cost'] as const).map((m) => ({
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
  COST_DIMENSIONS.map((dim) => ({
    id: dim,
    label: t(`pages.tokenAnalytics.barDimensions.${dim}`),
    disabled: !costDimensionEnabled.value[dim],
    testId: `token-analytics-cost-dimension-${dim}`,
  })),
)

function selectCostDimension(dimension: string) {
  const dim = dimension as CostDimension
  if (costDimensionEnabled.value[dim]) costDimension.value = dim
}

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
    reconcileCostDimension()
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

function goRun(runId: string) {
  void router.push(`/runs/${runId}`)
}

// ---------- Tables ----------

const modelRows = computed(() => (data.value?.modelRanking ?? []).filter((m) => m.total > 0))

function share(total: number): string {
  const all = data.value?.kpi.total || 0
  return all > 0 ? fmtPct(total / all) : '—'
}

function exportModels() {
  const rows = modelRows.value.map((m) => [
    m.name, m.modelKey || '', m.total, m.inputTokens || 0, m.outputTokens || 0, m.cacheReadTokens || 0, m.cacheWriteTokens || 0, m.cost || 0,
  ])
  downloadCsv('token-usage-models.csv', toCsv(['model', 'model_key', 'total', 'input', 'output', 'cache_read', 'cache_write', 'cost'], rows))
}

function exportRuns() {
  const rows = (data.value?.topRuns ?? []).map((r) => [
    runTitleReadable(r.title), r.runId, r.workflowName, r.modelName || r.modelKey, r.status || '', r.nodeCount || 0,
    r.total, r.inputTokens || 0, r.outputTokens || 0, r.cacheReadTokens || 0, r.cacheWriteTokens || 0, r.cost || 0, r.firstAt || '',
  ])
  downloadCsv(
    'token-usage-runs.csv',
    toCsv(['run', 'run_id', 'workflow', 'model', 'status', 'nodes', 'total', 'input', 'output', 'cache_read', 'cache_write', 'cost', 'first_at'], rows),
  )
}

const RUN_TITLE_MAX_DISPLAY = 60

/** Session-only widths for the "highest-usage runs" table (g1.2). Refresh restores defaults. */
type RunsColKey = 'run' | 'workflow' | 'model' | 'total' | 'cost'

const RUNS_COL_DEFS = [
  { key: 'run' as const, labelKey: 'pages.tokenAnalytics.tables.colRun', align: 'left' as const, defaultWidth: 220, minWidth: 80 },
  { key: 'workflow' as const, labelKey: 'pages.tokenAnalytics.colWorkflow', align: 'left' as const, defaultWidth: 140, minWidth: 64 },
  { key: 'model' as const, labelKey: 'pages.tokenAnalytics.tables.colModel', align: 'left' as const, defaultWidth: 140, minWidth: 64 },
  { key: 'total' as const, labelKey: 'pages.tokenAnalytics.tables.colTotal', align: 'right' as const, defaultWidth: 88, minWidth: 56 },
  { key: 'cost' as const, labelKey: 'pages.tokenAnalytics.tables.colCost', align: 'right' as const, defaultWidth: 80, minWidth: 56 },
] as const

const RUNS_COL_MIN: Record<RunsColKey, number> = Object.fromEntries(RUNS_COL_DEFS.map((c) => [c.key, c.minWidth])) as Record<RunsColKey, number>

const runsColWidths = ref<Record<RunsColKey, number>>(
  Object.fromEntries(RUNS_COL_DEFS.map((c) => [c.key, c.defaultWidth])) as Record<RunsColKey, number>,
)
const runsTableWidth = computed(() => Object.values(runsColWidths.value).reduce((total, width) => total + width, 56))
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

/* ── redesign: filter popover + chips, workflow table (restores the old 项目明细表 slot) ── */
const fxOpen = ref(false)
/**
 * The filter button is the last item of a wrapping toolbar: on one line it sits
 * at the right edge, once wrapped it starts a line on the left. Anchor the
 * 340px popover to whichever side keeps it inside the viewport.
 */
const FX_POP_WIDTH = 340
const fxAlignRight = ref(false)
const fxBtn = ref<HTMLElement | null>(null)
function syncFxAlign() {
  const el = fxBtn.value
  if (!el || typeof window === 'undefined') return
  const next = el.getBoundingClientRect().left + FX_POP_WIDTH > window.innerWidth - 16
  if (next !== fxAlignRight.value) fxAlignRight.value = next
}
function toggleFx() {
  if (!fxOpen.value) syncFxAlign()
  fxOpen.value = !fxOpen.value
}
// The button can move while the popover is open (count badge / chips re-wrap
// the toolbar, window resize): re-anchor after each render and on resize.
onUpdated(() => { if (fxOpen.value) void nextTick(syncFxAlign) })
function onFxResize() { if (fxOpen.value) syncFxAlign() }
onMounted(() => window.addEventListener('resize', onFxResize))
onBeforeUnmount(() => window.removeEventListener('resize', onFxResize))
type FxKey = 'modelKey' | 'workflowId' | 'nodeType' | 'status' | 'phase'
const FX_FIELDS: { key: FxKey; labelKey: string }[] = [
  { key: 'modelKey', labelKey: 'pages.tokenAnalytics.fxModel' },
  { key: 'workflowId', labelKey: 'pages.tokenAnalytics.fxWorkflow' },
  { key: 'nodeType', labelKey: 'pages.tokenAnalytics.fxNodeType' },
  { key: 'status', labelKey: 'pages.tokenAnalytics.fxStatus' },
  { key: 'phase', labelKey: 'pages.tokenAnalytics.fxPhase' },
]
function fxOptions(key: FxKey) {
  const src = key === 'modelKey' ? modelOptions.value : key === 'workflowId' ? workflowOptions.value : key === 'nodeType' ? nodeTypeOptions.value : key === 'status' ? statusOptions.value : phaseOptions.value
  return src.map((o) => (o.value ? o : { value: '', label: t('pages.tokenAnalytics.fxAll') }))
}
function sourceName(s: TokenSourceFilter) {
  return s === 'all' ? t('pages.tokenAnalytics.fxAllSources') : s === 'workflow' ? t('pages.tokenAnalytics.sourceWorkflow') : s === 'pm' ? t('pages.tokenAnalytics.sourcePm') : t('pages.tokenAnalytics.sourceStudio')
}
const fxChips = computed(() => {
  const out: { key: string; label: string; clear: () => void }[] = []
  for (const f of FX_FIELDS) {
    const v = filters.value[f.key] as string
    if (v) out.push({ key: f.key, label: t('pages.tokenAnalytics.fxChipLabel', { name: t(f.labelKey), value: fxOptions(f.key).find((o) => o.value === v)?.label || v }), clear: () => setFilter(f.key, '' as never) })
  }
  if (filters.value.runId) out.push({ key: 'runId', label: t('pages.tokenAnalytics.fxChipLabel', { name: t('pages.tokenAnalytics.drill.dims.run'), value: filters.value.runId.slice(0, 8) }), clear: () => setFilter('runId', '') })
  return out
})
const workflowRows = computed(() => (data.value?.workflows ?? []).filter((w) => w.total > 0))
function exportWorkflows() {
  const rows = workflowRows.value.map((w) => [w.name, w.workflowId || '', w.kind || '', w.total, w.inputTokens || 0, w.outputTokens || 0, w.cacheReadTokens || 0, w.cacheWriteTokens || 0, w.cost || 0])
  downloadCsv('token-usage-workflows.csv', toCsv(['workflow', 'workflow_id', 'kind', 'total', 'input', 'output', 'cache_read', 'cache_write', 'cost'], rows))
}
onMounted(() => document.addEventListener('click', closeFx))
onBeforeUnmount(() => document.removeEventListener('click', closeFx))
function closeFx() { fxOpen.value = false }
const fxCount = computed(() => fxChips.value.length)
</script>

<template>
  <div class="usage-page min-w-0" data-testid="token-analytics-page">
    <div class="token-analytics-main min-w-0">
      <div class="usage-content">
        <header class="usage-page-header" data-testid="token-analytics-header">
          <div class="min-w-0">
            <h1 class="m-0 flex items-center gap-3 text-lg font-semibold text-txt">
              {{ t('pages.tokenAnalytics.title') }}
              <span v-if="loading && data" class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-accent/30 border-t-accent" data-testid="token-analytics-refreshing" :aria-label="t('pages.tokenAnalytics.loading')" />
            </h1>
          </div>
          <div class="usage-page-actions">
            <AppButton variant="outline" size="sm" data-testid="token-analytics-pricing" @click="pricingOpen = true">
              {{ t('pages.tokenAnalytics.pricing.button') }}
            </AppButton>
            <AppButton variant="outline" size="sm" data-testid="token-analytics-refresh" @click="retry">
              {{ t('pages.tokenAnalytics.refresh') }}
            </AppButton>
          </div>
        </header>

        <section class="usage-toolbar" :aria-label="t('pages.tokenAnalytics.analysisRange')" data-testid="token-analytics-controls">
          <div class="usage-toolbar-row">
            <div class="usage-toolbar-group" data-testid="token-analytics-period">
              <SlidingTabs
                :value="filters.window"
                class="usage-seg flex gap-1 rounded-lg bg-elevated p-1"
                role="group"
                :aria-label="t('pages.board.tokenStats.windowAria')"
                data-testid="token-analytics-window"
              >
                <button
                  v-for="w in WINDOWS"
                  :key="w"
                  type="button"
                  class="whitespace-nowrap rounded-md px-2.5 py-1 text-[12px] leading-5 transition"
                  :class="filters.window === w ? 'font-semibold text-txt' : 'text-txt3 hover:text-txt2'"
                  :data-testid="`token-analytics-window-${w}`"
                  :aria-pressed="filters.window === w"
                  @click="selectWindow(w)"
                >
                  {{ t(`pages.board.tokenStats.windows.${w}`) }}
                </button>
                <button
                  type="button"
                  class="whitespace-nowrap rounded-md px-2.5 py-1 text-[12px] leading-5 transition"
                  :class="filters.window === 'custom' ? 'font-semibold text-txt' : 'text-txt3 hover:text-txt2'"
                  data-testid="token-analytics-window-custom"
                  :aria-pressed="filters.window === 'custom'"
                  @click="selectCustomWindow"
                >
                  {{ t('pages.tokenAnalytics.windowCustom') }}
                </button>
              </SlidingTabs>
              <div v-if="filters.window === 'custom'" class="flex items-center gap-1.5">
                <input
                  type="date"
                  class="usage-date-input rounded-md border border-line bg-base px-3 text-[12px] text-txt outline-none transition hover:border-line-strong focus:border-accent"
                  :value="filters.from"
                  :max="filters.to || undefined"
                  :aria-label="t('pages.tokenAnalytics.rangeFrom')"
                  data-testid="token-analytics-range-from"
                  @change="setRange('from', ($event.target as HTMLInputElement).value)"
                />
                <span class="text-xs text-txt3">–</span>
                <input
                  type="date"
                  class="usage-date-input rounded-md border border-line bg-base px-3 text-[12px] text-txt outline-none transition hover:border-line-strong focus:border-accent"
                  :value="filters.to"
                  :min="filters.from || undefined"
                  :aria-label="t('pages.tokenAnalytics.rangeTo')"
                  data-testid="token-analytics-range-to"
                  @change="setRange('to', ($event.target as HTMLInputElement).value)"
                />
              </div>
              <SlidingTabs :value="filters.source" class="usage-seg flex gap-1 rounded-lg bg-elevated p-1" role="group" :aria-label="t('pages.tokenAnalytics.sourceLabel')">
                <button
                  v-for="s in SOURCES"
                  :key="s"
                  type="button"
                  class="whitespace-nowrap rounded-md px-2.5 py-1 text-[12px] leading-5 transition"
                  :class="filters.source === s ? 'font-semibold text-txt' : 'text-txt3 hover:text-txt2'"
                  :data-testid="`token-analytics-source-${s}`"
                  :aria-pressed="filters.source === s"
                  @click="setFilter('source', s)"
                >{{ sourceName(s) }}</button>
              </SlidingTabs>
            </div>
            <div class="usage-toolbar-group">
              <div class="relative" @click.stop>
                <button
                  ref="fxBtn"
                  type="button"
                  class="toolbar-control flex items-center gap-2 border border-line bg-surface text-sm text-txt2 transition hover:bg-elevated"
                  :class="{ 'border-accent/60 text-txt': fxCount }"
                  :aria-expanded="fxOpen"
                  data-testid="token-analytics-filter-btn"
                  @click="toggleFx"
                >
                  <Icon name="filter" :size="15" class="text-txt3" />
                  {{ t('pages.tokenAnalytics.fxFilter') }}
                  <span v-if="fxCount" class="chip">{{ fxCount }}</span>
                </button>
                <Transition name="overlay-pop">
                  <div v-if="fxOpen" class="card absolute z-30 mt-1.5 w-[340px] max-w-[calc(100vw-2rem)] p-3" :class="fxAlignRight ? 'right-0' : 'left-0'" data-testid="token-analytics-filter-pop">

                    <div class="grid grid-cols-[72px_1fr] items-center gap-x-3 gap-y-2" data-testid="token-analytics-filters">
                      <span class="text-xs text-txt3">{{ t('pages.tokenAnalytics.fxModel') }}</span>
                      <AppSelect
                        size="sm"
                        data-testid="token-analytics-filter-model"
                        :model-value="filters.modelKey"
                        :options="fxOptions('modelKey')"
                        :aria-label="t('pages.tokenAnalytics.fxModel')"
                        @update:model-value="setFilter('modelKey', $event)"
                      />
                      <span class="text-xs text-txt3">{{ t('pages.tokenAnalytics.fxWorkflow') }}</span>
                      <AppSelect
                        size="sm"
                        data-testid="token-analytics-filter-workflow"
                        :model-value="filters.workflowId"
                        :options="fxOptions('workflowId')"
                        :aria-label="t('pages.tokenAnalytics.fxWorkflow')"
                        @update:model-value="setFilter('workflowId', $event)"
                      />
                      <span class="text-xs text-txt3">{{ t('pages.tokenAnalytics.fxNodeType') }}</span>
                      <AppSelect
                        size="sm"
                        data-testid="token-analytics-filter-node-type"
                        :model-value="filters.nodeType"
                        :options="fxOptions('nodeType')"
                        :aria-label="t('pages.tokenAnalytics.fxNodeType')"
                        @update:model-value="setFilter('nodeType', $event)"
                      />
                      <span class="text-xs text-txt3">{{ t('pages.tokenAnalytics.fxStatus') }}</span>
                      <AppSelect
                        size="sm"
                        data-testid="token-analytics-filter-status"
                        :model-value="filters.status"
                        :options="fxOptions('status')"
                        :aria-label="t('pages.tokenAnalytics.fxStatus')"
                        @update:model-value="setFilter('status', $event as TokenStatsFilters['status'])"
                      />
                      <span class="text-xs text-txt3">{{ t('pages.tokenAnalytics.fxPhase') }}</span>
                      <AppSelect
                        size="sm"
                        data-testid="token-analytics-filter-phase"
                        :model-value="filters.phase"
                        :options="fxOptions('phase')"
                        :aria-label="t('pages.tokenAnalytics.fxPhase')"
                        @update:model-value="setFilter('phase', $event as TokenStatsFilters['phase'])"
                      />
                    </div>
                    <div class="mt-3 flex justify-end border-t border-line/60 pt-2">
                      <AppButton variant="ghost" size="sm" :disabled="!filterCount" data-testid="token-analytics-clear" @click="clearFilters">{{ t('pages.tokenAnalytics.clearFilters') }}</AppButton>
                    </div>
                  </div>
                </Transition>
              </div>
            </div>
          </div>
          <div v-if="fxChips.length" class="usage-chips" data-testid="token-analytics-filter-chips">
            <span
              v-for="c in fxChips"
              :key="c.key"
              class="chip border-accent/40 bg-accent-dim px-2.5 py-1 text-accent-2"
              :data-testid="`token-analytics-chip-${c.key}`"
            >
              {{ c.label }}
              <button type="button" :aria-label="t('pages.tokenAnalytics.fxChipRemove', { name: c.label })" :data-testid="`token-analytics-chip-remove-${c.key}`" @click="c.clear()"><Icon name="close" :size="12" /></button>
            </span>
            <AppButton variant="ghost" size="sm" data-testid="token-analytics-chips-clear" @click="clearFilters">{{ t('pages.tokenAnalytics.fxClearAll') }}</AppButton>
          </div>
        </section>

        <div v-if="loading && !data" data-testid="token-analytics-loading" :aria-label="t('pages.tokenAnalytics.loading')">
          <div class="mb-3 grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6">
            <div v-for="i in 6" :key="i" class="token-skeleton h-[92px]" />
          </div>
          <div class="token-skeleton mb-3 h-[280px]" />
          <div class="grid grid-cols-1 gap-2.5 lg:grid-cols-2">
            <div class="token-skeleton h-[220px]" />
            <div class="token-skeleton h-[220px]" />
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
          <section id="overview" class="usage-overview" data-testid="token-analytics-kpis" :aria-label="t('pages.tokenAnalytics.sections.overview')">
            <p class="m-0 mb-3 text-[12px] leading-5 text-txt3" data-testid="token-analytics-overview-hint">{{ t('pages.tokenAnalytics.sections.overviewHint') }}</p>
            <div class="usage-kpi-grid">
              <div
                class="usage-metric"
                data-testid="token-analytics-kpi-total"
                :data-token-count="data.kpi.total"
              >
                <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiTotal') }}</div>
                <div class="mt-1 text-[26px] font-semibold tracking-tight tabular-nums" :title="fmtTokenCount(data.kpi.total)">{{ fmtCompactTokenCount(data.kpi.total) }}</div>
                <div class="mt-1 text-[11px]" :class="deltaClass">{{ deltaLabel }}</div>
              </div>
              <div class="usage-metric" data-testid="token-analytics-kpi-cost">
                <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiCost') }}</div>
                <template v-if="kpiCostPriced">
                  <div class="mt-1 text-[26px] font-semibold tracking-tight tabular-nums">{{ fmtCost(data.kpi.cost, currency) }}</div>
                  <div class="mt-1 text-[11px]" :class="costDeltaLabel ? ((data.kpi.costDeltaPct || 0) >= 0 ? 'text-emerald-600' : 'text-amber-600') : 'text-txt3'">
                    {{ costDeltaLabel ?? t('pages.tokenAnalytics.noPrev') }}
                  </div>
                  <button
                    v-if="data.unpricedModels?.length"
                    type="button"
                    class="mt-0.5 block text-left text-[11px] text-warn hover:underline"
                    @click="pricingOpen = true"
                  >{{ t('pages.tokenAnalytics.kpiCostUnpricedModels', { n: data.unpricedModels.length }) }}</button>
                </template>
                <template v-else>
                  <div class="mt-1 text-[15px] font-semibold text-txt3">{{ t('pages.tokenAnalytics.kpiCostUnpriced') }}</div>
                  <button type="button" class="mt-1 block text-left text-[11px] text-accent-2 hover:underline" @click="pricingOpen = true">
                    {{ t('pages.tokenAnalytics.kpiCostConfigure') }}
                  </button>
                </template>
              </div>
              <div
                class="usage-metric token-analytics-kpi-merge relative outline-none"
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
              <div class="usage-metric" data-testid="token-analytics-kpi-scope">
                <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiScope') }}</div>
                <div class="mt-1 text-[26px] font-semibold tracking-tight tabular-nums">{{ fmtTokenCount(data.kpi.runCount) }}</div>
                <div class="mt-1 text-[11px] text-txt3">
                  {{ t('pages.tokenAnalytics.kpiModels', { n: data.kpi.modelCount }) }}
                </div>
                <div v-if="data.kpi.avgPerRun" class="mt-0.5 text-[11px] text-txt3">
                  {{ t('pages.tokenAnalytics.kpiAvgPerRun', { n: fmtCompactTokenCount(Math.round(data.kpi.avgPerRun)) }) }}
                </div>
              </div>
              <div
                class="usage-metric"
                data-testid="token-analytics-kpi-cache"
                :title="`${fmtTokenCount(data.kpi.cacheReadTokens)} / ${fmtTokenCount(data.kpi.cacheWriteTokens)}`"
              >
                <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.cacheHit') }}</div>
                <div class="mt-1 text-[26px] font-semibold tracking-tight tabular-nums">{{ fmtPct(data.kpi.cacheHitRate) }}</div>
                <div class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-elevated">
                  <div class="h-full rounded-full bg-[#22a6b3]" :style="{ width: `${Math.min(100, (data.kpi.cacheHitRate || 0) * 100)}%` }" />
                </div>
                <div class="mt-1 text-[11px] text-txt3">
                  {{ t('pages.tokenAnalytics.kpiCacheDetail', { read: fmtCompactTokenCount(data.kpi.cacheReadTokens), write: fmtCompactTokenCount(data.kpi.cacheWriteTokens) }) }}
                </div>
              </div>
              <button
                type="button"
                class="usage-metric flex flex-col items-stretch justify-start text-left transition-colors enabled:hover:bg-elevated/50"
                data-testid="token-analytics-kpi-failed"
                :disabled="!data.kpi.failedTotal"
                @click="openDrill([{ dim: 'status', key: 'failed', name: statusLabel(t, 'failed') }])"
              >
                <div class="text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiFailed') }}</div>
                <div class="mt-1 text-[26px] font-semibold tracking-tight tabular-nums" :class="data.kpi.failedTotal ? 'text-err' : ''">
                  {{ fmtCompactTokenCount(data.kpi.failedTotal || 0) }}
                </div>
                <div class="mt-1 text-[11px] text-txt3">{{ t('pages.tokenAnalytics.kpiFailedShare', { pct: fmtPct(failedShare) }) }}</div>
              </button>
            </div>
          </section>

          <TokenChartCard
            id="lines"
            class="usage-primary-trend"
            data-testid="token-analytics-lines"
            :title="t('pages.tokenAnalytics.charts.lines')"
            :hint="t('pages.tokenAnalytics.charts.linesHint')"
            :modes="lineModes"
            :mode="lineMode"
            @update:mode="lineMode = $event as TrendMode"
          >
            <template #actions>
              <AppSelect
                class="usage-select w-[132px]"
                size="sm"
                :model-value="filters.granularity"
                :options="granularityOptions"
                :aria-label="t('pages.tokenAnalytics.granularity.aria')"
                data-testid="token-analytics-granularity"
                @update:model-value="setFilter('granularity', $event as TokenStatsFilters['granularity'])"
              />
            </template>
            <div class="token-analytics-plot mt-4 h-[300px] overflow-visible sm:h-[320px]" data-testid="token-analytics-plot-lines">
              <VChart v-if="lineOption" :option="lineOption" autoresize class="h-full w-full" @click="onChartClick" />
            </div>
          </TokenChartCard>
          <section :aria-label="t('pages.tokenAnalytics.sections.breakdown')" data-testid="token-analytics-breakdown">
            <TokenChartCard
              :heading-level="3"
              id="pies"
              class="usage-composition-card"
              data-testid="token-analytics-pies"
              :title="t('pages.tokenAnalytics.charts.pies')"
              :hint="t('pages.tokenAnalytics.charts.piesHint')"
            >
              <div class="mt-2 grid grid-cols-1 gap-6 md:grid-cols-3">
                <div v-for="cfg in TOP_PIES" :key="cfg.key" class="text-center" :data-testid="cfg.key === 'model' ? 'token-analytics-model-share' : undefined">
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
            <div class="usage-chart-grid grid grid-cols-1 gap-5" :class="costOption && hasComparableBarDimension ? 'xl:grid-cols-[3fr_2fr]' : ''">
              <TokenChartCard
                :heading-level="3"
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
                :heading-level="3"
                v-if="costOption"
                data-testid="token-analytics-cost"
                :title="t('pages.tokenAnalytics.charts.costRank')"
                :hint="t('pages.tokenAnalytics.charts.costRankHint')"
                :modes="costModes"
                :mode="costDimension"
                @update:mode="selectCostDimension"
              >
                <div class="token-analytics-plot mt-2 h-[240px] overflow-visible" data-testid="token-analytics-plot-cost">
                  <VChart :option="costOption" autoresize class="h-full w-full" @click="onChartClick" />
                </div>
              </TokenChartCard>
            </div>
          </section>
          <section class="usage-patterns" :aria-label="t('pages.tokenAnalytics.sections.patterns')" data-testid="token-analytics-patterns">
            <div class="usage-chart-grid grid grid-cols-1 gap-5 xl:grid-cols-2">
              <TokenChartCard
                :heading-level="3"
                id="area"
                data-testid="token-analytics-area"
                :title="t('pages.tokenAnalytics.charts.area')"
                :hint="t('pages.tokenAnalytics.charts.areaHint')"
                :modes="areaModes"
                :mode="areaMode"
                @update:mode="areaMode = $event as AreaMode"
              >
                <div class="token-analytics-plot mt-2 h-[260px] overflow-visible" data-testid="token-analytics-plot-area">
                  <VChart v-if="areaOption" :option="areaOption" autoresize class="h-full w-full" @click="onChartClick" />
                </div>
              </TokenChartCard>

              <TokenChartCard :heading-level="3" data-testid="token-analytics-weekhour" :title="t('pages.tokenAnalytics.charts.weekHour')" :hint="t('pages.tokenAnalytics.charts.weekHourHint')">
                <div class="token-analytics-plot mt-2 h-[260px] overflow-visible" data-testid="token-analytics-plot-weekhour">
                  <VChart v-if="weekHourOption" :option="weekHourOption" autoresize class="h-full w-full" />
                  <p v-else class="py-20 text-center text-xs text-txt3">{{ t('pages.tokenAnalytics.noData') }}</p>
                </div>
              </TokenChartCard>
            </div>
            <TokenChartCard
              :heading-level="3"
              id="nodeWf"
              class="usage-detail-card"
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
              :heading-level="3"
              v-if="treeOption"
              class="usage-detail-card"
              data-testid="token-analytics-treemap"
              :title="t('pages.tokenAnalytics.charts.treemap')"
              :hint="t('pages.tokenAnalytics.charts.treemapHint')"
            >
              <div class="token-analytics-plot mt-2 h-[320px] overflow-visible" data-testid="token-analytics-plot-treemap">
                <VChart :option="treeOption" autoresize class="h-full w-full" @click="onChartClick" />
              </div>
            </TokenChartCard>
          </section>
          <section id="tables" :aria-label="t('pages.tokenAnalytics.sections.details')" data-testid="token-analytics-details">
            <div class="usage-table-grid grid grid-cols-1 gap-x-8 2xl:grid-cols-2">
              <TokenChartCard :heading-level="3" data-testid="token-analytics-models-table" :title="t('pages.tokenAnalytics.tables.models')">
                <template #actions>
                  <button type="button" class="text-[11px] text-txt3 hover:text-accent-2" data-testid="token-analytics-export-models" @click="exportModels">
                    {{ t('pages.tokenAnalytics.exportCsv') }}
                  </button>
                </template>
                <div class="token-analytics-table-scroll mt-2 min-w-0 max-w-full overflow-x-auto">
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
                      <tr v-for="(m, index) in modelRows" :key="m.modelKey || m.name" class="hover:bg-accent-dim/40" :data-model-key="m.modelKey" :data-unknown="m.unknown ? '1' : undefined">
                        <td class="border-b border-line/60 py-1.5" :title="modelLabel(m)">
                          <div class="flex min-w-0 max-w-[320px] items-center gap-2 pr-3">
                            <span class="token-model-swatch h-1.5 w-1.5 shrink-0 rounded-full" :style="{ backgroundColor: isUnnamedModel(m) ? '#71717a' : paletteColor(index) }" aria-hidden="true" />
                            <button
                              v-if="m.modelKey && !m.other"
                              type="button"
                              class="min-w-0 truncate text-left hover:text-accent-2 hover:underline"
                              :class="isUnnamedModel(m) ? 'text-txt3' : ''"
                              @click="openDrill([{ dim: 'model', key: m.modelKey!, name: modelLabel(m) }])"
                            >{{ modelLabel(m) }}</button>
                            <span v-else class="min-w-0 truncate text-txt3">{{ modelLabel(m) }}</span>
                            <span v-if="isUnnamedModel(m)" class="shrink-0 rounded bg-elevated px-1 py-px text-[10px] text-txt3" data-testid="unknown-model-badge">{{ t('pages.tokenAnalytics.unknownBadge') }}</span>
                          </div>
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
              <TokenChartCard :heading-level="3" data-testid="token-analytics-workflows-table" :title="t('pages.tokenAnalytics.workflowsTable')">
                <template #actions>
                  <button type="button" class="text-[11px] text-txt3 hover:text-accent-2" data-testid="token-analytics-export-workflows" @click="exportWorkflows">
                    {{ t('pages.tokenAnalytics.exportCsv') }}
                  </button>
                </template>
                <div class="token-analytics-table-scroll mt-2 min-w-0 max-w-full overflow-x-auto">
                  <table class="w-full border-collapse text-xs">
                    <thead class="sticky top-0 bg-surface">
                      <tr class="text-txt3">
                        <th class="whitespace-nowrap border-b border-line py-1.5 text-left font-semibold">{{ t('pages.tokenAnalytics.colWorkflow') }}</th>
                        <th class="whitespace-nowrap border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colTotal') }}</th>
                        <th class="whitespace-nowrap border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colShare') }}</th>
                        <th class="whitespace-nowrap border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colInput') }}</th>
                        <th class="whitespace-nowrap border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colOutput') }}</th>
                        <th class="whitespace-nowrap border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colCache') }}</th>
                        <th class="whitespace-nowrap border-b border-line py-1.5 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colCost') }}</th>
                        <th class="w-12 border-b border-line" />
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="(w, index) in workflowRows" :key="(w.kind || '') + (w.workflowId || w.name)" class="hover:bg-accent-dim/40" data-testid="token-analytics-workflow-row">
                        <td class="border-b border-line/60 py-1.5">
                          <div class="flex min-w-0 max-w-[320px] items-center gap-2 pr-3">
                            <span class="h-1.5 w-1.5 shrink-0 rounded-full" :style="{ backgroundColor: w.other ? '#71717a' : paletteColor(index) }" aria-hidden="true" />
                            <span class="min-w-0 truncate" :class="w.other ? 'text-txt3' : ''" :title="w.name">{{ w.kind === 'pm' ? sourceName('pm') : w.name }}</span>
                          </div>
                        </td>
                        <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right font-semibold tabular-nums" :title="fmtTokenCount(w.total)">{{ fmtCompactTokenCount(w.total) }}</td>
                        <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right tabular-nums text-txt3">{{ share(w.total) }}</td>
                        <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right tabular-nums">{{ fmtCompactTokenCount(w.inputTokens || 0) }}</td>
                        <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right tabular-nums">{{ fmtCompactTokenCount(w.outputTokens || 0) }}</td>
                        <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right tabular-nums">{{ fmtCompactTokenCount(w.cacheReadTokens || 0) }} / {{ fmtCompactTokenCount(w.cacheWriteTokens || 0) }}</td>
                        <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right tabular-nums">{{ w.cost ? fmtCost(w.cost, currency) : '—' }}</td>
                        <td class="whitespace-nowrap border-b border-line/60 py-1.5 text-right">
                          <button v-if="workflowDrill(w)" type="button" class="text-[11px] text-txt3 hover:text-accent-2" data-testid="token-analytics-workflow-detail" @click="openDrill([workflowDrill(w)!])">{{ t('pages.tokenAnalytics.tables.details') }}</button>
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </TokenChartCard>
            </div>
            <div class="min-w-0 py-4" data-testid="token-analytics-runs-table">
              <div class="mb-2 flex shrink-0 items-center justify-between gap-2">
                <h3 class="m-0 text-sm font-semibold">{{ t('pages.tokenAnalytics.tables.runs') }}</h3>
                <button type="button" class="text-[11px] text-txt3 hover:text-accent-2" data-testid="token-analytics-export-runs" @click="exportRuns">
                  {{ t('pages.tokenAnalytics.exportCsv') }}
                </button>
              </div>
              <div class="token-analytics-table-scroll min-w-0 max-w-full overflow-x-auto">
                <!-- plan coverage: g1.1 / g1.2 / g1.3 — header sashes, session col widths, truncate without misfire -->
                <table
                  class="token-analytics-runs-grid min-w-full border-collapse text-xs"
                  :class="runsColDragging ? 'select-none' : ''"
                  :style="{ width: `${runsTableWidth}px`, minWidth: '100%' }"
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
                      <td class="overflow-hidden text-ellipsis whitespace-nowrap border-b border-line/60 py-1.5" :title="r.workflowName" data-testid="token-analytics-run-workflow">{{ r.workflowName || '—' }}</td>
                      <td class="overflow-hidden text-ellipsis whitespace-nowrap border-b border-line/60 py-1.5" :title="r.modelName || r.modelKey">{{ r.modelName || r.modelKey }}</td>
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
.usage-toolbar { display: flex; flex-direction: column; gap: 10px; padding-bottom: 14px; }
.usage-toolbar-row { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; }
.usage-toolbar-group { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.usage-chips { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.usage-content {
  width: 100%;
  max-width: 1480px;
  margin: 0 auto;
  padding: 0 0 8px;
}
.usage-page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 20px;
}
.usage-page-actions,
.usage-control-items {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.usage-page-actions {
  flex-shrink: 0;
}
.usage-controls {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px 0;
  border-block: 1px solid rgb(var(--c-line) / 0.6);
}
.usage-control-row {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 16px;
}
.usage-control-label {
  width: 64px;
  flex-shrink: 0;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  line-height: 20px;
}
.usage-control-items {
  flex: 1 1 auto;
}
.usage-seg {
  max-width: 100%;
  flex-wrap: wrap;
}
.usage-select {
  min-width: 132px;
  max-width: 200px;
}
.usage-date-input {
  height: 36px;
}
.usage-filter-rows {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.usage-overview,
.usage-analysis-section {
  margin-top: 28px;
}
.usage-section-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 8px 16px;
  margin-bottom: 14px;
}
.usage-section-heading h2 {
  margin: 0;
  color: rgb(var(--c-txt));
  font-size: 16px;
  font-weight: 600;
  line-height: 24px;
}
.usage-section-heading p {
  margin: 0;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  line-height: 20px;
}
.usage-kpi-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  border-top: 1px solid rgb(var(--c-line) / 0.6);
}
.usage-metric {
  min-width: 0;
  min-height: 116px;
  padding: 18px 20px;
  border-left: 1px solid rgb(var(--c-line) / 0.6);
}
.usage-metric:nth-child(3n + 1) {
  border-left: 0;
}
.usage-metric:nth-child(n + 4) {
  border-top: 1px solid rgb(var(--c-line) / 0.6);
}
.usage-primary-trend {
  margin-top: 0;
}
.usage-composition-card,
.usage-detail-card {
  margin-bottom: 20px;
}
.usage-detail-card {
  margin-top: 20px;
}
.usage-analysis-section > .usage-detail-card:last-child {
  margin-bottom: 0;
}
.usage-table-grid table {
  min-width: 640px;
}
/* 1440: keep 3 columns (six columns squeezed 输入/输出 and the cache bar). */
@media (max-width: 639px) {
  .usage-content { padding-bottom: 8px; }
  .usage-page-header { flex-direction: column; gap: 16px; margin-bottom: 20px; }
  .usage-controls { padding: 16px 0; }
  .usage-control-row { flex-direction: column; align-items: stretch; gap: 8px; }
  .usage-control-label { width: auto; }
  .usage-select { max-width: 100%; min-width: 0; flex: 1 1 130px; }
  .usage-kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .usage-metric:nth-child(n) { border-left: 1px solid rgb(var(--c-line) / 0.6); border-top: 1px solid rgb(var(--c-line) / 0.6); }
  .usage-metric:nth-child(2n + 1) { border-left: 0; }
  .usage-metric:nth-child(-n + 2) { border-top: 0; }
  .usage-metric { padding: 16px; }
  .usage-overview,
  .usage-analysis-section { margin-top: 24px; }
}
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

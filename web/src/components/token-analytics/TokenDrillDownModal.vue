<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import VChart from 'vue-echarts'
import AppModal from '@/components/ui/AppModal.vue'
import Pagination from '@/components/ui/Pagination.vue'
import { registerECharts } from '@/components/charts/echartsSetup'
import { api } from '@/lib/api/api'
import type { GlobalTokenStats, TokenUsageEventRow } from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'
import { TOKEN_PART_COLORS, TOKEN_PART_KEYS } from '@/components/board/token-stats/tokenStatsShared'
import {
  TOKEN_LEDGER_SOURCE_COLORS,
  TOKEN_LEDGER_STATUS_COLORS,
  applyDrill,
  boardQueryFromFilters,
  downloadCsv,
  filtersToParams,
  fmtCost,
  fmtPct,
  isDrillable,
  paletteColor,
  toCsv,
  type DrillTarget,
  type TokenStatsFilters,
} from './tokenAnalyticsShared'
import {
  bucketPart,
  drillFromChartEvent,
  drillPieOption,
  listDrillBreakdowns,
  partLabel,
  sourceLabel,
  stackedBarOption,
  statusLabel,
  trendChartOption,
} from './tokenAnalyticsCharts'
import TokenEventsTable from './TokenEventsTable.vue'

registerECharts()

const props = defineProps<{
  open: boolean
  /** Page filters the drill starts from. */
  base: TokenStatsFilters
  /** Initial drill path (clicked element). */
  path: DrillTarget[]
}>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'apply', filters: TokenStatsFilters): void
}>()

const { t } = useI18n()
const router = useRouter()

type Tab = 'overview' | 'breakdown' | 'events'
const stack = ref<DrillTarget[]>([])
const tab = ref<Tab>('overview')
const stats = ref<GlobalTokenStats | null>(null)
const loading = ref(false)
const failed = ref(false)
const events = ref<TokenUsageEventRow[]>([])
const eventsTotal = ref(0)
const eventsPage = ref(1)
const eventsPageSize = ref(20)
const eventsSort = ref<'time' | 'total' | 'cost'>('time')
const eventsLoading = ref(false)
const eventsCurrency = ref<string | undefined>(undefined)
let statsAbort: AbortController | null = null
let eventsAbort: AbortController | null = null
let statsGen = 0
let eventsGen = 0

const filters = computed(() => applyDrill(props.base, stack.value))
const current = computed(() => stack.value[stack.value.length - 1] ?? null)
const currency = computed(() => stats.value?.currency)

function mergeStep(path: DrillTarget[], step: DrillTarget): DrillTarget[] {
  // Same dimension twice replaces the earlier step instead of stacking contradictory filters.
  const idx = path.findIndex((p) => p.dim === step.dim)
  if (idx >= 0) return [...path.slice(0, idx), step]
  return [...path, step]
}

function drillInto(steps: DrillTarget[]) {
  let next = stack.value
  for (const s of steps) if (isDrillable(s)) next = mergeStep(next, s)
  if (next === stack.value) return
  stack.value = next
}

function goCrumb(i: number) {
  if (i < 0 || i >= stack.value.length - 1) return
  stack.value = stack.value.slice(0, i + 1)
}

function crumbLabel(s: DrillTarget): string {
  return `${t(`pages.tokenAnalytics.drill.dims.${s.dim}`)}：${s.name || s.key}`
}

async function loadStats() {
  const gen = ++statsGen
  statsAbort?.abort()
  statsAbort = new AbortController()
  loading.value = true
  failed.value = false
  try {
    const res = await api.getGlobalTokenStats(filtersToParams(filters.value), { signal: statsAbort.signal })
    if (gen !== statsGen) return
    stats.value = res
  } catch (e: unknown) {
    if (gen !== statsGen || (e as { name?: string })?.name === 'AbortError') return
    failed.value = true
    stats.value = null
  } finally {
    if (gen === statsGen) loading.value = false
  }
}

async function loadEvents() {
  const gen = ++eventsGen
  eventsAbort?.abort()
  eventsAbort = new AbortController()
  eventsLoading.value = true
  try {
    const res = await api.listTokenUsageEvents(
      { ...filtersToParams(filters.value), page: eventsPage.value, pageSize: eventsPageSize.value, sort: eventsSort.value },
      { signal: eventsAbort.signal },
    )
    if (gen !== eventsGen) return
    events.value = res.items || []
    eventsTotal.value = res.total || 0
    eventsCurrency.value = res.currency
  } catch (e: unknown) {
    if (gen !== eventsGen || (e as { name?: string })?.name === 'AbortError') return
    events.value = []
    eventsTotal.value = 0
  } finally {
    if (gen === eventsGen) eventsLoading.value = false
  }
}

watch(
  () => [props.open, props.path] as const,
  ([open]) => {
    if (!open) {
      statsAbort?.abort()
      eventsAbort?.abort()
      return
    }
    stack.value = props.path.filter((p) => isDrillable(p))
    tab.value = current.value?.dim === 'run' ? 'events' : 'overview'
  },
  { immediate: true },
)

watch(
  () => (props.open ? JSON.stringify(filters.value) : ''),
  (key) => {
    if (!key) return
    eventsPage.value = 1
    void loadStats()
    if (tab.value === 'events') void loadEvents()
  },
  { immediate: true },
)

watch(tab, (v) => {
  if (v === 'events' && props.open) void loadEvents()
})
watch([eventsPage, eventsPageSize, eventsSort], () => {
  if (tab.value === 'events' && props.open) void loadEvents()
})
watch(eventsSort, () => {
  eventsPage.value = 1
})

function onPageSize(size: number) {
  eventsPageSize.value = size
  eventsPage.value = 1
}

onBeforeUnmount(() => {
  statsAbort?.abort()
  eventsAbort?.abort()
})

const kpi = computed(() => stats.value?.kpi ?? null)

const miniKpis = computed(() => {
  const k = kpi.value
  if (!k) return []
  return [
    { id: 'total', label: t('pages.tokenAnalytics.kpiTotal'), value: fmtCompactTokenCount(k.total), title: fmtTokenCount(k.total) },
    { id: 'input', label: t('pages.executionTimeline.partInput'), value: fmtCompactTokenCount(k.inputTokens), title: fmtTokenCount(k.inputTokens) },
    { id: 'output', label: t('pages.executionTimeline.partOutput'), value: fmtCompactTokenCount(k.outputTokens), title: fmtTokenCount(k.outputTokens) },
    { id: 'cache', label: t('pages.tokenAnalytics.cacheHit'), value: fmtPct(k.cacheHitRate), title: `${fmtTokenCount(k.cacheReadTokens)} / ${fmtTokenCount(k.cacheWriteTokens)}` },
    { id: 'cost', label: t('pages.tokenAnalytics.kpiCost'), value: fmtCost(k.cost, stats.value?.currency), title: '' },
    { id: 'runs', label: t('pages.tokenAnalytics.drill.calls'), value: String(k.eventCount ?? 0), title: t('pages.tokenAnalytics.kpiRuns', { n: k.runCount }) },
    { id: 'failed', label: t('pages.tokenAnalytics.kpiFailed'), value: fmtCompactTokenCount(k.failedTotal || 0), title: fmtTokenCount(k.failedTotal || 0) },
  ]
})

const trendOption = computed(() => trendChartOption(stats.value, 'total', t))

const partsOption = computed(() => {
  const c = stats.value?.composition
  if (!c) return null
  return drillPieOption(
    TOKEN_PART_KEYS.map((key) => ({ name: partLabel(t, key), value: bucketPart(c, key), key, color: TOKEN_PART_COLORS[key] })).filter((s) => s.value > 0),
    true,
  )
})

const sourceOption = computed(() => {
  const rows = stats.value?.sources ?? []
  return drillPieOption(
    rows.filter((r) => r.total > 0).map((r) => ({
      name: sourceLabel(t, r.key || r.name),
      value: r.total,
      key: r.key,
      color: TOKEN_LEDGER_SOURCE_COLORS[r.key || ''],
      drill: { dim: 'source', key: r.key || '', name: sourceLabel(t, r.key || r.name) },
    })),
    true,
  )
})

const statusOption = computed(() => {
  const rows = stats.value?.statuses ?? []
  if (filters.value.status) return null
  return drillPieOption(
    rows.filter((r) => r.total > 0).map((r) => ({
      name: statusLabel(t, r.key || r.name),
      value: r.total,
      key: r.key,
      color: TOKEN_LEDGER_STATUS_COLORS[r.key || ''],
      drill: { dim: 'status', key: r.key || '', name: statusLabel(t, r.key || r.name) },
    })),
    true,
  )
})

const breakdowns = computed(() => {
  const s = stats.value
  if (!s) return []
  return listDrillBreakdowns(s, filters.value, t)
    .map((b) => ({ id: b.id, title: b.title, option: stackedBarOption(b.rows, t) }))
    .filter((b) => b.option)
})

const tabs = computed(() => [
  { id: 'overview' as const, label: t('pages.tokenAnalytics.drill.tabs.overview') },
  { id: 'breakdown' as const, label: t('pages.tokenAnalytics.drill.tabs.breakdown'), disabled: !breakdowns.value.length },
  { id: 'events' as const, label: t('pages.tokenAnalytics.drill.tabs.events', { n: kpi.value?.eventCount ?? 0 }) },
])

function onChartClick(params: unknown) {
  const path = drillFromChartEvent(params)
  if (path) drillInto(path)
}

function onEventDrill(step: DrillTarget) {
  drillInto([step])
}

function exportEvents() {
  const header = ['time', 'source', 'phase', 'status', 'project', 'workflow', 'run', 'node_type', 'model', 'total', 'input', 'output', 'cache_read', 'cache_write', 'cost']
  const rows = events.value.map((r) => [
    r.at, r.source, r.phase || '', r.status, r.projectName || r.projectId || '', r.workflowName || '', r.runTitle || r.runId || '',
    r.nodeType || '', r.modelKey, r.total, r.inputTokens, r.outputTokens, r.cacheReadTokens, r.cacheWriteTokens, r.priced ? r.cost : '',
  ])
  downloadCsv(`token-usage-events-p${eventsPage.value}.csv`, toCsv(header, rows))
}

function applyToPage() {
  emit('apply', filters.value)
}

function openRun() {
  const s = current.value
  if (s?.dim !== 'run') return
  emit('close')
  void router.push(`/runs/${s.key}`)
}

function openProject() {
  const s = current.value
  if (s?.dim !== 'project') return
  emit('close')
  // plan coverage: g3.4 — drill-down keeps the page time range when opening the board
  void router.push({ path: `/projects/${s.key}`, query: boardQueryFromFilters(filters.value) })
}
</script>

<template>
  <AppModal :open="open" :width="1080" @close="emit('close')">
    <template #header>
      <nav class="flex min-w-0 flex-wrap items-center gap-1 text-[13px]" data-testid="token-drill-breadcrumb" :aria-label="t('pages.tokenAnalytics.drill.title')">
        <span class="shrink-0 font-semibold text-txt">{{ t('pages.tokenAnalytics.drill.title') }}</span>
        <template v-for="(s, i) in stack" :key="`${s.dim}:${s.key}`">
          <span class="text-txt3">/</span>
          <button
            type="button"
            class="max-w-[220px] truncate rounded px-1 py-0.5"
            :class="i === stack.length - 1 ? 'font-semibold text-accent-2' : 'text-txt2 hover:bg-elevated'"
            :title="crumbLabel(s)"
            :data-testid="`token-drill-crumb-${i}`"
            @click="goCrumb(i)"
          >{{ crumbLabel(s) }}</button>
        </template>
      </nav>
    </template>

    <div data-testid="token-drill-modal" class="flex min-h-[420px] flex-col gap-3 text-txt">
      <div v-if="loading && !stats" class="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7" data-testid="token-drill-loading">
        <div v-for="i in 7" :key="i" class="token-skeleton h-[58px] rounded-lg" />
        <div class="token-skeleton col-span-full h-[220px] rounded-lg" />
      </div>
      <div v-else-if="failed" class="py-16 text-center text-sm text-txt3">
        {{ t('pages.tokenAnalytics.loadFailed') }}
        <button type="button" class="ml-2 text-accent-2" @click="loadStats">{{ t('pages.tokenAnalytics.retry') }}</button>
      </div>
      <template v-else-if="stats">
        <div class="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7" :class="loading ? 'opacity-60' : ''" data-testid="token-drill-kpis">
          <div
            v-for="k in miniKpis"
            :key="k.id"
            class="rounded-lg border border-line bg-elevated/40 px-3 py-2"
            :title="k.title || undefined"
            :data-testid="`token-drill-kpi-${k.id}`"
          >
            <div class="text-[11px] text-txt3">{{ k.label }}</div>
            <div class="mt-0.5 text-[16px] font-bold tabular-nums">{{ k.value }}</div>
          </div>
        </div>

        <div class="flex gap-1 border-b border-line" role="tablist">
          <button
            v-for="tb in tabs"
            :key="tb.id"
            type="button"
            role="tab"
            class="-mb-px border-b-2 px-3 py-1.5 text-xs"
            :class="[
              tab === tb.id ? 'border-accent font-semibold text-txt' : 'border-transparent text-txt3 hover:text-txt2',
              tb.disabled ? 'cursor-not-allowed opacity-40' : '',
            ]"
            :aria-selected="tab === tb.id"
            :disabled="tb.disabled"
            :data-testid="`token-drill-tab-${tb.id}`"
            @click="tab = tb.id"
          >{{ tb.label }}</button>
        </div>

        <div v-if="stats.empty" class="py-14 text-center text-sm text-txt3">{{ t('pages.tokenAnalytics.emptyHint') }}</div>

        <template v-else-if="tab === 'overview'">
          <div class="h-[220px] overflow-visible">
            <VChart v-if="trendOption" :option="trendOption" autoresize class="h-full w-full" @click="onChartClick" />
          </div>
          <div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
            <div v-for="pie in [
              { id: 'parts', option: partsOption, title: t('pages.tokenAnalytics.charts.compPie') },
              { id: 'source', option: sourceOption, title: t('pages.tokenAnalytics.charts.sourcePie') },
              { id: 'status', option: statusOption, title: t('pages.tokenAnalytics.charts.statusPie') },
            ].filter((p) => p.option)" :key="pie.id" class="rounded-lg border border-line/70 p-2">
              <p class="m-0 text-center text-xs font-semibold">{{ pie.title }}</p>
              <VChart :option="pie.option!" autoresize class="h-[150px] w-full" @click="onChartClick" />
            </div>
          </div>
          <p class="m-0 text-[11px] text-txt3">{{ t('pages.tokenAnalytics.drill.nestedHint') }}</p>
        </template>

        <div v-else-if="tab === 'breakdown'" class="grid grid-cols-1 gap-2.5 lg:grid-cols-2" data-testid="token-drill-breakdowns">
          <div v-for="b in breakdowns" :key="b.id" class="rounded-lg border border-line/70 p-2" :data-testid="`token-drill-breakdown-${b.id}`">
            <p class="m-0 text-xs font-semibold">{{ b.title }}</p>
            <div class="h-[200px] overflow-visible">
              <VChart :option="b.option!" autoresize class="h-full w-full" @click="onChartClick" />
            </div>
          </div>
        </div>

        <div v-else class="flex min-h-0 flex-col gap-2">
          <TokenEventsTable
            v-model:sort="eventsSort"
            :items="events"
            :currency="eventsCurrency"
            :loading="eventsLoading"
            @drill="onEventDrill"
          />
          <Pagination
            :page="eventsPage"
            :page-size="eventsPageSize"
            :total="eventsTotal"
            @update:page="eventsPage = $event"
            @update:page-size="onPageSize"
            :loading="eventsLoading"
            :page-size-options="[20, 50, 100, 200]"
          />
        </div>
      </template>
    </div>

    <template #footer>
      <button
        v-if="tab === 'events' && events.length"
        type="button"
        class="mr-auto rounded-md border border-line px-3 py-1.5 text-xs text-txt2 hover:bg-elevated"
        data-testid="token-drill-export"
        @click="exportEvents"
      >{{ t('pages.tokenAnalytics.exportCsv') }}</button>
      <button
        v-if="current?.dim === 'run'"
        type="button"
        class="rounded-md border border-line px-3 py-1.5 text-xs text-txt2 hover:bg-elevated"
        data-testid="token-drill-open-run"
        @click="openRun"
      >{{ t('pages.tokenAnalytics.drill.openRun') }}</button>
      <button
        v-if="current?.dim === 'project'"
        type="button"
        class="rounded-md border border-line px-3 py-1.5 text-xs text-txt2 hover:bg-elevated"
        data-testid="token-drill-open-project"
        @click="openProject"
      >{{ t('pages.tokenAnalytics.drill.openProject') }}</button>
      <button
        type="button"
        class="rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90"
        data-testid="token-drill-apply"
        @click="applyToPage"
      >{{ t('pages.tokenAnalytics.drill.apply') }}</button>
    </template>
  </AppModal>
</template>

<style scoped>
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

/**
 * Execution stats panel: single/multi aggregation and candidate picker orchestration.
 */
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { NODE_DEFS } from '@/data/nodeRegistry'
import { fmtCompactDuration, fmtDuration, fmtMultiAvgDuration, fmtTime } from '@/lib/shared/format'
import { resolveNodeDisplayLabel } from '@/lib/run/resolveNodeDisplayLabel'
import {
  aggregateMultiRuns,
  aggregateSingleRun,
  bottleneckDisplayName,
  flattenProcesses,
  isInvalidStart,
  resolveRunWallSec,
  type MultiDimension,
  type SingleDimension,
} from '@/lib/run/runStats'
import {
  fmtCompactTokenCount,
  fmtCompactTokenRate,
  fmtTokenCount,
  fmtTokenRate,
  mergeTokenUsage,
} from '@/lib/run/tokenUsage'
import type { Run, TokenUsage, WFNode } from '@/lib/shared/types'

export type StatsTab = 'single' | 'multi'
type KpiTipId =
  | 'wall'
  | 'nodeSum'
  | 'gap'
  | 'tokens'
  | 'rate'
  | 'avgWall'
  | 'sumTokens'
  | 'avgTokens'
  | 'multiRate'

export interface ExecutionStatsProps {
  run: Run
  nodes: WFNode[]
  wallSec: number
  nowMs: number
  statsTab?: StatsTab
  unknownModelDisplayName?: string | null
}

export type ExecutionStatsEmit = { (e: 'update:statsTab', tab: StatsTab): void }


export function useExecutionStats(props: ExecutionStatsProps, emit: ExecutionStatsEmit) {

const { t, locale } = useI18n()

const localTab = ref<StatsTab>('single')
const tabControlled = computed(() => props.statsTab != null)
const statsTab = computed({
  get: () => props.statsTab ?? localTab.value,
  set: (v: StatsTab) => {
    if (props.statsTab != null) emit('update:statsTab', v)
    else localTab.value = v
  },
})
const singleDim = ref<SingleDimension>('process')
const multiDim = ref<MultiDimension>('node')

function labelFn(label: string | undefined, type: WFNode['type'], nodeId: string) {
  return resolveNodeDisplayLabel(label, type, t, { nodeId })
}

function typeLabel(type: WFNode['type']): string {
  const key = NODE_DEFS[type]?.label
  return key ? t(key) : type
}

function localizeItemLabel(label: string, type: WFNode['type'], isTypeDim: boolean): string {
  if (isTypeDim) return typeLabel(type)
  // NODE_DEFS keys left on merged type rows somehow
  if (label.startsWith('nodes.')) return t(label)
  return label
}

const singleSummary = computed(() =>
  aggregateSingleRun(props.run, props.nodes, singleDim.value, props.wallSec, props.nowMs, labelFn),
)

const singleItems = computed(() =>
  singleSummary.value.items.map((it) => ({
    ...it,
    label: localizeItemLabel(it.label, it.type, singleDim.value === 'type'),
  })),
)

const singleBottleneck = computed(() => {
  const bn = singleSummary.value.bottleneck
  if (!bn) return null
  const localized = {
    ...bn,
    label: localizeItemLabel(bn.label, bn.type, singleDim.value === 'type'),
  }
  return {
    item: localized,
    name: bottleneckDisplayName(localized, {
      iterationLabel: (n) => t('common.iterationN', { n }),
      mergeLabel: (n) => t('pages.executionStats.mergeCount', { n }),
    }),
  }
})

/** Panel-side merge of process usage for total-token tip (Timeline-aligned parts). */
const processAtoms = computed(() =>
  flattenProcesses(props.run, props.nodes, props.nowMs, labelFn),
)

const mergedUsage = computed((): TokenUsage => {
  const merged = mergeTokenUsage(...processAtoms.value.map((p) => p.usage))
  return (
    merged ?? {
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheWriteTokens: 0,
    }
  )
})

const modelUsageParts = computed(() =>
  processAtoms.value.map((p) => ({ usage: p.usage, usageByModel: p.usageByModel })),
)

const tipUsageParts = computed(() => {
  const u = mergedUsage.value
  return [
    {
      key: 'input',
      label: t('pages.executionTimeline.partInput'),
      full: u.inputTokens || 0,
      highlight: false,
    },
    {
      key: 'output',
      label: t('pages.executionTimeline.partOutput'),
      full: u.outputTokens || 0,
      highlight: false,
    },
    {
      key: 'cacheRead',
      label: t('pages.executionTimeline.partCacheRead'),
      full: u.cacheReadTokens || 0,
      highlight: true,
    },
    {
      key: 'cacheWrite',
      label: t('pages.executionTimeline.partCacheWrite'),
      full: u.cacheWriteTokens || 0,
      highlight: false,
    },
  ]
})

function durationTipText(sec: number): string {
  return t('pages.executionStats.tipDuration', {
    clock: fmtDuration(sec),
    sec: Math.floor(Math.max(0, sec)),
  })
}

function compactTokensMain(n: number | null | undefined): string {
  if (n == null) return t('pages.executionStats.dash')
  return `${fmtCompactTokenCount(n)} ${t('pages.executionStats.tokenUnit')}`
}

function compactRateMain(totalTokens: number | null, wallSec: number): string {
  if (totalTokens == null) return t('pages.executionStats.dash')
  return fmtCompactTokenRate(totalTokens, wallSec)
}

/** Multi token/s main: same formula as compactRateMain, but 0 wall → 0.00/s (F3). */
function multiRateMain(totalTokens: number | null, wallSec: number): string {
  if (totalTokens == null) return t('pages.executionStats.dash')
  if (wallSec <= 0) return '0.00/s'
  return fmtCompactTokenRate(totalTokens, wallSec)
}

function tokenRateTipText(totalTokens: number | null, wallSec: number): string {
  if (wallSec <= 0) return t('pages.executionStats.tipTokenRateZeroWall')
  const tokens = totalTokens ?? 0
  const rate = fmtTokenRate(tokens, wallSec) ?? '0'
  return t('pages.executionStats.tipTokenRate', {
    tokens: fmtTokenCount(tokens),
    sec: Math.floor(wallSec),
    rate,
  })
}

// LiveLog-style self-managed tip (hover + keyboard focus).
const openTip = ref<KpiTipId | null>(null)
const tipUid = useId().replace(/:/g, '')
function tipDomId(id: KpiTipId): string {
  return `stats-kpi-tip-${id}-${tipUid}`
}
function showTip(id: KpiTipId) {
  openTip.value = id
}
function hideTip(id: KpiTipId) {
  if (openTip.value === id) openTip.value = null
}

// —— Multi-run candidate list + cache ——
interface Candidate {
  id: string
  label: string
  listWallSec: number
  startedAt: string
  status?: Run['status']
}

const candidates = ref<Candidate[]>([])
const selectedIds = ref<Set<string>>(new Set())
const detailCache = ref<Record<string, Run>>({})
const detailErrors = ref<Record<string, string>>({})
const candidatesLoading = ref(false)
const detailsLoading = ref(false)
const candidatesError = ref<string | null>(null)
/** True until the first successful candidate load for the current run. */
const selectionInitialized = ref(false)
/** Candidate picker collapsed by default (Demo progressive disclosure). */
const pickerOpen = ref(false)
let loadGen = 0

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function localYmd(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

function startedAtYmd(iso: string): string | null {
  if (isInvalidStart(iso)) return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  return localYmd(d)
}

/** Large start clock HH:mm from startedAt (local); invalid / missing → 时间未知. */
function fmtStartHm(iso: string): string {
  if (isInvalidStart(iso)) return t('pages.executionStats.unknownTime')
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return t('pages.executionStats.unknownTime')
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

function fmtCandidateTime(iso: string): string {
  if (isInvalidStart(iso)) return t('pages.executionStats.unknownTime')
  return fmtTime(iso)
}

function fmtOlderDateLabel(iso: string): string {
  if (isInvalidStart(iso)) return t('pages.executionStats.unknownTime')
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return t('pages.executionStats.unknownTime')
  const loc = String(locale.value || 'zh-CN')
  if (loc.toLowerCase().startsWith('zh')) {
    return `${d.getMonth() + 1} 月 ${d.getDate()} 日`
  }
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

function dayGroupLabel(iso: string): string {
  const ymd = startedAtYmd(iso)
  if (!ymd) return t('pages.executionStats.unknownTime')
  const today = localYmd(new Date())
  const ydayD = new Date()
  ydayD.setDate(ydayD.getDate() - 1)
  const yday = localYmd(ydayD)
  if (ymd === today) return t('pages.executionStats.today')
  if (ymd === yday) return t('pages.executionStats.yesterday')
  return fmtOlderDateLabel(iso)
}

function wallSourceForId(id: string): Pick<Run, 'status' | 'startedAt' | 'durationSec'> {
  if (id === props.run.id) return props.run
  const cached = detailCache.value[id]
  if (cached) return cached
  const c = candidates.value.find((item) => item.id === id)
  return {
    status: c?.status ?? 'completed',
    startedAt: c?.startedAt ?? '',
    durationSec: c?.listWallSec ?? 0,
  }
}

/** Candidate / list duration uses the same validated wall as KPI contribution. */
function candidateWallSec(c: Candidate): number {
  const wall = resolveRunWallSec(wallSourceForId(c.id), props.nowMs)
  if (wall > 0) return wall
  return c.listWallSec > 0 ? c.listWallSec : 0
}

function durationWithPrefix(sec: number): string {
  return `${t('pages.executionStats.duration')} ${fmtDuration(sec)}`
}

function isCurrentRun(id: string): boolean {
  return id === props.run.id
}

function defaultSelectedIds(list: Candidate[], currentId: string): Set<string> {
  const defaults = new Set<string>()
  defaults.add(currentId)
  for (const c of list) {
    if (defaults.size >= 3) break
    defaults.add(c.id)
  }
  return defaults
}

async function loadCandidates() {
  const wfId = props.run.workflowId
  if (!wfId) {
    candidates.value = []
    return
  }
  candidatesLoading.value = true
  candidatesError.value = null
  try {
    const data = await api.listRuns({ wf: wfId, page: 1, pageSize: 20 })
    const items = data.items
    let nextCandidates: Candidate[] = items.map((r) => ({
      id: r.id,
      label: '#' + r.id.replace(/^run-/, ''),
      listWallSec: r.durationSec || 0,
      startedAt: r.startedAt || '',
      status: r.status,
    }))
    // If current run missing from page, still keep it selected.
    if (!nextCandidates.some((c) => c.id === props.run.id)) {
      nextCandidates = [
        {
          id: props.run.id,
          label: '#' + props.run.id.replace(/^run-/, ''),
          listWallSec: props.run.durationSec || 0,
          startedAt: props.run.startedAt,
          status: props.run.status,
        },
        ...nextCandidates,
      ].slice(0, 20)
    }
    candidates.value = nextCandidates

    if (!selectionInitialized.value) {
      selectedIds.value = defaultSelectedIds(nextCandidates, props.run.id)
      selectionInitialized.value = true
    } else {
      // Refresh: keep ids still present in the new candidate set.
      const valid = new Set(nextCandidates.map((c) => c.id))
      const kept = new Set([...selectedIds.value].filter((id) => valid.has(id)))
      if (!kept.size) {
        selectedIds.value = defaultSelectedIds(nextCandidates, props.run.id)
      } else {
        // Ensure current run stays selected when still in candidates.
        if (valid.has(props.run.id)) kept.add(props.run.id)
        selectedIds.value = kept
      }
    }
  } catch (e: any) {
    candidatesError.value = e?.message || t('pages.executionStats.loadCandidatesFailed')
  } finally {
    candidatesLoading.value = false
  }
}

watch(
  () => [statsTab.value, props.run.workflowId, props.run.id] as const,
  ([tab], prev) => {
    const prevId = prev?.[2]
    if (prevId != null && prevId !== props.run.id) {
      selectionInitialized.value = false
      selectedIds.value = new Set()
      detailCache.value = {}
      detailErrors.value = {}
      pickerOpen.value = false
    }
    if (tab === 'multi') loadCandidates()
  },
  { immediate: true },
)

watch(
  selectedIds,
  async (ids) => {
    const gen = ++loadGen
    const missing = [...ids].filter((id) => id !== props.run.id && !detailCache.value[id])
    if (!missing.length) return
    detailsLoading.value = true
    try {
      await Promise.all(
        missing.map(async (id) => {
          try {
            const r = await api.getRun(id)
            if (gen !== loadGen) return
            detailCache.value = { ...detailCache.value, [id]: r }
            const { [id]: _, ...rest } = detailErrors.value
            detailErrors.value = rest
          } catch {
            if (gen !== loadGen) return
            detailErrors.value = {
              ...detailErrors.value,
              [id]: t('pages.executionStats.loadRunFailed', { id: id.replace(/^run-/, '') }),
            }
          }
        }),
      )
    } finally {
      if (gen === loadGen) detailsLoading.value = false
    }
  },
  { deep: true },
)

function toggleRun(id: string) {
  // Current detail run cannot be deselected (f10).
  if (id === props.run.id && selectedIds.value.has(id)) return
  const next = new Set(selectedIds.value)
  if (next.has(id)) {
    if (next.size <= 1) return
    next.delete(id)
  } else {
    next.add(id)
  }
  selectedIds.value = next
}

function togglePicker() {
  pickerOpen.value = !pickerOpen.value
}

/** Selected compare-bar cells in candidate list order (near → far), not Set insertion order. */
const selectedCandidates = computed(() => candidates.value.filter((c) => selectedIds.value.has(c.id)))

const selectedRunsTitle = computed(() =>
  t('pages.executionStats.selectedRunsCount', {
    n: selectedIds.value.size,
    total: candidates.value.length,
  }),
)

interface CandidateGroup {
  key: string
  label: string
  items: Candidate[]
}

const candidateGroups = computed((): CandidateGroup[] => {
  const today = localYmd(new Date())
  const ydayD = new Date()
  ydayD.setDate(ydayD.getDate() - 1)
  const yday = localYmd(ydayD)
  const buckets = new Map<string, CandidateGroup>()
  for (const c of candidates.value) {
    const ymd = startedAtYmd(c.startedAt)
    const key = ymd ?? 'unknown'
    let group = buckets.get(key)
    if (!group) {
      let label: string
      if (key === 'unknown') label = t('pages.executionStats.unknownTime')
      else if (key === today) label = t('pages.executionStats.today')
      else if (key === yday) label = t('pages.executionStats.yesterday')
      else label = fmtOlderDateLabel(c.startedAt)
      group = { key, label, items: [] }
      buckets.set(key, group)
    }
    group.items.push(c)
  }
  const out: CandidateGroup[] = []
  const todayG = buckets.get(today)
  const ydayG = buckets.get(yday)
  const unknownG = buckets.get('unknown')
  if (todayG) out.push(todayG)
  if (ydayG) out.push(ydayG)
  for (const g of buckets.values()) {
    if (g.key === today || g.key === yday || g.key === 'unknown') continue
    out.push(g)
  }
  if (unknownG) out.push(unknownG)
  return out
})

const multiInputs = computed(() => {
  const out: { run: Run; wallSec: number; nodes?: WFNode[] }[] = []
  for (const id of selectedIds.value) {
    if (id === props.run.id) {
      // Never trust props.wallSec (elapsedSec) — clamp year-1 / unset starts to 0.
      out.push({ run: props.run, wallSec: resolveRunWallSec(props.run, props.nowMs), nodes: props.nodes })
      continue
    }
    const r = detailCache.value[id]
    if (!r) continue
    let wall = resolveRunWallSec(r, props.nowMs)
    if (wall <= 0) {
      const listWall = candidates.value.find((c) => c.id === id)?.listWallSec || 0
      if (listWall > 0) wall = listWall
    }
    out.push({ run: r, wallSec: wall, nodes: r.nodes })
  }
  return out
})

const multiSummary = computed(() =>
  aggregateMultiRuns(multiInputs.value, multiDim.value, props.nowMs, labelFn),
)

const multiAvgWallSec = computed(() => {
  if (!multiSummary.value.selectedCount) return 0
  return multiSummary.value.wallSumSec / multiSummary.value.selectedCount
})

const multiItems = computed(() =>
  multiSummary.value.items.map((it) => ({
    ...it,
    label: localizeItemLabel(it.label, it.type, multiDim.value === 'type'),
  })),
)

const pendingDetailCount = computed(
  () =>
    [...selectedIds.value].filter((id) => id !== props.run.id && !detailCache.value[id] && !detailErrors.value[id])
      .length,
)

const multiReady = computed(() => pendingDetailCount.value === 0 && !detailsLoading.value)

const selectedCountDisplay = computed(() => selectedIds.value.size)

const pieFootnote = computed(() =>
  singleSummary.value.gapSec > 0 ? t('pages.executionStats.pieRelativeNote') : undefined,
)

const dimOptionsSingle = computed(() => [
  { id: 'process' as const, label: t('pages.executionStats.dimProcess') },
  { id: 'node' as const, label: t('pages.executionStats.dimNode') },
  { id: 'type' as const, label: t('pages.executionStats.dimType') },
])

const dimOptionsMulti = computed(() => [
  { id: 'node' as const, label: t('pages.executionStats.dimNode') },
  { id: 'type' as const, label: t('pages.executionStats.dimType') },
])

const singleNote = computed(() => {
  if (singleDim.value === 'process') return t('pages.executionStats.noteProcess')
  if (singleDim.value === 'node') return t('pages.executionStats.noteNode')
  return t('pages.executionStats.noteType')
})

const pieCenterSub = computed(() => {
  if (singleDim.value === 'process') return t('pages.executionStats.pieProcess')
  if (singleDim.value === 'node') return t('pages.executionStats.pieNode')
  return t('pages.executionStats.pieType')
})

const maxMultiAvg = computed(() => multiItems.value[0]?.avgSec || 1)

function fmtTokensOrDash(n: number | null | undefined): string {
  if (n == null) return t('pages.executionStats.dash')
  return fmtTokenCount(n)
}

const singleTokenHint = computed(() =>
  singleSummary.value.totalTokens == null
    ? t('pages.executionStats.hintTotalTokensNone')
    : t('pages.executionStats.hintTotalTokens'),
)

const singleRateHint = computed(() =>
  singleSummary.value.totalTokens == null
    ? t('pages.executionStats.hintTokenRateNone')
    : t('pages.executionStats.hintTokenRate'),
)

const multiSumHint = computed(() =>
  multiSummary.value.totalTokens == null
    ? t('pages.executionStats.hintSumTokensNone')
    : t('pages.executionStats.hintSumTokens'),
)

const multiAvgHint = computed(() =>
  multiSummary.value.totalTokens == null
    ? t('pages.executionStats.hintAvgTokensNone')
    : t('pages.executionStats.hintAvgTokens', { n: multiSummary.value.usageRunCount }),
)

const multiRateHint = computed(() =>
  multiSummary.value.totalTokens == null
    ? t('pages.executionStats.hintMultiTokenRateNone')
    : t('pages.executionStats.hintMultiTokenRate'),
)

  return {
  t,
  localTab,
  tabControlled,
  statsTab,
  singleDim,
  multiDim,
  labelFn,
  typeLabel,
  localizeItemLabel,
  singleSummary,
  singleItems,
  singleBottleneck,
  processAtoms,
  mergedUsage,
  modelUsageParts,
  tipUsageParts,
  durationTipText,
  compactTokensMain,
  compactRateMain,
  multiRateMain,
  tokenRateTipText,
  openTip,
  tipUid,
  tipDomId,
  showTip,
  hideTip,
  candidates,
  selectedIds,
  detailCache,
  detailErrors,
  candidatesLoading,
  detailsLoading,
  candidatesError,
  selectionInitialized,
  pickerOpen,
  loadGen,
  pad2,
  localYmd,
  startedAtYmd,
  fmtStartHm,
  fmtCandidateTime,
  fmtOlderDateLabel,
  dayGroupLabel,
  wallSourceForId,
  candidateWallSec,
  durationWithPrefix,
  isCurrentRun,
  defaultSelectedIds,
  loadCandidates,
  toggleRun,
  togglePicker,
  selectedCandidates,
  selectedRunsTitle,
  candidateGroups,
  multiInputs,
  multiSummary,
  multiAvgWallSec,
  multiItems,
  pendingDetailCount,
  multiReady,
  selectedCountDisplay,
  pieFootnote,
  dimOptionsSingle,
  dimOptionsMulti,
  singleNote,
  pieCenterSub,
  maxMultiAvg,
  fmtTokensOrDash,
  singleTokenHint,
  singleRateHint,
  multiSumHint,
  multiAvgHint,
  multiRateHint,
  fmtCompactDuration,
  fmtDuration,
  fmtMultiAvgDuration,
  fmtTokenCount,
  fmtCompactTokenCount,
  }
}

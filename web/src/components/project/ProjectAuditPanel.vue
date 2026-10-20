<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AuditFilterDropdown, { type AuditDdOption } from '@/components/project/AuditFilterDropdown.vue'
import Icon from '@/components/ui/Icon.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Pagination from '@/components/ui/Pagination.vue'
import { api } from '@/lib/api/api'
import { prettyAuditPayload } from '@/lib/shared/auditPayload'
import { AUDIT_SYSTEM_LABEL, formatAuditNodeName, formatAuditNodeTitle } from '@/lib/shared/auditNodeLabel'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useToast } from '@/lib/composables/useToast'
import { fmtTime } from '@/lib/shared/format'
import type {
  ProjectAuditEvent,
  ProjectAuditFacetResource,
  ProjectAuditFacetRun,
  ProjectAuditStats,
} from '@/lib/shared/types'

const props = defineProps<{
  projectId: string
  /** Test/demo hook: force the Demo「无权查看」state without a read-only role. */
  forceDenied?: boolean
}>()

const { t } = useI18n()
const toast = useToast()
const detailId = `audit-detail-${useId()}`
const { isMobile } = useBreakpoint()

type Mode = 'run' | 'all'

const loading = ref(false)
const denied = ref(false)
const events = ref<ProjectAuditEvent[]>([])
const total = ref(0)
const stats = ref<ProjectAuditStats>({ total: 0, mcp: 0, fail: 0 })
const page = ref(1)
const pageSize = ref(10)
const openId = ref<string | null>(null)
const mode = ref<Mode>('run')
const initialized = ref(false)
/** User overrides for Run group expand; missing key = Demo defaultOpen. */
const groupOpen = ref<Record<string, boolean>>({})
const runCapped = ref(false)
const runFetched = ref(0)

const RUN_FETCH_PAGE_SIZE = 100
const RUN_FETCH_MAX_PAGES = 5
const RUN_FETCH_HARD_CAP = 500
/** Mobile filter editor: default collapsed (plan g2 / Demo). */
const filtersExpanded = ref(false)

const timeWindow = ref<'24h' | '7d' | '30d'>('24h')
const runId = ref('')
const nodeId = ref('')
const callerKind = ref('')
const resource = ref('')
const search = ref('')

const runOptions = ref<ProjectAuditFacetRun[]>([])
const nodeOptions = ref<{ nodeId: string; label: string }[]>([])
const resourceOptions = ref<ProjectAuditFacetResource[]>([])

const AUDIT_PAGE_SIZE_OPTIONS = [5, 10, 20]

const CALLER_LABEL_KEYS: Record<string, string> = {
  pm: 'pages.projectDetail.audit.callerPm',
  apikey: 'pages.projectDetail.audit.callerApiKey',
  system: 'pages.projectDetail.audit.callerSystem',
  external: 'pages.projectDetail.audit.callerExternal',
}

const runDdOptions = computed<AuditDdOption[]>(() =>
  runOptions.value.map((r) => ({
    value: r.runId,
    label: r.label,
    sub: r.sub,
    dot: 'run',
  })),
)

const nodeDdOptions = computed<AuditDdOption[]>(() => [
  { value: '', label: t('pages.projectDetail.audit.filterAll') },
  ...nodeOptions.value.map((n) => ({
    value: n.nodeId,
    label: formatAuditNodeTitle(n.nodeId),
    sub: n.nodeId,
  })),
])

const callerDdOptions = computed<AuditDdOption[]>(() => [
  { value: '', label: t('pages.projectDetail.audit.filterAll') },
  { value: 'pm', label: t('pages.projectDetail.audit.callerPm'), sub: t('pages.projectDetail.audit.callerPmSub') },
  { value: 'apikey', label: t('pages.projectDetail.audit.callerApiKey'), sub: t('pages.projectDetail.audit.callerApiKeySub') },
  { value: 'system', label: t('pages.projectDetail.audit.callerSystem'), sub: t('pages.projectDetail.audit.callerSystemSub') },
  { value: 'external', label: t('pages.projectDetail.audit.callerExternal'), sub: t('pages.projectDetail.audit.callerExternalSub') },
])

const resourceDdOptions = computed<AuditDdOption[]>(() => [
  { value: '', label: t('pages.projectDetail.audit.filterAll'), dot: '' },
  ...resourceOptions.value.map((r) => {
    const value = r.resource || [r.resourceType, r.resourceId].filter(Boolean).join('/')
    return {
      value,
      label: value,
      sub: r.resourceType || '',
      short: value,
      dot: resDot(value),
    }
  }),
])

const timeDdOptions = computed<AuditDdOption[]>(() => [
  { value: '24h', label: t('pages.projectDetail.audit.time24h') },
  { value: '7d', label: t('pages.projectDetail.audit.time7d') },
  { value: '30d', label: t('pages.projectDetail.audit.time30d') },
])

type Chip = { key: string; label: string; value: string; clearable: boolean }

const chips = computed<Chip[]>(() => {
  const list: Chip[] = []
  if (mode.value === 'run') {
    if (nodeId.value) {
      list.push({
        key: 'node',
        label: t('pages.projectDetail.audit.colNode'),
        value: formatAuditNodeTitle(nodeId.value),
        clearable: true,
      })
    }
  } else if (callerKind.value) {
    list.push({
      key: 'caller',
      label: t('pages.projectDetail.audit.colCaller'),
      value: t(CALLER_LABEL_KEYS[callerKind.value] || '') || callerKind.value,
      clearable: true,
    })
  }
  if (resource.value) {
    list.push({
      key: 'resource',
      label: t('pages.projectDetail.audit.colResource'),
      value: resource.value,
      clearable: true,
    })
  }
  if (search.value.trim()) {
    list.push({
      key: 'search',
      label: t('pages.projectDetail.audit.filterSearch'),
      value: search.value.trim(),
      clearable: true,
    })
  }
  if (timeWindow.value !== '24h') {
    const lab =
      timeWindow.value === '7d'
        ? t('pages.projectDetail.audit.time7d')
        : t('pages.projectDetail.audit.time30d')
    list.push({
      key: 'time',
      label: t('pages.projectDetail.audit.filterTime'),
      value: lab,
      clearable: true,
    })
  }
  return list
})

const clearableChips = computed(() => chips.value.filter((c) => c.clearable))
const noRuns = computed(() => mode.value === 'run' && initialized.value && runOptions.value.length === 0)

const timeWindowLabel = computed(() => {
  if (timeWindow.value === '7d') return t('pages.projectDetail.audit.time7d')
  if (timeWindow.value === '30d') return t('pages.projectDetail.audit.time30d')
  return t('pages.projectDetail.audit.time24h')
})

/** One-line mobile filter summary (plan g2.1/g2.2); not the chips row. */
const filterSummaryText = computed(() => {
  const all = t('pages.projectDetail.audit.filterAll')
  const resLab = resource.value || all
  const timeLab = timeWindowLabel.value
  if (mode.value === 'run') {
    const runLab = runId.value
      ? runOptions.value.find((r) => r.runId === runId.value)?.label || shortRun(runId.value)
      : t('pages.projectDetail.audit.noRun')
    const nodeLab = nodeId.value ? formatAuditNodeTitle(nodeId.value) : all
    return `Run · ${runLab} · ${timeLab} · ${t('pages.projectDetail.audit.colNode')} ${nodeLab} / ${t('pages.projectDetail.audit.colResource')} ${resLab}`
  }
  const callerLab = callerKind.value
    ? t(CALLER_LABEL_KEYS[callerKind.value] || '') || callerKind.value
    : all
  return `${t('pages.projectDetail.audit.colCaller')} · ${callerLab} · ${timeLab} · ${t('pages.projectDetail.audit.colResource')} ${resLab}`
})

function toggleFiltersExpanded() {
  filtersExpanded.value = !filtersExpanded.value
}

function resDot(v: string) {
  if (!v) return ''
  if (v.startsWith('mcp/') || v === 'mcp') return 'mcp'
  if (v.startsWith('run/')) return 'run'
  if (v.startsWith('gate/')) return 'gate'
  if (v.startsWith('workflow/')) return 'wf'
  if (v.startsWith('project/')) return 'prj'
  if (v.startsWith('audit/')) return 'aud'
  return ''
}

function resGroup(o: AuditDdOption) {
  if (!o.value) return t('pages.projectDetail.audit.filterAll')
  const i = o.value.indexOf('/')
  return i > 0 ? o.value.slice(0, i) : 'other'
}

function shortRun(id: string) {
  const s = id.replace(/^run-/, '')
  return s.length > 8 ? s.slice(0, 8) : s
}

function callerLabel(ev: ProjectAuditEvent) {
  const kind = ev.callerKind || (ev.unattributable || ev.actor === 'system' ? 'system' : 'pm')
  const key = CALLER_LABEL_KEYS[kind]
  return key ? t(key) : kind
}

function nodeLabel(id?: string) {
  if (!id) return AUDIT_SYSTEM_LABEL
  return formatAuditNodeTitle(id)
}

/** Demo「类别 / 标识」辅行；缺字段则空，不回退到 action。 */
function resourceAux(ev: ProjectAuditEvent) {
  const type = (ev.resourceType || '').trim()
  const id = (ev.resourceId || '').trim()
  if (type && id) return `${type} / ${id}`
  const raw = (ev.resource || '').trim()
  if (!raw || raw === '—') return ''
  const i = raw.indexOf('/')
  if (i > 0 && i < raw.length - 1) {
    return `${raw.slice(0, i)} / ${raw.slice(i + 1)}`
  }
  return ''
}

type RunGroup = {
  id: string
  title: string
  type: string
  fullId: string
  events: ProjectAuditEvent[]
  ok: number
  fail: number
}

const runGroups = computed<RunGroup[]>(() => {
  if (mode.value !== 'run') return []
  const chronological = events.value.slice().sort((a, b) => {
    if (a.occurredAt === b.occurredAt) return a.id < b.id ? -1 : 1
    return a.occurredAt < b.occurredAt ? -1 : 1
  })
  const order: string[] = []
  const map = new Map<string, ProjectAuditEvent[]>()
  for (const ev of chronological) {
    const key = ev.nodeId?.trim() || '_system'
    if (!map.has(key)) {
      map.set(key, [])
      order.push(key)
    }
    map.get(key)!.push(ev)
  }
  return order.map((id) => {
    const list = map.get(id) || []
    const fail = list.filter((e) => e.outcome === 'fail').length
    const meta = id === '_system' ? formatAuditNodeName('') : formatAuditNodeName(id)
    return {
      id,
      title: meta.title,
      type: meta.type,
      fullId: id === '_system' ? '' : id,
      events: list,
      ok: list.length - fail,
      fail,
    }
  })
})

const statOk = computed(() => Math.max(0, (stats.value.total || 0) - (stats.value.fail || 0)))

const listFailCount = computed(
  () => events.value.filter((e) => e.outcome === 'fail').length,
)

function defaultGroupOpen(g: RunGroup) {
  if (runGroups.value.length === 1 || listFailCount.value === 0) return true
  return g.fail > 0
}

function isGroupOpen(g: RunGroup) {
  if (Object.prototype.hasOwnProperty.call(groupOpen.value, g.id)) {
    return groupOpen.value[g.id]
  }
  return defaultGroupOpen(g)
}

function pruneGroupOpen() {
  const ids = new Set(runGroups.value.map((g) => g.id))
  const cur = groupOpen.value
  const keys = Object.keys(cur)
  if (!keys.some((k) => !ids.has(k))) return
  const next: Record<string, boolean> = {}
  for (const k of keys) {
    if (ids.has(k)) next[k] = cur[k]!
  }
  groupOpen.value = next
}

function resetGroupOpen() {
  groupOpen.value = {}
}

function toggleGroup(id: string) {
  const g = runGroups.value.find((x) => x.id === id)
  if (!g) return
  groupOpen.value = { ...groupOpen.value, [id]: !isGroupOpen(g) }
}

function outcomeLabel(ev: ProjectAuditEvent) {
  return ev.outcome === 'fail'
    ? t('pages.projectDetail.audit.outcomeFail')
    : t('pages.projectDetail.audit.outcomeOk')
}

function resourceText(ev: ProjectAuditEvent) {
  return ev.resource || [ev.resourceType, ev.resourceId].filter(Boolean).join('/') || '—'
}

function prettyPayload(payload: Record<string, unknown> | undefined) {
  return prettyAuditPayload(payload)
}

function buildParams(extra?: { page?: number; pageSize?: number }) {
  const params: Record<string, string | number | undefined> = {
    time: timeWindow.value,
    resource: resource.value || undefined,
    search: search.value.trim() || undefined,
    page: extra?.page ?? page.value,
    pageSize: extra?.pageSize ?? pageSize.value,
  }
  if (mode.value === 'run') {
    params.runId = runId.value || undefined
    params.nodeId = nodeId.value || undefined
  } else {
    params.callerKind = callerKind.value || undefined
  }
  return params
}

async function loadFacets(selectedRun?: string) {
  if (props.forceDenied || denied.value) {
    runOptions.value = []
    nodeOptions.value = []
    resourceOptions.value = []
    return
  }
  try {
    const res = await api.listProjectAuditFacets(props.projectId, {
      time: timeWindow.value,
      runId: selectedRun || (mode.value === 'run' ? runId.value || undefined : undefined),
    })
    runOptions.value = Array.isArray(res.runs) ? res.runs : []
    nodeOptions.value = Array.isArray(res.nodes) ? res.nodes : []
    resourceOptions.value = Array.isArray(res.resources) ? res.resources : []
  } catch (e: any) {
    if (e?.status === 403) {
      denied.value = true
      runOptions.value = []
      nodeOptions.value = []
      resourceOptions.value = []
      return
    }
  }
}

async function loadRunAligned() {
  const collected: ProjectAuditEvent[] = []
  let nextPage = 1
  let hasMore = true
  let pageStats: ProjectAuditStats | undefined
  let fullTotal = 0
  while (hasMore && nextPage <= RUN_FETCH_MAX_PAGES && collected.length < RUN_FETCH_HARD_CAP) {
    const res = await api.listProjectAudit(
      props.projectId,
      buildParams({ page: nextPage, pageSize: RUN_FETCH_PAGE_SIZE }),
    )
    const items = res.items
    collected.push(...items)
    fullTotal = res.total
    if (!pageStats && res.stats) pageStats = res.stats
    hasMore = res.hasMore && items.length > 0
    nextPage += 1
    if (collected.length >= RUN_FETCH_HARD_CAP) break
  }
  const sliced = collected.slice(0, RUN_FETCH_HARD_CAP)
  events.value = sliced
  runFetched.value = sliced.length
  total.value = fullTotal
  page.value = 1
  runCapped.value = hasMore || sliced.length < fullTotal
  stats.value = pageStats || {
    total: fullTotal,
    mcp: sliced.filter((e) => e.action.startsWith('mcp')).length,
    fail: sliced.filter((e) => e.outcome === 'fail').length,
  }
}

async function load(resetPage = false) {
  if (props.forceDenied) {
    denied.value = true
    events.value = []
    total.value = 0
    stats.value = { total: 0, mcp: 0, fail: 0 }
    return
  }
  if (mode.value === 'run' && !runId.value) {
    events.value = []
    total.value = 0
    stats.value = { total: 0, mcp: 0, fail: 0 }
    runCapped.value = false
    runFetched.value = 0
    resetGroupOpen()
    loading.value = false
    return
  }
  if (resetPage) page.value = 1
  loading.value = true
  denied.value = false
  try {
    if (mode.value === 'run') {
      await loadRunAligned()
    } else {
      runCapped.value = false
      runFetched.value = 0
      const res = await api.listProjectAudit(props.projectId, buildParams())
      const pageData = res
      events.value = pageData.items
      total.value = pageData.total
      page.value = pageData.page
      stats.value = pageData.stats || {
        total: pageData.total,
        mcp: events.value.filter((e) => e.action.startsWith('mcp')).length,
        fail: events.value.filter((e) => e.outcome === 'fail').length,
      }
    }
  } catch (e: any) {
    if (e?.status === 403) {
      denied.value = true
      events.value = []
      total.value = 0
      return
    }
    toast.error(e?.message || t('pages.projectDetail.audit.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function bootstrap() {
  resetGroupOpen()
  if (props.forceDenied) {
    denied.value = true
    initialized.value = true
    return
  }
  loading.value = true
  await loadFacets()
  if (runOptions.value.length > 0) {
    mode.value = 'run'
    runId.value = runOptions.value[0]!.runId
    await loadFacets(runId.value)
  } else {
    // No runs in window — stay in run mode with empty state; user can switch to all.
    mode.value = 'run'
    runId.value = ''
  }
  initialized.value = true
  await load(true)
}

async function setMode(next: Mode) {
  if (mode.value === next) return
  mode.value = next
  openId.value = null
  resetGroupOpen()
  page.value = 1
  nodeId.value = ''
  callerKind.value = ''
  resource.value = ''
  if (next === 'run') {
    await loadFacets()
    if (!runId.value && runOptions.value.length) {
      runId.value = runOptions.value[0]!.runId
    }
    if (runId.value) await loadFacets(runId.value)
  } else {
    await loadFacets()
  }
  await load(true)
}

async function onRunChange(v: string) {
  runId.value = v
  nodeId.value = ''
  resource.value = ''
  openId.value = null
  resetGroupOpen()
  await loadFacets(v)
  await load(true)
}

function onFilterChange() {
  openId.value = null
  void load(true)
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    openId.value = null
    void load(true)
  }, 200)
}

function clearChip(key: string) {
  if (key === 'node') nodeId.value = ''
  if (key === 'caller') callerKind.value = ''
  if (key === 'resource') resource.value = ''
  if (key === 'search') search.value = ''
  if (key === 'time') timeWindow.value = '24h'
  void (async () => {
    await loadFacets(mode.value === 'run' ? runId.value : undefined)
    await load(true)
  })()
}

function clearOptional() {
  nodeId.value = ''
  callerKind.value = ''
  resource.value = ''
  search.value = ''
  timeWindow.value = '24h'
  void (async () => {
    await loadFacets(mode.value === 'run' ? runId.value : undefined)
    await load(true)
  })()
}

function toggleOpen(id: string) {
  openId.value = openId.value === id ? null : id
}

async function exportAudit() {
  if (denied.value || props.forceDenied) {
    toast.error(t('pages.projectDetail.audit.exportDenied'))
    return
  }
  try {
    const url = api.exportProjectAuditUrl(props.projectId, {
      format: 'json',
      time: timeWindow.value,
      callerKind: mode.value === 'all' ? callerKind.value || undefined : undefined,
      resource: resource.value || undefined,
      runId: mode.value === 'run' ? runId.value || undefined : undefined,
      nodeId: mode.value === 'run' ? nodeId.value || undefined : undefined,
      search: search.value.trim() || undefined,
    })
    const a = document.createElement('a')
    a.href = url
    a.download = ''
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    a.remove()
    toast.success(t('pages.projectDetail.audit.exportStarted', { format: 'JSON' }))
    setTimeout(() => void load(true), 400)
  } catch (e: any) {
    toast.error(e?.message || t('pages.projectDetail.audit.exportFailed'))
  }
}

function onPageChange(p: number) {
  page.value = p
  openId.value = null
  void load()
}

function onPageSizeChange(size: number) {
  pageSize.value = size || 10
  openId.value = null
  void load(true)
}

async function onTimeChange(v: string) {
  timeWindow.value = v as '24h' | '7d' | '30d'
  openId.value = null
  const prevRun = runId.value
  await loadFacets(mode.value === 'run' ? runId.value : undefined)
  if (mode.value === 'run' && runId.value && !runOptions.value.some((r) => r.runId === runId.value)) {
    runId.value = runOptions.value[0]?.runId || ''
    if (runId.value) await loadFacets(runId.value)
  }
  if (runId.value !== prevRun) resetGroupOpen()
  await load(true)
}

watch(runGroups, () => {
  pruneGroupOpen()
})

watch(
  () => [props.projectId, props.forceDenied],
  () => {
    initialized.value = false
    void bootstrap()
  },
)

onMounted(() => {
  void bootstrap()
})

onUnmounted(() => {
  resetGroupOpen()
})
</script>

<template>
  <div class="audit-panel" data-testid="project-audit-panel">
    <div
      v-if="denied || forceDenied"
      class="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-6 py-16 text-center"
      data-testid="project-audit-denied"
    >
      <div class="text-sm font-semibold text-txt">{{ t('pages.projectDetail.audit.deniedTitle') }}</div>
      <p class="max-w-md text-[13px] text-txt3">{{ t('pages.projectDetail.audit.deniedDesc') }}</p>
    </div>

    <template v-else>
      <p class="audit-intro">{{ t('pages.projectDetail.audit.description') }}</p>
      <div class="audit-toolbar">
        <div class="seg" role="group" :aria-label="t('pages.projectDetail.audit.title')" data-testid="project-audit-mode">
          <button type="button" :class="{ on: mode === 'run' }" :aria-pressed="mode === 'run'" data-testid="project-audit-mode-run" @click="setMode('run')">
            {{ t('pages.projectDetail.audit.modeRun') }}
          </button>
          <button type="button" :class="{ on: mode === 'all' }" :aria-pressed="mode === 'all'" data-testid="project-audit-mode-all" @click="setMode('all')">
            {{ t('pages.projectDetail.audit.modeAll') }}
          </button>
        </div>
        <div class="toolbar-end">
          <div class="toolbar-stats" data-testid="project-audit-stats">
            <span class="stat-chip" data-testid="project-audit-run-count">{{ t('pages.projectDetail.audit.statTotal') }} <b>{{ stats.total }}</b></span>
            <span class="stat-chip ok">{{ t('pages.projectDetail.audit.statOk') }} <b>{{ statOk }}</b></span>
            <span class="stat-chip" :class="{ fail: stats.fail }">{{ t('pages.projectDetail.audit.statFail') }} <b>{{ stats.fail }}</b></span>
            <span class="stat-chip">MCP <b>{{ stats.mcp }}</b></span>
          </div>
          <button type="button" class="btn export-btn" data-testid="project-audit-export" @click="exportAudit">
            <Icon name="download" :size="14" />{{ t('pages.projectDetail.audit.export') }}
          </button>
        </div>
      </div>
      <div class="filters" :class="{ 'filters-mobile': isMobile }" data-testid="project-audit-filters">
        <div class="search">
          <Icon name="search" :size="14" />
          <input v-model="search" type="search" :aria-label="t('pages.projectDetail.audit.filterSearch')" :placeholder="t('pages.projectDetail.audit.searchPlaceholder')" autocomplete="off" data-testid="project-audit-search" @input="onSearchInput" />
        </div>
        <button v-if="isMobile" type="button" class="filter-summary" data-testid="project-audit-filter-summary" :aria-expanded="filtersExpanded" @click="toggleFiltersExpanded">
          <Icon name="filter" :size="14" />
          <span class="sum-text">{{ filterSummaryText }}</span>
          <span class="sum-action">{{ filtersExpanded ? t('pages.projectDetail.audit.filterCollapse') : t('pages.projectDetail.audit.filterExpand') }}</span>
        </button>
        <div v-show="!isMobile || filtersExpanded" class="filters-editor" :class="{ 'run-filters': mode === 'run' }" data-testid="project-audit-filters-editor">
          <AuditFilterDropdown v-if="mode === 'run'" :model-value="runId" label-key="Run" :options="runDdOptions" searchable :width="320" :block="isMobile" :empty-label="t('pages.projectDetail.audit.noRun')" test-id="project-audit-run" @update:model-value="onRunChange" />
          <AuditFilterDropdown v-if="mode === 'run'" :model-value="nodeId" :label-key="t('pages.projectDetail.audit.colNode')" :options="nodeDdOptions" searchable :block="isMobile" :empty-label="t('pages.projectDetail.audit.filterAll')" test-id="project-audit-node" @update:model-value="(v) => { nodeId = v; onFilterChange() }" />
          <AuditFilterDropdown v-if="mode === 'all'" :model-value="callerKind" :label-key="t('pages.projectDetail.audit.colCaller')" :options="callerDdOptions" :block="isMobile" :empty-label="t('pages.projectDetail.audit.filterAll')" test-id="project-audit-caller" @update:model-value="(v) => { callerKind = v; onFilterChange() }" />
          <AuditFilterDropdown :model-value="resource" :label-key="t('pages.projectDetail.audit.colResource')" :options="resourceDdOptions" searchable :width="320" :block="isMobile" :empty-label="t('pages.projectDetail.audit.filterAll')" :group-by="resGroup" test-id="project-audit-resource" @update:model-value="(v) => { resource = v; onFilterChange() }" />
          <AuditFilterDropdown :model-value="timeWindow" :label-key="t('pages.projectDetail.audit.filterTime')" :options="timeDdOptions" :block="isMobile" test-id="project-audit-time" @update:model-value="onTimeChange" />
        </div>
      </div>

      <div v-if="!isMobile && chips.length" class="chips" data-testid="project-audit-chips">
        <span v-for="ch in chips" :key="ch.key" class="chip">
          <em>{{ ch.label }}</em>
          <b>{{ ch.value }}</b>
          <button
            v-if="ch.clearable"
            type="button"
            class="x"
            :data-testid="`project-audit-chip-clear-${ch.key}`"
            @click="clearChip(ch.key)"
          >
            ×
          </button>
        </span>
        <button
          v-if="clearableChips.length"
          type="button"
          class="linkish"
          data-testid="project-audit-clear"
          @click="clearOptional"
        >
          {{ t('pages.projectDetail.audit.clearFilters') }}
        </button>
      </div>

      <p v-if="mode === 'run' && runCapped && !loading" class="cap-hint" data-testid="project-audit-capped">
        {{ t('pages.projectDetail.audit.runCapped', { n: runFetched }) }}
      </p>

      <div v-if="loading" class="list-placeholder text-[13px] text-txt2">
        {{ t('pages.projectDetail.audit.loading') }}
      </div>
      <div
        v-else-if="noRuns"
        class="empty list-placeholder"
        data-testid="project-audit-empty-runs"
      >
        <div class="big">{{ t('pages.projectDetail.audit.emptyRunsTitle') }}</div>
        <div>
          {{ t('pages.projectDetail.audit.emptyRunsDesc') }}
          <button type="button" class="linkish" @click="setMode('all')">
            {{ t('pages.projectDetail.audit.modeAll') }}
          </button>
        </div>
      </div>
      <EmptyState
        v-else-if="!events.length"
        class="list-placeholder"
        data-testid="project-audit-empty"
        :title="t('pages.projectDetail.audit.emptyTitle')"
        :desc="t('pages.projectDetail.audit.emptyDesc')"
      >
        <button
          v-if="mode === 'run'"
          type="button"
          class="linkish"
          data-testid="project-audit-empty-all"
          @click="setMode('all')"
        >
          {{ t('pages.projectDetail.audit.modeAll') }}
        </button>
      </EmptyState>
      <!-- Mobile event cards (plan g3); shared by run + all modes -->
      <div
        v-else-if="isMobile"
        class="event-cards"
        data-testid="project-audit-list"
        data-layout="cards"
      >
        <button
          v-for="ev in events"
          :key="ev.id"
          type="button"
          class="event-card"
          :class="{ open: openId === ev.id, fail: ev.outcome === 'fail' }"
          :data-testid="`project-audit-event-${ev.id}`"
          :aria-expanded="openId === ev.id"
          :aria-controls="`${detailId}-${ev.id}`"
          @click="toggleOpen(ev.id)"
        >
          <div class="ec-top">
            <span class="ec-time mono" :title="ev.occurredAt">{{ fmtTime(ev.occurredAt) }}</span>
            <span class="outcome" :class="ev.outcome === 'fail' ? 'bad' : 'ok'"><i aria-hidden="true" />{{ outcomeLabel(ev) }}<Icon name="chevron-down" :size="12" class="event-chevron" :class="{ expanded: openId === ev.id }" /></span>
          </div>
          <div class="ec-sum" :class="{ open: openId === ev.id }">{{ ev.summary }}</div>
          <div class="ec-row">
            <span class="k">{{ t('pages.projectDetail.audit.colNode') }}</span>
            {{ nodeLabel(ev.nodeId) }}
            <span v-if="resourceAux(ev)" class="ec-aux">{{ resourceAux(ev) }}</span>
          </div>
          <div v-if="openId === ev.id" :id="`${detailId}-${ev.id}`" class="ec-detail" @click.stop>
            <div>
              <span class="k">{{ t('pages.projectDetail.audit.colCaller') }}</span>
              {{ callerLabel(ev) }}
            </div>
            <div>
              <span class="k">{{ t('pages.projectDetail.audit.colResource') }}</span>
              {{ resourceText(ev) }}
            </div>
            <div v-if="mode === 'all' && ev.runId">
              <span class="k">{{ t('pages.projectDetail.audit.colRun') }}</span>
              <code>{{ ev.runId }}</code>
            </div>
            <div>
              <span class="k">action</span>
              <code>{{ ev.action }}</code>
            </div>
            <pre
              class="payload"
              data-testid="project-audit-payload"
              v-html="prettyPayload(ev.payload)"
            />
          </div>
          <div v-if="openId === ev.id" class="ec-hint">
            {{
              openId === ev.id
                ? t('pages.projectDetail.audit.cardCollapseHint')
                : t('pages.projectDetail.audit.cardExpandHint')
            }}
          </div>
        </button>
      </div>
      <!-- Desktop: Run groups (g3/g4.1) or all-logs 4-col table (g4.2) -->
      <div
        v-else-if="mode === 'run'"
        class="groups-wrap"
        data-testid="project-audit-list"
        data-layout="groups"
      >
        <div class="audit-column-labels" aria-hidden="true"><span>{{ t('pages.projectDetail.audit.colTime') }}</span><span>{{ t('pages.projectDetail.audit.colSummary') }}</span><span>{{ t('pages.projectDetail.audit.colOutcome') }}</span></div>
        <div
          v-for="g in runGroups"
          :key="g.id"
          class="group"
          :class="{ open: isGroupOpen(g) }"
          :data-testid="`project-audit-group-${g.id}`"
          :data-open="isGroupOpen(g) ? 'true' : 'false'"
        >
          <button
            type="button"
            class="group-head"
            :aria-expanded="isGroupOpen(g)"
            :aria-controls="`${detailId}-group-${g.id}`"
            :data-testid="`project-audit-group-head-${g.id}`"
            @click="toggleGroup(g.id)"
          >
            <span class="chev" aria-hidden="true" />
            <span class="group-meta">
              <span class="group-name">
                <span class="node-dot" :class="g.type" />
                <span class="group-title">{{ g.title }}</span>
                <span v-if="g.fullId" class="nid" :title="g.fullId">{{ g.fullId }}</span>
              </span>
              <span class="group-sub">
                {{ t('pages.projectDetail.audit.groupEvents', { n: g.events.length }) }}
              </span>
            </span>
            <span class="group-counts">
              <span class="stat-chip ok">{{ t('pages.projectDetail.audit.statOk') }} <strong>{{ g.ok }}</strong></span>
              <span v-if="g.fail" class="stat-chip fail">{{ t('pages.projectDetail.audit.statFail') }} <strong>{{ g.fail }}</strong></span>
            </span>
          </button>
          <div v-show="isGroupOpen(g)" :id="`${detailId}-group-${g.id}`" class="group-body">
            <table>
              <colgroup><col class="audit-time-col" /><col /><col class="audit-outcome-col" /></colgroup>
              <thead class="sr-only">
                <tr>
                  <th>{{ t('pages.projectDetail.audit.colTime') }}</th>
                  <th>{{ t('pages.projectDetail.audit.colSummary') }}</th>
                  <th>{{ t('pages.projectDetail.audit.colOutcome') }}</th>
                </tr>
              </thead>
              <tbody>
                <template v-for="ev in g.events" :key="ev.id">
                  <tr
                    class="row"
                    :class="{ open: openId === ev.id, fail: ev.outcome === 'fail' }"
                    :data-testid="`project-audit-event-${ev.id}`"
                    @click="toggleOpen(ev.id)"
                  >
                    <td>
                      <div class="time-main mono" :title="ev.occurredAt">{{ fmtTime(ev.occurredAt) }}</div>
                    </td>
                    <td class="summary">
                      <button type="button" class="summary-main" :title="ev.summary" :aria-expanded="openId === ev.id" :aria-controls="`${detailId}-${ev.id}`" @click.stop="toggleOpen(ev.id)">{{ ev.summary }}</button>
                      <div v-if="resourceAux(ev)" class="summary-aux" :title="resourceAux(ev)">{{ resourceAux(ev) }}</div>
                    </td>
                    <td>
                      <span class="outcome" :class="ev.outcome === 'fail' ? 'bad' : 'ok'"><i aria-hidden="true" />{{ outcomeLabel(ev) }}<Icon name="chevron-down" :size="12" class="event-chevron" :class="{ expanded: openId === ev.id }" /></span>
                    </td>
                  </tr>
                  <tr v-if="openId === ev.id" :id="`${detailId}-${ev.id}`" class="detail">
                    <td colspan="3">
                      <div class="detail-inner" @click.stop>
                        <div class="detail-meta">
                          <span>action <code>{{ ev.action }}</code></span>
                          <span v-if="ev.nodeId">{{ t('pages.projectDetail.audit.colNode') }} <code>{{ ev.nodeId }}</code></span>
                          <span>{{ t('pages.projectDetail.audit.colResource') }} <code>{{ resourceText(ev) }}</code></span>
                          <span v-if="ev.runId">Run <code>{{ ev.runId }}</code></span>
                          <span>{{ t('pages.projectDetail.audit.colCaller') }} <code>{{ callerLabel(ev) }}</code></span>
                        </div>
                        <pre
                          class="payload"
                          data-testid="project-audit-payload"
                          v-html="prettyPayload(ev.payload)"
                        />
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </div>
      </div>
      <div
        v-else
        class="table-wrap"
        data-testid="project-audit-list"
        data-layout="table"
      >
        <table>
          <colgroup><col class="audit-time-col" /><col class="audit-node-col" /><col /><col class="audit-outcome-col" /></colgroup>
          <thead>
            <tr>
              <th>{{ t('pages.projectDetail.audit.colTime') }}</th>
              <th>{{ t('pages.projectDetail.audit.colNode') }}</th>
              <th>{{ t('pages.projectDetail.audit.colSummary') }}</th>
              <th>{{ t('pages.projectDetail.audit.colOutcome') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="ev in events" :key="ev.id">
              <tr
                class="row"
                :class="{ open: openId === ev.id, fail: ev.outcome === 'fail' }"
                :data-testid="`project-audit-event-${ev.id}`"
                @click="toggleOpen(ev.id)"
              >
                <td>
                  <div class="time-main mono" :title="ev.occurredAt">{{ fmtTime(ev.occurredAt) }}</div>
                </td>
                <td>
                  <span class="node">{{ nodeLabel(ev.nodeId) }}</span>
                </td>
                <td class="summary">
                  <button type="button" class="summary-main" :title="ev.summary" :aria-expanded="openId === ev.id" :aria-controls="`${detailId}-${ev.id}`" @click.stop="toggleOpen(ev.id)">{{ ev.summary }}</button>
                  <div v-if="resourceAux(ev)" class="summary-aux" :title="resourceAux(ev)">{{ resourceAux(ev) }}</div>
                </td>
                <td>
                  <span class="outcome" :class="ev.outcome === 'fail' ? 'bad' : 'ok'"><i aria-hidden="true" />{{ outcomeLabel(ev) }}<Icon name="chevron-down" :size="12" class="event-chevron" :class="{ expanded: openId === ev.id }" /></span>
                </td>
              </tr>
              <tr v-if="openId === ev.id" :id="`${detailId}-${ev.id}`" class="detail">
                <td colspan="4">
                  <div class="detail-inner" @click.stop>
                    <div class="detail-meta">
                      <span>action <code>{{ ev.action }}</code></span>
                      <span v-if="ev.nodeId">{{ t('pages.projectDetail.audit.colNode') }} <code>{{ ev.nodeId }}</code></span>
                      <span>{{ t('pages.projectDetail.audit.colResource') }} <code>{{ resourceText(ev) }}</code></span>
                      <span v-if="ev.runId">Run <code>{{ ev.runId }}</code></span>
                      <span>{{ t('pages.projectDetail.audit.colCaller') }} <code>{{ callerLabel(ev) }}</code></span>
                    </div>
                    <pre
                      class="payload"
                      data-testid="project-audit-payload"
                      v-html="prettyPayload(ev.payload)"
                    />
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>

      <Pagination
        v-if="mode === 'all' && !noRuns"
        class="shrink-0"
        :page="page"
        :page-size="pageSize"
        :total="total"
        :loading="loading"
        :page-size-options="AUDIT_PAGE_SIZE_OPTIONS"
        summary-test-id="project-audit-pager-info"
        page-size-test-id="project-audit-page-size"
        @update:page="onPageChange"
        @update:page-size="onPageSizeChange"
      />
    </template>
  </div>
</template>

<style scoped>
.audit-panel {
  display: flex;
  flex: 1;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  container-type: inline-size;
}

.audit-intro {
  flex: none;
  margin: -6px 0 20px;
  color: rgb(var(--c-txt3));
  font-size: 13px;
  line-height: 1.7;
}

.audit-toolbar {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px 24px;
  border-bottom: 1px solid rgb(var(--c-line));
}

.seg {
  display: flex;
  align-self: stretch;
  gap: 24px;
}

.seg button {
  position: relative;
  min-height: 42px;
  padding: 0 0 12px;
  border: 0;
  background: transparent;
  color: rgb(var(--c-txt3));
  font: inherit;
  font-size: 13px;
  cursor: pointer;
}

.seg button:hover {
  color: rgb(var(--c-txt));
}

.seg button.on {
  color: rgb(var(--c-txt));
  font-weight: 600;
}

.seg button.on::after {
  position: absolute;
  right: 0;
  bottom: -1px;
  left: 0;
  height: 2px;
  border-radius: 2px;
  background: rgb(var(--c-accent));
  content: '';
}

.toolbar-end {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 20px;
  padding-bottom: 10px;
}

.toolbar-stats {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 14px;
}

.stat-chip {
  display: inline-flex;
  align-items: baseline;
  gap: 5px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  white-space: nowrap;
}

.stat-chip b, .stat-chip strong {
  color: rgb(var(--c-txt2));
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.stat-chip.ok b, .stat-chip.ok strong {
  color: rgb(var(--c-ok));
}

.stat-chip.fail, .stat-chip.fail b, .stat-chip.fail strong {
  color: rgb(var(--c-err));
}

.btn {
  display: inline-flex;
  min-height: 32px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 0 10px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt2));
  font: inherit;
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}

.btn:hover:not(:disabled) {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
}

.btn:disabled {
  opacity: .4;
  cursor: not-allowed;
}

.filters {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 10px;
  padding: 16px 0;
}

.search {
  display: flex;
  min-width: 0;
  max-width: 240px;
  height: 34px;
  flex: 1 1 190px;
  align-items: center;
  gap: 8px;
  padding: 0 10px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: rgb(var(--c-surface));
}

.search:focus-within {
  border-color: rgb(var(--c-accent) / .65);
  box-shadow: 0 0 0 2px rgb(var(--c-accent) / .1);
}

.search :deep(svg) {
  flex: none;
  color: rgb(var(--c-txt3));
}

.search input {
  width: 100%;
  min-width: 0;
  height: 100%;
  flex: 1;
  padding: 0;
  border: 0;
  outline: none;
  background: transparent;
  color: rgb(var(--c-txt));
  font: inherit;
  font-size: 12px;
}

.search input::placeholder {
  color: rgb(var(--c-txt3));
}

.filters-editor {
  display: flex;
  min-width: 0;
  flex: 4 1 600px;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.filters-editor :deep(.audit-dd) {
  min-width: 0;
  max-width: 100%;
}

.filters-editor :deep(.audit-dd-trig) {
  max-width: 100%;
}

.chips {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 0 0 14px;
}

.chip {
  display: inline-flex;
  max-width: 100%;
  min-height: 26px;
  align-items: center;
  gap: 5px;
  padding: 3px 5px 3px 8px;
  border-radius: 6px;
  background: rgb(var(--c-accent) / .06);
  color: rgb(var(--c-txt2));
  font-size: 11px;
}

.chip em {
  flex: none;
  color: rgb(var(--c-txt3));
  font-style: normal;
}

.chip b {
  min-width: 0;
  overflow: hidden;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chip .x {
  width: 20px;
  height: 20px;
  flex: none;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: rgb(var(--c-txt3));
  font-size: 14px;
  cursor: pointer;
}

.chip .x:hover {
  background: rgb(var(--c-accent) / .1);
  color: rgb(var(--c-txt));
}

.linkish {
  padding: 3px 5px;
  border: 0;
  background: transparent;
  color: rgb(var(--c-accent));
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}

.cap-hint {
  flex: none;
  margin: 0 0 12px;
  padding: 8px 10px;
  border-left: 2px solid rgb(var(--c-accent));
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-accent) / .04);
  font-size: 12px;
  line-height: 1.6;
}

.table-wrap, .groups-wrap, .event-cards {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  background: rgb(var(--c-surface));
}

.audit-column-labels {
  display: grid;
  grid-template-columns: 148px minmax(0,1fr) 96px;
  padding: 9px 0;
  border-bottom: 1px solid rgb(var(--c-line));
  color: rgb(var(--c-txt3));
  font-size: 11px;
}

.audit-column-labels > span {
  padding: 0 10px;
}

.audit-column-labels > span:last-child {
  text-align: right;
}

.group {
  min-width: 0;
  border-bottom: 1px solid rgb(var(--c-line));
}

.group-head {
  display: grid;
  width: 100%;
  min-height: 50px;
  grid-template-columns: 12px minmax(0,1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 11px 10px;
  border: 0;
  background: transparent;
  color: rgb(var(--c-txt));
  text-align: left;
  font: inherit;
  cursor: pointer;
}

.group-head:hover {
  background: rgb(var(--c-elevated) / .5);
}

.chev {
  width: 6px;
  height: 6px;
  border-right: 1.5px solid rgb(var(--c-txt3));
  border-bottom: 1.5px solid rgb(var(--c-txt3));
  transform: rotate(-45deg);
  transition: transform .15s ease;
}

.group.open .chev {
  transform: rotate(45deg);
}

.group-meta {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 12px;
}

.group-name {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
}

.group-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-name .nid {
  min-width: 0;
  max-width: 180px;
  overflow: hidden;
  color: rgb(var(--c-txt3));
  font: 10px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-dot {
  width: 6px;
  height: 6px;
  flex: none;
  border-radius: 50%;
  background: rgb(var(--c-txt3));
}

.node-dot.input, .node-dot.output {
  background: rgb(var(--c-info));
}

.node-dot.clarify, .node-dot.implement, .node-dot.test_review, .node-dot.agent {
  background: rgb(var(--c-accent));
}

.node-dot.human_gate {
  background: rgb(var(--c-warn));
}

.node-dot.system {
  background: rgb(var(--c-ok));
}

.group-sub {
  flex: none;
  color: rgb(var(--c-txt3));
  font-size: 11px;
}

.group-counts {
  display: flex;
  flex: none;
  gap: 12px;
}

.group-body {
  min-width: 0;
}

table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  border-spacing: 0;
}

.audit-time-col {
  width: 148px;
}

.audit-node-col {
  width: 140px;
}

.audit-outcome-col {
  width: 96px;
}

thead:not(.sr-only) th {
  position: sticky;
  top: 0;
  z-index: 2;
  padding: 9px 10px;
  border-bottom: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt3));
  text-align: left;
  font-size: 11px;
  font-weight: 500;
}

thead:not(.sr-only) th:last-child {
  text-align: right;
}

td {
  padding: 9px 10px;
  border-bottom: 1px solid rgb(var(--c-line) / .55);
  vertical-align: middle;
  font-size: 12px;
}

.group tbody:last-child tr:last-child td {
  border-bottom: 0;
}

tr.row {
  cursor: pointer;
}

tr.row:hover td {
  background: rgb(var(--c-elevated) / .5);
}

tr.row.open td {
  background: rgb(var(--c-accent) / .045);
}

tr.row.fail td:first-child {
  box-shadow: inset 2px 0 0 rgb(var(--c-err) / .65);
}

.summary {
  min-width: 0;
}

.summary-main {
  display: -webkit-box;
  width: 100%;
  margin: 0;
  padding: 0;
  overflow: hidden;
  border: 0;
  background: transparent;
  color: rgb(var(--c-txt));
  text-align: left;
  font: inherit;
  font-size: 13px;
  line-height: 1.65;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow-wrap: anywhere;
  cursor: pointer;
}

tr.row.open .summary-main {
  -webkit-line-clamp: unset;
}

.summary-aux {
  margin-top: 1px;
  overflow: hidden;
  color: rgb(var(--c-txt3));
  font: 10px/1.6 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mono {
  color: rgb(var(--c-txt3));
  font: 11px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.node {
  display: -webkit-box;
  overflow: hidden;
  color: rgb(var(--c-txt2));
  font-size: 11px;
  line-height: 1.6;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow-wrap: anywhere;
}

.outcome {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  white-space: nowrap;
  font-size: 11px;
}

.outcome i {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: currentColor;
}

.ok {
  color: rgb(var(--c-ok));
}

.bad {
  color: rgb(var(--c-err));
}

.event-chevron {
  color: rgb(var(--c-txt3));
  transition: transform .15s ease;
}

.event-chevron.expanded {
  transform: rotate(180deg);
}

tr.detail td {
  padding: 0;
  border-bottom: 1px solid rgb(var(--c-line));
}

.detail-inner {
  min-width: 0;
  margin: 0 10px 14px 158px;
  padding: 12px 0 0 14px;
  border-left: 2px solid rgb(var(--c-accent) / .3);
}

.detail-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
  margin-bottom: 12px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  line-height: 1.8;
  overflow-wrap: anywhere;
}

.detail-meta span {
  min-width: 0;
  max-width: 100%;
}

.detail-meta code, .ec-detail code {
  color: rgb(var(--c-txt2));
  font: 11px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  overflow-wrap: anywhere;
}

.payload {
  max-width: 100%;
  margin: 0;
  padding: 12px;
  border: 1px solid rgb(var(--c-line) / .6);
  border-radius: 6px;
  background: rgb(var(--c-elevated) / .45);
  color: rgb(var(--c-txt2));
  font: 11px/1.7 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  overflow-x: auto;
}

.list-placeholder {
  display: flex;
  min-height: 150px;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
}

.empty {
  padding: 48px 16px;
  color: rgb(var(--c-txt3));
  text-align: center;
  font-size: 12px;
}

.empty .big {
  color: rgb(var(--c-txt));
  font-size: 14px;
  font-weight: 600;
}

.filter-summary {
  display: flex;
  width: 100%;
  min-height: 44px;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border: 0;
  background: transparent;
  color: rgb(var(--c-txt3));
  text-align: left;
  font: inherit;
  cursor: pointer;
}

.sum-text {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sum-action {
  flex: none;
  color: rgb(var(--c-accent));
  font-size: 11px;
}

.filters-mobile {
  flex-direction: column;
  flex-wrap: nowrap;
  align-items: stretch;
  gap: 4px;
}

.filters-mobile .search {
  width: 100%;
  max-width: none;
  height: 44px;
  min-height: 44px;
  flex: none;
}

.filters-mobile .filters-editor {
  display: grid;
  width: 100%;
  flex: none;
  grid-template-columns: repeat(2,minmax(0,1fr));
  align-items: stretch;
  gap: 8px;
  padding: 8px 0;
}

.filters-mobile .filters-editor :deep(.audit-dd:first-child), .filters-mobile .filters-editor.run-filters :deep(.audit-dd:last-child) {
  grid-column: 1 / -1;
}

.event-cards {
  display: flex;
  flex-direction: column;
}

.event-card {
  display: block;
  width: 100%;
  flex: none;
  padding: 14px 2px;
  border: 0;
  border-bottom: 1px solid rgb(var(--c-line) / .65);
  border-radius: 0;
  background: transparent;
  color: rgb(var(--c-txt));
  text-align: left;
  font: inherit;
  cursor: pointer;
}

.event-card:hover {
  background: rgb(var(--c-elevated) / .4);
}

.event-card.open {
  background: rgb(var(--c-accent) / .035);
}

.event-card.fail {
  box-shadow: inset 2px 0 0 rgb(var(--c-err) / .65);
  padding-left: 10px;
}

.ec-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ec-sum {
  display: -webkit-box;
  margin-top: 7px;
  overflow: hidden;
  color: rgb(var(--c-txt));
  font-size: 13px;
  line-height: 1.75;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow-wrap: anywhere;
}

.ec-sum.open {
  display: block;
  -webkit-line-clamp: unset;
}

.ec-row {
  margin-top: 5px;
  color: rgb(var(--c-txt2));
  font-size: 11px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.ec-row .k {
  margin-right: 5px;
  color: rgb(var(--c-txt3));
}

.ec-aux {
  display: block;
  max-width: 100%;
  overflow: hidden;
  color: rgb(var(--c-txt3));
  font: 10px/1.7 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ec-detail {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid rgb(var(--c-line));
  color: rgb(var(--c-txt2));
  font-size: 11px;
  line-height: 1.8;
  overflow-wrap: anywhere;
}

.ec-detail .k {
  margin-right: 6px;
  color: rgb(var(--c-txt3));
}

.ec-detail .payload {
  margin-top: 10px;
  text-align: left;
}

.ec-hint {
  margin-top: 8px;
  color: rgb(var(--c-txt3));
  font-size: 10px;
}

.seg button:focus-visible, .btn:focus-visible, .linkish:focus-visible, .filter-summary:focus-visible, .group-head:focus-visible, .summary-main:focus-visible, .event-card:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .6);
  outline-offset: -2px;
  border-radius: 4px;
}

@container (max-width:780px) {
  .filters:not(.filters-mobile) .search {
    max-width: none;
    flex-basis: 100%;
  }
  .filters:not(.filters-mobile) .filters-editor {
    flex-basis: 100%;
  }
  .audit-toolbar {
    gap: 10px;
  }
  .toolbar-end {
    gap: 12px;
  }
  .toolbar-stats {
    gap: 10px;
  }
  .group-name .nid {
    max-width: 110px;
  }
  .detail-inner {
    margin-left: 10px;
  }
  .audit-time-col {
    width: 132px;
  }
  .audit-node-col {
    width: 100px;
  }
  .audit-column-labels {
    grid-template-columns: 132px minmax(0,1fr) 96px;
  }
}

@media (max-width:767px) {
  .audit-intro {
    margin-bottom: 16px;
    font-size: 12px;
  }
  .audit-toolbar {
    align-items: flex-start;
  }
  .seg {
    gap: 18px;
  }
  .seg button {
    min-height: 44px;
    padding-bottom: 10px;
    font-size: 12px;
  }
  .toolbar-end {
    display: contents;
  }
  .toolbar-stats {
    width: 100%;
    order: 2;
    padding-bottom: 12px;
  }
  .export-btn {
    min-height: 44px;
    margin-top: 2px;
  }
  .filters {
    padding: 12px 0;
  }
  .chip .x {
    min-width: 28px;
    min-height: 28px;
  }
}

@media (prefers-reduced-motion:reduce) {
  .chev, .event-chevron {
    transition: none;
  }
}

:deep(.audit-mask) {
  color: rgb(var(--c-warn));
}
</style>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AuditFilterDropdown, { type AuditDdOption } from '@/components/project/AuditFilterDropdown.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Pagination from '@/components/ui/Pagination.vue'
import { api, isPaginated, type PaginatedResponse } from '@/lib/api/api'
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
    const pageData: PaginatedResponse<ProjectAuditEvent> & { stats?: ProjectAuditStats } = isPaginated(res)
      ? res
      : {
          items: res as ProjectAuditEvent[],
          total: (res as ProjectAuditEvent[]).length,
          page: nextPage,
          pageSize: RUN_FETCH_PAGE_SIZE,
          hasMore: false,
        }
    const items = pageData.items || []
    collected.push(...items)
    fullTotal = pageData.total
    if (!pageStats && pageData.stats) pageStats = pageData.stats
    hasMore = Boolean(pageData.hasMore) && items.length > 0
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
      const pageData: PaginatedResponse<ProjectAuditEvent> & { stats?: ProjectAuditStats } = isPaginated(res)
        ? res
        : {
            items: res as ProjectAuditEvent[],
            total: (res as ProjectAuditEvent[]).length,
            page: 1,
            pageSize: pageSize.value,
            hasMore: false,
          }
      events.value = pageData.items || []
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
      class="rounded-lg flex min-h-0 flex-1 flex-col items-center justify-center gap-2 border border-dashed border-line bg-surface px-6 py-16 text-center"
      data-testid="project-audit-denied"
    >
      <div class="text-sm font-semibold text-txt">{{ t('pages.projectDetail.audit.deniedTitle') }}</div>
      <p class="max-w-md text-[13px] text-txt3">{{ t('pages.projectDetail.audit.deniedDesc') }}</p>
    </div>

    <template v-else>
      <div class="filters" :class="{ 'filters-mobile': isMobile }" data-testid="project-audit-filters">
        <!-- Mobile: collapsible filter summary (plan g2); search/export stay outside fold -->
        <template v-if="isMobile">
          <button
            type="button"
            class="filter-summary"
            data-testid="project-audit-filter-summary"
            :aria-expanded="filtersExpanded ? 'true' : 'false'"
            @click="toggleFiltersExpanded"
          >
            <span class="sum-text">{{ filterSummaryText }}</span>
            <span class="sum-action">
              {{
                filtersExpanded
                  ? t('pages.projectDetail.audit.filterCollapse')
                  : t('pages.projectDetail.audit.filterExpand')
              }}
            </span>
          </button>

          <div
            v-show="filtersExpanded"
            class="filters-editor"
            data-testid="project-audit-filters-editor"
          >
            <AuditFilterDropdown
              v-if="mode === 'run'"
              :model-value="runId"
              :label-key="'Run'"
              :options="runDdOptions"
              :searchable="true"
              :block="true"
              :empty-label="t('pages.projectDetail.audit.noRun')"
              test-id="project-audit-run"
              @update:model-value="onRunChange"
            />
            <AuditFilterDropdown
              v-if="mode === 'run'"
              :model-value="nodeId"
              :label-key="t('pages.projectDetail.audit.colNode')"
              :options="nodeDdOptions"
              :searchable="true"
              :block="true"
              :empty-label="t('pages.projectDetail.audit.filterAll')"
              test-id="project-audit-node"
              @update:model-value="(v) => { nodeId = v; onFilterChange() }"
            />
            <AuditFilterDropdown
              v-if="mode === 'all'"
              :model-value="callerKind"
              :label-key="t('pages.projectDetail.audit.colCaller')"
              :options="callerDdOptions"
              :block="true"
              :empty-label="t('pages.projectDetail.audit.filterAll')"
              test-id="project-audit-caller"
              @update:model-value="(v) => { callerKind = v; onFilterChange() }"
            />
            <AuditFilterDropdown
              :model-value="resource"
              :label-key="t('pages.projectDetail.audit.colResource')"
              :options="resourceDdOptions"
              :searchable="true"
              :block="true"
              :empty-label="t('pages.projectDetail.audit.filterAll')"
              :group-by="resGroup"
              test-id="project-audit-resource"
              @update:model-value="(v) => { resource = v; onFilterChange() }"
            />
            <AuditFilterDropdown
              :model-value="timeWindow"
              :label-key="t('pages.projectDetail.audit.filterTime')"
              :options="timeDdOptions"
              :block="true"
              test-id="project-audit-time"
              @update:model-value="onTimeChange"
            />
          </div>

          <div class="search">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="7" />
              <path d="M20 20l-3-3" />
            </svg>
            <input
              v-model="search"
              type="search"
              :placeholder="t('pages.projectDetail.audit.searchPlaceholder')"
              autocomplete="off"
              data-testid="project-audit-search"
              @input="onSearchInput"
            />
          </div>
          <div class="seg" role="tablist" data-testid="project-audit-mode">
            <button
              type="button"
              :class="{ on: mode === 'run' }"
              data-testid="project-audit-mode-run"
              @click="setMode('run')"
            >
              {{ t('pages.projectDetail.audit.modeRun') }}
            </button>
            <button
              type="button"
              :class="{ on: mode === 'all' }"
              data-testid="project-audit-mode-all"
              @click="setMode('all')"
            >
              {{ t('pages.projectDetail.audit.modeAll') }}
            </button>
          </div>
          <div class="toolbar-end">
            <div class="toolbar-stats" data-testid="project-audit-stats">
              <span class="stat-chip" data-testid="project-audit-run-count">
                {{ t('pages.projectDetail.audit.statTotal') }} <b>{{ stats.total }}</b>
              </span>
              <span class="stat-chip ok">{{ t('pages.projectDetail.audit.statOk') }} <b>{{ statOk }}</b></span>
              <span class="stat-chip" :class="{ fail: stats.fail }">{{ t('pages.projectDetail.audit.statFail') }} <b>{{ stats.fail }}</b></span>
              <span class="stat-chip">MCP <b>{{ stats.mcp }}</b></span>
            </div>
            <button type="button" class="btn" data-testid="project-audit-export" @click="exportAudit">
              {{ t('pages.projectDetail.audit.export') }}
            </button>
          </div>
        </template>

        <!-- Desktop: horizontal filters + wide table (plan g4.1) -->
        <template v-else>
          <div class="search">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="7" />
              <path d="M20 20l-3-3" />
            </svg>
            <input
              v-model="search"
              type="search"
              :placeholder="t('pages.projectDetail.audit.searchPlaceholder')"
              autocomplete="off"
              data-testid="project-audit-search"
              @input="onSearchInput"
            />
          </div>

          <AuditFilterDropdown
            v-if="mode === 'run'"
            :model-value="runId"
            :label-key="'Run'"
            :options="runDdOptions"
            :searchable="true"
            :width="280"
            :empty-label="t('pages.projectDetail.audit.noRun')"
            test-id="project-audit-run"
            @update:model-value="onRunChange"
          />
          <AuditFilterDropdown
            v-if="mode === 'run'"
            :model-value="nodeId"
            :label-key="t('pages.projectDetail.audit.colNode')"
            :options="nodeDdOptions"
            :searchable="true"
            :empty-label="t('pages.projectDetail.audit.filterAll')"
            test-id="project-audit-node"
            @update:model-value="(v) => { nodeId = v; onFilterChange() }"
          />
          <AuditFilterDropdown
            v-if="mode === 'all'"
            :model-value="callerKind"
            :label-key="t('pages.projectDetail.audit.colCaller')"
            :options="callerDdOptions"
            :empty-label="t('pages.projectDetail.audit.filterAll')"
            test-id="project-audit-caller"
            @update:model-value="(v) => { callerKind = v; onFilterChange() }"
          />
          <AuditFilterDropdown
            :model-value="resource"
            :label-key="t('pages.projectDetail.audit.colResource')"
            :options="resourceDdOptions"
            :searchable="true"
            :width="280"
            :empty-label="t('pages.projectDetail.audit.filterAll')"
            :group-by="resGroup"
            test-id="project-audit-resource"
            @update:model-value="(v) => { resource = v; onFilterChange() }"
          />
          <AuditFilterDropdown
            :model-value="timeWindow"
            :label-key="t('pages.projectDetail.audit.filterTime')"
            :options="timeDdOptions"
            test-id="project-audit-time"
            @update:model-value="onTimeChange"
          />

          <div class="seg" role="tablist" data-testid="project-audit-mode">
            <button
              type="button"
              :class="{ on: mode === 'run' }"
              data-testid="project-audit-mode-run"
              @click="setMode('run')"
            >
              {{ t('pages.projectDetail.audit.modeRun') }}
            </button>
            <button
              type="button"
              :class="{ on: mode === 'all' }"
              data-testid="project-audit-mode-all"
              @click="setMode('all')"
            >
              {{ t('pages.projectDetail.audit.modeAll') }}
            </button>
          </div>
          <div class="toolbar-end">
            <div class="toolbar-stats" data-testid="project-audit-stats">
              <span class="stat-chip" data-testid="project-audit-run-count">
                {{ t('pages.projectDetail.audit.statTotal') }} <b>{{ stats.total }}</b>
              </span>
              <span class="stat-chip ok">{{ t('pages.projectDetail.audit.statOk') }} <b>{{ statOk }}</b></span>
              <span class="stat-chip" :class="{ fail: stats.fail }">{{ t('pages.projectDetail.audit.statFail') }} <b>{{ stats.fail }}</b></span>
              <span class="stat-chip">MCP <b>{{ stats.mcp }}</b></span>
            </div>
            <button type="button" class="btn" data-testid="project-audit-export" @click="exportAudit">
              {{ t('pages.projectDetail.audit.export') }}
            </button>
          </div>
        </template>
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
          @click="toggleOpen(ev.id)"
        >
          <div class="ec-top">
            <span class="ec-time mono">{{ fmtTime(ev.occurredAt) }}</span>
            <span :class="ev.outcome === 'fail' ? 'bad' : 'ok'">{{ outcomeLabel(ev) }}</span>
          </div>
          <div class="ec-sum" :class="{ open: openId === ev.id }">{{ ev.summary }}</div>
          <div class="ec-row">
            <span class="k">{{ t('pages.projectDetail.audit.colNode') }}</span>
            {{ nodeLabel(ev.nodeId) }}
            <span v-if="resourceAux(ev)" class="ec-aux">{{ resourceAux(ev) }}</span>
          </div>
          <div v-if="openId === ev.id" class="ec-detail" @click.stop>
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
          <div class="ec-hint">
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
            :data-testid="`project-audit-group-head-${g.id}`"
            @click="toggleGroup(g.id)"
          >
            <span class="chev" aria-hidden="true" />
            <span class="group-meta">
              <span class="group-name">
                <span class="node-dot" :class="g.type" />
                {{ g.title }}
                <span v-if="g.fullId" class="nid">{{ g.fullId }}</span>
              </span>
              <span class="group-sub">
                {{ t('pages.projectDetail.audit.groupEvents', { n: g.events.length }) }}
              </span>
            </span>
            <span class="group-counts">
              <span class="stat-chip ok">{{ t('pages.projectDetail.audit.statOk') }} <strong>{{ g.ok }}</strong></span>
              <span class="stat-chip" :class="{ fail: g.fail }">{{ t('pages.projectDetail.audit.statFail') }} <strong>{{ g.fail }}</strong></span>
            </span>
          </button>
          <div v-show="isGroupOpen(g)" class="group-body">
            <table>
              <thead>
                <tr>
                  <th style="width: 148px">{{ t('pages.projectDetail.audit.colTime') }}</th>
                  <th>{{ t('pages.projectDetail.audit.colSummary') }}</th>
                  <th style="width: 72px">{{ t('pages.projectDetail.audit.colOutcome') }}</th>
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
                      <div class="time-main mono">{{ fmtTime(ev.occurredAt) }}</div>
                    </td>
                    <td class="summary">
                      <div class="summary-main">{{ ev.summary }}</div>
                      <div v-if="resourceAux(ev)" class="summary-aux">{{ resourceAux(ev) }}</div>
                    </td>
                    <td>
                      <span :class="ev.outcome === 'fail' ? 'bad' : 'ok'">{{ outcomeLabel(ev) }}</span>
                    </td>
                  </tr>
                  <tr v-if="openId === ev.id" class="detail">
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
          <thead>
            <tr>
              <th style="width: 148px">{{ t('pages.projectDetail.audit.colTime') }}</th>
              <th style="width: 140px">{{ t('pages.projectDetail.audit.colNode') }}</th>
              <th>{{ t('pages.projectDetail.audit.colSummary') }}</th>
              <th style="width: 72px">{{ t('pages.projectDetail.audit.colOutcome') }}</th>
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
                  <div class="time-main mono">{{ fmtTime(ev.occurredAt) }}</div>
                </td>
                <td>
                  <span class="node">{{ nodeLabel(ev.nodeId) }}</span>
                </td>
                <td class="summary">
                  <div class="summary-main">{{ ev.summary }}</div>
                  <div v-if="resourceAux(ev)" class="summary-aux">{{ resourceAux(ev) }}</div>
                </td>
                <td>
                  <span :class="ev.outcome === 'fail' ? 'bad' : 'ok'">{{ outcomeLabel(ev) }}</span>
                </td>
              </tr>
              <tr v-if="openId === ev.id" class="detail">
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
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
}
.seg {
  display: inline-flex;
  background: rgb(var(--c-elevated));
  padding: 3px;
  gap: 2px;
  border-radius: 8px;
}
.seg button {
  border: 0;
  background: transparent;
  height: 28px;
  padding: 0 12px;
  font: inherit;
  font-size: 12px;
  color: rgb(var(--c-txt2));
  cursor: pointer;
  font-weight: 500;
  border-radius: 6px;
}
.seg button:hover {
  color: rgb(var(--c-txt));
}
.seg button.on {
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt));
  font-weight: 600;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
}
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  padding: 12px 16px;
  border-bottom: 1px solid rgb(var(--c-line));
}
.search {
  flex: 1 1 180px;
  min-width: 140px;
  max-width: 280px;
  height: 32px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 0 10px;
  border-radius: 8px;
}
.search:focus-within {
  border-color: rgb(var(--c-accent));
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.12);
}
.search svg {
  opacity: 0.4;
  flex: 0 0 auto;
  color: rgb(var(--c-txt2));
}
.search input {
  flex: 1;
  border: 0;
  outline: none;
  font: inherit;
  background: transparent;
  min-width: 0;
  height: 30px;
  font-size: 12px;
  color: rgb(var(--c-txt));
}
.search input::placeholder {
  color: rgb(var(--c-txt3));
}
.toolbar-end {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
  flex-wrap: wrap;
}
.toolbar-stats {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.btn {
  height: 32px;
  padding: 0 12px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt));
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
  border-radius: 8px;
}
.btn:hover:not(:disabled) {
  background: rgb(var(--c-elevated));
  border-color: rgb(var(--c-line-strong));
}
.btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  flex-shrink: 0;
  padding: 0 16px 12px;
  border-bottom: 1px solid rgb(var(--c-line));
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding: 0 4px 0 8px;
  background: rgb(var(--c-elevated));
  border: 1px solid rgb(var(--c-line));
  font-size: 11px;
  color: rgb(var(--c-txt));
}
.chip em {
  font-style: normal;
  color: rgb(var(--c-txt2));
  font-weight: 500;
}
.chip b {
  font-weight: 600;
}
.chip .x {
  width: 18px;
  height: 18px;
  border: 0;
  background: transparent;
  color: rgb(var(--c-txt2));
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
}
.chip .x:hover {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-overlay));
}
.linkish {
  border: 0;
  background: transparent;
  color: rgb(var(--c-txt2));
  font: inherit;
  font-size: 11px;
  cursor: pointer;
  padding: 0 4px;
}
.linkish:hover {
  color: rgb(var(--c-accent));
}
.stat-chip {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-elevated));
  padding: 2px 8px;
  font-size: 12px;
  color: rgb(var(--c-txt2));
}
.stat-chip b,
.stat-chip strong {
  color: rgb(var(--c-txt));
  font-weight: 600;
}
.stat-chip.ok b,
.stat-chip.ok strong {
  color: rgb(var(--c-ok));
}
.stat-chip.fail b,
.stat-chip.fail strong {
  color: rgb(var(--c-err));
}
.cap-hint {
  margin: 0;
  padding: 8px 16px;
  font-size: 12px;
  color: rgb(var(--c-txt2));
  border-bottom: 1px dashed rgb(var(--c-line-strong));
  background: rgb(var(--c-accent-dim));
  flex-shrink: 0;
}
.table-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.groups-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;
  background: rgb(var(--c-base));
}
.group {
  border-bottom: 1px solid rgb(var(--c-line));
}
.group-head {
  width: 100%;
  display: grid;
  grid-template-columns: 18px 1fr auto;
  gap: 10px;
  align-items: center;
  padding: 10px 14px;
  background: rgb(var(--c-elevated));
  border: none;
  text-align: left;
  color: rgb(var(--c-txt));
  cursor: pointer;
  font: inherit;
}
.group-head:hover {
  background: rgb(var(--c-overlay));
}
.chev {
  width: 0;
  height: 0;
  border-left: 5px solid rgb(var(--c-txt3));
  border-top: 4px solid transparent;
  border-bottom: 4px solid transparent;
  transition: transform 0.15s ease;
  transform-origin: 40% 50%;
}
.group.open .chev {
  transform: rotate(90deg);
  border-left-color: rgb(var(--c-accent));
}
.group-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.group-name {
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 8px;
}
.group-name .nid {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-weight: 400;
  font-size: 11px;
  color: rgb(var(--c-txt3));
}
.node-dot {
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  background: rgb(var(--c-txt3));
}
.node-dot.input,
.node-dot.output {
  background: rgb(var(--c-info));
}
.node-dot.clarify,
.node-dot.implement,
.node-dot.test_review,
.node-dot.agent {
  background: rgb(var(--c-accent));
}
.node-dot.human_gate,
.node-dot.proposal_select {
  background: rgb(var(--c-warn));
}
.node-dot.branch,
.node-dot.set_var {
  background: rgb(var(--c-txt2));
}
.node-dot.system {
  background: rgb(var(--c-ok));
}
.group-sub {
  font-size: 11.5px;
  color: rgb(var(--c-txt3));
}
.group-counts {
  display: flex;
  gap: 6px;
}
.group-body {
  background: rgb(var(--c-base));
}
.summary-main {
  color: rgb(var(--c-txt));
  font-size: 13px;
  line-height: 1.4;
}
.summary-aux,
.ec-aux {
  color: rgb(var(--c-txt3));
  font-size: 11.5px;
  margin-top: 2px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  min-width: 820px;
}
thead th {
  position: sticky;
  top: 0;
  z-index: 2;
  text-align: left;
  padding: 9px 14px;
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt2));
  font-size: 11px;
  font-weight: 600;
  border-bottom: 1px solid rgb(var(--c-line));
}
td {
  padding: 11px 14px;
  border-bottom: 1px solid rgb(var(--c-line));
  vertical-align: middle;
  font-size: 13px;
}
tr.row {
  cursor: pointer;
}
tr.row:hover td {
  background: rgb(var(--c-elevated));
}
tr.row.open td {
  background: rgb(var(--c-accent-dim));
}
tr.row.fail td:first-child {
  box-shadow: inset 2px 0 0 rgb(var(--c-err));
}
tr.detail td {
  padding: 0;
  background: rgb(var(--c-elevated));
  border-bottom: 1px solid rgb(var(--c-line));
}
.detail-inner {
  padding: 12px 14px 14px;
  margin-left: 2px;
  border-left: 2px solid rgb(var(--c-accent));
}
.detail-meta {
  font-size: 12px;
  color: rgb(var(--c-txt2));
  margin-bottom: 8px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.detail-meta code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  color: rgb(var(--c-txt));
  background: rgb(var(--c-surface));
  border: 1px solid rgb(var(--c-line));
  padding: 1px 5px;
}
.payload {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  font: 11px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-word;
  color: rgb(var(--c-txt2));
  overflow-x: auto;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: rgb(var(--c-txt2));
}
.time-main {
  font-variant-numeric: tabular-nums;
  color: rgb(var(--c-txt));
  font-size: 12px;
}
.who {
  color: rgb(var(--c-txt));
  font-weight: 500;
}
.act {
  font-size: 12px;
  font-weight: 500;
}
.act.mcp {
  color: #7c3aed;
}
.act.run {
  color: #52525b;
}
.act.gate {
  color: #b45309;
}
.act.cfg {
  color: #2563eb;
}
.act.exp {
  color: #db2777;
}
.res {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: #71717a;
}
.node {
  color: #52525b;
  font-size: 12px;
}
.ok {
  color: rgb(var(--c-ok));
  font-size: 12px;
  font-weight: 500;
}
.bad {
  color: rgb(var(--c-err));
  font-size: 12px;
  font-weight: 500;
}
.summary {
  color: rgb(var(--c-txt));
}
.list-placeholder {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}
.empty {
  padding: 48px 16px;
  text-align: center;
  color: rgb(var(--c-txt2));
}
.empty .big {
  font-size: 14px;
  font-weight: 600;
  color: rgb(var(--c-txt));
  margin-bottom: 4px;
}
.col-run-hide :deep(.col-run),
.col-node-hide :deep(.col-node) {
  display: none;
}
.col-run-hide .col-run,
.col-node-hide .col-node {
  display: none;
}
.filter-summary {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 44px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-elevated));
  padding: 10px 12px;
  font: inherit;
  text-align: left;
  cursor: pointer;
  user-select: none;
}
.filter-summary .sum-text {
  flex: 1;
  min-width: 0;
  font-size: 12px;
  color: rgb(var(--c-txt));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.filter-summary .sum-action {
  flex: 0 0 auto;
  font-size: 11px;
  font-weight: 600;
  color: rgb(var(--c-accent));
  white-space: nowrap;
}
.filters-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
  /* 与搜索同宽（f1/g2.3）：仅纵向内边距，避免左右 inset 缩窄触发器 */
  padding: 10px 0;
  border-top: 1px solid rgb(var(--c-line));
  border-bottom: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-base));
}
.filters-mobile {
  flex-direction: column;
  align-items: stretch;
}
.filters-mobile .search {
  max-width: none;
  width: 100%;
  flex: 0 0 auto;
  height: 44px;
  min-height: 44px;
}
.filters-mobile .search input {
  height: 42px;
}
.filters-mobile .toolbar-end {
  margin-left: 0;
  width: 100%;
  flex: 0 0 auto;
}
.filters-mobile .toolbar-end .btn {
  flex: 1;
  width: 100%;
  min-height: 44px;
  height: auto;
}
.filters-mobile .toolbar-stats {
  width: 100%;
}
.filters-mobile .filter-summary {
  flex: 0 0 auto;
}
.event-cards {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 12px 16px;
}
.event-card {
  display: block;
  width: 100%;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 10px;
  text-align: left;
  font: inherit;
  color: inherit;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
  border-radius: 12px;
}
.event-card:hover {
  border-color: rgb(var(--c-accent));
}
.event-card.open {
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent-dim));
}
.event-card.fail {
  box-shadow: inset 2px 0 0 rgb(var(--c-err));
}
.ec-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 4px;
}
.ec-time {
  font-size: 11px;
  color: #888;
  font-variant-numeric: tabular-nums;
}
.ec-row {
  font-size: 12px;
  color: #333;
  margin-top: 2px;
}
.ec-row .k,
.ec-detail .k {
  color: #888;
}
.ec-sum {
  margin-top: 6px;
  font-size: 12px;
  color: #444;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.ec-sum.open {
  display: block;
  -webkit-line-clamp: unset;
}
.ec-detail {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed #e5e5e5;
  font-size: 12px;
  color: #444;
}
.ec-detail div {
  margin-top: 3px;
}
.ec-detail code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  color: rgb(var(--c-txt));
  background: #fff;
  border: 1px solid rgb(var(--c-line));
  padding: 1px 5px;
}
.ec-detail .payload {
  margin-top: 8px;
}
.ec-hint {
  margin-top: 6px;
  font-size: 10px;
  color: rgb(var(--c-accent));
}
@media (max-width: 767px) {
  .filters:not(.filters-mobile) {
    flex-direction: column;
    align-items: stretch;
  }
  .filters:not(.filters-mobile) .search {
    max-width: none;
    width: 100%;
  }
  .filters:not(.filters-mobile) .toolbar-end {
    margin-left: 0;
    width: 100%;
  }
  .filters:not(.filters-mobile) .toolbar-end .btn {
    flex: 1;
    width: 100%;
  }
}
:deep(.audit-mask) {
  color: #b45309;
}
</style>

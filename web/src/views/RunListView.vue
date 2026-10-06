<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import StatusPill from '@/components/ui/StatusPill.vue'
import PriorityBadge from '@/components/ui/PriorityBadge.vue'
import StatusFilter from '@/components/ui/StatusFilter.vue'
import WorkflowFilter from '@/components/ui/WorkflowFilter.vue'
import ProjectFilter from '@/components/ui/ProjectFilter.vue'
import TagFilter from '@/components/ui/TagFilter.vue'
import Pagination from '@/components/ui/Pagination.vue'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { useWorkflowFilter } from '@/lib/composables/useWorkflowFilter'
import { useTagFilter } from '@/lib/composables/useTagFilter'
import { useProjectContext } from '@/lib/composables/useProjectContext'
import {
  useStatusFilter,
  parseStatusQuery,
  serializeStatusQuery,
  normalizeStatuses,
  initStatusFilterFromStorage,
} from '@/lib/composables/useStatusFilter'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { fmtTime, fmtDuration, truncateText, formatTrigger } from '@/lib/shared/format'
import { displayRunTitle } from '@/lib/run/runTitle'
import type { Run } from '@/lib/shared/types'

const PAGE_SIZE = 20
const SKELETON_ROWS = 6

/** Allowed run-list sort fields (must match API whitelist). */
type RunSortKey = 'started_at' | 'priority'
type RunSortOrder = 'asc' | 'desc'

const ALLOWED_SORT: Record<RunSortKey, true> = { started_at: true, priority: true }
/** First click on a column uses desc for both started_at and priority. */
const DEFAULT_ORDER: Record<RunSortKey, RunSortOrder> = {
  started_at: 'desc',
  priority: 'desc',
}

/** Persists across route remounts within the same session. */
let hasInitialLoaded = false

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()
const runs = ref<Run[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const initialLoading = ref(false)
const initialLoadFailed = ref(false)
const showTableLoading = computed(() => loading.value && hasInitialLoaded)
let requestSeq = 0
let activeLoadingSeq = 0
const { selected: selectedWf } = useWorkflowFilter()
const { selected: selectedProject, ensureHydrated: hydrateProject } = useProjectContext()
const { selectedStatuses } = useStatusFilter()
const statusFilterOpen = ref(false)
const workflowFilterOpen = ref(false)
const projectFilterOpen = ref(false)
const tagFilterOpen = ref(false)
const { selectedTags } = useTagFilter()

/** Whole-list fade key when filters/page/sort change (g3.2; no per-row stagger). */
const listFadeKey = computed(() =>
  [
    page.value,
    selectedWf.value || '',
    selectedProject.value || '',
    selectedStatuses.value.slice().sort().join(','),
    selectedTags.value.slice().sort().join(','),
    String(route.query.sort || ''),
    String(route.query.order || ''),
  ].join('|'),
)

function parseRunSort(
  sortRaw: unknown,
  orderRaw: unknown,
): { sort: RunSortKey; order: RunSortOrder } | null {
  const sort = typeof sortRaw === 'string' ? sortRaw.trim() : ''
  const order = typeof orderRaw === 'string' ? orderRaw.trim().toLowerCase() : ''
  if (!(sort in ALLOWED_SORT)) return null
  if (order !== 'asc' && order !== 'desc') return null
  return { sort: sort as RunSortKey, order }
}

/** Active user sort from URL; null means default hybrid-time order (headers inactive). */
const activeSort = computed(() => parseRunSort(route.query.sort, route.query.order))

function ariaSortFor(key: RunSortKey): 'none' | 'ascending' | 'descending' {
  const cur = activeSort.value
  if (!cur || cur.sort !== key) return 'none'
  return cur.order === 'asc' ? 'ascending' : 'descending'
}

function sortThClass(key: RunSortKey): string[] {
  const cur = activeSort.value
  const classes = ['sortable']
  if (cur?.sort === key) {
    classes.push('active', cur.order)
  }
  return classes
}

async function applySortClick(key: RunSortKey) {
  const cur = activeSort.value
  const nextSort: RunSortKey = key
  let nextOrder: RunSortOrder = DEFAULT_ORDER[key]
  if (cur?.sort === key) {
    nextOrder = cur.order === 'desc' ? 'asc' : 'desc'
  }
  const query = { ...route.query, sort: nextSort, order: nextOrder }
  await router.replace({ query })
}

const cancelTarget = ref<Run | null>(null)
const cancellingRun = ref(false)
const cancelRunError = ref('')
const deleteTarget = ref<Run | null>(null)
const deletingRun = ref(false)
const deleteRunError = ref('')

function canCancelRun(r: Run) {
  return r.status === 'queued' || r.status === 'running' || r.status === 'waiting_human'
}

function canDeleteRun(r: Run) {
  return r.status === 'completed' || r.status === 'failed' || r.status === 'cancelled'
}

/** Prefetch RunDetail chunk once so list → detail navigation feels snappy. */
let runDetailPrefetch: Promise<unknown> | null = null
function prefetchRunDetail() {
  if (!runDetailPrefetch) {
    runDetailPrefetch = import('@/views/RunDetailView.vue')
  }
}

function runHref(id: string) {
  return '/runs/' + id
}

function runTitle(r: Run): string {
  return displayRunTitle(r.title)
}

/** Display fields compared before poll/list assignment to skip no-op re-renders. */
function runListFingerprint(r: Run): string {
  return [
    r.id,
    r.status,
    r.title ?? '',
    r.priority ?? '',
    r.progress,
    r.durationSec,
    r.currentNodeLabel ?? '',
    r.workflowName,
    r.workflowVersion ?? '',
    r.trigger,
    r.startedAt,
    r.createdAt ?? '',
    (r.tags || []).join(','),
  ].join('\0')
}

function runsListUnchanged(next: Run[], nextTotal: number): boolean {
  if (nextTotal !== total.value) return false
  const prev = runs.value
  if (prev.length !== next.length) return false
  for (let i = 0; i < next.length; i++) {
    if (runListFingerprint(prev[i]) !== runListFingerprint(next[i])) return false
  }
  return true
}

function openCancelConfirm(r: Run) {
  if (!canCancelRun(r) || cancellingRun.value || deletingRun.value) return
  cancelRunError.value = ''
  cancelTarget.value = r
}

function closeCancelConfirm() {
  if (cancellingRun.value) return
  cancelTarget.value = null
  cancelRunError.value = ''
}

function mapCancelRunError(e: unknown): string {
  const status = (e as { status?: number })?.status
  const msg = e instanceof Error ? e.message : String(e || '')
  if (status === 404 || /not found/i.test(msg)) return t('pages.runDetail.cancelErrorNotFound')
  if (status === 400 || /already finished|cannot cancel/i.test(msg)) {
    return t('pages.runDetail.cancelErrorNotCancellable')
  }
  return msg || t('pages.runDetail.cancelErrorGeneric')
}

async function confirmCancelRun() {
  const target = cancelTarget.value
  if (!target || !canCancelRun(target) || cancellingRun.value) return
  cancellingRun.value = true
  cancelRunError.value = ''
  try {
    await api.cancelRun(target.id)
    cancelTarget.value = null
    toast.success(t('pages.runDetail.cancelSuccess'))
    await load()
  } catch (e) {
    cancelRunError.value = mapCancelRunError(e)
  } finally {
    cancellingRun.value = false
  }
}

function openDeleteConfirm(r: Run) {
  if (!canDeleteRun(r) || deletingRun.value || cancellingRun.value) return
  deleteRunError.value = ''
  deleteTarget.value = r
}

function closeDeleteConfirm() {
  if (deletingRun.value) return
  deleteTarget.value = null
  deleteRunError.value = ''
}

function mapDeleteRunError(e: unknown): string {
  const status = (e as { status?: number })?.status
  const msg = e instanceof Error ? e.message : String(e || '')
  if (status === 404 || /not found/i.test(msg)) return t('pages.runDetail.deleteErrorNotFound')
  if (status === 409 || /cannot delete run/i.test(msg)) return t('pages.runDetail.deleteErrorNotDeletable')
  return msg || t('pages.runDetail.deleteErrorGeneric')
}

async function confirmDeleteRun() {
  const target = deleteTarget.value
  if (!target || !canDeleteRun(target) || deletingRun.value) return
  deletingRun.value = true
  deleteRunError.value = ''
  try {
    await api.deleteRun(target.id)
    deleteTarget.value = null
    toast.success(t('pages.runList.deleteSuccess'))
    await load()
  } catch (e) {
    deleteRunError.value = mapDeleteRunError(e)
  } finally {
    deletingRun.value = false
  }
}

watch(statusFilterOpen, (v) => {
  if (v) {
    workflowFilterOpen.value = false
    projectFilterOpen.value = false
    tagFilterOpen.value = false
  }
})
watch(workflowFilterOpen, (v) => {
  if (v) {
    statusFilterOpen.value = false
    projectFilterOpen.value = false
    tagFilterOpen.value = false
  }
})
watch(projectFilterOpen, (v) => {
  if (v) {
    statusFilterOpen.value = false
    workflowFilterOpen.value = false
    tagFilterOpen.value = false
  }
})
watch(tagFilterOpen, (v) => {
  if (v) {
    statusFilterOpen.value = false
    workflowFilterOpen.value = false
    projectFilterOpen.value = false
  }
})

const hasFilter = computed(() => {
  const statuses = parseStatusQuery(typeof route.query.status === 'string' ? route.query.status : '')
  return !!(statuses.length || route.query.wf || route.query.projectId || selectedTags.value.length)
})

const emptyMessage = computed(() => {
  if (runs.value.length) return ''
  return hasFilter.value ? t('common.empty.noMatchingRuns') : t('common.empty.noRuns')
})

function runIdShort(id: string) {
  return id.replace('run-', '')
}

function showNodeLabel(r: Run) {
  return (r.status === 'running' || r.status === 'waiting_human') && !!r.currentNodeLabel
}

function listParams() {
  const status = typeof route.query.status === 'string' ? route.query.status : undefined
  const tag = typeof route.query.tag === 'string' ? route.query.tag : undefined
  const wf = typeof route.query.wf === 'string' ? route.query.wf : undefined
  const projectId = typeof route.query.projectId === 'string' ? route.query.projectId : undefined
  const sort = activeSort.value
  return {
    status,
    tag,
    wf,
    projectId,
    page: page.value,
    pageSize: PAGE_SIZE,
    ...(sort ? { sort: sort.sort, order: sort.order } : {}),
  }
}

async function load({ showLoading = false }: { showLoading?: boolean } = {}) {
  const localSeq = ++requestSeq
  const isFirstLoad = !hasInitialLoaded

  if (isFirstLoad) {
    initialLoading.value = true
    initialLoadFailed.value = false
  } else if (showLoading) {
    activeLoadingSeq = localSeq
    loading.value = true
  }

  try {
    const data = await api.listRuns(listParams())
    if (localSeq === requestSeq) {
      if (!runsListUnchanged(data.items, data.total)) {
        runs.value = data.items
        total.value = data.total
      }
      if (initialLoadFailed.value) initialLoadFailed.value = false
    }
  } catch {
    if (isFirstLoad) {
      initialLoadFailed.value = true
    }
    /* non-first failure: keep previous list silently */
  } finally {
    if (isFirstLoad) {
      hasInitialLoaded = true
      initialLoading.value = false
    } else if (showLoading && activeLoadingSeq === localSeq) {
      loading.value = false
    }
  }
}

async function validateQueryParams(): Promise<boolean> {
  let changed = false
  const query = { ...route.query }

  const rawStatus = typeof route.query.status === 'string' ? route.query.status : ''
  if (rawStatus) {
    const valid = parseStatusQuery(rawStatus)
    const normalized = normalizeStatuses(valid)
    if (!normalized.length) {
      delete query.status
      changed = true
    } else {
      const serialized = serializeStatusQuery(normalized)
      if (serialized !== rawStatus) {
        query.status = serialized
        changed = true
      }
    }
  }

  const wf = typeof route.query.wf === 'string' ? route.query.wf : ''
  if (wf) {
    try {
      const workflows = await api.listWorkflows()
      if (!workflows.some((w) => w.id === wf)) {
        delete query.wf
        changed = true
      }
    } catch {
      /* if workflows fail to load, keep wf and let backend return empty/filtered */
    }
  }

  // Strip unpaired / unknown sort+order; keep other filters.
  const hasSortKey = route.query.sort != null && route.query.sort !== ''
  const hasOrderKey = route.query.order != null && route.query.order !== ''
  if (hasSortKey || hasOrderKey) {
    const parsed = parseRunSort(route.query.sort, route.query.order)
    if (!parsed) {
      delete query.sort
      delete query.order
      changed = true
    } else {
      if (route.query.sort !== parsed.sort) {
        query.sort = parsed.sort
        changed = true
      }
      if (route.query.order !== parsed.order) {
        query.order = parsed.order
        changed = true
      }
    }
  }

  if (changed) {
    await router.replace({ query })
  }
  return changed
}

watch(() => ({ ...route.query }), () => {
  page.value = 1
  load({ showLoading: true })
})

watch(page, () => {
  load({ showLoading: true })
})

// Poll so statuses stay current; re-sync when the tab regains focus.
let timer: number | undefined
function onVisible() {
  if (document.visibilityState === 'visible') load()
}

function onFocus() {
  load()
}

onMounted(async () => {
  hydrateProject()
  const urlChanged = await validateQueryParams()
  const restored = await initStatusFilterFromStorage(route, router)
  if (!urlChanged && !restored) load({ showLoading: true })
  timer = window.setInterval(load, 3000)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('focus', onFocus)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('focus', onFocus)
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="mb-5 flex shrink-0 flex-col gap-2.5 md:flex-row md:items-start md:justify-between">
      <div class="min-w-0">
        <h2 class="text-lg font-semibold text-txt">{{ t('pages.runList.title') }}</h2>
        <p class="text-sm text-txt3">{{ t('pages.runList.subtitle') }}</p>
      </div>
      <div class="flex w-full flex-col gap-2 md:w-auto md:flex-row md:items-center">
        <TagFilter
          v-model="selectedTags"
          v-model:open="tagFilterOpen"
          :project-id="selectedProject"
        />
        <ProjectFilter
          v-model="selectedProject"
          v-model:open="projectFilterOpen"
          :count="total"
        />
        <StatusFilter
          v-model="selectedStatuses"
          v-model:open="statusFilterOpen"
          :count="total"
        />
        <WorkflowFilter
          v-model="selectedWf"
          v-model:open="workflowFilterOpen"
          :count="total"
        />
      </div>
    </div>

    <!-- Mobile card list -->
    <div
      v-if="isMobile"
      class="flex min-h-0 flex-1 flex-col overflow-hidden"
      :class="{ 'table-loading': showTableLoading }"
    >
      <template v-if="initialLoading">
        <div class="card flex min-h-0 flex-1 flex-col overflow-hidden p-2">
          <div class="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
            <div
              v-for="n in SKELETON_ROWS"
              :key="'skel-card-' + n"
              class="rounded-lg border border-line bg-surface p-3"
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0 flex-1">
                  <div class="h-3.5 w-[75%] rounded bg-elevated animate-pulse" />
                  <div class="mt-1.5 h-2.5 w-20 rounded bg-elevated animate-pulse" />
                </div>
                <div class="h-5 w-14 shrink-0 rounded bg-elevated animate-pulse" />
              </div>
              <div class="mt-2 flex items-center gap-2">
                <div class="h-1.5 w-20 shrink-0 rounded-full bg-elevated animate-pulse" />
                <div class="h-2.5 w-7 rounded bg-elevated animate-pulse" />
              </div>
              <div class="mt-2.5 h-2.5 w-[55%] rounded bg-elevated animate-pulse" />
            </div>
          </div>
        </div>
      </template>
      <div
        v-else-if="initialLoadFailed"
        class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto px-5 py-10 text-center"
      >
        <div class="text-[13px] font-medium text-txt">{{ t('pages.runList.loadFailedTitle') }}</div>
        <p class="mx-auto mt-1 max-w-[360px] text-xs text-txt3">{{ t('pages.runList.loadFailedDesc') }}</p>
      </div>
      <div
        v-else-if="!runs.length"
        class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto px-5 py-10 text-center text-[13px] text-txt3"
      >
        {{ emptyMessage }}
      </div>
      <div v-else class="card flex min-h-0 flex-1 flex-col overflow-hidden">
        <Transition name="ui-fade" mode="out-in">
        <div :key="listFadeKey" class="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-2" data-testid="run-list-fade">
        <!--
          custom + navigate (not a real <a>): ops @click.stop must not sit inside
          a native link, or stopPropagation blocks Vue Router's preventDefault and
          the browser follows href (cancel/delete → whole-page jump to /runs/:id).
        -->
        <RouterLink
          v-for="r in runs"
          :key="r.id"
          :to="runHref(r.id)"
          custom
          v-slot="{ navigate, href }"
        >
          <div
            role="link"
            :data-href="href"
            tabindex="0"
            class="list-card-lift flex w-full cursor-pointer flex-col gap-2 rounded-lg border border-line bg-surface p-3 text-left no-underline hover:border-line-strong hover:bg-elevated"
            @click="navigate"
            @keydown.enter.prevent="() => navigate()"
            @mouseenter="prefetchRunDetail"
            @touchstart.passive="prefetchRunDetail"
            @pointerdown="prefetchRunDetail"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0 flex-1">
                <div
                  v-if="runTitle(r)"
                  class="truncate text-sm font-semibold text-txt"
                  :title="runTitle(r).length > 60 ? runTitle(r) : undefined"
                >{{ truncateText(runTitle(r), 60) }}</div>
                <div
                  class="font-mono text-xs text-txt3"
                  :class="runTitle(r) ? 'mt-0.5' : 'text-[13px] font-medium'"
                >#{{ runIdShort(r.id) }}</div>
              </div>
              <StatusPill :status="r.status" size="sm" />
            </div>
            <div class="flex flex-wrap items-center gap-1.5">
              <PriorityBadge :priority="r.priority" />
              <span v-for="tag in r.tags || []" :key="tag" class="chip text-txt2">{{ tag }}</span>
            </div>
            <div class="flex min-w-0 flex-col gap-1">
              <div class="flex items-center gap-2">
                <div class="h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-elevated">
                  <div class="h-full rounded-full bg-accent" :style="{ width: r.progress * 100 + '%' }" />
                </div>
                <span class="text-[11px] tabular-nums text-txt3">{{ Math.round(r.progress * 100) }}%</span>
              </div>
              <div
                v-if="showNodeLabel(r)"
                :key="`${r.id}-${r.currentNodeLabel}`"
                class="node-label-fade max-w-full truncate text-[11px]"
                :class="r.status === 'waiting_human' ? 'text-warn' : 'text-txt3'"
                :title="r.currentNodeLabel!.length > 60 ? r.currentNodeLabel : undefined"
              >{{ truncateText(r.currentNodeLabel!, 60) }}</div>
            </div>
            <div class="flex min-w-0 items-center justify-between gap-2">
              <div class="flex min-w-0 items-center gap-1.5 text-[12px] text-txt2">
                <span class="truncate">{{ r.workflowName }}</span>
                <span v-if="r.workflowVersion" class="chip shrink-0">v{{ r.workflowVersion }}</span>
              </div>
              <div class="shrink-0" data-testid="run-ops" @click.stop @keydown.stop>
                <AppButton
                  v-if="canCancelRun(r)"
                  data-testid="cancel-run-btn"
                  variant="danger"
                  size="sm"
                  :disabled="cancellingRun && cancelTarget?.id === r.id"
                  @click="openCancelConfirm(r)"
                >{{ t('common.buttons.cancel') }}</AppButton>
                <AppButton
                  v-else-if="canDeleteRun(r)"
                  data-testid="delete-run-btn"
                  variant="danger"
                  size="sm"
                  :disabled="deletingRun && deleteTarget?.id === r.id"
                  @click="openDeleteConfirm(r)"
                >{{ t('common.buttons.delete') }}</AppButton>
                <span
                  v-else
                  data-testid="run-ops-placeholder"
                  class="select-none px-1 text-sm text-txt3/50"
                  aria-hidden="true"
                >—</span>
              </div>
            </div>
          </div>
        </RouterLink>
        </div>
        </Transition>
        <Pagination v-if="total > PAGE_SIZE" v-model:page="page" class="shrink-0" :page-size="PAGE_SIZE" :total="total" />
      </div>
    </div>

    <!-- Desktop table -->
    <div
      v-else-if="initialLoadFailed"
      class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto px-5 py-10 text-center"
      :class="{ 'table-loading': showTableLoading }"
    >
      <div class="text-[13px] font-medium text-txt">{{ t('pages.runList.loadFailedTitle') }}</div>
      <p class="mx-auto mt-1 max-w-[360px] text-xs text-txt3">{{ t('pages.runList.loadFailedDesc') }}</p>
    </div>

    <!-- Desktop: empty / no matching -->
    <div
      v-else-if="!runs.length && !initialLoading"
      class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto px-5 py-10 text-center text-[13px] text-txt3"
      :class="{ 'table-loading': showTableLoading }"
    >
      {{ emptyMessage }}
    </div>

    <!-- Desktop table (loading skeleton or rows) -->
    <div
      v-else
      class="card flex min-h-0 flex-1 flex-col overflow-hidden"
      :class="{ 'table-loading': showTableLoading }"
    >
      <div class="min-h-0 flex-1 overflow-auto">
      <Transition name="ui-fade" mode="out-in">
      <div :key="listFadeKey" data-testid="run-list-fade-desktop">
      <table class="w-full text-sm">
        <thead>
          <tr class="text-left text-[11px] uppercase tracking-wider text-txt3">
            <th class="px-5 py-2.5 font-medium">{{ t('common.table.title') }}</th>
            <th class="px-5 py-2.5 font-medium">{{ t('common.table.workflow') }}</th>
            <th class="px-5 py-2.5 font-medium">{{ t('common.table.trigger') }}</th>
            <th
              class="px-5 py-2.5 font-medium"
              :class="sortThClass('started_at')"
              role="columnheader"
              tabindex="0"
              :aria-sort="ariaSortFor('started_at')"
              @click="applySortClick('started_at')"
              @keydown.enter.prevent="applySortClick('started_at')"
              @keydown.space.prevent="applySortClick('started_at')"
            >
              <span class="th-inner">
                {{ t('common.table.startTime') }}
                <span class="sort-icon" aria-hidden="true">
                  <svg class="up" viewBox="0 0 10 10"><path fill="currentColor" d="M5 2 L9 7 H1 Z" /></svg>
                  <svg class="down" viewBox="0 0 10 10"><path fill="currentColor" d="M5 8 L1 3 H9 Z" /></svg>
                </span>
              </span>
            </th>
            <th class="w-0 min-w-[calc(8ch+2.5rem)] whitespace-nowrap px-5 py-2.5 font-medium">
              <span class="inline-block min-w-[8ch]">{{ t('common.table.duration') }}</span>
            </th>
            <th class="px-5 py-2.5 font-medium">{{ t('common.table.progress') }}</th>
            <th class="px-5 py-2.5 font-medium">{{ t('common.table.status') }}</th>
            <th
              class="px-5 py-2.5 font-medium"
              :class="sortThClass('priority')"
              role="columnheader"
              tabindex="0"
              :aria-sort="ariaSortFor('priority')"
              @click="applySortClick('priority')"
              @keydown.enter.prevent="applySortClick('priority')"
              @keydown.space.prevent="applySortClick('priority')"
            >
              <span class="th-inner">
                {{ t('common.table.priority') }}
                <span class="sort-icon" aria-hidden="true">
                  <svg class="up" viewBox="0 0 10 10"><path fill="currentColor" d="M5 2 L9 7 H1 Z" /></svg>
                  <svg class="down" viewBox="0 0 10 10"><path fill="currentColor" d="M5 8 L1 3 H9 Z" /></svg>
                </span>
              </span>
            </th>
            <th class="px-5 py-2.5 font-medium">{{ t('pages.runList.tagsLabel') }}</th>
            <th class="px-5 py-2.5 text-right font-medium">{{ t('common.table.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-if="initialLoading">
            <tr v-for="n in SKELETON_ROWS" :key="'skel-' + n" class="border-t border-line">
              <td class="px-5 py-3">
                <div class="h-3.5 w-[90%] rounded bg-elevated animate-pulse" />
                <div class="mt-1 h-2.5 w-[60%] rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="h-3 w-[70%] rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="h-3 w-[50%] rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="h-3 w-[72px] rounded bg-elevated animate-pulse" />
              </td>
              <td class="w-0 min-w-[calc(8ch+2.5rem)] whitespace-nowrap px-5 py-3">
                <div class="h-3 w-[8ch] rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="h-3 w-[80%] rounded bg-elevated animate-pulse" />
                <div class="mt-1 h-2.5 w-12 rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="h-3 w-14 rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="h-3 w-12 rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="h-3 w-[70%] rounded bg-elevated animate-pulse" />
              </td>
              <td class="px-5 py-3">
                <div class="ml-auto h-3 w-12 rounded bg-elevated animate-pulse" />
              </td>
            </tr>
          </template>
          <template v-else>
            <RouterLink
              v-for="r in runs"
              :key="r.id"
              :to="runHref(r.id)"
              custom
              v-slot="{ navigate, href }"
            >
              <tr
                role="link"
                :data-href="href"
                tabindex="0"
                class="cursor-pointer border-t border-line transition hover:bg-elevated active:bg-overlay"
                @click="navigate"
                @keydown.enter.prevent="() => navigate()"
                @mouseenter="prefetchRunDetail"
                @touchstart.passive="prefetchRunDetail"
                @pointerdown="prefetchRunDetail"
              >
                <td class="max-w-[340px] px-5 py-3">
                  <template v-if="runTitle(r)">
                    <div
                      class="max-w-[320px] truncate font-semibold text-txt"
                      :title="runTitle(r).length > 60 ? runTitle(r) : undefined"
                    >{{ truncateText(runTitle(r), 60) }}</div>
                    <div class="mt-0.5 font-mono text-xs text-txt3">#{{ runIdShort(r.id) }}</div>
                  </template>
                  <span v-else class="font-mono text-[13px] font-medium text-txt3">#{{ runIdShort(r.id) }}</span>
                </td>
                <td class="px-5 py-3 text-txt2">
                  {{ r.workflowName }}
                  <span v-if="r.workflowVersion" class="chip ml-1.5">v{{ r.workflowVersion }}</span>
                </td>
                <td class="px-5 py-3 text-txt3">{{ formatTrigger(r.trigger) }}</td>
                <td class="px-5 py-3 text-txt3">
                  <template v-if="r.status === 'queued'">
                    {{ fmtTime(r.createdAt ?? '') }}
                    <span class="ml-1 text-[10px] text-[#7B61FF]">{{ t('pages.runList.queued') }}</span>
                  </template>
                  <template v-else>{{ fmtTime(r.startedAt) }}</template>
                </td>
                <td class="w-0 min-w-[calc(8ch+2.5rem)] whitespace-nowrap px-5 py-3 tabular-nums text-txt3">
                  <span class="inline-block min-w-[8ch]">{{ fmtDuration(r.durationSec) }}</span>
                </td>
                <td class="px-5 py-3">
                  <div class="min-w-[148px] max-w-[168px]">
                    <div class="flex items-center gap-2">
                      <div class="h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-elevated">
                        <div class="h-full rounded-full bg-accent" :style="{ width: r.progress * 100 + '%' }" />
                      </div>
                      <span class="text-[11px] text-txt3">{{ Math.round(r.progress * 100) }}%</span>
                    </div>
                    <div
                      v-if="showNodeLabel(r)"
                      :key="`${r.id}-${r.currentNodeLabel}`"
                      class="node-label-fade mt-1 max-w-[148px] truncate text-[11px]"
                      :class="r.status === 'waiting_human' ? 'text-warn' : 'text-txt3'"
                      :title="r.currentNodeLabel!.length > 60 ? r.currentNodeLabel : undefined"
                    >{{ truncateText(r.currentNodeLabel!, 60) }}</div>
                  </div>
                </td>
                <td class="px-5 py-3"><StatusPill :status="r.status" size="sm" /></td>
                <td class="px-5 py-3"><PriorityBadge :priority="r.priority" /></td>
                <td class="px-5 py-3">
                  <div class="flex flex-wrap gap-1.5">
                    <span v-for="tag in r.tags || []" :key="tag" class="chip text-txt2">{{ tag }}</span>
                  </div>
                </td>
                <td class="px-5 py-3 text-right" data-testid="run-ops" @click.stop>
                  <AppButton
                    v-if="canCancelRun(r)"
                    data-testid="cancel-run-btn"
                    variant="danger"
                    size="sm"
                    :disabled="cancellingRun && cancelTarget?.id === r.id"
                    @click="openCancelConfirm(r)"
                  >{{ t('common.buttons.cancel') }}</AppButton>
                  <AppButton
                    v-else-if="canDeleteRun(r)"
                    data-testid="delete-run-btn"
                    variant="danger"
                    size="sm"
                    :disabled="deletingRun && deleteTarget?.id === r.id"
                    @click="openDeleteConfirm(r)"
                  >{{ t('common.buttons.delete') }}</AppButton>
                  <span
                    v-else
                    data-testid="run-ops-placeholder"
                    class="select-none px-1 text-sm text-txt3/50"
                    aria-hidden="true"
                  >—</span>
                </td>
              </tr>
            </RouterLink>
          </template>
        </tbody>
      </table>
      </div>
      </Transition>
      </div>
      <Pagination v-if="total > PAGE_SIZE" v-model:page="page" class="shrink-0" :page-size="PAGE_SIZE" :total="total" />
    </div>

    <AppModal
      :open="!!cancelTarget"
      :title="t('pages.runDetail.cancelTitle')"
      :width="440"
      @close="closeCancelConfirm"
    >
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.runDetail.cancelWarning') }}
        </div>
        <p>{{ t('pages.runList.cancelConfirm') }}</p>
        <div
          v-if="cancelRunError"
          class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        >
          <Icon name="alert" :size="14" class="mt-0.5" />{{ cancelRunError }}
        </div>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="cancellingRun" @click="closeCancelConfirm">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton
          data-testid="confirm-cancel-run-btn"
          variant="danger"
          icon="close"
          :disabled="cancellingRun"
          @click="confirmCancelRun"
        >
          {{ cancellingRun ? t('common.buttons.cancelling') : t('common.buttons.confirmCancelRun') }}
        </AppButton>
      </template>
    </AppModal>

    <AppModal
      :open="!!deleteTarget"
      :title="t('pages.runDetail.deleteTitle')"
      :width="440"
      @close="closeDeleteConfirm"
    >
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.runDetail.deleteWarning') }}
        </div>
        <p>{{ t('pages.runList.deleteConfirm') }}</p>
        <div
          v-if="deleteRunError"
          class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        >
          <Icon name="alert" :size="14" class="mt-0.5" />{{ deleteRunError }}
        </div>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="deletingRun" @click="closeDeleteConfirm">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton
          data-testid="confirm-delete-run-btn"
          variant="danger"
          icon="trash"
          :disabled="deletingRun"
          @click="confirmDeleteRun"
        >
          {{ deletingRun ? t('common.buttons.deleting') : t('common.buttons.confirmDelete') }}
        </AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.table-loading {
  opacity: 0.55;
}

thead th {
  position: sticky;
  top: 0;
  z-index: 2;
  background: rgb(var(--c-elevated));
}

@keyframes nodeLabelFadeIn {
  from {
    opacity: 0;
    transform: translateY(-2px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.node-label-fade {
  animation: nodeLabelFadeIn 0.35s ease;
}

th.sortable {
  cursor: pointer;
  user-select: none;
  color: rgb(var(--c-txt2));
}
th.sortable:hover {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
}
th.sortable:focus-visible {
  outline: 2px solid #7b61ff;
  outline-offset: -2px;
}
th .th-inner {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
th .sort-icon {
  display: inline-flex;
  flex-direction: column;
  width: 10px;
  height: 14px;
  opacity: 0.35;
  position: relative;
}
th.sortable:hover .sort-icon {
  opacity: 0.55;
}
th.active .sort-icon {
  opacity: 1;
  color: #7b61ff;
}
th .sort-icon svg {
  width: 10px;
  height: 10px;
  display: block;
}
th .sort-icon .up {
  margin-bottom: -3px;
}
th.asc .sort-icon .down {
  opacity: 0.25;
}
th.desc .sort-icon .up {
  opacity: 0.25;
}
th:not(.active) .sort-icon .up,
th:not(.active) .sort-icon .down {
  opacity: 1;
}
</style>

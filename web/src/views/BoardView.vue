<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import RunBoardColumn from '@/components/board/RunBoardColumn.vue'
import RunBoardPreviewDrawer from '@/components/board/RunBoardPreviewDrawer.vue'
import RunBoardStatusListModal from '@/components/board/RunBoardStatusListModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import { serializeStatusQuery } from '@/lib/composables/useStatusFilter'
import { runBoardTitle, runIdShort } from '@/lib/run/runBoard'
import { useRunBoard, type BoardColumnKey } from '@/lib/run/useRunBoard'
import type { Run } from '@/lib/shared/types'

defineProps<{
  /** Hide the standalone heading when hosted by the workspace tool page. */
  embedded?: boolean
}>()

const router = useRouter()
const { t } = useI18n()

const extraEnabled = reactive({
  queued: false,
  failed: false,
  cancelled: false,
})

const extraStatuses = computed(() => {
  const set = new Set<string>()
  if (extraEnabled.queued) set.add('queued')
  if (extraEnabled.failed) set.add('failed')
  if (extraEnabled.cancelled) set.add('cancelled')
  return set
})

const { load, column, loading, hasLoaded, error } = useRunBoard({
  mode: 'full',
  extraStatuses: () => extraStatuses.value,
})
const selected = ref<Run | null>(null)
const drawerOpen = ref(false)

/** Column-header status list modal (independent of useRunBoard column cache). */
const listModalOpen = ref(false)
const listModalStatus = ref<string>('')
const listModalTitle = ref('')

const segments = [
  { id: 'all', labelKey: 'pages.board.segments.all' },
  { id: 'running', labelKey: 'pages.board.segments.running' },
  { id: 'waiting_human', labelKey: 'pages.board.segments.waitingHuman' },
  { id: 'completed', labelKey: 'pages.board.segments.completed' },
] as const
type SegmentId = (typeof segments)[number]['id']
const segment = ref<SegmentId>('all')
const search = ref('')

const mainCols: { key: BoardColumnKey; accent: 'running' | 'waiting' | 'done'; titleKey: string; hintKey: string; emptyKey: string; dot: string }[] = [
  { key: 'running', accent: 'running', titleKey: 'pages.board.columns.running', hintKey: 'pages.board.hints.inProgress', emptyKey: 'pages.board.empty.running', dot: 'bg-info' },
  { key: 'waiting_human', accent: 'waiting', titleKey: 'pages.board.columns.waitingHuman', hintKey: 'pages.board.hints.inReview', emptyKey: 'pages.board.empty.waitingHuman', dot: 'bg-warn' },
  { key: 'completed', accent: 'done', titleKey: 'pages.board.columns.completed', hintKey: 'pages.board.hints.doneRecent', emptyKey: 'pages.board.empty.completed', dot: 'bg-ok' },
]

const extraCols: { key: BoardColumnKey; titleKey: string; emptyKey: string }[] = [
  { key: 'queued', titleKey: 'common.status.queued', emptyKey: 'pages.board.empty.extra' },
  { key: 'failed', titleKey: 'common.status.failed', emptyKey: 'pages.board.empty.extra' },
  { key: 'cancelled', titleKey: 'common.status.cancelled', emptyKey: 'pages.board.empty.extra' },
]

const visibleExtras = computed(() => extraCols.filter((c) => extraEnabled[c.key as keyof typeof extraEnabled]))

const showInitialLoading = computed(() => loading.value && !hasLoaded.value)
const refreshing = ref(false)

function queryText(): string {
  return search.value.trim().toLowerCase()
}

function matchesQuery(run: Run): boolean {
  const q = queryText()
  if (!q) return true
  const title = runBoardTitle(run).toLowerCase()
  const workflow = (run.workflowName || '').toLowerCase()
  const shortId = runIdShort(run.id).toLowerCase()
  return title.includes(q) || workflow.includes(q) || shortId.includes(q)
}

const filteredByKey = computed(() => {
  // Read the query up front so an all-empty board still tracks search changes.
  const q = queryText()
  const out: Partial<Record<BoardColumnKey, Run[]>> = {}
  for (const col of [...mainCols, ...extraCols]) {
    out[col.key] = column(col.key).items.filter((run) => !q || matchesQuery(run))
  }
  return out
})

const overview = computed(() => mainCols.map((col) => {
  const state = column(col.key)
  const unavailable = !hasLoaded.value || !!state.error
  return {
    key: col.key,
    label: col.key === 'completed' ? t('pages.board.overviewRecent') : t(col.titleKey),
    count: unavailable ? '—' : col.key === 'completed' ? state.items.length : state.total,
    note: col.key === 'completed'
      ? t('pages.board.overviewRecords', { n: unavailable ? '—' : state.total })
      : t(col.hintKey),
    dot: col.dot,
  }
}))

function visibleItems(key: BoardColumnKey): Run[] {
  return filteredByKey.value[key] || []
}

function emptyTextFor(key: BoardColumnKey, fallbackKey: string): string {
  const state = column(key)
  if (queryText() && state.items.length > 0 && visibleItems(key).length === 0) {
    return t('pages.board.empty.noMatch')
  }
  return t(fallbackKey)
}

function mainColumnVisible(key: string): boolean {
  return segment.value === 'all' || segment.value === key
}

async function refreshBoard() {
  if (refreshing.value) return
  refreshing.value = true
  try { await load({ intent: 'user_initiated' }) }
  finally { refreshing.value = false }
}

function truncatedHint(key: BoardColumnKey): string {
  const col = column(key)
  if (!col.truncated) return ''
  if (key === 'running' || key === 'waiting_human' || key === 'queued' || key === 'failed' || key === 'cancelled') {
    return t('pages.board.truncated.showingFirst100')
  }
  return ''
}

function closeListModal() {
  listModalOpen.value = false
}

function openPreview(run: Run) {
  // Mutual exclusion with column-header list modal (f6).
  closeListModal()
  selected.value = run
  drawerOpen.value = true
}

function closePreview() {
  drawerOpen.value = false
  selected.value = null
}

function openStatusList(status: string) {
  const col = mainCols.find((c) => c.key === status)
  if (!col) return
  // Mutual exclusion with card preview drawer (f6). Search does not filter this list.
  closePreview()
  listModalStatus.value = status
  listModalTitle.value = t(col.titleKey)
  listModalOpen.value = true
}

function onListSelect(run: Run) {
  closeListModal()
  router.push('/runs/' + run.id)
}

function goMoreCompleted() {
  router.push({
    path: '/runs',
    query: {
      status: serializeStatusQuery(['completed']),
    },
  })
}

function toggleExtra(key: 'queued' | 'failed' | 'cancelled') {
  extraEnabled[key] = !extraEnabled[key]
  load()
}

let timer: number | undefined
function onVisible() {
  if (document.visibilityState === 'visible') load()
}
function onFocus() {
  load()
}

onMounted(() => {
  load()
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
  <!-- plan g1.3: fill-height + single overflow-y-auto (standalone + embedded) -->
  <div data-testid="board-view" class="board-workbench flex h-full min-h-0 min-w-0 flex-col">
    <header class="mb-4 flex shrink-0 flex-wrap items-start justify-between gap-3" data-testid="board-toolbar">
      <div class="min-w-0">
        <h2 v-if="!embedded" class="text-lg font-semibold text-txt">{{ t('pages.board.title') }}</h2>
        <p class="text-sm text-txt2" :class="{ 'mt-1': !embedded }" data-testid="board-subtitle">{{ t('pages.board.subtitle') }}</p>
      </div>
      <nav class="flex flex-wrap items-center gap-2 text-xs" :aria-label="t('pages.board.actionsLabel')">
        <RouterLink to="/stats" class="board-text-link" data-testid="board-usage-link"><Icon name="chart" :size="14" />{{ t('pages.board.usage') }}</RouterLink>
        <span class="h-3 w-px bg-line" aria-hidden="true" />
        <RouterLink to="/runs" class="board-text-link" data-testid="board-all-runs"><Icon name="runs" :size="14" />{{ t('pages.board.allRuns') }}</RouterLink>
        <AppButton variant="ghost" size="sm" icon="refresh" :loading="refreshing" data-testid="board-refresh" class="min-h-9" @click="refreshBoard">{{ t('pages.board.refresh') }}</AppButton>
      </nav>
    </header>

    <div class="board-scroll flex min-h-0 flex-1 overflow-y-auto flex-col">
    <div
      v-if="error"
      class="mb-4 flex shrink-0 flex-wrap items-center justify-between gap-2 rounded-lg border border-err/40 bg-err/10 px-3 py-2 text-[13px] text-err"
      data-testid="board-load-error"
    >
      <span>{{ t('pages.board.loadFailed') }}</span>
      <button
        type="button"
        class="rounded-md border border-err/40 px-2.5 py-1 text-xs text-err hover:bg-err/10"
        data-testid="board-retry"
        @click="load()"
      >
        {{ t('pages.board.retry') }}
      </button>
    </div>

    <section class="board-overview shrink-0" :aria-label="t('pages.board.overview')" data-testid="board-overview">
      <div v-for="item in overview" :key="item.key" class="board-metric" :data-testid="`board-overview-${item.key}`">
        <div class="board-metric-value" data-testid="board-overview-value">{{ item.count }}</div>
        <div class="min-w-0">
          <div class="board-metric-label">
            <span class="board-metric-dot" :class="item.dot" aria-hidden="true" />
            <span>{{ item.label }}</span>
          </div>
          <p class="board-metric-note">{{ item.note }}</p>
        </div>
      </div>
    </section>

    <div class="board-filters mb-3 flex shrink-0 flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <SlidingTabs
          :value="segment"
          class="board-segments inline-flex w-fit gap-1 rounded-lg bg-elevated p-1"
          role="group"
          :aria-label="t('pages.board.segments.label')"
          indicator-class="rounded-md bg-surface shadow-sm"
          indicator-testid="board-segment-indicator"
          data-testid="board-segments"
        >
          <button
            v-for="seg in segments"
            :key="seg.id"
            type="button"
            class="board-segment border-0 bg-transparent rounded-md px-3 py-1 text-[12px] leading-5"
            :class="segment === seg.id ? 'font-semibold text-txt' : 'text-txt3 hover:text-txt'"
            :aria-pressed="segment === seg.id"
            :data-testid="`board-segment-${seg.id}`"
            @click="segment = seg.id"
          >
            {{ t(seg.labelKey) }}
          </button>
        </SlidingTabs>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <span class="mr-1 text-xs text-txt3">{{ t('pages.board.filters.label') }}</span>
          <button
            v-for="key in (['queued', 'failed', 'cancelled'] as const)"
            :key="key"
            type="button"
            class="board-chip rounded-md inline-flex items-center gap-1.5 border px-2.5 py-1 text-xs transition"
            :class="
              extraEnabled[key]
                ? 'border-line-strong bg-elevated text-txt'
                : 'border-line bg-surface text-txt2 hover:border-line-strong'
            "
            :data-testid="`board-filter-${key}`"
            @click="toggleExtra(key)"
          >
            <Icon :name="extraEnabled[key] ? 'check' : 'plus'" :size="12" class="text-txt3" />
            {{ t(`common.status.${key}`) }}
          </button>
        </div>
      </div>
      <label class="board-search" data-testid="board-search">
        <Icon name="search" :size="15" />
        <input
          v-model="search"
          type="search"
          data-testid="board-search-input"
          :aria-label="t('pages.board.searchLabel')"
          :placeholder="t('pages.board.searchPlaceholder')"
        />
      </label>
    </div>

    <div class="board-stage">
    <div class="board-columns main" :class="segment === 'all' ? 'is-all' : 'is-single'" data-testid="board-main-columns">
      <RunBoardColumn
        v-for="col in mainCols"
        v-show="mainColumnVisible(col.key)"
        :key="col.key"
        class="board-column"
        fill
        :title="t(col.titleKey)"
        :hint="t(col.hintKey)"
        :accent="col.accent"
        :items="visibleItems(col.key)"
        :listed-count="column(col.key).items.length"
        :total="column(col.key).total"
        :loading="showInitialLoading"
        :loading-text="t('pages.board.loading')"
        :empty-text="emptyTextFor(col.key, col.emptyKey)"
        :truncated-hint="truncatedHint(col.key)"
        header-activatable
        :status="col.key"
        @select="openPreview"
        @activate-header="openStatusList"
      >
        <template v-if="col.key === 'completed'" #footer>
          <button
            type="button"
            class="text-xs text-txt3 hover:text-txt"
            data-testid="board-view-more-completed"
            @click="goMoreCompleted"
          >
            {{ t('pages.board.viewMoreCompleted') }}
          </button>
        </template>
        <template v-else #footer>
          <span>{{ t('pages.board.toolbarHint') }}</span>
        </template>
      </RunBoardColumn>
    </div>

    <div
      v-if="visibleExtras.length"
      class="board-columns extra"
      data-testid="board-extra-columns"
    >
      <RunBoardColumn
        v-for="col in visibleExtras"
        :key="col.key"
        class="board-column"
        fill
        :title="t(col.titleKey)"
        :hint="t('pages.board.hints.extraFilter')"
        accent="extra"
        :items="visibleItems(col.key)"
        :listed-count="column(col.key).items.length"
        :total="column(col.key).total"
        :loading="loading && !column(col.key).items.length && !column(col.key).error"
        :loading-text="t('pages.board.loading')"
        :empty-text="emptyTextFor(col.key, col.emptyKey)"
        :truncated-hint="truncatedHint(col.key)"
        @select="openPreview"
      />
    </div>
    </div>
    </div>

    <RunBoardPreviewDrawer :open="drawerOpen" :run="selected" @close="closePreview" />

    <RunBoardStatusListModal
      :open="listModalOpen"
      :status="listModalStatus"
      :title="listModalTitle"
      @close="closeListModal"
      @select="onListSelect"
    />
  </div>
</template>

<style scoped>
.board-workbench { container: board-workbench / inline-size; }
.board-stage {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  gap: 14px;
  overflow: hidden;
}
.board-columns { display: grid; align-items: stretch; gap: 14px; min-height: 0; }
.board-columns.main { flex: 1; }
.board-columns.main.is-all { grid-template-columns: minmax(0, 0.96fr) minmax(0, 1.12fr) minmax(0, 0.96fr); }
.board-columns.main.is-single { grid-template-columns: minmax(0, 1fr); }
.board-columns.extra {
  flex: 0 1 auto;
  max-height: 42%;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.board-column { height: 100%; min-height: 0; }

.board-text-link { display: inline-flex; min-height: 36px; align-items: center; gap: 6px; padding: 0 6px; border-radius: 6px; color: rgb(var(--c-txt3)); }
.board-text-link:hover { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .035); }
.board-text-link:focus-visible,
.board-segment:focus-visible,
.board-chip:focus-visible { outline: 2px solid rgb(var(--c-accent) / .45); outline-offset: 2px; }
.board-segment { border: 0; background: transparent; }
.board-segment:hover { background: transparent; }

.board-search {
  display: flex;
  width: min(240px, 100%);
  height: 36px;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  padding: 0 10px;
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-surface));
}
.board-search:focus-within { border-color: rgb(var(--c-accent) / .5); }
.board-search input {
  width: 100%;
  min-width: 0;
  height: 100%;
  border: 0;
  padding: 0;
  background: transparent;
  color: rgb(var(--c-txt));
  font-size: 13px;
  outline: none;
  box-shadow: none;
}
.board-search input:focus,
.board-search input:focus-visible { outline: none; border: 0; box-shadow: none; }

.board-overview {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  margin: 0 0 20px;
  border-block: 1px solid rgb(var(--c-line));
}
.board-metric { display: flex; align-items: center; gap: 16px; min-width: 0; padding: 16px 20px; }
.board-metric + .board-metric { border-left: 1px solid rgb(var(--c-line)); }
.board-metric:first-child { padding-left: 2px; }
.board-metric-value {
  min-width: 44px;
  color: rgb(var(--c-txt));
  font-size: 32px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 40px;
}
.board-metric-label { display: flex; align-items: center; gap: 8px; color: rgb(var(--c-txt)); font-size: 14px; font-weight: 500; }
.board-metric-dot { width: 6px; height: 6px; flex: none; border-radius: 99px; }
.board-metric-note { margin: 4px 0 0; color: rgb(var(--c-txt3)); font-size: 12px; }

@container board-workbench (max-width: 1039px) {
  .board-columns.main.is-all { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@container board-workbench (max-width: 799px) {
  .board-stage { flex: none; overflow: visible; }
  .board-columns.main,
  .board-columns.extra { display: flex; flex: none; flex-direction: column; max-height: none; }
  .board-column { height: auto; min-height: 0; }
  .board-column :deep([data-testid="run-board-column-body"]) {
    flex: none;
    min-height: auto;
    overflow: visible;
  }
  .board-search { width: 100%; height: 40px; }
  .board-metric { gap: 8px; padding-inline: 12px; }
  .board-metric-value { font-size: 28px; }
  .board-text-link { min-height: 44px; }
}
@media (prefers-reduced-motion: reduce) {
  .board-chip { transition: none; }
}
</style>

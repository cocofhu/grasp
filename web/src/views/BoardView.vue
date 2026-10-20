<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import RunBoardColumn from '@/components/board/RunBoardColumn.vue'
import RunBoardPreviewDrawer from '@/components/board/RunBoardPreviewDrawer.vue'
import RunBoardStatusListModal from '@/components/board/RunBoardStatusListModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import { serializeStatusQuery } from '@/lib/composables/useStatusFilter'
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

const mainCols: { key: BoardColumnKey; accent: 'running' | 'waiting' | 'done'; titleKey: string; hintKey: string; emptyKey: string }[] = [
  { key: 'running', accent: 'running', titleKey: 'pages.board.columns.running', hintKey: 'pages.board.hints.inProgress', emptyKey: 'pages.board.empty.running' },
  { key: 'waiting_human', accent: 'waiting', titleKey: 'pages.board.columns.waitingHuman', hintKey: 'pages.board.hints.inReview', emptyKey: 'pages.board.empty.waitingHuman' },
  { key: 'completed', accent: 'done', titleKey: 'pages.board.columns.completed', hintKey: 'pages.board.hints.doneRecent', emptyKey: 'pages.board.empty.completed' },
]

const extraCols: { key: BoardColumnKey; titleKey: string; emptyKey: string }[] = [
  { key: 'queued', titleKey: 'common.status.queued', emptyKey: 'pages.board.empty.extra' },
  { key: 'failed', titleKey: 'common.status.failed', emptyKey: 'pages.board.empty.extra' },
  { key: 'cancelled', titleKey: 'common.status.cancelled', emptyKey: 'pages.board.empty.extra' },
]

const visibleExtras = computed(() => extraCols.filter((c) => extraEnabled[c.key as keyof typeof extraEnabled]))

const showInitialLoading = computed(() => loading.value && !hasLoaded.value)
const refreshing = ref(false)
const overview = computed(() => mainCols.map((col) => {
  const state = column(col.key)
  return {
    key: col.key,
    label: col.key === 'completed' ? t('pages.board.overviewRecent') : t(col.titleKey),
    count: !hasLoaded.value || state.error ? '—' : col.key === 'completed' ? state.items.length : state.total,
  }
}))

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
  // Mutual exclusion with card preview drawer (f6).
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
        <p class="text-[13px] leading-6 text-txt3">{{ t('pages.board.subtitle') }}</p>
      </div>
      <nav class="flex flex-wrap items-center gap-2 text-xs" :aria-label="t('pages.board.actionsLabel')">
        <RouterLink to="/stats" class="board-text-link" data-testid="board-usage-link"><Icon name="chart" :size="14" />{{ t('pages.board.usage') }}</RouterLink>
        <span class="h-3 w-px bg-line" aria-hidden="true" />
        <RouterLink to="/runs" class="board-text-link" data-testid="board-all-runs"><Icon name="runs" :size="14" />{{ t('pages.board.allRuns') }}</RouterLink>
        <AppButton variant="ghost" size="sm" icon="refresh" :loading="refreshing" data-testid="board-refresh" class="min-h-9" @click="refreshBoard">{{ t('pages.board.refresh') }}</AppButton>
      </nav>
    </header>

    <div class="min-h-0 flex-1 overflow-y-auto">
    <div
      v-if="error"
      class="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-err/40 bg-err/10 px-3 py-2 text-[13px] text-err"
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

    <section class="board-overview mb-5" :aria-label="t('pages.board.overview')" data-testid="board-overview">
      <div class="board-overview-heading"><span class="inline-block h-1.5 w-1.5 rounded-full bg-ok/70" aria-hidden="true" /><span>{{ t('pages.board.overview') }}</span></div>
      <dl class="board-overview-values">
        <div v-for="item in overview" :key="item.key" :data-testid="`board-overview-${item.key}`"><dt>{{ item.label }}</dt><dd>{{ item.count }}</dd></div>
      </dl>
    </section>

    <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <span class="mr-1 text-xs text-txt3">{{ t('pages.board.filters.label') }}</span>
        <button
          v-for="key in (['queued', 'failed', 'cancelled'] as const)"
          :key="key"
          type="button"
          class="rounded-md inline-flex items-center gap-1.5 border px-2.5 py-1 text-xs transition"
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
      <span class="text-[11px] text-txt3">{{ t('pages.board.toolbarHint') }}</span>
    </div>

    <div class="board-columns">
      <RunBoardColumn
        v-for="col in mainCols"
        :key="col.key"
        :title="t(col.titleKey)"
        :hint="t(col.hintKey)"
        :accent="col.accent"
        :items="column(col.key).items"
        :total="column(col.key).total"
        :loading="showInitialLoading"
        :loading-text="t('pages.board.loading')"
        :empty-text="t(col.emptyKey)"
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
      </RunBoardColumn>
    </div>

    <div
      v-if="visibleExtras.length"
      class="board-columns mt-3.5"
      data-testid="board-extra-columns"
    >
      <RunBoardColumn
        v-for="col in visibleExtras"
        :key="col.key"
        :title="t(col.titleKey)"
        :hint="t('pages.board.hints.extraFilter')"
        accent="extra"
        :items="column(col.key).items"
        :total="column(col.key).total"
        :loading="loading && !column(col.key).items.length && !column(col.key).error"
        :loading-text="t('pages.board.loading')"
        :empty-text="t(col.emptyKey)"
        :truncated-hint="truncatedHint(col.key)"
        @select="openPreview"
      />
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
.board-columns { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); align-items: start; gap: 14px; }
@container board-workbench (max-width: 1039px) {
  .board-columns { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@container board-workbench (max-width: 799px) {
  .board-columns { grid-template-columns: minmax(0, 1fr); }
}

.board-text-link { display: inline-flex; min-height: 36px; align-items: center; gap: 6px; padding: 0 6px; border-radius: 6px; color: rgb(var(--c-txt3)); }
.board-text-link:hover { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .035); }
.board-text-link:focus-visible { outline: 2px solid rgb(var(--c-accent) / .45); outline-offset: 2px; }
.board-overview { display: flex; flex-wrap: wrap; align-items: center; gap: 18px 32px; border-block: 1px solid rgb(var(--c-line) / .7); padding: 15px 2px; }
.board-overview-heading { display: flex; align-items: center; gap: 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.board-overview-values { display: flex; flex: 1; gap: 40px; margin: 0; }
.board-overview-values > div { display: flex; align-items: baseline; gap: 12px; }
.board-overview-values dt { color: rgb(var(--c-txt3)); font-size: 12px; white-space: nowrap; }
.board-overview-values dd { margin: 0; color: rgb(var(--c-txt)); font-size: 21px; font-weight: 600; font-variant-numeric: tabular-nums; line-height: 1; }
@container board-workbench (max-width: 799px) {
  .board-overview { gap: 14px; padding-block: 16px; }
  .board-overview-heading { width: 100%; }
  .board-overview-values { justify-content: space-between; gap: 12px; }
  .board-overview-values > div { flex-direction: column; gap: 8px; }
  .board-text-link { min-height: 44px; }
}
</style>

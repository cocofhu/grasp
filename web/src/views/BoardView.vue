<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, toRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import RunBoardColumn from '@/components/board/RunBoardColumn.vue'
import RunBoardPreviewDrawer from '@/components/board/RunBoardPreviewDrawer.vue'
import RunBoardStatusListModal from '@/components/board/RunBoardStatusListModal.vue'
import TokenStatsPanel from '@/components/board/token-stats/TokenStatsPanel.vue'
import { serializeStatusQuery } from '@/lib/composables/useStatusFilter'
import { useRunBoard, type BoardColumnKey } from '@/lib/run/useRunBoard'
import type { Run } from '@/lib/shared/types'

const props = defineProps<{
  /** Required project boundary — board never loads unfiltered platform runs. */
  projectId: string
  /** When embedded in project detail, hide the standalone page heading. */
  embedded?: boolean
}>()

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

function queryText(key: string): string {
  const v = route.query[key]
  return typeof v === 'string' ? v : ''
}

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

const projectIdRef = toRef(props, 'projectId')

const { load, column, loading, hasLoaded, error } = useRunBoard({
  mode: 'full',
  projectId: projectIdRef,
  extraStatuses: () => extraStatuses.value,
})

watch(projectIdRef, () => {
  void load()
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
      projectId: props.projectId,
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
  <div data-testid="board-view" class="flex h-full min-h-0 min-w-0 flex-col">
    <div v-if="!embedded" class="mb-5 flex shrink-0 flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-lg font-semibold text-txt">{{ t('pages.board.title') }}</h2>
        <p class="text-sm text-txt3">{{ t('pages.board.subtitle') }}</p>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto">
    <div
      v-if="error && error !== 'missing_project'"
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

    <TokenStatsPanel
      :project-id="projectId"
      :initial-window="queryText('window')"
      :initial-from="queryText('from')"
      :initial-to="queryText('to')"
      :initial-granularity="queryText('granularity')"
    />

    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <span class="mr-1 text-xs text-txt3">{{ t('pages.board.filters.label') }}</span>
        <button
          v-for="key in (['queued', 'failed', 'cancelled'] as const)"
          :key="key"
          type="button"
          class="rounded-md inline-flex items-center gap-1.5 border px-2.5 py-1 text-xs transition"
          :class="
            extraEnabled[key]
              ? 'border-accent-2/45 bg-accent-dim text-txt'
              : 'border-line bg-surface text-txt2 hover:border-line-strong'
          "
          :data-testid="`board-filter-${key}`"
          @click="toggleExtra(key)"
        >
          <span
            class="inline-block h-2 w-2 rounded-full border"
            :class="extraEnabled[key] ? 'border-accent-2 bg-accent-2' : 'border-line-strong bg-transparent'"
          />
          {{ t(`common.status.${key}`) }}
        </button>
      </div>
      <span class="text-[11px] text-txt3">{{ t('pages.board.toolbarHint') }}</span>
    </div>

    <div class="grid grid-cols-1 items-start gap-3.5 md:grid-cols-3">
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
      class="mt-3.5 grid grid-cols-1 items-start gap-3.5 sm:grid-cols-2 lg:grid-cols-3"
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
      :project-id="projectId"
      :status="listModalStatus"
      :title="listModalTitle"
      @close="closeListModal"
      @select="onListSelect"
    />
  </div>
</template>

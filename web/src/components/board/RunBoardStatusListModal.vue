<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import PriorityBadge from '@/components/ui/PriorityBadge.vue'
import { api } from '@/lib/api/api'
import { fmtDuration } from '@/lib/shared/format'
import { runBoardTitle, runIdShort } from '@/lib/run/runBoard'
import type { Run } from '@/lib/shared/types'

const PAGE_SIZE = 20

const props = defineProps<{
  open: boolean
  projectId: string
  /** Run status filter matching the activated board column. */
  status: string
  /** Localized column title for the modal heading. */
  title: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'select', run: Run): void
}>()

const { t } = useI18n()

const items = ref<Run[]>([])
const total = ref(0)
const page = ref(0)
const hasMore = ref(false)
const loading = ref(false)
/** True while the very first page is in flight (empty body). */
const initialLoading = ref(false)
const loadFailed = ref(false)
/** Distinguishes first-page failure from append failure. */
const failKind = ref<'initial' | 'more' | null>(null)

let requestSeq = 0

const shownCount = computed(() => items.value.length)

const rangeText = computed(() => {
  if (loadFailed.value && failKind.value === 'initial') {
    return t('pages.board.statusList.rangeInitialFailed')
  }
  if (loadFailed.value && failKind.value === 'more') {
    return t('pages.board.statusList.rangeMoreFailed', {
      shown: shownCount.value,
      total: total.value,
    })
  }
  if (!loading.value && !hasMore.value && shownCount.value > 0) {
    return t('pages.board.statusList.rangeComplete', {
      shown: shownCount.value,
      total: total.value,
    })
  }
  return t('pages.board.statusList.range', {
    shown: shownCount.value,
    total: total.value,
  })
})

const subtitle = computed(() => {
  if (loadFailed.value) {
    return t('pages.board.statusList.subtitleFailed', { title: props.title })
  }
  return t('pages.board.statusList.subtitle', {
    title: props.title,
    total: total.value,
  })
})

function resetState() {
  items.value = []
  total.value = 0
  page.value = 0
  hasMore.value = false
  loading.value = false
  initialLoading.value = false
  loadFailed.value = false
  failKind.value = null
  requestSeq += 1
}

async function fetchPage(nextPage: number, mode: 'initial' | 'more') {
  if (!props.projectId || !props.status) return
  const seq = ++requestSeq
  loading.value = true
  if (mode === 'initial') {
    initialLoading.value = true
    loadFailed.value = false
    failKind.value = null
  } else {
    loadFailed.value = false
    failKind.value = null
  }

  try {
    const data = await api.listRuns({
      projectId: props.projectId,
      status: props.status,
      page: nextPage,
      pageSize: PAGE_SIZE,
    })

    if (seq !== requestSeq) return

    const batch = data.items
    if (mode === 'initial') {
      items.value = batch
    } else {
      items.value = [...items.value, ...batch]
    }
    total.value = data.total
    page.value = data.page
    hasMore.value = data.hasMore
    loadFailed.value = false
    failKind.value = null
  } catch {
    if (seq !== requestSeq) return
    loadFailed.value = true
    failKind.value = mode
    // Keep already-shown items on more-failure (n5).
  } finally {
    if (seq === requestSeq) {
      loading.value = false
      initialLoading.value = false
    }
  }
}

function loadInitial() {
  resetState()
  void fetchPage(1, 'initial')
}

function loadMore() {
  if (loading.value || !hasMore.value) return
  void fetchPage(page.value + 1, 'more')
}

function retry() {
  if (loading.value) return
  if (failKind.value === 'more' && items.value.length > 0) {
    void fetchPage(page.value + 1, 'more')
    return
  }
  loadInitial()
}

function rowMeta(run: Run): string {
  const node = run.currentNodeLabel || '—'
  const prog = run.progress != null ? `${run.progress}%` : '—'
  const dur = fmtDuration(run.durationSec ?? 0)
  return `${node} · ${prog} · ${dur}`
}

function onSelect(run: Run) {
  emit('select', run)
}

watch(
  () => [props.open, props.projectId, props.status] as const,
  ([open]) => {
    if (open) {
      loadInitial()
    } else {
      resetState()
    }
  },
  { immediate: true },
)
</script>

<template>
  <AppModal
    :open="open"
    :title="title"
    :width="720"
    :close-on-esc="true"
    @close="emit('close')"
  >
    <template #header>
      <div class="min-w-0">
        <div class="text-[15px] font-semibold text-txt">{{ title }}</div>
        <p class="mt-0.5 truncate text-xs text-txt2" data-testid="board-status-list-subtitle">
          {{ subtitle }}
        </p>
      </div>
    </template>

    <div class="-m-5" data-testid="board-status-list-body">
      <div v-if="initialLoading" class="px-5 py-10 text-center text-sm text-txt2" data-testid="board-status-list-loading">
        {{ t('pages.board.statusList.loading') }}
      </div>

      <div
        v-else-if="loadFailed && failKind === 'initial'"
        class="px-5 py-10 text-center text-sm"
        data-testid="board-status-list-error"
      >
        <strong class="mb-1 block text-txt">{{ t('pages.board.statusList.errorTitle') }}</strong>
        <p class="text-err">{{ t('pages.board.statusList.errorHint') }}</p>
      </div>

      <div
        v-else-if="!items.length"
        class="px-5 py-10 text-center text-sm text-txt2"
        data-testid="board-status-list-empty"
      >
        <strong class="mb-1 block text-txt">{{ t('pages.board.statusList.emptyTitle') }}</strong>
        <p>{{ t('pages.board.statusList.emptyHint', { title }) }}</p>
      </div>

      <div v-else class="flex flex-col" data-testid="board-status-list-rows">
        <button
          v-for="run in items"
          :key="run.id"
          type="button"
          class="grid w-full grid-cols-[92px_minmax(0,1fr)_auto] items-center gap-2.5 border-b border-line px-4 py-2.5 text-left transition hover:bg-elevated focus-visible:bg-elevated focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-[-1px] focus-visible:outline-accent"
          :data-testid="`board-status-list-row-${run.id}`"
          @click="onSelect(run)"
        >
          <span class="font-mono text-[11px] text-txt3">#{{ runIdShort(run.id) }}</span>
          <span class="min-w-0">
            <span class="block truncate text-[13px] font-semibold text-txt">{{ runBoardTitle(run) }}</span>
            <span class="mt-0.5 block truncate text-[11px] text-txt3">{{ rowMeta(run) }}</span>
          </span>
          <PriorityBadge :priority="run.priority" />
        </button>
      </div>
    </div>

    <template #footer>
      <div class="flex w-full items-center justify-between gap-2" data-testid="board-status-list-footer">
        <span class="text-xs text-txt3" data-testid="board-status-list-range">{{ rangeText }}</span>
        <div class="flex items-center gap-2">
          <AppButton
            v-if="loadFailed"
            variant="outline"
            size="sm"
            data-testid="board-status-list-retry"
            :disabled="loading"
            @click="retry"
          >
            {{ t('pages.board.statusList.retry') }}
          </AppButton>
          <AppButton
            v-else-if="hasMore"
            variant="primary"
            size="sm"
            data-testid="board-status-list-load-more"
            :loading="loading"
            :disabled="loading"
            @click="loadMore"
          >
            {{ t('pages.board.statusList.loadMore') }}
          </AppButton>
        </div>
      </div>
    </template>
  </AppModal>
</template>

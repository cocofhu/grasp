<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Pagination from '@/components/ui/Pagination.vue'
import RunOutputPptModal from '@/components/shell/RunOutputPptModal.vue'
import { useNotificationsPageEntry } from '@/lib/composables/useNotificationsPageEntry'
import { relTime } from '@/lib/shared/format'
import {
  NOTIFICATION_PAGE_SIZE,
  useRunTerminalNotifications,
} from '@/lib/run/useRunTerminalNotifications'
import type { NotificationReadFilter } from '@/lib/api/clients/notificationsClient'

type ReadFilter = NotificationReadFilter

const PAGE_SIZE = NOTIFICATION_PAGE_SIZE

const { t } = useI18n()
const router = useRouter()
const { enterNonce } = useNotificationsPageEntry()

const {
  listItems,
  listTotal,
  unreadCount,
  allCount,
  readCount,
  markRead,
  markAllRead,
  refreshPage,
  ensureUsername,
  loading,
  pageLoading,
} = useRunTerminalNotifications()

const filter = ref<ReadFilter>('all')
const page = ref(1)
const outputOpen = ref(false)

/** Whole-list fade when filter/page changes (g3.2). */
const listFadeKey = computed(() => `${filter.value}|${page.value}`)
const outputRunId = ref<string | null>(null)
const outputContext = ref('')

const filterCounts = computed<Record<ReadFilter, number>>(() => ({
  all: allCount.value,
  unread: unreadCount.value,
  read: readCount.value,
}))

const pagedItems = computed(() => listItems.value)

const filteredTotal = computed(() => listTotal.value)

const pagerSummary = computed(() =>
  t('pages.notifications.pagerSummary', { total: filteredTotal.value, pageSize: PAGE_SIZE }),
)

const showLoading = computed(
  () => (loading.value || pageLoading.value) && !listItems.value.length,
)

function setFilter(next: ReadFilter) {
  filter.value = next
  page.value = 1
  void refreshPage({ page: 1, filter: next, source: 'page' })
}

function resetToAllFirstPage() {
  filter.value = 'all'
  page.value = 1
  void refreshPage({ page: 1, filter: 'all', source: 'page' })
}

function clampPage() {
  const n = filteredTotal.value
  if (n <= 0) {
    page.value = 1
    return
  }
  const last = Math.ceil(n / PAGE_SIZE)
  if (page.value > last || pagedItems.value.length === 0) {
    page.value = last
    void refreshPage({ page: last, filter: filter.value, source: 'page' })
  }
}

watch(enterNonce, () => {
  resetToAllFirstPage()
})

watch(page, (next, prev) => {
  if (next === prev) return
  void refreshPage({ page: next, filter: filter.value, source: 'page' })
})

watch(filteredTotal, () => {
  clampPage()
})

function statusLabel(status: string) {
  return status === 'failed' ? t('common.status.failed') : t('common.status.completed')
}

function itemTitle(item: {
  title: string
  titleNeutral?: boolean
  workflowName: string
  runId: string
  status: 'completed' | 'failed'
}) {
  if (item.titleNeutral) {
    return t('shell.runNotifications.neutralTitle', {
      name: item.workflowName || item.runId,
      status: statusLabel(item.status),
    })
  }
  return item.title
}

function itemContext(item: { workflowName: string; runId: string; title: string }) {
  const wf = item.workflowName || item.title
  return wf ? `${wf} · ${item.runId}` : item.runId
}

async function onItemClick(item: {
  runId: string
  status: 'completed' | 'failed'
  title: string
  workflowName: string
}) {
  if (item.status === 'failed') {
    markRead(item.runId)
    await router.push(`/runs/${item.runId}`)
    return
  }
  // completed: defer markRead until user clicks「标记已读」in the output modal
  outputContext.value = itemContext(item)
  outputRunId.value = item.runId
  outputOpen.value = true
}

function closeOutputModal() {
  outputOpen.value = false
  outputRunId.value = null
  outputContext.value = ''
}

function onOutputMarkRead() {
  if (outputRunId.value) markRead(outputRunId.value)
  closeOutputModal()
}

onMounted(() => {
  ensureUsername()
  void refreshPage({ page: 1, filter: 'all', source: 'manual' })
})

defineExpose({
  page,
  filter,
  PAGE_SIZE,
})
</script>

<template>
  <div class="notifications-page flex h-full min-h-0 min-w-0 flex-col" data-testid="notifications-page">
    <header class="notifications-header">
      <h1>{{ t('pages.notifications.title') }}</h1>
    </header>

    <div class="notifications-toolbar">
      <div class="notifications-filters" role="group" :aria-label="t('pages.notifications.title')">
        <button
          v-for="opt in (['all', 'unread', 'read'] as const)" :key="opt" type="button"
          class="notification-filter min-h-11 border-b-2 border-transparent px-4 text-[12px] text-txt2 hover:text-txt"
          :class="filter === opt ? 'border-accent text-txt' : ''" :data-testid="`notifications-filter-${opt}`"
          :aria-pressed="filter === opt" @click="setFilter(opt)"
        >
          {{ t(`pages.notifications.filter.${opt}`) }}
          <span class="notification-filter-count">{{ filterCounts[opt] }}</span>
        </button>
      </div>
      <div class="notifications-actions">
        <span class="notification-unread-summary" data-testid="notifications-unread-count">
          <span v-if="unreadCount" class="notification-summary-dot" aria-hidden="true" />
          {{ t('shell.runNotifications.unreadCount', { n: unreadCount }) }}
        </span>
        <button
          type="button"
          class="notification-mark-all rounded-md min-h-11 border border-line bg-transparent px-3 text-[12px] text-txt2 hover:border-accent hover:text-accent disabled:opacity-40"
          data-testid="notifications-mark-all" :disabled="unreadCount === 0" @click="markAllRead()"
        >
          <Icon name="check" :size="14" />{{ t('shell.runNotifications.markAllRead') }}
        </button>
      </div>
    </div>

    <div class="notifications-body flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <div class="notifications-scroll min-h-0 flex-1 overflow-y-auto">
        <div v-if="showLoading" class="flex h-full items-center justify-center px-6 py-14 text-center text-sm text-txt3" data-testid="notifications-loading">
          {{ t('pages.notifications.loading') }}
        </div>
        <div v-else-if="!pagedItems.length" class="flex h-full items-center justify-center px-6 py-14">
          <EmptyState icon="bell"
            :title="filter === 'all' ? t('shell.runNotifications.empty') : t('pages.notifications.filterEmpty')"
            :desc="filter === 'all' ? `${t('shell.runNotifications.emptyHint')} ${t('shell.runNotifications.emptyRunsHint')}` : t('pages.notifications.filterEmptyHint')"
          />
        </div>
        <div v-else>
          <Transition name="ui-fade" mode="out-in">
            <div :key="listFadeKey" data-testid="notifications-list-fade">
              <button
                v-for="item in pagedItems" :key="item.runId" type="button" class="notification-row"
                :class="{ 'is-unread': item.unread }" data-testid="notifications-item"
                :data-run-id="item.runId" :data-status="item.status" :data-unread="item.unread ? 'true' : 'false'"
                :data-before-baseline="item.beforeBaseline ? 'true' : 'false'" @click="onItemClick(item)"
              >
                <span class="notification-signal" aria-hidden="true" />
                <span v-if="item.unread" class="sr-only">{{ t('pages.notifications.filter.unread') }}</span>
                <span class="notification-content">
                  <span class="notification-title" :title="itemTitle(item)">{{ itemTitle(item) }}</span>
                  <span class="notification-meta">
                    <span class="notification-status">
                      <Icon :key="item.status" :name="item.status === 'failed' ? 'alert' : 'check'" :size="12" />{{ statusLabel(item.status) }}
                    </span>
                    <span class="notification-source" :title="item.workflowName || item.title">{{ item.workflowName || item.title }}</span>
                    <span class="notification-run-id" :title="item.runId">{{ item.runId }}</span>
                    <span v-if="item.beforeBaseline" class="notification-baseline" data-testid="notifications-before-baseline">{{ t('shell.runNotifications.beforeBaseline') }}</span>
                  </span>
                </span>
                <span class="notification-trailing">
                  <time class="notification-time" :datetime="item.finishedApprox || item.startedAt" :title="item.finishedApprox || item.startedAt">{{ relTime(item.finishedApprox || item.startedAt) }}</time>
                  <span class="notification-open">
                    {{ item.status === 'completed' ? t('shell.runNotifications.clickForOutput') : t('shell.runNotifications.clickForDetail') }}
                    <Icon name="arrow-right" :size="12" />
                  </span>
                </span>
              </button>
            </div>
          </Transition>
        </div>
      </div>
      <Pagination v-if="filteredTotal > PAGE_SIZE" v-model:page="page" class="shrink-0" data-testid="notifications-pagination"
        :page-size="PAGE_SIZE" :total="filteredTotal" :summary-override="pagerSummary" summary-test-id="notifications-pager-summary"
      />
    </div>

    <RunOutputPptModal :open="outputOpen" :run-id="outputRunId" :context-label="outputContext" @close="closeOutputModal" @mark-read="onOutputMarkRead" />
  </div>
</template>

<style scoped>
.notifications-header {
  flex: none;
  margin-bottom: 20px;
}

.notifications-header h1 {
  margin: 0;
  color: rgb(var(--c-txt));
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -.02em;
}

.notifications-toolbar {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px 24px;
  border-bottom: 1px solid rgb(var(--c-line));
}

.notifications-filters {
  display: flex;
  align-self: stretch;
}

.notification-filter {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-bottom: -1px;
  font-size: 13px;
}

.notification-filter[aria-pressed="true"] {
  border-bottom-color: rgb(var(--c-accent));
  color: rgb(var(--c-txt));
  font-weight: 600;
}

.notification-filter-count {
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 400;
  font-variant-numeric: tabular-nums;
}

.notification-filter[aria-pressed="true"] .notification-filter-count {
  color: rgb(var(--c-txt2));
}

.notifications-actions {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 16px;
  padding-bottom: 8px;
}

.notification-unread-summary {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.notification-summary-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: rgb(var(--c-accent));
}

.notification-mark-all {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border-radius: 8px;
}

.notifications-scroll {
  min-width: 0;
}

.notification-row {
  display: grid;
  width: 100%;
  min-width: 0;
  grid-template-columns: 6px minmax(0, 1fr) auto;
  align-items: start;
  column-gap: 14px;
  padding: 17px 10px;
  border: 0;
  border-bottom: 1px solid rgb(var(--c-line) / .6);
  border-radius: 0;
  background: transparent;
  color: rgb(var(--c-txt));
  text-align: left;
  font: inherit;
  cursor: pointer;
  transition: background var(--dur-ui);
}

.notification-row:last-child {
  border-bottom: 0;
}

.notification-row:hover {
  background: rgb(var(--c-elevated) / .5);
}

.notification-row:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .5);
  outline-offset: -2px;
  background: rgb(var(--c-accent) / .035);
}

.notification-filter:focus-visible, .notification-mark-all:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .5);
  outline-offset: -2px;
}

.notification-signal {
  width: 5px;
  height: 5px;
  margin-top: 8px;
  border-radius: 50%;
  background: rgb(var(--c-accent));
  visibility: hidden;
}

.is-unread .notification-signal {
  visibility: visible;
}

.notification-content {
  display: block;
  min-width: 0;
}

.notification-title {
  display: block;
  overflow: hidden;
  color: rgb(var(--c-txt2));
  font-size: 14px;
  font-weight: 500;
  line-height: 1.65;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.is-unread .notification-title {
  color: rgb(var(--c-txt));
  font-weight: 600;
}

.notification-meta {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 12px;
  margin-top: 5px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  line-height: 1.6;
}

.notification-status {
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 5px;
  color: rgb(var(--c-ok));
}

.notification-row[data-status="failed"] .notification-status {
  color: rgb(var(--c-err));
}

.notification-source {
  max-width: min(100%, 260px);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notification-run-id {
  max-width: min(100%, 160px);
  min-width: 0;
  overflow: hidden;
  font: 10px/1.6 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notification-baseline {
  max-width: 100%;
  font-size: 10px;
  overflow-wrap: anywhere;
}

.notification-trailing {
  display: flex;
  min-width: 110px;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  padding-top: 2px;
}

.notification-time {
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.notification-open {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: rgb(var(--c-accent-2));
  font-size: 11px;
  opacity: 0;
  transition: opacity var(--dur-ui);
}

.notification-row:hover .notification-open, .notification-row:focus-visible .notification-open {
  opacity: 1;
}

@media (max-width: 767px) {
  .notifications-header {
    margin-bottom: 16px;
  }
  .notifications-toolbar {
    gap: 0;
  }
  .notifications-actions {
    width: 100%;
    justify-content: space-between;
    gap: 10px;
    padding: 10px 0 12px;
  }
  .notification-row {
    grid-template-columns: 6px minmax(0, 1fr);
    column-gap: 11px;
    padding: 16px 2px;
  }
  .notification-title {
    display: -webkit-box;
    white-space: normal;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow-wrap: anywhere;
  }
  .notification-meta {
    gap: 4px 10px;
  }
  .notification-source {
    max-width: min(100%, 170px);
  }
  .notification-run-id {
    max-width: min(100%, 130px);
  }
  .notification-trailing {
    min-width: 0;
    grid-column: 2;
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-top: 7px;
    padding: 0;
  }
  .notification-open {
    opacity: 1;
  }
}

@media (hover: none) {
  .notification-open {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .notification-row, .notification-open {
    transition: none;
  }
}
</style>

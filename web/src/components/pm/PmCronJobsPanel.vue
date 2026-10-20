<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import AppButton from '@/components/ui/AppButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Icon from '@/components/ui/Icon.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import { api } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import { useToast } from '@/lib/composables/useToast'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import type { AgentCronJob } from '@/lib/shared/types'

const SKELETON_ROWS = 5
const { t } = useI18n()
const toast = useToast()
const cronSeq = createListRequestSeq()

const items = ref<AgentCronJob[]>([])
const loading = ref(true)
const hasInitialLoaded = ref(false)
const loadFailed = ref(false)
const loadDenied = ref(false)
const togglingIds = ref<string[]>([])
const deletingId = ref<string | null>(null)

const showRefreshProgress = computed(
  () => loading.value && hasInitialLoaded.value && items.value.length > 0,
)
const showSkeleton = computed(
  () => loading.value && items.value.length === 0 && !loadFailed.value && !loadDenied.value,
)

async function load() {
  const localSeq = cronSeq.beginListRequest()
  loading.value = true
  loadFailed.value = false
  loadDenied.value = false
  try {
    const res = await api.listCronJobs()
    if (!cronSeq.isCurrentSeq(localSeq)) return
    items.value = res.items || []
  } catch (e: unknown) {
    if (!cronSeq.isCurrentSeq(localSeq)) return
    if (items.value.length > 0) {
      toast.error(String((e as Error)?.message || e))
      return
    }
    const status = httpStatusOf(e)
    loadDenied.value = status === 403
    loadFailed.value = status !== 403
  } finally {
    if (!cronSeq.isCurrentSeq(localSeq)) return
    loading.value = false
    hasInitialLoaded.value = true
  }
}

function scheduleLabel(job: AgentCronJob): string {
  const kind = job.scheduleKind || ''
  const expr = job.scheduleExpr || ''
  if (!kind && !expr) return '—'
  return kind ? `${kind}: ${expr}` : expr
}

async function onDeliverToggle(job: AgentCronJob, checked: boolean) {
  if (togglingIds.value.includes(job.id) || deletingId.value === job.id) return
  const prev = job.deliverToChannel
  job.deliverToChannel = checked
  togglingIds.value = [...togglingIds.value, job.id]
  try {
    const updated = await api.patchCronJob(job.id, {
      deliverToChannel: checked,
    })
    const idx = items.value.findIndex((j) => j.id === job.id)
    if (idx >= 0) items.value[idx] = { ...items.value[idx], ...updated }
  } catch (e: any) {
    job.deliverToChannel = prev
    toast.error(String(e?.message || e))
  } finally {
    togglingIds.value = togglingIds.value.filter((id) => id !== job.id)
  }
}

async function removeJob(id: string) {
  if (!confirm(t('pages.workspace.cron.deleteConfirm'))) return
  deletingId.value = id
  try {
    await api.deleteCronJob(id)
    await load()
    toast.success(t('pages.workspace.cron.deleted'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    deletingId.value = null
  }
}

onMounted(() => void load())
</script>

<template>
  <div
    class="flex min-h-[420px] flex-col gap-4"
    data-testid="workspace-cron-jobs-panel"
    :aria-busy="loading ? 'true' : 'false'"
  >
    <div
      v-if="showRefreshProgress"
      class="h-[2px] overflow-hidden bg-line"
      data-testid="cron-thin-progress"
      aria-hidden="true"
    >
      <i class="admin-list-thin-bar bg-accent" />
    </div>

    <div :class="showRefreshProgress ? 'opacity-[0.55]' : ''">
      <div
        v-if="showSkeleton"
        class="overflow-x-auto border-y border-line/60"
        data-testid="cron-table-skeleton"
        aria-hidden="true"
      >
        <table class="cron-table w-full min-w-[1380px] text-left text-sm">
          <thead class="bg-elevated text-xs text-txt3">
            <tr>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colName') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colAgent') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colSchedule') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colEnabled') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colDeliver') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colNextRun') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colLastRun') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colStatus') }}</th>
              <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colActions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="n in SKELETON_ROWS" :key="'cron-skel-' + n" class="border-t border-line">
              <td v-for="c in 9" :key="'c' + c" class="px-3 py-2.5">
                <div class="h-3 w-3/4 bg-elevated animate-pulse" />
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div
        v-else-if="loadDenied"
        role="status"
        data-testid="cron-denied"
        class="px-5 py-10 text-center"
      >
        <Icon name="lock" :size="22" class="mx-auto mb-3 text-warn" />
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="cron-retry" @click="load">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>

      <div
        v-else-if="loadFailed"
        role="status"
        data-testid="cron-failed"
        class="px-5 py-10 text-center"
      >
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="cron-retry" @click="load">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>

      <EmptyState
        v-else-if="!items.length"
        :title="t('pages.workspace.cron.emptyTitle')"
        :desc="t('pages.workspace.cron.emptyDesc')"
      />
      <div v-else class="overflow-hidden border-y border-line/60">
        <div class="scroll-area overflow-x-auto">
          <table class="cron-table w-full min-w-[1380px] text-left text-sm" data-testid="cron-table">
            <thead class="bg-elevated text-xs text-txt3">
              <tr>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colName') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colAgent') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colSchedule') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colEnabled') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colDeliver') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colNextRun') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colLastRun') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colStatus') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colLastError') }}</th>
                <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.workspace.cron.colActions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="job in items" :key="job.id" class="border-t border-line">
                <td class="px-3 py-2.5">
                  <div class="font-medium text-txt">{{ job.name }}</div>
                </td>
                <td class="px-3 py-2.5 text-txt2">{{ job.agentName || '—' }}</td>
                <td class="px-3 py-2.5 font-mono text-xs text-txt2">{{ scheduleLabel(job) }}</td>
                <td class="px-3 py-2.5">
                  <span class="text-txt2">
                    {{ job.enabled ? t('pages.workspace.cron.enabledYes') : t('pages.workspace.cron.enabledNo') }}
                  </span>
                </td>
                <td class="px-3 py-2.5">
                  <label
                    class="inline-flex items-center gap-2"
                    :title="t('pages.workspace.cron.deliverHint')"
                  >
                    <AppSwitch
                      data-testid="cron-deliver-toggle"
                      :model-value="job.deliverToChannel"
                      :disabled="togglingIds.includes(job.id) || deletingId === job.id"
                      :aria-label="t('pages.workspace.cron.deliverLabel')"
                      @update:model-value="onDeliverToggle(job, $event)"
                    />
                    <span class="text-xs text-txt3 whitespace-nowrap">{{ t('pages.workspace.cron.deliverLabel') }}</span>
                  </label>
                </td>
                <td class="px-3 py-2.5 text-txt3">{{ job.nextRunAt ? fmtTime(job.nextRunAt) : '—' }}</td>
                <td class="px-3 py-2.5 text-txt3">{{ job.lastRunAt ? fmtTime(job.lastRunAt) : '—' }}</td>
                <td class="px-3 py-2.5">
                  <StatusPill v-if="job.lastStatus" :status="job.lastStatus" size="sm" />
                  <span v-else class="text-txt3">—</span>
                </td>
                <td class="px-3 py-2.5 text-[11px] text-err" data-testid="cron-last-error">
                  {{ job.lastError || '—' }}
                </td>
                <td class="px-3 py-2.5 text-right">
                  <button
                    type="button"
                    class="whitespace-nowrap text-[11px] text-err disabled:cursor-not-allowed disabled:opacity-40"
                    data-testid="workspace-cron-delete"
                    :disabled="deletingId === job.id || togglingIds.includes(job.id)"
                    @click="removeJob(job.id)"
                  >
                    {{ deletingId === job.id ? t('common.buttons.deleting') : t('common.buttons.delete') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cron-table { table-layout: auto; }
.cron-table th, .cron-table td { white-space: nowrap; }
.cron-table td:first-child { min-width: 220px; max-width: 300px; white-space: normal; }
.cron-table td:nth-child(3) { min-width: 150px; }
.cron-table td:nth-child(6), .cron-table td:nth-child(7) { min-width: 160px; }
.cron-table td:nth-child(9) { min-width: 180px; max-width: 320px; white-space: normal; overflow-wrap: anywhere; }
</style>

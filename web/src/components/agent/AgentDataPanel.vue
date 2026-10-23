<script setup lang="ts">
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import { computed, ref, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import Icon from '@/components/ui/Icon.vue'
import { api } from '@/lib/api/api'
import type { MemoryItem, ChatThread, ChatMessage, AgentCronJob } from '@/lib/shared/types'
import { fmtTime } from '@/lib/shared/format'
import { useToast } from '@/lib/composables/useToast'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import AppButton from '@/components/ui/AppButton.vue'

export type DataSubTab = 'memory' | 'context' | 'jobs'

const props = withDefaults(
  defineProps<{ agentName: string; subTab?: DataSubTab }>(),
  { subTab: undefined },
)
const emit = defineEmits<{ 'update:subTab': [value: DataSubTab] }>()

const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()

const adminRequiredTip = computed(() => t('pages.workspace.pm.adminRequired'))
const dataSeq = createListRequestSeq()

const sub = ref<DataSubTab>(props.subTab || 'memory')
const loadFailed = ref(false)
const loadDenied = ref(false)
const deletingMemId = ref<string | null>(null)
const deletingThreadId = ref<string | null>(null)
const deletingJobId = ref<string | null>(null)

function classifyLoadError(err: unknown) {
  const status = httpStatusOf(err)
  loadDenied.value = status === 403
  loadFailed.value = status !== 403
}

watch(
  () => props.subTab,
  (v) => {
    if (v && v !== sub.value) sub.value = v
  },
)

function setSub(next: DataSubTab) {
  if (sub.value === next) return
  sub.value = next
  emit('update:subTab', next)
}

function permissionMessage(err: unknown): string {
  const msg = String((err as { message?: string })?.message || err || '')
  if (/admin required/i.test(msg)) return adminRequiredTip.value
  return msg
}

const memories = ref<MemoryItem[]>([])
const memLoading = ref(false)
const memTitle = ref('')
const memContent = ref('')
const memEditingId = ref<string | null>(null)
const memSaving = ref(false)

async function loadMemories(localSeq = dataSeq.beginListRequest()) {
  const keepStale = memories.value.length > 0
  memLoading.value = true
  loadFailed.value = false
  loadDenied.value = false
  try {
    const res = await api.listAgentMemories(props.agentName)
    if (!dataSeq.isCurrentSeq(localSeq)) return
    memories.value = res.items || []
  } catch (e: unknown) {
    if (!dataSeq.isCurrentSeq(localSeq)) return
    if (keepStale) {
      toast.error(permissionMessage(e))
      return
    }
    classifyLoadError(e)
  } finally {
    if (!dataSeq.isCurrentSeq(localSeq)) return
    memLoading.value = false
  }
}
function editMemory(m: MemoryItem) {
  memEditingId.value = m.id
  memTitle.value = m.title
  memContent.value = m.content
}
function resetMemForm() {
  memEditingId.value = null
  memTitle.value = ''
  memContent.value = ''
}
async function saveMemory() {
  if (!memTitle.value.trim()) {
    toast.error(t('pages.agentStudio.data.memory.titleRequired'))
    return
  }
  memSaving.value = true
  try {
    if (memEditingId.value) {
      await api.updateAgentMemory(props.agentName, memEditingId.value, {
        title: memTitle.value,
        content: memContent.value,
      })
    } else {
      await api.upsertAgentMemory(props.agentName, { title: memTitle.value, content: memContent.value })
    }
    resetMemForm()
    await loadMemories()
  } catch (e: any) {
    toast.error(permissionMessage(e))
  } finally {
    memSaving.value = false
  }
}
async function removeMemory(id: string) {
  deletingMemId.value = id
  try {
    await api.deleteAgentMemory(props.agentName, id)
    if (memEditingId.value === id) resetMemForm()
    await loadMemories()
  } catch (e: any) {
    toast.error(permissionMessage(e))
  } finally {
    deletingMemId.value = null
  }
}
async function clearMemories() {
  if (!confirm(t('pages.agentStudio.data.memory.clearConfirm'))) return
  try {
    await api.clearAgentMemories(props.agentName)
    resetMemForm()
    await loadMemories()
  } catch (e: any) {
    toast.error(permissionMessage(e))
  }
}

const threads = ref<ChatThread[]>([])
const threadCounts = ref<Record<string, number>>({})
const ctxLoading = ref(false)
const openThreadId = ref<string | null>(null)
const threadMessages = ref<ChatMessage[]>([])
const msgLoading = ref(false)

async function loadThreads(localSeq = dataSeq.beginListRequest()) {
  const keepStale = threads.value.length > 0
  ctxLoading.value = true
  loadFailed.value = false
  loadDenied.value = false
  try {
    const res = await api.listAgentThreads(props.agentName)
    if (!dataSeq.isCurrentSeq(localSeq)) return
    threads.value = res.items || []
    threadCounts.value = res.messageCounts || {}
  } catch (e: unknown) {
    if (!dataSeq.isCurrentSeq(localSeq)) return
    if (keepStale) {
      toast.error(permissionMessage(e))
      return
    }
    classifyLoadError(e)
  } finally {
    if (!dataSeq.isCurrentSeq(localSeq)) return
    ctxLoading.value = false
  }
}
async function openThread(id: string) {
  openThreadId.value = id
  msgLoading.value = true
  try {
    const res = await api.listAgentThreadMessages(props.agentName, id)
    threadMessages.value = res.items || []
  } catch (e: any) {
    toast.error(permissionMessage(e))
  } finally {
    msgLoading.value = false
  }
}
async function removeThread(id: string) {
  if (!confirm(t('pages.agentStudio.data.context.deleteConfirm'))) return
  deletingThreadId.value = id
  try {
    await api.deleteAgentThread(props.agentName, id)
    if (openThreadId.value === id) {
      openThreadId.value = null
      threadMessages.value = []
    }
    await loadThreads()
  } catch (e: any) {
    toast.error(permissionMessage(e))
  } finally {
    deletingThreadId.value = null
  }
}

const jobs = ref<AgentCronJob[]>([])
const jobsLoading = ref(false)
const jobBusy = ref<string | null>(null)

async function loadJobs(localSeq = dataSeq.beginListRequest()) {
  const keepStale = jobs.value.length > 0
  jobsLoading.value = true
  loadFailed.value = false
  loadDenied.value = false
  try {
    const res = await api.listAgentCronJobs(props.agentName)
    if (!dataSeq.isCurrentSeq(localSeq)) return
    jobs.value = res.items || []
  } catch (e: unknown) {
    if (!dataSeq.isCurrentSeq(localSeq)) return
    if (keepStale) {
      toast.error(permissionMessage(e))
      return
    }
    classifyLoadError(e)
  } finally {
    if (!dataSeq.isCurrentSeq(localSeq)) return
    jobsLoading.value = false
  }
}
async function patchJob(job: AgentCronJob, body: { enabled?: boolean; deliverToChannel?: boolean }) {
  jobBusy.value = job.id
  try {
    const updated = await api.patchAgentCronJob(props.agentName, job.id, body)
    const idx = jobs.value.findIndex((j) => j.id === job.id)
    if (idx >= 0) jobs.value[idx] = { ...jobs.value[idx], ...updated }
    if (body.enabled !== undefined) {
      toast.success(
        body.enabled
          ? t('pages.agentStudio.data.jobs.enabledOn')
          : t('pages.agentStudio.data.jobs.enabledOff'),
      )
    } else if (body.deliverToChannel !== undefined) {
      toast.success(
        body.deliverToChannel
          ? t('pages.agentStudio.data.jobs.deliverOn')
          : t('pages.agentStudio.data.jobs.deliverOff'),
      )
    }
  } catch (e: any) {
    toast.error(permissionMessage(e))
    await loadJobs()
  } finally {
    jobBusy.value = null
  }
}
async function removeJob(id: string) {
  if (!confirm(t('pages.agentStudio.data.jobs.deleteConfirm'))) return
  deletingJobId.value = id
  try {
    await api.deleteAgentCronJob(props.agentName, id)
    await loadJobs()
    toast.success(t('pages.agentStudio.data.jobs.deleted'))
  } catch (e: any) {
    toast.error(permissionMessage(e))
  } finally {
    deletingJobId.value = null
  }
}

function reload() {
  const localSeq = dataSeq.beginListRequest()
  if (sub.value === 'memory') {
    void loadMemories(localSeq)
  } else if (sub.value === 'context') {
    void loadThreads(localSeq)
  } else void loadJobs(localSeq)
}
watch(() => [props.agentName, sub.value], () => reload())
onMounted(() => reload())

const tabLoading = computed(() =>
  sub.value === 'memory' ? memLoading.value : sub.value === 'context' ? ctxLoading.value : jobsLoading.value,
)
const tabHasItems = computed(() =>
  sub.value === 'memory' ? memories.value.length > 0 : sub.value === 'context' ? threads.value.length > 0 : jobs.value.length > 0,
)
const showRefreshProgress = computed(() => tabLoading.value && tabHasItems.value)
const showSkeleton = computed(
  () => tabLoading.value && !tabHasItems.value && !loadFailed.value && !loadDenied.value,
)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col" data-testid="agent-data-panel" :aria-busy="tabLoading ? 'true' : 'false'">
    <div class="border-b border-line px-4 py-3">
      <h3 class="text-sm font-semibold text-txt">{{ t('pages.agentStudio.data.title') }}</h3>
      <p class="mt-0.5 text-[12px] text-txt3">
        {{ t('pages.agentStudio.data.hint') }}
      </p>
      <SlidingTabs :value="sub" indicator-class="rounded bg-accent-dim" class="mt-3 flex gap-1 overflow-x-auto [-webkit-overflow-scrolling:touch]">
        <button
          v-for="k in (['memory', 'context', 'jobs'] as const)"
          :key="k"
          type="button"
          class="shrink-0 rounded px-2.5 text-[12px]"
          :class="[
            isMobile ? 'min-h-11 px-3 py-2.5' : 'py-1',
            sub === k ? 'text-accent-2' : 'text-txt3 hover:text-txt',
          ]"
          :data-active="sub === k"
          @click="setSub(k)"
        >
          {{ t(`pages.agentStudio.data.tabs.${k}`) }}
        </button>
      </SlidingTabs>
    </div>

    <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
      <div
        v-if="showRefreshProgress"
        class="mb-3 h-[2px] overflow-hidden bg-line"
        data-testid="agent-data-thin-progress"
        aria-hidden="true"
      >
        <i class="admin-list-thin-bar bg-accent" />
      </div>

      <div
        v-if="loadDenied"
        role="status"
        data-testid="agent-data-denied"
        class="border-y border-line/60 py-10 text-center"
      >
        <Icon name="lock" :size="22" class="mx-auto mb-3 text-warn" />
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="agent-data-retry" @click="reload">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>
      <div
        v-else-if="loadFailed"
        role="status"
        data-testid="agent-data-failed"
        class="border-y border-line/60 py-10 text-center"
      >
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="agent-data-retry" @click="reload">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>

      <!-- memory: any authenticated user -->
      <div v-else-if="sub === 'memory'" class="space-y-4" :class="showRefreshProgress ? 'opacity-[0.55]' : ''">
        <div class="border-b border-line/60 py-4">
          <div class="grid gap-2">
            <input v-model="memTitle" class="rounded border border-line bg-surface px-3 py-2 text-sm" :placeholder="t('pages.agentStudio.data.memory.titlePh')" />
            <textarea v-model="memContent" rows="3" class="rounded border border-line bg-surface px-3 py-2 text-sm" :placeholder="t('pages.agentStudio.data.memory.contentPh')" />
            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                class="rounded bg-accent px-3 text-[12px] text-white disabled:opacity-50"
                :class="isMobile ? 'min-h-11 px-4' : 'py-1.5'"
                data-testid="agent-data-mem-save"
                :disabled="memSaving"
                @click="saveMemory"
              >
                {{ memSaving ? t('common.buttons.saving') : memEditingId ? t('common.buttons.save') : t('pages.agentStudio.data.memory.add') }}
              </button>
              <button
                v-if="memEditingId"
                type="button"
                class="rounded border border-line px-3 text-[12px]"
                :class="isMobile ? 'min-h-11 px-4' : 'py-1.5'"
                @click="resetMemForm"
              >{{ t('common.buttons.cancel') }}</button>
              <button
                v-if="memories.length"
                type="button"
                class="ml-auto text-[12px] text-err"
                :class="isMobile ? 'min-h-11 px-2' : ''"
                @click="clearMemories"
              >{{ t('pages.agentStudio.data.memory.clear') }}</button>
            </div>
          </div>
        </div>
        <div
          v-if="showSkeleton"
          class="space-y-2"
          data-testid="agent-data-mem-skeleton"
          aria-hidden="true"
        >
          <div v-for="n in 3" :key="'mem-skel-' + n" class="border-b border-line/60 py-4">
            <div class="h-3.5 w-1/3 bg-elevated animate-pulse" />
            <div class="mt-2 h-10 w-full bg-elevated animate-pulse" />
          </div>
        </div>
        <div v-else-if="!memories.length" class="text-sm text-txt3">{{ t('pages.agentStudio.data.memory.empty') }}</div>
        <div v-else class="space-y-2">
          <div v-for="m in memories" :key="m.id" class="border-b border-line/60 py-4" data-testid="agent-memory-row">
            <div class="flex items-start justify-between gap-2" :class="isMobile ? 'flex-col' : ''">
              <div class="min-w-0">
                <div class="truncate text-[13px] font-medium">{{ m.title }}</div>
                <p class="mt-1 whitespace-pre-wrap text-[12px] text-txt2">{{ m.content }}</p>
                <p v-if="m.updatedBy" class="mt-1 text-[11px] text-txt3">
                  {{ t('pages.agentStudio.data.memory.updatedBy', { user: m.updatedBy }) }}
                </p>
              </div>
              <div class="flex shrink-0 gap-1" :class="isMobile ? 'w-full' : ''">
                <button
                  type="button"
                  class="rounded border border-line px-2 text-[11px]"
                  :class="isMobile ? 'min-h-11 flex-1 px-3' : 'py-1'"
                  @click="editMemory(m)"
                >{{ t('common.buttons.edit') }}</button>
                <button
                  type="button"
                  class="rounded border border-err/30 px-2 text-[11px] text-err disabled:opacity-40"
                  :class="isMobile ? 'min-h-11 flex-1 px-3' : 'py-1'"
                  :disabled="deletingMemId === m.id"
                  data-testid="agent-mem-delete"
                  @click="removeMemory(m.id)"
                >{{ deletingMemId === m.id ? t('common.buttons.deleting') : t('common.buttons.delete') }}</button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- context: any authenticated user -->
      <div v-else-if="sub === 'context'" class="space-y-3" :class="showRefreshProgress ? 'opacity-[0.55]' : ''">
        <p class="text-[12px] text-txt3">{{ t('pages.agentStudio.data.context.hint') }}</p>
        <div
          v-if="showSkeleton"
          class="space-y-2"
          data-testid="agent-data-ctx-skeleton"
          aria-hidden="true"
        >
          <div v-for="n in 3" :key="'ctx-skel-' + n" class="border-b border-line/60 py-3">
            <div class="h-3.5 w-1/2 bg-elevated animate-pulse" />
            <div class="mt-2 h-2.5 w-2/3 bg-elevated animate-pulse" />
          </div>
        </div>
        <div v-else-if="!threads.length" class="text-sm text-txt3">{{ t('pages.agentStudio.data.context.empty') }}</div>
        <div v-else class="space-y-2">
          <div v-for="th in threads" :key="th.id" class="border-b border-line/60">
            <div class="flex items-center gap-2" :class="isMobile ? 'min-h-11 py-2' : 'py-2'">
              <button type="button" class="min-w-0 flex-1 text-left" :class="isMobile ? 'min-h-11' : ''" @click="openThread(th.id)">
                <div class="truncate text-[13px] font-medium">{{ th.title || th.id }}</div>
                <div class="text-[11px] text-txt3">{{ th.kind || 'user' }} · {{ threadCounts[th.id] || 0 }} msgs · {{ fmtTime(th.updatedAt) }}</div>
              </button>
              <button
                type="button"
                class="rounded border border-err/30 px-2 text-[11px] text-err disabled:opacity-40"
                :class="isMobile ? 'min-h-11 min-w-11' : 'py-1'"
                :disabled="deletingThreadId === th.id"
                data-testid="agent-thread-delete"
                :aria-label="deletingThreadId === th.id ? t('common.buttons.deleting') : t('common.buttons.delete')"
                @click="removeThread(th.id)"
              >
                <Icon name="trash" :size="12" />
              </button>
            </div>
            <div v-if="openThreadId === th.id" class="border-t border-line/60 py-3">
              <div v-if="msgLoading" class="text-[12px] text-txt3">{{ t('common.buttons.loading') }}</div>
              <div v-else class="max-h-64 space-y-2 overflow-auto">
                <div v-for="msg in threadMessages" :key="msg.id" class="text-[12px]">
                  <span class="font-medium text-txt2">{{ msg.role }}:</span>
                  <span class="ml-1 whitespace-pre-wrap text-txt">{{ msg.content }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- jobs: list + write for any authenticated user -->
      <div v-else class="space-y-3" :class="showRefreshProgress ? 'opacity-[0.55]' : ''">
        <p class="text-[12px] text-txt3">
          {{ t('pages.agentStudio.data.jobs.hint') }}
        </p>
        <div
          v-if="showSkeleton"
          class="overflow-x-auto border-y border-line/60"
          data-testid="agent-data-jobs-skeleton"
          aria-hidden="true"
        >
          <div v-if="isMobile" class="space-y-3 p-2">
            <div v-for="n in 3" :key="'job-skel-m-' + n" class="border-b border-line/60 py-4">
              <div class="h-3.5 w-1/2 bg-elevated animate-pulse" />
              <div class="mt-2 h-8 w-full bg-elevated animate-pulse" />
            </div>
          </div>
          <table v-else class="w-full text-left text-sm">
            <thead class="text-xs text-txt3">
              <tr>
                <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colName') }}</th>
                <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colSchedule') }}</th>
                <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colEnabled') }}</th>
                <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colDeliver') }}</th>
                <th class="px-2 py-1.5" />
              </tr>
            </thead>
            <tbody>
              <tr v-for="n in 4" :key="'job-skel-' + n" class="border-t border-line">
                <td v-for="c in 5" :key="'jc' + c" class="px-2 py-2"><div class="h-3 w-3/4 bg-elevated animate-pulse" /></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else-if="!jobs.length" class="text-sm text-txt3">{{ t('pages.agentStudio.data.jobs.empty') }}</div>

        <!-- narrow: card stack (no 5-col table) -->
        <div v-else-if="isMobile" class="space-y-3" data-testid="agent-cron-mobile-cards">
          <div
            v-for="job in jobs"
            :key="job.id"
            class="border-b border-line/60 py-4"
            data-testid="agent-cron-card"
          >
            <div class="text-[13px] font-medium text-txt">{{ job.name }}</div>
            <div class="mt-1 text-[12px] text-txt2">{{ job.scheduleKind }}: {{ job.scheduleExpr }}</div>
            <div class="mt-3 space-y-2">
              <label class="flex min-h-11 items-center justify-between gap-3 text-[12px] text-txt2">
                <span>{{ t('pages.agentStudio.data.jobs.colEnabled') }}</span>
                <AppSwitch
                  data-testid="agent-cron-enabled"
                  :model-value="job.enabled"
                  :disabled="jobBusy === job.id"
                  :aria-label="t('pages.agentStudio.data.jobs.colEnabled')"
                  @update:model-value="patchJob(job, { enabled: $event })"
                />
              </label>
              <label class="flex min-h-11 items-center justify-between gap-3 text-[12px] text-txt2">
                <span>{{ t('pages.agentStudio.data.jobs.colDeliver') }}</span>
                <AppSwitch
                  data-testid="agent-cron-deliver"
                  :model-value="job.deliverToChannel"
                  :disabled="jobBusy === job.id"
                  :aria-label="t('pages.agentStudio.data.jobs.colDeliver')"
                  @update:model-value="patchJob(job, { deliverToChannel: $event })"
                />
              </label>
              <button
                type="button"
                class="mt-1 min-h-11 w-full rounded border border-err/30 px-3 text-[12px] text-err disabled:cursor-not-allowed disabled:opacity-40"
                data-testid="agent-cron-delete"
                :disabled="jobBusy === job.id || deletingJobId === job.id"
                @click="removeJob(job.id)"
              >
                {{ deletingJobId === job.id ? t('common.buttons.deleting') : t('common.buttons.delete') }}
              </button>
            </div>
          </div>
        </div>

        <!-- desktop: table -->
        <table v-else class="w-full text-left text-sm" data-testid="agent-cron-desktop-table">
          <thead class="text-xs text-txt3">
            <tr>
              <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colName') }}</th>
              <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colSchedule') }}</th>
              <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colEnabled') }}</th>
              <th class="px-2 py-1.5">{{ t('pages.agentStudio.data.jobs.colDeliver') }}</th>
              <th class="px-2 py-1.5" />
            </tr>
          </thead>
          <tbody>
            <tr v-for="job in jobs" :key="job.id" class="border-t border-line">
              <td class="px-2 py-2">{{ job.name }}</td>
              <td class="px-2 py-2 text-txt2">{{ job.scheduleKind }}: {{ job.scheduleExpr }}</td>
              <td class="px-2 py-2">
                <AppSwitch
                  data-testid="agent-cron-enabled"
                  :model-value="job.enabled"
                  :disabled="jobBusy === job.id"
                  :aria-label="t('pages.agentStudio.data.jobs.colEnabled')"
                  @update:model-value="patchJob(job, { enabled: $event })"
                />
              </td>
              <td class="px-2 py-2">
                <AppSwitch
                  data-testid="agent-cron-deliver"
                  :model-value="job.deliverToChannel"
                  :disabled="jobBusy === job.id"
                  :aria-label="t('pages.agentStudio.data.jobs.colDeliver')"
                  @update:model-value="patchJob(job, { deliverToChannel: $event })"
                />
              </td>
              <td class="px-2 py-2 text-right">
                <button
                  type="button"
                  class="text-[11px] text-err disabled:cursor-not-allowed disabled:opacity-40"
                  data-testid="agent-cron-delete"
                  :disabled="jobBusy === job.id || deletingJobId === job.id"
                  @click="removeJob(job.id)"
                >
                  {{ deletingJobId === job.id ? t('common.buttons.deleting') : t('common.buttons.delete') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

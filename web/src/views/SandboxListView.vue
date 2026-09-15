<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { api, type SandboxView } from '@/lib/api/api'
import { httpStatusOf } from '@/lib/shared/listRequestSeq'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import { sandboxPurposeLabelKey, sandboxSourceTextKey } from '@/lib/agent/sandboxPurposeLabel'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useToast } from '@/lib/composables/useToast'

const SKELETON_ROWS = 6
const SKELETON_CARDS = 4
const NAMED_ENDPOINT_KEYS = ['session', 'ide', 'ssh'] as const

/** Persists across route remounts within the same session. */
let hasInitialLoaded = false

const router = useRouter()
const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()
const rows = ref<SandboxView[]>([])
const loading = ref(false)
const initialLoading = ref(false)
const initialLoadFailed = ref(false)
const loadDenied = ref(false)
const error = ref('')
const now = ref(Date.now())
const showTableLoading = computed(() => loading.value && hasInitialLoaded)
// Delete/cleanup confirmation targets (null = closed).
const destroyTarget = ref<SandboxView | null>(null)
const cleanupOpen = ref(false)
const acting = ref(false)
const copiedId = ref<number | null>(null)
let copiedTimer: number | undefined
let poll: number | undefined
let tick: number | undefined
let requestSeq = 0
let activeLoadingSeq = 0
let inFlight = false

// Detail modal (AppModal); list row data is never overwritten by a failed fetch.
const detailOpen = ref(false)
const detailLoading = ref(false)
const detailError = ref('')
const detailView = ref<SandboxView | null>(null)
const detailListSnapshot = ref<SandboxView | null>(null)
const copiedKey = ref('')
let detailCopiedTimer: number | undefined
let detailSeq = 0

async function load({ showLoading = false }: { showLoading?: boolean } = {}) {
  if (inFlight && !showLoading) return
  inFlight = true
  const localSeq = ++requestSeq
  const isFirstLoad = !hasInitialLoaded

  if (isFirstLoad) {
    initialLoading.value = true
    initialLoadFailed.value = false
    loadDenied.value = false
  } else if (showLoading) {
    activeLoadingSeq = localSeq
    loading.value = true
  }

  try {
    const data = await api.listSandboxes()
    if (localSeq !== requestSeq) return
    rows.value = data
    error.value = ''
    if (initialLoadFailed.value) initialLoadFailed.value = false
    if (loadDenied.value) loadDenied.value = false
  } catch (e: any) {
    if (localSeq !== requestSeq) return
    if (isFirstLoad) {
      const status = httpStatusOf(e)
      loadDenied.value = status === 403
      initialLoadFailed.value = status !== 403
      error.value = String(e?.message || e)
    } else {
      /* keep previous list; surface error banner for actions/poll awareness */
      error.value = String(e?.message || e)
    }
  } finally {
    inFlight = false
    if (isFirstLoad) {
      hasInitialLoaded = true
      initialLoading.value = false
    } else if (showLoading && activeLoadingSeq === localSeq) {
      loading.value = false
    }
  }
}

function shortId(id?: string): string {
  if (!id) return '—'
  return id.length > 10 ? id.slice(0, 8) : id
}

function purposeOf(s: SandboxView): { label: string; cls: string } {
  const label = t(sandboxPurposeLabelKey(s.purpose))
  if (s.purpose === 'run') return { label, cls: 'border-accent/40 text-accent-2' }
  if (s.purpose === 'agent' || s.purpose === 'pm') return { label, cls: 'border-accent/55 text-accent-2 bg-accent/8' }
  return { label, cls: 'border-line text-txt3' }
}

function statusOf(s: SandboxView): { label: string; cls: string } {
  // "creating" takes precedence over "busy": a run sandbox is marked busy while
  // it provisions, but the user needs to see it is still starting up.
  if (s.status === 'pulling') return { label: t('pages.sandboxes.status.pulling'), cls: 'border-warn/30 text-warn' }
  if (s.status === 'creating') return { label: t('pages.sandboxes.status.creating'), cls: 'border-warn/30 text-warn' }
  if (s.busy) return { label: t('pages.sandboxes.status.busy'), cls: 'border-accent/40 text-accent-2' }
  if (s.status === 'error') return { label: t('pages.sandboxes.status.error'), cls: 'border-err/30 text-err' }
  if (s.status === 'stopped') return { label: t('pages.sandboxes.status.stopped'), cls: 'border-line text-txt3' }
  if (s.containerStatus === 'running') return { label: t('pages.sandboxes.status.running'), cls: 'border-ok/30 text-ok' }
  if (s.containerStatus === 'not_found') return { label: t('pages.sandboxes.status.notFound'), cls: 'border-err/30 text-err' }
  return { label: s.status || s.containerStatus, cls: 'border-warn/30 text-warn' }
}

function ttl(s: SandboxView): string {
  if (s.busy) return t('pages.sandboxes.ttl.busy')
  if (!s.destroyAt) return t('common.format.empty')
  const ms = new Date(s.destroyAt).getTime() - now.value
  if (ms <= 0) return t('pages.sandboxes.ttl.soon')
  const m = Math.floor(ms / 60000)
  const sec = Math.floor((ms % 60000) / 1000)
  return m > 0 ? `${m}m ${sec}s` : `${sec}s`
}

function sourceText(s: SandboxView): string {
  if (s.purpose === 'run') {
    const wf = s.workflowName || s.workflowId || t('pages.sandboxes.source.workflow')
    const run = s.runId || '—'
    return s.nodeId ? `${wf} / run ${run} / ${s.nodeId}` : `${wf} / run ${run}`
  }
  const key = sandboxSourceTextKey(s.purpose)
  return key ? t(key) : t('pages.sandboxes.source.chatTest')
}

function proxyPaths(id: number) {
  return [
    { key: 'session', label: t('pages.sandboxes.detail.proxy.session'), value: `/sandbox/${id}` },
    { key: 'ide', label: t('pages.sandboxes.detail.proxy.ide'), value: `/sandbox-bridge/${id}` },
    { key: 'vnc', label: t('pages.sandboxes.detail.proxy.vnc'), value: `/sandbox-vnc/${id}/ws`, preview: true },
  ]
}

function namedEndpoints(endpoints?: Record<string, string>) {
  if (!endpoints) return []
  return NAMED_ENDPOINT_KEYS
    .filter((k) => !!endpoints[k])
    .map((k) => ({ key: k, label: k, value: endpoints[k] }))
}

const detailMetaRows = computed(() => {
  const s = detailView.value
  if (!s) return []
  const rows = [
    { key: 'id', label: t('pages.sandboxes.detail.fields.id'), value: String(s.id), plain: false, error: false },
    { key: 'name', label: t('pages.sandboxes.detail.fields.name'), value: s.name, plain: false, error: false },
    { key: 'profile', label: t('pages.sandboxes.detail.fields.profile'), value: s.profile, plain: false, error: false },
    { key: 'purpose', label: t('pages.sandboxes.detail.fields.purpose'), value: purposeOf(s).label, plain: true, error: false },
    { key: 'status', label: t('pages.sandboxes.detail.fields.status'), value: statusOf(s).label, plain: true, error: false },
    { key: 'ttl', label: t('pages.sandboxes.detail.fields.ttl'), value: ttl(s), plain: true, error: false },
    { key: 'source', label: t('pages.sandboxes.detail.fields.source'), value: sourceText(s), plain: true, error: false },
    { key: 'repoUrl', label: t('pages.sandboxes.detail.fields.repoUrl'), value: s.repoUrl || '—', plain: false, error: false },
    { key: 'createdAt', label: t('pages.sandboxes.detail.fields.createdAt'), value: new Date(s.createdAt).toLocaleString(), plain: true, error: false },
  ]
  if (s.error) {
    rows.push({ key: 'error', label: t('pages.sandboxes.detail.fields.error'), value: s.error, plain: true, error: true })
  }
  return rows
})

const detailProxyRows = computed(() => {
  const id = detailView.value?.id ?? detailListSnapshot.value?.id
  if (id == null) return []
  return proxyPaths(id)
})

const detailEndpointRows = computed(() => namedEndpoints(detailView.value?.endpoints))

async function openDetail(s: SandboxView) {
  detailListSnapshot.value = s
  detailView.value = null
  detailError.value = ''
  detailOpen.value = true
  detailLoading.value = true
  const seq = ++detailSeq
  try {
    const fresh = await api.getSandbox(s.id)
    if (seq !== detailSeq) return
    detailView.value = fresh
  } catch (e: any) {
    if (seq !== detailSeq) return
    detailError.value = String(e?.message || e)
  } finally {
    if (seq === detailSeq) detailLoading.value = false
  }
}

function closeDetail() {
  detailSeq++
  detailOpen.value = false
  detailLoading.value = false
  detailError.value = ''
  detailView.value = null
  detailListSnapshot.value = null
  copiedKey.value = ''
  if (detailCopiedTimer) window.clearTimeout(detailCopiedTimer)
}

function openSandboxVncPreview(id: number) {
  closeDetail()
  router.push({ path: `/sandboxes/${id}/console`, query: { tab: 'novnc' } })
}

async function copyText(key: string, text: string) {
  const ok = await copyToClipboard(text)
  if (!ok) {
    toast.error(t('common.toast.copyFailed'))
    return
  }
  copiedKey.value = key
  if (detailCopiedTimer) window.clearTimeout(detailCopiedTimer)
  detailCopiedTimer = window.setTimeout(() => {
    if (copiedKey.value === key) copiedKey.value = ''
  }, 1200)
}

async function copySandboxId(s: SandboxView) {
  const ok = await copyToClipboard(String(s.id))
  if (!ok) {
    toast.error(t('common.toast.copyFailed'))
    return
  }
  copiedId.value = s.id
  if (copiedTimer) window.clearTimeout(copiedTimer)
  copiedTimer = window.setTimeout(() => {
    if (copiedId.value === s.id) copiedId.value = null
  }, 1500)
}

async function stop(s: SandboxView) {
  try {
    await api.stopSandbox(s.id)
    await load({ showLoading: true })
  } catch (e: any) {
    error.value = String(e?.message || e)
  }
}
async function confirmDestroy() {
  const s = destroyTarget.value
  if (!s) return
  acting.value = true
  try {
    await api.destroySandbox(s.id)
    destroyTarget.value = null
    await load({ showLoading: true })
  } catch (e: any) {
    error.value = String(e?.message || e)
  } finally {
    acting.value = false
  }
}
async function confirmCleanup() {
  acting.value = true
  try {
    await api.cleanupSandboxes()
    cleanupOpen.value = false
    await load({ showLoading: true })
  } catch (e: any) {
    error.value = String(e?.message || e)
  } finally {
    acting.value = false
  }
}

onMounted(() => {
  load({ showLoading: true })
  poll = window.setInterval(() => load(), 5000)
  tick = window.setInterval(() => (now.value = Date.now()), 1000)
})
onBeforeUnmount(() => {
  if (poll) clearInterval(poll)
  if (tick) clearInterval(tick)
  if (copiedTimer) window.clearTimeout(copiedTimer)
  if (detailCopiedTimer) window.clearTimeout(detailCopiedTimer)
})
</script>

<template>
  <!-- plan g1.2: fill-height + single overflow-y-auto scroll exit -->
  <div
    class="flex h-full min-h-0 flex-col"
    data-testid="sandbox-list-panel"
    :aria-busy="loading || initialLoading ? 'true' : 'false'"
  >
    <div class="mb-5 flex shrink-0 flex-col items-start gap-2.5 md:flex-row md:items-end md:justify-between">
      <div class="min-w-0">
        <h2 class="text-lg font-semibold text-txt">{{ t('pages.sandboxes.title') }}</h2>
      </div>
      <AppButton variant="outline" icon="trash" @click="cleanupOpen = true">{{ t('common.buttons.cleanupIdle') }}</AppButton>
    </div>

    <div
      v-if="error && !initialLoading && rows.length && !initialLoadFailed && !loadDenied"
      class="card mb-3 shrink-0 border-err/40 p-3 text-[13px] text-err"
    >{{ t('pages.sandboxes.errorPrefix') }}{{ error }}</div>

    <div
      v-if="showTableLoading"
      class="mb-2 h-[2px] shrink-0 overflow-hidden bg-line"
      data-testid="sandbox-list-thin-progress"
      aria-hidden="true"
    >
      <i class="admin-list-thin-bar bg-accent" />
    </div>

    <!-- Mobile card list — plan g2.1: py-0.5 so list-card-lift hover top border is not clipped -->
    <div
      v-if="isMobile"
      class="min-h-0 flex-1 overflow-y-auto py-0.5"
      :class="{ 'table-loading': showTableLoading }"
    >
      <template v-if="initialLoading">
        <div class="flex flex-col gap-2">
          <div
            v-for="n in SKELETON_CARDS"
            :key="'skel-card-' + n"
            class="rounded-lg border border-line bg-surface p-3"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0 flex-1">
                <div class="h-3.5 w-[75%] rounded bg-elevated animate-pulse" />
                <div class="mt-2 h-2.5 w-[40%] rounded bg-elevated animate-pulse" />
              </div>
              <div class="h-5 w-14 shrink-0 rounded bg-elevated animate-pulse" />
            </div>
            <div class="mt-3 h-2.5 w-[55%] rounded bg-elevated animate-pulse" />
          </div>
        </div>
      </template>
      <div
        v-else-if="loadDenied"
        role="status"
        data-testid="sandbox-list-denied"
        class="card border-warn/40 bg-warn/10 px-5 py-10 text-center"
      >
        <Icon name="lock" :size="22" class="mx-auto mb-3 text-warn" />
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="sandbox-list-retry" @click="load({ showLoading: true })">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>
      <div
        v-else-if="initialLoadFailed"
        role="status"
        data-testid="sandbox-list-failed"
        class="card border-err/40 bg-err/10 px-5 py-10 text-center"
      >
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="sandbox-list-retry" @click="load({ showLoading: true })">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>
      <div v-else-if="!rows.length" class="card px-5 py-10 text-center text-[13px] text-txt3">
        {{ t('pages.sandboxes.empty') }}
      </div>
      <div v-else class="flex flex-col gap-2">
        <div
          v-for="s in rows"
          :key="s.id"
          class="list-card-lift rounded-lg border border-line bg-surface p-3 hover:border-line-strong hover:bg-elevated"
        >
          <div class="flex items-start justify-between gap-2.5">
            <div class="min-w-0 flex-1">
              <div class="flex min-w-0 items-center gap-1.5 font-mono text-[13px] font-medium text-txt">
                <Icon name="terminal" :size="13" class="shrink-0 text-accent-2" />
                <span class="truncate" :title="s.name">{{ s.name }}</span>
              </div>
              <div class="mt-1 text-[12px] text-txt2">{{ s.profile }}</div>
            </div>
            <span class="chip shrink-0" :class="statusOf(s).cls">{{ statusOf(s).label }}</span>
          </div>

          <div class="mt-2.5 flex flex-wrap gap-1.5">
            <span class="chip" :class="purposeOf(s).cls">{{ purposeOf(s).label }}</span>
            <span class="chip border-line text-txt3">TTL {{ ttl(s) }}</span>
          </div>

          <div class="mt-2.5 flex flex-col gap-1 text-[12px]">
            <div class="flex min-w-0 items-baseline gap-2">
              <span class="w-[52px] shrink-0 text-[11px] text-txt3">{{ t('pages.sandboxes.meta.source') }}</span>
              <div class="min-w-0">
                <template v-if="s.purpose === 'run'">
                  <div class="flex flex-col gap-0.5">
                    <RouterLink
                      v-if="s.runId"
                      :to="`/runs/${s.runId}`"
                      class="flex items-center gap-1 text-[12px] text-accent-2 hover:underline"
                    ><Icon name="workflow" :size="12" />{{ s.workflowName || s.workflowId || t('pages.sandboxes.source.workflow') }}</RouterLink>
                    <span v-else class="text-[12px] text-txt2">{{ s.workflowName || t('pages.sandboxes.source.workflow') }}</span>
                    <span class="font-mono text-[11px] text-txt3">
                      run {{ shortId(s.runId) }}<template v-if="s.nodeId"> · {{ s.nodeId }}</template>
                    </span>
                  </div>
                </template>
                <span v-else class="text-[12px] text-txt3">{{ sourceText(s) }}</span>
              </div>
            </div>
            <div class="flex min-w-0 items-baseline gap-2">
              <span class="w-[52px] shrink-0 text-[11px] text-txt3">{{ t('pages.sandboxes.meta.created') }}</span>
              <span class="text-[12px] text-txt3">{{ new Date(s.createdAt).toLocaleString() }}</span>
            </div>
          </div>

          <div class="mt-3 flex flex-wrap gap-1.5 border-t border-line pt-2.5">
            <button
              type="button"
              class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong"
              @click="copySandboxId(s)"
            >
              <Icon name="copy" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ copiedId === s.id ? t('pages.sandboxes.copied') : t('pages.sandboxes.copyId') }}
            </button>
            <button
              type="button"
              class="btn-detail-hint rounded border px-2 py-1 text-[11px]"
              @click="openDetail(s)"
            ><Icon name="doc" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ t('common.buttons.details') }}</button>
            <button
              type="button"
              class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong"
              :disabled="s.containerStatus !== 'running'"
              :class="{ 'opacity-40': s.containerStatus !== 'running' }"
              @click="router.push(`/sandboxes/${s.id}/console`)"
            ><Icon name="terminal" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ t('common.buttons.console') }}</button>
            <button
              type="button"
              class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong disabled:opacity-40"
              :disabled="s.busy || s.status === 'stopped'"
              @click="stop(s)"
            >{{ t('common.buttons.stop') }}</button>
            <button
              type="button"
              class="rounded border border-err/30 px-2 py-1 text-[11px] text-err hover:bg-err/10 disabled:opacity-40"
              :disabled="s.busy"
              @click="destroyTarget = s"
            ><Icon name="trash" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ t('pages.sandboxes.destroy') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Desktop table -->
    <div
      v-else
      class="card flex min-h-0 flex-1 flex-col overflow-hidden"
      :class="{ 'table-loading': showTableLoading }"
    >
      <div class="scroll-area min-h-0 flex-1 overflow-auto">
        <table class="w-full min-w-[920px] text-left text-[13px]">
          <thead class="border-b border-line text-[11px] uppercase tracking-wider text-txt3">
            <tr>
              <th class="px-4 py-2.5 font-medium">{{ t('common.table.sandbox') }}</th>
              <th class="px-4 py-2.5 font-medium">{{ t('common.table.agent') }}</th>
              <th class="px-4 py-2.5 font-medium">{{ t('common.table.purpose') }}</th>
              <th class="px-4 py-2.5 font-medium">{{ t('common.table.source') }}</th>
              <th class="px-4 py-2.5 font-medium">{{ t('common.table.status') }}</th>
              <th class="px-4 py-2.5 font-medium">{{ t('common.table.recycleCountdown') }}</th>
              <th class="px-4 py-2.5 font-medium">{{ t('common.table.createdAt') }}</th>
              <th class="px-4 py-2.5 text-right font-medium">{{ t('common.table.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-if="initialLoading">
              <tr v-for="n in SKELETON_ROWS" :key="'skel-' + n" class="border-b border-line/60 last:border-0">
                <td class="px-4 py-2.5">
                  <div class="h-3.5 w-[85%] rounded bg-elevated animate-pulse" />
                </td>
                <td class="px-4 py-2.5">
                  <div class="h-3 w-[60%] rounded bg-elevated animate-pulse" />
                </td>
                <td class="px-4 py-2.5">
                  <div class="h-3 w-12 rounded bg-elevated animate-pulse" />
                </td>
                <td class="px-4 py-2.5">
                  <div class="h-3 w-[70%] rounded bg-elevated animate-pulse" />
                </td>
                <td class="px-4 py-2.5">
                  <div class="h-3 w-14 rounded bg-elevated animate-pulse" />
                </td>
                <td class="px-4 py-2.5">
                  <div class="h-3 w-10 rounded bg-elevated animate-pulse" />
                </td>
                <td class="px-4 py-2.5">
                  <div class="h-3 w-[72px] rounded bg-elevated animate-pulse" />
                </td>
                <td class="px-4 py-2.5">
                  <div class="ml-auto h-3 w-20 rounded bg-elevated animate-pulse" />
                </td>
              </tr>
            </template>
            <tr v-else-if="loadDenied">
              <td colspan="8" class="px-4 py-10 text-center">
                <div role="status" data-testid="sandbox-list-denied" class="rounded-lg border border-warn/40 bg-warn/10 px-5 py-8">
                  <Icon name="lock" :size="22" class="mx-auto mb-3 text-warn" />
                  <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h3>
                  <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
                  <AppButton class="mt-4" variant="outline" data-testid="sandbox-list-retry" @click="load({ showLoading: true })">
                    {{ t('common.buttons.retry') }}
                  </AppButton>
                </div>
              </td>
            </tr>
            <tr v-else-if="initialLoadFailed">
              <td colspan="8" class="px-4 py-10 text-center">
                <div role="status" data-testid="sandbox-list-failed" class="rounded-lg border border-err/40 bg-err/10 px-5 py-8">
                  <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
                  <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
                  <AppButton class="mt-4" variant="outline" data-testid="sandbox-list-retry" @click="load({ showLoading: true })">
                    {{ t('common.buttons.retry') }}
                  </AppButton>
                </div>
              </td>
            </tr>
            <tr v-else-if="!rows.length">
              <td colspan="8" class="px-4 py-10 text-center text-sm text-txt3">
                {{ t('pages.sandboxes.empty') }}
              </td>
            </tr>
            <template v-else>
              <tr v-for="s in rows" :key="s.id" class="border-b border-line/60 last:border-0 hover:bg-elevated/40">
                <td class="px-4 py-2.5">
                  <div class="flex items-center gap-1.5 font-mono text-[12px] text-txt2">
                    <Icon name="terminal" :size="13" class="text-accent-2" />{{ s.name }}
                  </div>
                </td>
                <td class="px-4 py-2.5 text-txt2">{{ s.profile }}</td>
                <td class="px-4 py-2.5"><span class="chip" :class="purposeOf(s).cls">{{ purposeOf(s).label }}</span></td>
                <td class="px-4 py-2.5">
                  <template v-if="s.purpose === 'run'">
                    <div class="flex flex-col gap-0.5">
                      <RouterLink
                        v-if="s.runId"
                        :to="`/runs/${s.runId}`"
                        class="flex items-center gap-1 text-[12px] text-accent-2 hover:underline"
                      ><Icon name="workflow" :size="12" />{{ s.workflowName || s.workflowId || t('pages.sandboxes.source.workflow') }}</RouterLink>
                      <span v-else class="text-[12px] text-txt2">{{ s.workflowName || t('pages.sandboxes.source.workflow') }}</span>
                      <span class="font-mono text-[11px] text-txt3">
                        run {{ shortId(s.runId) }}<template v-if="s.nodeId"> · {{ s.nodeId }}</template>
                      </span>
                    </div>
                  </template>
                  <span v-else class="text-[12px] text-txt3">{{ sourceText(s) }}</span>
                </td>
                <td class="px-4 py-2.5"><span class="chip" :class="statusOf(s).cls">{{ statusOf(s).label }}</span></td>
                <td class="px-4 py-2.5 font-mono text-[12px] text-txt3">{{ ttl(s) }}</td>
                <td class="px-4 py-2.5 text-[12px] text-txt3">{{ new Date(s.createdAt).toLocaleString() }}</td>
                <td class="px-4 py-2.5">
                  <div class="flex items-center justify-end gap-1.5">
                    <button
                      type="button"
                      class="btn-detail-hint rounded border px-2 py-1 text-[11px]"
                      @click="openDetail(s)"
                    ><Icon name="doc" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ t('common.buttons.details') }}</button>
                    <button
                      class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong"
                      :disabled="s.containerStatus !== 'running'"
                      :class="{ 'opacity-40': s.containerStatus !== 'running' }"
                      @click="router.push(`/sandboxes/${s.id}/console`)"
                    ><Icon name="terminal" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ t('common.buttons.console') }}</button>
                    <button
                      class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong disabled:opacity-40"
                      :disabled="s.busy || s.status === 'stopped'"
                      @click="stop(s)"
                    >{{ t('common.buttons.stop') }}</button>
                    <button
                      class="rounded border border-err/30 px-2 py-1 text-[11px] text-err hover:bg-err/10 disabled:opacity-40"
                      :disabled="s.busy"
                      @click="destroyTarget = s"
                    ><Icon name="trash" :size="12" /></button>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Sandbox detail (metadata / proxy URLs / named endpoints) -->
    <AppModal :open="detailOpen" :title="t('pages.sandboxes.detail.title')" :width="560" @close="closeDetail">
      <div v-if="detailLoading" class="flex flex-col gap-2.5 py-1" aria-busy="true">
        <div class="h-3 w-[40%] rounded bg-elevated animate-pulse" />
        <div class="h-[72px] w-full rounded bg-elevated animate-pulse" />
        <div class="mt-2 h-3 w-[32%] rounded bg-elevated animate-pulse" />
        <div class="h-24 w-full rounded bg-elevated animate-pulse" />
        <div class="mt-2 h-3 w-[36%] rounded bg-elevated animate-pulse" />
        <div class="h-[72px] w-full rounded bg-elevated animate-pulse" />
      </div>
      <div v-else-if="detailError" class="space-y-3">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          <div>
            <div class="font-medium">{{ t('pages.sandboxes.detail.loadFailed') }}</div>
            <div class="mt-0.5 text-err/90">{{ detailError }}</div>
          </div>
        </div>
      </div>
      <div v-else-if="detailView" class="space-y-5">
        <section>
          <h3 class="mb-2.5 text-[12px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.sandboxes.detail.sectionMeta') }}</h3>
          <div class="rounded-lg border border-line bg-base">
            <div
              v-for="row in detailMetaRows"
              :key="row.key"
              class="grid grid-cols-[110px_1fr] gap-2 border-b border-line px-3 py-2 text-[12px] last:border-0"
            >
              <div class="pt-px text-txt3">{{ row.label }}</div>
              <div
                class="min-w-0 break-all leading-snug"
                :class="row.error ? 'text-err' : row.plain ? 'text-txt' : 'font-mono text-txt'"
              >{{ row.value }}</div>
            </div>
          </div>
        </section>

        <section>
          <h3 class="mb-1 text-[12px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.sandboxes.detail.sectionProxy') }}</h3>
          <p class="mb-2.5 text-[11px] leading-snug text-txt3">{{ t('pages.sandboxes.detail.sectionProxyHint') }}</p>
          <div class="rounded-lg border border-line bg-base">
            <div
              v-for="row in detailProxyRows"
              :key="row.key"
              class="grid grid-cols-[110px_1fr_auto] items-start gap-2 border-b border-line px-3 py-2 text-[12px] last:border-0"
            >
              <div class="pt-px text-txt3">{{ row.label }}</div>
              <div class="min-w-0 break-all font-mono leading-snug text-txt">{{ row.value }}</div>
              <span class="flex shrink-0 items-center gap-1.5 self-center">
                <button
                  type="button"
                  class="rounded-md border border-line px-2 py-0.5 text-[11px] text-txt2 hover:border-line-strong hover:text-txt"
                  :class="{ 'border-ok/40 text-ok': copiedKey === 'proxy-' + row.key }"
                  @click="copyText('proxy-' + row.key, row.value)"
                >{{ copiedKey === 'proxy-' + row.key ? t('pages.sandboxes.detail.copied') : t('pages.sandboxes.detail.copy') }}</button>
                <button
                  v-if="row.preview"
                  type="button"
                  data-testid="sandbox-vnc-open-preview"
                  class="rounded-md border border-accent bg-accent px-2 py-0.5 text-[11px] text-white transition hover:bg-accent-hover"
                  @click="openSandboxVncPreview(detailView.id)"
                >{{ t('pages.sandboxes.detail.proxy.openPreview') }}</button>
              </span>
            </div>
          </div>
        </section>

        <section>
          <h3 class="mb-1 text-[12px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.sandboxes.detail.sectionEndpoints') }}</h3>
          <div
            data-testid="sandbox-endpoints-notice"
            class="rounded-lg mb-2.5 border border-[rgb(var(--c-info)/0.45)] bg-[rgb(var(--c-info)/0.14)] px-3 py-2 text-[12px] leading-snug text-txt"
          >{{ t('pages.sandboxes.detail.endpointsNotice') }}</div>
          <p class="mb-2.5 text-[11px] leading-snug text-txt3">{{ t('pages.sandboxes.detail.sectionEndpointsHint') }}</p>
          <div
            v-if="!detailEndpointRows.length"
            class="rounded-lg border border-dashed border-line-strong bg-base px-3 py-4 text-center text-[12px] text-txt3"
          >{{ t('pages.sandboxes.detail.endpointsEmpty') }}</div>
          <div v-else class="rounded-lg border border-line bg-base">
            <div
              v-for="row in detailEndpointRows"
              :key="row.key"
              class="grid grid-cols-[110px_1fr_auto] items-start gap-2 border-b border-line px-3 py-2 text-[12px] last:border-0"
            >
              <div class="pt-px text-txt3">{{ row.label }}</div>
              <div class="min-w-0 break-all font-mono leading-snug text-txt">{{ row.value }}</div>
              <button
                type="button"
                class="rounded-md shrink-0 self-center border border-line px-2 py-0.5 text-[11px] text-txt2 hover:border-line-strong hover:text-txt"
                :class="{ 'border-ok/40 text-ok': copiedKey === 'ep-' + row.key }"
                @click="copyText('ep-' + row.key, row.value)"
              >{{ copiedKey === 'ep-' + row.key ? t('pages.sandboxes.detail.copied') : t('pages.sandboxes.detail.copy') }}</button>
            </div>
          </div>
        </section>
      </div>
      <template #footer>
        <AppButton variant="ghost" @click="closeDetail">{{ t('pages.sandboxes.detail.close') }}</AppButton>
      </template>
    </AppModal>

    <!-- destroy one sandbox -->
    <AppModal :open="!!destroyTarget" :title="t('pages.sandboxes.destroyTitle')" :width="440" @close="!acting && (destroyTarget = null)">
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.sandboxes.destroyWarning') }}
        </div>
        <p>{{ t('pages.sandboxes.destroyConfirm', { name: destroyTarget?.name }) }}</p>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="acting" @click="destroyTarget = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" :disabled="acting" @click="confirmDestroy">{{ acting ? t('common.buttons.destroying') : t('common.buttons.confirmDestroy') }}</AppButton>
      </template>
    </AppModal>

    <AppModal :open="cleanupOpen" :title="t('pages.sandboxes.cleanupTitle')" :width="440" @close="!acting && (cleanupOpen = false)">
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.sandboxes.cleanupWarning') }}
        </div>
        <p>{{ t('pages.sandboxes.cleanupConfirm') }}</p>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="acting" @click="cleanupOpen = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" :disabled="acting" @click="confirmCleanup">{{ acting ? t('common.buttons.cleaning') : t('common.buttons.confirmCleanup') }}</AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.table-loading {
  opacity: 0.55;
}
.btn-detail-hint {
  border-color: rgb(var(--c-accent-2) / 0.45);
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent-2) / 0.08);
}
.btn-detail-hint:hover {
  border-color: rgb(var(--c-accent-2) / 0.7);
  background: rgb(var(--c-accent-2) / 0.14);
}
</style>

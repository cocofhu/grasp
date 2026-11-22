<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type PreviewPort } from '@/lib/api/api'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import NovncPreviewPanel from './NovncPreviewPanel.vue'
import DirectPreviewLauncher from './DirectPreviewLauncher.vue'
import AppPreviewShareButton from './AppPreviewShareButton.vue'
import ExternalUrlPreviewFrame from './ExternalUrlPreviewFrame.vue'
import PreviewFeedbackChat from './PreviewFeedbackChat.vue'
import RefreshStrip from './RefreshStrip.vue'
import HardLoadLayer from './HardLoadLayer.vue'
import { isAbortError } from '@/lib/run/liveLogRehydrate'

const props = withDefaults(
  defineProps<{
    runId: string
    nodeId: string
    compact?: boolean
    fill?: boolean
    /** When false, hide PreviewFeedbackChat (review ReAct is the primary dialogue). */
    showFeedback?: boolean
    /** Standalone conversations provide their own sandbox and active preview (null when none). */
    registeredPreview?: PreviewPort | null
    soloSessionId?: string
    residentSandboxId?: number | null
  }>(),
  { compact: false, fill: false, showFeedback: true },
)

const emit = defineEmits<{
  (e: 'pick', payload: AppPreviewPickPayload): void
  (e: 'staged-pick', payload: AppPreviewPickPayload | null): void
  (e: 'issues-changed'): void
}>()

const { t } = useI18n()
const preview = ref<PreviewPort | null>(null)
const loading = ref(false)
const loadError = ref<string | null>(null)
const pickedSelector = ref('')
let loadGen = 0
let loadAbort: AbortController | null = null

/** The active preview is an external URL shown over the desktop. */
const urlTabActive = computed(() => preview.value?.kind === 'url')
/** Port the desktop shows; null for an external URL. */
const activePort = computed(() => (preview.value && !urlTabActive.value ? preview.value.port : null))
const activeDirectUrl = computed(() =>
  activePort.value && preview.value?.mode === 'direct' ? (preview.value.directUrl || '').trim() : '',
)
/** The node's sandbox: its desktop is the one VNC screen for the active preview. */
const sandboxId = ref<number | null>(null)
/** creating/pulling keep the empty poll; absent and ready stop it. */
const sandboxPhase = ref<'creating' | 'pulling' | 'ready' | 'absent' | null>(null)
/** Something to show: the Agent's desktop, or the active preview. */
const hasScreen = computed(() => sandboxId.value != null || preview.value != null)
const previewLabel = computed(() => {
  const p = preview.value
  if (!p) return ''
  const label = (p.label || '').trim()
  if (label) return label
  if (urlTabActive.value) {
    const url = (p.url || '').trim()
    try {
      const parsed = new URL(url)
      return parsed.host + parsed.pathname
    } catch {
      return url
    }
  }
  return String(p.port)
})

function sandboxNumericId(id: unknown): number | null {
  const n = typeof id === 'number' ? id : Number(id)
  return Number.isFinite(n) && n > 0 ? n : null
}

function applySandboxView(sbx: { id?: unknown; status?: unknown } | null) {
  if (!sbx) {
    sandboxId.value = null
    sandboxPhase.value = 'absent'
    return
  }
  const status = String(sbx.status || '').toLowerCase()
  const id = sandboxNumericId(sbx.id)
  if (status === 'creating' || status === 'pulling') {
    sandboxPhase.value = status
    sandboxId.value = id
    return
  }
  sandboxId.value = id
  sandboxPhase.value = id ? 'ready' : 'absent'
}

async function loadSandbox(gen: number) {
  if (
    sandboxId.value != null &&
    sandboxPhase.value !== 'creating' &&
    sandboxPhase.value !== 'pulling'
  ) {
    return
  }
  const runId = props.runId
  const nodeId = props.nodeId
  const sbx = await api.getRunNodeSandbox(runId, nodeId)
  if (gen !== loadGen || props.runId !== runId || props.nodeId !== nodeId) return
  applySandboxView(sbx)
}

function onPick(payload: AppPreviewPickPayload) {
  pickedSelector.value = payload.selector
  emit('pick', payload)
}

function onStagedPick(payload: AppPreviewPickPayload | null) {
  if (payload) pickedSelector.value = payload.selector
  emit('staged-pick', payload)
}

async function loadPreview(opts?: { silent?: boolean }) {
  loadAbort?.abort()
  const gen = ++loadGen
  loadAbort = new AbortController()
  const silent = !!opts?.silent
  // Visible loading only on first load / manual retry — empty poll must stay silent.
  if (!silent) {
    loading.value = true
    loadError.value = null
  }
  try {
    const [r] = await Promise.all([
      api.nodePreview(props.runId, props.nodeId, { signal: loadAbort.signal }),
      loadSandbox(gen),
    ])
    if (gen !== loadGen) return
    preview.value = r.preview ?? null
    if (silent) loadError.value = null
  } catch (e: any) {
    if (gen !== loadGen || isAbortError(e) || loadAbort.signal.aborted) return
    // Silent empty polls must not surface errors or flash loading layers.
    if (silent) return
    loadError.value = t('pages.appPreview.loadFailed')
  } finally {
    if (gen === loadGen && !silent) loading.value = false
  }
}

function retryLoadPreview() {
  return loadPreview()
}

const EMPTY_POLL_MS = 2500
let emptyPoll: ReturnType<typeof setInterval> | null = null

function stopEmptyPoll() {
  if (!emptyPoll) return
  clearInterval(emptyPoll)
  emptyPoll = null
}

watch(
  () => [props.runId, props.nodeId],
  () => {
    if (props.registeredPreview !== undefined) return
    stopEmptyPoll()
    sandboxId.value = null
    sandboxPhase.value = null
    void loadPreview()
  },
  { immediate: true },
)

watch(
  () => [props.registeredPreview, props.residentSandboxId] as const,
  ([registered, id]) => {
    if (registered === undefined) return
    applySandboxView(id ? { id } : null)
    preview.value = registered
  },
  { immediate: true },
)

watch(
  () => ({
    provisioning: sandboxPhase.value === 'creating' || sandboxPhase.value === 'pulling',
    loading: loading.value,
    err: loadError.value,
  }),
  ({ provisioning, loading: busy, err }) => {
    if (provisioning && !busy && !err) {
      if (!emptyPoll) emptyPoll = setInterval(() => loadPreview({ silent: true }), EMPTY_POLL_MS)
      return
    }
    stopEmptyPoll()
  },
)

onUnmounted(() => {
  stopEmptyPoll()
  loadAbort?.abort()
  loadAbort = null
  loadGen++
})

function issueDirectTicket() {
  return props.soloSessionId
    ? api.soloEmbedTicket(props.soloSessionId)
    : api.embedTicket(props.runId, props.nodeId)
}
</script>

<template>
  <div
    data-testid="app-preview"
    class="relative flex min-h-0 flex-col"
    :class="fill ? 'h-full flex-1' : ''"
    :aria-busy="loading ? 'true' : 'false'"
  >
    <RefreshStrip v-if="loading && hasScreen" />
    <HardLoadLayer
      v-else-if="loading && !hasScreen && !loadError"
      :overlay="false"
      :stuck-after-ms="10_000"
      :stage="t('pages.appPreview.loading')"
      @retry="retryLoadPreview"
    />
    <div
      v-if="loadError"
      class="rounded-md border border-err/30 bg-err/10 p-4 text-xs text-err"
      role="alert"
      data-testid="app-preview-load-error"
    >
      <p>{{ loadError }}</p>
      <button
        type="button"
        class="rounded-lg mt-2 inline-flex min-h-11 items-center border border-line px-3 text-[12px] text-txt"
        @click="retryLoadPreview"
      >
        {{ t('common.chatImage.retry') }}
      </button>
    </div>
    <div
      v-else-if="!loading && !hasScreen"
      class="rounded-md border border-line bg-elevated p-4 text-xs text-txt3"
      data-testid="app-preview-empty"
    >
      {{ t('pages.appPreview.noPorts') }}
    </div>
    <template v-if="hasScreen">
      <div
        class="flex min-h-0 flex-col overflow-hidden rounded-md border border-line bg-surface"
        :class="fill ? 'flex-1' : compact ? 'h-[280px]' : 'h-[420px]'"
      >
        <ExternalUrlPreviewFrame
          v-if="preview && urlTabActive"
          :url="(preview.url || '').trim()"
          :title="previewLabel"
        />
        <NovncPreviewPanel
          v-if="sandboxId != null"
          v-show="!urlTabActive"
          :sandbox-id="sandboxId"
          :target-port="activePort ?? undefined"
          fill
          :compact="compact"
          @pick="onPick"
          @staged-pick="onStagedPick"
        >
          <template v-if="activePort" #toolbar-extra>
            <DirectPreviewLauncher
              v-if="activeDirectUrl"
              :key="activeDirectUrl"
              compact
              :direct-url="activeDirectUrl"
              :issue-ticket="issueDirectTicket"
            />
            <AppPreviewShareButton
              v-if="activeDirectUrl"
              :key="`${runId}:${nodeId}:${activePort}`"
              :scope="{ runId, nodeId, soloSessionId, port: activePort }"
            />
          </template>
        </NovncPreviewPanel>
        <div
          v-else-if="!urlTabActive"
          class="flex h-full items-center justify-center p-4 text-center text-xs text-txt3"
          data-testid="app-preview-no-sandbox"
        >
          {{ t('pages.appPreview.sandboxRecycled') }}
        </div>
      </div>
      <PreviewFeedbackChat
        v-if="!compact && showFeedback && preview"
        :run-id="runId"
        :node-id="nodeId"
        :port="activePort ?? 0"
        :selector="pickedSelector"
        copy-variant="review"
        :compact="fill"
        @clear-selector="pickedSelector = ''"
        @issues-changed="emit('issues-changed')"
      />
    </template>
  </div>
</template>

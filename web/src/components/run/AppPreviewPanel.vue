<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type PreviewPort } from '@/lib/api/api'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { isUrlPreview, previewTabKey, previewTabLabel } from '@/lib/shared/previewTabKey'
import NovncPreviewPanel from './NovncPreviewPanel.vue'
import DirectPreviewFrame from './DirectPreviewFrame.vue'
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
  }>(),
  { compact: false, fill: false, showFeedback: true },
)

const emit = defineEmits<{
  (e: 'pick', payload: AppPreviewPickPayload): void
  (e: 'staged-pick', payload: AppPreviewPickPayload | null): void
  (e: 'issues-changed'): void
}>()

const { t } = useI18n()
const ports = ref<PreviewPort[]>([])
const loading = ref(false)
const loadError = ref<string | null>(null)
const activeKey = ref<string | null>(null)
const pickedSelector = ref('')
let portsGen = 0
let portsAbort: AbortController | null = null

function isDirectPort(p: PreviewPort): boolean {
  return p.mode === 'direct' && !!(p.directUrl || '').trim()
}

/** Window iframe stays on the approval origin so session cookies are first-party. */
function previewEmbedUrl(p: PreviewPort): string {
  const proxy = (p.proxyUrl || '').trim()
  if (proxy) return proxy
  return `/preview/${props.runId}/${props.nodeId}/${p.port}/`
}

function isVncPort(p: PreviewPort): boolean {
  return !isUrlPreview(p) && !isDirectPort(p)
}

const activePort = ref<number | null>(null)
const vncPorts = computed(() => ports.value.filter(isVncPort))
const activeVnc = computed(() => vncPorts.value.find((p) => previewTabKey(p) === activeKey.value) || null)
/** Port the single noVNC socket is bound to; changing it reconnects. */
const vncConnPort = ref<number | null>(null)
/** Last VNC port shown; kept while a direct/url tab is active so we do not navigate away. */
const vncTargetPort = ref<number | null>(null)

function syncActivePort() {
  const current = ports.value.find((p) => previewTabKey(p) === activeKey.value)
  activePort.value = current && !isUrlPreview(current) ? current.port : null
  const vnc = vncPorts.value
  if (!vnc.length) {
    vncConnPort.value = null
    vncTargetPort.value = null
    return
  }
  if (activeVnc.value) vncTargetPort.value = activeVnc.value.port
  else if (!vnc.some((p) => p.port === vncTargetPort.value)) vncTargetPort.value = vnc[0].port
  if (!vnc.some((p) => p.port === vncConnPort.value)) vncConnPort.value = vncTargetPort.value
}

function onPick(payload: AppPreviewPickPayload) {
  pickedSelector.value = payload.selector
  emit('pick', payload)
}

function onStagedPick(payload: AppPreviewPickPayload | null) {
  if (payload) pickedSelector.value = payload.selector
  emit('staged-pick', payload)
}

async function loadPorts(opts?: { silent?: boolean }) {
  portsAbort?.abort()
  const gen = ++portsGen
  portsAbort = new AbortController()
  const silent = !!opts?.silent
  // Visible loading only on first load / manual retry — empty poll must stay silent.
  if (!silent) {
    loading.value = true
    loadError.value = null
  }
  try {
    const r = await api.nodePreviews(props.runId, props.nodeId, { signal: portsAbort.signal })
    if (gen !== portsGen) return
    ports.value = r.ports || []
    if (ports.value.length && activeKey.value == null) {
      activeKey.value = previewTabKey(ports.value[0])
    } else if (
      activeKey.value != null &&
      !ports.value.some((p) => previewTabKey(p) === activeKey.value)
    ) {
      activeKey.value = ports.value[0] ? previewTabKey(ports.value[0]) : null
    }
    syncActivePort()
    if (silent) loadError.value = null
  } catch (e: any) {
    if (gen !== portsGen || isAbortError(e) || portsAbort.signal.aborted) return
    // Silent empty polls must not surface errors or flash loading layers.
    if (silent) return
    loadError.value = t('pages.appPreview.loadFailed')
    if (!ports.value.length) ports.value = []
  } finally {
    if (gen === portsGen && !silent) loading.value = false
  }
}

function retryLoadPorts() {
  return loadPorts()
}

watch(() => [props.runId, props.nodeId], () => loadPorts(), { immediate: true })

const EMPTY_POLL_MS = 2500
let emptyPoll: ReturnType<typeof setInterval> | null = null

function stopEmptyPoll() {
  if (!emptyPoll) return
  clearInterval(emptyPoll)
  emptyPoll = null
}

watch(
  () => ({ empty: !ports.value.length, loading: loading.value, err: loadError.value }),
  ({ empty, loading: busy, err }) => {
    if (empty && !busy && !err) {
      if (!emptyPoll) emptyPoll = setInterval(() => loadPorts({ silent: true }), EMPTY_POLL_MS)
      return
    }
    stopEmptyPoll()
  },
)

onUnmounted(() => {
  stopEmptyPoll()
  portsAbort?.abort()
  portsAbort = null
  portsGen++
})

function selectPreview(key: string) {
  activeKey.value = key
  syncActivePort()
}
</script>

<template>
  <div
    data-testid="app-preview"
    class="relative flex min-h-0 flex-col"
    :class="fill ? 'h-full flex-1' : ''"
    :aria-busy="loading ? 'true' : 'false'"
  >
    <RefreshStrip v-if="loading && ports.length" />
    <HardLoadLayer
      v-else-if="loading && !ports.length && !loadError"
      :overlay="false"
      :stuck-after-ms="10_000"
      :stage="t('pages.appPreview.loading')"
      @retry="retryLoadPorts"
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
        @click="retryLoadPorts"
      >
        {{ t('common.chatImage.retry') }}
      </button>
    </div>
    <div
      v-else-if="!loading && !ports.length"
      class="rounded-md border border-line bg-elevated p-4 text-xs text-txt3"
      data-testid="app-preview-empty"
    >
      {{ t('pages.appPreview.noPorts') }}
    </div>
    <template v-if="ports.length">
      <div v-if="ports.length > 1" class="mb-2 flex flex-wrap gap-1">
        <button
          v-for="p in ports"
          :key="previewTabKey(p)"
          type="button"
          class="rounded-md px-2.5 py-1 text-xs font-medium transition"
          :class="
            previewTabKey(p) === activeKey
              ? 'bg-accent/15 text-accent'
              : 'border border-line text-txt2 hover:bg-elevated hover:text-txt'
          "
          @click="selectPreview(previewTabKey(p))"
        >
          {{ previewTabLabel(p) }}
        </button>
      </div>
      <div
        class="flex min-h-0 flex-col overflow-hidden rounded-md border border-line bg-surface"
        :class="fill ? 'flex-1' : compact ? 'h-[280px]' : 'h-[420px]'"
      >
        <ExternalUrlPreviewFrame
          v-for="p in ports.filter((x) => isUrlPreview(x))"
          v-show="activeKey === previewTabKey(p)"
          :key="`url-${previewTabKey(p)}`"
          :url="(p.url || '').trim()"
          :title="previewTabLabel(p)"
        />
        <DirectPreviewFrame
          v-for="p in ports.filter((x) => isDirectPort(x))"
          v-show="activeKey === previewTabKey(p)"
          :key="`direct-${previewTabKey(p)}`"
          :direct-url="p.directUrl || ''"
          :embed-url="previewEmbedUrl(p)"
          :title="previewTabLabel(p)"
          @pick="onPick"
          @staged-pick="onStagedPick"
        />
        <NovncPreviewPanel
          v-if="vncConnPort != null"
          v-show="!!activeVnc"
          :run-id="runId"
          :node-id="nodeId"
          :port="vncConnPort"
          :target-port="vncTargetPort ?? undefined"
          fill
          :compact="compact"
          @pick="onPick"
          @staged-pick="onStagedPick"
        />
      </div>
      <PreviewFeedbackChat
        v-if="!compact && showFeedback"
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

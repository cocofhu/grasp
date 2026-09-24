<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import NovncPreviewPanel from '@/components/run/NovncPreviewPanel.vue'
import DirectPreviewFrame from '@/components/run/DirectPreviewFrame.vue'
import ExternalUrlPreviewFrame from '@/components/run/ExternalUrlPreviewFrame.vue'
import {
  publicGateApi,
  publicPreviewVncWsUrl,
  type PublicPreviewPort,
} from '@/lib/inbox/gateShareLink'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { isAbortError } from '@/lib/run/liveLogRehydrate'

const props = withDefaults(
  defineProps<{
    token: string
    ports: PublicPreviewPort[]
    /** Share link still active (remote allowed). */
    active?: boolean
    mobile?: boolean
    fill?: boolean
  }>(),
  { active: true, mobile: false, fill: true },
)

const emit = defineEmits<{
  (e: 'pick', payload: AppPreviewPickPayload): void
  (e: 'staged-pick', payload: AppPreviewPickPayload | null): void
}>()

const { t } = useI18n()

const activeKey = ref<string | null>(null)
const activePort = ref<number | null>(null)
const vncWsUrl = ref('')
const ticketBusy = ref(false)
const ticketError = ref('')
const linkInactive = ref(false)
let ticketAbort: AbortController | null = null
let ticketGen = 0

const sortedPorts = computed(() =>
  props.ports.filter((p) => p.port > 0 || (p.kind || '') === 'url' || !!(p.url || '').trim()),
)

function isUrlPreview(p: PublicPreviewPort): boolean {
  return p.kind === 'url' || !!(p.url || '').trim()
}

function publicTabKey(p: PublicPreviewPort): string {
  if (isUrlPreview(p)) return `url:${(p.url || p.directUrl || '').trim()}`
  return `port:${p.port}`
}

function isDirectPort(p: PublicPreviewPort): boolean {
  if (isUrlPreview(p)) return true
  return !!(p.directUrl || '').trim()
}

function tabLabel(p: PublicPreviewPort): string {
  const label = (p.label || '').trim()
  if (label) return label
  if (isUrlPreview(p)) {
    const u = (p.url || p.directUrl || '').trim()
    try {
      const parsed = new URL(u)
      return parsed.host + parsed.pathname
    } catch {
      return u
    }
  }
  return String(p.port)
}

const activeMeta = computed(
  () => sortedPorts.value.find((p) => publicTabKey(p) === activeKey.value) || null,
)
const activeIsUrl = computed(() => (activeMeta.value ? isUrlPreview(activeMeta.value) : false))
const activeIsDirect = computed(() => (activeMeta.value ? isDirectPort(activeMeta.value) : false))
const activeDirectUrl = computed(() => {
  if (!activeMeta.value) return ''
  if (isUrlPreview(activeMeta.value)) {
    return (activeMeta.value.url || activeMeta.value.directUrl || '').trim()
  }
  return (activeMeta.value.directUrl || '').trim()
})

function selectPreview(key: string) {
  activeKey.value = key
  const meta = sortedPorts.value.find((p) => publicTabKey(p) === key)
  activePort.value = meta && !isUrlPreview(meta) ? meta.port : null
}

async function exchangeTicket() {
  ticketAbort?.abort()
  const gen = ++ticketGen
  ticketAbort = new AbortController()
  vncWsUrl.value = ''
  ticketError.value = ''
  linkInactive.value = false

  if (!props.active) {
    linkInactive.value = true
    return
  }
  if (props.mobile) return
  const meta = activeMeta.value
  if (!meta) return
  if (isUrlPreview(meta) || isDirectPort(meta)) {
    ticketBusy.value = false
    return
  }
  const port = meta.port
  if (port <= 0) return

  ticketBusy.value = true
  try {
    const res = await publicGateApi.previewTicket(props.token, port, 'vnc', ticketAbort.signal)
    if (gen !== ticketGen) return
    if (res.status && res.status !== 'active') {
      linkInactive.value = true
      ticketError.value = t('pages.publicGate.appPreviewLinkInactive')
      return
    }
    if (!res.ticket) {
      ticketError.value = t('pages.publicGate.appPreviewUnavailable')
      return
    }
    vncWsUrl.value = publicPreviewVncWsUrl(res.ticket, res.wsPath)
  } catch (e: any) {
    if (gen !== ticketGen || isAbortError(e) || ticketAbort.signal.aborted) return
    const status = e?.body?.status || e?.status
    if (status === 'expired' || status === 'revoked' || status === 'used' || status === 'invalid') {
      linkInactive.value = true
      ticketError.value = t('pages.publicGate.appPreviewLinkInactive')
      return
    }
    ticketError.value = t('pages.publicGate.appPreviewUnavailable')
  } finally {
    if (gen === ticketGen) ticketBusy.value = false
  }
}

watch(
  () => [props.token, props.active, props.mobile, sortedPorts.value.map((p) => publicTabKey(p)).join(',')],
  () => {
    if (!sortedPorts.value.length) {
      activeKey.value = null
      activePort.value = null
      return
    }
    if (
      activeKey.value == null ||
      !sortedPorts.value.some((p) => publicTabKey(p) === activeKey.value)
    ) {
      activeKey.value = publicTabKey(sortedPorts.value[0])
    }
    const meta = sortedPorts.value.find((p) => publicTabKey(p) === activeKey.value)
    activePort.value = meta && !isUrlPreview(meta) ? meta.port : null
  },
  { immediate: true },
)

watch(
  () => [props.token, props.active, props.mobile, activeKey.value],
  () => {
    void exchangeTicket()
  },
  { immediate: true },
)

onUnmounted(() => {
  ticketAbort?.abort()
  ticketAbort = null
  ticketGen++
})

function onPick(payload: AppPreviewPickPayload) {
  emit('pick', payload)
}

function onStagedPick(payload: AppPreviewPickPayload | null) {
  emit('staged-pick', payload)
}

function retry() {
  void exchangeTicket()
}
</script>

<template>
  <div
    class="relative flex h-full min-h-0 flex-col"
    data-testid="public-gate-app-preview"
    :aria-busy="ticketBusy ? 'true' : 'false'"
  >
    <div
      v-if="mobile"
      class="flex h-full flex-col items-center justify-center gap-2 px-6 text-center text-sm text-txt3"
      data-testid="public-gate-app-preview-mobile"
    >
      <p>{{ t('pages.publicGate.appPreviewMobileHint') }}</p>
    </div>

    <template v-else-if="!sortedPorts.length">
      <div
        class="flex h-full flex-col items-center justify-center gap-2 px-6 text-center text-sm text-txt3"
        data-testid="public-gate-app-preview-empty"
      >
        <p>{{ t('pages.publicGate.appPreviewNoPorts') }}</p>
        <button
          type="button"
          class="rounded-lg mt-1 inline-flex min-h-11 items-center border border-line px-3 text-[12px] text-txt"
          data-testid="public-gate-app-preview-retry"
          @click="retry"
        >
          {{ t('pages.appPreview.novnc.reconnect') }}
        </button>
      </div>
    </template>

    <template v-else>
      <div v-if="sortedPorts.length > 1" class="mb-2 flex shrink-0 flex-wrap gap-1 px-2 pt-2">
        <button
          v-for="p in sortedPorts"
          :key="publicTabKey(p)"
          type="button"
          class="px-2.5 py-1 text-xs font-medium transition"
          :class="
            publicTabKey(p) === activeKey
              ? 'bg-accent/15 text-accent'
              : 'border border-line text-txt2 hover:bg-elevated hover:text-txt'
          "
          :data-testid="
            isUrlPreview(p)
              ? `public-gate-app-preview-url-${publicTabKey(p).slice('url:'.length)}`
              : `public-gate-app-preview-port-${p.port}`
          "
          @click="selectPreview(publicTabKey(p))"
        >
          {{ tabLabel(p) }}
        </button>
      </div>

      <div class="relative min-h-0 flex-1 overflow-hidden border-t border-line bg-surface">
        <div
          v-if="linkInactive"
          class="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-base/90 px-6 text-center text-sm text-txt3"
          data-testid="public-gate-app-preview-inactive"
        >
          <p>{{ t('pages.publicGate.appPreviewLinkInactive') }}</p>
        </div>
        <div
          v-else-if="ticketError && !vncWsUrl && !activeIsDirect"
          class="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-base/90 px-6 text-center text-sm text-txt3"
          data-testid="public-gate-app-preview-error"
        >
          <p>{{ ticketError }}</p>
          <button
            type="button"
            class="rounded-lg mt-1 inline-flex min-h-11 items-center border border-line px-3 text-[12px] text-txt"
            data-testid="public-gate-app-preview-retry"
            @click="retry"
          >
            {{ t('pages.appPreview.novnc.reconnect') }}
          </button>
        </div>

        <ExternalUrlPreviewFrame
          v-if="activeIsUrl && activeDirectUrl"
          :url="activeDirectUrl"
          :title="activeMeta ? tabLabel(activeMeta) : 'preview'"
        />
        <DirectPreviewFrame
          v-else-if="activeIsDirect && activeDirectUrl"
          :direct-url="activeDirectUrl"
          :title="activeMeta ? tabLabel(activeMeta) : 'preview'"
          data-testid="public-gate-app-preview-api"
          @pick="onPick"
          @staged-pick="onStagedPick"
        />
        <NovncPreviewPanel
          v-else-if="vncWsUrl"
          :key="`public-vnc-${activePort}-${vncWsUrl}`"
          :ws-url="vncWsUrl"
          :port="activePort ?? undefined"
          fill
          @pick="onPick"
          @staged-pick="onStagedPick"
          @reconnect-request="retry"
        />
        <div
          v-else-if="ticketBusy"
          class="flex h-full items-center justify-center text-sm text-txt3"
          data-testid="public-gate-app-preview-connecting"
        >
          {{ t('pages.appPreview.novnc.connectingTitle') }}
        </div>
      </div>
    </template>
  </div>
</template>

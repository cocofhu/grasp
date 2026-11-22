<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import NovncPreviewPanel from '@/components/run/NovncPreviewPanel.vue'
import DirectPreviewLauncher from '@/components/run/DirectPreviewLauncher.vue'
import ExternalUrlPreviewFrame from '@/components/run/ExternalUrlPreviewFrame.vue'
import {
  publicGateApi,
  publicPreviewVncWsUrl,
  type PublicPreview,
} from '@/lib/inbox/gateShareLink'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { isAbortError } from '@/lib/run/liveLogRehydrate'

const props = withDefaults(
  defineProps<{
    token: string
    preview: PublicPreview | null
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

const vncWsUrl = ref('')
const ticketBusy = ref(false)
const ticketError = ref('')
const linkInactive = ref(false)
let ticketAbort: AbortController | null = null
let ticketGen = 0

const activeIsUrl = computed(() => props.preview?.kind === 'url')
const activePort = computed(() => (props.preview && !activeIsUrl.value ? props.preview.port : null))
/** IP-direct sandbox port: previews over noVNC and can also open in a new tab. */
const activeIsDirect = computed(() => !!props.preview && !activeIsUrl.value && !!(props.preview.directUrl || '').trim())
const activeDirectUrl = computed(() => {
  const p = props.preview
  if (!p) return ''
  return (activeIsUrl.value ? p.url || p.directUrl || '' : p.directUrl || '').trim()
})
const previewLabel = computed(() => {
  const p = props.preview
  if (!p) return 'preview'
  const label = (p.label || '').trim()
  if (label) return label
  if (activeIsUrl.value) {
    try {
      const parsed = new URL(activeDirectUrl.value)
      return parsed.host + parsed.pathname
    } catch {
      return activeDirectUrl.value
    }
  }
  return String(p.port)
})

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

  // One ticket watches the node's sandbox desktop.
  ticketBusy.value = true
  try {
    const res = await publicGateApi.previewTicket(props.token, 0, 'vnc', ticketAbort.signal)
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
  () => [props.token, props.active, props.mobile],
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

    <template v-else>
      <div class="relative min-h-0 flex-1 overflow-hidden border-t border-line bg-surface">
        <div
          v-if="linkInactive"
          class="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-base/90 px-6 text-center text-sm text-txt3"
          data-testid="public-gate-app-preview-inactive"
        >
          <p>{{ t('pages.publicGate.appPreviewLinkInactive') }}</p>
        </div>
        <div
          v-else-if="ticketError && !vncWsUrl && !activeIsUrl"
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
          :title="previewLabel"
        />
        <NovncPreviewPanel
          v-if="vncWsUrl"
          v-show="!activeIsUrl"
          :key="`public-vnc-${vncWsUrl}`"
          :ws-url="vncWsUrl"
          :target-port="activePort ?? undefined"
          fill
          @pick="onPick"
          @staged-pick="onStagedPick"
          @reconnect-request="retry"
        >
          <template v-if="activeIsDirect && activeDirectUrl" #toolbar-extra>
            <DirectPreviewLauncher
              :key="activeDirectUrl"
              compact
              :direct-url="activeDirectUrl"
              :issue-ticket="active ? () => publicGateApi.embedTicket(token) : undefined"
              data-testid="public-gate-app-preview-api"
            />
          </template>
        </NovncPreviewPanel>
        <div
          v-else-if="ticketBusy && !activeIsUrl"
          class="flex h-full items-center justify-center text-sm text-txt3"
          data-testid="public-gate-app-preview-connecting"
        >
          {{ t('pages.appPreview.novnc.connectingTitle') }}
        </div>
      </div>
    </template>
  </div>
</template>

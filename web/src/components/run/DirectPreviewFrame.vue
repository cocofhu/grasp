<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import {
  DIRECT_PREVIEW_HOST,
  DIRECT_PREVIEW_INSPECT,
  DIRECT_PREVIEW_NAV,
  DIRECT_PREVIEW_PING,
  iframeOrigin,
  isDirectPreviewOrigin,
  parseDirectPreviewMessage,
  resolveDirectPreviewGoto,
  type DirectPreviewNavAction,
} from '@/lib/shared/directPreviewPick'

const SCRIPT_WAIT_MS = 2500
const PING_GRACE_MS = 500

const props = withDefaults(
  defineProps<{
    directUrl: string
    title?: string
  }>(),
  { title: 'preview' },
)

// Each in-page pick becomes a chat annotation chip right away; the chips are the pending list.
const emit = defineEmits<{
  (e: 'pick', payload: AppPreviewPickPayload): void
}>()

const { t } = useI18n()
const frameSrc = ref(props.directUrl)
const address = ref(props.directUrl)
const inspect = ref(false)
const inspectToggleLabel = computed(() =>
  t(inspect.value ? 'pages.appPreview.novnc.cancelInspect' : 'pages.appPreview.novnc.inspect'),
)
const scriptReady = ref(false)
const scriptTip = ref(false)
const inlineTip = ref<string | null>(null)
const iframeRef = ref<HTMLIFrameElement | null>(null)
let waitTimer: ReturnType<typeof setTimeout> | null = null
let readySeq = 0
let loadedSeq = 0

function originOf(): string {
  return iframeOrigin(props.directUrl)
}

function postToFrame(msg: Record<string, unknown>) {
  const win = iframeRef.value?.contentWindow
  const origin = originOf()
  if (!win || !origin) return
  win.postMessage(msg, origin)
}

function clearWait() {
  if (waitTimer) {
    clearTimeout(waitTimer)
    waitTimer = null
  }
}

function pingFrame() {
  postToFrame({ type: DIRECT_PREVIEW_HOST })
  postToFrame({ type: DIRECT_PREVIEW_PING })
}

function armScriptWait() {
  clearWait()
  // Anything announced from here on belongs to the document being waited for.
  loadedSeq = readySeq
  scriptTip.value = false
  inspect.value = false
  const armed = readySeq
  waitTimer = setTimeout(() => {
    if (readySeq !== armed) return
    pingFrame()
    waitTimer = setTimeout(() => {
      if (readySeq !== armed) return
      scriptReady.value = false
      scriptTip.value = true
    }, PING_GRACE_MS)
  }, SCRIPT_WAIT_MS)
}

// A classic <script> runs before the iframe "load" event, so a page carrying
// the script has already announced by now. Comparing against the previous load
// tells announced-for-this-document apart from a stale flag left by the page we
// just navigated away from.
function onIframeLoad() {
  if (readySeq > loadedSeq) {
    loadedSeq = readySeq
    scriptReady.value = true
    scriptTip.value = false
    clearWait()
    // Each new document starts standalone until it hears from us.
    postToFrame({ type: DIRECT_PREVIEW_HOST })
    return
  }
  scriptReady.value = false
  armScriptWait()
  pingFrame()
}

function onMessage(event: MessageEvent) {
  if (!isDirectPreviewOrigin(props.directUrl, event.origin)) return
  const parsed = parseDirectPreviewMessage(event.data)
  if (!parsed) return
  if (parsed.type === 'direct-preview-ready' || parsed.type === 'direct-preview-url') {
    readySeq += 1
    scriptReady.value = true
    scriptTip.value = false
    clearWait()
    address.value = parsed.url
    return
  }
  if (parsed.type === 'direct-preview-canceled') {
    inspect.value = false
    return
  }
  if (parsed.type === 'direct-preview-inspect-state') {
    inspect.value = parsed.on
    return
  }
  if (parsed.type === 'direct-preview-picked') {
    emit('pick', {
      selector: parsed.selector,
      tagName: parsed.tagName,
      outerHTML: parsed.outerHTML,
      text: parsed.text,
      url: parsed.url,
    })
  }
}

function nav(action: DirectPreviewNavAction) {
  postToFrame({ type: DIRECT_PREVIEW_NAV, action })
}

function openAddress() {
  inlineTip.value = null
  const next = resolveDirectPreviewGoto(props.directUrl, address.value)
  if (!next) {
    inlineTip.value = t('pages.appPreview.directGotoInvalid')
    return
  }
  if (next === frameSrc.value) {
    postToFrame({ type: DIRECT_PREVIEW_NAV, action: 'reload' })
    return
  }
  armScriptWait()
  frameSrc.value = next
}

function toggleInspect() {
  inlineTip.value = null
  if (!scriptReady.value) {
    scriptTip.value = true
    inlineTip.value = t('pages.appPreview.directScriptMissing')
    return
  }
  const next = !inspect.value
  inspect.value = next
  postToFrame({ type: DIRECT_PREVIEW_INSPECT, on: next })
}

watch(
  () => props.directUrl,
  (url) => {
    frameSrc.value = url
    address.value = url
    armScriptWait()
  },
)

onMounted(() => {
  window.addEventListener('message', onMessage)
  armScriptWait()
})

onUnmounted(() => {
  window.removeEventListener('message', onMessage)
  clearWait()
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col" data-testid="app-preview-direct">
    <form
      class="flex shrink-0 flex-wrap items-center gap-2 border-b border-line bg-elevated px-3 py-1.5"
      @submit.prevent="openAddress"
    >
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.back')"
        data-testid="direct-preview-back"
        @click="nav('back')"
      >
        ←
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.forward')"
        data-testid="direct-preview-forward"
        @click="nav('forward')"
      >
        →
      </button>
      <button
        type="button"
        class="rounded px-1.5 py-0.5 text-[11px] text-txt2 hover:bg-overlay"
        :title="t('pages.appPreview.novnc.reload')"
        data-testid="direct-preview-reload"
        @click="nav('reload')"
      >
        ⟳
      </button>
      <input
        v-model="address"
        type="text"
        class="min-w-0 flex-1 rounded border border-line bg-base px-2 py-1 font-mono text-[11px] text-txt outline-none focus:border-accent"
        :placeholder="t('pages.appPreview.directAddressPlaceholder')"
        spellcheck="false"
        autocomplete="off"
        data-testid="direct-preview-address"
      />
      <button
        type="submit"
        class="shrink-0 rounded bg-accent-dim px-2.5 py-1 text-[11px] text-txt hover:bg-accent/25"
        data-testid="direct-preview-go"
      >
        {{ t('pages.sandboxConsole.novncOpen') }}
      </button>
      <button
        type="button"
        class="rounded px-2 py-0.5 text-[11px] transition"
        :class="inspect ? 'bg-ok/20 text-ok' : 'text-txt2 hover:bg-overlay'"
        :title="inspectToggleLabel"
        :aria-pressed="inspect ? 'true' : 'false'"
        data-testid="direct-preview-inspect"
        @click="toggleInspect"
      >
        {{ inspectToggleLabel }}
      </button>
      <a
        :href="frameSrc"
        target="_blank"
        rel="noopener noreferrer"
        class="text-[11px] text-accent hover:underline"
        data-testid="app-preview-direct-open"
      >{{ t('pages.appPreview.directOpenTab') }}</a>
      <div
        v-if="inlineTip || scriptTip"
        class="rounded-md basis-full border px-2.5 py-1.5 text-[11px] leading-snug"
        :class="inlineTip ? 'border-err/40 bg-err/10 text-err' : 'border-warn/40 bg-warn/10 text-warn'"
        role="status"
        data-testid="direct-preview-tip"
      >
        {{ inlineTip || t('pages.appPreview.directScriptMissing') }}
      </div>
    </form>
    <iframe
      ref="iframeRef"
      :src="frameSrc"
      class="min-h-0 w-full flex-1 border-0 bg-base"
      :title="title"
      data-testid="app-preview-direct-frame"
      @load="onIframeLoad"
    />
  </div>
</template>

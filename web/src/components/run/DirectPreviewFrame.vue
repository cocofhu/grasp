<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import {
  DIRECT_PREVIEW_INSPECT,
  DIRECT_PREVIEW_NAV,
  DIRECT_PREVIEW_PING,
  acceptsPreviewFrameMessage,
  parseDirectPreviewMessage,
  previewFrameMessageOrigin,
  resolvePreviewFrameGoto,
  type DirectPreviewNavAction,
} from '@/lib/shared/directPreviewPick'

const SCRIPT_WAIT_MS = 2500
const PING_GRACE_MS = 500

const props = withDefaults(
  defineProps<{
    /** App origin (http://IP:port/). New tab keeps this so top-level login still works. */
    directUrl: string
    /** Same-origin preview path (`/preview/.../`). When set, the iframe loads this instead. */
    embedUrl?: string
    title?: string
  }>(),
  { title: 'preview', embedUrl: '' },
)

const emit = defineEmits<{
  (e: 'pick', payload: AppPreviewPickPayload): void
  (e: 'staged-pick', payload: AppPreviewPickPayload | null): void
}>()

const { t } = useI18n()

function initialFrameUrl(): string {
  return (props.embedUrl || '').trim() || props.directUrl
}

const frameSrc = ref(initialFrameUrl())
const address = ref(initialFrameUrl())
const openHref = computed(() => {
  const direct = (props.directUrl || '').trim()
  if (/^https?:\/\//i.test(direct)) return direct
  return frameSrc.value
})
const inspect = ref(false)
const inspectToggleLabel = computed(() =>
  t(inspect.value ? 'pages.appPreview.novnc.cancelInspect' : 'pages.appPreview.novnc.inspect'),
)
const scriptReady = ref(false)
const scriptTip = ref(false)
const inlineTip = ref<string | null>(null)
const picked = ref<AppPreviewPickPayload | null>(null)
const iframeRef = ref<HTMLIFrameElement | null>(null)
let waitTimer: ReturnType<typeof setTimeout> | null = null
let readySeq = 0
let loadedSeq = 0

function originOf(): string {
  return previewFrameMessageOrigin(props.embedUrl || '', props.directUrl)
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
    return
  }
  scriptReady.value = false
  armScriptWait()
  pingFrame()
}

function onMessage(event: MessageEvent) {
  if (!acceptsPreviewFrameMessage(props.embedUrl || '', props.directUrl, event.origin)) return
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
  if (parsed.type === 'direct-preview-picked') {
    inspect.value = false
    const payload: AppPreviewPickPayload = {
      selector: parsed.selector,
      tagName: parsed.tagName,
      outerHTML: parsed.outerHTML,
      url: parsed.url,
    }
    picked.value = payload
    emit('staged-pick', payload)
  }
}

function nav(action: DirectPreviewNavAction) {
  postToFrame({ type: DIRECT_PREVIEW_NAV, action })
}

function openAddress() {
  inlineTip.value = null
  const next = resolvePreviewFrameGoto(props.embedUrl || '', props.directUrl, address.value)
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

function usePick() {
  if (!picked.value) return
  emit('pick', picked.value)
}

function clearPick() {
  picked.value = null
  emit('staged-pick', null)
}

watch(
  () => [(props.embedUrl || '').trim(), props.directUrl] as const,
  ([embed, direct]) => {
    const next = embed || direct
    frameSrc.value = next
    address.value = next
    picked.value = null
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
        :href="openHref"
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
    <div
      v-if="picked"
      class="shrink-0 border-t border-line bg-elevated px-3 py-2"
      data-testid="direct-preview-pick-result"
    >
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-[11px] text-txt2">{{ t('pages.appPreview.novnc.pickedLabel') }}</span>
        <span class="text-[10px] lowercase text-txt3">{{ t('pages.appPreview.novnc.selectorLabel') }}</span>
        <code class="max-w-full break-all text-[11px] text-ok" data-testid="direct-preview-pick-selector">{{
          picked.selector
        }}</code>
        <span class="text-[10px] lowercase text-txt3">{{ t('pages.appPreview.novnc.urlLabel') }}</span>
        <code class="max-w-full break-all text-[11px] text-info" data-testid="direct-preview-pick-url">{{
          picked.url || ''
        }}</code>
        <span class="ml-auto flex shrink-0 items-center gap-2">
          <button
            type="button"
            class="rounded bg-ok/15 px-2 py-1 text-[11px] font-medium text-ok hover:bg-ok/25"
            data-testid="direct-preview-use-pick"
            @click="usePick"
          >
            {{ t('pages.appPreview.novnc.usePick') }}
          </button>
          <button
            type="button"
            class="rounded px-2 py-1 text-[11px] text-txt2 hover:bg-overlay hover:text-txt"
            :title="t('pages.appPreview.novnc.clearPick')"
            data-testid="direct-preview-clear-pick"
            @click="clearPick"
          >
            {{ t('pages.appPreview.novnc.clearPick') }}
          </button>
        </span>
      </div>
    </div>
  </div>
</template>

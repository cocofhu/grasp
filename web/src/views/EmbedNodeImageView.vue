<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import ChatImagePreviewBody from '@/components/ui/ChatImagePreviewBody.vue'
import {
  embedImageChannelName,
  parseEmbedImageFrameMessage,
  type EmbedImageFrameMessage,
} from '@/lib/inbox/embedImagePreview'
import { EMBED_IMAGE_MESSAGE, parseEmbedLocaleFromHash, parseEmbedThemeFromHash, parseEmbedThemeMessage } from '@/lib/inbox/embedChat'
import { applyPublicLocale, LOCALE_STORAGE_KEY, setLocale, type AppLocale } from '@/lib/shared/locale'
import { setThemeOverride } from '@/lib/shared/theme'

const route = useRoute()
const runId = String(route.params.runId || '')
const nodeId = String(route.params.nodeId || '')

const shown = ref(false)
const src = ref('')
const label = ref('')
const missing = ref(false)

let channel: BroadcastChannel | null = null
let openingLocale: AppLocale | null = null
let knownParentOrigin = ''

function parentOrigin(): string {
  return window.location.ancestorOrigins?.[0] || knownParentOrigin || '*'
}

function takeHash(): string {
  const hash = window.location.hash
  if (hash) history.replaceState(history.state, '', `${window.location.pathname}${window.location.search}`)
  return hash
}

function postFrame(msg: EmbedImageFrameMessage) {
  channel?.postMessage(msg)
}

function onFrameMessage(ev: MessageEvent) {
  const msg = parseEmbedImageFrameMessage(ev.data)
  if (!msg || msg.kind !== 'show' || msg.runId !== runId || msg.nodeId !== nodeId) return
  label.value = msg.label
  missing.value = msg.missing
  src.value = msg.src
  shown.value = !!msg.src
}

function onMessage(e: MessageEvent) {
  if (window.parent === window || e.source !== window.parent) return
  const origin = parentOrigin()
  if (origin !== '*' && e.origin !== origin) return
  if (e.origin && e.origin !== 'null') knownParentOrigin = e.origin
  const theme = parseEmbedThemeMessage(e.data)
  if (theme) {
    setThemeOverride(theme)
    return
  }
  // The host only asks the frame to announce itself again. The picture address
  // arrives on the same-origin channel, never through the preview page.
  const data = e.data as { type?: unknown; action?: unknown } | null
  if (data && data.type === EMBED_IMAGE_MESSAGE && data.action === 'open') {
    postFrame({ kind: 'ready', runId, nodeId })
  }
}

function onStorage(e: StorageEvent) {
  if (openingLocale) return
  if (e.key !== LOCALE_STORAGE_KEY) return
  if (e.newValue === 'zh-CN' || e.newValue === 'en') void setLocale(e.newValue)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape' || window.parent === window) return
  e.preventDefault()
  window.parent.postMessage({ type: EMBED_IMAGE_MESSAGE, action: 'close' }, parentOrigin())
}

function onBlobMissing(blobId: string) {
  if (!blobId) return
  postFrame({ kind: 'missing', runId, nodeId, blobId })
}

onMounted(() => {
  const hash = takeHash()
  const theme = parseEmbedThemeFromHash(hash)
  openingLocale = parseEmbedLocaleFromHash(hash)
  if (openingLocale) void applyPublicLocale(openingLocale)
  if (theme) setThemeOverride(theme)
  try {
    channel = new BroadcastChannel(embedImageChannelName(runId, nodeId))
  } catch {
    channel = null
  }
  if (channel) {
    channel.onmessage = onFrameMessage
    postFrame({ kind: 'ready', runId, nodeId })
  }
  window.addEventListener('message', onMessage)
  window.addEventListener('storage', onStorage)
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  postFrame({ kind: 'bye', runId, nodeId })
  channel?.close()
  channel = null
  window.removeEventListener('message', onMessage)
  window.removeEventListener('storage', onStorage)
  window.removeEventListener('keydown', onKeydown)
  setThemeOverride(null)
})
</script>

<template>
  <div class="flex h-screen min-h-0 flex-col bg-base text-txt" data-testid="embed-image-root">
    <ChatImagePreviewBody
      v-if="shown && src"
      class="min-h-0 flex-1"
      :src="src"
      :label="label"
      :force-missing="missing"
      test-id-prefix="embed-image-preview"
      fit="frame"
      @missing="onBlobMissing"
    />
  </div>
</template>

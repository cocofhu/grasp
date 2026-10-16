<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import HtmlPreview from '@/components/ui/HtmlPreview.vue'
import {
  embedDemoChannelName,
  parseEmbedDemoFrameMessage,
  type EmbedDemoFrameMessage,
} from '@/lib/inbox/embedDemoPreview'
import { EMBED_DEMO_MESSAGE, parseEmbedThemeFromHash, parseEmbedThemeMessage } from '@/lib/inbox/embedChat'
import { LOCALE_STORAGE_KEY, setLocale } from '@/lib/shared/locale'
import { setThemeOverride } from '@/lib/shared/theme'

const route = useRoute()
const runId = String(route.params.runId || '')
const nodeId = String(route.params.nodeId || '')

const shown = ref(false)
const html = ref('')
const label = ref('')

let channel: BroadcastChannel | null = null

function takeHash(): string {
  const hash = window.location.hash
  if (hash) history.replaceState(history.state, '', `${window.location.pathname}${window.location.search}`)
  return hash
}

function postFrame(msg: EmbedDemoFrameMessage) {
  channel?.postMessage(msg)
}

function onFrameMessage(ev: MessageEvent) {
  const msg = parseEmbedDemoFrameMessage(ev.data)
  if (!msg || msg.kind !== 'show' || msg.runId !== runId || msg.nodeId !== nodeId) return
  label.value = msg.label
  html.value = msg.html
  shown.value = !!msg.html.trim()
}

function onMessage(e: MessageEvent) {
  if (window.parent === window || e.source !== window.parent) return
  const origin = window.location.ancestorOrigins?.[0]
  if (origin && e.origin !== origin) return
  const theme = parseEmbedThemeMessage(e.data)
  if (theme) {
    setThemeOverride(theme)
    return
  }
  // The host only asks the frame to announce itself again. The demo HTML
  // arrives on the same-origin channel, never through the preview page.
  const data = e.data as { type?: unknown; action?: unknown } | null
  if (data && data.type === EMBED_DEMO_MESSAGE && data.action === 'open') {
    postFrame({ kind: 'ready', runId, nodeId })
  }
}

function onStorage(e: StorageEvent) {
  if (e.key !== LOCALE_STORAGE_KEY) return
  if (e.newValue === 'zh-CN' || e.newValue === 'en') void setLocale(e.newValue)
}

onMounted(() => {
  const theme = parseEmbedThemeFromHash(takeHash())
  if (theme) setThemeOverride(theme)
  try {
    channel = new BroadcastChannel(embedDemoChannelName(runId, nodeId))
  } catch {
    channel = null
  }
  if (channel) {
    channel.onmessage = onFrameMessage
    postFrame({ kind: 'ready', runId, nodeId })
  }
  window.addEventListener('message', onMessage)
  window.addEventListener('storage', onStorage)
})

onUnmounted(() => {
  postFrame({ kind: 'bye', runId, nodeId })
  channel?.close()
  channel = null
  window.removeEventListener('message', onMessage)
  window.removeEventListener('storage', onStorage)
  setThemeOverride(null)
})
</script>

<template>
  <div class="flex h-screen min-h-0 flex-col bg-base text-txt" data-testid="embed-demo-root">
    <div v-if="shown && html.trim()" class="embed-demo-fill min-h-0 flex-1">
      <HtmlPreview :html="html" mode="demo" :enlargeable="false" :modal-title="label" />
    </div>
  </div>
</template>

<style scoped>
.embed-demo-fill {
  display: flex;
  min-height: 0;
  flex-direction: column;
}
.embed-demo-fill :deep(iframe) {
  height: 100%;
  min-height: 0;
  flex: 1;
}
</style>

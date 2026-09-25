<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import PageControlStatus from '@/components/run/PageControlStatus.vue'
import PublicGateApprovalView from '@/views/PublicGateApprovalView.vue'
import {
  EMBED_READY_MESSAGE,
  clearEmbedSession,
  loadEmbedSession,
  parseEmbedCmdResult,
  parseEmbedControlMessage,
  parseEmbedPickMessage,
  parseEmbedThemeFromHash,
  parseEmbedThemeMessage,
  parseEmbedTicketFromHash,
  redeemEmbedTicket,
  saveEmbedSession,
} from '@/lib/inbox/embedChat'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { setThemeOverride } from '@/lib/shared/theme'
import { usePageControl } from '@/lib/inbox/embedPageControl'

type ChatRef = {
  addPick?: (payload: AppPreviewPickPayload) => void
  sendEventsFrame?: (frame: Record<string, unknown>) => boolean
}

const { t } = useI18n()
const route = useRoute()
const runId = String(route.params.runId || '')
const nodeId = String(route.params.nodeId || '')

const phase = ref<'connecting' | 'ready' | 'expired' | 'network'>('connecting')
const token = ref('')
const chatRef = ref<ChatRef | null>(null)
let announced = false

const pageControl = usePageControl({
  runId,
  nodeId,
  post: (msg) => {
    if (window.parent !== window) window.parent.postMessage(msg, parentOrigin())
  },
  send: (frame) => chatRef.value?.sendEventsFrame?.(frame) ?? false,
})
const { supported: pageControlSupported, enabled: pageControlOn, state: pageControlState, active: pageControlActive } = pageControl

function takeHash(): string {
  const hash = window.location.hash
  if (hash) history.replaceState(history.state, '', `${window.location.pathname}${window.location.search}`)
  return hash
}

async function connect() {
  phase.value = 'connecting'
  const hash = takeHash()
  const theme = parseEmbedThemeFromHash(hash)
  if (theme) setThemeOverride(theme)
  const ticket = parseEmbedTicketFromHash(hash)
  if (ticket) {
    try {
      const s = await redeemEmbedTicket(ticket, runId, nodeId)
      if (s) {
        saveEmbedSession(runId, nodeId, s)
        token.value = s.token
        phase.value = 'ready'
        return
      }
    } catch {
      if (!loadEmbedSession(runId, nodeId)) {
        phase.value = 'network'
        return
      }
    }
  }
  const stored = loadEmbedSession(runId, nodeId)
  if (stored) {
    token.value = stored.token
    phase.value = 'ready'
    return
  }
  phase.value = 'expired'
}

function onStatus(status: string) {
  // The page queues picks until the chat can take them.
  if (status === 'active' && !announced && window.parent !== window) {
    announced = true
    window.parent.postMessage({ type: EMBED_READY_MESSAGE }, parentOrigin())
    pageControl.awaitHello()
  }
  if (status === 'invalid' || status === 'expired' || status === 'revoked') {
    pageControl.reset()
    clearEmbedSession(runId, nodeId)
    token.value = ''
    phase.value = 'expired'
  }
}

function parentOrigin(): string {
  return window.location.ancestorOrigins?.[0] || '*'
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
  const control = parseEmbedControlMessage(e.data)
  if (control) {
    pageControl.onPageControl(control)
    return
  }
  const result = parseEmbedCmdResult(e.data)
  if (result) {
    pageControl.onPageResult(result)
    return
  }
  const pick = parseEmbedPickMessage(e.data)
  if (pick) chatRef.value?.addPick?.(pick)
}

onMounted(() => {
  window.addEventListener('message', onMessage)
  void connect()
})
onUnmounted(() => {
  window.removeEventListener('message', onMessage)
  pageControl.dispose()
  setThemeOverride(null)
})
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden bg-base text-txt" data-testid="embed-chat-root">
    <div
      v-if="phase === 'ready' && token && pageControlSupported !== null"
      class="shrink-0 border-b border-line px-3 py-2"
      data-page-agent-not-interactive
      data-testid="page-control-bar"
    >
      <template v-if="pageControlSupported">
        <label class="flex items-center justify-between gap-3 text-[12px] text-txt2">
          <span>{{ t('pages.embedChat.pageControl.toggle') }}</span>
          <AppSwitch
            :model-value="pageControlOn"
            :aria-label="t('pages.embedChat.pageControl.toggle')"
            data-testid="page-control-toggle"
            @update:model-value="pageControl.setEnabled"
          />
        </label>
        <p v-if="!pageControlOn" class="m-0 mt-1 text-[11px] leading-snug text-txt3">
          {{ t('pages.embedChat.pageControl.privacy') }}
        </p>
        <PageControlStatus v-else class="mt-1" :state="pageControlState" :active="pageControlActive" />
      </template>
      <p v-else class="m-0 text-[11px] leading-snug text-txt3" data-testid="page-control-unsupported">
        {{ t('pages.embedChat.pageControl.unsupported') }}
      </p>
    </div>
    <PublicGateApprovalView
      v-if="phase === 'ready' && token"
      ref="chatRef"
      class="min-h-0 flex-1"
      :embed-token="token"
      @status="onStatus"
      @events-ready="pageControl.onEventsReady"
      @events-closed="pageControl.onEventsClosed"
      @page-frame="pageControl.onServerFrame"
    />
    <div
      v-else-if="phase === 'connecting'"
      class="flex flex-1 flex-col items-center justify-center gap-3 text-center"
      role="status"
      aria-busy="true"
      data-testid="embed-chat-connecting"
    >
      <Icon name="spinner" :size="24" class="animate-spin text-accent" aria-hidden="true" />
      <p class="text-sm text-txt3">{{ t('pages.embedChat.connecting') }}</p>
    </div>
    <div
      v-else-if="phase === 'network'"
      class="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center"
      role="alert"
      data-testid="embed-chat-network"
    >
      <Icon name="alert" :size="24" class="text-warn" />
      <p class="max-w-[36ch] text-sm text-txt2">{{ t('pages.embedChat.networkError') }}</p>
    </div>
    <div
      v-else
      class="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center"
      role="status"
      data-testid="embed-chat-expired"
    >
      <Icon name="alert" :size="24" class="text-warn" />
      <h1 class="text-base font-semibold">{{ t('pages.embedChat.expiredTitle') }}</h1>
      <p class="max-w-[36ch] text-sm text-txt3">{{ t('pages.embedChat.expiredHint') }}</p>
    </div>
  </div>
</template>

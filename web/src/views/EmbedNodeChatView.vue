<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import PublicGateApprovalView from '@/views/PublicGateApprovalView.vue'
import {
  EMBED_READY_MESSAGE,
  clearEmbedSession,
  loadEmbedSession,
  parseEmbedPickMessage,
  parseEmbedTicketFromHash,
  redeemEmbedTicket,
  saveEmbedSession,
} from '@/lib/inbox/embedChat'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'

type ChatRef = { addPick?: (payload: AppPreviewPickPayload) => void }

const { t } = useI18n()
const route = useRoute()
const runId = String(route.params.runId || '')
const nodeId = String(route.params.nodeId || '')

const phase = ref<'connecting' | 'ready' | 'expired' | 'network'>('connecting')
const token = ref('')
const chatRef = ref<ChatRef | null>(null)

function takeTicket(): string {
  const ticket = parseEmbedTicketFromHash(window.location.hash)
  if (ticket) history.replaceState(history.state, '', `${window.location.pathname}${window.location.search}`)
  return ticket
}

async function connect() {
  phase.value = 'connecting'
  const ticket = takeTicket()
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
  if (status === 'invalid' || status === 'expired' || status === 'revoked') {
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
  const pick = parseEmbedPickMessage(e.data)
  if (pick) chatRef.value?.addPick?.(pick)
}

onMounted(() => {
  window.addEventListener('message', onMessage)
  if (window.parent !== window) window.parent.postMessage({ type: EMBED_READY_MESSAGE }, parentOrigin())
  void connect()
})
onUnmounted(() => window.removeEventListener('message', onMessage))
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden bg-base text-txt" data-testid="embed-chat-root">
    <PublicGateApprovalView
      v-if="phase === 'ready' && token"
      ref="chatRef"
      :embed-token="token"
      @status="onStatus"
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

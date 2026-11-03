<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatUserMessage from '@/components/run/ChatUserMessage.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import { parseShareTokenFromHash } from '@/lib/inbox/gateShareLink'
import { publicAppPreviewClient, type PreviewChatRow, type PreviewShareSession } from '@/lib/inbox/appPreviewShare'

const { t } = useI18n()
const client = publicAppPreviewClient(parseShareTokenFromHash(window.location.hash))
const controller = new AbortController()
const session = ref<PreviewShareSession>()
const tab = ref<'preview' | 'chat'>('preview')
const rows = ref<PreviewChatRow[]>([])
const input = ref('')
const messageError = ref('')
const pageError = ref<'inactive' | 'network' | ''>('')
const busy = ref(false)
const sending = ref(false)
const loading = ref(true)
const iframeLoading = ref(true)
const chatReady = ref(false)
const transcript = ref<HTMLElement | null>(null)
let renew: ReturnType<typeof setTimeout> | undefined
let poll: ReturnType<typeof setTimeout> | undefined
let socket: WebSocket | undefined
let messageNumber = 0
let guestReplyId = ''

function followChat() {
  const el = transcript.value
  if (!el || el.scrollHeight - el.scrollTop - el.clientHeight > 80) return
  void nextTick(() => { el.scrollTop = el.scrollHeight })
}

function expire() {
  pageError.value = 'inactive'
  session.value = undefined
  chatReady.value = false
  clearTimeout(poll)
  socket?.close()
}

function handleError(error: unknown) {
  if (controller.signal.aborted) return
  if ((error as { status?: number }).status === 410) expire()
  else if (!session.value) pageError.value = 'network'
  else messageError.value = t('pages.appPreview.share.chatUnavailable')
}

async function loadSession() {
  clearTimeout(renew)
  const old = session.value
  if (!old) {
    loading.value = true
    pageError.value = ''
  }
  try {
    const next = await client.session(old?.ticket || '', controller.signal)
    if (controller.signal.aborted) return
    if (next.iframePath !== old?.iframePath) iframeLoading.value = true
    session.value = next
    if (next.chatMode === 'isolated') {
      if (!old) connectGuest()
    } else if (!old) {
      void pollMain()
    }
    renew = setTimeout(loadSession, 55_000)
  } catch (error) {
    handleError(error)
    if (session.value && !controller.signal.aborted) renew = setTimeout(loadSession, 5_000)
  } finally {
    loading.value = false
  }
}

async function pollMain() {
  if (!session.value || controller.signal.aborted) return
  try {
    const next = await client.history(controller.signal)
    if (!session.value || controller.signal.aborted) return
    followChat()
    rows.value = next.rows
    busy.value = next.busy
    if (!chatReady.value) messageError.value = ''
    chatReady.value = true
  } catch (error) {
    handleError(error)
    chatReady.value = false
  } finally {
    if (session.value && !controller.signal.aborted) poll = setTimeout(pollMain, 1_800)
  }
}

async function retryGuest() {
  await loadSession()
  if (session.value) connectGuest()
}

function connectGuest() {
  if (!session.value || socket?.readyState === WebSocket.CONNECTING || socket?.readyState === WebSocket.OPEN) return
  messageError.value = ''
  const current = client.connectGuest(session.value.ticket)
  socket = current
  current.onopen = () => { if (socket === current) chatReady.value = true }
  current.onmessage = (event) => {
    if (socket !== current || typeof event.data !== 'string') return
    const message = JSON.parse(event.data) as { type?: string; text?: string; failed?: boolean }
    if (message.type === 'reply' || message.type === 'done') {
      const row = rows.value.find(row => row.id === guestReplyId)
      followChat()
      if (row) row.text = message.text || ''
    }
    if (message.type === 'done' || message.type === 'error') {
      busy.value = false
      if (message.failed || message.type === 'error') messageError.value = t('pages.appPreview.share.sendFailed')
    }
  }
  current.onclose = () => {
    if (socket !== current || controller.signal.aborted) return
    chatReady.value = false
    busy.value = false
    messageError.value = t('pages.appPreview.share.chatUnavailable')
  }
}

async function submit() {
  const text = input.value.trim()
  if (!text || !chatReady.value || sending.value || !session.value) return
  messageError.value = ''
  if (session.value.chatMode === 'isolated') {
    if (busy.value || socket?.readyState !== WebSocket.OPEN) return
    const id = ++messageNumber
    guestReplyId = `assistant-${id}`
    followChat()
    rows.value.push({ id: `user-${id}`, role: 'user', text }, { id: guestReplyId, role: 'assistant', text: '' })
    input.value = ''
    busy.value = true
    socket.send(JSON.stringify({ text }))
    return
  }
  sending.value = true
  try {
    await client.send(text, controller.signal)
    if (input.value.trim() === text) input.value = ''
    busy.value = true
  } catch (error) {
    if ((error as { status?: number }).status === 410) expire()
    else if (!controller.signal.aborted) messageError.value = t('pages.appPreview.share.sendFailed')
  } finally {
    sending.value = false
  }
}

function onInputKey(event: KeyboardEvent) {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
    event.preventDefault()
    void submit()
  }
}

onMounted(loadSession)
onBeforeUnmount(() => {
  controller.abort()
  clearTimeout(renew)
  clearTimeout(poll)
  socket?.close()
})
</script>

<template>
  <main class="flex h-[100dvh] min-h-0 flex-col overflow-hidden bg-base text-txt" data-testid="shared-app-preview">
    <header class="flex shrink-0 items-center gap-3 border-b border-line bg-surface px-4 py-2.5">
      <Icon name="globe" :size="17" class="text-accent" />
      <h1 class="min-w-0 flex-1 truncate text-sm font-semibold">{{ t('pages.appPreview.share.publicTitle') }}</h1>
      <span v-if="session" class="rounded-full bg-accent/10 px-2 py-1 text-[11px] text-accent">
        {{ t(`pages.appPreview.share.${session.chatMode}`) }}
      </span>
      <div v-if="session" class="flex rounded-lg bg-elevated p-0.5 md:hidden">
        <button
          v-for="choice in (['preview', 'chat'] as const)"
          :key="choice"
          type="button"
          :aria-pressed="tab === choice"
          class="rounded-md px-2.5 py-1 text-xs transition-colors"
          :class="tab === choice ? 'bg-surface text-accent shadow-sm' : 'text-txt2'"
          @click="tab = choice"
        >{{ t(`pages.appPreview.share.${choice}Tab`) }}</button>
      </div>
    </header>
    <div v-if="session" class="grid min-h-0 flex-1 md:grid-cols-[minmax(0,1fr)_minmax(300px,380px)]">
      <section class="relative min-h-0 min-w-0 flex-col" :class="tab === 'preview' ? 'flex' : 'hidden md:flex'">
        <iframe
          :src="session.iframePath"
          :title="t('pages.appPreview.share.previewTab')"
          class="h-full w-full flex-1 border-0 bg-white"
          sandbox="allow-scripts allow-forms"
          referrerpolicy="no-referrer"
          @load="iframeLoading = false"
        />
        <div v-if="iframeLoading" class="pointer-events-none absolute inset-0 grid place-items-center bg-surface" role="status">
          <Icon name="spinner" :size="24" class="text-accent" />
        </div>
      </section>
      <section class="min-h-0 min-w-0 flex-col bg-surface md:border-l md:border-line" :class="tab === 'chat' ? 'flex' : 'hidden md:flex'">
        <div class="border-b border-line px-4 py-3 text-xs leading-relaxed text-txt2">
          {{ t(`pages.appPreview.share.${session.chatMode}GuestHint`) }}
        </div>
        <div ref="transcript" class="scroll-area min-h-0 flex-1 space-y-4 overflow-y-auto p-4" role="log">
          <p v-if="!rows.length" class="text-sm text-txt3">{{ t('pages.appPreview.share.emptyChat') }}</p>
          <template v-for="row in rows" :key="row.id">
            <ChatUserMessage v-if="row.role === 'user' || row.role === 'human'" :text="row.text" />
            <AgentTimeline v-else-if="row.role === 'assistant' || row.role === 'agent'" :text="row.text" />
            <p v-else class="text-xs text-txt3">{{ row.text }}</p>
          </template>
          <p v-if="busy" role="status" class="text-xs text-txt3">{{ t('pages.solo.phase.running') }}</p>
        </div>
        <form class="shrink-0 space-y-2 border-t border-line p-3" @submit.prevent="submit">
          <div v-if="messageError" class="flex items-center gap-2 text-xs text-err" role="alert">
            <span class="flex-1">{{ messageError }}</span>
            <AppButton v-if="!chatReady && session.chatMode === 'isolated'" size="sm" type="button" @click="retryGuest">
              {{ t('common.buttons.retry') }}
            </AppButton>
          </div>
          <textarea
            v-model="input"
            rows="2"
            maxlength="2500"
            :aria-label="t('pages.appPreview.share.message')"
            :placeholder="t('pages.appPreview.share.message')"
            class="w-full resize-y rounded-lg border border-line bg-base p-2.5 text-sm focus:border-accent focus:outline-none"
            data-testid="shared-app-chat-input"
            @keydown="onInputKey"
          />
          <div class="flex justify-end">
            <AppButton type="submit" variant="primary" icon="chat-send" :loading="sending" :disabled="!chatReady || !input.trim() || (session.chatMode === 'isolated' && busy)">
              {{ t('pages.solo.chat.send') }}
            </AppButton>
          </div>
        </form>
      </section>
    </div>
    <div v-else class="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center" role="status">
      <Icon :name="loading ? 'spinner' : 'alert'" :size="26" class="text-accent" />
      <p class="text-sm text-txt2">{{ t(loading ? 'pages.appPreview.share.loading' : `pages.appPreview.share.${pageError || 'inactive'}`) }}</p>
      <AppButton v-if="pageError === 'network'" size="sm" @click="loadSession">{{ t('common.buttons.retry') }}</AppButton>
    </div>
  </main>
</template>

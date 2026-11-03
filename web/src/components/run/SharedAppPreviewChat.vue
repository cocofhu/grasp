<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatUserMessage from '@/components/run/ChatUserMessage.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import { previewPickLabel, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { publicAppPreviewClient, type PreviewChatRow, type PreviewShareSession } from '@/lib/inbox/appPreviewShare'

const { t } = useI18n()
const props = defineProps<{ token: string }>()
const emit = defineEmits<{ status: [status: 'active' | 'expired'] }>()
const client = publicAppPreviewClient(props.token)
const controller = new AbortController()
const session = ref<PreviewShareSession>()
const rows = ref<PreviewChatRow[]>([])
const input = ref('')
const picks = ref<string[]>([])
const messageError = ref('')
const pageError = ref<'inactive' | 'network' | ''>('')
const busy = ref(false)
const sending = ref(false)
const loading = ref(true)
const chatReady = ref(false)
const transcript = ref<HTMLElement | null>(null)
let renew: ReturnType<typeof setTimeout> | undefined
let poll: ReturnType<typeof setTimeout> | undefined
let readingMain = false
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
  emit('status', 'expired')
  session.value = undefined
  chatReady.value = false
  clearTimeout(renew)
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
    session.value = next
    if (!old) emit('status', 'active')
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
  if (!session.value || controller.signal.aborted || readingMain) return
  clearTimeout(poll)
  readingMain = true
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
    readingMain = false
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

function addPick(payload: AppPreviewPickPayload) {
  const selector = payload.selector.slice(0, 120)
  const label = previewPickLabel(payload.url, selector, payload.tagName)
  const context = label.length <= 120 ? label : selector
  if (picks.value.length < 4 && !picks.value.includes(context)) picks.value.push(context)
}
defineExpose({ addPick })

async function submit() {
  const draft = input.value.trim()
  const selected = picks.value.slice()
  if ((!draft && !selected.length) || !chatReady.value || sending.value || !session.value) return
  const context = selected.join('\n')
  const text = context ? [draft, `Selected page elements:\n${context}`].filter(Boolean).join('\n\n') : draft
  if (new TextEncoder().encode(text).byteLength > 8192) {
    messageError.value = t('pages.appPreview.share.messageTooLong')
    return
  }
  messageError.value = ''
  if (session.value.chatMode === 'isolated') {
    if (busy.value || socket?.readyState !== WebSocket.OPEN) return
    const id = ++messageNumber
    guestReplyId = `assistant-${id}`
    followChat()
    rows.value.push({ id: `user-${id}`, role: 'user', text }, { id: guestReplyId, role: 'assistant', text: '' })
    input.value = ''
    picks.value = []
    busy.value = true
    socket.send(JSON.stringify({ text }))
    return
  }
  sending.value = true
  try {
    await client.send(text, controller.signal)
    if (input.value.trim() === draft) input.value = ''
    picks.value = picks.value.filter(pick => !selected.includes(pick))
    busy.value = true
    void pollMain()
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
  <section v-if="session" class="flex min-h-0 min-w-0 flex-1 flex-col bg-surface" data-testid="shared-app-chat">
    <div class="border-b border-line px-4 py-3 text-xs leading-relaxed text-txt2">
      <span class="mr-1 font-medium text-txt">{{ t(`pages.appPreview.share.${session.chatMode}`) }}</span>
      {{ t(`pages.appPreview.share.${session.chatMode}GuestHint`) }}
    </div>
    <div ref="transcript" class="scroll-area min-h-0 flex-1 space-y-4 overflow-y-auto p-4" role="log">
      <p v-if="!rows.length && !messageError" class="text-sm text-txt3">
        {{ t(chatReady ? 'pages.appPreview.share.emptyChat' : 'pages.embedChat.connecting') }}
      </p>
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
      <div v-if="picks.length" class="flex flex-wrap gap-1">
        <button
          v-for="(pick, i) in picks"
          :key="pick"
          type="button"
          class="max-w-full truncate rounded border border-line bg-overlay px-2 py-1 text-xs"
          data-testid="shared-app-chat-pick"
          @click="picks.splice(i, 1)"
        >{{ pick }} ×</button>
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
        <AppButton type="submit" variant="primary" icon="chat-send" :loading="sending" :disabled="!chatReady || (!input.trim() && !picks.length) || (session.chatMode === 'isolated' && busy)">
          {{ t('pages.solo.chat.send') }}
        </AppButton>
      </div>
    </form>
  </section>
  <div v-else class="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center" role="status">
    <Icon :name="loading ? 'spinner' : 'alert'" :size="26" class="text-accent" />
    <p class="text-sm text-txt2">{{ t(loading ? 'pages.embedChat.connecting' : `pages.appPreview.share.${pageError || 'inactive'}`) }}</p>
    <AppButton v-if="pageError === 'network'" size="sm" @click="loadSession">{{ t('common.buttons.retry') }}</AppButton>
  </div>
</template>

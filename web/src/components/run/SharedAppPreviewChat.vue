<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatUserMessage from '@/components/run/ChatUserMessage.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import AnnotationChip from './AnnotationChip.vue'
import ComposerShell from './ComposerShell.vue'
import ComposerPrimaryAction from './ComposerPrimaryAction.vue'
import ParagraphInput from '@/components/ui/ParagraphInput.vue'
import VisitModelSelect, { type VisitModelTransport } from './VisitModelSelect.vue'
import type { VisitModelChoice } from '@/lib/api/clients/runsClient'
import type { ReactAnnotation } from '@/lib/shared/types'
import { appendPreviewPick, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { publicAppPreviewClient, type PreviewChatRow, type PreviewShareSession } from '@/lib/inbox/appPreviewShare'

const { t } = useI18n()
const props = defineProps<{ token: string }>()
const emit = defineEmits<{ status: [status: 'active' | 'expired'] }>()
const client = publicAppPreviewClient(props.token)
const controller = new AbortController()
const session = ref<PreviewShareSession>()
const rows = ref<PreviewChatRow[]>([])
const mainChoice = ref<VisitModelChoice>()
const input = ref('')
const picks = ref<ReactAnnotation[]>([])
const messageError = ref('')
const pageError = ref<'inactive' | 'network' | ''>('')
const busy = ref(false)
const sending = ref(false)
const loading = ref(true)
const chatReady = ref(false)
const guestModelChanging = ref(false)
const guestConnection = ref(0)
let guestModelReply: ((ok: boolean) => void) | undefined
let guestChoice: VisitModelChoice = {}
let mainModelRevision = 0

const modelTransport: VisitModelTransport = {
  modelCatalog: () => client.modelCatalog(controller.signal).catch(error => {
    handleError(error)
    throw error
  }),
  getVisitModel: async () => session.value?.chatMode === 'isolated' ? guestChoice : client.getModel(controller.signal),
  setVisitModel: async (_run, _node, body) => {
    if (session.value?.chatMode !== 'isolated') {
      ++mainModelRevision
      try {
        const choice = await client.setModel(controller.signal, body)
        mainChoice.value = choice
        return choice
      } catch (error) {
        if ((error as { status?: number }).status === 410) expire()
        throw error
      } finally {
        ++mainModelRevision
      }
    }
    if (socket?.readyState !== WebSocket.OPEN || busy.value || guestModelChanging.value) {
      throw new Error(t('pages.appPreview.share.chatUnavailable'))
    }
    const activeSocket = socket
    guestModelChanging.value = true
    try {
      await new Promise<void>((resolve, reject) => {
        guestModelReply = (ok) => {
          guestModelReply = undefined
          if (ok) resolve()
          else reject(new Error(t('pages.solo.errors.model_switch_failed')))
        }
        activeSocket.send(JSON.stringify({ modelId: body.modelId || '', reasoningEffort: body.reasoningEffort || '' }))
      })
      guestChoice = { currentModelId: body.modelId, currentModelSet: true, reasoningEffort: body.reasoningEffort }
      return guestChoice
    } finally {
      guestModelReply = undefined
      guestModelChanging.value = false
    }
  },
}
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
  guestModelReply?.(false)
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
  const modelRevision = mainModelRevision
  try {
    const next = await client.history(controller.signal)
    if (!session.value || controller.signal.aborted) return
    followChat()
    rows.value = next.rows
    busy.value = next.busy
    if (next.model && modelRevision === mainModelRevision) mainChoice.value = next.model
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
  guestChoice = {}
  guestConnection.value++
  const current = client.connectGuest(session.value.ticket)
  socket = current
  current.onopen = () => { if (socket === current) chatReady.value = true }
  current.onmessage = (event) => {
    if (socket !== current || typeof event.data !== 'string') return
    const message = JSON.parse(event.data) as { type?: string; text?: string; failed?: boolean; ok?: boolean }
    if (message.type === 'model') {
      guestModelReply?.(message.ok === true)
      return
    }
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
    guestModelReply?.(false)
    chatReady.value = false
    busy.value = false
    messageError.value = t('pages.appPreview.share.chatUnavailable')
  }
}

function addPick(payload: AppPreviewPickPayload) {
  appendPreviewPick(picks.value, payload, 4)
}
defineExpose({ addPick })

async function submit() {
  const draft = input.value.trim()
  const selected = picks.value.slice()
  if ((!draft && !selected.length) || !chatReady.value || sending.value || guestModelChanging.value || !session.value) return
  if (new TextEncoder().encode(draft).byteLength > 8192) {
    messageError.value = t('pages.appPreview.share.messageTooLong')
    return
  }
  messageError.value = ''
  if (session.value.chatMode === 'isolated') {
    if (busy.value || socket?.readyState !== WebSocket.OPEN) return
    const id = ++messageNumber
    guestReplyId = `assistant-${id}`
    followChat()
    rows.value.push({ id: `user-${id}`, role: 'user', text: draft, annotations: selected }, { id: guestReplyId, role: 'assistant', text: '' })
    input.value = ''
    picks.value = []
    busy.value = true
    socket.send(JSON.stringify({ text: draft, annotations: selected }))
    return
  }
  sending.value = true
  try {
    await client.send(draft, selected, controller.signal)
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
  guestModelReply?.(false)
  socket?.close()
})
</script>

<template>
  <section v-if="session" class="flex min-h-0 min-w-0 flex-1 flex-col bg-surface" data-testid="shared-app-chat">
    <div class="border-b border-line px-4 py-3 text-xs leading-relaxed text-txt2">
      <span class="mr-1 font-medium text-txt">{{ t(`pages.appPreview.share.${session.chatMode}`) }}</span>
      {{ t(`pages.appPreview.share.${session.chatMode}GuestHint`) }}
    </div>
    <div ref="transcript" class="chat-thread scroll-area min-h-0 flex-1 space-y-4 overflow-y-auto p-4" role="log">
      <p v-if="!rows.length && !messageError" class="text-sm text-txt3">
        {{ t(chatReady ? 'pages.appPreview.share.emptyChat' : 'pages.embedChat.connecting') }}
      </p>
      <template v-for="row in rows" :key="row.id">
        <div v-if="row.role === 'user' || row.role === 'human'" class="flex justify-end">
          <ChatUserMessage class="min-w-0 max-w-[88%]" :text="row.text" :annotations="row.annotations" />
        </div>
        <AgentTimeline v-else-if="row.role === 'assistant' || row.role === 'agent'" :text="row.text" />
        <p v-else class="text-xs text-txt3">{{ row.text }}</p>
      </template>
      <p v-if="busy" role="status" class="text-xs text-txt3">{{ t('pages.solo.phase.running') }}</p>
    </div>
    <div class="shrink-0 space-y-2 border-t border-line p-3">
      <div v-if="messageError" class="flex items-center gap-2 text-xs text-err" role="alert">
        <span class="flex-1">{{ messageError }}</span>
        <AppButton v-if="!chatReady && session.chatMode === 'isolated'" size="sm" type="button" @click="retryGuest">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>
      <div v-if="picks.length" class="flex flex-wrap gap-1">
        <AnnotationChip
          v-for="(pick, i) in picks"
          :key="i"
          :ann="pick"
          removable
          test-id="shared-app-chat-pick"
          @remove="picks.splice(i, 1)"
        />
      </div>
      <ComposerShell compact wrap-toolbar :show-footer="false">
        <template #input>
          <ParagraphInput
            v-model:text="input"
            compact embedded text-only
            :placeholder="t('pages.appPreview.share.message')"
            data-testid="shared-app-chat-input"
            @keydown="onInputKey"
          />
        </template>
        <template #toolbar-start>
          <VisitModelSelect
            :key="guestConnection"
            run-id="" node-id="" agent-name=""
            :transport="modelTransport"
            :current-model-set="mainChoice?.currentModelSet"
            :current-model-id="mainChoice?.currentModelId"
            :current-configuration-id="mainChoice?.currentConfigurationId"
            :reasoning-effort="mainChoice?.reasoningEffort"
            :model-notice="mainChoice?.modelNotice"
            :disabled="!chatReady || (session.chatMode === 'isolated' && busy)"
          />
        </template>
        <template #toolbar-end>
          <ComposerPrimaryAction
            kind="send"
            :label="t('pages.solo.chat.send')"
            :busy="sending"
            :disabled="!chatReady || sending || guestModelChanging || (!input.trim() && !picks.length) || (session.chatMode === 'isolated' && busy)"
            testid="shared-app-chat-send"
            @click="submit"
          />
        </template>
      </ComposerShell>
    </div>
  </section>
  <div v-else class="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center" role="status">
    <Icon :name="loading ? 'spinner' : 'alert'" :size="26" class="text-accent" />
    <p class="text-sm text-txt2">{{ t(loading ? 'pages.embedChat.connecting' : `pages.appPreview.share.${pageError || 'inactive'}`) }}</p>
    <AppButton v-if="pageError === 'network'" size="sm" @click="loadSession">{{ t('common.buttons.retry') }}</AppButton>
  </div>
</template>

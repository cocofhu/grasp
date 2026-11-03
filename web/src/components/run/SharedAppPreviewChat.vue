<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowReactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatUserMessage from '@/components/run/ChatUserMessage.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import ThinkingBusyCue from './ThinkingBusyCue.vue'
import { useChatTailScroll } from '@/lib/chat/chatScroll'
import { relTime } from '@/lib/shared/format'
import ComposerPrimaryAction from './ComposerPrimaryAction.vue'
import ChatComposer from '@/components/chat/ChatComposer.vue'
import { useChatComposerDraft } from '@/lib/chat/useChatComposerDraft'
import ChatAttachments from '@/components/chat/ChatAttachments.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { attachmentByteLength, isImageAttachment, SITE_ATTACH_MAX_BYTES } from '@/lib/shared/attachments'
import type { ReferenceSearchParams } from '@/lib/api/clients/soloClient'
import VisitModelSelect, { type VisitModelTransport } from './VisitModelSelect.vue'
import type { VisitModelChoice } from '@/lib/api/clients/runsClient'
import type { ReactAnnotation } from '@/lib/shared/types'
import { appendPreviewPick, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { publicAppPreviewClient, type PreviewChatMessage, type PreviewChatRow, type PreviewShareSession } from '@/lib/inbox/appPreviewShare'

const { t } = useI18n()
const props = defineProps<{ token: string }>()
const emit = defineEmits<{ status: [status: 'active' | 'expired'] }>()
const client = publicAppPreviewClient(props.token)
const controller = new AbortController()
const session = ref<PreviewShareSession>()
const rows = ref<PreviewChatRow[]>([])
const mainChoice = ref<VisitModelChoice>()
const draft = useChatComposerDraft()
const { input, textarea, attach, mention } = draft
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
const { scroller, onScroll, scrollDown } = useChatTailScroll()
const imagePreview = useChatImagePreview()
const referenceScope = { sessionId: 'shared-app-preview' }
const searchReferences = <T,>(_id: string, params: ReferenceSearchParams, signal?: AbortSignal) =>
  client.searchReferences<T>(params, signal ?? controller.signal)
const canSubmit = computed(() => !!input.value.trim() || !!picks.value.length || !!attach.attachments.value.length || !!mention.refs.value.length)

const imageURLs = shallowReactive(new Map<string, string>())
const imageRequests = new Set<string>()
function imageKey(row: PreviewChatRow, index: number) { return `${row.id}:${index}` }
function shownAttachments(row: PreviewChatRow) {
  return (row.images || []).map((image, index) => ({ ...image, url: image.url || imageURLs.get(imageKey(row, index)) }))
}
function loadImages(next: PreviewChatRow[]) {
  for (const row of next) {
    for (const [index, image] of (row.images || []).entries()) {
      const key = imageKey(row, index)
      if (!isImageAttachment(image) || imageRequests.has(key)) continue
      imageRequests.add(key)
      void client.image(row.id, index, controller.signal).then(blob => {
        if (!controller.signal.aborted) imageURLs.set(key, URL.createObjectURL(blob))
      }).catch(() => { imageRequests.delete(key) })
    }
  }
}
watch(rows, () => scrollDown(), { deep: true })
let renew: ReturnType<typeof setTimeout> | undefined
let poll: ReturnType<typeof setTimeout> | undefined
let readingMain = false
let socket: WebSocket | undefined
let messageNumber = 0
let guestReplyId = ''

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
    rows.value = next.rows
    loadImages(next.rows)
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
    const reply = rows.value.find(row => row.id === guestReplyId)
    if (reply && (message.type === 'reply' || message.type === 'done')) {
      reply.text = message.text || ''
      reply.streaming = message.type === 'reply'
      if (message.type === 'done') reply.at = new Date().toISOString()
    }
    if (message.type === 'done' || message.type === 'error') {
      if (reply) reply.streaming = false
      busy.value = false
      if (message.failed || message.type === 'error') messageError.value = t('pages.appPreview.share.sendFailed')
    }
  }
  current.onclose = () => {
    if (socket !== current || controller.signal.aborted) return
    guestModelReply?.(false)
    chatReady.value = false
    busy.value = false
    const reply = rows.value.find(row => row.id === guestReplyId)
    if (reply) reply.streaming = false
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
  const images = attach.attachments.value.slice()
  const references = mention.refs.value
  if (!canSubmit.value || !chatReady.value || sending.value || guestModelChanging.value || !session.value) return
  if (attach.blockSendIfOversized(images)) return
  if (images.length > 16 || images.reduce((total, image) => total + attachmentByteLength(image), 0) > SITE_ATTACH_MAX_BYTES) {
    messageError.value = t('pages.appPreview.share.attachmentLimit')
    return
  }
  if (new TextEncoder().encode(draft).byteLength > 8192) {
    messageError.value = t('pages.appPreview.share.messageTooLong')
    return
  }
  const message: PreviewChatMessage = {
    text: draft, annotations: selected, images,
    references: references.map(({ kind, target }) => ({ kind, target })),
  }
  messageError.value = ''
  if (session.value.chatMode === 'isolated') {
    if (busy.value || socket?.readyState !== WebSocket.OPEN) return
    try {
      socket.send(JSON.stringify(message))
    } catch {
      messageError.value = t('pages.appPreview.share.sendFailed')
      return
    }
    const id = ++messageNumber
    guestReplyId = `assistant-${id}`
    rows.value.push(
      { id: `user-${id}`, role: 'user', text: draft, annotations: selected, images, references, at: new Date().toISOString() },
      { id: guestReplyId, role: 'assistant', text: '', streaming: true },
    )
    scrollDown(true)
    mention.clear()
    attach.clearAttachments()
    picks.value = []
    busy.value = true
    void nextTick(() => textarea.value?.focus())
    return
  }
  sending.value = true
  try {
    await client.send(message, controller.signal)
    if (input.value.trim() === draft) mention.clear()
    attach.attachments.value = attach.attachments.value.filter(image => !images.includes(image))
    picks.value = picks.value.filter(pick => !selected.includes(pick))
    busy.value = true
    scrollDown(true)
    void nextTick(() => textarea.value?.focus())
    void pollMain()
  } catch (error) {
    if ((error as { status?: number }).status === 410) expire()
    else if (!controller.signal.aborted) messageError.value = error instanceof Error && error.message ? error.message : t('pages.appPreview.share.sendFailed')
  } finally {
    sending.value = false
  }
}

onMounted(loadSession)
onBeforeUnmount(() => {
  controller.abort()
  for (const url of imageURLs.values()) URL.revokeObjectURL(url)
  clearTimeout(renew)
  clearTimeout(poll)
  guestModelReply?.(false)
  socket?.close()
})
</script>

<template>
  <section v-if="session" class="chat-pane flex min-h-0 min-w-0 flex-1 flex-col bg-surface" data-testid="shared-app-chat">
    <div class="border-b border-line px-4 py-3 text-xs leading-relaxed text-txt2">
      <span class="mr-1 font-medium text-txt">{{ t(`pages.appPreview.share.${session.chatMode}`) }}</span>
      {{ t(`pages.appPreview.share.${session.chatMode}GuestHint`) }}
    </div>
    <div ref="scroller" class="chat-pane-transcript chat-thread scroll-area min-h-0 flex-1 overflow-y-auto" role="log" @scroll.passive="onScroll">
      <div class="chat-pane-column flex flex-col gap-6">
        <p v-if="!rows.length && !messageError" class="text-sm text-txt3">
          {{ t(chatReady ? 'pages.appPreview.share.emptyChat' : 'pages.embedChat.connecting') }}
        </p>
        <template v-for="row in rows" :key="row.id">
          <div v-if="row.role === 'user' || row.role === 'human'" class="flex justify-end">
            <div class="min-w-0 max-w-[85%]">
              <ChatAttachments v-if="row.images?.length" :images="shownAttachments(row)" sent @preview="imagePreview.openChatImagePreview" />
              <ChatUserMessage v-if="row.text || row.annotations?.length" :text="row.text" :annotations="row.annotations" :references="row.references" :created-at="row.at" />
              <div v-if="row.at" class="chat-meta mt-1.5 text-right">{{ relTime(row.at) }}</div>
            </div>
          </div>
          <AgentTimeline
            v-else-if="row.role === 'assistant' || row.role === 'agent'"
            :text="row.text"
            :streaming="row.streaming"
            :completed="!row.streaming"
            :metadata="row.at ? { createdAt: row.at } : undefined"
            @reveal="scrollDown()"
          />
          <p v-else class="text-xs text-txt3">{{ row.text }}</p>
        </template>
        <div v-if="busy" role="status">
          <ThinkingBusyCue :label="t('pages.solo.phase.running')" />
        </div>
      </div>
    </div>
    <div class="chat-pane-composer shrink-0 space-y-2 py-3">
      <div v-if="messageError" class="flex items-center gap-2 text-xs text-err" role="alert">
        <span class="flex-1">{{ messageError }}</span>
        <AppButton v-if="!chatReady && session.chatMode === 'isolated'" size="sm" type="button" @click="retryGuest">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>
      <ChatComposer
        class="chat-pane-column"
        :draft="draft"
        :picks="picks"
        :placeholder="t('pages.solo.chat.placeholder', { agent: session.agentName || 'Agent' })"
        :scope="referenceScope"
        :scoped-search="searchReferences"
        :allow-solo="false"
        :disabled="!chatReady"
        test-prefix="shared-app-chat"
        pick-test-id="shared-app-chat-pick"
        @send="submit"
        @remove-pick="picks.splice($event, 1)"
        @preview="imagePreview.openChatImagePreview"
      >
        <template #model>
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
        <template #actions>
          <ComposerPrimaryAction
            kind="send"
            :label="t('pages.solo.chat.send')"
            :busy="sending"
            :disabled="!chatReady || sending || guestModelChanging || !canSubmit || (session.chatMode === 'isolated' && busy)"
            testid="shared-app-chat-send"
            @click="submit"
          />
        </template>
      </ChatComposer>
    </div>
    <ChatImagePreviewModal
      :open="!!imagePreview.preview.value"
      :src="imagePreview.preview.value?.src || ''"
      :label="imagePreview.preview.value?.label || ''"
      @close="imagePreview.closeChatImagePreview"
    />
  </section>
  <div v-else class="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center" role="status">
    <Icon :name="loading ? 'spinner' : 'alert'" :size="26" class="text-accent" />
    <p class="text-sm text-txt2">{{ t(loading ? 'pages.embedChat.connecting' : `pages.appPreview.share.${pageError || 'inactive'}`) }}</p>
    <AppButton v-if="pageError === 'network'" size="sm" @click="loadSession">{{ t('common.buttons.retry') }}</AppButton>
  </div>
</template>

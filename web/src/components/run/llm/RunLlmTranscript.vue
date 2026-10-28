<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import ClarifyChat from '../ClarifyChat.vue'
import ClarifyMessageThread from '../ClarifyMessageThread.vue'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'
import { currentAnchorId, priorRailItems, revealRailTarget, turnRailItems } from '../clarifyChatAnchors'
import { api } from '@/lib/api/api'
import type { LlmTranscriptResponse } from '@/lib/api/apiTypes'
import { buildLlmTranscript, carryInflight } from '@/lib/run/llmTranscript'
import { llmTranscriptThread } from '@/lib/run/llmTranscriptThread'
import { resolveNodeDisplayLabel } from '@/lib/run/resolveNodeDisplayLabel'
import { bootKey as makeBootKey, getBootRecord } from '@/lib/run/bootRecord'
import type { AcpEvent, ClarifyImage, ReactAnnotation, Run, WFNode } from '@/lib/shared/types'
import { useI18n } from 'vue-i18n'

const POLL_MS = 4000
const REFETCH_DEBOUNCE_MS = 600
const FOLLOW_SLACK_PX = 120

const props = defineProps<{
  run: Run
  nodes: WFNode[]
  liveEvents?: Record<string, AcpEvent[] | undefined>
}>()

defineEmits<{
  locate: [nodeId: string, execIdx: number]
  'open-artifacts': []
}>()

const { t, locale } = useI18n()

const chatRef = ref<{
  applyReviewFrame?: (frame: Record<string, unknown>) => boolean | void
  applyAcpEvents?: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
  isSessionBusy?: () => boolean
  discardLastQueued?: () => void
} | null>(null)

defineExpose({
  getNodeId: () => thread.value.conversational ? thread.value.current?.nodeId || '' : '',
  isChatReady: () => !!chatRef.value,
  applyReviewFrame: (frame: Record<string, unknown>) => chatRef.value?.applyReviewFrame?.(frame) ?? false,
  applyAcpEvents: (events: AcpEvent[] | undefined, nodeId?: string) =>
    chatRef.value?.applyAcpEvents?.(events, nodeId) ?? false,
  isSessionBusy: () => !!chatRef.value?.isSessionBusy?.(),
})

const transcript = ref<LlmTranscriptResponse | null>(null)
const loading = ref(false)
const loadError = ref(false)
let ctrl: AbortController | null = null

async function load() {
  const runId = props.run?.id
  if (!runId) return
  ctrl?.abort()
  const mine = new AbortController()
  ctrl = mine
  loading.value = !transcript.value
  try {
    const res = await api.llmTranscript(runId, { signal: mine.signal })
    if (ctrl !== mine) return
    transcript.value = carryInflight(transcript.value, res)
    loadError.value = false
  } catch {
    if (mine.signal.aborted) return
    loadError.value = true
  } finally {
    if (ctrl === mine) loading.value = false
  }
}

const runActive = computed(() => ['running', 'waiting_human', 'queued'].includes(props.run?.status))

watch(
  () => props.run?.id,
  () => {
    transcript.value = null
    loadError.value = false
    void load()
  },
  { immediate: true },
)

const runSignature = computed(() => {
  const r = props.run
  if (!r) return ''
  let events = 0
  for (const list of Object.values(r.nodeExecutions || {})) {
    for (const ex of list) events += ex.events?.length || 0
  }
  return `${r.status}|${r.trace?.length || 0}|${events}`
})
let debounce: ReturnType<typeof setTimeout> | undefined
watch(runSignature, () => {
  if (debounce) clearTimeout(debounce)
  debounce = setTimeout(() => void load(), REFETCH_DEBOUNCE_MS)
})

let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  poll = setInterval(() => {
    if (runActive.value && document.visibilityState !== 'hidden') void load()
  }, POLL_MS)
})
onBeforeUnmount(() => {
  ctrl?.abort()
  if (poll) clearInterval(poll)
  if (debounce) clearTimeout(debounce)
})

const nodeById = computed<Record<string, WFNode>>(() => {
  const map: Record<string, WFNode> = {}
  for (const node of props.nodes || []) map[node.id] = node
  return map
})

function nodeInfo(id: string) {
  const node = nodeById.value[id]
  return {
    label: node ? resolveNodeDisplayLabel(node.label, node.type, t, { nodeId: id }) : id,
    type: node?.type || 'agent',
  }
}

const items = computed(() => {
  void locale.value
  return buildLlmTranscript({ run: props.run, transcript: transcript.value, liveEvents: props.liveEvents, nodeInfo })
})
const thread = computed(() => llmTranscriptThread(props.run, items.value))

const bootRecordKey = computed(() => {
  const current = thread.value.current
  return current ? makeBootKey(props.run.id, current.nodeId, current.iteration) : ''
})
const bootSummary = computed(() => !!getBootRecord(bootRecordKey.value)?.live)
const bootStarting = computed(() => {
  const record = bootRecordKey.value ? getBootRecord(bootRecordKey.value) : null
  return !!record && !record.live
})

const scroller = ref<HTMLElement | null>(null)
const nearBottom = ref(true)
function onScroll() {
  const el = scroller.value
  if (!el) return
  nearBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < FOLLOW_SLACK_PX
}
const contentSignature = computed(() => {
  const turns = thread.value.currentTurns
  const last = turns[turns.length - 1]
  return `${thread.value.priors.length}|${turns.length}|${last?.text?.length || 0}|${last?.parts?.length || 0}`
})
watch(contentSignature, async () => {
  if (thread.value.conversational || !nearBottom.value) return
  await nextTick()
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
})

const railItems = computed(() => {
  void locale.value
  const options = { fallbackTitle: t('pages.clarify.myChoice') }
  const nodeId = thread.value.current?.nodeId || ''
  return [
    ...priorRailItems(thread.value.priors, options),
    ...turnRailItems(thread.value.currentTurns, index => currentAnchorId(nodeId, index), options),
  ]
})
function onMessageNavigate(id: string) {
  nearBottom.value = false
  revealRailTarget(scroller.value, id)
}

async function sendRequest(text: string, images: ClarifyImage[], annotations: ReactAnnotation[], clientMessageId?: string) {
  const current = thread.value.current
  if (!current) return false
  try {
    if (clientMessageId) await api.reactReply(props.run.id, current.nodeId, text, images, false, annotations, false, false, clientMessageId)
    else await api.reactReply(props.run.id, current.nodeId, text, images, false, annotations)
    return true
  } catch {
    return false
  }
}
async function onFinish() {
  const current = thread.value.current
  if (!current) return
  await api.reactReply(props.run.id, current.nodeId, '', [], true, [])
}

async function onSend(text: string, images: ClarifyImage[], annotations: ReactAnnotation[], clientMessageId?: string) {
  if (!await sendRequest(text, images, annotations, clientMessageId)) chatRef.value?.discardLastQueued?.()
}

async function onCancel() {
  const current = thread.value.current
  if (current) await api.reactCancel(props.run.id, current.nodeId)
}

async function onRetryLast() {
  const current = thread.value.current
  if (current) await api.reactReply(props.run.id, current.nodeId, '', [], false, [], true)
}

async function onQueueRemove(id: string | undefined) {
  const current = thread.value.current
  if (current && id) await api.reactQueueRemove(props.run.id, current.nodeId, id)
}

async function onQueueReorder(ids: string[]) {
  const current = thread.value.current
  if (current) await api.reactQueueReorder(props.run.id, current.nodeId, ids)
}
</script>

<template>
  <div class="chat-thread flex h-full min-h-0 min-w-0 w-full flex-col bg-surface" data-testid="run-llm-transcript">
    <template v-if="thread.conversational && thread.current">
      <ClarifyMessageThread
        v-if="loadError"
        class="shrink-0 px-6 pt-3"
        :run-id="run.id"
        :load-error="true"
        @retry="load"
      />
      <ClarifyChat
        ref="chatRef"
        class="min-h-0 flex-1"
        :run-id="run.id"
        :node-id="thread.current.nodeId"
        :iteration="thread.current.iteration"
        :turns="thread.currentTurns"
        :prior-segments="thread.priors"
        :system-notes="thread.currentNotes"
        :node-label="thread.current.label"
        :active="true"
        :done="false"
        :review-mode="run.currentNode?.kind === 'review'"
        :send-request="sendRequest"
        @send="onSend"
        @cancel="onCancel"
        @retry-last="onRetryLast"
        @queue-remove="onQueueRemove"
        @queue-reorder="onQueueReorder"
        @finish="onFinish"
      />
    </template>
    <div v-else class="relative flex min-h-0 flex-1 flex-col">
    <ChatMessageRail
      :items="railItems"
      :scroller="scroller"
      :scope-key="`${run.id}:transcript`"
      @navigate="onMessageNavigate"
    />
    <div
      ref="scroller"
      class="scroll-area min-h-0 flex-1 overflow-y-auto"
      data-testid="clarify-scroller"
      @scroll.passive="onScroll"
    >
      <ClarifyMessageThread
        :run-id="run.id"
        :prior-segments="thread.priors"
        :turns="thread.currentTurns"
        :current-notes="thread.currentNotes"
        :node-id="thread.current?.nodeId || ''"
        :node-label="thread.current?.label || ''"
        :boot-summary="bootSummary"
        :boot-starting="bootStarting"
        :boot-record-key="bootRecordKey"
        :load-error="loadError"
        :show-empty="!loading && !loadError && !thread.priors.length && !thread.currentTurns.length"
        @retry="load"
      />
    </div>
    </div>
  </div>
</template>

<style scoped>
[data-testid="clarify-scroller"] {
  padding: 24px 24px 56px 44px;
  scrollbar-gutter: stable;
}
</style>

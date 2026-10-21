<script setup lang="ts">
/**
 * The conversation message list: earlier-node folds, user bubbles, the agent
 * timeline, boot row and the thinking placeholder. ClarifyChat renders its
 * history through this component; the run LLM transcript renders the whole
 * thread through it. Do not keep a second copy of these rows.
 */
import { computed, inject, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AcpEvent, AgentPart, ClarifyImage, ClarifyTurn, ReactQuestion } from '@/lib/shared/types'
import { api } from '@/lib/api/api'
import { isImageAttachment } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { EMBED_IMAGE_PREVIEW_KEY } from '@/lib/inbox/embedImagePreview'
import { choiceEchoRows, restoreChoiceAnswers } from '@/lib/inbox/clarifyDemo'
import { replySpan } from '@/lib/inbox/turnSpan'
import { partsFromAcp } from '@/lib/run/acpTools'
import { bootKey as makeBootKey, getBootRecord } from '@/lib/run/bootRecord'
import { fmtTurnElapsed, relTime as formatRelTime } from '@/lib/shared/format'
import { renderChatMarkdown } from '@/lib/shared/markdown'
import type { ClarifyPriorSegment } from './ClarifyChat.vue'
import { nodeAsk, priorAsk } from './clarifyPrior'
import AgentTimeline from './AgentTimeline.vue'
import AgentToolGroup from './AgentToolGroup.vue'
import ChatImagePreviewModal from '../ui/ChatImagePreviewModal.vue'
import ChatTurnAttachments from './ChatTurnAttachments.vue'
import ChatUserMessage from './ChatUserMessage.vue'
import ClarifyBootLoader from './ClarifyBootLoader.vue'
import ClarifyHandoffBubble from './ClarifyHandoffBubble.vue'
import ClarifyThinkingRow from './ClarifyThinkingRow.vue'
import Icon from '../ui/Icon.vue'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'

type ChoiceRows = { q: string; answers: string[] }[] | null
type FormRows = { label: string; value: string }[] | null

const props = withDefaults(
  defineProps<{
    runId: string
    priorSegments?: ClarifyPriorSegment[]
    nodeLabel?: string
    nodeTask?: string
    /** Current visit's boot is live, so the divider carries 环境已就绪. */
    bootSummary?: boolean
    bootStarting?: boolean
    bootRecordKey?: string
    dividerTitle?: string
    /** History folds only. The host keeps drawing the open conversation. */
    priorsOnly?: boolean
    /** Open conversation under the folds (LLM transcript). */
    turns?: ClarifyTurn[]
    loadError?: boolean
    showEmpty?: boolean
    parseChoiceSummary?: (text: string) => ChoiceRows
    parseFormSummary?: (text: string) => FormRows
  }>(),
  {
    priorSegments: () => [],
    nodeLabel: '',
    nodeTask: '',
    bootSummary: false,
    bootStarting: false,
    bootRecordKey: '',
    dividerTitle: '',
    priorsOnly: false,
    turns: () => [],
    loadError: false,
    showEmpty: false,
  },
)

const emit = defineEmits<{ retry: [] }>()

const { t: translate } = useI18n()
const embedImagePreview = inject(EMBED_IMAGE_PREVIEW_KEY, null)
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

function attachmentLabel(images: ClarifyImage[], index: number): string {
  const named = images[index]?.name?.trim()
  if (named) return named
  if (images.length <= 1) return translate('common.chatImage.imageFallback')
  return translate('common.chatImage.imageFallbackN', { n: index + 1 })
}
function openTurnImagePreview(images: ClarifyImage[], index: number) {
  const im = images[index]
  if (!im || !isImageAttachment(im)) return
  const label = attachmentLabel(images, index)
  if (embedImagePreview) embedImagePreview.open(imgSrc(im), label)
  else openChatImagePreview(imgSrc(im), label)
}

const relClock = ref(0)
let relClockTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  relClockTimer = setInterval(() => {
    relClock.value++
  }, 30_000)
})
onBeforeUnmount(() => {
  if (relClockTimer) clearInterval(relClockTimer)
  autoLoadCtrl?.abort()
})
function relTime(iso: string): string {
  void relClock.value
  return formatRelTime(iso)
}

function autoMember(seg: ClarifyPriorSegment) {
  return { nodeId: seg.nodeId, iteration: seg.iteration, label: seg.label, at: seg.at, endedAt: seg.endedAt }
}
/**
 * Earlier nodes as transcript rows. Consecutive unattended (auto) nodes that
 * have no turns of their own fold into one row. A node that already carries
 * its transcript stays its own row so those bubbles are not dropped.
 */
const priorRows = computed<ClarifyPriorSegment[]>(() => {
  const rows: ClarifyPriorSegment[] = []
  for (const seg of props.priorSegments ?? []) {
    const last = rows[rows.length - 1]
    if (
      seg.kind === 'auto' &&
      !seg.turns.length &&
      last &&
      last.kind === 'auto' &&
      !last.turns.length &&
      last.status === 'completed' &&
      seg.status === 'completed'
    ) {
      rows[rows.length - 1] = {
        ...last,
        group: [...(last.group ?? [last.label]), seg.label],
        members: [...(last.members?.length ? last.members : [autoMember(last)]), autoMember(seg)],
        endedAt: seg.endedAt,
        nodeId: last.nodeId,
      }
      continue
    }
    rows.push(seg.kind === 'auto' ? { ...seg, members: seg.members?.length ? seg.members : [autoMember(seg)] } : seg)
  }
  return rows
})
function autoMembers(seg: ClarifyPriorSegment) {
  return seg.members?.length ? seg.members : [autoMember(seg)]
}
function priorExpandable(seg: ClarifyPriorSegment): boolean {
  if (seg.kind === 'auto') return true
  return (seg.kind === 'chat' || seg.kind === 'gate') && seg.turns.length > 0
}
function segSeconds(seg: ClarifyPriorSegment): number | null {
  if (!seg.endedAt || !seg.at) return null
  const sec = (Date.parse(seg.endedAt) - Date.parse(seg.at)) / 1000
  return Number.isFinite(sec) && sec >= 0 ? sec : null
}
function priorSegmentMeta(seg: ClarifyPriorSegment): string {
  const sec = segSeconds(seg)
  const time = sec == null ? '' : fmtTurnElapsed(sec)
  return time ? translate('pages.clarify.turnElapsed', { time }) : relTime(seg.at)
}
function priorSegmentTitle(seg: ClarifyPriorSegment): string {
  if (seg.kind === 'gate') {
    const decision = String(seg.decision || '').toLowerCase()
    return /^(reject|rejected|fail|failed|revise)$/.test(decision)
      ? translate('pages.clarify.segmentGateRejected', { name: seg.label })
      : translate('pages.clarify.segmentGate', { name: seg.label })
  }
  if (seg.kind === 'chat') return translate('pages.clarify.segmentChat', { name: seg.label, n: seg.turns.length })
  if (seg.group && seg.group.length > 1) return translate('pages.clarify.segmentAutoGroup', { names: seg.group.join('、') })
  return translate('pages.clarify.segmentAuto', { name: seg.label })
}

type AutoRecord = { status: 'loading' | 'ready' | 'error'; parts?: AgentPart[] }
const autoOpen = ref<Record<string, boolean>>({})
const autoRecords = ref<Record<string, AutoRecord>>({})
let autoLoadCtrl: AbortController | null = null
function priorRowKey(seg: ClarifyPriorSegment) {
  return `${seg.nodeId}:${seg.iteration}`
}
function priorDetailsOpen(seg: ClarifyPriorSegment) {
  return seg.kind === 'auto' ? !!autoOpen.value[priorRowKey(seg)] : seg.open
}
function autoRecordKey(member: { nodeId: string; iteration: number }) {
  return `${props.runId}:${member.nodeId}:${member.iteration}`
}
function autoRecord(member: { nodeId: string; iteration: number }): AutoRecord {
  return autoRecords.value[autoRecordKey(member)] ?? { status: 'loading' }
}
function recordParts(events: AcpEvent[] | undefined): AgentPart[] | undefined {
  const fromTimeline = partsFromAcp(events)
  if (fromTimeline?.length) return fromTimeline
  const parts: AgentPart[] = []
  for (const ev of events || []) {
    if ((ev.kind === 'thought' || ev.kind === 'message') && ev.text) parts.push({ kind: ev.kind, text: ev.text })
    else if (ev.kind === 'tool_call' && ev.title) parts.push({ kind: 'tool', title: ev.title, status: ev.status })
  }
  return parts.length ? parts : undefined
}
async function loadAutoRecords(seg: ClarifyPriorSegment) {
  if (!autoLoadCtrl) autoLoadCtrl = new AbortController()
  const signal = autoLoadCtrl.signal
  await Promise.all(autoMembers(seg).map(async (member) => {
    const key = autoRecordKey(member)
    const cur = autoRecords.value[key]
    if (cur?.status === 'ready' || cur?.status === 'loading') return
    autoRecords.value = { ...autoRecords.value, [key]: { status: 'loading' } }
    try {
      const res = await api.nodeEvents(props.runId, member.nodeId, { signal })
      if (signal.aborted) return
      if (res.unavailable) throw new Error(res.error || 'unavailable')
      autoRecords.value = { ...autoRecords.value, [key]: { status: 'ready', parts: recordParts(res.events) } }
    } catch {
      if (signal.aborted) return
      autoRecords.value = { ...autoRecords.value, [key]: { status: 'error' } }
    }
  }))
}
function onPriorToggle(seg: ClarifyPriorSegment, ev: Event) {
  if (seg.kind !== 'auto') return
  const open = (ev.currentTarget as HTMLDetailsElement).open
  const key = priorRowKey(seg)
  if (!!autoOpen.value[key] !== open) autoOpen.value = { ...autoOpen.value, [key]: open }
  if (open && !seg.turns.length) void loadAutoRecords(seg)
}
function choiceRowsOf(text: string): ChoiceRows {
  return props.parseChoiceSummary?.(text) ?? null
}
function formRowsOf(text: string): FormRows {
  return props.parseFormSummary?.(text) ?? null
}
function priorChoiceEchoes(turns: ClarifyTurn[], index: number) {
  const rows = choiceRowsOf(turns[index]?.text || '')
  if (!rows) return []
  const questions: ReactQuestion[] = []
  for (let j = index - 1; j >= 0; j--) {
    const turn = turns[j]
    if (turn?.role === 'agent' && turn.questions?.length) {
      questions.push(...turn.questions)
      break
    }
  }
  return choiceEchoRows(rows, questions)
}
function priorSelectedLabels(turns: ClarifyTurn[], agentIndex: number, question: ReactQuestion): string[] {
  for (let j = agentIndex + 1; j < turns.length; j++) {
    const turn = turns[j]
    if (!turn || turn.role !== 'human') {
      if (turn?.questions?.length) break
      continue
    }
    const parsed = choiceRowsOf(turn.text)
    if (!parsed) continue
    const row = parsed.find((item) => item.q === question.prompt)
    if (!row) return []
    return restoreChoiceAnswers(row.answers, question.options.map((option) => option.label))
  }
  return []
}
function priorOptionSelected(turns: ClarifyTurn[], agentIndex: number, question: ReactQuestion, label: string) {
  return priorSelectedLabels(turns, agentIndex, question).includes(label)
}
function askFor(nodeId: string, iteration: number) {
  return priorAsk(props.priorSegments || [], nodeId, iteration)
}
function priorReplySpan(seg: ClarifyPriorSegment, index: number) {
  return replySpan(seg.turns[index - 1], seg.turns[index]!, seg.at)
}
function visitBootKey(nodeId: string, iteration: number) {
  return makeBootKey(props.runId, nodeId, iteration)
}
const resolvedDividerTitle = computed(() =>
  props.dividerTitle || (props.nodeLabel ? translate('pages.clarify.handoffDoneTo', { name: props.nodeLabel }) : translate('pages.clarify.handoffDone')),
)
const currentAsk = computed(() => nodeAsk(props.priorSegments || [], (props.priorSegments || []).length, props.nodeTask))
const hasPriors = computed(() => (props.priorSegments?.length || 0) > 0)

function turnParts(turn: ClarifyTurn): AgentPart[] {
  return turn.parts || []
}
function waitingTurn(turn: ClarifyTurn): boolean {
  return !!turn.streaming && !turn.thought && !String(turn.text || '').trim() && !turn.parts?.length && !turn.tools?.length
}
function tailOutputting(turn: ClarifyTurn): boolean {
  const parts = turn.parts || []
  for (let i = parts.length - 1; i >= 0; i--) {
    const part = parts[i]!
    if (part.kind === 'tool') return false
    if ((part.kind === 'message' || part.kind === 'thought') && part.text?.trim()) return part.kind === 'message'
  }
  return !!String(turn.text || '').trim()
}
const firstAgentAt = computed(() => props.turns.findIndex((turn) => turn.role === 'agent'))
</script>

<template>
  <div :class="priorsOnly ? 'contents' : 'clarify-transcript'" data-testid="clarify-message-thread">
    <div
      v-if="loadError && !priorsOnly"
      class="chat-meta mt-1 flex min-w-0 items-start gap-2 py-1"
      role="alert"
      data-testid="clarify-empty-fail"
    >
      <Icon name="alert" :size="16" class="mt-0.5 shrink-0 text-err" />
      <div class="min-w-0 flex-1">
        <div class="text-[13px] font-medium text-txt" data-testid="clarify-empty-fail-title">
          {{ translate('pages.llmTranscript.loadFailed') }}
        </div>
        <button
          type="button"
          class="mt-1 text-[12px] font-medium text-txt2 hover:text-txt"
          data-testid="clarify-empty-fail-retry"
          @click="emit('retry')"
        >
          {{ translate('pages.clarify.retry') }}
        </button>
      </div>
    </div>
    <p
      v-if="showEmpty && !priorsOnly && !loadError"
      class="text-[12px] leading-relaxed text-txt2"
      data-testid="clarify-approve-empty-hint"
    >
      {{ translate('pages.clarify.clarifyEmptyHint') }}
    </p>

    <template v-if="priorSegments && priorSegments.length">
      <template v-for="seg in priorRows" :key="seg.nodeId + ':' + seg.iteration">
        <details
          v-if="priorExpandable(seg)"
          class="chat-thought chat-meta clarify-prior-segment w-full"
          data-testid="clarify-prior-segment"
          :data-node-id="seg.nodeId"
          :data-kind="seg.kind"
          :open="priorDetailsOpen(seg)"
          @toggle="onPriorToggle(seg, $event)"
        >
          <summary class="chat-thought-summary clarify-status-row" data-testid="clarify-prior-summary">
            <ThoughtSummaryStatus
              :completed="seg.status !== 'failed' && seg.status !== 'cancelled'"
              :interrupted="seg.status === 'failed' || seg.status === 'cancelled'"
              :title="priorSegmentTitle(seg)"
              :done-label="priorSegmentMeta(seg)"
              :interrupted-label="priorSegmentMeta(seg)"
            />
            <Icon name="chevron-down" :size="16" class="chat-thought-chevron" aria-hidden="true" />
          </summary>
          <div v-if="seg.kind !== 'auto' || priorDetailsOpen(seg)" class="clarify-prior-body flex flex-col gap-3 pb-1 pt-2">
            <template v-if="seg.kind === 'auto' && !seg.turns.length">
              <div
                v-for="member in autoMembers(seg)"
                :key="member.nodeId + ':' + member.iteration"
                data-testid="clarify-auto-record"
                :data-node-id="member.nodeId"
              >
                <ClarifyHandoffBubble v-if="askFor(member.nodeId, member.iteration)" class="mb-3" :ask="askFor(member.nodeId, member.iteration)!" />
                <ClarifyBootLoader phase="done" variant="inline" :boot-key="visitBootKey(member.nodeId, member.iteration)" />
                <p
                  v-if="autoRecord(member).status === 'error'"
                  class="text-[12px] leading-relaxed text-txt2"
                  data-testid="clarify-auto-record-status"
                >{{ translate('pages.clarify.priorRecordFailed') }}</p>
                <p
                  v-else-if="autoRecord(member).status === 'ready' && !autoRecord(member).parts?.length"
                  class="text-[12px] leading-relaxed text-txt2"
                  data-testid="clarify-auto-record-status"
                >{{ translate('pages.clarify.priorRecordEmpty') }}</p>
                <p
                  v-else-if="autoRecord(member).status !== 'ready'"
                  class="text-[12px] leading-relaxed text-txt2"
                  data-testid="clarify-auto-record-status"
                >{{ translate('pages.clarify.priorRecordLoading') }}</p>
                <AgentTimeline
                  v-else
                  :parts="autoRecord(member).parts!"
                  :completed="seg.status === 'completed'"
                  :interrupted="seg.status === 'failed' || seg.status === 'cancelled'"
                  :run-id="runId"
                  :started-at="member.at"
                  :ended-at="member.endedAt"
                  message-test-id="clarify-prior-agent-message"
                />
              </div>
            </template>
            <template v-else>
              <ClarifyHandoffBubble v-if="seg.kind === 'chat' && askFor(seg.nodeId, seg.iteration)" :ask="askFor(seg.nodeId, seg.iteration)!" />
              <ClarifyBootLoader
                v-if="seg.kind === 'chat' || (seg.kind === 'auto' && !!getBootRecord(visitBootKey(seg.nodeId, seg.iteration))?.live)"
                phase="done"
                variant="inline"
                :boot-key="visitBootKey(seg.nodeId, seg.iteration)"
              />
              <div
                v-for="(pt, pi) in seg.turns"
                :key="pi"
                class="flex"
                :class="pt.role === 'human' ? 'justify-end' : ''"
                :data-turn-role="pt.role"
              >
                <div
                  v-if="pt.role === 'human' && priorChoiceEchoes(seg.turns, pi).length"
                  class="chat-user-message chat-response-summary min-w-0 max-w-[88%]"
                  data-testid="clarify-prior-choice"
                >
                  <div class="chat-response-heading flex items-center gap-2 text-txt2">
                    <Icon name="check" :size="16" /> {{ translate('pages.clarify.myChoice') }}
                  </div>
                  <div
                    v-for="(echo, ri) in priorChoiceEchoes(seg.turns, pi)"
                    :key="ri"
                    class="chat-response-row min-w-0 max-w-full"
                  >
                    <div
                      v-if="echo.showPrompt"
                      class="chat-response-question mb-1 min-w-0 max-w-full break-words text-txt2 [overflow-wrap:anywhere]"
                    >{{ echo.prompt }}</div>
                    <div class="chat-response-answers flex min-w-0 max-w-full flex-wrap gap-x-2 gap-y-1">
                      <span
                        v-for="(answer, ai) in [...echo.cards.map((card) => card.label), ...echo.texts]"
                        :key="ai"
                        class="min-w-0 max-w-full break-words text-txt [overflow-wrap:anywhere]"
                        data-testid="clarify-prior-choice-answer"
                      >{{ answer }}</span>
                    </div>
                  </div>
                </div>
                <div
                  v-else-if="pt.role === 'human' && formRowsOf(pt.text)"
                  class="chat-user-message chat-response-summary min-w-0 max-w-[88%]"
                  data-testid="clarify-prior-form"
                >
                  <div class="chat-response-heading flex items-center gap-2 text-txt2">
                    <Icon name="ci" :size="16" /> {{ translate('pages.clarify.myForm') }}
                  </div>
                  <div
                    v-for="(row, ri) in formRowsOf(pt.text)!"
                    :key="ri"
                    class="chat-response-row min-w-0 max-w-full"
                  >
                    <div class="chat-response-question mb-1 min-w-0 break-words text-txt2 [overflow-wrap:anywhere]">{{ row.label }}</div>
                    <div class="min-w-0 break-words text-txt [overflow-wrap:anywhere] whitespace-pre-wrap">{{ row.value }}</div>
                  </div>
                </div>
                <ChatUserMessage
                  v-else-if="pt.role === 'human'"
                  class="max-w-[88%]"
                  :text="pt.text"
                />
                <div v-else class="min-w-0 w-full">
                  <AgentTimeline
                    v-if="pt.parts?.length"
                    :parts="pt.parts"
                    :completed="true"
                    :run-id="runId"
                    :started-at="priorReplySpan(seg, pi)?.startedAt"
                    :ended-at="priorReplySpan(seg, pi)?.endedAt"
                    message-test-id="clarify-prior-agent-message"
                  />
                  <template v-else>
                    <AgentToolGroup v-if="pt.tools?.length" :tools="pt.tools" :run-id="runId" />
                    <div v-if="pt.text?.trim()" class="chat-prose md w-full" v-html="renderChatMarkdown(pt.text)" />
                  </template>
                  <div
                    v-if="pt.questions?.length"
                    class="clarify-request clarify-request--history mt-2 min-w-0"
                    data-testid="clarify-prior-questions"
                  >
                    <div class="mb-2 flex items-center gap-2 text-[12px] font-medium text-txt2">
                      <Icon name="chat" :size="16" />
                      {{ translate('pages.clarify.questionsThisRound', { n: pt.questions.length }) }}
                      <span class="ml-auto text-[11px] font-normal text-txt2">{{ translate('pages.clarify.readonly') }}</span>
                    </div>
                    <div v-for="(q, qi) in pt.questions" :key="q.id || qi" class="mb-3 last:mb-0">
                      <div class="mb-1.5 min-w-0 text-[12px] leading-snug text-txt2 [overflow-wrap:anywhere]">
                        <span class="text-txt3">{{ qi + 1 }}.</span> {{ q.prompt }}
                      </div>
                      <div
                        v-for="o in q.options"
                        :key="o.id"
                        class="flex items-start gap-1.5 py-0.5 text-[12px]"
                        data-testid="clarify-prior-option"
                        :data-selected="priorOptionSelected(seg.turns, pi, q, o.label) ? 'true' : 'false'"
                      >
                        <Icon
                          v-if="priorOptionSelected(seg.turns, pi, q, o.label)"
                          name="check"
                          :size="12"
                          class="mt-0.5 shrink-0 text-accent"
                        />
                        <span v-else class="mt-0.5 inline-block h-3 w-3 shrink-0" aria-hidden="true" />
                        <span
                          class="min-w-0 break-words [overflow-wrap:anywhere]"
                          :class="priorOptionSelected(seg.turns, pi, q, o.label) ? 'text-accent' : 'text-txt2'"
                        >{{ o.label }}</span>
                      </div>
                    </div>
                  </div>
                  <div
                    v-if="pt.forms?.length"
                    class="clarify-request clarify-request--history mt-2 min-w-0"
                    data-testid="clarify-prior-forms"
                  >
                    <div class="mb-2 flex items-center gap-2 text-[12px] font-medium text-txt2">
                      <Icon name="ci" :size="16" />
                      {{ translate('pages.clarify.formsThisRound', { n: pt.forms.length }) }}
                      <span class="ml-auto text-[11px] font-normal text-txt2">{{ translate('pages.clarify.readonly') }}</span>
                    </div>
                    <div v-for="(form, fi) in pt.forms" :key="fi" class="mb-2 last:mb-0">
                      <div v-if="form.title" class="mb-1 break-words text-[13px] text-txt2 [overflow-wrap:anywhere]">{{ form.title }}</div>
                      <div
                        v-for="field in form.fields"
                        :key="field.name"
                        class="mb-1 break-words text-[12px] text-txt2 [overflow-wrap:anywhere]"
                      >{{ field.label }}</div>
                    </div>
                  </div>
                </div>
              </div>
            </template>
          </div>
        </details>
        <div
          v-else
          class="clarify-handoff-step clarify-status-row chat-meta flex w-full min-w-0"
          data-testid="clarify-prior-segment"
          :data-node-id="seg.nodeId"
          :data-kind="seg.kind"
        >
          <ThoughtSummaryStatus
            :completed="seg.status !== 'failed' && seg.status !== 'cancelled'"
            :interrupted="seg.status === 'failed' || seg.status === 'cancelled'"
            :title="priorSegmentTitle(seg)"
            :done-label="priorSegmentMeta(seg)"
            :interrupted-label="priorSegmentMeta(seg)"
          />
        </div>
      </template>
      <div
        class="clarify-handoff-step clarify-status-row clarify-segment-divider chat-meta flex w-full min-w-0"
        data-testid="clarify-segment-divider"
        :role="bootSummary ? undefined : 'separator'"
        :data-boot="bootSummary ? 'merged' : undefined"
      >
        <ClarifyBootLoader
          v-if="bootSummary"
          phase="done"
          variant="inline"
          :boot-key="bootRecordKey"
          :title="resolvedDividerTitle"
        />
        <ThoughtSummaryStatus
          v-else
          :completed="true"
          :title="resolvedDividerTitle"
          :done-label="translate('pages.clarify.segmentCurrent')"
        />
      </div>
    </template>

    <template v-if="!priorsOnly">
      <ClarifyHandoffBubble v-if="currentAsk" class="clarify-turn" :ask="currentAsk" />
      <ClarifyBootLoader
        v-if="bootStarting && bootRecordKey && getBootRecord(bootRecordKey)"
        phase="starting"
        variant="inline"
        :boot-key="bootRecordKey"
      />
      <template v-for="(turn, index) in turns" :key="index">
        <ClarifyBootLoader
          v-if="bootSummary && !hasPriors && index === firstAgentAt"
          phase="done"
          variant="inline"
          :boot-key="bootRecordKey"
        />
        <div
          class="clarify-turn flex"
          :class="turn.role === 'human' ? 'justify-end' : ''"
          :data-turn-role="turn.role"
        >
          <div class="min-w-0" :class="turn.role === 'human' ? 'max-w-[88%]' : 'w-full'">
            <ChatTurnAttachments
              v-if="turn.role === 'human' && turn.images?.length"
              :images="turn.images"
              role="human"
              @preview="openTurnImagePreview(turn.images, $event)"
            />
            <ChatUserMessage
              v-if="turn.role === 'human' && turn.text"
              test-id="clarify-human-message"
              :text="turn.text"
            />
            <template v-else-if="turn.role === 'agent'">
              <ClarifyThinkingRow v-if="waitingTurn(turn)" />
              <AgentTimeline
                v-if="turnParts(turn).length"
                :parts="turnParts(turn)"
                :streaming="!!turn.streaming"
                :completed="!turn.streaming"
                :interrupted="!!turn.interrupted"
                :run-id="runId"
                :started-at="turn.startedAt"
                :ended-at="turn.endedAt"
                message-test-id="clarify-agent-message"
              />
              <ClarifyThinkingRow
                v-if="turn.streaming && !waitingTurn(turn)"
                variant="status"
                :outputting="tailOutputting(turn)"
              />
            </template>
          </div>
        </div>
      </template>
      <ClarifyBootLoader
        v-if="bootSummary && !hasPriors && firstAgentAt < 0"
        phase="done"
        variant="inline"
        :boot-key="bootRecordKey"
      />
    </template>
    <ChatImagePreviewModal
      v-if="!priorsOnly"
      :open="!!imagePreview"
      :src="imagePreview?.src || ''"
      :label="imagePreview?.label || ''"
      test-id-prefix="clarify-image-preview"
      @close="closeChatImagePreview"
    />
  </div>
</template>

<style scoped>
.clarify-transcript { width: 100%; max-width: 760px; margin-inline: auto; }
.clarify-turn { margin-top: 28px; }
.chat-response-heading { margin-bottom: 10px; font-size: 12px; font-weight: 500; }
.chat-response-row + .chat-response-row {
  margin-top: 10px;
  border-top: 1px solid rgb(var(--c-line) / .7);
  padding-top: 10px;
}
.chat-response-question { font-size: 12px; line-height: 1.65; }
.chat-response-answers { font-size: 14px; line-height: 1.75; }
.chat-response-answers > span + span::before {
  content: '·';
  margin-right: 8px;
  color: rgb(var(--c-txt2));
}
.clarify-request {
  max-width: 100%;
  border-left: 1px solid rgb(var(--c-line-strong) / .8);
  padding: 2px 0 2px 14px;
}
.clarify-request--history { border-color: rgb(var(--c-line)); }
.clarify-handoff-step { padding: 6px 8px; margin-inline: -8px; width: calc(100% + 16px); }
.clarify-status-row { margin-inline: -8px; padding: 6px 8px; width: calc(100% + 16px); }
.clarify-segment-divider { margin-top: 6px; padding-top: 10px; border-top: 1px solid rgb(var(--c-line) / .7); border-radius: 0; }
.clarify-prior-body { opacity: .72; }
.clarify-prior-body:hover { opacity: 1; }
</style>

<script lang="ts">
export type FeedbackAttachment = {
  ref?: string
  name?: string
  mimeType?: string
  sizeBytes?: number
}

export type FeedbackAnnotation = {
  jsonPath?: string
  selector?: string
  url?: string
  label?: string
  note?: string
  quote?: string
  truncated?: boolean
}

export type FeedbackTurn = {
  role?: string
  text?: string
  at?: string
  annotations?: FeedbackAnnotation[]
  attachments?: FeedbackAttachment[]
  interrupted?: boolean
}

export type FeedbackTarget = {
  name?: string
  before?: string
  after?: string
  changed?: boolean
}

export type FeedbackRoundDoc = {
  runId?: string
  kind?: string
  node?: { id?: string; label?: string; type?: string }
  iteration?: number
  round?: number
  seq?: number
  at?: string
  actor?: { name?: string; callerKind?: string; unattributable?: boolean }
  action?: string
  interrupted?: boolean
  /** Cumulative ReAct conclusion, covering every recorded round in this execution. */
  summary?: string
  roundCount?: number
  latestRound?: number
  /**
   * Agent-authored induction of the whole dialogue, produced once at
   * 「确认并流转」. Absent on legacy rounds and whenever the summary turn was
   * skipped or unparseable.
   */
  agentSummary?: string
  feedback?: {
    text?: string
    annotations?: FeedbackAnnotation[]
    attachments?: FeedbackAttachment[]
  }
  transcript?: FeedbackTurn[]
  targets?: FeedbackTarget[]
  priorRounds?: { round?: number; kind?: string; at?: string; summary?: string }[]
  prev?: string
  index?: string
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AnnotationChip from '../AnnotationChip.vue'
import ChatImageThumb from '../../ui/ChatImageThumb.vue'
import { blobContentUrl } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import { renderMarkdown } from '@/lib/shared/markdown'
import type { ReactAnnotation } from '@/lib/shared/types'

const props = defineProps<{ doc: FeedbackRoundDoc }>()

const { t } = useI18n()

const agentSummary = computed(() => (props.doc.agentSummary || '').trim())
const body = computed(() => (props.doc.summary || props.doc.feedback?.text || '').trim())
const annotations = computed(() => props.doc.feedback?.annotations || [])
const attachments = computed(() => props.doc.feedback?.attachments || [])
const transcript = computed(() => props.doc.transcript || [])
/** Only changed products are worth showing: unchanged rows are noise. */
const changedTargets = computed(() => (props.doc.targets || []).filter((x) => x.changed))

function attachmentSrc(a: FeedbackAttachment): string {
  return a.ref ? blobContentUrl(a.ref) : ''
}

function attachmentLabel(a: FeedbackAttachment, i: number): string {
  return a.name || `#${i + 1}`
}

/** AnnotationChip takes the platform ReactAnnotation shape; the JSON matches it. */
function asAnnotation(a: FeedbackAnnotation): ReactAnnotation {
  return a as ReactAnnotation
}

/** Same empty-body gate as ClarifyChat: blank / whitespace-only produces no md block. */
function turnBody(text: string | undefined): string {
  return (text || '').trim()
}
</script>

<template>
  <div class="space-y-3">
    <!-- Order (Demo): Agent 总结 → 全文 → 附件/受影响产物 → 本轮对话 → 此前各轮 -->
    <section
      v-if="agentSummary"
      data-testid="feedback-agent-summary"
      class="report-section"
    >
      <div class="mb-1.5 flex flex-wrap items-center gap-2">
        <h3 class="report-heading font-semibold text-txt3">
          {{ t('pages.product.feedback.agentSummary') }}
        </h3>
        <span class="rounded-md border border-info/40 bg-info/10 px-1.5 py-0.5 text-[11px] text-info">
          {{ t('pages.product.feedback.agentSummaryTag') }}
        </span>
      </div>
      <p class="whitespace-pre-wrap text-[13px] leading-relaxed text-txt">{{ agentSummary }}</p>
    </section>

    <section v-if="body || annotations.length" class="report-section">
      <h3 v-if="agentSummary && body" class="report-heading mb-1.5 font-semibold text-txt3">
        {{ t('pages.product.feedback.fullText') }}
      </h3>
      <div
        v-if="body"
        class="md break-words text-[13px] leading-relaxed text-txt"
        data-testid="feedback-round-body"
        v-html="renderMarkdown(body)"
      />
      <div v-if="annotations.length" class="mt-1.5 flex flex-wrap gap-1.5">
        <AnnotationChip v-for="(a, i) in annotations" :key="i" :ann="asAnnotation(a)" />
      </div>
    </section>

    <section v-if="attachments.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">
        {{ t('pages.product.feedback.attachments', { n: attachments.length }) }}
      </h3>
      <div class="flex flex-wrap gap-1.5">
        <ChatImageThumb
          v-for="(a, i) in attachments"
          :key="i"
          mode="previewable"
          :src="attachmentSrc(a)"
          :label="attachmentLabel(a, i)"
        />
      </div>
    </section>

    <section v-if="changedTargets.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">
        {{ t('pages.product.feedback.targets') }}
      </h3>
      <ul class="space-y-1">
        <li v-for="(x, i) in changedTargets" :key="i" class="flex flex-wrap items-center gap-1.5 text-[13px] text-txt2">
          <code class="font-mono text-[13px] text-txt">{{ x.name }}</code>
          <span class="text-txt3">{{ x.before || '—' }} → {{ x.after || '—' }}</span>
        </li>
      </ul>
    </section>

    <section v-if="transcript.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">
        {{ t('pages.product.feedback.transcript') }}
      </h3>
      <div class="space-y-1.5">
        <div
          v-for="(turn, i) in transcript"
          :key="i"
          class="report-transcript-entry"
          :class="turn.role === 'human' ? 'border-l-accent/50' : 'border-l-line'"
          :data-testid="`feedback-turn-${i}`"
        >
          <div class="mb-0.5 flex items-center gap-2">
            <span class="text-[11px] font-medium uppercase tracking-wide" :class="turn.role === 'human' ? 'text-accent-2' : 'text-txt3'">
              {{ turn.role === 'human' ? t('pages.product.feedback.roleHuman') : t('pages.product.feedback.roleAgent') }}
            </span>
            <span v-if="turn.interrupted" class="text-[11px] text-warn">{{ t('pages.product.feedback.interrupted') }}</span>
            <span v-if="turn.at" class="ml-auto text-[11px] text-txt3">{{ fmtTime(turn.at) }}</span>
          </div>
          <div
            v-if="turnBody(turn.text)"
            class="md break-words text-[13px] leading-relaxed text-txt2"
            data-testid="feedback-turn-body"
            v-html="renderMarkdown(turn.text || '')"
          />
        </div>
      </div>
    </section>

    <section v-if="doc.priorRounds?.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">
        {{ t('pages.product.feedback.priorRounds') }}
      </h3>
      <ul class="space-y-1">
        <li v-for="(p, i) in doc.priorRounds" :key="i" class="flex items-start gap-1.5 text-[13px] leading-5 text-txt3">
          <span class="shrink-0 text-txt3">{{ t('pages.product.feedback.roundN', { n: p.round }) }}</span>
          <span class="min-w-0 flex-1">{{ p.summary }}</span>
        </li>
      </ul>
    </section>
  </div>
</template>

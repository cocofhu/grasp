<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import StatusPill from '../../ui/StatusPill.vue'
import { NODE_DEFS, nodeColor } from '@/data/nodeRegistry'
import { fmtDuration } from '@/lib/shared/format'
import { fmtTokenCount, tokenUsageTotal } from '@/lib/run/tokenUsage'
import { fmtClock, type LlmTranscriptItem } from '@/lib/run/llmTranscript'

type SystemItem = Exclude<LlmTranscriptItem, { type: 'turn' }>

const DETAIL_PREVIEW = 160

const props = defineProps<{ item: SystemItem }>()
const emit = defineEmits<{ locate: [nodeId: string, execIdx: number] }>()

const { t } = useI18n()

const detailOpen = ref(false)

function iconOf(type: string) {
  return NODE_DEFS[type as keyof typeof NODE_DEFS]?.icon || 'agent'
}

const traceText = computed(() => {
  const it = props.item
  if (it.type !== 'trace') return ''
  const to = it.toLabel || it.to || ''
  switch (it.event) {
    case 'transition':
      return it.edgeKind === 'failure'
        ? t('pages.llmTranscript.trace.transitionFailure', { to })
        : t('pages.llmTranscript.trace.transition', { to })
    case 'rollback':
      return t('pages.llmTranscript.trace.rollback', { to })
    case 'pause':
      return t('pages.llmTranscript.trace.pause', { node: it.label })
    case 'resume':
      return t('pages.llmTranscript.trace.resume', { node: it.label })
    case 'exit':
      return t('pages.llmTranscript.trace.exit', { node: it.label })
    default:
      return t('pages.llmTranscript.trace.other', { node: it.label, event: it.event })
  }
})

const traceIcon = computed(() => {
  if (props.item.type !== 'trace') return 'dot'
  switch (props.item.event) {
    case 'transition':
      return 'chevron-right'
    case 'rollback':
      return 'history'
    case 'pause':
      return 'gate'
    case 'resume':
      return 'play'
    default:
      return 'dot'
  }
})

const traceTone = computed(() => {
  if (props.item.type !== 'trace') return ''
  if (props.item.event === 'pause') return 'text-warn'
  if (props.item.event === 'rollback' || props.item.edgeKind === 'failure') return 'text-err'
  return 'text-txt3'
})

const detail = computed(() => (props.item.type === 'trace' ? (props.item.detail || '').trim() : ''))
const detailLong = computed(() => detail.value.length > DETAIL_PREVIEW || detail.value.includes('\n'))
</script>

<template>
  <!-- Node divider: entering one execution of a node. -->
  <div v-if="item.type === 'node'" class="flex items-center gap-3 pt-3" data-testid="llm-node-divider" :data-node-id="item.nodeId">
    <div class="h-px flex-1 bg-line" />
    <div class="flex min-w-0 max-w-full flex-wrap items-center justify-center gap-x-2 gap-y-1 rounded-full border border-line bg-surface px-3 py-1 text-[12px]">
      <span
        class="flex h-5 w-5 shrink-0 items-center justify-center rounded"
        :style="{ background: nodeColor(item.nodeType as any, 0.13), color: nodeColor(item.nodeType as any) }"
      >
        <Icon :name="iconOf(item.nodeType)" :size="12" />
      </span>
      <span class="truncate font-semibold text-txt">{{ item.label }}</span>
      <span
        v-if="item.iteration > 1"
        class="shrink-0 rounded-full border border-warn/40 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
      >{{ t('common.iterationN', { n: item.iteration }) }}</span>
      <StatusPill :status="item.status" size="sm" />
      <span v-if="item.startedAt" class="tabular-nums text-[11px] text-txt3">{{ fmtClock(item.startedAt) }}</span>
      <span v-if="item.durationSec" class="tabular-nums text-[11px] text-txt3">{{ fmtDuration(item.durationSec) }}</span>
      <span v-if="item.turnCount" class="text-[11px] text-txt3">{{ t('pages.llmTranscript.turns', { n: item.turnCount }) }}</span>
      <span v-if="item.usage" class="font-mono text-[11px] tabular-nums text-txt3">
        {{ t('pages.llmTranscript.tokens', { n: fmtTokenCount(tokenUsageTotal(item.usage)) }) }}
      </span>
      <button
        type="button"
        class="inline-flex items-center gap-0.5 text-[11px] text-accent-2 hover:text-accent"
        data-testid="llm-locate"
        @click="emit('locate', item.nodeId, item.execIdx)"
      >
        {{ t('pages.llmTranscript.locate') }}<Icon name="chevron-right" :size="11" />
      </button>
    </div>
    <div class="h-px flex-1 bg-line" />
  </div>

  <!-- FSM trace: transition / rollback / pause / resume. -->
  <div v-else-if="item.type === 'trace'" class="flex justify-center" data-testid="llm-trace" :data-event="item.event">
    <div class="flex max-w-[min(720px,92%)] flex-col items-center gap-1">
      <div class="inline-flex items-center gap-1.5 text-[12px]" :class="traceTone">
        <Icon :name="traceIcon" :size="12" />
        <span>{{ traceText }}</span>
        <span class="tabular-nums text-[10px] text-txt3">{{ fmtClock(item.at) }}</span>
      </div>
      <div v-if="detail" class="max-w-full text-center text-[11px] text-txt3">
        <span class="whitespace-pre-wrap break-words">{{ detailOpen || !detailLong ? detail : detail.slice(0, DETAIL_PREVIEW).split('\n')[0] + '…' }}</span>
        <button
          v-if="detailLong"
          type="button"
          class="ml-1 text-accent-2 hover:text-accent"
          @click="detailOpen = !detailOpen"
        >{{ detailOpen ? t('pages.llmTranscript.collapsePrompt') : t('pages.llmTranscript.expandPrompt') }}</button>
      </div>
    </div>
  </div>

  <div
    v-else-if="item.type === 'error'"
    class="rounded-lg flex w-full min-w-0 max-w-full items-start gap-1.5 overflow-hidden border border-err/40 bg-err/[0.06] px-3 py-2 text-[12px] text-err"
    data-testid="llm-exec-error"
  >
    <Icon name="alert" :size="13" class="mt-0.5 shrink-0" />
    <span class="min-w-0 flex-1 whitespace-pre-wrap break-words [overflow-wrap:anywhere]"><b>{{ t('pages.llmTranscript.execError') }}</b> · {{ item.text }}</span>
  </div>
</template>

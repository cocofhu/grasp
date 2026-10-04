<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import { renderMarkdown, renderMarkdownBlocks, type MarkdownBlockCache } from '@/lib/shared/markdown'
import AgentToolGroup from '../AgentToolGroup.vue'
import AgentTimeline from '../AgentTimeline.vue'
import StreamMarkdown from '../StreamMarkdown.vue'
import { fmtDuration } from '@/lib/shared/format'
import { fmtTokenCount, tokenUsageTotal } from '@/lib/run/tokenUsage'
import type { LlmTurn } from '@/lib/run/llmTranscript'

const props = defineProps<{
  turn: LlmTurn
  models: string[]
  nodeLabel: string
  expandAll: boolean
  showThought: boolean
}>()

const emit = defineEmits<{ 'open-artifact': [] }>()

const { t } = useI18n()
const blockCache: MarkdownBlockCache = new Map()

/** Open detail sections, keyed `${answerIdx}:${section}` (section: thought|plan) or `mcp`. */
const open = ref<Set<string>>(new Set())
const openMcp = ref<Set<number>>(new Set())

function allKeys(): Set<string> {
  const s = new Set<string>(['mcp'])
  props.turn.answers.forEach((_, i) => {
    s.add(`${i}:plan`)
    if (props.showThought) s.add(`${i}:thought`)
  })
  return s
}

watch(
  () => props.expandAll,
  (v) => {
    open.value = v ? allKeys() : new Set()
  },
  { immediate: true },
)

function toggle(key: string) {
  const s = new Set(open.value)
  if (s.has(key)) s.delete(key)
  else s.add(key)
  open.value = s
}

function toggleMcp(i: number) {
  const s = new Set(openMcp.value)
  if (s.has(i)) s.delete(i)
  else s.add(i)
  openMcp.value = s
}

const durationSec = computed(() => {
  const a = props.turn.prompt?.at
  const b = props.turn.endedAt
  if (!a || !b) return null
  const d = (Date.parse(b) - Date.parse(a)) / 1000
  return Number.isFinite(d) && d >= 0 ? d : null
})

const hasReply = computed(() => props.turn.answers.some((a) => a.text || a.tools.length || a.plan || a.thought || a.parts?.length))
</script>

<template>
  <div class="flex min-w-0 w-full gap-2.5" data-testid="llm-answer">
    <span
      class="mt-5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-line-strong bg-elevated text-txt2"
      :title="t('pages.llmTranscript.speaker.agent')"
    >
      <Icon name="robot" :size="14" />
    </span>
    <div class="flex min-w-0 w-full max-w-full flex-1 flex-col">
      <div class="mb-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-txt3">
        <span class="font-medium text-txt2">{{ nodeLabel }}</span>
        <span v-for="m in models" :key="m" class="rounded border border-line bg-elevated px-1 py-px font-mono text-[10px]">{{ m }}</span>
        <span v-if="durationSec != null" class="tabular-nums">{{ t('pages.llmTranscript.duration', { time: fmtDuration(durationSec) }) }}</span>
        <span v-if="turn.usage" class="font-mono tabular-nums" data-testid="llm-turn-tokens">
          {{ t('pages.llmTranscript.tokens', { n: fmtTokenCount(tokenUsageTotal(turn.usage)) }) }}
          <span class="text-txt3">({{ fmtTokenCount(turn.usage.inputTokens || 0) }} / {{ fmtTokenCount(turn.usage.outputTokens || 0) }})</span>
        </span>
      </div>

      <div class="rounded-xl min-w-0 max-w-full overflow-x-auto space-y-2 rounded-tl-sm border border-line bg-surface px-3 py-2">
        <template v-for="(a, i) in turn.answers" :key="i">
          <div v-if="i > 0" class="border-t border-dashed border-line" />
          <AgentTimeline
            v-if="a.parts?.length"
            :parts="a.parts"
            :streaming="turn.live"
            :completed="!turn.live && !turn.failed"
            :interrupted="!turn.live && turn.failed"
            :hide-thought="!showThought"
            :expanded="expandAll"
            bare
            message-test-id="llm-answer-text"
          />
          <div v-if="a.thought && showThought && !a.parts?.length" class="text-[12px]">
            <button
              type="button"
              class="inline-flex items-center gap-1 text-txt3 hover:text-txt2"
              data-testid="llm-thought-toggle"
              @click="toggle(`${i}:thought`)"
            >
              <Icon name="sparkles" :size="12" />{{ t('pages.llmTranscript.thought') }}
              <Icon name="chevron-down" :size="11" :class="open.has(`${i}:thought`) ? 'rotate-180' : ''" />
            </button>
            <div
              v-if="open.has(`${i}:thought`)"
              class="mt-1 whitespace-pre-wrap border-l-2 border-line pl-2.5 italic leading-5 text-txt3"
              data-testid="llm-thought"
            >{{ a.thought }}</div>
          </div>
          <div v-if="a.plan" class="text-[12px]">
            <button type="button" class="inline-flex items-center gap-1 text-accent-2 hover:text-accent" @click="toggle(`${i}:plan`)">
              <Icon name="doc" :size="12" />{{ t('pages.llmTranscript.plan') }}
              <Icon name="chevron-down" :size="11" :class="open.has(`${i}:plan`) ? 'rotate-180' : ''" />
            </button>
            <pre v-if="open.has(`${i}:plan`)" class="m-0 mt-1 whitespace-pre-wrap font-mono text-[11px] leading-5 text-txt2">{{ a.plan }}</pre>
          </div>
          <AgentToolGroup v-if="a.tools.length && !a.parts?.length" :tools="a.tools" :busy="turn.live" :expanded="expandAll" />
          <div
            v-if="a.text && !a.parts?.length"
            class="md min-w-0 text-[13px] leading-6 text-txt"
            data-testid="llm-answer-text"
          >
            <StreamMarkdown v-if="turn.live" :blocks="renderMarkdownBlocks(a.text, blockCache)" />
            <span v-else v-html="renderMarkdown(a.text)" />
          </div>
          <div v-if="a.tools.some((tool) => tool.artifact)" class="flex flex-wrap gap-1.5">
            <button
              v-for="(tool, ti) in a.tools.filter((x) => x.artifact)"
              :key="ti"
              type="button"
              class="inline-flex items-center gap-1 rounded-md border border-n-artifact/40 bg-n-artifact/10 px-2 py-0.5 text-[11px] text-n-artifact hover:brightness-110"
              data-testid="llm-artifact-chip"
              @click="emit('open-artifact')"
            >
              <Icon name="artifact" :size="12" />{{ t('pages.llmTranscript.artifact') }} · {{ tool.artifact!.name }}
            </button>
          </div>
        </template>

        <div v-if="turn.live && !hasReply" class="flex items-center gap-2 text-[12px] text-txt3">
          <Icon name="spinner" :size="13" class="animate-spin text-accent" />{{ t('pages.llmTranscript.waitingAnswer') }}
        </div>
        <div v-else-if="turn.live" class="flex items-center gap-2 text-[11px] text-info">
          <span class="h-1.5 w-1.5 animate-pulseglow rounded-full bg-info" />{{ t('pages.llmTranscript.answering') }}
        </div>
        <div v-else-if="!hasReply && !turn.failed" class="text-[12px] italic text-txt3">{{ t('pages.llmTranscript.noReply') }}</div>

        <div v-if="turn.mcpCalls.length" class="rounded-md border border-line bg-base/60 p-1.5 font-mono text-[11px]">
          <button type="button" class="flex w-full items-center gap-1 text-txt3 hover:text-txt2" @click="toggle('mcp')">
            <Icon name="terminal" :size="12" class="text-info" />{{ t('pages.llmTranscript.mcp', { n: turn.mcpCalls.length }) }}
            <Icon name="chevron-down" :size="11" :class="open.has('mcp') ? 'rotate-180' : ''" />
          </button>
          <div v-if="open.has('mcp')" class="mt-1 space-y-1" data-testid="llm-mcp">
            <div v-for="(c, ci) in turn.mcpCalls" :key="ci" class="rounded border border-line/70">
              <button type="button" class="flex w-full items-center gap-1.5 px-1.5 py-1 text-left hover:bg-elevated" @click="toggleMcp(ci)">
                <span :class="c.isError ? 'text-err' : 'text-ok'">{{ c.isError ? '✗' : '✓' }}</span>
                <span class="font-semibold" :class="c.isError ? 'text-err' : 'text-info'">{{ c.tool }}</span>
                <span class="ml-auto truncate text-[10px] text-txt3">{{ c.args }}</span>
              </button>
              <div v-if="openMcp.has(ci)" class="space-y-1 border-t border-line/70 px-2 py-1.5">
                <div><span class="text-txt3">{{ t('pages.liveLog.args') }}</span> <span class="whitespace-pre-wrap break-all text-txt2">{{ c.args || t('common.emptyParen') }}</span></div>
                <div><span class="text-txt3">{{ t('pages.liveLog.result') }}</span> <span class="whitespace-pre-wrap break-all" :class="c.isError ? 'text-err' : 'text-txt2'">{{ c.result || t('common.emptyParen') }}</span></div>
              </div>
            </div>
          </div>
        </div>

        <div
          v-if="turn.failed"
          class="rounded-md flex items-start gap-1.5 border border-err/40 bg-err/[0.06] px-2.5 py-1.5 text-[12px] text-err"
          data-testid="llm-turn-failed"
        >
          <Icon name="alert" :size="13" class="mt-0.5 shrink-0" />
          <span class="whitespace-pre-wrap break-words"><b>{{ t('pages.llmTranscript.turnFailed') }}</b><template v-if="turn.error"> · {{ turn.error }}</template></span>
        </div>
      </div>
    </div>
  </div>
</template>

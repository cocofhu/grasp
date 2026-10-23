<script setup lang="ts">
/**
 * One agent reply as it happened: each thought run, tool group and message
 * run in arrival order. While the reply streams, the step being written stays
 * open. Once it stops, process folds into one elapsed line, a hairline, and
 * the last message under that line.
 */
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AgentPart } from '@/lib/shared/types'
import { timelineBlocks, timelineFolds } from '@/lib/run/acpTools'
import { thoughtParagraphs } from '@/lib/run/thoughtParagraphs'
import { createStreamTextReveal } from '@/lib/run/streamTextReveal'
import { fmtTurnElapsed, wallClockSec } from '@/lib/shared/format'
import { renderChatMarkdown, renderChatMarkdownBlocks, type MarkdownBlockCache } from '@/lib/shared/markdown'
import { createStreamMarkdownPreview } from '@/lib/shared/streamMarkdownPreview'
import AgentToolGroup from './AgentToolGroup.vue'
import StreamMarkdown from './StreamMarkdown.vue'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'
import Icon from '../ui/Icon.vue'

const props = withDefaults(
  defineProps<{
    parts: AgentPart[]
    /** The reply is still streaming. */
    streaming?: boolean
    /** Finished on its own (thought summaries show 已完成 while streaming). */
    completed?: boolean
    interrupted?: boolean
    /** Hide thought steps (LLM 过程 preference). The elapsed line stays. */
    hideThought?: boolean
    /** Parent "expand all": open the fold unless this page has pinned it. */
    expanded?: boolean
    /** data-testid of each message bubble. */
    messageTestId?: string
    /** Messages without their own bubble (the host already draws one). */
    bare?: boolean
    /** Run whose artifact stage tool rows link to. */
    runId?: string
    /** ISO when this reply started outputting. */
    startedAt?: string
    /** ISO when this reply stopped. */
    endedAt?: string
    /**
     * The host already shows one busy line (「思考中…」), so the streaming
     * thought summary drops its own 生成中… badge (observe view, before text).
     */
    quietThought?: boolean
  }>(),
  {
    streaming: false,
    completed: false,
    interrupted: false,
    hideThought: false,
    expanded: false,
    messageTestId: 'agent-timeline-message',
    bare: false,
    runId: undefined,
    startedAt: undefined,
    endedAt: undefined,
    quietThought: false,
  },
)

const emit = defineEmits<{ reveal: [] }>()

const { t } = useI18n()
const workId = `agent-elapsed-${useId().replace(/:/g, '')}`
const cache: MarkdownBlockCache = new Map()
const blocks = computed(() => timelineBlocks(props.parts).filter((b) => !(props.hideThought && b.kind === 'thought')))

const elapsedTime = computed(() => {
  const sec = wallClockSec(props.startedAt, props.endedAt)
  if (sec == null) return ''
  const compact = fmtTurnElapsed(sec)
  return compact ? t('pages.clarify.turnElapsed', { time: compact }) : ''
})
const failedCount = computed(() => {
  let n = 0
  for (const b of blocks.value) {
    if (b.kind !== 'tools') continue
    for (const tool of b.tools) if ((tool.status || '').toLowerCase() === 'failed') n++
  }
  return n
})
/**
 * A finished turn folds into the elapsed line (#843). Failed tool calls fold
 * too; the line says how many failed, and opening it shows them expanded.
 */
const settled = computed(() => !props.streaming && timelineFolds(props.parts))
const statusLabel = computed(() => {
  if (!settled.value) return ''
  if (props.interrupted) return t('pages.clarify.thoughtInterrupted')
  return ''
})
const failedLabel = computed(() => (settled.value && failedCount.value ? t('pages.clarify.toolFailedCount', { n: failedCount.value }) : ''))
/** Shown in place of the time when the reply's start/end are unknown, so the line never is a bare chevron. */
const processLabel = computed(() => (elapsedTime.value ? '' : t('pages.clarify.turnProcess')))
const elapsedName = computed(() =>
  [elapsedTime.value || processLabel.value, failedLabel.value, statusLabel.value].filter(Boolean).join(' '),
)

/**
 * Settled reply: the last non-empty message sits under the elapsed rule,
 * even when tools follow it. Earlier messages stay in the fold, once.
 * Failed-tool counts still come from every tool block, including those after the reply.
 */
const tailBlock = computed(() => {
  if (!settled.value) return null
  for (let i = blocks.value.length - 1; i >= 0; i--) {
    const block = blocks.value[i]
    if (block?.kind === 'message') return block
  }
  return null
})
const workBlocks = computed(() => {
  if (!settled.value) return []
  const tail = tailBlock.value
  if (!tail) return blocks.value
  return blocks.value.filter((block) => block.key !== tail.key)
})

/** null follows expand-all; a click pins this reply for the rest of the page. */
const manualOpen = ref<boolean | null>(null)
watch(
  () => props.expanded,
  () => {
    manualOpen.value = null
  },
)
const processOpen = computed(() => (manualOpen.value === null ? props.expanded : manualOpen.value))
function toggleProcess() {
  manualOpen.value = !processOpen.value
}

const liveBlocks = ref<string[]>([])
const liveThought = ref('')
const preview = createStreamMarkdownPreview<string[]>({
  render: (src) => renderChatMarkdownBlocks(src, cache),
  empty: [],
})
const unsubPreview = preview.subscribe((html) => {
  liveBlocks.value = html
  emit('reveal')
})
const syncReveal = Boolean(import.meta.env.VITEST)
let liveKey = ''
let liveKind: 'thought' | 'message' | '' = ''
const reveal = createStreamTextReveal({
  sync: syncReveal,
  onReveal: (text) => {
    if (liveKind === 'thought') {
      liveThought.value = text
      emit('reveal')
      return
    }
    preview.setText(text)
    if (syncReveal) preview.flush()
  },
})

const lastText = computed(() => {
  const b = blocks.value[blocks.value.length - 1]
  return b && b.kind !== 'tools' ? { key: b.key, kind: b.kind, text: b.text } : null
})
watch(
  () => [lastText.value, props.streaming] as const,
  ([last, streaming], prev) => {
    if (!last || (!streaming && !prev?.[1])) return
    if (last.key !== liveKey) {
      reveal.reset()
      preview.reset()
      liveKey = last.key
      liveKind = last.kind
    }
    reveal.setTarget(last.text)
    if (!streaming) {
      reveal.flush()
      preview.flush()
    }
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  unsubPreview()
  reveal.stop()
  preview.reset()
})
const thoughtOpen = ref<Record<number, boolean>>({})

const openByDefault = (last: boolean) => props.expanded || (props.streaming && last)
const isOpen = (index: number, last: boolean) => thoughtOpen.value[index] ?? openByDefault(last)
// Browsers fire toggle for :open changes too; only a state that differs from the default is the user's.
function onToggle(index: number, last: boolean, e: Event) {
  const open = (e.target as HTMLDetailsElement).open
  const next = { ...thoughtOpen.value }
  if (open === openByDefault(last)) delete next[index]
  else next[index] = open
  thoughtOpen.value = next
}
</script>

<template>
  <div class="flex min-w-0 flex-col" data-testid="agent-timeline">
    <template v-if="settled">
      <button
        type="button"
        class="agent-elapsed"
        data-testid="agent-timeline-elapsed"
        :aria-expanded="processOpen"
        :aria-controls="processOpen ? workId : undefined"
        :aria-label="elapsedName"
        @click="toggleProcess"
      >
        <span v-if="elapsedTime" class="agent-elapsed-time tabular-nums" data-testid="agent-timeline-elapsed-time">{{ elapsedTime }}</span>
        <span v-else class="agent-elapsed-time" data-testid="agent-timeline-elapsed-process">{{ processLabel }}</span>
        <Icon name="chevron-right" :size="12" class="agent-elapsed-chevron" data-testid="agent-timeline-elapsed-chevron" aria-hidden="true" />
        <span v-if="failedLabel" class="agent-elapsed-failed" data-testid="agent-timeline-elapsed-failed">{{ failedLabel }}</span>
        <span v-if="statusLabel" class="agent-elapsed-status" data-testid="agent-timeline-elapsed-status">{{ statusLabel }}</span>
      </button>
      <div v-if="processOpen && workBlocks.length" :id="workId" class="agent-elapsed-work" data-testid="agent-timeline-work">
        <template v-for="b in workBlocks" :key="b.key">
          <AgentToolGroup v-if="b.kind === 'tools'" :tools="b.tools" :expanded="expanded" :run-id="runId" />
          <div v-else-if="b.kind === 'thought'" class="agent-elapsed-thought" data-testid="agent-timeline-thought">
            <p v-for="(para, pi) in thoughtParagraphs(b.text)" :key="pi">{{ para }}</p>
          </div>
          <div v-else class="chat-prose md agent-elapsed-note min-w-0" :data-testid="messageTestId">
            <span v-html="renderChatMarkdown(b.text)" />
          </div>
        </template>
      </div>
      <hr class="agent-elapsed-rule" data-testid="agent-timeline-rule" />
      <div v-if="tailBlock" class="chat-prose md min-w-0" :data-testid="messageTestId">
        <span v-html="renderChatMarkdown(tailBlock.text)" />
      </div>
    </template>
    <template v-else>
      <template v-for="b in blocks" :key="b.key">
        <AgentToolGroup v-if="b.kind === 'tools'" :tools="b.tools" :busy="streaming" :expanded="expanded" :run-id="runId" />
        <details
          v-else-if="b.kind === 'thought'"
          class="agent-thought chat-meta w-full min-w-0"
          data-testid="agent-timeline-thought"
          :open="isOpen(b.index, b.last)"
          @toggle="onToggle(b.index, b.last, $event)"
        >
          <summary class="agent-thought-summary">
            <ThoughtSummaryStatus
              :busy="streaming && b.last && !quietThought"
              :completed="!streaming && completed"
              :interrupted="!streaming && interrupted"
            />
            <Icon name="chevron-down" :size="12" class="agent-thought-chevron" aria-hidden="true" />
          </summary>
          <div class="agent-thought-body">
            <p v-for="(para, pi) in thoughtParagraphs(streaming && b.last ? liveThought : b.text)" :key="pi">{{ para }}</p>
          </div>
        </details>
        <div
          v-else
          class="chat-prose md mb-3 min-w-0 last:mb-0"
          :data-testid="messageTestId"
        >
          <StreamMarkdown v-if="streaming && b.last" :blocks="liveBlocks" /><span
            v-else
            v-html="renderChatMarkdown(b.text)"
          /><slot v-if="streaming && b.last" name="caret" />
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.agent-elapsed {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  min-height: 24px;
  margin: 0;
  padding: 0;
  border: 0;
  background: none;
  color: rgb(var(--c-txt2));
  font: inherit;
  font-size: 13px;
  line-height: 1;
  letter-spacing: 0.01em;
  cursor: pointer;
}
.agent-elapsed:hover { color: rgb(var(--c-txt)); }
.agent-elapsed-failed { color: var(--chat-error, rgb(var(--c-err))); }
.agent-elapsed:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .45);
  outline-offset: 4px;
  border-radius: 4px;
}
.agent-elapsed-chevron {
  flex-shrink: 0;
  color: rgb(var(--c-txt2));
  transition: transform var(--dur-ui, 160ms);
}
.agent-elapsed:hover .agent-elapsed-chevron { color: rgb(var(--c-txt)); }
.agent-elapsed[aria-expanded='true'] .agent-elapsed-chevron { transform: rotate(90deg); }
.agent-elapsed-work { margin: 14px 0 2px; }
.agent-elapsed-thought {
  margin: 0 0 8px;
  color: rgb(var(--c-txt3));
  font-size: 12.5px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}
.agent-elapsed-thought p { margin: 0 0 8px; }
.agent-elapsed-thought p:last-child { margin-bottom: 0; }
.agent-elapsed-thought:last-child,
.agent-elapsed-note:last-child { margin-bottom: 4px; }
.agent-elapsed-note { margin-bottom: 18px; }
.agent-elapsed-rule {
  height: 1px;
  margin: 4px 0 8px;
  border: 0;
  background: rgb(var(--c-line));
}
.agent-thought { margin: 0 0 2px; color: var(--chat-muted, rgb(var(--c-txt2))); }
.agent-thought-summary {
  display: flex;
  min-width: 0;
  height: 20px;
  min-height: 20px;
  max-height: 20px;
  align-items: center;
  gap: 6px;
  border-radius: 4px;
  padding: 0 4px;
  cursor: pointer;
  list-style: none;
  user-select: none;
  font-size: 12px;
  line-height: 20px;
  font-weight: 500;
  transition: background-color var(--dur-ui), color var(--dur-ui);
}
.agent-thought-summary::-webkit-details-marker { display: none; }
.agent-thought-summary:hover { background: rgb(var(--c-elevated) / .6); color: rgb(var(--c-txt)); }
.agent-thought-summary:focus-visible { outline: 2px solid rgb(var(--c-accent) / .45); outline-offset: 2px; }
.agent-thought-summary :deep(.app-icon) { flex: none; width: 12px; height: 12px; }
.agent-thought-chevron { flex-shrink: 0; width: 12px; height: 12px; color: rgb(var(--c-txt3)); transition: transform var(--dur-ui); }
.agent-thought[open] > .agent-thought-summary .agent-thought-chevron { transform: rotate(180deg); }
.agent-thought-body {
  min-width: 0;
  margin: 2px 0 8px 13px;
  border-left: 1px solid rgb(var(--c-line) / .8);
  padding: 2px 6px 6px 15px;
  overflow-wrap: anywhere;
  color: rgb(var(--c-txt2));
  font-size: 12.5px;
  line-height: 1.45;
}
.agent-thought-body p { margin: 0 0 8px; }
.agent-thought-body p:last-child { margin-bottom: 0; }
@media (pointer: coarse) {
  .agent-elapsed,
  .agent-thought-summary { height: 44px; min-height: 44px; max-height: 44px; line-height: 44px; }
}
@media (prefers-reduced-motion: reduce) {
  .agent-elapsed-chevron,
  .agent-thought-summary,
  .agent-thought-chevron { transition: none; }
}
</style>

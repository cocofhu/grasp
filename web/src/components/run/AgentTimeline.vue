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
 * A finished turn folds into the elapsed line (#843) — except when a tool call
 * failed: then the steps stay inline exactly as before #843, so the failed
 * tool (its red status, title and detail) is visible without opening a fold.
 */
const settled = computed(() => !props.streaming && failedCount.value === 0 && timelineFolds(props.parts))
const statusLabel = computed(() => {
  if (!settled.value) return ''
  if (props.interrupted) return t('pages.clarify.thoughtInterrupted')
  return ''
})
const elapsedName = computed(
  () => [elapsedTime.value, statusLabel.value].filter(Boolean).join(' ') || t('pages.clarify.turnProcess'),
)

const tailBlock = computed(() => {
  if (!settled.value) return null
  const last = blocks.value[blocks.value.length - 1]
  return last?.kind === 'message' ? last : null
})
const workBlocks = computed(() => {
  if (!settled.value) return []
  return tailBlock.value ? blocks.value.slice(0, -1) : blocks.value
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
        <Icon name="chevron-right" :size="12" class="agent-elapsed-chevron" data-testid="agent-timeline-elapsed-chevron" aria-hidden="true" />
        <span v-if="statusLabel" class="agent-elapsed-status" data-testid="agent-timeline-elapsed-status">{{ statusLabel }}</span>
      </button>
      <div v-if="processOpen && workBlocks.length" :id="workId" class="agent-elapsed-work" data-testid="agent-timeline-work">
        <template v-for="b in workBlocks" :key="b.key">
          <AgentToolGroup v-if="b.kind === 'tools'" :tools="b.tools" :expanded="expanded" :run-id="runId" />
          <div v-else-if="b.kind === 'thought'" class="agent-elapsed-thought whitespace-pre-wrap" data-testid="agent-timeline-thought">{{ b.text }}</div>
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
          class="agent-thought chat-meta mb-3 w-full min-w-0"
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
            <Icon name="chevron-down" :size="16" class="agent-thought-chevron" aria-hidden="true" />
          </summary>
          <div class="agent-thought-body whitespace-pre-wrap">{{ streaming && b.last ? liveThought : b.text }}</div>
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
  margin: 0 0 18px;
  color: rgb(var(--c-txt3));
  font-size: 13px;
  line-height: 1.75;
  overflow-wrap: anywhere;
}
.agent-elapsed-thought:last-child,
.agent-elapsed-note:last-child { margin-bottom: 4px; }
.agent-elapsed-note { margin-bottom: 18px; }
.agent-elapsed-rule {
  height: 1px;
  margin: 4px 0 8px;
  border: 0;
  background: rgb(var(--c-line));
}
.agent-thought { color: var(--chat-muted, rgb(var(--c-txt2))); }
.agent-thought-summary {
  display: flex;
  min-width: 0;
  min-height: 32px;
  align-items: center;
  gap: 8px;
  border-radius: 6px;
  padding: 4px 6px;
  cursor: pointer;
  list-style: none;
  user-select: none;
  transition: background-color var(--dur-ui), color var(--dur-ui);
}
.agent-thought-summary::-webkit-details-marker { display: none; }
.agent-thought-summary:hover { background: rgb(var(--c-elevated) / .6); color: rgb(var(--c-txt)); }
.agent-thought-summary:focus-visible { outline: 2px solid rgb(var(--c-accent) / .45); outline-offset: 2px; }
.agent-thought-chevron { flex-shrink: 0; color: rgb(var(--c-txt3)); transition: transform var(--dur-ui); }
.agent-thought[open] > .agent-thought-summary .agent-thought-chevron { transform: rotate(180deg); }
.agent-thought-body {
  min-width: 0;
  margin: 3px 0 0 13px;
  border-left: 1px solid rgb(var(--c-line) / .8);
  padding: 3px 6px 8px 15px;
  overflow-wrap: anywhere;
  color: rgb(var(--c-txt2));
  font-size: 12.5px;
  line-height: 1.8;
}
@media (max-width: 767px) {
  .agent-elapsed,
  .agent-thought-summary { min-height: 44px; }
}
@media (prefers-reduced-motion: reduce) {
  .agent-elapsed-chevron,
  .agent-thought-summary,
  .agent-thought-chevron { transition: none; }
}
</style>

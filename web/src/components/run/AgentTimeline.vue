<script setup lang="ts">
/**
 * One agent reply as it happened: each thought run, tool group and message
 * run in arrival order. Consecutive tool calls fold into one AgentToolGroup;
 * only the thought being written stays open while the reply streams.
 * The block being written is revealed gradually and rendered at most once per
 * frame; settled blocks render their full text.
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { AgentPart } from '@/lib/shared/types'
import { timelineBlocks } from '@/lib/run/acpTools'
import { createStreamTextReveal } from '@/lib/run/streamTextReveal'
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
    /** Finished on its own (thought summaries show 已完成). */
    completed?: boolean
    interrupted?: boolean
    /** Hide thought steps (LLM 过程 preference). */
    hideThought?: boolean
    /** Parent "expand all": tool groups and thoughts follow it. */
    expanded?: boolean
    /** data-testid of each message bubble. */
    messageTestId?: string
    /** Messages without their own bubble (the host already draws one). */
    bare?: boolean
    /** Run whose artifact stage tool rows link to. */
    runId?: string
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
  },
)

const emit = defineEmits<{ reveal: [] }>()

const cache: MarkdownBlockCache = new Map()
const blocks = computed(() => timelineBlocks(props.parts).filter((b) => !(props.hideThought && b.kind === 'thought')))

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
            :busy="streaming && b.last"
            :completed="streaming ? !b.last : completed"
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
  </div>
</template>

<style scoped>
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
@media (max-width: 767px) { .agent-thought-summary { min-height: 44px; } }
@media (prefers-reduced-motion: reduce) {
  .agent-thought-summary,
  .agent-thought-chevron { transition: none; }
}
</style>

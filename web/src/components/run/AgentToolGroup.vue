<script setup lang="ts">
/**
 * Folded tool-call row of an agent turn: one line with an aggregate status,
 * "used N tools" and the first tool names; expands to every call + status.
 * Calls may carry a server-redacted summary and input/output (authenticated
 * pages only); a call with details expands on its own.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AgentTool } from '../../lib/shared/types'
import Icon from '../ui/Icon.vue'

const props = withDefaults(
  defineProps<{
    tools: AgentTool[]
    /** Whole turn still streaming; a "running" tool only spins while busy. */
    busy?: boolean
    /** Parent-driven open state (e.g. "expand all"); the head still toggles locally. */
    expanded?: boolean
  }>(),
  { busy: false, expanded: false },
)

const { t } = useI18n()
const open = ref(false)
const openRows = ref(new Set<number>())
function toggleRow(i: number) {
  const next = new Set(openRows.value)
  if (next.has(i)) next.delete(i)
  else next.add(i)
  openRows.value = next
}
watch(
  () => props.expanded,
  (v) => {
    open.value = v
  },
  { immediate: true },
)

type ToolState = 'running' | 'failed' | 'done'

function stateOf(tool: AgentTool): ToolState {
  const s = (tool.status || '').toLowerCase()
  if (s === 'failed') return 'failed'
  if (props.busy && (s === 'running' || s === 'pending' || s === 'in_progress')) return 'running'
  return 'done'
}

const rows = computed(() =>
  props.tools.map((tool) => ({
    title: tool.title,
    state: stateOf(tool),
    summary: tool.summary || '',
    input: tool.input || '',
    output: tool.output || '',
    details: !!(tool.input || tool.output),
  })),
)
const overall = computed<ToolState>(() => {
  if (rows.value.some((r) => r.state === 'running')) return 'running'
  if (rows.value.some((r) => r.state === 'failed')) return 'failed'
  return 'done'
})
const names = computed(() => {
  const seen: string[] = []
  for (const r of rows.value) if (!seen.includes(r.title)) seen.push(r.title)
  const head = seen.slice(0, 3).join(' · ')
  return seen.length > 3 ? `${head} …` : head
})
const label = (s: ToolState) =>
  s === 'running' ? t('pages.clarify.toolRunning') : s === 'failed' ? t('pages.clarify.toolFailed') : t('pages.clarify.toolDone')
</script>

<template>
  <div
    v-if="tools.length"
    class="mb-2 w-full rounded-md border border-line bg-base/60 text-[11.5px] text-txt3"
    data-testid="agent-tool-group"
    :data-state="overall"
  >
    <button
      type="button"
      class="flex w-full min-w-0 items-center gap-1.5 px-2.5 py-1.5 text-left hover:text-txt2"
      :aria-expanded="open"
      data-testid="agent-tool-group-head"
      @click="open = !open"
    >
      <Icon v-if="overall === 'running'" name="spinner" :size="12" class="shrink-0 text-accent-2" aria-hidden="true" />
      <Icon v-else-if="overall === 'failed'" name="alert" :size="12" class="shrink-0 text-warn" aria-hidden="true" />
      <Icon v-else name="check" :size="12" class="shrink-0 text-ok" aria-hidden="true" />
      <span class="shrink-0 text-txt2">{{ t('pages.clarify.toolsUsed', { n: tools.length }, tools.length) }}</span>
      <span class="min-w-0 flex-1 truncate font-mono text-[11px] text-txt3" data-testid="agent-tool-group-names">{{ names }}</span>
      <Icon
        name="chevron-down"
        :size="12"
        class="shrink-0 transition-transform duration-150"
        :class="{ 'rotate-180': open }"
        aria-hidden="true"
      />
    </button>
    <ul v-if="open" class="border-t border-dashed border-line px-2.5 py-1" data-testid="agent-tool-list">
      <li v-for="(r, i) in rows" :key="i" data-testid="agent-tool-row" :data-state="r.state">
        <component
          :is="r.details ? 'button' : 'div'"
          :type="r.details ? 'button' : undefined"
          class="flex w-full min-w-0 items-center gap-1.5 py-0.5 text-left"
          :class="{ 'hover:text-txt2': r.details }"
          :aria-expanded="r.details ? openRows.has(i) : undefined"
          :data-testid="r.details ? 'agent-tool-row-toggle' : undefined"
          @click="r.details && toggleRow(i)"
        >
          <Icon v-if="r.state === 'running'" name="spinner" :size="11" class="shrink-0 text-accent-2" aria-hidden="true" />
          <Icon v-else-if="r.state === 'failed'" name="alert" :size="11" class="shrink-0 text-warn" aria-hidden="true" />
          <Icon v-else name="check" :size="11" class="shrink-0 text-ok" aria-hidden="true" />
          <span class="shrink-0 font-mono font-semibold text-txt2" :class="{ 'min-w-0 flex-1 truncate font-normal': !r.summary }" :title="r.title">{{ r.title }}</span>
          <span
            v-if="r.summary"
            class="min-w-0 flex-1 truncate font-mono text-[11px] text-txt3"
            :title="r.summary"
            data-testid="agent-tool-summary"
          >{{ r.summary }}</span>
          <span
            class="shrink-0 text-[11px]"
            :class="{ 'text-accent-2': r.state === 'running', 'text-warn': r.state === 'failed', 'text-txt3': r.state === 'done' }"
          >{{ label(r.state) }}</span>
          <Icon
            v-if="r.details"
            name="chevron-down"
            :size="11"
            class="shrink-0 transition-transform duration-150"
            :class="{ 'rotate-180': openRows.has(i) }"
            aria-hidden="true"
          />
        </component>
        <div v-if="r.details && openRows.has(i)" class="mb-1 ml-4 flex flex-col gap-1.5" data-testid="agent-tool-detail">
          <div v-if="r.input">
            <div class="mb-0.5 text-[10.5px] text-txt3">{{ t('pages.clarify.toolInput') }}</div>
            <pre class="m-0 max-h-60 overflow-auto whitespace-pre-wrap rounded border border-line bg-surface px-2 py-1.5 font-mono text-[11px] leading-5 text-txt2 [overflow-wrap:anywhere]" data-testid="agent-tool-input">{{ r.input }}</pre>
          </div>
          <div v-if="r.output">
            <div class="mb-0.5 text-[10.5px] text-txt3">{{ t('pages.clarify.toolOutput') }}</div>
            <pre class="m-0 max-h-60 overflow-auto whitespace-pre-wrap rounded border border-line bg-surface px-2 py-1.5 font-mono text-[11px] leading-5 text-txt2 [overflow-wrap:anywhere]" data-testid="agent-tool-output">{{ r.output }}</pre>
          </div>
        </div>
      </li>
    </ul>
  </div>
</template>


<script setup lang="ts">
/**
 * Folded tool-call row of an agent turn: one line with an aggregate status,
 * "used N tools" and the first tool names; expands to every call + status.
 * Name and status only — tool input/output never reach the chat.
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

const rows = computed(() => props.tools.map((tool) => ({ title: tool.title, state: stateOf(tool) })))
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
      <li
        v-for="(r, i) in rows"
        :key="i"
        class="flex min-w-0 items-center gap-1.5 py-0.5"
        data-testid="agent-tool-row"
        :data-state="r.state"
      >
        <Icon v-if="r.state === 'running'" name="spinner" :size="11" class="shrink-0 text-accent-2" aria-hidden="true" />
        <Icon v-else-if="r.state === 'failed'" name="alert" :size="11" class="shrink-0 text-warn" aria-hidden="true" />
        <Icon v-else name="check" :size="11" class="shrink-0 text-ok" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate font-mono text-txt2" :title="r.title">{{ r.title }}</span>
        <span
          class="shrink-0 text-[11px]"
          :class="{ 'text-accent-2': r.state === 'running', 'text-warn': r.state === 'failed', 'text-txt3': r.state === 'done' }"
        >{{ label(r.state) }}</span>
      </li>
    </ul>
  </div>
</template>

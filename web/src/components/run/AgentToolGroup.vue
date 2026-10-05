<script setup lang="ts">
/**
 * Folded tool-call row of an agent turn: one line with an aggregate status,
 * "used N tools" and the first tool names (or the call still running);
 * expands to every call with its icon, readable label, status and duration.
 * Calls may carry a server-redacted summary and input/output (authenticated
 * pages only); a call with details expands on its own, a failed one opens by
 * default. Calls that wrote an artifact or registered a preview link to the
 * stage of the same run when one is mounted.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AgentTool } from '../../lib/shared/types'
import { TOOL_ICONS, formatToolDuration, toolMeta } from '../../lib/run/toolLabels'
import { stageLinksFor } from '../../lib/run/stageLinks'
import Icon from '../ui/Icon.vue'

const props = withDefaults(
  defineProps<{
    tools: AgentTool[]
    /** Whole turn still streaming; a "running" tool only spins while busy. */
    busy?: boolean
    /** Parent-driven open state (e.g. "expand all"); the head still toggles locally. */
    expanded?: boolean
    /** Run whose artifact stage "查看" / "打开预览" target. */
    runId?: string
  }>(),
  { busy: false, expanded: false, runId: undefined },
)

const { t } = useI18n()
const open = ref(false)
const rowOverride = ref(new Map<number, boolean>())
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

const stage = computed(() => stageLinksFor(props.runId))

const rows = computed(() =>
  props.tools.map((tool) => {
    const meta = toolMeta(tool)
    const state = stateOf(tool)
    const label = meta.labelKey ? t(`pages.clarify.tools.${meta.labelKey}`) : tool.title
    const links = state === 'done' ? stage.value : null
    return {
      title: tool.title,
      label,
      icon: TOOL_ICONS[meta.kind],
      state,
      summary: tool.summary || '',
      input: tool.input || '',
      output: tool.output || '',
      duration: formatToolDuration(tool.durationMs),
      details: !!(tool.input || tool.output),
      artifact: meta.artifact && links?.hasArtifact(meta.artifact) ? meta.artifact : '',
      preview: !!(meta.preview && links?.canOpenPreview()),
    }
  }),
)
type Row = (typeof rows.value)[number]

const rowOpen = (r: Row, i: number) => r.details && (rowOverride.value.get(i) ?? r.state === 'failed')
function toggleRow(r: Row, i: number) {
  const next = new Map(rowOverride.value)
  next.set(i, !rowOpen(r, i))
  rowOverride.value = next
}

const overall = computed<ToolState>(() => {
  if (rows.value.some((r) => r.state === 'running')) return 'running'
  if (rows.value.some((r) => r.state === 'failed')) return 'failed'
  return 'done'
})
const failedCount = computed(() => rows.value.filter((r) => r.state === 'failed').length)
const current = computed(() => {
  for (let i = rows.value.length - 1; i >= 0; i--) if (rows.value[i]!.state === 'running') return rows.value[i]!
  return null
})
const names = computed(() => {
  const seen: string[] = []
  for (const r of rows.value) if (!seen.includes(r.label)) seen.push(r.label)
  const head = seen.slice(0, 3).join(' · ')
  return seen.length > 3 ? `${head} …` : head
})
const label = (s: ToolState) =>
  s === 'running' ? t('pages.clarify.toolRunning') : s === 'failed' ? t('pages.clarify.toolFailed') : t('pages.clarify.toolDone')

function openArtifact(name: string) {
  stage.value?.openArtifact(name)
}
function openPreview() {
  stage.value?.openPreview()
}
</script>

<template>
  <div
    v-if="tools.length"
    class="mb-2 w-full rounded-md border bg-base/60 text-[11.5px] text-txt3"
    :class="overall === 'failed' ? 'border-warn/40' : 'border-line'"
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
      <Icon v-if="overall === 'running'" name="spinner" :size="12" class="shrink-0 animate-spin text-accent-2" aria-hidden="true" />
      <Icon v-else-if="overall === 'failed'" name="alert" :size="12" class="shrink-0 text-warn" aria-hidden="true" />
      <Icon v-else name="check" :size="12" class="shrink-0 text-ok" aria-hidden="true" />
      <span class="shrink-0 text-txt2">{{ t('pages.clarify.toolsUsed', { n: tools.length }, tools.length) }}</span>
      <span v-if="failedCount && overall === 'failed'" class="shrink-0 text-warn" data-testid="agent-tool-group-failed">{{
        t('pages.clarify.toolFailedCount', { n: failedCount })
      }}</span>
      <span v-if="current" class="min-w-0 flex-1 truncate text-[11px] text-accent-2" data-testid="agent-tool-group-current"
        >{{ current.label }}<span v-if="current.summary" class="font-mono"> · {{ current.summary }}</span></span
      >
      <span v-else class="min-w-0 flex-1 truncate text-[11px] text-txt3" data-testid="agent-tool-group-names">{{ names }}</span>
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
        <div class="flex min-w-0 items-center gap-1.5">
          <component
            :is="r.details ? 'button' : 'div'"
            :type="r.details ? 'button' : undefined"
            class="flex min-w-0 flex-1 items-center gap-1.5 py-0.5 text-left"
            :class="{ 'hover:text-txt2': r.details }"
            :aria-expanded="r.details ? rowOpen(r, i) : undefined"
            :data-testid="r.details ? 'agent-tool-row-toggle' : undefined"
            @click="r.details && toggleRow(r, i)"
          >
            <Icon v-if="r.state === 'running'" name="spinner" :size="11" class="shrink-0 animate-spin text-accent-2" aria-hidden="true" />
            <Icon v-else-if="r.state === 'failed'" name="alert" :size="11" class="shrink-0 text-warn" aria-hidden="true" />
            <Icon v-else :name="r.icon" :size="11" class="shrink-0 text-txt3" aria-hidden="true" data-testid="agent-tool-icon" />
            <span
              class="shrink-0 font-semibold text-txt2"
              :class="{ 'min-w-0 flex-1 truncate font-normal': !r.summary }"
              :title="r.title"
              data-testid="agent-tool-label"
              >{{ r.label }}</span
            >
            <span v-if="r.summary" class="min-w-0 flex-1 truncate font-mono text-[11px] text-txt3" :title="r.summary" data-testid="agent-tool-summary">{{
              r.summary
            }}</span>
            <span
              v-if="r.duration"
              class="shrink-0 font-mono text-[10.5px] tabular-nums text-txt3"
              :title="t('pages.clarify.toolDuration', { d: r.duration })"
              data-testid="agent-tool-duration"
              >{{ r.duration }}</span
            >
            <span
              class="shrink-0 text-[11px]"
              :class="{ 'text-accent-2': r.state === 'running', 'text-warn': r.state === 'failed', 'text-txt3': r.state === 'done' }"
              >{{ label(r.state) }}</span
            >
            <Icon
              v-if="r.details"
              name="chevron-down"
              :size="11"
              class="shrink-0 transition-transform duration-150"
              :class="{ 'rotate-180': rowOpen(r, i) }"
              aria-hidden="true"
            />
          </component>
          <button
            v-if="r.artifact"
            type="button"
            class="shrink-0 rounded px-1.5 py-px text-[11px] text-accent-2 hover:bg-accent-2/10"
            :title="r.artifact"
            data-testid="agent-tool-open-artifact"
            @click="openArtifact(r.artifact)"
          >
            {{ t('pages.clarify.toolOpenArtifact') }}
          </button>
          <button
            v-else-if="r.preview"
            type="button"
            class="shrink-0 rounded px-1.5 py-px text-[11px] text-accent-2 hover:bg-accent-2/10"
            data-testid="agent-tool-open-preview"
            @click="openPreview()"
          >
            {{ t('pages.clarify.toolOpenPreview') }}
          </button>
        </div>
        <div v-if="rowOpen(r, i)" class="mb-1 ml-4 flex flex-col gap-1.5" data-testid="agent-tool-detail">
          <div v-if="r.label !== r.title" class="text-[10.5px] text-txt3">
            {{ t('pages.clarify.toolRawName') }} <span class="font-mono" data-testid="agent-tool-raw-name">{{ r.title }}</span>
          </div>
          <div v-if="r.input">
            <div class="mb-0.5 text-[10.5px] text-txt3">{{ t('pages.clarify.toolInput') }}</div>
            <pre
              class="m-0 max-h-60 overflow-auto whitespace-pre-wrap rounded border border-line bg-surface px-2 py-1.5 font-mono text-[11px] leading-5 text-txt2 [overflow-wrap:anywhere]"
              data-testid="agent-tool-input"
              >{{ r.input }}</pre
            >
          </div>
          <div v-if="r.output">
            <div class="mb-0.5 text-[10.5px]" :class="r.state === 'failed' ? 'text-warn' : 'text-txt3'">{{ t('pages.clarify.toolOutput') }}</div>
            <pre
              class="m-0 max-h-60 overflow-auto whitespace-pre-wrap rounded border bg-surface px-2 py-1.5 font-mono text-[11px] leading-5 text-txt2 [overflow-wrap:anywhere]"
              :class="r.state === 'failed' ? 'border-warn/40' : 'border-line'"
              data-testid="agent-tool-output"
              >{{ r.output }}</pre
            >
          </div>
        </div>
      </li>
    </ul>
  </div>
</template>

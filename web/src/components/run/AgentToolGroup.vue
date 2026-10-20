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
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AgentTool } from '../../lib/shared/types'
import { TOOL_ICONS, formatToolDuration, toolMeta } from '../../lib/run/toolLabels'
import { stageLinksFor } from '../../lib/run/stageLinks'
import { useConversationArtifacts } from '@/lib/chat/conversationArtifacts'
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
const artifactWorkspace = useConversationArtifacts()
const listId = `agent-tools-${useId().replace(/:/g, '')}`
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

// A tester without a run must never pick up another conversation's stage.
const stage = computed(() => props.runId ? stageLinksFor(props.runId) : null)

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
  if (artifactWorkspace && props.runId) {
    void artifactWorkspace.open({ name, runId: props.runId })
    return
  }
  stage.value?.openArtifact(name)
}
function openPreview() {
  if (artifactWorkspace && props.runId) {
    void artifactWorkspace.open({ runId: props.runId, preview: true })
    return
  }
  stage.value?.openPreview()
}
</script>

<template>
  <div
    v-if="tools.length"
    class="agent-tools chat-meta mb-3 w-full min-w-0"
    data-testid="agent-tool-group"
    :data-state="overall"
  >
    <button
      type="button"
      class="agent-tools-head"
      :aria-expanded="open"
      :aria-controls="open ? listId : undefined"
      data-testid="agent-tool-group-head"
      @click="open = !open"
    >
      <Icon v-if="overall === 'running'" name="spinner" :size="16" class="agent-tool-icon agent-tool-running animate-spin" aria-hidden="true" />
      <Icon v-else-if="overall === 'failed'" name="alert" :size="16" class="agent-tool-icon agent-tool-failed" aria-hidden="true" />
      <Icon v-else name="check" :size="16" class="agent-tool-icon" aria-hidden="true" />
      <span class="agent-tools-count">{{ t('pages.clarify.toolsUsed', { n: tools.length }, tools.length) }}</span>
      <span v-if="failedCount && overall === 'failed'" class="agent-tools-count agent-tool-failed" data-testid="agent-tool-group-failed">{{
        t('pages.clarify.toolFailedCount', { n: failedCount })
      }}</span>
      <span v-if="current" class="agent-tools-context agent-tool-running" data-testid="agent-tool-group-current"
        >{{ current.label }}<span v-if="current.summary" class="font-mono"> · {{ current.summary }}</span></span
      >
      <span v-else class="agent-tools-context" data-testid="agent-tool-group-names">{{ names }}</span>
      <Icon
        name="chevron-down"
        :size="16"
        class="agent-tool-chevron"
        :class="{ 'rotate-180': open }"
        aria-hidden="true"
      />
    </button>
    <ul v-if="open" :id="listId" class="agent-tool-list" data-testid="agent-tool-list">
      <li v-for="(r, i) in rows" :key="i" data-testid="agent-tool-row" :data-state="r.state">
        <div class="agent-tool-line">
          <component
            :is="r.details ? 'button' : 'div'"
            :type="r.details ? 'button' : undefined"
            class="agent-tool-row-main"
            :aria-expanded="r.details ? rowOpen(r, i) : undefined"
            :aria-controls="rowOpen(r, i) ? `${listId}-${i}` : undefined"
            :data-testid="r.details ? 'agent-tool-row-toggle' : undefined"
            @click="r.details && toggleRow(r, i)"
          >
            <Icon v-if="r.state === 'running'" name="spinner" :size="16" class="agent-tool-icon agent-tool-running animate-spin" aria-hidden="true" />
            <Icon v-else-if="r.state === 'failed'" name="alert" :size="16" class="agent-tool-icon agent-tool-failed" aria-hidden="true" />
            <Icon v-else :key="r.icon" :name="r.icon" :size="16" class="agent-tool-icon" aria-hidden="true" data-testid="agent-tool-icon" />
            <span
              class="agent-tool-label"
              :class="{ 'agent-tool-label--summary': !!r.summary }"
              :title="r.title"
              data-testid="agent-tool-label"
              >{{ r.label }}</span
            >
            <span v-if="r.summary" class="agent-tool-summary font-mono" :title="r.summary" data-testid="agent-tool-summary">{{
              r.summary
            }}</span>
            <span
              v-if="r.duration"
              class="agent-tool-duration tabular-nums"
              :title="t('pages.clarify.toolDuration', { d: r.duration })"
              data-testid="agent-tool-duration"
              >{{ r.duration }}</span
            >
            <span
              class="agent-tool-state"
              :class="{ 'agent-tool-running': r.state === 'running', 'agent-tool-failed': r.state === 'failed' }"
              >{{ label(r.state) }}</span
            >
            <Icon
              v-if="r.details"
              name="chevron-down"
              :size="16"
              class="agent-tool-chevron"
              :class="{ 'rotate-180': rowOpen(r, i) }"
              aria-hidden="true"
            />
          </component>
          <button
            v-if="r.artifact"
            type="button"
            class="agent-tool-action"
            :title="r.artifact"
            data-testid="agent-tool-open-artifact"
            @click="openArtifact(r.artifact)"
          >
            {{ t('pages.clarify.toolOpenArtifact') }}
          </button>
          <button
            v-else-if="r.preview"
            type="button"
            class="agent-tool-action"
            data-testid="agent-tool-open-preview"
            @click="openPreview()"
          >
            {{ t('pages.clarify.toolOpenPreview') }}
          </button>
        </div>
        <div v-if="rowOpen(r, i)" :id="`${listId}-${i}`" class="agent-tool-detail" data-testid="agent-tool-detail">
          <div v-if="r.label !== r.title" class="agent-tool-raw-name">
            {{ t('pages.clarify.toolRawName') }} <span class="font-mono" data-testid="agent-tool-raw-name">{{ r.title }}</span>
          </div>
          <div v-if="r.input">
            <div class="agent-tool-detail-label">{{ t('pages.clarify.toolInput') }}</div>
            <pre
              class="agent-tool-code font-mono"
              data-testid="agent-tool-input"
              >{{ r.input }}</pre
            >
          </div>
          <div v-if="r.output">
            <div class="agent-tool-detail-label" :class="{ 'agent-tool-failed': r.state === 'failed' }">{{ t('pages.clarify.toolOutput') }}</div>
            <pre
              class="agent-tool-code font-mono"
              :class="{ 'agent-tool-code--failed': r.state === 'failed' }"
              data-testid="agent-tool-output"
              >{{ r.output }}</pre
            >
          </div>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.agent-tools {
  color: var(--chat-muted, rgb(var(--c-txt2)));
  font-size: 12px;
  line-height: 1.65;
}
.agent-tools-head,
.agent-tool-row-main {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  border-radius: 6px;
  text-align: left;
}
.agent-tools-head {
  width: 100%;
  min-height: 32px;
  padding: 4px 6px;
  transition: background-color var(--dur-ui), color var(--dur-ui);
}
.agent-tools-head:hover,
button.agent-tool-row-main:hover {
  background: rgb(var(--c-elevated) / .6);
  color: rgb(var(--c-txt));
}
.agent-tools-head:focus-visible,
button.agent-tool-row-main:focus-visible,
.agent-tool-action:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .45);
  outline-offset: 2px;
}
.agent-tools-count { flex-shrink: 0; font-weight: 500; }
.agent-tools-context {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-tool-icon,
.agent-tool-chevron { flex-shrink: 0; }
.agent-tool-chevron {
  color: rgb(var(--c-txt3));
  transition: transform var(--dur-ui);
}
.agent-tool-running { color: rgb(var(--c-accent-2)); }
.agent-tool-failed { color: var(--chat-error, rgb(var(--c-err))); }
.agent-tool-list {
  min-width: 0;
  margin: 3px 0 0 13px;
  border-left: 1px solid rgb(var(--c-line) / .8);
  padding: 0 0 2px 15px;
  list-style: none;
}
.agent-tool-line {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 4px;
}
.agent-tool-row-main {
  flex: 1;
  min-height: 30px;
  padding: 4px;
  transition: background-color var(--dur-ui), color var(--dur-ui);
}
.agent-tool-label {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-tool-label--summary { flex: 0 1 auto; max-width: 44%; }
.agent-tool-summary {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
}
.agent-tool-duration,
.agent-tool-state { flex-shrink: 0; font-size: 11px; }
.agent-tool-action {
  flex-shrink: 0;
  min-height: 30px;
  border-radius: 6px;
  padding: 4px 7px;
  color: rgb(var(--c-accent-2));
  font-size: 11px;
  font-weight: 500;
  transition: background-color var(--dur-ui);
}
.agent-tool-action:hover { background: rgb(var(--c-accent) / .07); }
.agent-tool-detail {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 12px;
  margin: 2px 0 10px 28px;
  font-size: 12px;
}
.agent-tool-raw-name { overflow-wrap: anywhere; font-size: 11px; }
.agent-tool-detail-label { margin-bottom: 3px; font-size: 11px; font-weight: 500; }
.agent-tool-code {
  max-height: 240px;
  margin: 0;
  overflow: auto;
  padding-block: 2px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  line-height: 1.75;
}
.agent-tool-code--failed { border-left: 2px solid rgb(var(--c-err) / .45); padding-left: 12px; }
.agent-tool-running.animate-spin { animation-duration: 1.4s; }
@media (max-width: 767px) {
  .agent-tools-head,
  button.agent-tool-row-main,
  .agent-tool-action { min-height: 44px; }
  .agent-tool-row-main { gap: 6px; }
}
@media (prefers-reduced-motion: reduce) {
  .agent-tools-head,
  .agent-tool-row-main,
  .agent-tool-action,
  .agent-tool-chevron { transition: none; }
  .agent-tool-running.animate-spin { animation: none !important; }
}
</style>

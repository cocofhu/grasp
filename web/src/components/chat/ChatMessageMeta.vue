<script lang="ts">
let closeMessageInfo: (() => void) | undefined
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChatMessageMetadata, TokenUsage } from '@/lib/shared/types'
import { effectiveModelUsageRows, fmtTokenCount, tokenUsageTotal, TOKEN_USAGE_SOURCE_BRIDGE, unknownDisplayName } from '@/lib/run/tokenUsage'
import { fmtTime, fmtTurnElapsed, wallClockSec } from '@/lib/shared/format'
import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'
import Icon from '@/components/ui/Icon.vue'

const props = defineProps<{ metadata?: ChatMessageMetadata; unknownModelDisplayName?: string | null }>()
const { t, locale } = useI18n()
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLElement>()
const open = ref(false)
const pinned = ref(false)
const position = ref({ left: '0px', top: '0px', maxHeight: 'calc(100dvh - 24px)' })
const tipId = `chat-reply-info-${useId().replace(/:/g, '')}`
let closeTimer: ReturnType<typeof setTimeout> | undefined

const stamp = computed(() => [props.metadata?.execution?.startedAt, props.metadata?.createdAt, props.metadata?.at, props.metadata?.endedAt, props.metadata?.startedAt].find(value => value && Number.isFinite(Date.parse(value))) || '')
const fullTime = computed(() => stamp.value ? fmtTime(stamp.value) : '')
const clockTime = computed(() => stamp.value ? new Date(stamp.value).toLocaleTimeString(locale.value, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }) : '')
const modelRows = computed(() => effectiveModelUsageRows(props.metadata?.usage, props.metadata?.usageByModel))
const usage = computed<TokenUsage | null>(() => props.metadata?.usage ?? (modelRows.value.length ? modelRows.value.reduce((sum, row) => ({
  inputTokens: sum.inputTokens + row.inputTokens,
  outputTokens: sum.outputTokens + row.outputTokens,
  cacheReadTokens: sum.cacheReadTokens + row.cacheReadTokens,
  cacheWriteTokens: sum.cacheWriteTokens + row.cacheWriteTokens,
}), { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 }) : null))
const notReported = computed(() => t('pages.chatMessageInfo.notReported'))
const model = computed(() => props.metadata?.execution?.modelId || modelRows.value.filter(row => !row.unknown && !row.filled && row.source !== TOKEN_USAGE_SOURCE_BRIDGE).map(row => row.modelKey).join(', ') || notReported.value)
const tool = computed(() => {
  const backend = props.metadata?.execution?.backend
  return ACP_BACKENDS.find(row => row.id === backend)?.label || backend || notReported.value
})
const duration = computed(() => {
  const ms = props.metadata?.durationMs
  const sec = ms != null && Number.isFinite(ms) && ms >= 0 ? ms / 1000 : wallClockSec(props.metadata?.execution?.startedAt || props.metadata?.startedAt, props.metadata?.endedAt || props.metadata?.createdAt || props.metadata?.at)
  return sec == null ? notReported.value : ms === 0 ? '0s' : fmtTurnElapsed(sec)
})
const rows = computed(() => [
  { key: 'time', label: t('pages.chatMessageInfo.time'), value: fullTime.value || notReported.value },
  { key: 'agent', label: 'Agent', value: props.metadata?.execution?.agentName || notReported.value },
  { key: 'tool', label: t('pages.chatMessageInfo.tool'), value: tool.value },
  { key: 'model', label: t('pages.chatMessageInfo.model'), value: model.value },
  ...(props.metadata?.execution?.configurationId ? [{ key: 'group', label: t('pages.chatMessageInfo.group'), value: props.metadata.execution.configurationId }] : []),
  ...(props.metadata?.execution?.reasoningEffort ? [{ key: 'effort', label: t('pages.chatMessageInfo.effort'), value: props.metadata.execution.reasoningEffort }] : []),
  { key: 'duration', label: t('pages.chatMessageInfo.duration'), value: duration.value },
])
const tokenRows = computed(() => usage.value ? [
  { key: 'input', label: t('pages.executionTimeline.partInput'), value: usage.value.inputTokens },
  { key: 'output', label: t('pages.executionTimeline.partOutput'), value: usage.value.outputTokens },
  { key: 'cacheRead', label: t('pages.executionTimeline.partCacheRead'), value: usage.value.cacheReadTokens },
  { key: 'cacheWrite', label: t('pages.executionTimeline.partCacheWrite'), value: usage.value.cacheWriteTokens },
] : [])

function clearClose() { if (closeTimer) clearTimeout(closeTimer); closeTimer = undefined }
function cleanup() {
  clearClose()
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', escape)
  window.removeEventListener('resize', close)
  window.removeEventListener('scroll', scrolled, true)
}
function close() { open.value = false; pinned.value = false; cleanup(); if (closeMessageInfo === close) closeMessageInfo = undefined }
function outside(event: PointerEvent) {
  if (event.target instanceof Node && !trigger.value?.contains(event.target) && !panel.value?.contains(event.target)) close()
}
function escape(event: KeyboardEvent) { if (event.key === 'Escape') close() }
function scrolled(event: Event) { if (!(event.target instanceof Node) || !panel.value?.contains(event.target)) void place() }
async function show() {
  if (closeMessageInfo !== close) closeMessageInfo?.()
  closeMessageInfo = close
  clearClose()
  if (!open.value) {
    open.value = true
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    window.addEventListener('resize', close)
    window.addEventListener('scroll', scrolled, true)
  }
  await place()
}
async function place() {
  await nextTick()
  if (!trigger.value || !panel.value || !open.value) return
  const box = trigger.value.getBoundingClientRect()
  if (box.bottom < 0 || box.top > innerHeight || box.right < 0 || box.left > innerWidth) { close(); return }
  const width = panel.value.offsetWidth
  const height = panel.value.scrollHeight + 2
  const above = Math.max(0, box.top - 20)
  const below = Math.max(0, innerHeight - box.bottom - 20)
  const useAbove = height <= above || (height > below && above > below)
  const maxHeight = Math.max(40, useAbove ? above : below)
  const shownHeight = Math.min(height, maxHeight)
  position.value = {
    left: `${Math.max(12, Math.min(innerWidth - width - 12, box.left))}px`,
    top: `${Math.max(12, useAbove ? box.top - shownHeight - 8 : box.bottom + 8)}px`,
    maxHeight: `${maxHeight}px`,
  }
}
function leave() {
  clearClose()
  if (!pinned.value && document.activeElement !== trigger.value) closeTimer = setTimeout(close, 120)
}
function toggle() { if (pinned.value) close(); else { pinned.value = true; void show() } }
onBeforeUnmount(close)
</script>

<template>
  <div class="chat-message-meta" data-testid="chat-message-meta">
    <time v-if="stamp" :datetime="stamp" :title="fullTime" class="tabular-nums" data-testid="chat-message-time">{{ clockTime }}</time>
    <button ref="trigger" type="button" class="chat-message-info" :aria-label="t('pages.chatMessageInfo.title')" :aria-expanded="open" :aria-describedby="open ? tipId : undefined" data-testid="chat-message-info" @pointerenter="event => { if (event.pointerType !== 'touch') show() }" @pointerleave="leave" @focus="show" @blur="leave" @click.stop="toggle" @keydown.esc.stop.prevent="close">
      <Icon name="info" :size="13" :interactive="false" aria-hidden="true" />
    </button>
    <slot />
    <Teleport to="body">
      <section v-if="open" :id="tipId" ref="panel" role="tooltip" class="chat-message-info-panel" :style="position" data-testid="chat-message-info-panel" @pointerenter="clearClose" @pointerleave="leave" @keydown.esc.stop.prevent="close">
        <strong class="block text-[12px] font-semibold text-txt">{{ t('pages.chatMessageInfo.title') }}</strong>
        <dl class="chat-info-rows">
          <div v-for="row in rows" :key="row.key" :data-info-row="row.key"><dt>{{ row.label }}</dt><dd>{{ row.value }}</dd></div>
        </dl>
        <div class="chat-info-tokens">
          <div class="flex items-center justify-between gap-3 text-[12px]"><strong>Tokens</strong><span class="font-mono tabular-nums" data-testid="chat-message-token-total">{{ usage ? fmtTokenCount(tokenUsageTotal(usage)) : notReported }}</span></div>
          <dl v-if="usage" class="chat-info-rows"><div v-for="row in tokenRows" :key="row.key" :data-token-row="row.key"><dt>{{ row.label }}</dt><dd class="font-mono tabular-nums">{{ fmtTokenCount(row.value) }}</dd></div></dl>
          <div v-if="modelRows.length" class="chat-info-models" data-testid="chat-message-model-usage">
            <div v-for="row in modelRows" :key="row.modelKey" class="flex items-start justify-between gap-3" :data-model-key="row.modelKey">
              <span class="min-w-0 break-words">{{ row.unknown ? unknownModelDisplayName?.trim() ? unknownDisplayName(row.modelKey, unknownModelDisplayName) : t('pages.chatMessageInfo.unknownModel') : row.modelKey }}<span class="block text-[10px] text-txt3">{{ t(row.filled || row.source === TOKEN_USAGE_SOURCE_BRIDGE ? 'pages.chatMessageInfo.sourceAttribution' : row.unknown ? 'pages.chatMessageInfo.sourceUnknown' : 'pages.chatMessageInfo.sourceReported') }}</span></span>
              <span class="shrink-0 font-mono tabular-nums">{{ fmtTokenCount(row.total) }}</span>
            </div>
          </div>
        </div>
      </section>
    </Teleport>
  </div>
</template>

<style scoped>
.chat-message-meta { display: flex; min-height: 24px; align-items: center; gap: 8px; margin-top: 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.chat-message-info { display: inline-flex; width: 24px; height: 24px; align-items: center; justify-content: center; flex-shrink: 0; border: 0; border-radius: 6px; background: transparent; color: inherit; cursor: pointer; }
.chat-message-info:hover, .chat-message-info[aria-expanded='true'] { background: rgb(var(--c-elevated)); color: rgb(var(--c-txt)); }
.chat-message-info:focus-visible { outline: 2px solid rgb(var(--c-accent) / .6); outline-offset: 2px; }
.chat-message-info-panel { position: fixed; z-index: 10010; width: min(320px, calc(100vw - 24px)); max-height: calc(100dvh - 24px); overflow: auto; border: 1px solid rgb(var(--c-line)); border-radius: 12px; padding: 14px; background: rgb(var(--c-surface)); color: rgb(var(--c-txt2)); box-shadow: 0 12px 36px rgb(0 0 0 / .22); font-size: 11px; line-height: 1.6; text-align: left; }
.chat-info-rows { display: grid; gap: 6px; margin: 10px 0 0; }
.chat-info-rows > div { display: grid; grid-template-columns: 76px minmax(0, 1fr); gap: 12px; }
.chat-info-rows dt { color: rgb(var(--c-txt3)); }
.chat-info-rows dd { margin: 0; overflow-wrap: anywhere; text-align: right; }
.chat-info-tokens { margin-top: 12px; border-top: 1px solid rgb(var(--c-line)); padding-top: 10px; }
.chat-info-models { display: grid; gap: 6px; margin-top: 10px; border-top: 1px solid rgb(var(--c-line)); padding-top: 10px; }
@media (pointer: coarse) { .chat-message-info { width: 32px; height: 32px; } }
</style>

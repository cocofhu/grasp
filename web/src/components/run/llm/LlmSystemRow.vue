<script lang="ts">
export type LlmTraceTone = 'accent' | 'ok' | 'warn' | 'err' | 'neutral'

/** Icon + tone for one LLM process trace. Enter stays off this row. */
export function llmTraceMark(event: string, edgeKind?: string): { name: string; tone: LlmTraceTone } | null {
  if (event === 'transition') {
    return { name: 'arrow-flow', tone: edgeKind === 'failure' ? 'err' : 'neutral' }
  }
  if (event === 'rollback') return { name: 'history-back', tone: 'neutral' }
  if (event === 'pause') return { name: 'user-hold', tone: 'warn' }
  if (event === 'resume') return { name: 'play-continue', tone: 'accent' }
  if (event === 'exit') return { name: 'quiet-dot', tone: 'neutral' }
  return null
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'

const props = defineProps<{
  event: string
  nodeLabel: string
  toLabel?: string
  edgeKind?: string
}>()

const { t } = useI18n()

const mark = computed(() => llmTraceMark(props.event, props.edgeKind))
const toneClass: Record<LlmTraceTone, string> = {
  accent: 'text-accent-2',
  ok: 'text-ok',
  warn: 'text-warn',
  err: 'text-err',
  neutral: 'text-txt3',
}
const label = computed(() => {
  const to = props.toLabel || ''
  if (props.event === 'transition' && props.edgeKind === 'failure') {
    return t('pages.llmTranscript.trace.transitionFailure', { to })
  }
  if (props.event === 'transition') return t('pages.llmTranscript.trace.transition', { to })
  if (props.event === 'rollback') return t('pages.llmTranscript.trace.rollback', { to })
  if (props.event === 'pause') return t('pages.llmTranscript.trace.pause', { node: props.nodeLabel })
  if (props.event === 'resume') return t('pages.llmTranscript.trace.resume', { node: props.nodeLabel })
  if (props.event === 'exit') return t('pages.llmTranscript.trace.exit', { node: props.nodeLabel })
  return t('pages.llmTranscript.trace.other', { node: props.nodeLabel, event: props.event })
})
</script>

<template>
  <div
    v-if="mark"
    class="llm-system-row grasp-status-host"
    data-testid="llm-system-row"
    :data-event="event"
    :data-edge="edgeKind || undefined"
    :data-icon="mark.name"
  >
    <Icon :name="mark.name" :size="16" :class="toneClass[mark.tone]" aria-hidden="true" />
    <span class="llm-system-row__label">{{ label }}</span>
  </div>
</template>

<style scoped>
.llm-system-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin-top: 8px;
  padding: 0 8px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  line-height: 20px;
}
.llm-system-row__label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

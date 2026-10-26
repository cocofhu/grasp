<script setup lang="ts">
/**
 * Shared thought <summary> status: generating / done / interrupted.
 * Bound to whole-turn busy — never treat thoughtStreaming===false as done
 * while message is still outputting.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'

export type ThoughtSummaryState = 'generating' | 'done' | 'interrupted' | 'idle'

const props = withDefaults(
  defineProps<{
    /** Whole-turn busy (thought or message still streaming / turn not finished). */
    busy?: boolean
    /** Successful completion (not interrupted). */
    completed?: boolean
    /** Interrupted or failed end. */
    interrupted?: boolean
    /** Row title (default「思考过程」); the inline hand-off step reuses this row. */
    title?: string
    /** Badge while busy (default「生成中…」). */
    busyLabel?: string
    /** Badge when done (default「已完成」); node separators show the hand-off time. */
    doneLabel?: string
    /** Badge when interrupted (default「已中断」); run-ended rows show the time instead. */
    interruptedLabel?: string
    /** Icon while busy. Generating / hand-off / preparing stay on the arc; auto-run uses the breath dot. */
    busyIcon?: string
  }>(),
  {
    busy: false,
    completed: false,
    interrupted: false,
    title: '',
    busyLabel: '',
    doneLabel: '',
    interruptedLabel: '',
    busyIcon: '',
  },
)

const { t } = useI18n()

const state = computed<ThoughtSummaryState>(() => {
  // Interrupted/failure must never look like success (no check / 已完成).
  if (props.interrupted) return 'interrupted'
  // Whole-turn busy (incl. message outputting) stays generating.
  if (props.busy) return 'generating'
  if (props.completed) return 'done'
  return 'idle'
})

const dataState = computed(() => {
  if (state.value === 'generating') return 'streaming'
  if (state.value === 'done') return 'done'
  if (state.value === 'interrupted') return 'interrupted'
  return 'idle'
})

const label = computed(() => {
  if (state.value === 'generating') return props.busyLabel || t('pages.clarify.thoughtGenerating')
  if (state.value === 'done') return props.doneLabel || t('pages.clarify.turnCompleted')
  if (state.value === 'interrupted') return props.interruptedLabel || t('pages.clarify.thoughtInterrupted')
  return ''
})

const iconName = computed(() => {
  if (state.value === 'generating') return props.busyIcon || 'arc-spinner'
  if (state.value === 'interrupted') return 'warn-pulse'
  if (state.value === 'done') return 'check-draw'
  return 'sparkles'
})
</script>

<template>
  <span
    class="thought-summary-status grasp-status-host inline-flex min-w-0 flex-1 items-center"
    :data-state="dataState"
    data-testid="thought-summary-state"
  >
    <Icon
      :key="iconName"
      :name="iconName"
      :busy="state === 'generating'"
      :size="12"
      class="thought-summary-icon"
      aria-hidden="true"
    />
    <span class="thought-summary-title">{{ title || t('pages.clarify.thought') }}</span>
    <span
      v-if="state !== 'idle'"
      class="thought-summary-badge"
    >
      <span>{{ label }}</span>
    </span>
  </span>
</template>

<style scoped>
.thought-summary-status {
  gap: 6px;
  color: var(--chat-muted, rgb(var(--c-txt2)));
  font-size: 12px;
  line-height: 20px;
}
.thought-summary-icon { flex-shrink: 0; width: 12px; height: 12px; }
.thought-summary-title { min-width: 0; font-weight: 500; }
.thought-summary-badge {
  flex-shrink: 0;
  margin-left: auto;
  padding-left: 8px;
  font-size: 11px;
  white-space: nowrap;
}
.thought-summary-status[data-state="streaming"] .thought-summary-icon,
.thought-summary-status[data-state="streaming"] .thought-summary-badge { color: rgb(var(--c-accent-2)); }
.thought-summary-status[data-state="done"] .thought-summary-icon { color: rgb(var(--c-ok)); }
.thought-summary-status[data-state="interrupted"] .thought-summary-icon,
.thought-summary-status[data-state="interrupted"] .thought-summary-badge { color: var(--chat-error, rgb(var(--c-err))); }
</style>

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
  }>(),
  {
    busy: false,
    completed: false,
    interrupted: false,
    title: '',
    busyLabel: '',
    doneLabel: '',
    interruptedLabel: '',
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
</script>

<template>
  <span
    class="thought-summary-status inline-flex min-w-0 flex-1 items-center"
    :data-state="dataState"
    data-testid="thought-summary-state"
  >
    <Icon
      :key="state"
      :name="state === 'generating' ? 'spinner' : state === 'interrupted' ? 'alert' : state === 'done' ? 'check' : 'sparkles'"
      :size="16"
      class="thought-summary-icon"
      :class="{ 'thought-summary-icon--running': state === 'generating' }"
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
  gap: 8px;
  color: var(--chat-muted, rgb(var(--c-txt2)));
  font-size: 12px;
  line-height: 1.65;
}
.thought-summary-icon { flex-shrink: 0; }
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
.thought-summary-status[data-state="interrupted"] .thought-summary-icon,
.thought-summary-status[data-state="interrupted"] .thought-summary-badge { color: var(--chat-error, rgb(var(--c-err))); }
.thought-summary-icon--running { animation: thought-summary-spin 1.4s linear infinite; }
@keyframes thought-summary-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) {
  .thought-summary-icon--running {
    animation: none !important;
    transform: none;
  }
}
</style>

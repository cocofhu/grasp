<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RunPriority } from './PrioritySegmented.vue'

const props = withDefaults(
  defineProps<{
    priority?: RunPriority | string | null
    size?: 'sm' | 'md'
    /** When wrapped in a trigger that owns the hover title, omit the inner title. */
    hideTitle?: boolean
  }>(),
  { size: 'sm', hideTitle: false },
)

const { t } = useI18n()

const value = computed<RunPriority>(() => {
  const p = props.priority
  if (p === 'high' || p === 'low' || p === 'normal') return p
  return 'normal'
})

const cls = computed(() => {
  if (value.value === 'high') return 'text-[#f87171] border-[#f87171]/35 bg-[#f87171]/10'
  if (value.value === 'low') return 'text-txt3 border-line-strong bg-elevated'
  return 'text-txt2 border-line bg-surface'
})

const pad = computed(() => (props.size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-1 text-xs'))
</script>

<template>
  <span
    class="rounded-lg inline-flex items-center gap-1.5 whitespace-nowrap border font-semibold tracking-wide"
    :class="[cls, pad]"
    :title="hideTitle ? undefined : t('common.priority.label')"
  >
    <span class="inline-block h-1.5 w-1.5 shrink-0 bg-current" aria-hidden="true" />
    {{ t(`common.priority.${value}`) }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from './Icon.vue'
import AppSpinner from './AppSpinner.vue'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'ghost' | 'outline' | 'danger' | 'subtle'
    size?: 'sm' | 'md'
    icon?: string
    block?: boolean
    disabled?: boolean
    loading?: boolean
  }>(),
  { variant: 'outline', size: 'md', disabled: false, loading: false },
)

const cls = computed(() => {
  const base =
    'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium outline-none disabled:opacity-50 disabled:cursor-not-allowed focus-visible:ring-2 focus-visible:ring-accent/40 focus-visible:ring-offset-1 focus-visible:ring-offset-base'
  // Height tokens: sm=h-6 (24px), md=h-9 (36px). Vertical padding no longer drives height.
  const sizes = props.size === 'sm' ? 'h-6 px-2.5 text-xs' : 'h-9 px-3.5 text-sm'
  // Hover fill moved to --hover-ink-color + v-hover-ink (plan g2.2); keep non-fill hover tokens.
  const variants: Record<string, string> = {
    primary: 'bg-accent text-white shadow-glow',
    // Ghost darkens icon + label only. No elevated fill (disabled stays still via :enabled).
    ghost: 'text-txt2 enabled:hover:text-txt',
    outline: 'border border-line bg-surface text-txt hover:border-line-strong',
    danger: 'border border-err/40 bg-err/10 text-err',
    subtle: 'bg-elevated text-txt2 hover:text-txt',
  }
  const press =
    props.disabled || props.loading ? '' : 'ui-pressable'
  return [base, sizes, variants[props.variant], press, props.block ? 'w-full' : '']
})

const inkColor = computed(() => {
  // Primary uses the theme accent-hover token (light #5424D8, dark keeps its own).
  // Ghost and subtle stay text-only so they never lay an elevated gray fill.
  switch (props.variant) {
    case 'primary':
      return 'rgb(var(--c-accent-hover))'
    case 'danger':
      return 'rgb(var(--c-err) / 0.2)'
    case 'outline':
      return 'rgb(var(--c-elevated))'
    default:
      return 'rgb(var(--c-elevated))'
  }
})

const isDisabled = computed(() => props.disabled || props.loading)
/** Disabled, loading, subtle, and ghost do not spread a hover fill. */
const inkEnabled = computed(
  () => !isDisabled.value && props.variant !== 'subtle' && props.variant !== 'ghost',
)
</script>

<template>
  <button
    v-hover-ink="{ enabled: inkEnabled }"
    :class="cls"
    :style="inkEnabled ? { '--hover-ink-color': inkColor } : undefined"
    :disabled="isDisabled"
    :aria-busy="loading ? 'true' : undefined"
  >
    <AppSpinner v-if="loading" :size="size === 'sm' ? 12 : 14" />
    <Icon v-else-if="icon" :name="icon" :size="size === 'sm' ? 14 : 16" />
    <!-- Wrap bare text slot so content stacking stays explicit (plan g1.2 / review v1). -->
    <span class="relative z-[1] inline-flex items-center gap-1.5"><slot /></span>
  </button>
</template>

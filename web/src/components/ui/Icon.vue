<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useIconMotion } from '@/lib/composables/useIconMotion'
import { resolveIcon, type IconName } from '@/lib/icons/catalog'
import '@/styles/icon-motion.css'

const props = withDefaults(defineProps<{
  name: IconName | (string & {})
  size?: number | string
  /** Only real loading states opt in. The spinner name is loading by default. */
  busy?: boolean
  disabled?: boolean
  /** Suppress hover/focus feedback while retaining explicit event feedback. */
  interactive?: boolean
  motionKey?: string | number
}>(), { busy: undefined, interactive: true })

const element = ref<SVGSVGElement | null>(null)
const definition = computed(() => resolveIcon(props.name))
const size = computed(() => props.size ?? 18)
const { state, reset } = useIconMotion(element, {
  duration: () => definition.value.duration,
  busy: () => props.busy ?? props.name === 'spinner',
  disabled: () => props.disabled === true,
  interactive: () => props.interactive !== false,
  motionKey: () => props.motionKey,
  eventMotion: () => props.name !== 'check-draw',
})

// Switching semantic states must not animate the new shape using an old hover.
watch(() => props.name, reset)

const statusNames = new Set([
  'arc-spinner', 'breath-dot', 'check-draw', 'warn-pulse', 'bubble-idle',
  'user-hold', 'arrow-flow', 'run-live', 'history-back', 'play-continue', 'quiet-dot',
])
</script>

<template>
  <svg
    ref="element"
    class="app-icon"
    aria-hidden="true"
    :class="{ 'icon-spinner': props.name === 'spinner', 'grasp-status': statusNames.has(props.name) }"
    :data-icon="props.name"
    :data-icon-motion="state"
    :width="size"
    :height="size"
    :viewBox="definition.viewBox"
    :fill="definition.filled ? 'currentColor' : 'none'"
    :stroke="definition.strokeWidth ? 'currentColor' : 'none'"
    :stroke-width="definition.strokeWidth || undefined"
    stroke-linecap="round"
    stroke-linejoin="round"
    v-html="definition.body"
  />
</template>

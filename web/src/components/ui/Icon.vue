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
  /**
   * Draws one extra ring inside this glyph. Sidebar breath rows use it for a
   * one-shot hover ripple so that ring does not share the busy loop's circle.
   * The shared breath-dot body in definitions.json stays unchanged.
   */
  breathPing?: boolean
}>(), { busy: undefined, interactive: true, breathPing: false })

/** Same radius as the shared breath ring. Not part of the glyph definition. */
const BREATH_PING_MARK = '<circle class="nav-breath-ping" cx="12" cy="12" r="3.4" fill="none" stroke="currentColor" stroke-width="1.15" vector-effect="non-scaling-stroke"/>'

const element = ref<SVGSVGElement | null>(null)
const definition = computed(() => resolveIcon(props.name))
const markup = computed(() => {
  const body = definition.value.body
  if (!props.breathPing || props.name !== 'breath-dot') return body
  return BREATH_PING_MARK + body
})
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
    v-html="markup"
  />
</template>

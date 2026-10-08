<script setup lang="ts">
import { computed } from 'vue'
import { dotPatternGeometry, type FlowViewport } from './composables/finiteViewport'

const props = defineProps<{
  viewport: FlowViewport
  /** Resolved --flow-dot. SVG attributes do not read CSS variables. */
  color: string
}>()

const geometry = computed(() => dotPatternGeometry(props.viewport))
const patternId = `canvas-dots-${Math.random().toString(36).slice(2, 8)}`
</script>

<template>
  <svg class="vue-flow__background vue-flow__container" data-testid="canvas-dots" aria-hidden="true">
    <pattern
      :id="patternId"
      :x="geometry.x"
      :y="geometry.y"
      :width="geometry.width"
      :height="geometry.height"
      :patternTransform="geometry.transform"
      patternUnits="userSpaceOnUse"
    >
      <circle :cx="geometry.cx" :cy="geometry.cy" :r="geometry.r" :fill="color" />
    </pattern>
    <rect x="0" y="0" width="100%" height="100%" :fill="`url(#${patternId})`" />
  </svg>
</template>

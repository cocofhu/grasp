<script setup lang="ts">
import { computed } from 'vue'
import { formatUnreadBadge } from '@/lib/run/useRunTerminalNotifications'

const props = defineProps<{
  count: number
  panelOpen: boolean
}>()

const label = computed(() => (props.panelOpen ? '' : formatUnreadBadge(props.count)))
</script>

<template>
  <span class="artifact-unread-glyph" data-testid="artifact-unread-glyph">
    <slot />
    <span
      v-if="label"
      :key="label"
      class="artifact-unread-badge"
      data-testid="artifact-unread-badge"
      aria-hidden="true"
    >{{ label }}</span>
  </span>
</template>

<style scoped>
.artifact-unread-glyph {
  position: relative;
  display: inline-flex;
  width: 16px;
  height: 16px;
  flex: none;
  align-items: center;
  justify-content: center;
  overflow: visible;
}

.artifact-unread-badge {
  position: absolute;
  z-index: 1;
  top: -5px;
  right: -7px;
  display: inline-flex;
  height: 12px;
  min-width: 12px;
  align-items: center;
  justify-content: center;
  padding: 0 3px;
  border-radius: 999px;
  background: rgb(var(--c-accent));
  color: #fff;
  font-size: 8px;
  font-weight: 700;
  line-height: 12px;
  letter-spacing: 0;
  white-space: nowrap;
  box-shadow: 0 0 0 1.5px rgb(var(--c-surface));
  pointer-events: none;
  transform-origin: 100% 0;
  animation: artifact-unread-pop 280ms ease;
}

@keyframes artifact-unread-pop {
  from {
    opacity: 0;
    transform: scale(0.55);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .artifact-unread-badge {
    animation: none;
    opacity: 1;
    transform: scale(1);
  }
}
</style>

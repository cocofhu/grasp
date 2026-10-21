<script setup lang="ts">
import { RECENT_REORDER_MOTION } from '@/lib/run/recentRuns'

/** Isolated wrapper so the reorder slide can be removed without touching the rows. */
withDefaults(defineProps<{ motion?: boolean }>(), { motion: RECENT_REORDER_MOTION })
</script>

<template>
  <TransitionGroup v-if="motion" name="workspace-recent" tag="div" class="workspace-recent-list" data-testid="sidebar-recent-list">
    <slot />
  </TransitionGroup>
  <div v-else class="workspace-recent-list" data-testid="sidebar-recent-list">
    <slot />
  </div>
</template>

<style scoped>
.workspace-recent-list { position: relative; }
:slotted(.workspace-recent-move) { transition: transform 220ms cubic-bezier(.22, 0, .18, 1); }
:slotted(.workspace-recent-enter-active) { transition: opacity 200ms ease; }
:slotted(.workspace-recent-enter-from) { opacity: 0; }
:slotted(.workspace-recent-leave-active) { display: none; }
@media (prefers-reduced-motion: reduce) {
  :slotted(.workspace-recent-move), :slotted(.workspace-recent-enter-active) { transition: none; }
}
</style>

<script setup lang="ts">
defineProps<{
  /** spin: sandbox still booting. pulse: agent is writing a reply. */
  kind: 'spin' | 'pulse'
}>()
</script>

<template>
  <svg
    v-if="kind === 'spin'"
    class="inbox-motion-spin"
    width="14"
    height="14"
    viewBox="0 0 14 14"
    fill="none"
    aria-hidden="true"
    data-testid="inbox-motion-spin"
  >
    <circle
      cx="7"
      cy="7"
      r="5"
      stroke="#22d3ee"
      stroke-width="1.6"
      stroke-linecap="round"
      stroke-dasharray="18 14"
    />
  </svg>
  <svg
    v-else
    class="inbox-motion-pulse"
    width="14"
    height="14"
    viewBox="0 0 14 14"
    aria-hidden="true"
    data-testid="inbox-motion-pulse"
  >
    <circle cx="7" cy="7" r="3" fill="#22d3ee" />
  </svg>
</template>

<style scoped>
.inbox-motion-spin {
  flex: none;
  animation: inbox-motion-spin 0.8s linear infinite;
}

.inbox-motion-pulse {
  flex: none;
  animation: inbox-motion-pulse 1.2s ease-in-out infinite;
}

@keyframes inbox-motion-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes inbox-motion-pulse {
  0%,
  100% {
    opacity: 0.35;
  }
  50% {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .inbox-motion-spin,
  .inbox-motion-pulse {
    animation: none;
    opacity: 1;
  }
}
</style>

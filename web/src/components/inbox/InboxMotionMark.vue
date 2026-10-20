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
      stroke="currentColor"
      stroke-width="1.7"
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
    <circle cx="7" cy="7" r="2.5" fill="currentColor" />
  </svg>
</template>

<style scoped>
.inbox-motion-spin {
  flex: none;
  color: rgb(var(--c-accent-2));
  animation: inbox-motion-spin 1s linear infinite;
}

.inbox-motion-pulse {
  flex: none;
  color: rgb(var(--c-accent-2));
  animation: inbox-motion-pulse 1.6s ease-in-out infinite;
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

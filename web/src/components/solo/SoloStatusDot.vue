<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SoloStatus } from '@/lib/api/apiTypes'

const props = defineProps<{ status: SoloStatus; busy?: boolean }>()
const { t } = useI18n()
const label = computed(() => (props.busy && props.status === 'running' ? t('pages.solo.phase.running') : t(`pages.solo.status.${props.status}`)))
</script>

<template>
  <span
    class="solo-dot"
    :class="[`solo-dot--${status}`, { 'solo-dot--busy': busy && status === 'running' }]"
    :title="label"
    role="img"
    :aria-label="label"
  />
</template>

<style scoped>
.solo-dot {
  position: relative;
  display: inline-block;
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  border-radius: 999px;
  background: rgb(var(--c-txt3) / 0.5);
}
.solo-dot--running {
  background: rgb(var(--c-ok));
}
.solo-dot--starting {
  background: rgb(var(--c-warn));
  animation: solo-dot-pulse 1.4s ease-in-out infinite;
}
.solo-dot--error {
  background: rgb(var(--c-err));
}
.solo-dot--busy::after {
  position: absolute;
  inset: -3px;
  border: 1.5px solid rgb(var(--c-ok) / 0.55);
  border-radius: 999px;
  animation: solo-dot-ring 1.6s ease-out infinite;
  content: '';
}
@keyframes solo-dot-pulse {
  50% {
    opacity: 0.4;
  }
}
@keyframes solo-dot-ring {
  from {
    opacity: 1;
    transform: scale(0.7);
  }
  to {
    opacity: 0;
    transform: scale(1.5);
  }
}
@media (prefers-reduced-motion: reduce) {
  .solo-dot--starting,
  .solo-dot--busy::after {
    animation: none;
  }
}
</style>

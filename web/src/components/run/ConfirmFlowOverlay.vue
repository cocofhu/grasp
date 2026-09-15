<script setup lang="ts">
/**
 * Full-desk success overlay (1:1 page.html): bg-base/92, disk fade-in,
 * single-path check stroke-dashoffset draw. Disk and SVG are separated
 * so disk scale does not hitch the check stroke (g1.1 / g1.2).
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConfirmFlowPhase } from '@/lib/inbox/confirmFlowCeremony'

const props = withDefaults(
  defineProps<{
    phase?: ConfirmFlowPhase
    reduceMotion?: boolean
    playToken?: number
  }>(),
  {
    phase: 'idle',
    reduceMotion: false,
    playToken: 0,
  },
)

const { t } = useI18n()

const active = computed(
  () => props.phase === 'play' || props.phase === 'hold' || props.phase === 'out',
)
</script>

<template>
  <div
    class="confirm-flow-overlay"
    :class="{
      'is-play': phase === 'play',
      'is-hold': phase === 'hold',
      'is-out': phase === 'out',
      'is-reduce': reduceMotion,
    }"
    :aria-hidden="active ? undefined : 'true'"
    :data-phase="phase"
    data-testid="confirm-flow-overlay"
  >
    <div class="confirm-flow-ring" data-testid="confirm-flow-ring">
      <div class="confirm-flow-disk" data-testid="confirm-flow-disk" />
      <svg
        :key="playToken"
        class="confirm-flow-check"
        viewBox="0 0 24 24"
        aria-hidden="true"
        data-testid="confirm-flow-check"
      >
        <!-- Single continuous path — no two-stroke split (g1.1 / g3.3). -->
        <path
          pathLength="28"
          d="M4.2 12.2 L9.3 17.3 L19.8 6.4"
          data-testid="confirm-flow-check-path"
        />
      </svg>
    </div>
    <div class="confirm-flow-title" data-testid="confirm-flow-title">
      {{ t('pages.clarify.confirmFlowOverlayTitle') }}
    </div>
    <div class="confirm-flow-sub" data-testid="confirm-flow-sub">
      {{ t('pages.clarify.confirmFlowOverlaySub') }}
    </div>
  </div>
</template>

<style scoped>
.confirm-flow-overlay {
  position: absolute;
  inset: 0;
  z-index: 20;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 16px;
  text-align: center;
  /* Gold sample page.html uses 180ms veil fade (n1). */
  --dur-overlay: 180ms;
  background: rgb(var(--c-base) / 0.92);
  opacity: 0;
  visibility: hidden;
  pointer-events: none;
  transform: translateZ(0);
}

.confirm-flow-overlay.is-play,
.confirm-flow-overlay.is-hold {
  visibility: visible;
  pointer-events: auto;
  animation: confirm-flow-fade-in var(--dur-overlay, 180ms) cubic-bezier(0.22, 1, 0.36, 1) both;
}

.confirm-flow-overlay.is-out {
  visibility: visible;
  pointer-events: none;
  animation: confirm-flow-fade-out var(--dur-overlay, 180ms) ease both;
}

.confirm-flow-ring {
  position: relative;
  display: flex;
  width: 64px;
  height: 64px;
  align-items: center;
  justify-content: center;
  color: rgb(var(--c-ok));
}

/* Disk is a sibling of the SVG — never shares transform with the check (g1.2). */
.confirm-flow-disk {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: rgb(var(--c-ok) / 0.15);
  transform: scale(0.88);
  opacity: 0;
}

.confirm-flow-overlay.is-play .confirm-flow-disk,
.confirm-flow-overlay.is-hold .confirm-flow-disk {
  animation: confirm-flow-disk-in 220ms cubic-bezier(0.22, 1, 0.36, 1) both;
}

.confirm-flow-check {
  position: relative;
  z-index: 1;
  width: 34px;
  height: 34px;
  overflow: visible;
  fill: none;
  stroke: currentColor;
  stroke-width: 2.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.confirm-flow-check path {
  stroke-dasharray: 28;
  stroke-dashoffset: 28;
}

.confirm-flow-overlay.is-play .confirm-flow-check path {
  animation: confirm-flow-check-draw 420ms cubic-bezier(0.33, 0, 0.2, 1) 90ms forwards;
}

.confirm-flow-overlay.is-hold .confirm-flow-check path {
  stroke-dashoffset: 0;
}

.confirm-flow-title {
  font-size: 15px;
  font-weight: 650;
  color: rgb(var(--c-txt));
  opacity: 0;
}

.confirm-flow-sub {
  font-size: 12px;
  color: rgb(var(--c-txt3));
  opacity: 0;
}

.confirm-flow-overlay.is-play .confirm-flow-title {
  animation: confirm-flow-fade-in var(--dur-ui, 160ms) ease 360ms forwards;
}

.confirm-flow-overlay.is-play .confirm-flow-sub {
  animation: confirm-flow-fade-in var(--dur-ui, 160ms) ease 420ms forwards;
}

.confirm-flow-overlay.is-hold .confirm-flow-title,
.confirm-flow-overlay.is-hold .confirm-flow-sub {
  opacity: 1;
}

.confirm-flow-overlay.is-reduce,
.confirm-flow-overlay.is-reduce .confirm-flow-disk,
.confirm-flow-overlay.is-reduce .confirm-flow-check path,
.confirm-flow-overlay.is-reduce .confirm-flow-title,
.confirm-flow-overlay.is-reduce .confirm-flow-sub {
  animation: none !important;
}

.confirm-flow-overlay.is-reduce.is-play,
.confirm-flow-overlay.is-reduce.is-hold,
.confirm-flow-overlay.is-reduce.is-out {
  opacity: 1;
  visibility: visible;
  pointer-events: auto;
}

.confirm-flow-overlay.is-reduce .confirm-flow-disk {
  opacity: 1;
  transform: none;
}

.confirm-flow-overlay.is-reduce .confirm-flow-check path,
.confirm-flow-overlay.is-reduce .confirm-flow-title,
.confirm-flow-overlay.is-reduce .confirm-flow-sub {
  opacity: 1;
  stroke-dashoffset: 0;
}

@keyframes confirm-flow-fade-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes confirm-flow-fade-out {
  from {
    opacity: 1;
  }
  to {
    opacity: 0;
  }
}

@keyframes confirm-flow-disk-in {
  from {
    transform: scale(0.88);
    opacity: 0;
  }
  to {
    transform: scale(1);
    opacity: 1;
  }
}

/* Single-keyframe dashoffset — continuous stroke, no mid-path pause (g3.3). */
@keyframes confirm-flow-check-draw {
  to {
    stroke-dashoffset: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .confirm-flow-overlay.is-play,
  .confirm-flow-overlay.is-play .confirm-flow-disk,
  .confirm-flow-overlay.is-play .confirm-flow-check path,
  .confirm-flow-overlay.is-play .confirm-flow-title,
  .confirm-flow-overlay.is-play .confirm-flow-sub {
    animation: none !important;
  }

  .confirm-flow-overlay.is-play {
    opacity: 1;
    visibility: visible;
  }

  .confirm-flow-overlay.is-play .confirm-flow-disk {
    opacity: 1;
    transform: none;
  }

  .confirm-flow-overlay.is-play .confirm-flow-check path {
    stroke-dashoffset: 0;
  }

  .confirm-flow-overlay.is-play .confirm-flow-title,
  .confirm-flow-overlay.is-play .confirm-flow-sub {
    opacity: 1;
  }
}
</style>

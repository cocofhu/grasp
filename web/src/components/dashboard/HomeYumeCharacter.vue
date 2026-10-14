<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const rootRef = ref<HTMLElement | null>(null)
const pressed = ref(false)
const jumping = ref(false)
const reducedMotion = ref(false)

let busy = false
let bounceTimer: ReturnType<typeof setTimeout> | null = null
let motionMql: MediaQueryList | null = null

function syncReducedMotion() {
  reducedMotion.value =
    typeof window !== 'undefined' &&
    !!window.matchMedia &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function eyesEl(): SVGGElement | null {
  return (rootRef.value?.querySelector('.c-eyes') as SVGGElement | null) ?? null
}

function resetEyes() {
  const eyes = eyesEl()
  if (eyes) eyes.style.transform = ''
}

function onPointerMove(e: PointerEvent) {
  if (reducedMotion.value) return
  const root = rootRef.value
  const eyes = eyesEl()
  if (!root || !eyes) return
  const r = root.getBoundingClientRect()
  if (r.width <= 0 || r.height <= 0) return
  const cx = r.left + r.width / 2
  const cy = r.top + r.height / 2
  const dx = Math.max(-1, Math.min(1, (e.clientX - cx) / (r.width / 2)))
  const dy = Math.max(-1, Math.min(1, (e.clientY - cy) / (r.height / 2)))
  eyes.style.transform = `translate(${dx * 6}px, ${dy * 4}px)`
}

function releasePress() {
  pressed.value = false
}

function onPointerDown(e: PointerEvent) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  pressed.value = true
}

function bounce() {
  if (busy || reducedMotion.value) return
  busy = true
  jumping.value = false
  void rootRef.value?.offsetWidth
  jumping.value = true
  if (bounceTimer != null) clearTimeout(bounceTimer)
  bounceTimer = setTimeout(() => {
    jumping.value = false
    busy = false
    bounceTimer = null
  }, 360)
}

function onPointerUp(e: PointerEvent) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  releasePress()
  bounce()
}

function onClick(e: MouseEvent) {
  if (e.detail === 0) bounce()
}

function onMotionChange() {
  syncReducedMotion()
  if (reducedMotion.value) {
    resetEyes()
    pressed.value = false
    jumping.value = false
    busy = false
  }
}

onMounted(() => {
  syncReducedMotion()
  if (typeof window === 'undefined') return
  window.addEventListener('pointermove', onPointerMove, { passive: true })
  motionMql = window.matchMedia('(prefers-reduced-motion: reduce)')
  motionMql.addEventListener('change', onMotionChange)
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('pointermove', onPointerMove)
  }
  motionMql?.removeEventListener('change', onMotionChange)
  motionMql = null
  if (bounceTimer != null) clearTimeout(bounceTimer)
})
</script>

<template>
  <div
    ref="rootRef"
    class="home-yume"
    :class="{
      pressed,
      jump: jumping,
      'home-yume--static': reducedMotion,
    }"
    data-testid="home-yume"
    role="button"
    tabindex="0"
    aria-label="yume"
    @pointerdown="onPointerDown"
    @pointerup="onPointerUp"
    @pointercancel="releasePress"
    @pointerleave="releasePress"
    @blur="releasePress"
    @click="onClick"
    @keydown.enter.prevent="bounce"
    @keydown.space.prevent="bounce"
  >
    <!-- B&W yume per INTERACTION.md; idle motion via scoped CSS :deep -->
    <svg
      xmlns="http://www.w3.org/2000/svg"
      class="home-yume__svg cat cat--yume cat--bw"
      viewBox="0 0 200 200"
      width="200"
      height="200"
      role="img"
      aria-hidden="true"
      focusable="false"
    >
      <ellipse class="c-shadow" cx="100" cy="186" rx="40" ry="7" fill="#1a1a1a" opacity=".22" />
      <path
        class="c-star s1"
        transform="translate(30,50)"
        d="M0 -9 l3 7 7 1 -5 5 1 7 -6 -4 -6 4 1 -7 -5 -5 7 -1z"
        fill="#e8e8e8"
        stroke="#1a1a1a"
        stroke-width="3"
      />
      <path
        class="c-star s2"
        transform="translate(168,42)"
        d="M0 -9 l3 7 7 1 -5 5 1 7 -6 -4 -6 4 1 -7 -5 -5 7 -1z"
        fill="#ffffff"
        stroke="#1a1a1a"
        stroke-width="3"
      />
      <path
        class="c-star s3"
        transform="translate(172,128)"
        d="M0 -9 l3 7 7 1 -5 5 1 7 -6 -4 -6 4 1 -7 -5 -5 7 -1z"
        fill="#d0d0d0"
        stroke="#1a1a1a"
        stroke-width="3"
      />
      <g class="c-float">
        <g class="c-squash">
          <path class="k c-tail" d="M140 140 Q172 132 166 104" fill="none" />
          <path
            class="k"
            d="M58 160 L58 92 L66 58 L84 76 Q100 70 116 76 L134 58 L142 92 L142 160 Q131 150 121 162 Q110 150 100 162 Q90 150 79 162 Q69 150 58 160Z"
            fill="#FFFFFF"
          />
          <g class="c-eyes">
            <g class="c-eye"><ellipse cx="84" cy="110" rx="6" ry="8" fill="#1a1a1a" /></g>
            <g class="c-eye"><ellipse cx="116" cy="110" rx="6" ry="8" fill="#1a1a1a" /></g>
          </g>
          <path class="k" d="M92 124 Q96 130 100 124 Q104 130 108 124" fill="none" />
          <g class="c-blush">
            <circle cx="74" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
            <circle cx="126" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
          </g>
        </g>
      </g>
    </svg>
  </div>
</template>

<style scoped>
.home-yume {
  --yume-size: 148px;
  box-sizing: border-box;
  display: grid;
  place-items: center;
  width: var(--yume-size);
  height: var(--yume-size);
  margin: 0 0 8px;
  flex-shrink: 0;
  padding: 0;
  border: 0;
  background: transparent;
  cursor: pointer;
  touch-action: manipulation;
  user-select: none;
  -webkit-user-select: none;
  outline: none;
}

.home-yume:focus-visible {
  border-radius: 24px;
  box-shadow:
    0 0 0 2px rgb(var(--c-bg, 255 255 255)),
    0 0 0 4px rgb(var(--c-accent, 99 102 241));
}

.home-yume__svg {
  display: block;
  width: 100%;
  height: 100%;
  pointer-events: none;
}

.home-yume :deep(.k) {
  stroke: #1a1a1a;
  stroke-width: 4;
  stroke-linejoin: round;
  stroke-linecap: round;
}

.home-yume :deep(.c-float) {
  animation: yume-c-float 2.6s ease-in-out infinite;
  transform-origin: 50% 100%;
}

.home-yume :deep(.c-squash) {
  transform-origin: 50% 100%;
}

.home-yume :deep(.c-shadow) {
  transform-origin: center;
  animation: yume-c-shadow 2.6s ease-in-out infinite;
}

.home-yume :deep(.c-eye) {
  transform-origin: center;
  animation: yume-c-blink 4.2s infinite;
}

.home-yume :deep(.c-tail) {
  transform-origin: 0 100%;
  animation: yume-c-wig 1.3s ease-in-out infinite;
}

.home-yume :deep(.c-star) {
  transform-origin: center;
  animation: yume-c-pop 2.4s cubic-bezier(0.34, 1.7, 0.5, 1) infinite;
}

.home-yume :deep(.c-star.s2) {
  animation-delay: 0.8s;
}

.home-yume :deep(.c-star.s3) {
  animation-delay: 1.6s;
}

@keyframes yume-c-float {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-9%);
  }
}

@keyframes yume-c-shadow {
  0%,
  100% {
    transform: scaleX(1);
    opacity: 0.2;
  }
  50% {
    transform: scaleX(0.72);
    opacity: 0.1;
  }
}

@keyframes yume-c-blink {
  0%,
  93%,
  100% {
    transform: scaleY(1);
  }
  96% {
    transform: scaleY(0.1);
  }
}

@keyframes yume-c-wig {
  0%,
  100% {
    transform: rotate(-10deg);
  }
  50% {
    transform: rotate(10deg);
  }
}

@keyframes yume-c-pop {
  0%,
  100% {
    transform: scale(0) rotate(0);
    opacity: 0;
  }
  25%,
  70% {
    transform: scale(1) rotate(72deg);
    opacity: 1;
  }
}

.home-yume.pressed :deep(.c-squash) {
  transform: scale(1.08, 0.82) !important;
  transition: transform 0.08s ease-out;
}

.home-yume.jump :deep(.c-squash) {
  animation: yume-spring-jump 0.35s cubic-bezier(0.2, 1.5, 0.4, 1);
}

@keyframes yume-spring-jump {
  0% {
    transform: scale(1.1, 0.75) translateY(8%);
  }
  55% {
    transform: scale(0.92, 1.08) translateY(-14%);
  }
  100% {
    transform: scale(1, 1) translateY(0);
  }
}

.home-yume--static :deep(.c-float),
.home-yume--static :deep(.c-shadow),
.home-yume--static :deep(.c-eye),
.home-yume--static :deep(.c-tail),
.home-yume--static :deep(.c-star) {
  animation: none !important;
}

.home-yume--static.pressed :deep(.c-squash),
.home-yume--static.jump :deep(.c-squash) {
  animation: none !important;
  transform: none !important;
  transition: none !important;
}

@media (prefers-reduced-motion: reduce) {
  .home-yume :deep(.c-float),
  .home-yume :deep(.c-shadow),
  .home-yume :deep(.c-eye),
  .home-yume :deep(.c-tail),
  .home-yume :deep(.c-star) {
    animation: none !important;
  }
}
</style>

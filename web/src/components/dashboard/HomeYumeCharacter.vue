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
    motionMql?.matches ?? false
}

function stopBounce() {
  if (bounceTimer != null) clearTimeout(bounceTimer)
  bounceTimer = null
  jumping.value = false
  busy = false
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
  if (reducedMotion.value) return
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
    stopBounce()
  }
}

onMounted(() => {
  if (typeof window === 'undefined') return
  window.addEventListener('pointermove', onPointerMove, { passive: true })
  motionMql = window.matchMedia('(prefers-reduced-motion: reduce)')
  syncReducedMotion()
  motionMql.addEventListener('change', onMotionChange)
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('pointermove', onPointerMove)
  }
  motionMql?.removeEventListener('change', onMotionChange)
  motionMql = null
  stopBounce()
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
    <!-- Body and tail share every pose; the tail rotates around its own fixed root. -->
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
      <g class="c-float">
        <g class="c-squash">
          <g class="c-breathe">
            <!-- Overlap the root under the white body so no gap opens during a wag. -->
            <g class="c-tail-anchor" transform="translate(138 142)">
              <path class="k c-tail" d="M0 0 C20 -2 33 -14 30 -36" fill="none" />
            </g>
            <path
              class="k c-body"
              d="M58 160 L58 92 L66 58 L84 76 Q100 70 116 76 L134 58 L142 92 L142 160 Q131 150 121 162 Q110 150 100 162 Q90 150 79 162 Q69 150 58 160Z"
              fill="#FFFFFF"
            />
            <g class="c-face">
              <g class="c-eyes">
                <g class="c-eye"><ellipse cx="84" cy="110" rx="6" ry="8" fill="#1a1a1a" /></g>
                <g class="c-eye"><ellipse cx="116" cy="110" rx="6" ry="8" fill="#1a1a1a" /></g>
              </g>
              <path class="k c-mouth" d="M92 124 Q96 130 100 124 Q104 130 108 124" fill="none" />
              <g class="c-blush">
                <circle cx="74" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
                <circle cx="126" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
              </g>
            </g>
          </g>
        </g>
      </g>
    </svg>
  </div>
</template>

<style scoped>
.home-yume {
  /* fill LOCKED: 240x200 crop; SVG 240x240 + margin-top -28px (feet near clip) */
  box-sizing: border-box;
  display: grid;
  place-items: center;
  width: var(--yume-w, 240px);
  height: var(--yume-h, 200px);
  margin: 0;
  flex-shrink: 0;
  padding: 0;
  border: 0;
  background: transparent;
  cursor: pointer;
  touch-action: manipulation;
  user-select: none;
  -webkit-user-select: none;
  outline: none;
  overflow: hidden;
  filter: drop-shadow(0 12px 28px rgba(0, 0, 0, 0.45));
  /* layout box fixed; float/squash/bounce use transform only */
}

html.light .home-yume {
  filter: drop-shadow(0 6px 10px rgba(16, 24, 40, 0.08));
}

.home-yume:focus-visible {
  border-radius: 24px;
  box-shadow:
    0 0 0 2px rgb(var(--c-bg, 255 255 255)),
    0 0 0 4px rgb(var(--c-accent, 99 102 241));
}

.home-yume__svg {
  display: block;
  width: var(--yume-w, 240px);
  height: var(--yume-w, 240px);
  margin-top: -28px;
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
  transform-box: view-box;
  transform-origin: 100px 160px;
}

.home-yume :deep(.c-squash) {
  transform-box: view-box;
  transform-origin: 100px 160px;
}

.home-yume :deep(.c-breathe) {
  transform-box: view-box;
  transform-origin: 100px 156px;
  animation: yume-c-breathe 6.8s ease-in-out infinite;
}

.home-yume :deep(.c-shadow) {
  transform-box: fill-box;
  transform-origin: center;
  animation: yume-c-shadow 2.6s ease-in-out infinite;
}

.home-yume :deep(.c-eye) {
  transform-box: fill-box;
  transform-origin: center;
  animation: yume-c-blink 5.8s infinite;
}
.home-yume :deep(.c-eye + .c-eye) { animation-delay: 80ms; }

.home-yume :deep(.c-tail) {
  transform-box: view-box;
  transform-origin: 0 0;
  animation: yume-c-wig 3.4s ease-in-out infinite;
}

.home-yume :deep(.c-eyes),
.home-yume :deep(.c-face) { transition: transform 140ms ease-out; }
.home-yume :deep(.c-mouth) {
  transform-box: fill-box;
  transform-origin: center;
  transition: transform 180ms ease-out;
}
.home-yume :deep(.c-blush) { opacity: .85; transition: opacity 180ms ease-out; }
.home-yume:is(:hover, :focus-visible) :deep(.c-breathe) { animation: yume-c-curious 2.2s ease-in-out infinite; }
.home-yume:is(:hover, :focus-visible) :deep(.c-tail) { animation: yume-c-greet 900ms ease-in-out infinite; }
.home-yume:is(:hover, :focus-visible) :deep(.c-face) { transform: translateY(-1px); }
.home-yume:is(:hover, :focus-visible) :deep(.c-mouth) { transform: scale(1.12, 1.05); }
.home-yume:is(:hover, :focus-visible) :deep(.c-blush) { opacity: 1; }

@keyframes yume-c-breathe {
  0%, 100% { transform: rotate(-1deg) scale(1); }
  30% { transform: rotate(1.2deg) scale(1.012, 1.018); }
  65% { transform: rotate(-.5deg) scale(1.005, .995); }
}

@keyframes yume-c-curious {
  0%, 100% { transform: rotate(0) scale(1); }
  45% { transform: rotate(2.5deg) scale(1.015); }
  75% { transform: rotate(-1deg) scale(1.005); }
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
  0%, 86%, 90%, 94%, 100% { transform: scaleY(1); }
  88%, 92% { transform: scaleY(.12); }
}

@keyframes yume-c-wig {
  0%, 100% { transform: rotate(-5deg); }
  20% { transform: rotate(8deg); }
  42% { transform: rotate(-4deg); }
  65%, 85% { transform: rotate(2deg); }
}

@keyframes yume-c-greet {
  0%, 100% { transform: rotate(-10deg); }
  50% { transform: rotate(12deg); }
}

/* Press and jump paint outside the 240×200 crop. Rest keeps overflow hidden. */
.home-yume.jump,
.home-yume.pressed {
  overflow: visible;
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

.home-yume--static :deep(:is(.c-float, .c-squash, .c-breathe, .c-eye, .c-eyes, .c-face, .c-mouth, .c-blush, .c-tail, .c-shadow)) {
  animation: none !important;
  transform: none !important;
  transition: none !important;
}
@media (prefers-reduced-motion: reduce) {
  .home-yume :deep(:is(.c-float, .c-squash, .c-breathe, .c-eye, .c-eyes, .c-face, .c-mouth, .c-blush, .c-tail, .c-shadow)) {
    animation: none !important;
    transform: none !important;
    transition: none !important;
  }
}
</style>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import markUrl from '@/assets/brand/logo-mark.svg'
import {
  createJumpGameController,
  type JumpGameController,
  type JumpPhase,
} from '@/lib/shared/homeLogoJumpGame'

const props = defineProps<{
  productName: string
  brandVisible: string
  brandCursorBlink: boolean
  brandCursorGone: boolean
}>()

const emit = defineEmits<{
  playingChange: [playing: boolean]
}>()

const rootRef = ref<HTMLElement | null>(null)
const markRef = ref<HTMLButtonElement | null>(null)
const canvasRef = ref<HTMLCanvasElement | null>(null)
const hovered = ref(false)
const phase = ref<JumpPhase>('idle')
const score = ref(0)
const scoreReveal = ref(false)
const markHidden = ref(false)
const uiShift = ref(0) // 0 idle, 1 fully out
const reduceMotion = ref(false)

let controller: JumpGameController | null = null
let raf = 0
let lastTs = 0
let keys = { left: false, right: false }
let motionMq: MediaQueryList | null = null
let resizeObserver: ResizeObserver | null = null
let scoreHideTimer: ReturnType<typeof setTimeout> | null = null

const showHints = computed(
  () => !reduceMotion.value && phase.value === 'idle' && hovered.value && !scoreReveal.value,
)
const playing = computed(() => phase.value !== 'idle')

const uiLayerStyle = computed(() => {
  const y = uiShift.value * 120 // vh-ish via %
  const o = 1 - uiShift.value
  return {
    transform: `translate3d(0, ${y}vh, 0)`,
    opacity: String(Math.max(0, o)),
    transition:
      phase.value === 'entering' || phase.value === 'exiting'
        ? 'transform 720ms cubic-bezier(.16, 1, .3, 1), opacity 560ms cubic-bezier(.16, 1, .3, 1)'
        : 'none',
    pointerEvents: uiShift.value > 0.5 ? 'none' : 'auto',
  } as Record<string, string>
})

function readReduceMotion() {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function setupCanvas() {
  const canvas = canvasRef.value
  const root = rootRef.value
  if (!canvas || !root || !controller) return
  const rect = root.getBoundingClientRect()
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  const w = Math.max(1, Math.floor(rect.width))
  const h = Math.max(1, Math.floor(rect.height))
  canvas.width = Math.floor(w * dpr)
  canvas.height = Math.floor(h * dpr)
  canvas.style.width = `${w}px`
  canvas.style.height = `${h}px`
  const ctx = canvas.getContext('2d')
  if (ctx) ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  controller.resize(w, h)
}

function markOrigin(): { x: number; y: number } {
  const root = rootRef.value
  const mark = markRef.value
  if (!root || !mark) {
    return { x: (root?.clientWidth ?? 300) / 2, y: 120 }
  }
  const rr = root.getBoundingClientRect()
  const mr = mark.getBoundingClientRect()
  return {
    x: mr.left - rr.left + mr.width / 2,
    y: mr.top - rr.top + mr.height / 2,
  }
}

function syncKeys() {
  controller?.setKeys(keys)
}

function onKeyDown(e: KeyboardEvent) {
  if (phase.value === 'idle') return
  if (e.key === 'ArrowLeft' || e.key === 'a' || e.key === 'A') {
    keys.left = true
    e.preventDefault()
    syncKeys()
  } else if (e.key === 'ArrowRight' || e.key === 'd' || e.key === 'D') {
    keys.right = true
    e.preventDefault()
    syncKeys()
  } else if (e.key === 'Escape' && (phase.value === 'playing' || phase.value === 'entering')) {
    // force exit by draining lives via soft fail — restart exit path
    scoreReveal.value = false
  }
}

function onKeyUp(e: KeyboardEvent) {
  if (e.key === 'ArrowLeft' || e.key === 'a' || e.key === 'A') {
    keys.left = false
    syncKeys()
  } else if (e.key === 'ArrowRight' || e.key === 'd' || e.key === 'D') {
    keys.right = false
    syncKeys()
  }
}

function frame(ts: number) {
  if (!controller) return
  const canvas = canvasRef.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const dt = lastTs ? ts - lastTs : 16.67
  lastTs = ts
  controller.step(dt)
  controller.paint(ctx)
  raf = requestAnimationFrame(frame)
}

function startLoop() {
  stopLoop()
  lastTs = 0
  raf = requestAnimationFrame(frame)
}

function stopLoop() {
  if (raf) {
    cancelAnimationFrame(raf)
    raf = 0
  }
}

function onPhase(p: JumpPhase) {
  phase.value = p
  const isPlaying = p !== 'idle'
  emit('playingChange', isPlaying)

  if (p === 'entering') {
    markHidden.value = true
    uiShift.value = 1
    scoreReveal.value = false
    if (scoreHideTimer) {
      clearTimeout(scoreHideTimer)
      scoreHideTimer = null
    }
  } else if (p === 'exiting') {
    uiShift.value = 0
  } else if (p === 'idle') {
    markHidden.value = false
    uiShift.value = 0
    keys = { left: false, right: false }
    syncKeys()
    stopLoop()
    // clear canvas
    const canvas = canvasRef.value
    const ctx = canvas?.getContext('2d')
    if (canvas && ctx) ctx.clearRect(0, 0, canvas.width, canvas.height)
  }
}

function onGameOver(finalScore: number) {
  score.value = finalScore
  if (finalScore > 0) {
    scoreReveal.value = true
    if (scoreHideTimer) clearTimeout(scoreHideTimer)
    scoreHideTimer = setTimeout(() => {
      scoreReveal.value = false
      scoreHideTimer = null
    }, 4200)
  }
}


function onCanvasPointer(e: PointerEvent) {
  if (phase.value !== 'playing' && phase.value !== 'entering') return
  const canvas = canvasRef.value
  if (!canvas) return
  const rect = canvas.getBoundingClientRect()
  const x = e.clientX - rect.left
  keys.left = x < rect.width / 2
  keys.right = x >= rect.width / 2
  syncKeys()
}

function onCanvasPointerEnd() {
  keys.left = false
  keys.right = false
  syncKeys()
}

function tryStart() {
  if (reduceMotion.value) return
  if (phase.value !== 'idle') return
  if (!controller) return
  setupCanvas()
  const o = markOrigin()
  controller.startEnter(o.x, o.y)
  startLoop()
}

function onMarkEnter() {
  hovered.value = true
}

function onMarkLeave() {
  hovered.value = false
}

function onMotionChange() {
  reduceMotion.value = readReduceMotion()
}

onMounted(() => {
  reduceMotion.value = readReduceMotion()
  controller = createJumpGameController({
    onScore: (s) => {
      score.value = s
    },
    onPhase,
    onGameOver,
  })
  nextTick(setupCanvas)

  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)

  if (typeof ResizeObserver !== 'undefined' && rootRef.value) {
    resizeObserver = new ResizeObserver(() => setupCanvas())
    resizeObserver.observe(rootRef.value)
  } else {
    window.addEventListener('resize', setupCanvas)
  }

  if (typeof window !== 'undefined' && window.matchMedia) {
    motionMq = window.matchMedia('(prefers-reduced-motion: reduce)')
    motionMq.addEventListener('change', onMotionChange)
  }
})

watch(playing, (v) => emit('playingChange', v))

onBeforeUnmount(() => {
  stopLoop()
  controller?.destroy()
  controller = null
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
  window.removeEventListener('resize', setupCanvas)
  resizeObserver?.disconnect()
  resizeObserver = null
  motionMq?.removeEventListener('change', onMotionChange)
  motionMq = null
  if (scoreHideTimer) clearTimeout(scoreHideTimer)
})
</script>

<template>
  <div
    ref="rootRef"
    class="home-logo-egg"
    data-testid="home-logo-easter-egg"
    :data-phase="phase"
  >
    <div class="home-logo-egg__brand-block">
      <div
        class="home-logo-egg__hints"
        data-testid="home-logo-hints"
        :class="{ 'home-logo-egg__hints--on': showHints }"
        aria-hidden="true"
      >
        <span class="home-logo-egg__key">←</span>
        <span class="home-logo-egg__key">→</span>
        <span class="home-logo-egg__hint-label">A / D</span>
      </div>

      <button
        ref="markRef"
        type="button"
        class="home-logo-egg__mark"
        data-testid="home-logo-mark"
        :class="{ 'home-logo-egg__mark--hidden': markHidden }"
        :aria-label="reduceMotion ? productName : `${productName} — 点击开始跳跃彩蛋`"
        :disabled="reduceMotion || phase !== 'idle'"
        @click="tryStart"
        @mouseenter="onMarkEnter"
        @mouseleave="onMarkLeave"
        @focus="onMarkEnter"
        @blur="onMarkLeave"
      >
        <img :src="markUrl" width="72" height="72" alt="" draggable="false" />
      </button>

      <h1 class="home-brand" :class="{ 'home-brand--playing': phase !== 'idle' }" data-testid="home-brand" :aria-label="productName">
        <span class="home-brand__text" data-testid="home-brand-text">{{ brandVisible }}</span>
        <span
          class="home-brand__cursor"
          :class="{
            'home-brand__cursor--blink': brandCursorBlink,
            'home-brand__cursor--gone': brandCursorGone,
          }"
          data-testid="home-brand-cursor"
          aria-hidden="true"
        />
      </h1>

      <div
        v-if="scoreReveal"
        class="home-logo-egg__score"
        data-testid="home-logo-score"
        aria-live="polite"
      >
        {{ score }}
      </div>
    </div>

    <div
      class="home-logo-egg__ui"
      data-testid="home-logo-ui-layer"
      :style="uiLayerStyle"
    >
      <slot />
    </div>

    <canvas
      ref="canvasRef"
      class="home-logo-egg__canvas"
      data-testid="home-logo-game-canvas"
      :class="{ 'home-logo-egg__canvas--active': phase !== 'idle' }"
      aria-hidden="true"
      @pointerdown="onCanvasPointer"
      @pointermove="onCanvasPointer"
      @pointerup="onCanvasPointerEnd"
      @pointercancel="onCanvasPointerEnd"
      @pointerleave="onCanvasPointerEnd"
    />
  </div>
</template>

<style scoped>
.home-logo-egg {
  position: relative;
  display: flex;
  width: 100%;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.home-logo-egg__brand-block {
  position: relative;
  z-index: 2;
  display: flex;
  flex-direction: column;
  align-items: center;
  flex-shrink: 0;
}

.home-logo-egg__hints {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  min-height: 28px;
  opacity: 0;
  transform: translate3d(0, 6px, 0);
  transition:
    opacity 280ms cubic-bezier(0.2, 1.5, 0.4, 1),
    transform 280ms cubic-bezier(0.2, 1.5, 0.4, 1);
  pointer-events: none;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.home-logo-egg__hints--on {
  opacity: 1;
  transform: translate3d(0, 0, 0);
}

.home-logo-egg__key {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  height: 28px;
  padding: 0 8px;
  border-radius: 8px;
  border: 1.5px solid rgb(var(--c-line));
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
  box-shadow: 2px 2px 0 #3b2f4a33;
}

.home-logo-egg__hint-label {
  opacity: 0.7;
  margin-left: 2px;
}

.home-logo-egg__mark {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 72px;
  height: 72px;
  margin: 0 0 12px;
  padding: 0;
  border: 0;
  background: transparent;
  cursor: pointer;
  border-radius: 18px;
  transition:
    transform 320ms cubic-bezier(0.2, 1.5, 0.4, 1),
    opacity 220ms ease;
  transform-origin: center bottom;
}

.home-logo-egg__mark:hover:not(:disabled),
.home-logo-egg__mark:focus-visible:not(:disabled) {
  transform: translateY(-2px) scale(1.04);
  outline: none;
}

.home-logo-egg__mark:focus-visible:not(:disabled) {
  box-shadow: 0 0 0 3px rgba(123, 97, 255, 0.45);
}

.home-logo-egg__mark:active:not(:disabled) {
  transform: scale(0.92, 1.08);
}

.home-logo-egg__mark--hidden {
  opacity: 0;
  pointer-events: none;
  transform: scale(0.6);
}

.home-logo-egg__mark img {
  display: block;
  width: 72px;
  height: 72px;
  user-select: none;
  -webkit-user-drag: none;
}

.home-brand--playing {
  opacity: 0.22;
  transition: opacity 320ms ease;
}

.home-brand {
  display: inline-flex;
  align-items: baseline;
  margin: 0;
  min-height: 1.1em;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: clamp(2.5rem, 8.5vw, 4.25rem);
  font-weight: 600;
  letter-spacing: 0.04em;
  line-height: 1.05;
  color: rgb(var(--c-txt));
}

.home-brand__text {
  white-space: pre;
}

.home-brand__cursor {
  display: inline-block;
  width: 0.08em;
  height: 0.92em;
  margin-left: 0.06em;
  vertical-align: -0.06em;
  flex-shrink: 0;
  background: rgb(var(--c-accent));
  opacity: 1;
  transition: opacity 0.2s ease;
}

.home-brand__cursor--blink {
  animation: home-brand-caret 0.85s steps(1) 3;
}

.home-brand__cursor--gone {
  opacity: 0;
}

.home-logo-egg__score {
  position: absolute;
  left: 50%;
  top: 8px;
  transform: translateX(-50%);
  padding: 4px 12px;
  border-radius: 999px;
  background: linear-gradient(90deg, #7b61ff, #a78bfa);
  color: #fff8ef;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.04em;
  box-shadow: 2px 2px 0 #3b2f4a66;
  animation: home-logo-score-in 420ms cubic-bezier(0.2, 1.5, 0.4, 1);
  z-index: 3;
  pointer-events: none;
}

.home-logo-egg__ui {
  position: relative;
  z-index: 2;
  display: flex;
  width: 100%;
  flex-direction: column;
  align-items: center;
  will-change: transform, opacity;
}

.home-logo-egg__canvas {
  position: absolute;
  inset: 0;
  z-index: 1;
  display: block;
  width: 100%;
  height: 100%;
  pointer-events: none;
  opacity: 0;
  transition: opacity 280ms ease;
}

.home-logo-egg__canvas--active {
  opacity: 1;
  pointer-events: auto;
}

@keyframes home-brand-caret {
  0%,
  49% {
    opacity: 1;
  }
  50%,
  100% {
    opacity: 0;
  }
}

@keyframes home-logo-score-in {
  from {
    opacity: 0;
    transform: translateX(-50%) translateY(8px) scale(0.9);
  }
  to {
    opacity: 1;
    transform: translateX(-50%) translateY(0) scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .home-logo-egg__hints,
  .home-logo-egg__mark,
  .home-logo-egg__score,
  .home-logo-egg__ui,
  .home-logo-egg__canvas {
    transition: none !important;
    animation: none !important;
  }

  .home-brand__cursor {
    opacity: 0 !important;
    animation: none !important;
  }

  .home-logo-egg__mark {
    cursor: default;
  }
}
</style>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    /** Home composer is focused: look down at it instead of following the pointer. */
    attentive?: boolean
    /** Bumped on every composer input; plays a throttled nod. */
    typingTick?: number
    /** Bumped when a sendable composer is submitted; plays a cheer. */
    cheerTick?: number
  }>(),
  { attentive: false, typingTick: 0, cheerTick: 0 },
)

type IdleAction = 'look' | 'ear' | 'tilt' | 'stretch'

interface Heart {
  id: number
  left: number
  drift: number
  delay: number
}

const IDLE_DURATION_MS: Record<IdleAction, number> = { look: 1600, ear: 700, tilt: 1400, stretch: 1200 }
const IDLE_ACTIONS = Object.keys(IDLE_DURATION_MS) as IdleAction[]
const IDLE_MIN_MS = 6000
const IDLE_SPREAD_MS = 4000
const SLEEP_AFTER_MS = 45_000
const TAP_WINDOW_MS = 800
const JUMP_MS = 360
const FLIP_MS = 700
const WAKE_MS = 400
const NOD_MS = 220
const NOD_REST_MS = 150
const CHEER_MS = 650
const HEARTS_MS = 1400
const MAX_HEARTS = 15

const rootRef = ref<HTMLElement | null>(null)
const pressed = ref(false)
const jumping = ref(false)
const flipping = ref(false)
const hovering = ref(false)
const sleeping = ref(false)
const waking = ref(false)
const nodding = ref(false)
const cheering = ref(false)
const idleAction = ref<IdleAction | null>(null)
const hearts = ref<Heart[]>([])
const reducedMotion = ref(false)

let mounted = false
let motionMql: MediaQueryList | null = null
let taps = 0
let lastTapAt = -Infinity
let lastIdle: IdleAction | null = null
let lastActivityAt = 0
let lastNodEndAt = -Infinity
let heartSeq = 0
const timers = new Map<string, ReturnType<typeof setTimeout>>()

function later(key: string, ms: number, fn: () => void) {
  cancel(key)
  timers.set(
    key,
    setTimeout(() => {
      timers.delete(key)
      fn()
    }, ms),
  )
}

function cancel(key: string) {
  const timer = timers.get(key)
  if (timer == null) return
  clearTimeout(timer)
  timers.delete(key)
}

function cancelAll() {
  for (const timer of timers.values()) clearTimeout(timer)
  timers.clear()
}

function motionAllowed() {
  return mounted && !reducedMotion.value
}

function pageHidden() {
  return typeof document !== 'undefined' && document.hidden
}

function syncReducedMotion() {
  reducedMotion.value =
    motionMql?.matches ?? false
}

function eyesEl(): SVGGElement | null {
  return (rootRef.value?.querySelector('.c-eyes') as SVGGElement | null) ?? null
}

function resetEyes() {
  const eyes = eyesEl()
  if (eyes) eyes.style.transform = ''
}

function stopAll() {
  cancelAll()
  taps = 0
  lastTapAt = -Infinity
  pressed.value = false
  jumping.value = false
  flipping.value = false
  sleeping.value = false
  waking.value = false
  nodding.value = false
  cheering.value = false
  idleAction.value = null
  hearts.value = []
  resetEyes()
}

function startAmbient() {
  if (!motionAllowed() || pageHidden()) return
  lastActivityAt = Date.now()
  scheduleIdle()
  scheduleSleep()
}

function scheduleIdle() {
  later('idle', IDLE_MIN_MS + Math.random() * IDLE_SPREAD_MS, runIdle)
}

function canIdle() {
  return !(
    hovering.value ||
    pressed.value ||
    jumping.value ||
    flipping.value ||
    cheering.value ||
    nodding.value ||
    waking.value ||
    sleeping.value ||
    idleAction.value ||
    props.attentive ||
    pageHidden()
  )
}

function runIdle() {
  if (canIdle()) {
    const options = IDLE_ACTIONS.filter((a) => a !== lastIdle)
    const action = options[Math.floor(Math.random() * options.length)] ?? options[0]
    lastIdle = action
    idleAction.value = action
    later('idleEnd', IDLE_DURATION_MS[action], () => {
      idleAction.value = null
    })
  }
  scheduleIdle()
}

function clearIdle() {
  cancel('idleEnd')
  idleAction.value = null
}

function scheduleSleep(delay = SLEEP_AFTER_MS) {
  later('sleep', delay, checkSleep)
}

function checkSleep() {
  if (props.attentive) {
    scheduleSleep()
    return
  }
  const idleFor = Date.now() - lastActivityAt
  if (idleFor < SLEEP_AFTER_MS) {
    scheduleSleep(SLEEP_AFTER_MS - idleFor)
    return
  }
  clearIdle()
  resetEyes()
  sleeping.value = true
}

function wake() {
  sleeping.value = false
  waking.value = true
  later('wake', WAKE_MS, () => {
    waking.value = false
  })
  scheduleSleep()
}

function noteActivity() {
  if (!motionAllowed()) return
  lastActivityAt = Date.now()
  if (sleeping.value) wake()
}

function onPointerMove(e: PointerEvent) {
  noteActivity()
  if (reducedMotion.value || props.attentive || sleeping.value) return
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

function onPointerEnter() {
  hovering.value = true
}

function onPointerLeave() {
  hovering.value = false
  releasePress()
}

function onPointerDown(e: PointerEvent) {
  if (reducedMotion.value) return
  if (e.pointerType === 'mouse' && e.button !== 0) return
  pressed.value = true
  clearIdle()
}

function jump() {
  if (jumping.value) return
  jumping.value = true
  later('jump', JUMP_MS, () => {
    jumping.value = false
  })
}

function flip() {
  if (flipping.value) return
  flipping.value = true
  later('flip', FLIP_MS, () => {
    flipping.value = false
  })
}

function spawnHearts() {
  if (hearts.value.length >= MAX_HEARTS) return
  const count = 3 + Math.floor(Math.random() * 3)
  const batch: Heart[] = []
  for (let i = 0; i < count; i++) {
    batch.push({
      id: ++heartSeq,
      left: 80 + Math.random() * 60,
      drift: Math.random() * 70 - 35,
      delay: i * 90,
    })
  }
  hearts.value = [...hearts.value, ...batch]
  later('hearts', HEARTS_MS, () => {
    hearts.value = []
  })
}

/** Taps inside TAP_WINDOW_MS escalate: 1 = jump, 2 = flip, 3+ = jump with hearts. */
function bounce() {
  if (!motionAllowed()) return
  noteActivity()
  clearIdle()
  const now = Date.now()
  taps = now - lastTapAt < TAP_WINDOW_MS ? taps + 1 : 1
  lastTapAt = now
  if (taps === 2) {
    flip()
    return
  }
  jump()
  if (taps >= 3) spawnHearts()
}

function onPointerUp(e: PointerEvent) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  releasePress()
  bounce()
}

function onClick(e: MouseEvent) {
  if (e.detail === 0) bounce()
}

function nod() {
  if (!motionAllowed()) return
  noteActivity()
  if (nodding.value || Date.now() - lastNodEndAt < NOD_REST_MS) return
  clearIdle()
  nodding.value = true
  later('nod', NOD_MS, () => {
    nodding.value = false
    lastNodEndAt = Date.now()
  })
}

function cheer() {
  if (!motionAllowed()) return
  noteActivity()
  if (cheering.value) return
  clearIdle()
  cheering.value = true
  later('cheer', CHEER_MS, () => {
    cheering.value = false
  })
}

watch(
  () => props.attentive,
  (on) => {
    if (!motionAllowed()) return
    noteActivity()
    if (on) {
      clearIdle()
      resetEyes()
    }
  },
)
watch(() => props.typingTick, nod)
watch(() => props.cheerTick, cheer)

function onVisibilityChange() {
  if (!motionAllowed()) return
  if (pageHidden()) {
    cancel('idle')
    cancel('sleep')
    clearIdle()
  } else {
    startAmbient()
  }
}

function onMotionChange() {
  syncReducedMotion()
  if (reducedMotion.value) stopAll()
  else startAmbient()
}

onMounted(() => {
  if (typeof window === 'undefined') return
  mounted = true
  window.addEventListener('pointermove', onPointerMove, { passive: true })
  window.addEventListener('pointerdown', noteActivity, { passive: true })
  window.addEventListener('keydown', noteActivity)
  document.addEventListener('visibilitychange', onVisibilityChange)
  motionMql = window.matchMedia('(prefers-reduced-motion: reduce)')
  syncReducedMotion()
  motionMql.addEventListener('change', onMotionChange)
  startAmbient()
})

onBeforeUnmount(() => {
  mounted = false
  if (typeof window !== 'undefined') {
    window.removeEventListener('pointermove', onPointerMove)
    window.removeEventListener('pointerdown', noteActivity)
    window.removeEventListener('keydown', noteActivity)
    document.removeEventListener('visibilitychange', onVisibilityChange)
  }
  motionMql?.removeEventListener('change', onMotionChange)
  motionMql = null
  stopAll()
})
</script>

<template>
  <div
    ref="rootRef"
    class="home-yume"
    :class="[
      {
        pressed,
        jump: jumping,
        flip: flipping,
        hearts: hearts.length > 0,
        cheer: cheering,
        nod: nodding,
        wake: waking,
        sleeping,
        attentive: attentive && !reducedMotion,
        'home-yume--static': reducedMotion,
      },
      idleAction ? `idle-${idleAction}` : '',
    ]"
    data-testid="home-yume"
    role="button"
    tabindex="0"
    aria-label="yume"
    @pointerenter="onPointerEnter"
    @pointerdown="onPointerDown"
    @pointerup="onPointerUp"
    @pointercancel="releasePress"
    @pointerleave="onPointerLeave"
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
          <g class="c-act">
            <g class="c-breathe">
              <!-- Overlap the root under the white body so no gap opens during a wag. -->
              <g class="c-tail-anchor" transform="translate(138 142)">
                <path class="k c-tail" d="M0 0 C20 -2 33 -14 30 -36" fill="none" />
              </g>
              <!-- Ear tips are cut below the ear overlays, so a twitching ear never reveals a second outline. -->
              <path
                class="k c-body"
                d="M58 160 L58 92 L62 75 L78 70 L84 76 Q100 70 116 76 L122 70 L138 75 L142 92 L142 160 Q131 150 121 162 Q110 150 100 162 Q90 150 79 162 Q69 150 58 160Z"
                fill="#FFFFFF"
              />
              <path class="k c-ear-l" d="M59.9 84 L66 58 L84 76" fill="#FFFFFF" />
              <path class="k c-ear-r" d="M116 76 L134 58 L140.1 84" fill="#FFFFFF" />
              <g class="c-face">
                <g class="c-eyes">
                  <g class="c-eye"><ellipse cx="84" cy="110" rx="6" ry="8" fill="#1a1a1a" /></g>
                  <g class="c-eye"><ellipse cx="116" cy="110" rx="6" ry="8" fill="#1a1a1a" /></g>
                </g>
                <path class="k c-mouth" d="M92 124 Q96 130 100 124 Q104 130 108 124" fill="none" />
                <path class="k c-mouth-open" d="M91 122 Q100 136 109 122 Z" fill="#1a1a1a" />
                <g class="c-blush">
                  <circle cx="74" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
                  <circle cx="126" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
                </g>
              </g>
            </g>
          </g>
        </g>
        <g v-if="sleeping" class="c-zz-group">
          <text class="c-zz" x="146" y="62">z</text>
          <text class="c-zz c-zz--late" x="158" y="50">z</text>
        </g>
      </g>
    </svg>
    <svg
      v-for="heart in hearts"
      :key="heart.id"
      class="c-heart"
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 22"
      width="22"
      height="20"
      aria-hidden="true"
      focusable="false"
      :style="{ left: `${heart.left}px`, '--heart-drift': `${heart.drift}px`, animationDelay: `${heart.delay}ms` }"
    >
      <path
        d="M12 20 C4 14 1 10 1 6.5 A5 5 0 0 1 12 4 A5 5 0 0 1 23 6.5 C23 10 20 14 12 20Z"
        fill="#c8c8c8"
        stroke="#1a1a1a"
        stroke-width="2.2"
        stroke-linejoin="round"
      />
    </svg>
  </div>
</template>

<style scoped>
.home-yume {
  /* fill LOCKED: 240x200 crop; SVG 240x240 + margin-top -28px (feet near clip) */
  box-sizing: border-box;
  position: relative;
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

.home-yume :deep(.c-act) {
  transform-box: view-box;
  transform-origin: 100px 160px;
  transition: transform 320ms ease-out;
}

.home-yume :deep(.c-breathe) {
  transform-box: view-box;
  transform-origin: 100px 156px;
  animation: yume-c-breathe 6.8s ease-in-out infinite;
}

.home-yume :deep(.c-ear-l),
.home-yume :deep(.c-ear-r) {
  transform-box: view-box;
}
.home-yume :deep(.c-ear-l) { transform-origin: 70px 80px; }
.home-yume :deep(.c-ear-r) { transform-origin: 130px 80px; }

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
  transition: transform 180ms ease-out, opacity 120ms ease-out;
}
.home-yume :deep(.c-mouth-open) {
  opacity: 0;
  transition: opacity 120ms ease-out;
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

/* Idle actions: one at a time, each on its own layer. */
.home-yume.idle-look :deep(.c-eyes) { animation: yume-c-look 1.6s ease-in-out; }
.home-yume.idle-ear :deep(.c-ear-l) { animation: yume-c-ear-l 500ms ease-in-out; }
.home-yume.idle-ear :deep(.c-ear-r) { animation: yume-c-ear-r 500ms ease-in-out 150ms; }
.home-yume.idle-tilt :deep(.c-act) { animation: yume-c-tilt 1.4s ease-in-out; }
.home-yume.idle-stretch :deep(.c-act) { animation: yume-c-stretch 1.2s ease-in-out; }

@keyframes yume-c-look {
  20%, 40% { transform: translate(-6px, 0); }
  60%, 80% { transform: translate(6px, -2px); }
}

@keyframes yume-c-ear-l {
  25%, 75% { transform: rotate(-12deg); }
  50% { transform: rotate(5deg); }
}

@keyframes yume-c-ear-r {
  25%, 75% { transform: rotate(12deg); }
  50% { transform: rotate(-5deg); }
}

@keyframes yume-c-tilt {
  25%, 70% { transform: rotate(-7deg); }
}

@keyframes yume-c-stretch {
  30% { transform: scale(1.07, 0.9); }
  65% { transform: scale(0.95, 1.1) translateY(-3%); }
}

/* Dozing: eyes shut, body sinks, z drifts up. */
.home-yume.sleeping :deep(.c-eye) {
  animation: none;
  transform: scaleY(.12);
  transition: transform 300ms ease-in;
}
.home-yume.sleeping :deep(.c-act) {
  transform: translateY(3%) rotate(3deg);
  transition-duration: 600ms;
}
.home-yume :deep(.c-zz) {
  font: 800 18px/1 ui-rounded, system-ui, sans-serif;
  fill: #c8c8c8;
  stroke: #1a1a1a;
  stroke-width: 1.4;
  paint-order: stroke;
  opacity: 0;
  transform-box: fill-box;
  transform-origin: center;
  animation: yume-c-zz 2.2s ease-out infinite;
}
.home-yume :deep(.c-zz--late) { animation-delay: 1.1s; }
.home-yume.wake :deep(.c-act) { animation: yume-c-wake 400ms ease-out; }

@keyframes yume-c-zz {
  0% { transform: translate(0, 0) scale(.7); opacity: 0; }
  30% { opacity: 1; }
  100% { transform: translate(12px, -26px) scale(1.1); opacity: 0; }
}

@keyframes yume-c-wake {
  0%, 100% { transform: none; }
  25% { transform: translateY(-4%) rotate(-3deg); }
  50% { transform: translateY(-4%) rotate(3deg); }
}

/* Composer focus: lean in and look down; typing nods. */
.home-yume.attentive :deep(.c-eyes) { transform: translate(0, 5px) !important; }
.home-yume.attentive :deep(.c-act) { transform: rotate(2deg); }
.home-yume.nod :deep(.c-act) { animation: yume-c-nod 220ms ease-in-out; }

@keyframes yume-c-nod {
  50% { transform: translateY(2%) rotate(3deg); }
}

.home-yume.flip :deep(.c-act) {
  transform-origin: 100px 120px;
  animation: yume-c-flip 0.7s cubic-bezier(0.3, 1.2, 0.5, 1);
}

@keyframes yume-c-flip {
  0% { transform: rotate(0); }
  40% { transform: translateY(-30%) rotate(-180deg); }
  80% { transform: rotate(-360deg) scale(1.08, 0.9); }
  100% { transform: rotate(-360deg); }
}

/* Press and jump paint outside the 240×200 crop. Rest keeps overflow hidden. */
.home-yume.jump,
.home-yume.pressed {
  overflow: visible;
}

.home-yume.flip,
.home-yume.hearts,
.home-yume.cheer {
  overflow: visible;
}

.home-yume.pressed :deep(.c-squash) {
  transform: scale(1.08, 0.82) !important;
  transition: transform 0.08s ease-out;
}

.home-yume.cheer :deep(.c-squash) {
  animation: yume-c-cheer 0.6s cubic-bezier(0.2, 1.5, 0.4, 1);
}
.home-yume.cheer :deep(.c-mouth) { opacity: 0; }
.home-yume.cheer :deep(.c-mouth-open) { opacity: 1; }

@keyframes yume-c-cheer {
  0% { transform: scale(1.1, 0.8); }
  40% { transform: translateY(-18%) scale(0.94, 1.08); }
  100% { transform: none; }
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

.c-heart {
  position: absolute;
  top: 36px;
  width: 22px;
  height: 20px;
  pointer-events: none;
  animation: yume-c-heart 1s ease-out both;
}

@keyframes yume-c-heart {
  0% { transform: translate(0, 0) scale(.3); opacity: 0; }
  25% { transform: translate(var(--heart-drift), -30px) scale(1); opacity: 1; }
  100% { transform: translate(calc(var(--heart-drift) * 1.6), -100px) scale(.8); opacity: 0; }
}

.home-yume--static :deep(:is(.c-float, .c-squash, .c-act, .c-breathe, .c-ear-l, .c-ear-r, .c-eye, .c-eyes, .c-face, .c-mouth, .c-blush, .c-tail, .c-shadow)) {
  animation: none !important;
  transform: none !important;
  transition: none !important;
}
@media (prefers-reduced-motion: reduce) {
  .home-yume :deep(:is(.c-float, .c-squash, .c-act, .c-breathe, .c-ear-l, .c-ear-r, .c-eye, .c-eyes, .c-face, .c-mouth, .c-blush, .c-tail, .c-shadow, .c-zz)),
  .c-heart {
    animation: none !important;
    transform: none !important;
    transition: none !important;
  }
}
</style>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    /** Rendered edge length in px. Sidebar uses 28, login uses 48. */
    size?: number
  }>(),
  { size: 28 },
)

type IdleAction = 'look' | 'tilt'

const IDLE_MIN_MS = 8000
const IDLE_SPREAD_MS = 6000
const IDLE_MS = 1400
const TAP_WINDOW_MS = 800
const JUMP_MS = 360
const FLIP_MS = 600

const jumping = ref(false)
const flipping = ref(false)
const hovering = ref(false)
const idleAction = ref<IdleAction | null>(null)
const reduced = ref(false)

let mounted = false
let motionMql: MediaQueryList | null = null
let responseTimer: ReturnType<typeof setTimeout> | null = null
let idleTimer: ReturnType<typeof setTimeout> | null = null
let idleEndTimer: ReturnType<typeof setTimeout> | null = null
let taps = 0
let lastTapAt = -Infinity
let lastIdle: IdleAction | null = null

function stopResponse() {
  if (responseTimer != null) clearTimeout(responseTimer)
  responseTimer = null
  jumping.value = false
  flipping.value = false
  taps = 0
  lastTapAt = -Infinity
}

function clearIdleAction() {
  if (idleEndTimer != null) clearTimeout(idleEndTimer)
  idleEndTimer = null
  idleAction.value = null
}

function stopIdle() {
  if (idleTimer != null) clearTimeout(idleTimer)
  idleTimer = null
  clearIdleAction()
}

function scheduleIdle() {
  if (!mounted || reduced.value) return
  if (idleTimer != null) clearTimeout(idleTimer)
  idleTimer = setTimeout(runIdle, IDLE_MIN_MS + Math.random() * IDLE_SPREAD_MS)
}

function runIdle() {
  idleTimer = null
  const busy = hovering.value || jumping.value || flipping.value || idleAction.value != null
  const hidden = typeof document !== 'undefined' && document.hidden
  if (!busy && !hidden) {
    const action: IdleAction = lastIdle === 'look' ? 'tilt' : 'look'
    lastIdle = action
    idleAction.value = action
    idleEndTimer = setTimeout(() => {
      idleEndTimer = null
      idleAction.value = null
    }, IDLE_MS)
  }
  scheduleIdle()
}

function syncReduced() {
  reduced.value = !!motionMql?.matches
  if (reduced.value) {
    stopResponse()
    stopIdle()
  } else {
    scheduleIdle()
  }
}

function respond(kind: 'jump' | 'flip', ms: number) {
  jumping.value = kind === 'jump'
  flipping.value = kind === 'flip'
  if (responseTimer != null) clearTimeout(responseTimer)
  responseTimer = setTimeout(() => {
    jumping.value = false
    flipping.value = false
    responseTimer = null
  }, ms)
}

/** Taps inside TAP_WINDOW_MS escalate: 1 = jump, 2+ = flip. */
function onPointerDown(e: PointerEvent) {
  if (reduced.value) return
  if (e.pointerType === 'mouse' && e.button !== 0) return
  clearIdleAction()
  const now = Date.now()
  taps = now - lastTapAt < TAP_WINDOW_MS ? taps + 1 : 1
  lastTapAt = now
  if (taps === 1) respond('jump', JUMP_MS)
  else if (!flipping.value) respond('flip', FLIP_MS)
}

onMounted(() => {
  if (typeof window === 'undefined' || !window.matchMedia) return
  mounted = true
  motionMql = window.matchMedia('(prefers-reduced-motion: reduce)')
  motionMql.addEventListener('change', syncReduced)
  syncReduced()
})

onBeforeUnmount(() => {
  mounted = false
  motionMql?.removeEventListener('change', syncReduced)
  motionMql = null
  stopResponse()
  stopIdle()
})
</script>

<template>
  <div
    class="brand-character"
    :class="[
      { jump: jumping, flip: flipping, 'brand-character--static': reduced },
      idleAction ? `idle-${idleAction}` : '',
    ]"
    data-testid="brand-character-mark"
    :data-size="props.size"
    :style="{ width: `${props.size}px`, height: `${props.size}px` }"
    aria-hidden="true"
    @pointerenter="hovering = true"
    @pointerleave="hovering = false"
    @pointerdown="onPointerDown"
  >
    <svg class="brand-character__svg" viewBox="48 52 104 118" focusable="false">
      <g class="brand-character__response">
        <g class="brand-character__jump">
          <g class="brand-character__idle">
            <g class="brand-character__motion">
              <g class="brand-character__breath">
                <path
                  d="M58 160 L58 92 L66 58 L84 76 Q100 70 116 76 L134 58 L142 92 L142 160 Q131 150 121 162 Q110 150 100 162 Q90 150 79 162 Q69 150 58 160Z"
                  fill="#fff"
                  stroke="#1a1a1a"
                  stroke-width="6"
                  stroke-linejoin="round"
                />
                <g class="brand-character__face">
                  <g class="brand-character__eyes">
                    <ellipse class="brand-character__eye" cx="84" cy="110" rx="6" ry="8" fill="#1a1a1a" />
                    <ellipse class="brand-character__eye brand-character__eye--right" cx="116" cy="110" rx="6" ry="8" fill="#1a1a1a" />
                  </g>
                  <path
                    d="M92 124 Q96 130 100 124 Q104 130 108 124"
                    fill="none"
                    stroke="#1a1a1a"
                    stroke-width="5"
                    stroke-linecap="round"
                  />
                  <circle cx="74" cy="126" r="6" fill="#c8c8c8" />
                  <circle cx="126" cy="126" r="6" fill="#c8c8c8" />
                </g>
              </g>
            </g>
          </g>
        </g>
      </g>
    </svg>
  </div>
</template>

<style scoped>
.brand-character {
  display: block;
  flex: none;
  padding: 0;
  border: 0;
  background: transparent;
  line-height: 0;
  filter: none;
  user-select: none;
  -webkit-user-select: none;
}

.brand-character__svg {
  display: block;
  width: 100%;
  height: 100%;
  overflow: visible;
}

.brand-character__response,
.brand-character__jump,
.brand-character__idle,
.brand-character__motion,
.brand-character__breath {
  transform-box: fill-box;
  transform-origin: 50% 100%;
}

.brand-character__response { transition: transform 260ms cubic-bezier(.22, 1, .36, 1); }
.brand-character__motion { animation: brand-character-sway 5.6s ease-in-out infinite; }
.brand-character__breath { animation: brand-character-breathe 3.8s ease-in-out infinite; }
.brand-character__face { transition: transform 220ms ease; }
.brand-character__eyes { transform-box: fill-box; }
.brand-character__eye {
  transform-box: fill-box;
  transform-origin: center;
  animation: brand-character-blink 6.8s ease-in-out infinite;
}
.brand-character__eye--right { animation-delay: 35ms; }

.brand-character:hover .brand-character__response,
[data-testid="sidebar-brand-lockup"]:hover .brand-character__response,
[data-testid="sidebar-brand-lockup"]:focus-within .brand-character__response {
  transform: translateY(-1.5%) rotate(-3deg) scale(1.035);
}

.brand-character:hover .brand-character__face,
[data-testid="sidebar-brand-lockup"]:hover .brand-character__face,
[data-testid="sidebar-brand-lockup"]:focus-within .brand-character__face {
  transform: translateY(-1px);
}

.brand-character.idle-look .brand-character__eyes {
  animation: brand-character-look 1.4s ease-in-out;
}

.brand-character.idle-tilt .brand-character__idle {
  animation: brand-character-tilt 1.2s ease-in-out;
}

.brand-character.jump .brand-character__jump {
  animation: brand-character-jump 360ms cubic-bezier(.22, 1, .36, 1);
}

.brand-character.flip .brand-character__jump {
  transform-origin: 50% 60%;
  animation: brand-character-flip 600ms cubic-bezier(.3, 1.2, .5, 1);
}

@keyframes brand-character-sway {
  0%, 100% { transform: translateY(0) rotate(-.8deg); }
  50% { transform: translateY(-1.5%) rotate(.8deg); }
}

@keyframes brand-character-breathe {
  0%, 100% { transform: scale(1); }
  45% { transform: scale(1.012, 1.023); }
}

@keyframes brand-character-blink {
  0%, 64%, 70%, 76%, 100% { transform: scaleY(1); }
  66%, 68% { transform: scaleY(.1); }
  72%, 74% { transform: scaleY(.16); }
}

@keyframes brand-character-look {
  25%, 45% { transform: translateX(-5px); }
  65%, 85% { transform: translateX(5px); }
}

@keyframes brand-character-tilt {
  25%, 70% { transform: rotate(-8deg); }
}

@keyframes brand-character-jump {
  0% { transform: translateY(0) scale(1); }
  20% { transform: translateY(0) scale(1.055, .93); }
  55% { transform: translateY(-8px) scale(.98, 1.055); }
  82% { transform: translateY(0) scale(1.025, .98); }
  100% { transform: translateY(0) scale(1); }
}

@keyframes brand-character-flip {
  0% { transform: rotate(0); }
  40% { transform: translateY(-6px) rotate(-180deg); }
  100% { transform: rotate(-360deg); }
}

.brand-character--static :is(.brand-character__response, .brand-character__jump, .brand-character__idle, .brand-character__motion, .brand-character__breath, .brand-character__face, .brand-character__eyes, .brand-character__eye) {
  animation: none !important;
  transition: none !important;
  transform: none !important;
}

@media (prefers-reduced-motion: reduce) {
  .brand-character__response,
  .brand-character__jump,
  .brand-character__idle,
  .brand-character__motion,
  .brand-character__breath,
  .brand-character__face,
  .brand-character__eyes,
  .brand-character__eye {
    animation: none !important;
    transition: none !important;
    transform: none !important;
  }
}
</style>

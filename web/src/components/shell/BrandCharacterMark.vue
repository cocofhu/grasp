<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    /** Rendered edge length in px. Sidebar uses 28, login uses 48. */
    size?: number
  }>(),
  { size: 28 },
)

type IdleAction = 'look' | 'happy' | 'tilt' | 'surprised' | 'wink' | 'sleepy'

/** Small-size cut of expression sheet A, played in this order. */
const IDLE_MS: Record<IdleAction, number> = {
  look: 1400,
  happy: 1600,
  tilt: 1200,
  surprised: 1400,
  wink: 1200,
  sleepy: 2000,
}
const IDLE_ORDER = Object.keys(IDLE_MS) as IdleAction[]
const IDLE_MIN_MS = 8000
const IDLE_SPREAD_MS = 6000
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
    const action = lastIdle ? IDLE_ORDER[(IDLE_ORDER.indexOf(lastIdle) + 1) % IDLE_ORDER.length] : IDLE_ORDER[0]
    lastIdle = action
    idleAction.value = action
    idleEndTimer = setTimeout(() => {
      idleEndTimer = null
      idleAction.value = null
    }, IDLE_MS[action])
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
    <svg class="brand-character__svg" viewBox="50 51 120 119" focusable="false">
      <g class="brand-character__response">
        <g class="brand-character__jump">
          <g class="brand-character__idle">
            <g class="brand-character__motion">
              <g class="brand-character__breath">
                <g class="brand-character__tail">
                  <path d="M139 150 C155 151 165 140 165 123 C165 115 162 109 158 106" fill="none" stroke="#fff" stroke-width="9" stroke-linecap="round" />
                  <path d="M139 150 C155 151 165 140 165 123 C165 115 162 109 158 106" fill="none" stroke="#1a1a1a" stroke-width="5" stroke-linecap="round" />
                </g>
                <path
                  d="M58 160 C55 136 55 108 59 90 C60 80 62 69 64 61 Q66 55 70 59 L84 74 Q100 68 116 74 L130 59 Q134 55 136 61 C138 69 140 80 141 90 C145 108 145 136 142 160 C139.5 165.5 132.5 165.5 130 160 C127.5 155.5 120.5 155.5 118 160 C115.5 165.5 108.5 165.5 106 160 C103.5 155.5 96.5 155.5 94 160 C91.5 165.5 84.5 165.5 82 160 C79.5 155.5 72.5 155.5 70 160 C67.5 165.5 60.5 165.5 58 160Z"
                  fill="#fff"
                  stroke="#1a1a1a"
                  stroke-width="5"
                  stroke-linejoin="round"
                />
                <g class="brand-character__face">
                  <g class="brand-character__eyes">
                    <g class="bc-part bc-dot on">
                      <ellipse class="brand-character__eye" cx="84" cy="110" rx="6" ry="8" fill="#1a1a1a" />
                    </g>
                    <g class="bc-part bc-dot bc-dot--r on">
                      <ellipse class="brand-character__eye brand-character__eye--right" cx="116" cy="110" rx="6" ry="8" fill="#1a1a1a" />
                    </g>
                    <path class="bc-part bc-line bc-arc" d="M77 113 Q84 101 91 113" />
                    <path class="bc-part bc-line bc-arc bc-arc--r" d="M109 113 Q116 101 123 113" />
                    <path class="bc-part bc-line bc-shut" d="M77 109 Q84 117 91 109 M109 109 Q116 117 123 109" />
                  </g>
                  <path
                    class="bc-part bc-mouth on"
                    d="M92 124 Q96 130 100 124 Q104 130 108 124"
                    fill="none"
                    stroke="#1a1a1a"
                    stroke-width="5"
                    stroke-linecap="round"
                  />
                  <path class="bc-part bc-open" d="M90 121 Q100 124 110 121 Q109 137 100 137 Q91 137 90 121Z" fill="#fff" stroke="#1a1a1a" stroke-width="5" stroke-linejoin="round" />
                  <ellipse class="bc-part bc-o" cx="100" cy="127" rx="5" ry="6.5" fill="#fff" stroke="#1a1a1a" stroke-width="5" />
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
.brand-character__face { transition: transform 220ms ease; }
.brand-character__tail {
  transform-box: view-box;
  transform-origin: 139px 150px;
}
.brand-character__eyes { transform-box: fill-box; }
.brand-character__eye {
  transform-box: fill-box;
  transform-origin: center;
}
.brand-character__eye--right { animation-delay: 35ms; }

.bc-line {
  fill: none;
  stroke: #1a1a1a;
  stroke-width: 5;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.bc-part {
  opacity: 0;
  transform: scale(1, .1);
  transform-box: fill-box;
  transform-origin: center;
  transition: opacity 80ms ease-in 30ms, transform 110ms ease-in;
}
.bc-part.on {
  opacity: 1;
  transform: none;
  transition: opacity 80ms ease-out 100ms, transform 260ms cubic-bezier(.34, 1.56, .5, 1) 100ms;
}

/* Expressions swap parts like a blink: the old part squeezes shut, then the new one springs open. */
.brand-character:is(.idle-happy, :hover) :is(.bc-dot, .bc-mouth),
[data-testid="sidebar-brand-lockup"]:hover .brand-character :is(.bc-dot, .bc-mouth),
.brand-character.idle-surprised .bc-mouth,
.brand-character.idle-wink :is(.bc-dot--r, .bc-mouth),
.brand-character.idle-sleepy :is(.bc-dot, .bc-mouth) {
  opacity: 0;
  transform: scale(1, .1);
  transition: opacity 80ms ease-in 30ms, transform 110ms ease-in;
}
.brand-character:is(.idle-happy, :hover) :is(.bc-arc, .bc-open),
[data-testid="sidebar-brand-lockup"]:hover .brand-character :is(.bc-arc, .bc-open),
.brand-character.idle-surprised .bc-o,
.brand-character.idle-wink :is(.bc-arc--r, .bc-open),
.brand-character.idle-sleepy :is(.bc-shut, .bc-o) {
  opacity: 1;
  transform: none;
  transition: opacity 80ms ease-out 100ms, transform 260ms cubic-bezier(.34, 1.56, .5, 1) 100ms;
}
.brand-character.idle-surprised .bc-dot { transform: scale(.85); }
.brand-character.idle-sleepy .bc-o { transform: scale(.55); }
.brand-character.idle-surprised .brand-character__idle { animation: brand-character-pop 1.4s ease-in-out; }
.brand-character.idle-sleepy .brand-character__idle { animation: brand-character-doze 2s ease-in-out; }

/* Animated SVG children repaint every frame (~60% of a core while the sidebar is
   on screen), so loops run only while hovered; at rest the mark is still between
   the timed idle actions. */
.brand-character:hover .brand-character__motion,
[data-testid="sidebar-brand-lockup"]:hover .brand-character__motion { animation: brand-character-sway 5.6s ease-in-out infinite; }
.brand-character:hover .brand-character__breath,
[data-testid="sidebar-brand-lockup"]:hover .brand-character__breath { animation: brand-character-breathe 3.8s ease-in-out infinite; }
.brand-character:hover .brand-character__eye,
[data-testid="sidebar-brand-lockup"]:hover .brand-character__eye { animation: brand-character-blink 6.8s ease-in-out infinite; }
.brand-character:is(.idle-look, .idle-tilt) .brand-character__eye { animation: brand-character-blink 1.4s ease-in-out; }
.brand-character:hover .brand-character__tail,
[data-testid="sidebar-brand-lockup"]:hover .brand-character__tail { animation: brand-character-wag 1.4s ease-in-out infinite; }
.brand-character:is(.idle-happy, .idle-wink) .brand-character__tail { animation: brand-character-wag 600ms ease-in-out 2; }

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

@keyframes brand-character-wag {
  0%, 100% { transform: rotate(0); }
  30% { transform: rotate(12deg); }
  70% { transform: rotate(-6deg); }
}

@keyframes brand-character-look {
  25%, 45% { transform: translateX(-5px); }
  65%, 85% { transform: translateX(5px); }
}

@keyframes brand-character-tilt {
  25%, 70% { transform: rotate(-8deg); }
}

@keyframes brand-character-pop {
  0%, 100% { transform: none; }
  25%, 70% { transform: translateY(-4%) scale(.97, 1.05); }
}

@keyframes brand-character-doze {
  0%, 100% { transform: none; }
  25%, 75% { transform: translateY(3%) rotate(4deg); }
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

.brand-character--static :is(.brand-character__response, .brand-character__jump, .brand-character__idle, .brand-character__motion, .brand-character__breath, .brand-character__tail, .brand-character__face, .brand-character__eyes, .brand-character__eye) {
  animation: none !important;
  transition: none !important;
  transform: none !important;
}
.brand-character--static .bc-part { transition: none !important; }

@media (prefers-reduced-motion: reduce) {
  .bc-part { transition: none !important; }
  .brand-character__response,
  .brand-character__jump,
  .brand-character__idle,
  .brand-character__motion,
  .brand-character__breath,
  .brand-character__tail,
  .brand-character__face,
  .brand-character__eyes,
  .brand-character__eye {
    animation: none !important;
    transition: none !important;
    transform: none !important;
  }
}
</style>

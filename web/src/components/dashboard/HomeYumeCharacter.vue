<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

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

type Mood =
  | 'idle'
  | 'happy'
  | 'laugh'
  | 'surprised'
  | 'sleepy'
  | 'angry'
  | 'worried'
  | 'think'
  | 'wave'
  | 'jump'
  | 'run'
  | 'peek'

interface Heart {
  id: number
  left: number
  drift: number
  delay: number
}

/** Ambient performance order. Each hold is a whole number of that mood's loop, so loops end at rest. */
const MOOD_HOLD_MS: Record<Mood, number> = {
  idle: 3200,
  happy: 2400,
  laugh: 2500,
  surprised: 1800,
  sleepy: 2800,
  angry: 2160,
  worried: 2400,
  think: 2800,
  wave: 2400,
  jump: 2700,
  run: 2520,
  peek: 2600,
}
const MOODS = Object.keys(MOOD_HOLD_MS) as Mood[]

interface Face {
  eyes: 'dot' | 'arc' | 'x' | 'shut' | 'wink' | 'glare'
  mouth: 'w' | 'open' | 'o' | 'tiny' | 'wavy' | 'frown' | 'hm'
  brows?: 'angry' | 'worry'
  arms?: 'wave' | 'up' | 'run'
  shine?: boolean
  paw?: boolean
  fx?: string
}

/** Expression sheet A: every mood is a mix of the same parts, so each one blinks into the next. */
const FACES: Record<Mood, Face> = {
  idle: { eyes: 'dot', mouth: 'w' },
  happy: { eyes: 'arc', mouth: 'open', fx: 'spark' },
  laugh: { eyes: 'x', mouth: 'open', fx: 'hash' },
  surprised: { eyes: 'dot', mouth: 'o', shine: true, fx: 'bang' },
  sleepy: { eyes: 'shut', mouth: 'tiny', fx: 'zz' },
  angry: { eyes: 'glare', mouth: 'frown', brows: 'angry', fx: 'vein' },
  worried: { eyes: 'dot', mouth: 'wavy', brows: 'worry', shine: true, fx: 'sweat' },
  think: { eyes: 'dot', mouth: 'hm', shine: true, paw: true, fx: 'cloud' },
  wave: { eyes: 'wink', mouth: 'open', arms: 'wave', fx: 'waves' },
  jump: { eyes: 'arc', mouth: 'open', arms: 'up', fx: 'hop' },
  run: { eyes: 'x', mouth: 'open', arms: 'run', fx: 'speed' },
  peek: { eyes: 'dot', mouth: 'open', shine: true, fx: 'alert' },
}
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
const mood = ref<Mood>('idle')
const hearts = ref<Heart[]>([])
const reducedMotion = ref(false)

/** Live states win over the ambient performance; transitions between them stay continuous. */
const shownMood = computed<Mood>(() => {
  if (reducedMotion.value) return 'idle'
  if (sleeping.value) return 'sleepy'
  if (cheering.value) return 'jump'
  if (props.attentive) return 'idle'
  if (hovering.value) return 'wave'
  return mood.value
})
const face = computed(() => FACES[shownMood.value])

let mounted = false
let motionMql: MediaQueryList | null = null
let taps = 0
let lastTapAt = -Infinity
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
  mood.value = 'idle'
  hearts.value = []
  resetEyes()
}

function startAmbient() {
  if (!motionAllowed() || pageHidden()) return
  lastActivityAt = Date.now()
  scheduleMood()
  scheduleSleep()
}

function scheduleMood() {
  later('mood', MOOD_HOLD_MS[mood.value], advanceMood)
}

/** Hover, focus and sleep hold the performance in place; it resumes from the same mood. */
function advanceMood() {
  const held = hovering.value || sleeping.value || cheering.value || props.attentive || pageHidden()
  if (!held) mood.value = MOODS[(MOODS.indexOf(mood.value) + 1) % MOODS.length]
  scheduleMood()
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
    if (on) resetEyes()
  },
)
watch(() => props.typingTick, nod)
watch(() => props.cheerTick, cheer)

function onVisibilityChange() {
  if (!motionAllowed()) return
  if (pageHidden()) {
    cancel('mood')
    cancel('sleep')
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
      `mood-${shownMood}`,
    ]"
    data-testid="home-yume"
    :data-mood="shownMood"
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
            <g class="c-pose">
              <g class="c-loop">
                <g class="c-breathe">
                  <!-- Overlap the root under the white body so no gap opens during a wag. -->
                  <g class="c-tail-anchor" transform="translate(139 150)">
                    <g class="c-tail-pose">
                      <path class="k c-tail" d="M0 0 C16 1 26 -10 26 -27 C26 -35 23 -41 19 -44" fill="none" />
                    </g>
                  </g>
                  <!-- Arms rest hidden behind the body edge and swing out from their shoulder. -->
                  <g class="c-arm-anchor" transform="translate(60 126)">
                    <g class="c-arm c-arm--l" :class="face.arms ? `arm-${face.arms}` : ''">
                      <g class="c-arm-swing">
                        <rect class="k" x="-7" y="-6" width="14" height="36" rx="7" fill="#FFFFFF" />
                      </g>
                    </g>
                  </g>
                  <g class="c-arm-anchor" transform="translate(140 126)">
                    <g class="c-arm c-arm--r" :class="face.arms ? `arm-${face.arms}` : ''">
                      <g class="c-arm-swing">
                        <rect class="k" x="-7" y="-6" width="14" height="36" rx="7" fill="#FFFFFF" />
                      </g>
                    </g>
                  </g>
                  <!-- Ear tips are cut below the ear overlays, so a twitching ear never reveals a second outline. -->
                  <path
                    class="k c-body"
                    d="M58 160 C55 136 55 108 59 90 C60 84 61 79 62 75 L77 70 L84 74 Q100 68 116 74 L123 70 L138 75 C139 79 140 84 141 90 C145 108 145 136 142 160 C139.5 165.5 132.5 165.5 130 160 C127.5 155.5 120.5 155.5 118 160 C115.5 165.5 108.5 165.5 106 160 C103.5 155.5 96.5 155.5 94 160 C91.5 165.5 84.5 165.5 82 160 C79.5 155.5 72.5 155.5 70 160 C67.5 165.5 60.5 165.5 58 160Z"
                    fill="#FFFFFF"
                  />
                  <path class="k c-ear-l" d="M59 90 C60 80 62 69 64 61 Q66 55 70 59 L84 74" fill="#FFFFFF" />
                  <path class="k c-ear-r" d="M116 74 L130 59 Q134 55 136 61 C138 69 140 80 141 90" fill="#FFFFFF" />
                  <g class="c-face">
                    <g class="c-eyes">
                      <g class="c-brows">
                        <g class="c-part" :class="{ on: face.brows === 'angry' }">
                          <path class="k c-brow-bold" d="M72 101 L92 108.5 M128 101 L108 108.5" fill="none" />
                        </g>
                        <g class="c-part" :class="{ on: face.brows === 'worry' }">
                          <path class="k" d="M76 95 Q82 90.5 89 89 M124 95 Q118 90.5 111 89" fill="none" />
                        </g>
                      </g>
                      <g class="c-gaze">
                        <g class="c-part c-dot" :class="{ on: face.eyes === 'dot' || face.eyes === 'wink' }">
                          <g class="c-dot-shape">
                            <g class="c-eye">
                              <ellipse cx="84" cy="110" rx="6" ry="8" fill="#1a1a1a" />
                              <circle class="c-shine" :class="{ on: face.shine }" cx="86.4" cy="106.4" r="2.2" fill="#FFFFFF" />
                            </g>
                          </g>
                        </g>
                        <g class="c-part c-dot" :class="{ on: face.eyes === 'dot' }">
                          <g class="c-dot-shape">
                            <g class="c-eye">
                              <ellipse cx="116" cy="110" rx="6" ry="8" fill="#1a1a1a" />
                              <circle class="c-shine" :class="{ on: face.shine }" cx="118.4" cy="106.4" r="2.2" fill="#FFFFFF" />
                            </g>
                          </g>
                        </g>
                        <g class="c-part" :class="{ on: face.eyes === 'arc' }">
                          <path class="k" d="M77 113 Q84 102 91 113" fill="none" />
                        </g>
                        <g class="c-part" :class="{ on: face.eyes === 'arc' || face.eyes === 'wink' }">
                          <path class="k" d="M109 113 Q116 102 123 113" fill="none" />
                        </g>
                        <g class="c-part" :class="{ on: face.eyes === 'glare' }">
                          <path d="M77 104 L91 109 Q92 118 84 118 Q76.5 118 77 104Z M123 104 L109 109 Q108 118 116 118 Q123.5 118 123 104Z" fill="#1a1a1a" />
                        </g>
                        <g class="c-part" :class="{ on: face.eyes === 'x' }">
                          <path class="k" d="M78 103 L90 110 L78 117 M122 103 L110 110 L122 117" fill="none" />
                        </g>
                        <g class="c-part" :class="{ on: face.eyes === 'shut' }">
                          <path class="k" d="M77 109 Q84 116 91 109 M109 109 Q116 116 123 109" fill="none" />
                        </g>
                      </g>
                    </g>
                    <g class="c-part" :class="{ on: face.mouth === 'w' }">
                      <path class="k c-mouth" d="M92 124 Q96 130 100 124 Q104 130 108 124" fill="none" />
                    </g>
                    <g class="c-part" :class="{ on: face.mouth === 'open' }">
                      <path class="k c-mouth-open" d="M91 121 Q100 124 109 121 Q108 135 100 135 Q92 135 91 121Z" fill="#FFFFFF" />
                    </g>
                    <g class="c-part" :class="{ on: face.mouth === 'o' }">
                      <ellipse class="k" cx="100" cy="127" rx="4.5" ry="6" fill="#FFFFFF" />
                    </g>
                    <g class="c-part" :class="{ on: face.mouth === 'tiny' }">
                      <ellipse class="k" cx="100" cy="126" rx="2.6" ry="3" fill="#FFFFFF" />
                    </g>
                    <g class="c-part" :class="{ on: face.mouth === 'wavy' }">
                      <path class="k" d="M91 128 Q94.5 123.5 98 128 Q101.5 132.5 105 128 Q107 125.5 109 127" fill="none" />
                    </g>
                    <g class="c-part" :class="{ on: face.mouth === 'frown' }">
                      <path class="k" d="M92 130 Q96 124.5 100 128.5 Q104 124.5 108 130" fill="none" />
                    </g>
                    <g class="c-part" :class="{ on: face.mouth === 'hm' }">
                      <path class="k" d="M95 127 Q100 123.5 106 126" fill="none" />
                    </g>
                    <g class="c-blush">
                      <circle cx="74" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
                      <circle cx="126" cy="126" r="6" fill="#c8c8c8" opacity=".85" />
                    </g>
                    <g class="c-part c-paw" :class="{ on: face.paw }">
                      <rect class="k" x="60" y="138" width="32" height="13" rx="6.5" transform="rotate(-30 76 144.5)" fill="#FFFFFF" />
                    </g>
                  </g>
                </g>
              </g>
            </g>
          </g>
        </g>
        <!-- Peek: a short pole in front of the body with both paws gripping it. -->
        <g class="fx fx-pole" :class="{ on: face.fx === 'alert' }">
          <path class="k" d="M143 86 L143 178" fill="none" />
          <circle class="k" cx="143" cy="110" r="7.5" fill="#FFFFFF" />
          <circle class="k" cx="143" cy="140" r="7.5" fill="#FFFFFF" />
        </g>
        <g class="fx" :class="{ on: face.fx === 'spark' }">
          <g class="fx-in"><path class="fx-k" d="M148 70 L157 61 M153 80 L165 77 M142 62 L145 51" /></g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'hash' }">
          <g class="fx-in">
            <path class="fx-k" d="M34 82 L50 80 M35 90 L51 88 M39 75 L42 96 M45 74 L48 95" />
            <path class="fx-k" d="M150 74 L166 72 M151 82 L167 80 M155 67 L158 88 M161 66 L164 87" />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'bang' }">
          <g class="fx-in">
            <path class="fx-k" d="M152 50 L150 66 M164 54 L159 69" />
            <circle cx="149.5" cy="73" r="2.6" fill="#7B61FF" />
            <circle cx="157.5" cy="75.5" r="2.6" fill="#7B61FF" />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'zz' }">
          <g class="fx-in fx-drift">
            <path class="fx-k" d="M146 64 H155 L146 73 H155" />
            <path class="fx-k" d="M160 46 H171 L160 57 H171" />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'vein' }">
          <g class="fx-in">
            <path
              class="fx-k"
              d="M115 83 Q121 83 121 77 M127 77 Q127 83 133 83 M115 89 Q121 89 121 95 M127 95 Q127 89 133 89"
            />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'sweat' }">
          <g class="fx-in fx-drop">
            <path d="M152 66 Q161 79 157 85 Q152 89 147 85 Q143 79 152 66Z" fill="#7B61FF" />
            <path d="M164 84 Q169 91 166 94 Q163 96 160 94 Q158 91 164 84Z" fill="#7B61FF" />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'cloud' }">
          <g class="fx-in fx-bob">
            <path
              d="M150 52 Q141 52 143 44 Q143 36 152 38 Q155 30 164 32 Q173 28 177 36 Q185 38 183 46 Q183 54 173 53 Q163 58 157 53 Q153 55 150 52Z"
              fill="#FFFFFF"
              stroke="#7B61FF"
              stroke-width="2.6"
              stroke-linejoin="round"
            />
            <circle cx="143" cy="62" r="3.4" fill="#FFFFFF" stroke="#7B61FF" stroke-width="2.2" />
            <circle cx="137" cy="70" r="2" fill="#FFFFFF" stroke="#7B61FF" stroke-width="2" />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'waves' }">
          <g class="fx-in"><path class="fx-k" d="M162 82 Q170 90 166 100 M170 74 Q182 88 175 104" /></g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'hop' }">
          <g class="fx-in">
            <path class="fx-k" d="M38 66 L46 74 M32 80 L43 82 M162 66 L154 74 M168 80 L157 82" />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'speed' }">
          <g class="fx-in fx-rush">
            <path class="fx-k" d="M152 92 L174 92 M156 103 L180 103 M152 114 L170 114" />
            <path
              d="M150 172 Q146 164 154 162 Q156 155 164 158 Q171 154 175 161 Q182 163 179 170 Q176 175 168 173 Q160 177 150 172Z"
              fill="#FFFFFF"
              stroke="#9a9a9a"
              stroke-width="2.4"
              stroke-linejoin="round"
            />
          </g>
        </g>
        <g class="fx" :class="{ on: face.fx === 'alert' }">
          <g class="fx-in"><path class="fx-k" d="M38 100 L49 105 M35 114 L48 114 M38 128 L49 123" /></g>
        </g>
        <g v-if="sleeping" class="c-zz-group">
          <text class="c-zz" x="146" y="40">z</text>
          <text class="c-zz c-zz--late" x="160" y="28">z</text>
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

.home-yume :deep(.fx-k) {
  fill: none;
  stroke: rgb(var(--c-accent, 123 97 255));
  stroke-width: 3.4;
  stroke-linecap: round;
  stroke-linejoin: round;
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

/* Mood pose eases with a soft overshoot; the loop layer only plays whole cycles. */
.home-yume :deep(.c-pose) {
  transform-box: view-box;
  transform-origin: 100px 160px;
  transition: transform 560ms cubic-bezier(.34, 1.36, .5, 1);
}

.home-yume :deep(.c-loop) {
  transform-box: view-box;
  transform-origin: 100px 160px;
}

.home-yume :deep(.c-breathe) {
  transform-box: view-box;
  transform-origin: 100px 156px;
  animation: yume-c-breathe 6.8s ease-in-out infinite;
}

.home-yume :deep(.c-ear-l),
.home-yume :deep(.c-ear-r) {
  transform-box: view-box;
  transition: transform 420ms cubic-bezier(.34, 1.4, .5, 1);
}
.home-yume :deep(.c-ear-l) { transform-origin: 70px 80px; }
.home-yume :deep(.c-ear-r) { transform-origin: 130px 80px; }

.home-yume :deep(.c-shadow) {
  transform-box: fill-box;
  transform-origin: center;
  animation: yume-c-shadow 2.6s ease-in-out infinite;
}

/* Face swaps read as a blink: the old part squeezes shut, then the new one springs open. */
.home-yume :deep(.c-part) {
  opacity: 0;
  transform: scale(1, .1);
  transform-box: fill-box;
  transform-origin: center;
  transition: opacity 90ms ease-in 30ms, transform 120ms ease-in;
}
.home-yume :deep(.c-part.on) {
  opacity: 1;
  transform: none;
  transition: opacity 80ms ease-out 110ms, transform 300ms cubic-bezier(.34, 1.56, .5, 1) 110ms;
}
.home-yume :deep(.c-paw) { transform: translateY(14px) rotate(-12deg); transform-origin: 50% 100%; }

.home-yume :deep(.c-dot-shape) {
  transform-box: fill-box;
  transform-origin: center;
  transition: transform 320ms cubic-bezier(.34, 1.5, .5, 1);
}
.home-yume :deep(.c-shine) {
  opacity: 0;
  transition: opacity 200ms ease;
}
.home-yume :deep(.c-shine.on) { opacity: 1; }

.home-yume :deep(.c-eye) {
  transform-box: fill-box;
  transform-origin: center;
  animation: yume-c-blink 5.8s infinite;
}
.home-yume :deep(.c-dot + .c-dot .c-eye) { animation-delay: 80ms; }

.home-yume :deep(.c-gaze) {
  transition: transform 360ms cubic-bezier(.34, 1.3, .5, 1);
}

.home-yume :deep(.c-tail) {
  transform-box: view-box;
  transform-origin: 0 0;
  animation: yume-c-wig 3.4s ease-in-out infinite;
}
.home-yume :deep(.c-tail-pose) {
  transform-box: view-box;
  transform-origin: 0 0;
  transition: transform 480ms cubic-bezier(.34, 1.3, .5, 1), opacity 220ms ease;
}

.home-yume :deep(.c-brow-bold) { stroke-width: 5.5; }

.home-yume :deep(.c-arm) {
  opacity: 0;
  transform-box: view-box;
  transform-origin: 0 0;
  transition: opacity 180ms ease, transform 520ms cubic-bezier(.34, 1.4, .5, 1);
}
.home-yume :deep(.c-arm-swing) {
  transform-box: view-box;
  transform-origin: 0 0;
}
.home-yume :deep(.c-arm.arm-wave.c-arm--r),
.home-yume :deep(.c-arm.arm-up),
.home-yume :deep(.c-arm.arm-run) { opacity: 1; }
.home-yume :deep(.c-arm.arm-wave.c-arm--r) { transform: rotate(-125deg); }
.home-yume :deep(.c-arm.arm-up.c-arm--l) { transform: rotate(130deg); }
.home-yume :deep(.c-arm.arm-up.c-arm--r) { transform: rotate(-130deg); }
.home-yume :deep(.c-arm.arm-run.c-arm--l) { transform: rotate(70deg); }
.home-yume :deep(.c-arm.arm-run.c-arm--r) { transform: rotate(-45deg); }
.home-yume :deep(.c-arm.arm-wave.c-arm--r .c-arm-swing) { animation: yume-m-wave 600ms ease-in-out infinite; }
.home-yume :deep(.c-arm.arm-up .c-arm-swing) { animation: yume-m-flap 900ms ease-in-out infinite; }
.home-yume :deep(.c-arm.arm-run.c-arm--l .c-arm-swing) { animation: yume-m-swing 360ms ease-in-out infinite; }
.home-yume :deep(.c-arm.arm-run.c-arm--r .c-arm-swing) { animation: yume-m-swing-back 360ms ease-in-out infinite; }

/* Mood effects pop in beside the head and keep a light pulse while shown. */
.home-yume :deep(.fx) {
  opacity: 0;
  transform: scale(.4);
  transform-box: fill-box;
  transform-origin: center;
  transition: opacity 140ms ease-in, transform 160ms ease-in;
}
.home-yume :deep(.fx.on) {
  opacity: 1;
  transform: none;
  transition: opacity 180ms ease-out 140ms, transform 420ms cubic-bezier(.34, 1.6, .5, 1) 140ms;
}
.home-yume :deep(.fx-pole) { transform: translateX(16px); }
.home-yume :deep(.fx-in) {
  transform-box: fill-box;
  transform-origin: center;
}
.home-yume :deep(.fx.on .fx-in) { animation: yume-fx-pulse 1.2s ease-in-out infinite; }
.home-yume :deep(.fx.on .fx-drift) { animation: yume-fx-drift 1.4s ease-in-out infinite; }
.home-yume :deep(.fx.on .fx-drop) { animation: yume-fx-drop 1.2s ease-in-out infinite; }
.home-yume :deep(.fx.on .fx-bob) { animation: yume-fx-bob 1.4s ease-in-out infinite; }
.home-yume :deep(.fx.on .fx-rush) { animation: yume-fx-rush 360ms linear infinite; }

.home-yume :deep(.c-eyes),
.home-yume :deep(.c-face) { transition: transform 140ms ease-out; }
.home-yume :deep(.c-blush) { opacity: .85; transition: opacity 180ms ease-out; }
.home-yume :deep(.c-blush circle) { transition: opacity 220ms ease; }

/* Mood poses. Each loop starts and ends at rest so entering or leaving a mood never jumps. */
.home-yume.mood-happy :deep(.c-pose) { transform: translateY(-3px) scale(1.02); }
.home-yume.mood-happy :deep(.c-loop) { animation: yume-m-bounce 800ms ease-in-out infinite; }
.home-yume.mood-happy :deep(.c-tail-pose) { transform: rotate(-10deg); }

.home-yume.mood-laugh :deep(.c-pose) { transform: translateY(-2px) scale(1.03, .98); }
.home-yume.mood-laugh :deep(.c-loop) { animation: yume-m-laugh 500ms ease-in-out infinite; }

.home-yume.mood-surprised :deep(.c-pose) { transform: translateY(-7px) scale(.96, 1.06); }
.home-yume.mood-surprised :deep(.c-dot-shape) { transform: scale(.85); }
.home-yume.mood-surprised :deep(.c-ear-l) { transform: rotate(7deg); }
.home-yume.mood-surprised :deep(.c-ear-r) { transform: rotate(-7deg); }
.home-yume.mood-surprised :deep(.c-tail-pose) { transform: rotate(-22deg); }

.home-yume.mood-sleepy :deep(.c-pose) { transform: translateY(5px) rotate(4deg) scale(1.02, .97); }
.home-yume.mood-sleepy :deep(.c-ear-l) { transform: rotate(-9deg); }
.home-yume.mood-sleepy :deep(.c-ear-r) { transform: rotate(9deg); }
.home-yume.mood-sleepy :deep(.c-tail-pose) { transform: rotate(28deg); }

.home-yume.mood-angry :deep(.c-pose) { transform: scale(1.04, .96); }
.home-yume.mood-angry :deep(.c-loop) { animation: yume-m-tremble 180ms linear infinite; }
.home-yume.mood-angry :deep(.c-ear-l) { transform: rotate(-10deg); }
.home-yume.mood-angry :deep(.c-ear-r) { transform: rotate(10deg); }

.home-yume.mood-worried :deep(.c-pose) { transform: translateY(3px) rotate(-2deg) scale(.97); }
.home-yume.mood-worried :deep(.c-loop) { animation: yume-m-shiver 600ms ease-in-out infinite; }
.home-yume.mood-worried :deep(.c-dot-shape) { transform: scale(1.32); }
.home-yume.mood-worried :deep(.c-ear-l) { transform: rotate(-8deg); }
.home-yume.mood-worried :deep(.c-ear-r) { transform: rotate(8deg); }

.home-yume.mood-think :deep(.c-pose) { transform: rotate(-5deg); }
.home-yume.mood-think :deep(.c-gaze) { transform: translate(3px, -3px); }
.home-yume.mood-think :deep(.c-ear-r) { transform: rotate(-6deg); }

.home-yume.mood-wave :deep(.c-pose) { transform: rotate(3deg); }
.home-yume.mood-wave :deep(.c-tail-pose) { transform: rotate(-12deg); }
.home-yume.mood-wave :deep(.c-tail) { animation-duration: 900ms; }

.home-yume.mood-jump :deep(.c-loop) { animation: yume-m-hop 900ms cubic-bezier(.3, .7, .4, 1) infinite; }
.home-yume.mood-jump :deep(.c-tail-pose) { transform: rotate(-16deg); }

.home-yume.mood-run :deep(.c-pose) { transform: translateX(-4px) rotate(-8deg); }
.home-yume.mood-run :deep(.c-loop) { animation: yume-m-run 360ms ease-in-out infinite; }
.home-yume.mood-run :deep(.c-tail-pose) { transform: rotate(40deg); }
.home-yume.mood-run :deep(.c-ear-l) { transform: rotate(-8deg); }
.home-yume.mood-run :deep(.c-ear-r) { transform: rotate(8deg); }

.home-yume.mood-peek :deep(.c-pose) { transform: translateX(5px) rotate(-14deg); }
.home-yume.mood-peek :deep(.c-loop) { animation: yume-m-peek 1300ms ease-in-out infinite; }
.home-yume.mood-peek :deep(.c-gaze) { transform: translate(1px, -1px); }
.home-yume.mood-peek :deep(.c-face) { transform: translateX(7px); }
.home-yume.mood-peek :deep(.c-blush circle:last-child) { opacity: 0; }
.home-yume.mood-peek :deep(.c-tail-pose) { opacity: 0; transform: rotate(-40deg); }

@keyframes yume-c-breathe {
  0%, 100% { transform: rotate(-1deg) scale(1); }
  30% { transform: rotate(1.2deg) scale(1.012, 1.018); }
  65% { transform: rotate(-.5deg) scale(1.005, .995); }
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

@keyframes yume-m-bounce {
  0%, 100% { transform: none; }
  50% { transform: translateY(-4px) scale(.99, 1.02); }
}

@keyframes yume-m-laugh {
  0%, 100% { transform: none; }
  25% { transform: translateY(-2px) rotate(-3deg); }
  75% { transform: translateY(-2px) rotate(3deg); }
}

@keyframes yume-m-tremble {
  0%, 100% { transform: none; }
  25% { transform: translateX(-1.2px); }
  75% { transform: translateX(1.2px); }
}

@keyframes yume-m-shiver {
  0%, 100% { transform: none; }
  50% { transform: rotate(-1.6deg) translateX(-1px); }
}

@keyframes yume-m-hop {
  0%, 100% { transform: none; }
  16% { transform: scale(1.07, .92); }
  50% { transform: translateY(-18px) scale(.96, 1.05); }
  82% { transform: scale(1.04, .96); }
}

@keyframes yume-m-run {
  0%, 100% { transform: none; }
  50% { transform: translateY(-5px) rotate(-1.5deg); }
}

@keyframes yume-m-peek {
  0%, 100% { transform: none; }
  50% { transform: translateX(-3px) rotate(-2deg); }
}

@keyframes yume-m-wave {
  0%, 100% { transform: rotate(0); }
  50% { transform: rotate(-24deg); }
}

@keyframes yume-m-flap {
  0%, 100% { transform: rotate(0); }
  50% { transform: rotate(14deg); }
}

@keyframes yume-m-swing {
  0%, 100% { transform: rotate(0); }
  25% { transform: rotate(28deg); }
  75% { transform: rotate(-28deg); }
}

@keyframes yume-m-swing-back {
  0%, 100% { transform: rotate(0); }
  25% { transform: rotate(-28deg); }
  75% { transform: rotate(28deg); }
}

@keyframes yume-fx-pulse {
  0%, 100% { transform: none; }
  50% { transform: scale(1.12); }
}

@keyframes yume-fx-drift {
  0%, 100% { transform: none; }
  50% { transform: translate(3px, -5px); }
}

@keyframes yume-fx-drop {
  0%, 100% { transform: none; }
  50% { transform: translateY(4px); }
}

@keyframes yume-fx-bob {
  0%, 100% { transform: none; }
  50% { transform: translateY(-3px); }
}

@keyframes yume-fx-rush {
  0% { transform: translateX(0); opacity: 1; }
  100% { transform: translateX(6px); opacity: .6; }
}

/* Dozing: the sleepy mood plus drifting z. */
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

.home-yume--static :deep(:is(.c-float, .c-squash, .c-act, .c-pose, .c-loop, .c-breathe, .c-ear-l, .c-ear-r, .c-eye, .c-eyes, .c-gaze, .c-face, .c-blush, .c-tail, .c-tail-pose, .c-shadow)) {
  animation: none !important;
  transform: none !important;
  transition: none !important;
}
.home-yume--static :deep(:is(.c-part, .c-dot-shape, .c-shine, .c-arm, .c-arm-swing, .fx, .fx-in)) {
  animation: none !important;
  transition: none !important;
}
@media (prefers-reduced-motion: reduce) {
  .home-yume :deep(:is(.c-float, .c-squash, .c-act, .c-pose, .c-loop, .c-breathe, .c-ear-l, .c-ear-r, .c-eye, .c-eyes, .c-gaze, .c-face, .c-blush, .c-tail, .c-tail-pose, .c-shadow, .c-zz)),
  .c-heart {
    animation: none !important;
    transform: none !important;
    transition: none !important;
  }
  .home-yume :deep(:is(.c-part, .c-dot-shape, .c-shine, .c-arm, .c-arm-swing, .fx, .fx-in)) {
    animation: none !important;
    transition: none !important;
  }
}
</style>

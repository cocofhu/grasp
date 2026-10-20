<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    /** Rendered edge length in px. Sidebar uses 28, login uses 48. */
    size?: number
  }>(),
  { size: 28 },
)

const jumping = ref(false)
const reduced = ref(false)

let motionMql: MediaQueryList | null = null
let jumpTimer: ReturnType<typeof setTimeout> | null = null

function stopJump() {
  if (jumpTimer != null) clearTimeout(jumpTimer)
  jumpTimer = null
  jumping.value = false
}

function syncReduced() {
  reduced.value = !!motionMql?.matches
  if (reduced.value) stopJump()
}

function onPointerDown(e: PointerEvent) {
  if (reduced.value) return
  if (e.pointerType === 'mouse' && e.button !== 0) return
  jumping.value = true
  if (jumpTimer != null) clearTimeout(jumpTimer)
  jumpTimer = setTimeout(() => {
    jumping.value = false
    jumpTimer = null
  }, 360)
}

onMounted(() => {
  if (typeof window === 'undefined' || !window.matchMedia) return
  motionMql = window.matchMedia('(prefers-reduced-motion: reduce)')
  syncReduced()
  motionMql.addEventListener('change', syncReduced)
})

onBeforeUnmount(() => {
  motionMql?.removeEventListener('change', syncReduced)
  motionMql = null
  stopJump()
})
</script>

<template>
  <div
    class="brand-character"
    :class="{ jump: jumping, 'brand-character--static': reduced }"
    data-testid="brand-character-mark"
    :data-size="props.size"
    :style="{ width: `${props.size}px`, height: `${props.size}px` }"
    aria-hidden="true"
    @pointerdown="onPointerDown"
  >
    <svg class="brand-character__svg" viewBox="48 52 104 118" focusable="false">
      <g class="brand-character__response">
        <g class="brand-character__jump">
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
.brand-character__motion,
.brand-character__breath {
  transform-box: fill-box;
  transform-origin: 50% 100%;
}

.brand-character__response { transition: transform 260ms cubic-bezier(.22, 1, .36, 1); }
.brand-character__motion { animation: brand-character-sway 5.6s ease-in-out infinite; }
.brand-character__breath { animation: brand-character-breathe 3.8s ease-in-out infinite; }
.brand-character__face { transition: transform 220ms ease; }
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

.brand-character.jump .brand-character__jump {
  animation: brand-character-jump 360ms cubic-bezier(.22, 1, .36, 1);
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

@keyframes brand-character-jump {
  0% { transform: translateY(0) scale(1); }
  20% { transform: translateY(0) scale(1.055, .93); }
  55% { transform: translateY(-8px) scale(.98, 1.055); }
  82% { transform: translateY(0) scale(1.025, .98); }
  100% { transform: translateY(0) scale(1); }
}

.brand-character--static :is(.brand-character__response, .brand-character__jump, .brand-character__motion, .brand-character__breath, .brand-character__face, .brand-character__eye) {
  animation: none !important;
  transition: none !important;
  transform: none !important;
}

@media (prefers-reduced-motion: reduce) {
  .brand-character__response,
  .brand-character__jump,
  .brand-character__motion,
  .brand-character__breath,
  .brand-character__face,
  .brand-character__eye {
    animation: none !important;
    transition: none !important;
    transform: none !important;
  }
}
</style>

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

function syncReduced() {
  reduced.value = !!motionMql?.matches
  if (reduced.value) jumping.value = false
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
  if (jumpTimer != null) clearTimeout(jumpTimer)
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
      <g class="brand-character__motion">
        <path
          d="M58 160 L58 92 L66 58 L84 76 Q100 70 116 76 L134 58 L142 92 L142 160 Q131 150 121 162 Q110 150 100 162 Q90 150 79 162 Q69 150 58 160Z"
          fill="#fff"
          stroke="#1a1a1a"
          stroke-width="6"
          stroke-linejoin="round"
        />
        <g class="brand-character__eyes">
          <ellipse class="brand-character__eye" cx="84" cy="110" rx="6" ry="8" fill="#1a1a1a" />
          <ellipse class="brand-character__eye" cx="116" cy="110" rx="6" ry="8" fill="#1a1a1a" />
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

.brand-character__motion {
  transform-box: fill-box;
  transform-origin: 50% 100%;
  animation: brand-character-float 2.8s ease-in-out infinite;
}

.brand-character__eye {
  transform-box: fill-box;
  transform-origin: center;
  animation: brand-character-blink 4.2s infinite;
}

.brand-character:hover .brand-character__motion {
  animation: none;
  transform: translateY(-1px) scale(1.06);
}

.brand-character.jump .brand-character__motion {
  animation: brand-character-jump 0.35s cubic-bezier(0.2, 1.5, 0.4, 1);
}

@keyframes brand-character-float {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-6%);
  }
}

@keyframes brand-character-blink {
  0%,
  92%,
  100% {
    transform: scaleY(1);
  }
  96% {
    transform: scaleY(0.1);
  }
}

@keyframes brand-character-jump {
  0% {
    transform: translateY(0) scale(1.04, 0.9);
  }
  55% {
    transform: translateY(-4px) scale(0.98, 1.08);
  }
  100% {
    transform: translateY(0) scale(1);
  }
}

.brand-character--static .brand-character__motion,
.brand-character--static .brand-character__eye,
.brand-character--static:hover .brand-character__motion,
.brand-character--static.jump .brand-character__motion {
  animation: none !important;
  transform: none !important;
}

@media (prefers-reduced-motion: reduce) {
  .brand-character__motion,
  .brand-character__eye,
  .brand-character:hover .brand-character__motion,
  .brand-character.jump .brand-character__motion {
    animation: none !important;
    transform: none !important;
  }
}
</style>

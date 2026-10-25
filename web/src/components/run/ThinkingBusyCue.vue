<script setup lang="ts">
/**
 * Local thinking-row cue: a small accent lamp plus a same-hue gray sheen.
 * Scoped here so sidebar / canvas breath dots keep the shared busy keyframes.
 */
defineProps<{
  label: string
}>()
</script>

<template>
  <span class="thinking-busy">
    <span class="thinking-lamp" data-testid="thinking-lamp" aria-hidden="true">
      <span class="thinking-lamp-halo" data-testid="thinking-lamp-halo"></span>
      <span class="thinking-lamp-core" data-testid="thinking-lamp-core"></span>
    </span>
    <span class="thinking-sheen" data-testid="thinking-sheen">{{ label }}</span>
  </span>
</template>

<style scoped>
.thinking-busy {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  font-size: 11px;
  font-weight: 400;
  line-height: 1.65;
}

/* 12px inline slot. Halo is absolute, so the row stays one line. */
.thinking-lamp {
  position: relative;
  width: 12px;
  height: 12px;
  flex: none;
  color: rgb(var(--c-accent-2));
  overflow: visible;
}

.thinking-lamp-core,
.thinking-lamp-halo {
  position: absolute;
  left: 50%;
  top: 50%;
  width: 4px;
  height: 4px;
  margin: -2px 0 0 -2px;
  border-radius: 50%;
  box-sizing: border-box;
}

.thinking-lamp-core {
  background: currentColor;
  animation: thinking-lamp-core 0.8s ease-in-out infinite;
}

.thinking-lamp-halo {
  border: 1px solid currentColor;
  /* 4px core × 2.2 = 8.8px, within the ~10px halo. */
  animation: thinking-lamp-halo 0.8s ease-out infinite;
}

/* Solid chat-meta gray. Clip failure never clears the glyphs. */
.thinking-sheen {
  min-width: 0;
  color: rgb(var(--c-txt2) / .82);
  -webkit-text-fill-color: rgb(var(--c-txt2) / .82);
  background-image: none;
}

@supports ((-webkit-background-clip: text) or (background-clip: text)) {
  .thinking-sheen {
    background-image: linear-gradient(
      100deg,
      rgb(var(--c-txt2) / .82) 0%,
      rgb(var(--c-txt2) / .82) 42%,
      rgb(var(--c-txt2) / 1) 50%,
      rgb(var(--c-txt2) / .82) 58%,
      rgb(var(--c-txt2) / .82) 100%
    );
    background-image: linear-gradient(
      100deg,
      rgb(var(--c-txt2) / .82) 0%,
      rgb(var(--c-txt2) / .82) 42%,
      light-dark(
        rgb(244 244 245 / .98),
        color-mix(in srgb, rgb(var(--c-txt2)) 40%, white)
      ) 50%,
      rgb(var(--c-txt2) / .82) 58%,
      rgb(var(--c-txt2) / .82) 100%
    );
    background-size: 220% 100%;
    background-position: 120% 50%;
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: thinking-sheen 1s linear infinite;
  }
}

@keyframes thinking-lamp-core {
  0%, 100% {
    transform: scale(1);
    opacity: 0.6;
    box-shadow: 0 0 0 0 rgb(var(--c-accent-2) / 0);
  }
  45% {
    transform: scale(1.15);
    opacity: 1;
    box-shadow: 0 0 2px 0 rgb(var(--c-accent-2) / .35);
  }
}

@keyframes thinking-lamp-halo {
  0% { transform: scale(1); opacity: 0.55; }
  100% { transform: scale(2.2); opacity: 0; }
}

@keyframes thinking-sheen {
  0% { background-position: 120% 50%; }
  100% { background-position: -20% 50%; }
}

@media (prefers-reduced-motion: reduce) {
  .thinking-lamp-core,
  .thinking-lamp-halo,
  .thinking-sheen {
    animation: none;
  }
  .thinking-lamp-halo {
    opacity: 0;
  }
  .thinking-lamp-core {
    opacity: 1;
    transform: none;
    box-shadow: none;
  }
  .thinking-sheen {
    background-image: none;
    -webkit-text-fill-color: rgb(var(--c-txt2) / .82);
    color: rgb(var(--c-txt2) / .82);
  }
}
</style>

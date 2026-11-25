<script setup lang="ts">
import { computed } from 'vue'
import vscodeLogo from '@/assets/ide-logos/vscode.svg'
import cursorLogo from '@/assets/ide-logos/cursor.svg'
import codebuddyLogo from '@/assets/ide-logos/codebuddy.svg'
import traeLogo from '@/assets/ide-logos/trae.svg'
import jetbrainsLogo from '@/assets/ide-logos/jetbrains.svg'
import type { RemoteIDE } from '@/lib/sandbox/sshConnect'

const props = withDefaults(defineProps<{ ide: RemoteIDE; size?: number }>(), { size: 16 })

const LOGOS: Record<RemoteIDE, { src: string; mono: boolean }> = {
  vscode: { src: vscodeLogo, mono: false },
  cursor: { src: cursorLogo, mono: true },
  codebuddy: { src: codebuddyLogo, mono: false },
  trae: { src: traeLogo, mono: false },
  jetbrains: { src: jetbrainsLogo, mono: true },
}

const logo = computed(() => LOGOS[props.ide])
/** Single-color marks follow the text color so they stay visible in both themes. */
const maskStyle = computed(() => ({
  width: `${props.size}px`,
  height: `${props.size}px`,
  maskImage: `url("${logo.value.src}")`,
  WebkitMaskImage: `url("${logo.value.src}")`,
}))
</script>

<template>
  <span v-if="logo.mono" class="ide-logo is-mono" :style="maskStyle" :data-ide-logo="ide" aria-hidden="true" />
  <img v-else class="ide-logo" :src="logo.src" :width="size" :height="size" alt="" :data-ide-logo="ide" aria-hidden="true" draggable="false">
</template>

<style scoped>
.ide-logo { display: inline-block; flex: none; object-fit: contain; }
.ide-logo.is-mono {
  background-color: currentColor;
  mask-repeat: no-repeat;
  mask-position: center;
  mask-size: contain;
  -webkit-mask-repeat: no-repeat;
  -webkit-mask-position: center;
  -webkit-mask-size: contain;
}
</style>

<script setup lang="ts">
import { computed } from 'vue'
import bytedanceLogo from '@/assets/provider-logos/bytedance.svg'
import claudeLogo from '@/assets/provider-logos/claude.svg'
import codebuddyLogo from '@/assets/provider-logos/codebuddy.svg'
import codexLogo from '@/assets/provider-logos/codex.svg'
import cursorLogo from '@/assets/provider-logos/cursor.svg'
import deepseekLogo from '@/assets/provider-logos/deepseek.svg'
import googleLogo from '@/assets/provider-logos/google.svg'
import moonshotLogo from '@/assets/provider-logos/moonshot.svg'
import openaiLogo from '@/assets/provider-logos/openai.svg'
import traeLogo from '@/assets/provider-logos/trae.svg'
import xaiLogo from '@/assets/provider-logos/xai.svg'
import Icon from '@/components/ui/Icon.vue'
import { monogramColor, vendorLabel, vendorLogoProvider } from '@/lib/agent/cliModelView'
import { theme } from '@/lib/shared/theme'

const props = withDefaults(
  defineProps<{
    /** Model vendor (`anthropic`), or a CLI backend id when `backend` is set. */
    vendor: string
    backend?: boolean
    /** Outer box in px; the mark is drawn at about 60%. */
    size?: number
    /** Small sparkles badge in the corner, for the "auto" choice. */
    auto?: boolean
    /** Generic mark for a model outside the shortlist. */
    unknown?: boolean
  }>(),
  { backend: false, size: 32, auto: false, unknown: false },
)

const ASSETS: Record<string, string> = {
  claude: claudeLogo,
  claude_code: claudeLogo,
  openai: openaiLogo,
  google: googleLogo,
  xai: xaiLogo,
  deepseek: deepseekLogo,
  deepseek_harness: deepseekLogo,
  moonshot: moonshotLogo,
  bytedance: bytedanceLogo,
  cursor: cursorLogo,
  codex: codexLogo,
  codebuddy: codebuddyLogo,
  trae: traeLogo,
}

/** These marks ship with wide built-in padding and read tiny at icon sizes. */
const PADDED = new Set(['openai', 'codex'])

const assetKey = computed(() => (props.backend ? props.vendor : vendorLogoProvider(props.vendor)))
const asset = computed(() => (props.unknown ? '' : ASSETS[assetKey.value] || ''))
const markPx = computed(() => Math.round(props.size * (PADDED.has(assetKey.value) ? 0.82 : 0.58)))
const label = computed(() => (props.backend ? props.vendor : vendorLabel(props.vendor)))
const initial = computed(() => label.value.charAt(0).toUpperCase() || '?')
const tint = computed(() => monogramColor(props.vendor))
const radius = computed(() => (props.size >= 28 ? 9 : props.size >= 20 ? 6 : 4))
</script>

<template>
  <span
    class="vendor-mark relative inline-flex shrink-0 items-center justify-center border border-line bg-surface"
    :style="{ width: `${size}px`, height: `${size}px`, borderRadius: `${radius}px` }"
    role="img"
    :aria-label="label"
    :data-vendor-mark="unknown ? 'unknown' : asset ? vendor : 'monogram'"
  >
    <Icon v-if="unknown" name="help" :interactive="false" :size="Math.round(size * 0.5)" class="text-warn" />
    <img
      v-else-if="asset"
      :src="asset"
      alt=""
      aria-hidden="true"
      class="object-contain"
      :class="theme === 'dark' ? 'invert' : ''"
      :style="{ width: `${markPx}px`, height: `${markPx}px` }"
    />
    <span
      v-else
      class="flex h-full w-full items-center justify-center font-semibold"
      :style="{ borderRadius: `${radius - 1}px`, background: `${tint}22`, color: tint, fontSize: `${Math.round(size * 0.44)}px` }"
    >
      {{ initial }}
    </span>
    <span
      v-if="auto"
      class="absolute -bottom-1 -right-1 flex items-center justify-center rounded-full border border-surface bg-accent text-white shadow-sm"
      :style="{ width: `${Math.max(12, Math.round(size * 0.45))}px`, height: `${Math.max(12, Math.round(size * 0.45))}px` }"
      data-vendor-mark-auto
    >
      <Icon name="sparkles" :interactive="false" :size="Math.max(8, Math.round(size * 0.28))" />
    </span>
  </span>
</template>

<style scoped>
.vendor-mark {
  transition: border-color 180ms ease, background-color 180ms ease;
}
/*
 * Keep the scoped attribute on .vendor-mark. Wrapping only the parent in
 * :global() makes Vue drop the class and emit a bare button:hover rule, which
 * paints every button (including a disabled Next and ghost Cancel) with the
 * elevated gray.
 */
button:hover .vendor-mark,
button:focus-visible .vendor-mark,
a:hover .vendor-mark,
a:focus-visible .vendor-mark,
[role="option"]:hover .vendor-mark {
  border-color: rgb(var(--c-accent) / .45);
  background-color: rgb(var(--c-elevated));
}
@media (prefers-reduced-motion: reduce) {
  .vendor-mark { transition: none; }
}
</style>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/ui/Icon.vue'
import claudeLogo from '@/assets/provider-logos/claude.svg'
import codebuddyLogo from '@/assets/provider-logos/codebuddy.svg'
import codexLogo from '@/assets/provider-logos/codex.svg'
import cursorLogo from '@/assets/provider-logos/cursor.svg'
import traeLogo from '@/assets/provider-logos/trae.svg'
import opencodeLogo from '@/assets/provider-logos/opencode.svg'
import githubLogo from '@/assets/provider-logos/github.svg'
import gitlabLogo from '@/assets/provider-logos/gitlab.svg'
import openaiLogo from '@/assets/provider-logos/openai.svg'
import deepseekLogo from '@/assets/provider-logos/deepseek.svg'
import openrouterLogo from '@/assets/provider-logos/openrouter.svg'
import xaiLogo from '@/assets/provider-logos/xai.svg'

const props = withDefaults(
  defineProps<{
    provider?: string
    name?: string
    type?: string
    envKey?: string
    configured?: boolean
    selected?: boolean
  }>(),
  { provider: '', name: '', type: '', envKey: '', configured: false, selected: false },
)

type LogoKey =
  | 'claude'
  | 'codebuddy'
  | 'codex'
  | 'cursor'
  | 'trae'
  | 'opencode'
  | 'github'
  | 'gitlab'
  | 'openai'
  | 'deepseek'
  | 'openrouter'
  | 'xai'
  | 'neutral'

const logoLabels: Record<LogoKey, string> = {
  claude: 'Claude Code',
  codebuddy: 'CodeBuddy',
  codex: 'Codex',
  cursor: 'Cursor',
  trae: 'Trae',
  opencode: 'OpenCode',
  github: 'GitHub',
  gitlab: 'GitLab',
  openai: 'OpenAI',
  deepseek: 'DeepSeek',
  openrouter: 'OpenRouter',
  xai: 'xAI',
  neutral: 'Credential provider',
}

const logoAssets: Partial<Record<LogoKey, string>> = {
  claude: claudeLogo,
  codebuddy: codebuddyLogo,
  codex: codexLogo,
  cursor: cursorLogo,
  trae: traeLogo,
  opencode: opencodeLogo,
  github: githubLogo,
  gitlab: gitlabLogo,
  openai: openaiLogo,
  deepseek: deepseekLogo,
  openrouter: openrouterLogo,
  xai: xaiLogo,
}

// These marks are supplied as black monochrome SVGs. In the dark theme they
// are inverted for contrast while retaining the exact brand geometry.
const monochromeLogos = new Set<LogoKey>(['codex', 'cursor', 'github', 'opencode', 'openai', 'xai'])

function normalize(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '')
}

const logoKey = computed<LogoKey>(() => {
  const values = [props.provider, props.name, props.envKey, props.type].map(normalize).filter(Boolean)
  if (values.some((value) => value === 'claudecode' || value === 'claude' || value === 'anthropic' || value.includes('claude'))) return 'claude'
  if (values.some((value) => value === 'codebuddy' || value.includes('codebuddy'))) return 'codebuddy'
  if (values.some((value) => value === 'codex' || value.includes('codex'))) return 'codex'
  if (values.some((value) => value === 'cursor' || value.includes('cursor'))) return 'cursor'
  if (values.some((value) => value === 'trae' || value.includes('trae'))) return 'trae'
  if (values.some((value) => value === 'github' || value.includes('github'))) return 'github'
  if (values.some((value) => value === 'gitlab' || value.includes('gitlab'))) return 'gitlab'
  if (values.some((value) => value === 'opencode' || value.includes('opencode'))) return 'opencode'
  if (values.some((value) => value === 'openai' || value.includes('openai'))) return 'openai'
  if (values.some((value) => value === 'deepseek' || value.includes('deepseek'))) return 'deepseek'
  if (values.some((value) => value === 'openrouter' || value.includes('openrouter'))) return 'openrouter'
  if (values.some((value) => value === 'xai' || value.includes('xai'))) return 'xai'
  return 'neutral'
})

const label = computed(() => logoLabels[logoKey.value])
const asset = computed(() => logoAssets[logoKey.value] ?? '')
</script>

<template>
  <span
    class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border"
    :class="selected ? 'border-accent bg-surface' : configured ? 'border-ok/35 bg-ok/10' : 'border-line bg-surface'"
    :data-provider-logo="logoKey"
    role="img"
    :aria-label="`${label} logo`"
    :title="`${label} logo`"
  >
    <img
      v-if="asset"
      :src="asset"
      class="h-5 w-5 object-contain"
      :class="{ 'provider-logo-image--invert': monochromeLogos.has(logoKey) }"
      :alt="`${label} logo`"
      aria-hidden="true"
      :data-logo-asset="logoKey"
      data-logo-source="bundled-brand-asset"
    />
    <Icon v-else name="lock" :size="17" class="text-txt3" aria-hidden="true" />
  </span>
</template>

<style scoped>
.provider-logo-image--invert {
  filter: invert(1);
}

:global(html.light) .provider-logo-image--invert {
  filter: none;
}
</style>

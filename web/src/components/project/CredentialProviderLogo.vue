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
import { theme } from '@/lib/shared/theme'

const props = withDefaults(
  defineProps<{
    provider?: string
    name?: string
    type?: string
    envKey?: string
    configured?: boolean
    selected?: boolean
    /** provider: ignore alias/name so a model-vendor row is not mistaken for a brand in the alias. */
    match?: 'any' | 'provider'
    /** SSH kinds have no brand mark. key is a private key, host is known hosts. */
    icon?: '' | 'key' | 'host'
  }>(),
  { provider: '', name: '', type: '', envKey: '', configured: false, selected: false, match: 'any', icon: '' },
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

// The bundled provider assets are monochrome black marks (the Simple Icons
// paths and the official Codex/OpenAI marks). In the dark theme they are
// inverted for contrast while retaining the exact brand geometry.
const monochromeLogos = new Set<LogoKey>([
  'claude',
  'codebuddy',
  'codex',
  'cursor',
  'trae',
  'opencode',
  'github',
  'gitlab',
  'openai',
  'deepseek',
  'openrouter',
  'xai',
])

function normalize(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '')
}

const logoKey = computed<LogoKey>(() => {
  const values = (props.match === 'provider' ? [props.provider] : [props.provider, props.name, props.envKey, props.type]).map(normalize).filter(Boolean)
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
const imageClasses = computed(() => ({
  'provider-logo-image--invert': monochromeLogos.has(logoKey.value) && theme.value === 'dark',
}))
</script>

<template>
  <span
    class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border"
    :class="selected ? 'border-accent bg-surface' : 'border-line bg-surface'"
    :data-provider-logo="icon === 'key' ? 'ssh-key' : icon === 'host' ? 'ssh-host' : logoKey"
    role="img"
    :aria-label="icon === 'key' ? 'SSH private key' : icon === 'host' ? 'SSH known hosts' : `${label} logo`"
    :title="icon === 'key' ? 'SSH private key' : icon === 'host' ? 'SSH known hosts' : `${label} logo`"
  >
    <Icon v-if="icon === 'key'" name="key" :size="17" class="text-txt2" aria-hidden="true" />
    <Icon v-else-if="icon === 'host'" name="server" :size="17" class="text-txt2" aria-hidden="true" />
    <img
      v-else-if="asset"
      :src="asset"
      class="h-5 w-5 object-contain"
      :class="imageClasses"
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
</style>

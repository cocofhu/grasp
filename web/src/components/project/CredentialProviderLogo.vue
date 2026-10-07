<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/ui/Icon.vue'

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
    <svg v-if="logoKey === 'claude'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <g fill="none" stroke="#D97757" stroke-linecap="round" stroke-width="2.35">
        <path d="M16 4.5v8.2M16 19.3v8.2M4.5 16h8.2M19.3 16h8.2M7.9 7.9l5.8 5.8M18.3 18.3l5.8 5.8M24.1 7.9l-5.8 5.8M13.7 18.3l-5.8 5.8" />
      </g>
      <circle cx="16" cy="16" r="2.4" fill="#D97757" />
    </svg>
    <svg v-else-if="logoKey === 'codebuddy'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="M8 7.5h11.7A4.3 4.3 0 0 1 24 11.8v8.4a4.3 4.3 0 0 1-4.3 4.3H12l-4 3.4v-16A4.3 4.3 0 0 1 12.3 7.5Z" fill="#4F46E5" />
      <path d="M13 14.5h7M13 18h4.5" fill="none" stroke="#fff" stroke-linecap="round" stroke-width="2" />
    </svg>
    <svg v-else-if="logoKey === 'codex'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="M16 4.5 26 10v12L16 27.5 6 22V10Z" fill="none" stroke="#16A34A" stroke-width="2.4" />
      <path d="m11 12 5 3 5-3M11 20l5-3 5 3M16 15v6" fill="none" stroke="#16A34A" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" />
    </svg>
    <svg v-else-if="logoKey === 'cursor'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="m7 4 18 10-8 2.5-3 8.5L7 4Z" fill="#18181B" />
      <path d="m15.2 16.5 4.3 8" fill="none" stroke="#fff" stroke-linecap="round" stroke-width="2" />
    </svg>
    <svg v-else-if="logoKey === 'trae'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <circle cx="16" cy="16" r="11.5" fill="#0EA5A4" />
      <path d="M10.5 11h11M16 11v11.5" fill="none" stroke="#fff" stroke-linecap="round" stroke-width="2.4" />
    </svg>
    <svg v-else-if="logoKey === 'opencode'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <rect x="6" y="6" width="20" height="20" rx="6" fill="#F97316" />
      <path d="m12 12-4 4 4 4M20 12l4 4-4 4" fill="none" stroke="#fff" stroke-linecap="round" stroke-linejoin="round" stroke-width="2.1" />
    </svg>
    <svg v-else-if="logoKey === 'github'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <circle cx="16" cy="16" r="11.5" fill="#18181B" />
      <path d="M11 21.5c1.5 1.3 3.2 2 5 2s3.5-.7 5-2M12 14.3a2 2 0 1 0 0 .1M20 14.3a2 2 0 1 0 0 .1M16 18.3v4" fill="none" stroke="#fff" stroke-linecap="round" stroke-width="1.65" />
    </svg>
    <svg v-else-if="logoKey === 'gitlab'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="m7 12 3-6 3 8h6l3-8 3 6-6 11-6 3-6-14Z" fill="#F97316" />
      <path d="m13 14 3 9 3-9" fill="#FBBF24" />
    </svg>
    <svg v-else-if="logoKey === 'openai'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="M16 6.5a5.1 5.1 0 0 1 4.7 3.1 5.1 5.1 0 0 1 4.8 5.2 5.1 5.1 0 0 1-2.5 4.4 5.1 5.1 0 0 1-2.5 5.2 5.1 5.1 0 0 1-5.3-.1 5.1 5.1 0 0 1-4.7-3.1 5.1 5.1 0 0 1-4.8-5.2 5.1 5.1 0 0 1 2.5-4.4 5.1 5.1 0 0 1 2.5-5.2 5.1 5.1 0 0 1 5.3.1Z" fill="none" stroke="#10A37F" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" />
      <path d="m11.8 11.1 8.4 4.8M11.8 20.9l8.4-4.8M16 6.8v9.7M16 15.5v9.7" fill="none" stroke="#10A37F" stroke-linecap="round" stroke-width="1.5" />
    </svg>
    <svg v-else-if="logoKey === 'deepseek'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="M5.5 17c2.8-6.4 8-9.6 14.1-8.2 3.4.8 5.1 3.1 6.9 6.6-2.2-1.3-4.2-1.5-6.2-.8 1.2 2.1 1.4 4.1.7 6.1-2.2 4.1-9.7 4.2-14.1 1.3-1.1-.8-1.6-2.5-1.4-5Z" fill="#4D6BFE" />
      <circle cx="20.5" cy="14" r="1.1" fill="#fff" />
    </svg>
    <svg v-else-if="logoKey === 'openrouter'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="M7 9h11a5 5 0 0 1 5 5v4M25 23H14a5 5 0 0 1-5-5v-4" fill="none" stroke="#6366F1" stroke-linecap="round" stroke-width="2.2" />
      <circle cx="7" cy="9" r="2.5" fill="#6366F1" /><circle cx="25" cy="23" r="2.5" fill="#6366F1" />
    </svg>
    <svg v-else-if="logoKey === 'xai'" viewBox="0 0 32 32" class="h-5 w-5" aria-hidden="true">
      <path d="m8 8 16 16M24 8 8 24" fill="none" stroke="#18181B" stroke-linecap="round" stroke-width="3" />
      <path d="m12 8 8 16" fill="none" stroke="#71717A" stroke-linecap="round" stroke-width="1.5" />
    </svg>
    <Icon v-else name="lock" :size="17" class="text-txt3" aria-hidden="true" />
  </span>
</template>

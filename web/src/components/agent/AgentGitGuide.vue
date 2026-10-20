<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import type { GitCredentialType } from '@/lib/agent/gitCredentialType'

type KV = { k: string; v: string }

const GIT_ENV_KEYS = new Set(['GIT_REPOS', 'GITHUB_URL', 'GITLAB_URL'])

const CHOICES: GitCredentialType[] = ['github_https', 'gitlab_https', 'ssh']

const props = defineProps<{
  env: KV[]
  upsertEnv: (key: string, value: string) => void
  credentialType?: GitCredentialType
}>()
const emit = defineEmits<{
  'update:credentialType': [value: GitCredentialType]
  help: [section: 'git']
}>()
const { t } = useI18n()

const showRecommend = computed(() => !!props.credentialType)

// Git tokens / SSH keys come from project credentials; only GIT_REPOS goes into env.
const recommendations: { key: string; value: string }[] = [
  { key: 'GIT_REPOS', value: '${vars.repos}' },
]

function selectType(type: GitCredentialType) {
  emit('update:credentialType', type)
}

function applyRecommended() {
  if (!props.credentialType) return
  for (const item of recommendations) {
    if (!props.env.some((entry) => entry.k === item.key)) {
      props.upsertEnv(item.key, item.value)
    }
  }
}

function isGitEnvKey(key: string): boolean {
  return GIT_ENV_KEYS.has(key)
}

defineExpose({ isGitEnvKey })
</script>

<template>
  <section
    class="mb-4 border-y border-line/60"
    data-test="git-guide"
  >
    <header class="flex items-start gap-3 border-b border-line/60 py-3">
      <div class="min-w-0 flex-1">
        <div class="text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.git.title') }}</div>
      </div>
      <AppButton
        type="button"
        size="sm"
        variant="outline"
        icon="help"
        data-test="git-help-link"
        @click="emit('help', 'git')"
      >
        {{ t('pages.agentStudio.envHelp.link') }}
      </AppButton>
    </header>

    <div class="py-4">
      <p class="mb-3 text-[13px] text-txt2">{{ t('pages.agentStudio.git.choiceHint') }}</p>
      <div class="grid grid-cols-3 gap-1">
        <button
          v-for="type in CHOICES"
          :key="type"
          type="button"
          class="rounded-md px-2.5 py-3 text-left"
          :class="
            credentialType === type
              ? 'bg-elevated text-txt'
              : 'text-txt2 hover:bg-elevated/50'
          "
          :data-test="`git-choice-${type}`"
          :aria-pressed="credentialType === type"
          @click="selectType(type)"
        >
          <strong class="block text-[14px] text-txt">{{ t(`pages.agentStudio.git.types.${type}`) }}</strong>
          <small class="mt-1 block text-[11px] text-txt3">{{ t(`pages.agentStudio.git.typeHints.${type}`) }}</small>
        </button>
      </div>
      <div v-if="showRecommend" class="mt-3">
        <AppButton
          size="sm"
          variant="outline"
          data-test="git-apply-recommended"
          @click="applyRecommended"
        >
          {{ t('pages.agentStudio.git.applyRecommended') }}
        </AppButton>
      </div>
    </div>
  </section>
</template>

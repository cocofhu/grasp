<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import CodeEditor from '@/components/ui/CodeEditor.vue'
import EnvCredentialHelpModal, {
  type EnvCredentialHelpSection,
} from '@/components/agent/EnvCredentialHelpModal.vue'
import { isSecretEnvKey } from '@/lib/agent/secretEnvKeys'
import {
  getRegionPolicy,
  isManagedRegionKey,
  normalizeRegions,
} from '@/lib/shared/regionPolicy'
import {
  kvToRec,
  recToKV,
  type AgentStudioDraft,
} from '@/lib/agent/agentStudioDraft'
import { MODEL_RUNTIME_ENV_KEYS } from '@/lib/agent/modelRouting'

const props = defineProps<{
  draft: AgentStudioDraft
  /** agent = Studio 单 Agent；shared = 工作区共享配置 */
  context?: 'agent' | 'shared'
}>()
const emit = defineEmits<{ toast: [msg: string]; 'open-settings-file': []; 'edit-model': [] }>()

const { t } = useI18n()


const envRaw = ref(false)
const envRawText = ref('')
const rawError = ref('')
const envHelpOpen = ref(false)
const envHelpSection = ref<EnvCredentialHelpSection>('inject')

const currentRegionPolicy = computed(() => getRegionPolicy(props.draft.acpBackend))
function isLockedKey(key: string): boolean {
  return isManagedRegionKey(key) || MODEL_RUNTIME_ENV_KEYS.has(key.trim())
}

function openEnvHelp(section: EnvCredentialHelpSection) {
  envHelpSection.value = section
  envHelpOpen.value = true
}

/** Secrets are owned by workspace credentials; env rows with these keys are rejected on save. */
const credentialsRoute = computed(() => ({ path: '/settings/credentials' }))
const rawHasSecretEnvKey = computed(() => props.draft.env.some((e) => isSecretEnvKey(e.k)))

function storedRegion(): string {
  const policy = getRegionPolicy(props.draft.acpBackend)
  if (!policy) return ''
  return props.draft.env.find((e) => e.k === policy.regionEnvKey)?.v || ''
}

function updateEnvKey(i: number, value: string) {
  if (isManagedRegionKey(value)) {
    props.draft.env[i].k = ''
    emit('toast', t('pages.agentStudio.region.managedConflict'))
    return
  }
  if (MODEL_RUNTIME_ENV_KEYS.has(value.trim())) {
    props.draft.env[i].k = ''
    emit('toast', t('pages.agentStudio.cliModel.managedBy'))
    return
  }
  props.draft.env[i].k = value
}

function toggleEnvRaw() {
  envRaw.value = !envRaw.value
  rawError.value = ''
  if (envRaw.value) envRawText.value = JSON.stringify(kvToRec(props.draft.env), null, 2)
}

function onEnvRaw(text: string) {
  envRawText.value = text
  try {
    const obj = JSON.parse(text)
    if (typeof obj !== 'object' || Array.isArray(obj)) throw new Error(t('pages.agentStudio.dialogs.jsonObject'))
    const incoming = obj as Record<string, string>
    if (Object.keys(incoming).some(key => MODEL_RUNTIME_ENV_KEYS.has(key))) throw new Error(t('pages.agentStudio.cliModel.managedBy'))
    const policy = getRegionPolicy(props.draft.acpBackend)
    if (policy) incoming[policy.regionEnvKey] = storedRegion()
    props.draft.env = recToKV(
      normalizeRegions(incoming, props.draft.acpBackend, 'preserve-special').env,
    )
    rawError.value = ''
  } catch (e: any) {
    rawError.value = t('pages.agentStudio.env.parseError') + (e?.message || e)
  }
}

watch(
  () => props.draft.name,
  () => {
    envRaw.value = false
    rawError.value = ''
  },
)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4">
      <AppButton
        type="button"
        size="sm"
        variant="outline"
        icon="help"
        data-test="env-help-inject"
        @click="openEnvHelp('inject')"
      >
        {{ t('pages.agentStudio.envHelp.link') }}
      </AppButton>
      <span class="flex-1" />
      <button class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong" @click="toggleEnvRaw">{{ envRaw ? t('pages.agentStudio.mcp.formEdit') : t('pages.agentStudio.mcp.rawJson') }}</button>
    </div>

    <div v-if="envRaw" class="flex min-h-0 flex-1 flex-col">
      <div v-if="rawError" class="border-b border-err/30 bg-err/10 px-4 py-1.5 text-[11px] text-err">{{ rawError }}</div>
      <div v-if="rawHasSecretEnvKey" class="border-b border-warn/30 bg-warn/10 px-4 py-1.5 text-[11px] text-warn" data-test="env-secret-hint">
        {{ t('pages.agentStudio.env.secretCredentialsOnly') }}
        <RouterLink v-if="credentialsRoute" :to="credentialsRoute" class="ml-1 text-accent-2 underline underline-offset-[3px]" data-test="env-secret-credentials-link">{{ t('pages.agentStudio.env.goCredentials') }}</RouterLink>
      </div>
      <div class="min-h-0 flex-1"><CodeEditor :model-value="envRawText" language="json" @update:model-value="onEnvRaw" /></div>
    </div>
    <div v-else class="scroll-area flex-1 space-y-2 overflow-y-auto p-4">
      <template v-for="(e, i) in draft.env" :key="i">
        <div v-if="!isLockedKey(e.k)" class="flex items-center gap-1.5">
          <input :value="e.k" placeholder="KEY" class="w-1/3 min-w-0 rounded border border-line bg-surface px-2 py-1.5 font-mono text-[12px] text-txt outline-none focus:border-accent" @input="updateEnvKey(i, ($event.target as HTMLInputElement).value)" />
          <input v-model="e.v" placeholder="value" class="min-w-0 flex-1 rounded border border-line bg-surface px-2 py-1.5 font-mono text-[12px] text-txt2 outline-none focus:border-accent" />
          <button class="text-txt3 hover:text-err" @click="draft.env.splice(i, 1)"><Icon name="close" :size="14" /></button>
        </div>
        <p v-if="isSecretEnvKey(e.k)" class="pl-1 text-[11px] text-warn" data-test="env-secret-hint">
          {{ t('pages.agentStudio.env.secretCredentialsOnly') }}
          <RouterLink v-if="credentialsRoute" :to="credentialsRoute" class="ml-1 text-accent-2 underline underline-offset-[3px]" data-test="env-secret-credentials-link">{{ t('pages.agentStudio.env.goCredentials') }}</RouterLink>
        </p>
      </template>
      <div v-if="currentRegionPolicy" class="border-t border-line/60 py-4">
        <div class="mb-2 text-[11px] text-txt3">{{ t('pages.agentStudio.region.managedByAcp') }}</div>
        <div class="flex items-center gap-1.5">
          <input :value="currentRegionPolicy.regionEnvKey" readonly :aria-label="t('pages.agentStudio.region.managedKey')" class="w-1/3 min-w-0 rounded border border-line bg-elevated px-2 py-1.5 font-mono text-[12px] text-txt3" />
          <input :value="storedRegion()" readonly :aria-label="t('pages.agentStudio.region.managedValue')" class="min-w-0 flex-1 rounded border border-line bg-elevated px-2 py-1.5 font-mono text-[12px] text-txt3" />
        </div>
      </div>
      <AppButton size="sm" variant="outline" icon="plus" @click="draft.env.push({ k: '', v: '' })">{{ t('pages.agentStudio.env.add') }}</AppButton>
    </div>

    <EnvCredentialHelpModal
      :open="envHelpOpen"
      :section="envHelpSection"
      :backend="draft.acpBackend || 'cursor'"
      @close="envHelpOpen = false"
    />
  </div>
</template>

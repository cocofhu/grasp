<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type { CredentialItem } from '@/lib/api/api'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import { LEGACY_GIT_AUTH_KEYS } from '@/lib/agent/gitBindings'
import AppSelect from '@/components/ui/AppSelect.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
const props = defineProps<{ draft: AgentStudioDraft }>()
const { t } = useI18n()
const credentials = ref<CredentialItem[]>([])
const error = ref('')
const keys = computed(() => [...new Set([...Object.keys(props.draft.credentialBindings || {}), ...credentials.value.map((credential) => credential.envKey || '').filter(Boolean)])].filter((key) => !LEGACY_GIT_AUTH_KEYS.has(key)).sort())
function selection(key: string): string {
  const bindings = props.draft.credentialBindings || {}
  return Object.prototype.hasOwnProperty.call(bindings, key) ? bindings[key] === null ? '__disabled' : bindings[key] || '__inherit' : '__inherit'
}
function change(key: string, value: string) {
  const bindings = { ...props.draft.credentialBindings }
  if (value === '__inherit') delete bindings[key]
  else bindings[key] = value === '__disabled' ? null : value
  props.draft.credentialBindings = bindings
}
function options(key: string) {
  const matching = credentials.value.filter((row) => row.envKey === key)
  const value = selection(key)
  return [
    { value: '__inherit', label: t('pages.workspaceDefaults.inherit') },
    { value: '__disabled', label: t('pages.workspaceDefaults.doNotInject') },
    ...matching.map((credential) => ({ value: credential.id, label: credential.name })),
    ...(value !== '__inherit' && value !== '__disabled' && !matching.some((row) => row.id === value)
      ? [{ value, label: `${value} · ${t('pages.workspaceDefaults.unavailable')}` }] : []),
  ]
}
function provider(key: string, id: string) {
  const credential = credentials.value.find((row) => row.envKey === key && row.id === id)
  return credential?.issuer || credential?.provider || key
}
function isCredential(value: string) { return value !== '__inherit' && value !== '__disabled' }
async function load() {
  error.value = ''
  try { credentials.value = (await api.listCredentials()).items || [] }
  catch (cause: unknown) { error.value = String((cause as Error)?.message || cause) }
}
onMounted(() => { void load() })
</script>
<template>
  <section class="mt-6 max-w-3xl border-t border-line pt-5" data-testid="agent-credential-bindings">
    <h3 class="text-sm font-semibold text-txt">{{ t('pages.workspaceDefaults.credentialTitle') }}</h3>
    <p class="mt-1 text-xs leading-5 text-txt3">{{ t('pages.workspaceDefaults.credentialHint') }}</p>
    <p v-if="error" class="mt-2 text-xs text-err" role="alert">{{ error }} <button type="button" class="underline" @click="load">{{ t('common.buttons.retry') }}</button></p>
    <div v-for="key in keys" :key="key" class="mt-3 flex flex-wrap items-center gap-3">
      <span class="min-w-48 break-all font-mono text-xs text-txt2">{{ key }}</span>
      <AppSelect :id="`credential-binding-${key}`" class="min-w-0 basis-56 flex-1" :model-value="selection(key)" :options="options(key)" :aria-label="key" searchable @update:model-value="change(key, $event)">
        <template #leading><CredentialProviderLogo v-if="isCredential(selection(key))" :provider="provider(key, selection(key))" match="provider" /></template>
        <template #option-leading="{ option }"><CredentialProviderLogo v-if="isCredential(option.value)" :provider="provider(key, option.value)" match="provider" /></template>
      </AppSelect>
    </div>
    <RouterLink to="/settings/credentials" class="mt-3 inline-block text-xs text-accent-2">{{ t('pages.workspaceDefaults.manageCredentials') }}</RouterLink>
  </section>
</template>

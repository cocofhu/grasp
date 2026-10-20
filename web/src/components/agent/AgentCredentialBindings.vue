<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type { ProjectCredentialItem } from '@/lib/api/api'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
const props = defineProps<{ draft: AgentStudioDraft }>()
const { t } = useI18n()
const { workspaceId, ensureLoaded } = useWorkspaceContext()
const credentials = ref<ProjectCredentialItem[]>([])
const error = ref('')
const keys = computed(() => [...new Set([...Object.keys(props.draft.credentialBindings || {}), ...credentials.value.map((credential) => credential.envKey || '').filter(Boolean)])].sort())
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
async function load() {
  error.value = ''
  try { await ensureLoaded(); credentials.value = (await api.getProjectCredentials(workspaceId.value)).items || [] }
  catch (cause: any) { error.value = String(cause?.message || cause) }
}
onMounted(() => { void load() })
</script>
<template>
  <section class="mt-6 max-w-3xl border-t border-line pt-5" data-testid="agent-credential-bindings">
    <h3 class="text-sm font-semibold text-txt">{{ t('pages.workspaceDefaults.credentialTitle') }}</h3>
    <p class="mt-1 text-xs leading-5 text-txt3">{{ t('pages.workspaceDefaults.credentialHint') }}</p>
    <p v-if="error" class="mt-2 text-xs text-err" role="alert">{{ error }} <button type="button" class="underline" @click="load">{{ t('common.buttons.retry') }}</button></p>
    <div v-for="key in keys" :key="key" class="mt-3 flex flex-wrap items-center gap-3">
      <label :for="`credential-binding-${key}`" class="min-w-48 break-all font-mono text-xs text-txt2">{{ key }}</label>
      <select :id="`credential-binding-${key}`" class="input min-w-56 flex-1 text-xs" :value="selection(key)" @change="change(key, ($event.target as HTMLSelectElement).value)">
        <option value="__inherit">{{ t('pages.workspaceDefaults.inherit') }}</option>
        <option value="__disabled">{{ t('pages.workspaceDefaults.doNotInject') }}</option>
        <option v-for="credential in credentials.filter((row) => row.envKey === key)" :key="credential.id" :value="credential.id">{{ credential.name }}</option>
        <option v-if="selection(key) !== '__inherit' && selection(key) !== '__disabled' && !credentials.some((row) => row.id === selection(key))" :value="selection(key)">{{ selection(key) }} · {{ t('pages.workspaceDefaults.unavailable') }}</option>
      </select>
    </div>
    <RouterLink to="/settings/credentials" class="mt-3 inline-block text-xs text-accent-2">{{ t('pages.workspaceDefaults.manageCredentials') }}</RouterLink>
  </section>
</template>

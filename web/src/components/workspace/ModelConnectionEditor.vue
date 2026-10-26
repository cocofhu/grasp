<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { api, type CredentialItem, type ModelConnection, type ModelConnectionBody, type ModelProtocol } from '@/lib/api/api'
import { isModelCredential, modelProviderPreset, MODEL_PROTOCOLS, useRoutingCopy } from '@/lib/agent/modelRouting'
import { useToast } from '@/lib/composables/useToast'
import AppButton from '@/components/ui/AppButton.vue'
import CredentialEditorSurface from './CredentialEditorSurface.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import CredentialProviderLogo from './CredentialProviderLogo.vue'

const props = defineProps<{ credentials: CredentialItem[]; inline?: boolean }>()
const emit = defineEmits<{ changed: []; error: [message: string]; editing: [active: boolean] }>()
const c = useRoutingCopy()
const toast = useToast()
const open = ref(false)
watch(open, (active) => emit('editing', active))
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const error = ref('')
const accountId = ref('')
const connectionId = ref('')
const requestedConnectionId = ref('')
let loadGeneration = 0
onBeforeUnmount(() => { loadGeneration++ })
function close() {
  if (saving.value) return
  loadGeneration++; open.value = false; loading.value = false; loaded.value = false
}
const account = computed(() => props.credentials.find((item) => item.id === accountId.value && isModelCredential(item)))
const form = reactive({ baseUrl: '', protocol: 'openai-chat-completions' as ModelProtocol, scheme: 'bearer' as 'bearer' | 'x-api-key' })
const protocolOptions = MODEL_PROTOCOLS.map((value) => ({ value, label: value }))
const authOptions = [{ value: 'bearer', label: 'Authorization: Bearer' }, { value: 'x-api-key', label: 'x-api-key' }]
const valid = computed(() => {
  if (!loaded.value || !account.value || !MODEL_PROTOCOLS.includes(form.protocol) || !authOptions.some((option) => option.value === form.scheme)) return false
  try { const url = new URL(form.baseUrl); return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password && !url.search && !url.hash }
  catch { return false }
})
function setProtocol(value: string) { if (MODEL_PROTOCOLS.includes(value as ModelProtocol)) form.protocol = value as ModelProtocol }
function setAuth(value: string) { if (value === 'bearer' || value === 'x-api-key') form.scheme = value }
function accountProvider() {
  const key = account.value
  const provider = key?.provider || key?.issuer || 'custom'
  return /^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(provider) ? provider : 'custom'
}
async function loadAccount() {
  const generation = ++loadGeneration
  const id = accountId.value
  const requested = requestedConnectionId.value
  const key = account.value
  loading.value = true; loaded.value = false; error.value = ''
  try {
    const rows = (await api.listModelConnections()).items
    if (generation !== loadGeneration || !open.value) return
    const row = requested ? rows.find((item) => item.id === requested && item.credentialId === id) : rows.find((item) => item.credentialId === id)
    if (requested && !row) throw new Error(c.value.missingConnection)
    connectionId.value = row?.id || ''
    const preset = modelProviderPreset(key?.issuer || key?.provider || '')
    Object.assign(form, { baseUrl: row?.baseUrl || preset?.baseUrl || '', protocol: row?.protocol || preset?.protocol || 'openai-chat-completions', scheme: row?.auth.scheme || preset?.scheme || 'bearer' })
    loaded.value = true
  } catch (cause) { if (generation === loadGeneration && open.value) error.value = String((cause as Error).message || cause) }
  finally { if (generation === loadGeneration && open.value) loading.value = false }
}
async function openForCredential(id: string, serviceId = '') {
  if (saving.value) return
  if (!props.credentials.some((item) => item.id === id && isModelCredential(item))) { emit('error', c.value.missingCredential); return }
  accountId.value = id; requestedConnectionId.value = serviceId; connectionId.value = ''
  open.value = true
  await loadAccount()
}
async function save() {
  if (!valid.value || saving.value || !account.value) return
  saving.value = true; error.value = ''
  try {
    const rows = (await api.listModelConnections()).items
    const current = connectionId.value ? rows.find((item) => item.id === connectionId.value && item.credentialId === accountId.value) : rows.find((item) => item.credentialId === accountId.value)
    if (connectionId.value && !current) throw new Error(c.value.missingConnection)
    const body: ModelConnectionBody = {
      name: current?.name || account.value.name,
      providerId: current?.providerId || accountProvider(),
      credentialId: accountId.value,
      baseUrl: form.baseUrl.trim(), protocol: form.protocol, auth: { scheme: form.scheme },
      models: current?.models || [], state: current?.state || 'active',
      ...(current?.service ? { service: current.service } : {}),
    }
    if (current) await api.updateModelConnection(current.id, body)
    else await api.createModelConnection(body)
    open.value = false; toast.success(c.value.connectionSaved); emit('changed')
  } catch (cause) { error.value = String((cause as Error).message || cause) }
  finally { saving.value = false }
}
defineExpose({ openForCredential })
</script>

<template>
  <CredentialEditorSurface :inline="inline" :open="open" :title="c.editService" :width="560" :close-on-esc="!saving" trap-focus :close-label="c.close" @close="close">
    <div class="mb-5 flex min-w-0 items-center gap-3" data-testid="api-account-identity">
      <CredentialProviderLogo :provider="account?.issuer || account?.provider" match="provider" />
      <span class="truncate text-[13px] font-medium text-txt">{{ account?.name }}</span>
    </div>
    <p v-if="loading" class="text-xs text-txt3">{{ c.loading }}</p>
    <form v-else-if="loaded" id="api-account-address-form" class="space-y-4" data-testid="model-connection-form" @submit.prevent="save">
      <label class="block"><span class="mb-1.5 block text-xs text-txt2">API Base URL</span><input v-model="form.baseUrl" type="url" class="w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-xs text-txt outline-none focus:border-accent" :disabled="saving" data-testid="model-connection-base-url" placeholder="https://…" /></label>
      <details><summary class="cursor-pointer text-xs text-txt3">{{ c.advancedSettings }}</summary><div class="mt-3 space-y-3">
        <label class="block"><span class="mb-1.5 block text-xs text-txt2">{{ c.protocol }}</span><AppSelect :model-value="form.protocol" :options="protocolOptions" :aria-label="c.protocol" :disabled="saving" searchable data-testid="model-connection-protocol" @update:model-value="setProtocol" /></label>
        <label class="block"><span class="mb-1.5 block text-xs text-txt2">{{ c.auth }}</span><AppSelect :model-value="form.scheme" :options="authOptions" :aria-label="c.auth" :disabled="saving" searchable data-testid="model-connection-auth" @update:model-value="setAuth" /></label>
      </div></details>
    </form>
    <p v-if="error" role="alert" class="mt-3 break-words text-xs text-err">{{ error }} <AppButton v-if="!loaded" size="sm" variant="ghost" data-testid="api-account-address-retry" @click="loadAccount">{{ c.retry }}</AppButton></p>
    <template #footer><AppButton variant="ghost" :disabled="saving" @click="close">{{ c.cancel }}</AppButton><AppButton variant="primary" type="submit" form="api-account-address-form" :loading="saving" :disabled="!valid || loading || saving" data-testid="model-connection-save">{{ c.save }}</AppButton></template>
  </CredentialEditorSurface>
</template>

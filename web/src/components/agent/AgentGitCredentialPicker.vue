<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type AgentGitBinding, type AgentGitBindings, type CredentialItem } from '@/lib/api/api'
import { cloneGitBindings, gitCredentialKind, usableGitCredential, validCredentialGitScope } from '@/lib/agent/gitBindings'
import { defaultGitCredentialHost, kindOfItem } from '@/lib/workspace/credentialKinds'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'

const props = withDefaults(defineProps<{
  modelValue?: AgentGitBindings
  items?: CredentialItem[]
  disabled?: boolean
  summary?: boolean
  compact?: boolean
  hideManageLink?: boolean
}>(), { disabled: false, summary: false, compact: false, hideManageLink: false })
const emit = defineEmits<{ 'update:modelValue': [value: AgentGitBindings]; validity: [valid: boolean]; help: [] }>()
const { t } = useI18n()
const copy = (key: string) => t(`pages.agentStudio.gitCredentials.${key}`)
const credentials = ref<CredentialItem[]>([])
const loading = ref(false)
const error = ref('')
const bindings = computed(() => props.modelValue || { items: [] })
const selectedIds = computed(() => new Set(bindings.value.items.map((item) => item.credentialId)))
const available = computed(() => credentials.value.filter((item) => usableGitCredential(item) && !selectedIds.value.has(item.id)))
const options = computed(() => available.value.map((item) => ({ value: item.id, label: item.name, hint: providerName(item) })))
function credential(id: string) { return credentials.value.find((item) => item.id === id) }
function kind(id: string) { const item = credential(id); return item ? kindOfItem(item) : undefined }
function providerName(item?: CredentialItem) {
  const id = item ? gitCredentialKind(item)?.id : undefined
  if (id === 'github' || id === 'gitlab') return (item?.host || defaultGitCredentialHost(id)).replace(/^https:\/\//, '').replace(/\/$/, '')
  return id === 'ssh_key' ? 'SSH' : ''
}
function title(id: string) { return credential(id)?.name || id }
function scopeValid(item: AgentGitBinding) { return validCredentialGitScope(item.scope || '', credential(item.credentialId)) }
function isSsh(item: AgentGitBinding) { return kind(item.credentialId)?.id === 'ssh_key' }
function unavailable(item: AgentGitBinding) { return !usableGitCredential(credential(item.credentialId)) }
function subtitle(item: AgentGitBinding) {
  const target = item.scope || providerName(credential(item.credentialId))
  if (!isSsh(item)) return target
  return `${target} · ${t(credential(item.credentialId)?.knownHosts ? 'pages.workspace.credentials.knownHostsConfigured' : 'pages.workspace.credentials.knownHostsNotConfigured')}`
}
function update(value: AgentGitBindings) { emit('update:modelValue', value) }
function add(id: string) {
  if (props.disabled || props.summary || !available.value.some((item) => item.id === id)) return
  const next = cloneGitBindings(bindings.value)
  next.items.push({ credentialId: id })
  update(next)
}
function remove(id: string) {
  if (props.disabled || props.summary) return
  const next = cloneGitBindings(bindings.value)
  next.items = next.items.filter((item) => item.credentialId !== id)
  update(next)
}
function changeScope(id: string, value: string) {
  if (props.disabled || props.summary) return
  const next = cloneGitBindings(bindings.value)
  const item = next.items.find((row) => row.credentialId === id)
  if (!item) return
  if (value) item.scope = value
  else delete item.scope
  update(next)
}
const valid = computed(() => !bindings.value.items.length || (!loading.value && !error.value && bindings.value.items.every((item) => !unavailable(item) && scopeValid(item))))
const compactIssues = computed(() => {
  if (loading.value || error.value) return []
  return bindings.value.items.flatMap((item) => unavailable(item) ? [`${title(item.credentialId)} · ${copy('unavailable')}`] : !scopeValid(item) ? [`${title(item.credentialId)} · ${copy('scopeInvalid')}`] : [])
})
watch(valid, (value) => emit('validity', value), { immediate: true })
watch(() => props.items, (value) => { if (value) credentials.value = value }, { immediate: true })
async function load() {
  loading.value = true; error.value = ''
  try { credentials.value = (await api.listCredentials()).items || [] }
  catch (cause) { error.value = String((cause as Error)?.message || cause) }
  finally { loading.value = false }
}
onMounted(() => { if (props.items === undefined) void load() })
</script>

<template>
  <section v-if="!compact || error || compactIssues.length" class="min-w-0 space-y-3" data-testid="agent-git-credential-picker">
    <template v-if="!compact">
    <div class="flex items-center justify-between gap-3">
      <h3 class="m-0 min-w-0 flex-1 text-[13px] font-semibold text-txt">{{ copy('title') }}</h3>
      <slot name="heading-actions" />
      <button v-if="!summary" type="button" class="shrink-0 text-[11px] text-txt3 hover:text-accent-2" data-testid="git-credential-help" data-test="git-help-link" @click="emit('help')">{{ copy('help') }}</button>
    </div>
    <p v-if="error" role="alert" class="text-xs text-err">{{ error }} <button type="button" class="underline" @click="load">{{ copy('retry') }}</button></p>
    <AppSelect v-if="!summary" model-value="" :options="options" :placeholder="loading ? copy('loading') : copy('add')" :aria-label="copy('add')" :empty-text="copy('noneAvailable')" :disabled="disabled || loading" searchable data-testid="git-credential-add" @update:model-value="add">
      <template #leading><Icon name="plus" :size="15" class="text-txt3" /></template>
      <template #option-leading="{ option }"><CredentialProviderLogo :provider="kind(option.value)?.logoProvider" :icon="kind(option.value)?.icon" match="provider" /></template>
    </AppSelect>
    <div v-if="bindings.items.length" class="divide-y divide-line rounded-xl border border-line">
      <div v-for="item in bindings.items" :key="item.credentialId" class="min-w-0 p-3" :data-testid="`git-credential-row-${item.credentialId}`">
        <div class="flex min-w-0 items-center gap-2.5">
          <CredentialProviderLogo :provider="kind(item.credentialId)?.logoProvider" :icon="kind(item.credentialId)?.icon" match="provider" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <div class="truncate text-xs font-medium text-txt" :title="title(item.credentialId)">{{ title(item.credentialId) }}</div>
            <div class="truncate text-[11px] text-txt3" :data-testid="`git-credential-subtitle-${item.credentialId}`">{{ subtitle(item) }}</div>
          </div>
          <button v-if="!summary" type="button" class="grid h-7 w-7 shrink-0 place-items-center rounded-md text-txt3 hover:bg-elevated hover:text-err" :aria-label="`${copy('remove')} · ${title(item.credentialId)}`" :disabled="disabled" :data-testid="`git-remove-${item.credentialId}`" @click="remove(item.credentialId)"><Icon name="close" :size="14" /></button>
        </div>
        <p v-if="!loading && !error && unavailable(item)" role="alert" class="mb-0 mt-2 text-[11px] text-err">{{ copy('unavailable') }}</p>
        <p v-if="summary && !loading && !error && !scopeValid(item)" role="alert" class="mb-0 mt-2 text-[11px] text-err">{{ copy('scopeInvalid') }}</p>
        <details v-if="!summary" class="mt-2" :data-testid="`git-options-${item.credentialId}`">
          <summary class="w-fit cursor-pointer text-[11px] text-txt3 hover:text-txt2">{{ copy('options') }}</summary>
          <div class="mt-3 space-y-3">
            <label class="block"><span class="mb-1.5 block text-[11px] text-txt2">{{ copy('scope') }}</span><input :value="item.scope || ''" :placeholder="copy('scopePlaceholder')" :disabled="disabled" :aria-invalid="!scopeValid(item)" :data-testid="`git-scope-${item.credentialId}`" class="w-full min-w-0 rounded-md border border-line bg-base px-3 py-2 text-xs text-txt outline-none focus:border-accent" spellcheck="false" @input="changeScope(item.credentialId, ($event.target as HTMLInputElement).value)" /></label>
            <p class="m-0 text-[11px] leading-5" :class="scopeValid(item) ? 'text-txt3' : 'text-err'">{{ scopeValid(item) ? copy('scopeHint') : copy('scopeInvalid') }}</p>
          </div>
        </details>
      </div>
    </div>
    <p v-else class="rounded-xl border border-dashed border-line px-4 py-6 text-center text-xs text-txt3">{{ copy('empty') }}</p>
    <RouterLink v-if="!summary && !hideManageLink" to="/settings/credentials" class="inline-block text-[11px] text-accent-2">{{ copy('manage') }}</RouterLink>
    </template>
    <template v-else>
      <p v-if="error" role="alert" class="m-0 text-xs text-err">{{ error }} <button type="button" class="underline" @click="load">{{ copy('retry') }}</button></p>
      <p v-for="issue in compactIssues" :key="issue" role="alert" class="m-0 text-xs text-err">{{ issue }}</p>
    </template>
  </section>
</template>

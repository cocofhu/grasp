<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type AgentGitBinding, type AgentGitBindings, type CredentialItem } from '@/lib/api/api'
import { cloneGitBindings, gitCredentialKind, usableGitCredential, validGitScope } from '@/lib/agent/gitBindings'
import { credentialUsable } from '@/lib/agent/modelRouting'
import { kindOfItem } from '@/lib/workspace/credentialKinds'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'

const props = withDefaults(defineProps<{
  modelValue?: AgentGitBindings
  items?: CredentialItem[]
  disabled?: boolean
}>(), { disabled: false })
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
function providerName(item?: CredentialItem) { const id = item ? gitCredentialKind(item)?.id : undefined; return id === 'github' ? 'GitHub' : id === 'gitlab' ? 'GitLab' : id === 'ssh_key' ? 'SSH' : '' }
function title(id: string) { return credential(id)?.name || id }
function scopeValid(item: AgentGitBinding) { return validGitScope(item.scope || '', gitCredentialKind(credential(item.credentialId))?.id) }
function isSsh(item: AgentGitBinding) { return kind(item.credentialId)?.id === 'ssh_key' }
function hostsUsable(id: string) { const item = credential(id); return !!item && kindOfItem(item)?.id === 'ssh_hosts' && credentialUsable(item) }
function unavailable(item: AgentGitBinding) { return !usableGitCredential(credential(item.credentialId)) || (!!item.sshHostsCredentialId && (!isSsh(item) || !hostsUsable(item.sshHostsCredentialId))) }
function hostsOptions(item: AgentGitBinding) {
  const rows = credentials.value.filter((row) => kindOfItem(row)?.id === 'ssh_hosts' && credentialUsable(row))
  return [{ value: '', label: copy('hostsNone') }, ...rows.map((row) => ({ value: row.id, label: row.name })), ...(item.sshHostsCredentialId && !rows.some((row) => row.id === item.sshHostsCredentialId) ? [{ value: item.sshHostsCredentialId, label: `${title(item.sshHostsCredentialId)} · ${copy('unavailable')}` }] : [])]
}
function update(value: AgentGitBindings) { emit('update:modelValue', value) }
function add(id: string) {
  if (props.disabled || !available.value.some((item) => item.id === id)) return
  const next = cloneGitBindings(bindings.value)
  if (!next.items.length) next.preferredCredentialId = id
  next.items.push({ credentialId: id })
  update(next)
}
function remove(id: string) {
  if (props.disabled) return
  const next = cloneGitBindings(bindings.value)
  next.items = next.items.filter((item) => item.credentialId !== id)
  if (next.preferredCredentialId === id) next.preferredCredentialId = next.items[0]?.credentialId
  if (!next.items.length) delete next.preferredCredentialId
  update(next)
}
function prefer(id: string) {
  if (props.disabled || !bindings.value.items.some((item) => item.credentialId === id) || !usableGitCredential(credential(id))) return
  if (bindings.value.preferredCredentialId !== id) update({ ...cloneGitBindings(bindings.value), preferredCredentialId: id })
}
function change(id: string, field: 'scope' | 'sshHostsCredentialId', value: string) {
  if (props.disabled) return
  const next = cloneGitBindings(bindings.value)
  const item = next.items.find((row) => row.credentialId === id)
  if (!item) return
  if (value) item[field] = value
  else delete item[field]
  update(next)
}
const valid = computed(() => !bindings.value.items.length || (!loading.value && !error.value && bindings.value.items.every((item) => !unavailable(item) && scopeValid(item)) && bindings.value.items.some((item) => item.credentialId === bindings.value.preferredCredentialId)))
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
  <section class="min-w-0 space-y-3" data-testid="agent-git-credential-picker">
    <div class="flex items-center justify-between gap-3">
      <h3 class="m-0 text-[13px] font-semibold text-txt">{{ copy('title') }}</h3>
      <button type="button" class="shrink-0 text-[11px] text-txt3 hover:text-accent-2" data-testid="git-credential-help" data-test="git-help-link" @click="emit('help')">{{ copy('help') }}</button>
    </div>
    <p class="m-0 text-xs leading-5 text-txt3">{{ copy('hint') }}</p>
    <p v-if="error" role="alert" class="text-xs text-err">{{ error }} <button type="button" class="underline" @click="load">{{ copy('retry') }}</button></p>
    <AppSelect model-value="" :options="options" :placeholder="loading ? copy('loading') : copy('add')" :aria-label="copy('add')" :empty-text="copy('noneAvailable')" :disabled="disabled || loading" searchable data-testid="git-credential-add" @update:model-value="add">
      <template #leading><Icon name="plus" :size="15" class="text-txt3" /></template>
      <template #option-leading="{ option }"><CredentialProviderLogo :provider="kind(option.value)?.logoProvider" :icon="kind(option.value)?.icon" match="provider" /></template>
    </AppSelect>
    <div v-if="bindings.items.length" class="divide-y divide-line rounded-xl border border-line">
      <div v-for="item in bindings.items" :key="item.credentialId" class="min-w-0 p-3" :data-testid="`git-credential-row-${item.credentialId}`">
        <div class="flex min-w-0 items-center gap-2.5">
          <CredentialProviderLogo :provider="kind(item.credentialId)?.logoProvider" :icon="kind(item.credentialId)?.icon" match="provider" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <div class="truncate text-xs font-medium text-txt" :title="title(item.credentialId)">{{ title(item.credentialId) }}</div>
            <div class="truncate text-[11px] text-txt3">{{ item.scope || providerName(credential(item.credentialId)) }}</div>
          </div>
          <button type="button" class="shrink-0 rounded-md px-2 py-1 text-[11px] transition" :class="item.credentialId === bindings.preferredCredentialId ? 'bg-accent-dim text-accent-2' : 'text-txt3 hover:bg-elevated hover:text-txt'" :aria-pressed="item.credentialId === bindings.preferredCredentialId" :aria-label="`${copy('setPreferred')} · ${title(item.credentialId)}`" :disabled="disabled || !usableGitCredential(credential(item.credentialId))" :data-testid="`git-preferred-${item.credentialId}`" @click="prefer(item.credentialId)">{{ item.credentialId === bindings.preferredCredentialId ? copy('preferred') : copy('setPreferred') }}</button>
          <button type="button" class="grid h-7 w-7 shrink-0 place-items-center rounded-md text-txt3 hover:bg-elevated hover:text-err" :aria-label="`${copy('remove')} · ${title(item.credentialId)}`" :disabled="disabled" :data-testid="`git-remove-${item.credentialId}`" @click="remove(item.credentialId)"><Icon name="close" :size="14" /></button>
        </div>
        <p v-if="!loading && !error && unavailable(item)" role="alert" class="mb-0 mt-2 text-[11px] text-err">{{ copy('unavailable') }}</p>
        <details class="mt-2" :data-testid="`git-options-${item.credentialId}`">
          <summary class="w-fit cursor-pointer text-[11px] text-txt3 hover:text-txt2">{{ copy('options') }}</summary>
          <div class="mt-3 space-y-3">
            <label class="block"><span class="mb-1.5 block text-[11px] text-txt2">{{ copy('scope') }}</span><input :value="item.scope || ''" :placeholder="copy('scopePlaceholder')" :disabled="disabled" :aria-invalid="!scopeValid(item)" :data-testid="`git-scope-${item.credentialId}`" class="w-full min-w-0 rounded-md border border-line bg-base px-3 py-2 text-xs text-txt outline-none focus:border-accent" spellcheck="false" @input="change(item.credentialId, 'scope', ($event.target as HTMLInputElement).value)" /></label>
            <p class="m-0 text-[11px] leading-5" :class="scopeValid(item) ? 'text-txt3' : 'text-err'">{{ scopeValid(item) ? copy('scopeHint') : copy('scopeInvalid') }}</p>
            <div v-if="isSsh(item)"><span class="mb-1.5 block text-[11px] text-txt2">{{ copy('hosts') }}</span><AppSelect :model-value="item.sshHostsCredentialId || ''" :options="hostsOptions(item)" :aria-label="`${copy('hosts')} · ${title(item.credentialId)}`" :disabled="disabled" :invalid="!!item.sshHostsCredentialId && !hostsUsable(item.sshHostsCredentialId)" searchable :data-testid="`git-hosts-${item.credentialId}`" @update:model-value="change(item.credentialId, 'sshHostsCredentialId', $event)"><template #leading><CredentialProviderLogo icon="host" /></template><template #option-leading="{ option }"><CredentialProviderLogo v-if="option.value" icon="host" /></template></AppSelect></div>
          </div>
        </details>
      </div>
    </div>
    <p v-else class="rounded-xl border border-dashed border-line px-4 py-6 text-center text-xs text-txt3">{{ copy('empty') }}</p>
    <p v-if="bindings.items.length && !bindings.items.some((row) => row.credentialId === bindings.preferredCredentialId)" role="alert" class="text-xs text-err">{{ copy('preferredMissing') }}</p>
    <p v-if="bindings.items.length" class="m-0 text-[11px] leading-5 text-txt3">{{ copy('allStaged') }}</p>
    <RouterLink to="/settings/credentials" class="inline-block text-[11px] text-accent-2">{{ copy('manage') }}</RouterLink>
  </section>
</template>

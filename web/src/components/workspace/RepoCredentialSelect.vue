<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSelect from '@/components/ui/AppSelect.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import { api, type CredentialItem } from '@/lib/api/api'
import { credentialFitsRepo, gitCredentialKind } from '@/lib/workspace/gitCredentials'
import { defaultGitCredentialHost, kindOfItem } from '@/lib/workspace/credentialKinds'

const props = withDefaults(
  defineProps<{
    repoUrl: string
    modelValue: string
    /** Preloaded credentials; omitted loads them from the workspace. */
    items?: CredentialItem[]
    disabled?: boolean
  }>(),
  { disabled: false },
)
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
const copy = (key: string) => t(`pages.projects.form.credential.${key}`)

const loaded = ref<CredentialItem[]>([])
const loading = ref(false)
const error = ref('')
const credentials = computed(() => props.items ?? loaded.value)

function where(item: CredentialItem): string {
  const id = gitCredentialKind(item)?.id
  if (id === 'ssh_key') return 'SSH'
  return (item.host || defaultGitCredentialHost(id || '')).replace(/^https?:\/\//, '').replace(/\/$/, '')
}

const fitting = computed(() => credentials.value.filter((item) => credentialFitsRepo(props.repoUrl, item)))
const options = computed(() => [
  { value: '', label: copy('none') },
  ...fitting.value.map((item) => ({ value: item.id, label: item.name, hint: where(item) })),
])
const current = computed(() => credentials.value.find((item) => item.id === props.modelValue))
/** A saved credential that was revoked, deleted or no longer matches the address. */
const mismatch = computed(
  () => !!props.modelValue && !loading.value && !error.value && !fitting.value.some((item) => item.id === props.modelValue),
)

function kind(id: string) {
  const item = credentials.value.find((row) => row.id === id)
  return item ? kindOfItem(item) : undefined
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    loaded.value = (await api.listCredentials()).items || []
  } catch (cause) {
    error.value = String((cause as Error)?.message || cause)
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  if (props.items === undefined) void load()
})
</script>

<template>
  <div class="space-y-1.5" data-testid="repo-credential-select">
    <AppSelect
      :model-value="modelValue"
      :options="options"
      :disabled="disabled || loading"
      :loading="loading"
      :invalid="mismatch"
      :placeholder="loading ? copy('loading') : copy('none')"
      :aria-label="copy('label')"
      searchable
      @update:model-value="emit('update:modelValue', $event)"
    >
      <template #option-leading="{ option }">
        <CredentialProviderLogo
          v-if="option.value"
          :provider="kind(option.value)?.logoProvider"
          :icon="kind(option.value)?.icon"
          match="provider"
        />
      </template>
    </AppSelect>
    <p v-if="error" role="alert" class="m-0 text-[11px] text-err">
      {{ error }} <button type="button" class="underline" @click="load">{{ copy('retry') }}</button>
    </p>
    <p v-else-if="mismatch" role="alert" class="m-0 text-[11px] text-err" data-testid="repo-credential-mismatch">
      {{ current ? copy('mismatch') : copy('missing') }}
    </p>
    <p v-else class="m-0 text-[11px] leading-5 text-txt3">
      {{ repoUrl.trim() && !fitting.length && !loading ? copy('noneFits') : copy('hint') }}
      <RouterLink to="/settings/credentials" class="text-accent-2">{{ copy('manage') }}</RouterLink>
    </p>
  </div>
</template>

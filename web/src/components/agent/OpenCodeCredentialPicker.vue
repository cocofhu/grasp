<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import OpenCodeProviderFields from '@/components/agent/OpenCodeProviderFields.vue'
import { api, type ProjectCredentialItem } from '@/lib/api/api'
import { loadOpenCodeProviders } from '@/lib/agent/openCodeCatalog'
import {
  DEFAULT_OPENCODE_PROVIDER,
  openCodeCustomBaseRequired,
  openCodeModelWithProvider,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'
import { useToast } from '@/lib/composables/useToast'

const props = defineProps<{
  mode: 'select' | 'manage'
  projectId: string
  /** When omitted, the picker loads project credentials itself. */
  items?: ProjectCredentialItem[]
  selectedId?: string
}>()

const emit = defineEmits<{
  'update:selectedId': [id: string]
  changed: []
}>()

const { t } = useI18n()
const toast = useToast()

const remoteItems = ref<ProjectCredentialItem[]>([])
const showAdd = ref(false)
const saving = ref(false)
const attempted = ref(false)
const replacingId = ref('')
const replaceValue = ref('')
const replaceAttempted = ref(false)
const clearingId = ref('')
const catalogTick = ref(0)

const form = reactive({
  name: '',
  provider: DEFAULT_OPENCODE_PROVIDER as OpenCodeProviderId,
  baseUrl: '',
  model: '',
  vision: false,
  apiKey: '',
})

const source = computed(() => (props.items === undefined ? remoteItems.value : props.items))
const rows = computed(() => source.value.filter(isModelVendor))
const baseRequired = computed(() => {
  catalogTick.value
  return openCodeCustomBaseRequired(form.provider, form.baseUrl)
})
const description = computed(() =>
  props.mode === 'select'
    ? t('pages.agentStudio.openCode.credentialDesc')
    : t('pages.projectDetail.projectCredentials.modelApiKeySubtitle'),
)

function isModelVendor(item: ProjectCredentialItem): boolean {
  return (
    (item.type || '').toLowerCase() === 'ai' &&
    (item.provider || '').toLowerCase() === 'opencode' &&
    (!item.source || item.source === 'project') &&
    !!item.configured
  )
}

function metaString(item: ProjectCredentialItem, key: string): string {
  const value = item.metadata?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

function vendorOf(item: ProjectCredentialItem): string {
  return metaString(item, 'provider')
}

function modelOf(item: ProjectCredentialItem): string {
  return metaString(item, 'model')
}

function abbrev(item: ProjectCredentialItem): string {
  const raw = vendorOf(item).replace(/[^a-z0-9]/gi, '').toUpperCase()
  return (raw || 'AI').slice(0, 2)
}

function resetForm() {
  form.name = ''
  form.provider = DEFAULT_OPENCODE_PROVIDER
  form.baseUrl = ''
  form.model = ''
  form.vision = false
  form.apiKey = ''
  attempted.value = false
}

function openAdd() {
  if (!props.projectId) return
  resetForm()
  showAdd.value = true
  replacingId.value = ''
}

function cancelAdd() {
  showAdd.value = false
  resetForm()
}

async function loadRemote() {
  if (props.items !== undefined || !props.projectId) {
    remoteItems.value = []
    return
  }
  try {
    const response = await api.getProjectCredentials(props.projectId)
    remoteItems.value = response.items || []
  } catch (e: unknown) {
    remoteItems.value = []
    toast.error(String((e as { message?: string })?.message || e))
  }
}

function choose(id: string) {
  if (props.mode !== 'select' || id === props.selectedId) return
  emit('update:selectedId', id)
}

async function saveAdd() {
  attempted.value = true
  if (
    !props.projectId ||
    saving.value ||
    !form.name.trim() ||
    !form.apiKey.trim() ||
    !form.model.trim() ||
    baseRequired.value
  ) {
    return
  }
  saving.value = true
  try {
    const created = await api.createProjectCredential(props.projectId, {
      type: 'ai',
      provider: 'opencode',
      name: form.name.trim(),
      envKey: 'GRASP_OPENCODE_API_KEY',
      value: form.apiKey,
      metadata: {
        provider: form.provider,
        baseUrl: form.baseUrl.trim(),
        model: openCodeModelWithProvider(form.model, form.provider),
        vision: form.vision,
      },
    })
    showAdd.value = false
    resetForm()
    if (props.items === undefined) await loadRemote()
    if (props.mode === 'select') emit('update:selectedId', created.id)
    emit('changed')
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    saving.value = false
  }
}

function openReplace(id: string) {
  replacingId.value = id
  replaceValue.value = ''
  replaceAttempted.value = false
  showAdd.value = false
}

function cancelReplace() {
  replacingId.value = ''
  replaceValue.value = ''
  replaceAttempted.value = false
}

async function saveReplace(id: string) {
  replaceAttempted.value = true
  const value = replaceValue.value.trim()
  if (!value || saving.value) return
  saving.value = true
  try {
    await api.putProjectCredential(props.projectId, id, { value })
    cancelReplace()
    if (props.items === undefined) await loadRemote()
    emit('changed')
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    saving.value = false
  }
}

async function clearRow(id: string) {
  if (clearingId.value) return
  clearingId.value = id
  try {
    await api.deleteProjectCredential(props.projectId, id)
    if (props.items === undefined) await loadRemote()
    emit('changed')
    toast.success(t('pages.projectDetail.projectCredentials.cleared'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    clearingId.value = ''
  }
}

onMounted(async () => {
  await loadOpenCodeProviders()
  catalogTick.value++
  await loadRemote()
})

watch(
  () => props.projectId,
  () => {
    showAdd.value = false
    cancelReplace()
    void loadRemote()
  },
)
</script>

<template>
  <div data-testid="project-credential-opencode" :data-mode="mode">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.openCode.credentialTitle') }}</div>
        <p class="mb-0 mt-1 text-[11px] leading-5 text-txt3">{{ description }}</p>
      </div>
      <AppButton
        size="sm"
        variant="outline"
        icon="plus"
        class="shrink-0"
        data-testid="opencode-credential-add"
        :disabled="!projectId || showAdd"
        @click="openAdd"
      >
        {{ t('pages.agentStudio.openCode.credentialAdd') }}
      </AppButton>
    </div>
    <p v-if="!projectId" class="mb-0 mt-2 text-[11px] text-txt3" data-testid="opencode-credential-need-project">
      {{ t('pages.agentStudio.openCode.credentialNeedProject') }}
    </p>

    <form
      v-if="showAdd"
      class="mt-3 space-y-3 rounded-lg border border-line bg-base p-3"
      data-testid="opencode-credential-form"
      @submit.prevent="saveAdd"
    >
      <label class="block">
        <span class="mb-1 block text-[12px] font-medium text-txt2">
          {{ t('pages.agentStudio.openCode.credentialName') }} <span class="text-err">*</span>
        </span>
        <input
          v-model="form.name"
          class="w-full rounded-md border bg-surface px-3 py-2 text-[12px] text-txt outline-none focus:border-accent"
          :class="attempted && !form.name.trim() ? 'border-err' : 'border-line'"
          data-testid="opencode-credential-name"
          autocomplete="off"
        />
        <p v-if="attempted && !form.name.trim()" class="mb-0 mt-1 text-[11px] text-err">
          {{ t('pages.agentStudio.openCode.credentialNameRequired') }}
        </p>
      </label>
      <OpenCodeProviderFields
        :provider="form.provider"
        :base-url="form.baseUrl"
        :model="form.model"
        :vision="form.vision"
        :require-base="attempted && baseRequired"
        :require-model="attempted && !form.model.trim()"
        :columns="mode === 'manage'"
        @update:provider="form.provider = $event"
        @update:base-url="form.baseUrl = $event"
        @update:model="form.model = $event"
        @update:vision="form.vision = $event"
      />
      <label class="block">
        <span class="mb-1 block text-[12px] font-medium text-txt2">
          {{ t('pages.agentStudio.openCode.credentialKey') }} <span class="text-err">*</span>
        </span>
        <input
          v-model="form.apiKey"
          type="password"
          class="w-full rounded-md border bg-surface px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
          :class="attempted && !form.apiKey.trim() ? 'border-err' : 'border-line'"
          data-testid="opencode-credential-key"
          autocomplete="new-password"
        />
        <p v-if="attempted && !form.apiKey.trim()" class="mb-0 mt-1 text-[11px] text-err">
          {{ t('pages.agentStudio.openCode.credentialKeyRequired') }}
        </p>
      </label>
      <div class="flex justify-end gap-2">
        <AppButton size="sm" variant="ghost" type="button" data-testid="opencode-credential-cancel" @click="cancelAdd">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton size="sm" variant="primary" type="submit" :loading="saving" data-testid="opencode-credential-save">
          {{ mode === 'select' ? t('pages.agentStudio.openCode.credentialSaveUse') : t('pages.agentStudio.openCode.credentialSave') }}
        </AppButton>
      </div>
    </form>

    <div
      v-if="!rows.length"
      class="mt-3 rounded-lg border border-dashed border-line px-3 py-3 text-[12px] text-txt3"
      data-testid="opencode-credential-empty"
    >
      {{ t('pages.agentStudio.openCode.credentialEmpty') }}
    </div>
    <div v-else class="mt-3 space-y-2">
      <div v-for="item in rows" :key="item.id" :data-testid="`opencode-credential-row-${item.id}`">
        <component
          :is="mode === 'select' ? 'button' : 'div'"
          :type="mode === 'select' ? 'button' : undefined"
          class="w-full rounded-lg border px-3 py-2.5 text-left transition"
          :class="mode === 'select' && selectedId === item.id ? 'border-accent bg-accent-dim text-txt' : 'border-line bg-base text-txt2'"
          @click="choose(item.id)"
        >
          <div class="flex items-start gap-3">
            <span
              class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border text-[10px] font-bold uppercase"
              :class="mode === 'select' && selectedId === item.id ? 'border-accent/40 bg-surface text-accent-2' : 'border-line bg-surface text-txt3'"
              aria-hidden="true"
            >
              {{ abbrev(item) }}
            </span>
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="truncate text-[12px] font-semibold text-txt">{{ item.name }}</span>
                <code class="shrink-0 font-mono text-[11px] text-txt3">{{ item.masked || '••••••••' }}</code>
              </div>
              <div class="mt-1 flex flex-wrap items-center justify-between gap-2">
                <span v-if="vendorOf(item) || modelOf(item)" class="text-[11px] text-txt3">
                  {{ t('pages.agentStudio.openCode.credentialVendorModel', { vendor: vendorOf(item), model: modelOf(item) }) }}
                </span>
                <span
                  v-if="mode === 'select' && selectedId === item.id"
                  class="text-[10px] font-semibold text-accent-2"
                  data-testid="opencode-credential-current"
                >
                  {{ t('pages.agentStudio.openCode.credentialCurrent') }}
                </span>
              </div>
            </div>
            <div v-if="mode === 'manage'" class="flex shrink-0 gap-1.5" @click.stop>
              <AppButton size="sm" variant="ghost" :data-testid="`opencode-credential-replace-${item.id}`" @click="openReplace(item.id)">
                {{ t('pages.agentStudio.openCode.credentialReplace') }}
              </AppButton>
              <AppButton
                size="sm"
                variant="danger"
                :loading="clearingId === item.id"
                :data-testid="`opencode-credential-clear-${item.id}`"
                @click="clearRow(item.id)"
              >
                {{ t('pages.agentStudio.openCode.credentialClear') }}
              </AppButton>
            </div>
          </div>
        </component>
        <form
          v-if="mode === 'manage' && replacingId === item.id"
          class="mt-2 rounded-lg border border-line bg-surface p-3"
          :data-testid="`opencode-credential-replace-form-${item.id}`"
          @submit.prevent="saveReplace(item.id)"
        >
          <p class="mb-2 mt-0 text-[11px] text-txt3">{{ t('pages.agentStudio.openCode.credentialReplaceHint') }}</p>
          <input
            v-model="replaceValue"
            type="password"
            class="w-full rounded-md border bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
            :class="replaceAttempted && !replaceValue.trim() ? 'border-err' : 'border-line'"
            data-testid="opencode-credential-replace-key"
            autocomplete="new-password"
          />
          <div class="mt-2 flex justify-end gap-2">
            <AppButton size="sm" variant="ghost" type="button" data-testid="opencode-credential-replace-cancel" @click="cancelReplace">
              {{ t('common.buttons.cancel') }}
            </AppButton>
            <AppButton size="sm" variant="primary" type="submit" :loading="saving" data-testid="opencode-credential-replace-save">
              {{ t('pages.agentStudio.openCode.credentialReplaceSave') }}
            </AppButton>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

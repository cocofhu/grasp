<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import OpenCodeProviderFields from '@/components/agent/OpenCodeProviderFields.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import { api, type CredentialItem } from '@/lib/api/api'
import { loadOpenCodeProviders } from '@/lib/agent/openCodeCatalog'
import {
  DEFAULT_OPENCODE_PROVIDER,
  openCodeCustomBaseRequired,
  openCodeModelWithProvider,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'
import { useToast } from '@/lib/composables/useToast'
import { conflictingAlias, isModelVendorCredential, kindById } from '@/lib/workspace/credentialKinds'

const props = withDefaults(defineProps<{
  mode: 'select' | 'manage'
  /** When omitted, the picker loads workspace credentials itself. */
  items?: CredentialItem[]
  selectedId?: string
  title?: string
  hint?: string
  /** False hides creating a credential. Omitted keeps the add button. */
  allowAdd?: boolean
  /** True shows a control that clears the current selection. */
  allowClear?: boolean
  /** Meta panels omit the hint paragraph under the picker title. */
  hideHint?: boolean
  /** Grouped management panels provide their own title and add control. */
  hideHeader?: boolean
}>(), {
  allowAdd: true,
  allowClear: false,
  hideHint: false,
  hideHeader: false,
})

const emit = defineEmits<{
  'update:selectedId': [id: string]
  changed: []
  add: []
}>()

const { t } = useI18n()
const toast = useToast()

const remoteItems = ref<CredentialItem[]>([])
const showAdd = ref(false)
const saving = ref(false)
const attempted = ref(false)
const aliasError = ref('')
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
const rows = computed(() => source.value.filter(isModelVendorCredential))
const baseRequired = computed(() => {
  void catalogTick.value
  return openCodeCustomBaseRequired(form.provider, form.baseUrl)
})
const description = computed(() => {
  if (props.hint) return props.hint
  return props.mode === 'select'
    ? t('pages.agentStudio.openCode.credentialDesc')
    : t('pages.workspace.credentials.modelApiKeySubtitle')
})
const heading = computed(() => props.title || t('pages.agentStudio.openCode.credentialTitle'))
const canAdd = computed(() => props.allowAdd !== false)

function metaString(item: CredentialItem, key: string): string {
  const value = item.metadata?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

function vendorOf(item: CredentialItem): string {
  return metaString(item, 'provider')
}

function modelOf(item: CredentialItem): string {
  return metaString(item, 'model')
}

function resetForm() {
  form.name = ''
  form.provider = DEFAULT_OPENCODE_PROVIDER
  form.baseUrl = ''
  form.model = ''
  form.vision = false
  form.apiKey = ''
  attempted.value = false
  aliasError.value = ''
}

function openAdd() {
  if (props.mode === 'manage') {
    emit('add')
    return
  }
  resetForm()
  showAdd.value = true
  replacingId.value = ''
}

function cancelAdd() {
  showAdd.value = false
  resetForm()
}

async function loadRemote() {
  if (props.items !== undefined) {
    remoteItems.value = []
    return
  }
  try {
    const response = await api.listCredentials()
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

function clearSelection() {
  if (!props.selectedId) return
  emit('update:selectedId', '')
}

async function saveAdd() {
  attempted.value = true
  aliasError.value = ''
  const opencodeKind = kindById('opencode')
  const taken = opencodeKind ? conflictingAlias(source.value, opencodeKind, form.name) : ''
  if (taken) {
    aliasError.value = t('pages.workspace.credentials.aliasTaken', { alias: taken })
    return
  }
  if (
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
    const created = await api.createCredential({
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
    toast.success(t('pages.workspace.credentials.saved'))
  } catch (e: unknown) {
    const err = e as { code?: string; alias?: string; message?: string }
    if (err.code === 'alias_taken') {
      aliasError.value = t('pages.workspace.credentials.aliasTaken', { alias: err.alias || form.name.trim() })
      return
    }
    toast.error(String(err.message || e))
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
    await api.putCredential(id, { value })
    cancelReplace()
    if (props.items === undefined) await loadRemote()
    emit('changed')
    toast.success(t('pages.workspace.credentials.saved'))
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
    await api.deleteCredential(id)
    if (props.items === undefined) await loadRemote()
    emit('changed')
    toast.success(t('pages.workspace.credentials.cleared'))
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
</script>

<template>
  <div data-testid="credential-opencode" :data-mode="mode">
    <div v-if="!hideHeader" class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="text-[12px] font-medium text-txt2">{{ heading }}</div>
        <p v-if="!hideHint" class="mb-0 mt-1 text-[11px] leading-5 text-txt3">{{ description }}</p>
      </div>
      <div class="flex shrink-0 gap-1.5">
        <AppButton
          v-if="allowClear && selectedId"
          size="sm"
          variant="ghost"
          data-testid="opencode-credential-clear-selection"
          @click="clearSelection"
        >
          {{ t('pages.workspace.credentials.pickClear') }}
        </AppButton>
        <AppButton
          v-if="canAdd"
          size="sm"
          variant="outline"
          icon="plus"
          class="shrink-0"
          data-testid="opencode-credential-add"
          :disabled="showAdd"
          @click="openAdd"
        >
          {{ t('pages.agentStudio.openCode.credentialAdd') }}
        </AppButton>
      </div>
    </div>

    <form
      v-if="showAdd"
      class="mt-3 space-y-3 border-y border-line/60 py-4"
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
          :placeholder="t('pages.agentStudio.openCode.credentialNamePlaceholder')"
        />
        <p v-if="aliasError" class="mb-0 mt-1 text-[11px] text-err" data-testid="opencode-credential-alias-error">
          {{ aliasError }}
        </p>
        <p v-else-if="attempted && !form.name.trim()" class="mb-0 mt-1 text-[11px] text-err">
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
      class="py-3 text-[12px] text-txt3"
      :class="{ 'mt-3': !hideHeader, 'credential-manage-empty': mode === 'manage' }"
      data-testid="opencode-credential-empty"
    >
      {{ t('pages.agentStudio.openCode.credentialEmpty') }}
    </div>
    <div
      v-else
      :class="mode === 'manage' ? ['credential-manage-list', { 'mt-3': !hideHeader }] : 'mt-3 divide-y divide-line/60'"
    >
      <div
        v-for="item in rows"
        :key="item.id"
        :class="{ 'credential-manage-row': mode === 'manage' }"
        :data-testid="`opencode-credential-row-${item.id}`"
      >
        <component
          :is="mode === 'select' ? 'button' : 'div'"
          :type="mode === 'select' ? 'button' : undefined"
          class="w-full text-left transition"
          :class="mode === 'manage' ? 'credential-manage-overview' : ['border-l-2 px-3 py-3', selectedId === item.id ? 'border-accent bg-accent-dim text-txt' : 'border-transparent text-txt2']"
          :aria-pressed="mode === 'select' ? selectedId === item.id : undefined"
          @click="choose(item.id)"
        >
          <div class="flex items-start gap-3" :class="{ 'credential-manage-content': mode === 'manage' }">
            <CredentialProviderLogo
              :provider="vendorOf(item) || 'opencode'"
              match="provider"
              :selected="mode === 'select' && selectedId === item.id"
              :configured="true"
            />
            <div class="min-w-0 flex-1" :class="{ 'credential-manage-identity': mode === 'manage' }">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span
                  class="text-[12px] font-semibold text-txt"
                  :class="mode === 'manage' ? 'credential-manage-name' : 'truncate'"
                  :title="item.name"
                >{{ item.name }}</span>
                <code v-if="mode === 'select'" class="shrink-0 font-mono text-[11px] text-txt3">{{ item.masked || '••••••••' }}</code>
              </div>
              <div class="mt-1 flex flex-wrap items-center justify-between gap-2" :class="{ 'credential-manage-model-line': mode === 'manage' }">
                <span
                  v-if="vendorOf(item) || modelOf(item)"
                  class="text-[11px] text-txt3"
                  :class="{ 'credential-manage-model': mode === 'manage' }"
                >
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
              <code v-if="mode === 'manage'" class="credential-manage-mask">{{ item.masked || '••••••••' }}</code>
            </div>
            <div v-if="mode === 'manage'" class="credential-manage-actions" @click.stop>
              <AppButton size="sm" variant="outline" :data-testid="`opencode-credential-replace-${item.id}`" @click="openReplace(item.id)">
                {{ t('pages.agentStudio.openCode.credentialReplace') }}
              </AppButton>
              <AppButton
                size="sm"
                variant="ghost"
                class="credential-manage-clear"
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
          class="credential-manage-replace"
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
          <div class="mt-2 flex flex-wrap justify-end gap-2">
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

<style scoped>
.credential-manage-list {
  display: grid;
  min-width: 0;
  border: 1px solid rgb(var(--c-line) / 0.7);
  border-radius: 10px;
  background: rgb(var(--c-surface));
  overflow: hidden;
  container-type: inline-size;
}

.credential-manage-row {
  min-width: 0;
}

.credential-manage-row + .credential-manage-row {
  border-top: 1px solid rgb(var(--c-line) / 0.7);
}

.credential-manage-overview {
  padding: 12px 16px;
}

.credential-manage-content {
  align-items: center;
  flex-wrap: wrap;
}

.credential-manage-identity {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr) minmax(0, 0.65fr);
  align-items: center;
  gap: 16px;
}

.credential-manage-model-line {
  margin-top: 0;
}

.credential-manage-name,
.credential-manage-model {
  min-width: 0;
  overflow-wrap: anywhere;
}

.credential-manage-mask {
  display: block;
  min-width: 0;
  color: rgb(var(--c-txt2));
  font-size: 11px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.credential-manage-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 4px;
  margin-left: auto;
}

.credential-manage-clear {
  color: rgb(var(--c-err));
}

.credential-manage-replace {
  padding: 14px;
  border-top: 1px solid rgb(var(--c-line) / 0.7);
  background: rgb(var(--c-surface));
}

.credential-manage-empty {
  padding: 20px 16px;
  border: 1px dashed rgb(var(--c-line));
  border-radius: 12px;
  text-align: center;
}

@container (max-width: 760px) {
  .credential-manage-identity {
    grid-template-columns: minmax(0, 1fr) minmax(0, 0.8fr);
    gap: 4px 12px;
  }

  .credential-manage-identity > :first-child {
    grid-column: 1 / -1;
  }
}

@container (max-width: 420px) {
  .credential-manage-content {
    align-items: flex-start;
  }

  .credential-manage-identity {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }

  .credential-manage-actions {
    flex-basis: 100%;
    justify-content: flex-start;
    margin-left: 44px;
  }
}
</style>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import OpenCodeProviderFields from '@/components/agent/OpenCodeProviderFields.vue'
import { api, type ProjectCredentialItem } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import { useToast } from '@/lib/composables/useToast'
import {
  DEFAULT_OPENCODE_PROVIDER,
  openCodeCustomBaseRequired,
  openCodeModelWithProvider,
  normalizeOpenCodeProvider,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'

const props = defineProps<{ projectId: string }>()

const { t } = useI18n()
const toast = useToast()

const loading = ref(true)
const loadError = ref('')
const items = ref<ProjectCredentialItem[]>([])
const drafts = reactive<Record<string, string>>({})
type OpenCodeDraft = {
  provider: OpenCodeProviderId
  baseUrl: string
  model: string
  vision: boolean
}
const openCodeDrafts = reactive<Record<string, OpenCodeDraft>>({})
const saving = reactive<Record<string, boolean>>({})
const clearing = reactive<Record<string, boolean>>({})
const showCreate = ref(false)
const creating = ref(false)
const createType = ref('custom')
const createProvider = ref('')
const createName = ref('')
const createTarget = ref('')
const createEnvKey = ref('')
const createFallbackEnvKey = ref('')
const createValue = ref('')

const credentialTypes = ['ai', 'git', 'ssh', 'mcp', 'custom'] as const
const createTargetMissing = computed(() =>
  createType.value === 'custom' && !createTarget.value.trim() && !createEnvKey.value.trim(),
)

const orderedItems = computed(() =>
  [...items.value].sort((a, b) => {
    // Put the model API key first so the primary setup path is visible without
    // making users scan past Git/SSH slots.
    const aModel = isOpenCodeModelCredential(a) ? 0 : 1
    const bModel = isOpenCodeModelCredential(b) ? 0 : 1
    if (aModel !== bModel) return aModel - bModel
    const aName = (a.name || a.provider || a.id).toLocaleLowerCase()
    const bName = (b.name || b.provider || b.id).toLocaleLowerCase()
    return aName.localeCompare(bName)
  }),
)

function itemLabel(item: ProjectCredentialItem): string {
  return item.name || item.provider || item.envKey || item.id
}

function itemTarget(item: ProjectCredentialItem): string {
  if (item.target) return item.target
  if (item.envKey) return item.envKey
  if (item.provider) return item.provider
  return item.type || item.kind || ''
}

function isOpenCodeModelCredential(item: ProjectCredentialItem): boolean {
  return (
    (item.type || item.kind || '').toLowerCase() === 'ai' &&
    (item.provider || '').toLowerCase() === 'opencode' &&
    (!item.source || item.source === 'project')
  )
}

function openCodeDraftFor(item: ProjectCredentialItem): OpenCodeDraft {
  const metadata = item.metadata || {}
  const provider = typeof metadata.provider === 'string' ? metadata.provider : ''
  const baseUrl = typeof metadata.baseUrl === 'string'
    ? metadata.baseUrl
    : typeof metadata.baseURL === 'string'
      ? metadata.baseURL
      : ''
  const model = typeof metadata.model === 'string' ? metadata.model : ''
  const vision = metadata.vision === true || metadata.vision === '1' || metadata.vision === 'true'
  return {
    provider: normalizeOpenCodeProvider(provider || DEFAULT_OPENCODE_PROVIDER),
    baseUrl,
    model,
    vision,
  }
}

function ensureOpenCodeDraft(item: ProjectCredentialItem) {
  if (isOpenCodeModelCredential(item) && !openCodeDrafts[item.id]) {
    openCodeDrafts[item.id] = openCodeDraftFor(item)
  }
}

function patchOpenCodeDraft(id: string, patch: Partial<OpenCodeDraft>) {
  const current = openCodeDrafts[id]
  if (!current) return
  Object.assign(current, patch)
}

function openCodeBaseRequired(fields: OpenCodeDraft | undefined): boolean {
  return fields ? openCodeCustomBaseRequired(fields.provider, fields.baseUrl) : false
}

function isMultiline(item: ProjectCredentialItem): boolean {
  const key = `${item.type || item.kind || ''} ${item.provider || ''} ${item.envKey || ''}`.toLowerCase()
  return key.includes('ssh') || key.includes('private') || key.includes('known_hosts')
}

function configuredText(item: ProjectCredentialItem): string {
  return item.configured ? t('pages.projectDetail.projectCredentials.configured') : t('pages.projectDetail.projectCredentials.notConfigured')
}

function updateItems(next: ProjectCredentialItem) {
  const i = items.value.findIndex((item) => item.id === next.id)
  if (i < 0) items.value = [...items.value, next]
  else items.value = items.value.map((item, index) => (index === i ? next : item))
  drafts[next.id] = ''
  delete openCodeDrafts[next.id]
  ensureOpenCodeDraft(next)
}

function setItems(next: ProjectCredentialItem[]) {
  items.value = next
  for (const key of Object.keys(drafts)) delete drafts[key]
  for (const key of Object.keys(openCodeDrafts)) delete openCodeDrafts[key]
  for (const item of next) drafts[item.id] = ''
  for (const item of next) ensureOpenCodeDraft(item)
}

function openCreate() {
  createType.value = 'custom'
  createProvider.value = ''
  createName.value = ''
  createTarget.value = ''
  createEnvKey.value = ''
  createFallbackEnvKey.value = ''
  createValue.value = ''
  showCreate.value = true
}

async function create() {
  if (!createName.value.trim() || !createValue.value.trim() || creating.value) return
  creating.value = true
  try {
    const created = await api.createProjectCredential(props.projectId, {
      type: createType.value,
      provider: createProvider.value.trim(),
      name: createName.value.trim(),
      target: createTarget.value.trim(),
      envKey: createEnvKey.value.trim(),
      fallbackEnvKey: createFallbackEnvKey.value.trim(),
      value: createValue.value,
    })
    updateItems(created)
    showCreate.value = false
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    creating.value = false
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const response = await api.getProjectCredentials(props.projectId)
    // Keep the client tolerant of an early backend response that returns the array directly.
    const next = Array.isArray(response) ? response : response?.items || []
    setItems(next)
  } catch (e: unknown) {
    loadError.value = String((e as { message?: string })?.message || e)
    setItems([])
  } finally {
    loading.value = false
  }
}

async function save(item: ProjectCredentialItem) {
  const value = drafts[item.id] || ''
  if (!value.trim() || saving[item.id]) return
  saving[item.id] = true
  try {
    const saved = await api.putProjectCredential(props.projectId, item.id, {
      type: item.type || item.kind,
      name: item.name,
      target: item.target,
      targetType: item.targetType,
      targetId: item.targetId,
      provider: item.provider,
      envKey: item.envKey,
      fallbackEnvKey: item.fallbackEnvKey,
      value,
      metadata: item.metadata,
      enabled: item.enabled,
    })
    updateItems(saved)
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    saving[item.id] = false
  }
}

async function saveOpenCode(item: ProjectCredentialItem) {
  const fields = openCodeDrafts[item.id]
  if (!fields || (!drafts[item.id]?.trim() && !item.configured) || !fields.model.trim() || openCodeBaseRequired(fields) || saving[item.id]) return
  saving[item.id] = true
  try {
    const saved = await api.putProjectCredential(props.projectId, item.id, {
      type: item.type || item.kind,
      name: item.name,
      target: item.target,
      targetType: item.targetType,
      targetId: item.targetId,
      // The row remains the OpenCode adapter slot. The selected vendor is kept
      // in metadata so one API key can carry both routing and authentication.
      provider: item.provider,
      envKey: item.envKey,
      fallbackEnvKey: item.fallbackEnvKey,
      value: drafts[item.id] || undefined,
      metadata: {
        ...(item.metadata || {}),
        provider: fields.provider.trim(),
        baseUrl: fields.baseUrl.trim(),
        model: openCodeModelWithProvider(fields.model, fields.provider),
        vision: fields.vision,
      },
      enabled: item.enabled,
    })
    updateItems(saved)
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    saving[item.id] = false
  }
}

async function clear(item: ProjectCredentialItem) {
  if (clearing[item.id] || !item.configured) return
  clearing[item.id] = true
  try {
    await api.deleteProjectCredential(props.projectId, item.id)
    updateItems({ ...item, configured: false, masked: '', revokedAt: undefined })
    toast.success(t('pages.projectDetail.projectCredentials.cleared'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    clearing[item.id] = false
  }
}

function retry() {
  void load()
}

watch(
  () => props.projectId,
  () => {
    void load()
  },
)

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="rounded-lg flex min-h-0 flex-1 flex-col overflow-hidden border border-b-0 border-line bg-surface shadow-[var(--shadow-card)]" data-testid="project-credentials-panel">
    <div v-if="loading" class="flex flex-1 items-center justify-center text-[13px] text-txt3">
      {{ t('common.loading.inProgress') }}
    </div>

    <div v-else-if="loadError" class="flex flex-1 flex-col items-center justify-center gap-2 px-4 text-center">
      <p class="text-[13px] text-err">{{ loadError }}</p>
      <AppButton size="sm" variant="outline" @click="retry">{{ t('common.buttons.retry') }}</AppButton>
    </div>

    <template v-else>
      <div class="flex shrink-0 items-start justify-between gap-3 border-b border-line bg-surface px-4 py-3.5 sm:px-6">
        <div>
          <h2 class="m-0 text-lg font-semibold text-txt">{{ t('pages.projectDetail.projectCredentials.title') }}</h2>
          <p class="mt-1 max-w-3xl text-[12px] leading-5 text-txt3">{{ t('pages.projectDetail.projectCredentials.subtitle') }}</p>
          <p class="mt-2 max-w-3xl rounded-md border border-info/30 bg-info/10 px-3 py-2 text-[11px] leading-5 text-info">
            {{ t('pages.projectDetail.projectCredentials.envFallbackHint') }}
          </p>
        </div>
        <AppButton variant="primary" size="sm" icon="plus" data-testid="project-credential-create" @click="openCreate">
          {{ t('pages.projectDetail.projectCredentials.create') }}
        </AppButton>
      </div>

      <div v-if="!orderedItems.length" class="flex flex-1 items-center justify-center px-6 py-12 text-center text-[13px] text-txt3">
        {{ t('pages.projectDetail.projectCredentials.empty') }}
      </div>
      <div v-else class="scroll-area min-h-0 flex-1 overflow-y-auto px-4 py-4 sm:px-6">
        <div class="mx-auto w-full max-w-5xl space-y-3">
          <article
            v-for="item in orderedItems"
            :key="item.id"
            class="rounded-lg border border-line bg-base/40"
            :class="isOpenCodeModelCredential(item) ? 'overflow-hidden' : 'p-4'"
            data-testid="project-credential-row"
          >
            <template v-if="isOpenCodeModelCredential(item)">
              <div class="flex flex-wrap items-start justify-between gap-3 border-b border-line bg-surface px-4 py-4 sm:px-5">
                <div class="flex min-w-0 items-start gap-3">
                  <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent-dim text-accent">
                    <Icon name="lock" :size="18" />
                  </span>
                  <div class="min-w-0">
                    <h3 class="m-0 text-base font-semibold text-txt">{{ t('pages.projectDetail.projectCredentials.modelApiKeyTitle') }}</h3>
                    <p class="mt-1 max-w-xl text-[12px] leading-5 text-txt3">{{ t('pages.projectDetail.projectCredentials.modelApiKeySubtitle') }}</p>
                  </div>
                </div>
                <span
                  class="rounded-full border px-2 py-0.5 text-[11px]"
                  :class="item.configured ? 'border-ok/40 bg-ok/10 text-ok' : 'border-line text-txt3'"
                  data-testid="project-credential-status"
                >
                  {{ configuredText(item) }}
                </span>
              </div>
              <div class="space-y-4 p-4 sm:p-5" data-testid="project-credential-opencode">
                <OpenCodeProviderFields
                  v-if="openCodeDrafts[item.id]"
                  :provider="openCodeDrafts[item.id].provider"
                  :base-url="openCodeDrafts[item.id].baseUrl"
                  :model="openCodeDrafts[item.id].model"
                  :vision="openCodeDrafts[item.id].vision"
                  :require-base="openCodeBaseRequired(openCodeDrafts[item.id])"
                  :require-model="!openCodeDrafts[item.id].model.trim()"
                  :columns="true"
                  @update:provider="patchOpenCodeDraft(item.id, { provider: $event })"
                  @update:base-url="patchOpenCodeDraft(item.id, { baseUrl: $event })"
                  @update:model="patchOpenCodeDraft(item.id, { model: $event })"
                  @update:vision="patchOpenCodeDraft(item.id, { vision: $event })"
                />
                <div v-if="item.masked" class="rounded-md border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt2" data-testid="project-credential-masked">
                  {{ item.masked }}
                </div>
                <label class="block">
                  <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                    {{ t('pages.projectDetail.projectCredentials.apiKeyLabel') }} <span class="text-err">*</span>
                  </span>
                  <input
                    v-model="drafts[item.id]"
                    type="password"
                    class="w-full rounded-md border border-line bg-surface px-3 py-2 font-mono text-[13px] text-txt outline-none focus:border-accent"
                    :placeholder="item.configured ? t('pages.projectDetail.projectCredentials.replacePlaceholder') : t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                    :data-testid="`project-credential-input-${item.id}`"
                    autocomplete="new-password"
                  />
                  <p class="mt-1.5 text-[11px] text-txt3">{{ t('pages.projectDetail.projectCredentials.apiKeyHint') }}</p>
                </label>
                <div class="flex flex-wrap justify-end gap-2">
                  <AppButton
                    size="sm"
                    variant="primary"
                    :disabled="(!drafts[item.id]?.trim() && !item.configured) || !openCodeDrafts[item.id]?.model.trim() || openCodeBaseRequired(openCodeDrafts[item.id]) || !!saving[item.id]"
                    :loading="!!saving[item.id]"
                    :data-testid="`project-credential-save-${item.id}`"
                    @click="saveOpenCode(item)"
                  >
                    {{ t('pages.projectDetail.projectCredentials.save') }}
                  </AppButton>
                  <AppButton
                    v-if="item.configured"
                    size="sm"
                    variant="danger"
                    :disabled="!!clearing[item.id]"
                    :loading="!!clearing[item.id]"
                    :data-testid="`project-credential-clear-${item.id}`"
                    @click="clear(item)"
                  >
                    {{ t('pages.projectDetail.projectCredentials.clear') }}
                  </AppButton>
                </div>
                <p v-if="item.updatedAt" class="text-[11px] text-txt3">
                  {{ t('pages.projectDetail.projectCredentials.updatedAt', { time: fmtTime(item.updatedAt) }) }}
                </p>
              </div>
            </template>

            <template v-else>
              <div class="flex flex-wrap items-start justify-between gap-3">
                <div class="min-w-0">
                  <h3 class="m-0 text-sm font-semibold text-txt">{{ itemLabel(item) }}</h3>
                  <p class="mt-1 font-mono text-[11px] text-txt3">{{ itemTarget(item) }}</p>
                </div>
                <span
                  class="rounded-full border px-2 py-0.5 text-[11px]"
                  :class="item.configured ? 'border-ok/40 bg-ok/10 text-ok' : 'border-line text-txt3'"
                  data-testid="project-credential-status"
                >
                  {{ configuredText(item) }}
                </span>
              </div>

              <div v-if="item.masked" class="mt-3 rounded-md border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt2" data-testid="project-credential-masked">
                {{ item.masked }}
              </div>

              <p v-if="item.source && item.source !== 'project'" class="mt-3 rounded-md border border-line bg-surface px-3 py-2 text-[11px] leading-5 text-txt3">
                {{ t('pages.projectDetail.projectCredentials.managedByAdapter', { source: item.source }) }}
              </p>
              <div v-else class="mt-3 flex flex-col gap-2 sm:flex-row sm:items-start">
                <textarea
                  v-if="isMultiline(item)"
                  v-model="drafts[item.id]"
                  rows="3"
                  class="min-h-[74px] min-w-0 flex-1 rounded-md border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
                  :placeholder="item.configured ? t('pages.projectDetail.projectCredentials.replacePlaceholder') : t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                  :data-testid="`project-credential-input-${item.id}`"
                  autocomplete="off"
                  style="-webkit-text-security: disc;"
                />
                <input
                  v-else
                  v-model="drafts[item.id]"
                  type="password"
                  class="min-w-0 flex-1 rounded-md border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
                  :placeholder="item.configured ? t('pages.projectDetail.projectCredentials.replacePlaceholder') : t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                  :data-testid="`project-credential-input-${item.id}`"
                  autocomplete="new-password"
                />
                <div class="flex shrink-0 gap-2">
                  <AppButton
                    size="sm"
                    variant="primary"
                    :disabled="!drafts[item.id]?.trim() || !!saving[item.id]"
                    :loading="!!saving[item.id]"
                    :data-testid="`project-credential-save-${item.id}`"
                    @click="save(item)"
                  >
                    {{ t('pages.projectDetail.projectCredentials.save') }}
                  </AppButton>
                  <AppButton
                    v-if="item.configured"
                    size="sm"
                    variant="danger"
                    :disabled="!!clearing[item.id]"
                    :loading="!!clearing[item.id]"
                    :data-testid="`project-credential-clear-${item.id}`"
                    @click="clear(item)"
                  >
                    {{ t('pages.projectDetail.projectCredentials.clear') }}
                  </AppButton>
                </div>
              </div>
              <p v-if="item.updatedAt" class="mt-2 text-[11px] text-txt3">
                {{ t('pages.projectDetail.projectCredentials.updatedAt', { time: fmtTime(item.updatedAt) }) }}
              </p>
            </template>
          </article>
        </div>
      </div>
    </template>

    <AppModal
      :open="showCreate"
      :title="t('pages.projectDetail.projectCredentials.createTitle')"
      :width="500"
      @close="!creating && (showCreate = false)"
    >
      <div class="space-y-3">
        <label class="block">
          <span class="label">{{ t('pages.projectDetail.projectCredentials.type') }}</span>
          <select v-model="createType" class="input w-full" data-testid="project-credential-create-type">
            <option v-for="kind in credentialTypes" :key="kind" :value="kind">{{ kind }}</option>
          </select>
        </label>
        <label class="block">
          <span class="label">{{ t('pages.projectDetail.projectCredentials.name') }}</span>
          <input v-model="createName" class="input w-full" data-testid="project-credential-create-name" autocomplete="off" />
        </label>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="block">
            <span class="label">{{ t('pages.projectDetail.projectCredentials.provider') }}</span>
            <input v-model="createProvider" class="input w-full" data-testid="project-credential-create-provider" autocomplete="off" />
          </label>
          <label class="block">
            <span class="label">{{ t('pages.projectDetail.projectCredentials.target') }}</span>
            <input v-model="createTarget" class="input w-full" data-testid="project-credential-create-target" autocomplete="off" />
          </label>
        </div>
        <label class="block">
          <span class="label">{{ t('pages.projectDetail.projectCredentials.envKey') }}</span>
          <input v-model="createEnvKey" class="input w-full font-mono" data-testid="project-credential-create-env" autocomplete="off" />
        </label>
        <label class="block">
          <span class="label">{{ t('pages.projectDetail.projectCredentials.fallbackEnvKey') }}</span>
          <input v-model="createFallbackEnvKey" class="input w-full font-mono" data-testid="project-credential-create-fallback-env" autocomplete="off" />
        </label>
        <label class="block">
          <span class="label">{{ t('pages.projectDetail.projectCredentials.value') }}</span>
          <textarea v-model="createValue" rows="3" class="input w-full font-mono" data-testid="project-credential-create-value" autocomplete="new-password" style="-webkit-text-security: disc;" />
        </label>
        <div class="flex justify-end gap-2 pt-1">
          <AppButton variant="ghost" :disabled="creating" @click="showCreate = false">{{ t('common.buttons.cancel') }}</AppButton>
          <AppButton variant="primary" :disabled="creating || !createName.trim() || !createValue.trim() || createTargetMissing" :loading="creating" data-testid="project-credential-create-submit" @click="create">
            {{ t('pages.projectDetail.projectCredentials.create') }}
          </AppButton>
        </div>
      </div>
    </AppModal>
  </div>
</template>

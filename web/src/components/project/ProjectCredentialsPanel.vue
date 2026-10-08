<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import OpenCodeCredentialPicker from '@/components/agent/OpenCodeCredentialPicker.vue'
import OpenCodeProviderFields from '@/components/agent/OpenCodeProviderFields.vue'
import CredentialProviderLogo from '@/components/project/CredentialProviderLogo.vue'
import { api, type ProjectCredentialItem } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import { useToast } from '@/lib/composables/useToast'
import {
  CREDENTIAL_KINDS,
  aliasKey,
  conflictingAlias,
  kindById,
  kindOfItem,
  type CredentialKindId,
} from '@/lib/project/credentialKinds'
import {
  DEFAULT_OPENCODE_PROVIDER,
  openCodeCustomBaseRequired,
  openCodeModelWithProvider,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'

const props = defineProps<{ projectId: string }>()

const { t } = useI18n()
const toast = useToast()

const loading = ref(true)
const loadError = ref('')
const items = ref<ProjectCredentialItem[]>([])
const drafts = reactive<Record<string, string>>({})
const saving = reactive<Record<string, boolean>>({})
const clearing = reactive<Record<string, boolean>>({})
const showCreate = ref(false)
const creating = ref(false)
const createStep = ref<1 | 2>(1)
const createKind = ref<CredentialKindId | ''>('')
const createAlias = ref('')
const createSecret = ref('')
const revealSecret = ref(false)
const aliasError = ref('')
const createAttempted = ref(false)
const stepMotion = ref<'forward' | 'back'>('forward')
const stepLock = ref(false)
const stepPane = ref<HTMLElement | null>(null)
const paneHeight = ref(0)
const STEP_LOCK_MS = 200
let stepTimer = 0
const modelForm = reactive({
  provider: DEFAULT_OPENCODE_PROVIDER as OpenCodeProviderId,
  baseUrl: '',
  model: '',
  vision: false,
})

const credentialTypeKeys = {
  ai: 'pages.projectDetail.projectCredentials.typeAi',
  git: 'pages.projectDetail.projectCredentials.typeGit',
  ssh: 'pages.projectDetail.projectCredentials.typeSsh',
  mcp: 'pages.projectDetail.projectCredentials.typeMcp',
  custom: 'pages.projectDetail.projectCredentials.typeCustom',
} as const
const selectedKind = computed(() => (createKind.value ? kindById(createKind.value) : undefined))
const modelBaseRequired = computed(() => openCodeCustomBaseRequired(modelForm.provider, modelForm.baseUrl))
const secretReady = computed(() => {
  const kind = selectedKind.value
  if (!kind) return false
  if (kind.secret === 'model') {
    return Boolean(createSecret.value.trim() && modelForm.model.trim() && !modelBaseRequired.value)
  }
  return Boolean(createSecret.value.trim())
})

type CredentialGroup = 'ai' | 'git' | 'ssh' | 'other'

function groupFor(item: ProjectCredentialItem): CredentialGroup {
  const kind = (item.type || '').toLowerCase()
  if (kind === 'ai') return 'ai'
  if (kind === 'git') return 'git'
  if (kind === 'ssh') return 'ssh'
  return 'other'
}

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

const configuredCount = computed(() => orderedItems.value.filter((item) => item.configured).length)
const apiKeyCount = computed(() => orderedItems.value.filter((item) => groupFor(item) === 'ai').length)
const credentialGroups = computed(() => {
  const buckets = new Map<string, ProjectCredentialItem[]>()
  const other: ProjectCredentialItem[] = []
  for (const item of orderedItems.value) {
    const kind = kindOfItem(item)
    if (!kind) {
      other.push(item)
      continue
    }
    buckets.set(kind.id, [...(buckets.get(kind.id) || []), item])
  }
  const groups = CREDENTIAL_KINDS
    .filter((kind) => (buckets.get(kind.id) || []).length > 0)
    .map((kind) => ({ id: kind.id, items: buckets.get(kind.id) || [] }))
  if (other.length) groups.push({ id: 'other', items: other })
  return groups
})

function groupTitle(group: string): string {
  if (group === 'other') return t('pages.projectDetail.projectCredentials.groups.other.title')
  return t(`pages.projectDetail.projectCredentials.kinds.${group}`)
}

function groupHint(group: string): string {
  const kind = kindById(group)
  if (!kind) return t('pages.projectDetail.projectCredentials.groups.other.hint')
  if (kind.type === 'git') return t('pages.projectDetail.projectCredentials.groups.git.hint')
  if (kind.type === 'ssh') return t('pages.projectDetail.projectCredentials.groups.ssh.hint')
  return t('pages.projectDetail.projectCredentials.groups.ai.hint')
}

function itemLabel(item: ProjectCredentialItem): string {
  return item.name || item.provider || item.id
}

function isOpenCodeModelCredential(item: ProjectCredentialItem): boolean {
  return (
    (item.type || '').toLowerCase() === 'ai' &&
    (item.provider || '').toLowerCase() === 'opencode' &&
    (!item.source || item.source === 'project')
  )
}

function isCodexLogin(item: ProjectCredentialItem): boolean {
  const provider = (item.provider || '').toLowerCase()
  const key = (item.envKey || '').toUpperCase()
  return provider === 'codex' || key === 'GRASP_CODEX_AUTH_JSON'
}

function isMultiline(item: ProjectCredentialItem): boolean {
  const key = `${item.type || ''} ${item.provider || ''} ${item.envKey || ''}`.toLowerCase()
  return isCodexLogin(item) || key.includes('ssh') || key.includes('private') || key.includes('known_hosts')
}

function configuredText(item: ProjectCredentialItem): string {
  return item.configured ? t('pages.projectDetail.projectCredentials.configured') : t('pages.projectDetail.projectCredentials.notConfigured')
}

function typeLabel(item: ProjectCredentialItem): string {
  const kind = kindOfItem(item)
  if (kind) return t(`pages.projectDetail.projectCredentials.kinds.${kind.id}`)
  const key = (item.type || 'custom') as keyof typeof credentialTypeKeys
  return t(credentialTypeKeys[key] || credentialTypeKeys.custom)
}

function vendorOf(item: ProjectCredentialItem): string {
  const value = item.metadata?.provider
  return typeof value === 'string' ? value.trim() : ''
}

function updateItems(next: ProjectCredentialItem) {
  const i = items.value.findIndex((item) => item.id === next.id)
  if (i < 0) items.value = [...items.value, next]
  else items.value = items.value.map((item, index) => (index === i ? next : item))
  drafts[next.id] = ''
}

function setItems(next: ProjectCredentialItem[]) {
  items.value = next
  for (const key of Object.keys(drafts)) delete drafts[key]
  for (const item of next) drafts[item.id] = ''
}

function resetModelForm() {
  modelForm.provider = DEFAULT_OPENCODE_PROVIDER
  modelForm.baseUrl = ''
  modelForm.model = ''
  modelForm.vision = false
}

function clearSecrets() {
  createSecret.value = ''
  revealSecret.value = false
  resetModelForm()
}

function releaseStepLock() {
  window.clearTimeout(stepTimer)
  stepLock.value = false
}

function lockStep() {
  stepLock.value = true
  window.clearTimeout(stepTimer)
  stepTimer = window.setTimeout(() => {
    stepLock.value = false
  }, STEP_LOCK_MS)
}

async function measurePane() {
  await nextTick()
  if (stepPane.value) paneHeight.value = stepPane.value.offsetHeight
}

function openCreate() {
  releaseStepLock()
  createStep.value = 1
  createKind.value = ''
  createAlias.value = ''
  aliasError.value = ''
  createAttempted.value = false
  stepMotion.value = 'forward'
  paneHeight.value = 0
  clearSecrets()
  showCreate.value = true
  void measurePane()
}

function selectKind(id: CredentialKindId) {
  if (stepLock.value || creating.value || createStep.value !== 1) return
  if (createKind.value !== id) clearSecrets()
  createKind.value = id
}

function goNext() {
  if (stepLock.value || creating.value || createStep.value !== 1 || !createKind.value) return
  stepMotion.value = 'forward'
  createAttempted.value = false
  createStep.value = 2
  lockStep()
  void measurePane()
}

function goBack() {
  if (stepLock.value || creating.value || createStep.value !== 2) return
  stepMotion.value = 'back'
  aliasError.value = ''
  createStep.value = 1
  lockStep()
  void measurePane()
}

function secretLabel(kindId: CredentialKindId): string {
  const kind = kindById(kindId)
  if (!kind) return t('pages.projectDetail.projectCredentials.value')
  if (kind.secret === 'loginFile') return t('pages.projectDetail.projectCredentials.secretLoginFile')
  if (kind.id === 'ssh_key') return t('pages.projectDetail.projectCredentials.secretPrivateKey')
  if (kind.id === 'ssh_hosts') return t('pages.projectDetail.projectCredentials.secretKnownHosts')
  return t('pages.projectDetail.projectCredentials.secretApiKey')
}

async function create() {
  if (stepLock.value || creating.value || createStep.value !== 2) return
  const kind = selectedKind.value
  const alias = createAlias.value.trim()
  if (!kind || !aliasKey(alias)) {
    aliasError.value = t('pages.projectDetail.projectCredentials.aliasRequired')
    return
  }
  const taken = conflictingAlias(items.value, kind, alias)
  if (taken) {
    aliasError.value = t('pages.projectDetail.projectCredentials.aliasTaken', { alias: taken })
    return
  }
  createAttempted.value = true
  if (!secretReady.value) return
  aliasError.value = ''
  creating.value = true
  try {
    const created = await api.createProjectCredential(props.projectId, {
      type: kind.type,
      provider: kind.provider,
      name: alias,
      envKey: kind.envKey,
      value: createSecret.value,
      ...(kind.secret === 'model'
        ? {
            metadata: {
              provider: modelForm.provider,
              baseUrl: modelForm.baseUrl.trim(),
              model: openCodeModelWithProvider(modelForm.model, modelForm.provider),
              vision: modelForm.vision,
            },
          }
        : {}),
    })
    updateItems(created)
    showCreate.value = false
    toast.success(t('pages.projectDetail.projectCredentials.saved'))
  } catch (e: unknown) {
    const err = e as { code?: string; alias?: string; message?: string }
    if (err.code === 'alias_taken') {
      aliasError.value = t('pages.projectDetail.projectCredentials.aliasTaken', { alias: err.alias || alias })
      return
    }
    toast.error(String(err.message || e))
  } finally {
    creating.value = false
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const response = await api.getProjectCredentials(props.projectId)
    setItems(response.items)
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
      type: item.type,
      name: item.name,
      target: item.target,
      targetType: item.targetType,
      targetId: item.targetId,
      provider: item.provider,
      envKey: item.envKey,
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

onUnmounted(() => {
  window.clearTimeout(stepTimer)
})
</script>

<template>
  <div
    class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-b-0 border-line bg-surface shadow-[var(--shadow-card)]"
    data-testid="project-credentials-panel"
  >
    <div v-if="loading" class="flex flex-1 items-center justify-center text-[13px] text-txt3">
      {{ t('common.loading.inProgress') }}
    </div>

    <div v-else-if="loadError" class="flex flex-1 flex-col items-center justify-center gap-2 px-4 text-center">
      <p class="text-[13px] text-err">{{ loadError }}</p>
      <AppButton size="sm" variant="outline" @click="retry">{{ t('common.buttons.retry') }}</AppButton>
    </div>

    <template v-else>
      <header class="flex shrink-0 flex-col gap-4 border-b border-line bg-surface px-4 py-5 sm:flex-row sm:items-start sm:justify-between sm:px-6 lg:px-8">
        <div class="min-w-0 max-w-3xl">
          <div class="flex items-center gap-2">
            <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-accent/35 bg-accent-dim text-accent-2">
              <Icon name="lock" :size="16" aria-hidden="true" />
            </span>
            <div>
              <p class="m-0 text-[11px] font-semibold uppercase tracking-[0.14em] text-accent-2">{{ t('pages.projectDetail.projectCredentials.eyebrow') }}</p>
              <h2 class="m-0 mt-0.5 text-lg font-semibold text-txt">{{ t('pages.projectDetail.projectCredentials.title') }}</h2>
            </div>
          </div>
          <p class="mt-3 max-w-2xl text-[13px] leading-6 text-txt2">{{ t('pages.projectDetail.projectCredentials.subtitle') }}</p>
        </div>
        <AppButton
          variant="primary"
          size="md"
          icon="plus"
          class="shrink-0 self-start sm:mt-1"
          data-testid="project-credential-create"
          @click="openCreate"
        >
          {{ t('pages.projectDetail.projectCredentials.create') }}
        </AppButton>
      </header>

      <div v-if="!orderedItems.length" class="flex flex-1 items-center justify-center px-6 py-12 text-center text-[13px] text-txt3">
        {{ t('pages.projectDetail.projectCredentials.empty') }}
      </div>
      <div v-else class="scroll-area min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6 lg:px-8">
        <div class="w-full space-y-6">
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-3" data-testid="project-credentials-summary">
            <div class="rounded-lg border border-line bg-base/45 px-4 py-3">
              <p class="m-0 text-[11px] font-medium uppercase tracking-[0.12em] text-txt3">{{ t('pages.projectDetail.projectCredentials.summary.total') }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums text-txt">{{ orderedItems.length }}</p>
            </div>
            <div class="rounded-lg border border-line bg-base/45 px-4 py-3">
              <p class="m-0 text-[11px] font-medium uppercase tracking-[0.12em] text-txt3">{{ t('pages.projectDetail.projectCredentials.summary.configured') }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums text-ok">{{ configuredCount }}<span class="ml-1 text-xs font-normal text-txt3">/ {{ orderedItems.length }}</span></p>
            </div>
            <div class="rounded-lg border border-accent/25 bg-accent-dim/35 px-4 py-3">
              <p class="m-0 text-[11px] font-medium uppercase tracking-[0.12em] text-accent-2">{{ t('pages.projectDetail.projectCredentials.summary.apiKeys') }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums text-txt">{{ apiKeyCount }}</p>
            </div>
          </div>

          <section
            v-for="group in credentialGroups"
            :key="group.id"
            :data-testid="`project-credential-group-${group.id}`"
          >
            <div class="mb-3 flex items-end justify-between gap-3 border-b border-line pb-2">
              <div class="min-w-0">
                <h3 class="m-0 text-sm font-semibold text-txt">{{ groupTitle(group.id) }}</h3>
                <p class="m-0 mt-1 text-[11px] leading-5 text-txt3">{{ groupHint(group.id) }}</p>
              </div>
              <span class="shrink-0 rounded-full border border-line bg-base px-2 py-0.5 text-[11px] tabular-nums text-txt3">{{ group.items.length }}</span>
            </div>

            <div
              v-if="group.id === 'opencode'"
              class="mb-3 rounded-xl border border-line bg-base/35 p-4 sm:p-5 lg:col-span-2 2xl:col-span-3"
            >
              <OpenCodeCredentialPicker
                mode="manage"
                :project-id="projectId"
                :items="items"
                @add="openCreate"
                @changed="load"
              />
            </div>

            <div class="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
              <template v-for="item in group.items" :key="item.id">
                <article
                  v-if="!isOpenCodeModelCredential(item)"
                  class="min-w-0 rounded-xl border border-line bg-base/35 p-4 transition-[border-color,background-color] duration-[160ms] ease-[cubic-bezier(0.16,1,0.3,1)] hover:border-line-strong hover:bg-base/60"
                  data-testid="project-credential-row"
                  :data-credential-kind="kindOfItem(item)?.id || 'other'"
                >
                  <div class="flex min-w-0 items-start gap-3">
                    <CredentialProviderLogo
                      :provider="isOpenCodeModelCredential(item) ? (vendorOf(item) || 'opencode') : (kindOfItem(item)?.logoProvider || item.provider)"
                      :icon="kindOfItem(item)?.icon"
                      match="provider"
                      :configured="item.configured"
                    />
                    <div class="min-w-0 flex-1">
                      <div class="flex flex-wrap items-start justify-between gap-2">
                        <div class="min-w-0">
                          <h4 class="m-0 truncate text-sm font-semibold text-txt" :title="itemLabel(item)" data-testid="project-credential-alias">{{ itemLabel(item) }}</h4>
                          <p class="m-0 mt-1 text-[10px] font-medium uppercase tracking-[0.1em] text-txt3" data-testid="project-credential-kind">{{ typeLabel(item) }}</p>
                        </div>
                        <span
                          class="shrink-0 rounded-full border px-2 py-0.5 text-[11px]"
                          :class="item.configured ? 'border-ok/40 bg-ok/10 text-ok' : 'border-line text-txt3'"
                          data-testid="project-credential-status"
                        >
                          {{ configuredText(item) }}
                        </span>
                      </div>
                    </div>
                  </div>

                  <div v-if="item.masked" class="mt-3 flex min-w-0 items-center justify-between gap-2 rounded-lg border border-line bg-surface px-3 py-2" data-testid="project-credential-masked">
                    <code class="min-w-0 truncate font-mono text-[12px] text-txt2">{{ item.masked }}</code>
                    <span class="shrink-0 text-[10px] tracking-[0.08em] text-txt3">{{ t('pages.projectDetail.projectCredentials.writeOnly') }}</span>
                  </div>

                  <p v-if="item.source && item.source !== 'project'" class="mt-3 rounded-lg border border-line bg-surface px-3 py-2 text-[11px] leading-5 text-txt3">
                    {{ t('pages.projectDetail.projectCredentials.managedByAdapter', { source: item.source }) }}
                  </p>
                  <div v-else class="mt-3 flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start">
                    <textarea
                      v-if="isMultiline(item)"
                      v-model="drafts[item.id]"
                      :rows="isCodexLogin(item) ? 8 : 2"
                      class="min-h-[68px] min-w-0 flex-1 resize-y rounded-lg border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt outline-none transition placeholder:text-txt3 focus:border-accent focus:ring-2 focus:ring-accent/15"
                      :placeholder="item.configured ? t('pages.projectDetail.projectCredentials.replacePlaceholder') : t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                      :data-testid="`project-credential-input-${item.id}`"
                      autocomplete="off"
                      style="-webkit-text-security: disc;"
                    />
                    <input
                      v-else
                      v-model="drafts[item.id]"
                      type="password"
                      class="min-w-0 flex-1 rounded-lg border border-line bg-surface px-3 py-2 font-mono text-[12px] text-txt outline-none transition placeholder:text-txt3 focus:border-accent focus:ring-2 focus:ring-accent/15"
                      :placeholder="item.configured ? t('pages.projectDetail.projectCredentials.replacePlaceholder') : t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                      :data-testid="`project-credential-input-${item.id}`"
                      autocomplete="new-password"
                    />
                    <div class="flex shrink-0 gap-2 sm:flex-col">
                      <AppButton
                        size="sm"
                        variant="primary"
                        class="flex-1 sm:flex-none"
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
                        class="flex-1 sm:flex-none"
                        :disabled="!!clearing[item.id]"
                        :loading="!!clearing[item.id]"
                        :data-testid="`project-credential-clear-${item.id}`"
                        @click="clear(item)"
                      >
                        {{ t('pages.projectDetail.projectCredentials.clear') }}
                      </AppButton>
                    </div>
                  </div>
                  <p v-if="item.updatedAt" class="m-0 mt-2 text-[10px] text-txt3">
                    {{ t('pages.projectDetail.projectCredentials.updatedAt', { time: fmtTime(item.updatedAt) }) }}
                  </p>
                </article>
              </template>
            </div>
          </section>
        </div>
      </div>
    </template>

    <AppModal
      :open="showCreate"
      :title="t('pages.projectDetail.projectCredentials.createTitle')"
      :width="640"
      :close-on-esc="!creating"
      @close="!creating && (showCreate = false)"
    >
      <div
        class="credential-step-shell"
        data-testid="project-credential-create-form"
        :data-credential-step="createStep"
        :data-step-motion="stepMotion"
        :data-step-locked="stepLock ? 'true' : 'false'"
        :style="paneHeight ? { height: `${paneHeight}px` } : undefined"
      >
        <Transition :name="stepMotion === 'forward' ? 'step-forward' : 'step-back'" @enter="measurePane">
          <div v-if="createStep === 1" key="kind" ref="stepPane" class="credential-step-pane">
            <p class="m-0 text-[12px] leading-5 text-txt3">{{ t('pages.projectDetail.projectCredentials.stepKindHint') }}</p>
            <div class="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
              <button
                v-for="kind in CREDENTIAL_KINDS"
                :key="kind.id"
                type="button"
                class="flex items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition-[border-color,background-color] duration-[160ms] ease-[cubic-bezier(0.16,1,0.3,1)]"
                :class="createKind === kind.id ? 'border-accent bg-accent-dim' : 'border-line bg-base hover:border-line-strong hover:bg-base/80'"
                :data-testid="`credential-kind-${kind.id}`"
                :aria-pressed="createKind === kind.id"
                @click="selectKind(kind.id)"
              >
                <CredentialProviderLogo :provider="kind.logoProvider" :icon="kind.icon" match="provider" :selected="createKind === kind.id" />
                <span class="min-w-0 truncate text-[13px] font-semibold text-txt">{{ t(`pages.projectDetail.projectCredentials.kinds.${kind.id}`) }}</span>
              </button>
            </div>
          </div>
          <div v-else key="secret" ref="stepPane" class="credential-step-pane space-y-4">
            <label class="block">
              <span class="label">{{ t('pages.projectDetail.projectCredentials.alias') }}</span>
              <input
                v-model="createAlias"
                class="input w-full"
                data-testid="project-credential-create-alias"
                autocomplete="off"
                :placeholder="t('pages.projectDetail.projectCredentials.aliasPlaceholder')"
                @input="aliasError = ''"
              />
              <p v-if="aliasError" class="mb-0 mt-1 text-[12px] text-err" data-testid="project-credential-alias-error">{{ aliasError }}</p>
            </label>
            <OpenCodeProviderFields
              v-if="selectedKind?.secret === 'model'"
              :provider="modelForm.provider"
              :base-url="modelForm.baseUrl"
              :model="modelForm.model"
              :vision="modelForm.vision"
              :require-base="createAttempted && modelBaseRequired"
              :require-model="createAttempted && !modelForm.model.trim()"
              @update:provider="modelForm.provider = $event"
              @update:base-url="modelForm.baseUrl = $event"
              @update:model="modelForm.model = $event"
              @update:vision="modelForm.vision = $event"
            />
            <label class="block">
              <span class="label flex items-center justify-between gap-2">
                <span>{{ selectedKind ? secretLabel(selectedKind.id) : t('pages.projectDetail.projectCredentials.value') }}</span>
                <button type="button" class="text-[11px] font-medium text-accent-2" data-testid="project-credential-secret-toggle" @click="revealSecret = !revealSecret">
                  {{ revealSecret ? t('pages.projectDetail.projectCredentials.hideSecret') : t('pages.projectDetail.projectCredentials.showSecret') }}
                </button>
              </span>
              <textarea
                v-if="selectedKind && (selectedKind.secret === 'multiline' || selectedKind.secret === 'loginFile')"
                v-model="createSecret"
                rows="6"
                class="input min-h-[120px] w-full resize-y font-mono"
                data-testid="project-credential-create-value"
                autocomplete="off"
                :placeholder="t('pages.projectDetail.projectCredentials.valuePlaceholder')"
                :style="revealSecret ? undefined : { '-webkit-text-security': 'disc' }"
              />
              <input
                v-else
                v-model="createSecret"
                :type="revealSecret ? 'text' : 'password'"
                class="input w-full font-mono"
                data-testid="project-credential-create-value"
                autocomplete="new-password"
                :placeholder="t('pages.projectDetail.projectCredentials.valuePlaceholder')"
              />
            </label>
          </div>
        </Transition>
      </div>
      <template #footer>
        <AppButton v-if="createStep === 1" variant="ghost" :disabled="creating" @click="showCreate = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton
          v-if="createStep === 1"
          variant="primary"
          :disabled="stepLock || !createKind"
          data-testid="project-credential-create-next"
          @click="goNext"
        >
          {{ t('pages.projectDetail.projectCredentials.next') }}
        </AppButton>
        <AppButton
          v-if="createStep === 2"
          variant="ghost"
          :disabled="creating || stepLock"
          data-testid="project-credential-create-back"
          @click="goBack"
        >
          {{ t('pages.projectDetail.projectCredentials.back') }}
        </AppButton>
        <AppButton
          v-if="createStep === 2"
          variant="primary"
          :disabled="creating || stepLock || !createAlias.trim() || !secretReady"
          :loading="creating"
          data-testid="project-credential-create-submit"
          @click="create"
        >
          {{ t('pages.projectDetail.projectCredentials.create') }}
        </AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.credential-step-shell {
  position: relative;
  overflow: hidden;
  transition: height 200ms cubic-bezier(0.16, 1, 0.3, 1);
}
.credential-step-pane {
  width: 100%;
}
.step-forward-enter-active,
.step-back-enter-active {
  transition:
    transform 200ms cubic-bezier(0.16, 1, 0.3, 1),
    opacity 200ms cubic-bezier(0.16, 1, 0.3, 1);
}
.step-forward-leave-active,
.step-back-leave-active {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  transition:
    transform 180ms cubic-bezier(0.16, 1, 0.3, 1),
    opacity 180ms cubic-bezier(0.16, 1, 0.3, 1);
}
.step-forward-enter-from,
.step-back-leave-to {
  opacity: 0;
  transform: translateX(28px);
}
.step-forward-leave-to,
.step-back-enter-from {
  opacity: 0;
  transform: translateX(-28px);
}
</style>

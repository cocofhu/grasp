<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api, type BrandSettings, type DashboardStats, type SandboxView, type SettingItem } from '@/lib/api/api'
import { useAuth } from '@/lib/composables/useAuth'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import { isIntegrationsQuery } from '@/data/settingsNav'
import { normalizeUnknownModelDisplayNameInput } from '@/lib/run/tokenUsage'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import IntegrationsPanel from '@/components/settings/IntegrationsPanel.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import { setBrandSettings, setExecutionMode } from '@/lib/composables/useBrandSettings'

const { t } = useI18n()
const route = useRoute()
const { user } = useAuth()
const { workspace, loading: workspaceLoading, error: workspaceError, ensureLoaded } = useWorkspaceContext()

const isAdmin = computed(() => !!user.value?.isAdmin)

const executionMode = ref<'worktree' | 'local' | 'sandbox'>('sandbox')
const worktreeMode = computed(() => executionMode.value === 'worktree' || executionMode.value === 'local')
const executionLabel = computed(() =>
  worktreeMode.value ? t('pages.settings.execution.worktree') : t('pages.settings.execution.sandbox'),
)
const executionHelp = computed(() =>
  worktreeMode.value ? t('pages.settings.execution.worktreeHelp') : t('pages.settings.execution.sandboxHelp'),
)

const groups = computed(() => {
  const all = [
    {
      id: 'concurrency',
      title: t('pages.settings.groups.concurrency.title'),
      desc: t('pages.settings.groups.concurrency.desc'),
      keys: ['max_concurrent_runs'],
    },
    {
      id: 'recycle',
      title: t('pages.settings.groups.recycle.title'),
      desc: t('pages.settings.groups.recycle.desc'),
      keys: ['run_sandbox_ttl_minutes', 'run_sandbox_max_retention_hours', 'test_sandbox_ttl_minutes'],
    },
    {
      id: 'capacity',
      title: t('pages.settings.groups.capacity.title'),
      desc: t('pages.settings.groups.capacity.desc'),
      keys: ['max_test_sandboxes'],
    },
    {
      id: 'resources',
      title: t('pages.settings.groups.resources.title'),
      desc: worktreeMode.value
        ? t('pages.settings.execution.sectionDesc')
        : t('pages.settings.groups.resources.desc'),
      keys: worktreeMode.value ? [] : ['sandbox_memory_mb', 'agent_idle_timeout_minutes'],
    },
  ]
  if (!worktreeMode.value) return all
  return all.filter((group) => group.id !== 'recycle' && group.id !== 'capacity')
})

const SETTING_UNITS: Record<string, string> = {
  run_sandbox_ttl_minutes: 'common.minutes',
  run_sandbox_max_retention_hours: 'pages.settings.units.hours',
  test_sandbox_ttl_minutes: 'common.minutes',
  sandbox_memory_mb: 'pages.settings.units.mib',
  agent_idle_timeout_minutes: 'common.minutes',
}

function settingDescription(key: string): string {
  return t(`pages.settings.settings.${key}`)
}

const detailedSettings = new Set(['run_sandbox_max_retention_hours', 'sandbox_memory_mb', 'agent_idle_timeout_minutes'])

function settingSummary(key: string): string {
  return detailedSettings.has(key) ? t(`pages.settings.summaries.${key}`) : settingDescription(key)
}

function settingLabel(key: string): string {
  return t(`pages.settings.labels.${key}`)
}

function settingUnit(key: string): string {
  const unitKey = SETTING_UNITS[key]
  return unitKey ? t(unitKey) : ''
}

function sourceLabelOf(source: string): string {
  return t(`pages.settings.sourceLabels.${source}`) || source
}

const items = ref<SettingItem[]>([])
const form = reactive<Record<string, number>>({})
const brandForm = reactive<BrandSettings>({ product_name: '', home_subtitle: '' })
const savedBrand = reactive<BrandSettings>({ product_name: '', home_subtitle: '' })
const workspaceForm = reactive({ name: '', description: '', unknownModelDisplayName: '' })
const workspaceSaved = reactive({ name: '', description: '', unknownModelDisplayName: '' })
const nameError = ref('')
const displayNameError = ref('')
let formEpoch = 0
const sandboxes = ref<SandboxView[]>([])
const dashboard = ref<DashboardStats | null>(null)
const loading = ref(true)
const hasInitialLoaded = ref(false)
const loadFailed = ref(false)
const loadDenied = ref(false)
const saving = ref(false)
const error = ref('')
const usageError = ref('')
const savedAt = ref(0)
const settingsSeq = createListRequestSeq()
const showRefreshProgress = computed(
  () => loading.value && hasInitialLoaded.value && items.value.length > 0,
)
const workspaceUnavailable = computed(
  () => !workspace.value && !workspaceLoading.value && !!workspaceError.value && !loadDenied.value,
)
const showSkeleton = computed(() => {
  if (loadFailed.value || loadDenied.value || workspaceUnavailable.value) return false
  if (loading.value && items.value.length === 0) return true
  return !workspace.value
})

const showIntegrations = computed(() => isIntegrationsQuery(route.query.integrations))

let poll: number | undefined

function itemOf(key: string): SettingItem | undefined {
  return items.value.find((it) => it.key === key)
}

function hydrate(list: SettingItem[]) {
  items.value = list
  for (const it of list) form[it.key] = it.value
}

function hydrateBrand(brand: BrandSettings) {
  brandForm.product_name = brand?.product_name || ''
  brandForm.home_subtitle = brand?.home_subtitle || ''
  savedBrand.product_name = brandForm.product_name
  savedBrand.home_subtitle = brandForm.home_subtitle
  setBrandSettings(brand)
}

function hydrateWorkspace(source: { name?: string; description?: string; unknownModelDisplayName?: string }) {
  workspaceForm.name = source.name || ''
  workspaceForm.description = source.description || ''
  workspaceForm.unknownModelDisplayName = source.unknownModelDisplayName || ''
  workspaceSaved.name = workspaceForm.name
  workspaceSaved.description = workspaceForm.description
  workspaceSaved.unknownModelDisplayName = workspaceForm.unknownModelDisplayName
  nameError.value = ''
  displayNameError.value = ''
}

function clearWorkspaceDraft() {
  formEpoch++
  workspaceForm.name = ''
  workspaceForm.description = ''
  workspaceForm.unknownModelDisplayName = ''
  workspaceSaved.name = ''
  workspaceSaved.description = ''
  workspaceSaved.unknownModelDisplayName = ''
  nameError.value = ''
  displayNameError.value = ''
}

function aggregateUsage(list: SandboxView[]) {
  const active = (s: SandboxView) =>
    s.status === 'creating' || s.status === 'pulling' || s.status === 'running'
  let testCount = 0
  let total = 0
  for (const s of list) {
    if (!active(s)) continue
    total++
    if (s.purpose === 'test') testCount++
  }
  return { testCount, total }
}

const usage = computed(() => aggregateUsage(sandboxes.value))
const runCount = computed(() => dashboard.value?.running ?? 0)
const capacityTotal = computed(() => usage.value.testCount + runCount.value)

const testLimit = computed(() => Math.max(1, Number(form.max_test_sandboxes) || 1))
const runLimit = computed(() => Math.max(1, Number(form.max_concurrent_runs) || 1))

const testRatio = computed(() => (testLimit.value > 0 ? usage.value.testCount / testLimit.value : 0))
const runRatio = computed(() => (runLimit.value > 0 ? runCount.value / runLimit.value : 0))

function usageBarClass(ratio: number, base: 'test' | 'run'): string {
  if (ratio >= 1) return 'bg-err'
  if (ratio >= 0.8) return 'bg-warn'
  return base === 'test' ? 'bg-info' : 'bg-accent-2'
}

function barWidth(ratio: number): string {
  return `${Math.min(100, ratio * 100)}%`
}

async function loadSettings() {
  const localSeq = settingsSeq.beginListRequest()
  loading.value = true
  error.value = ''
  loadFailed.value = false
  loadDenied.value = false
  try {
    const res = await api.getSettings()
    if (!settingsSeq.isCurrentSeq(localSeq)) return
    hydrate(res.items)
    hydrateBrand(res.brand)
    executionMode.value = res.execution_mode === 'local' ? 'local' : res.execution_mode === 'worktree' ? 'worktree' : 'sandbox'
    setExecutionMode(res.execution_mode)
  } catch (e: any) {
    if (!settingsSeq.isCurrentSeq(localSeq)) return
    if (items.value.length > 0) {
      error.value = e?.message || t('pages.settings.loadFailed')
      return
    }
    const status = httpStatusOf(e)
    loadDenied.value = status === 403
    loadFailed.value = status !== 403
    error.value = e?.message || t('pages.settings.loadFailed')
  } finally {
    if (!settingsSeq.isCurrentSeq(localSeq)) return
    loading.value = false
    hasInitialLoaded.value = true
  }
}

async function pollSandboxes() {
  try {
    const [sb, dash] = await Promise.all([api.listSandboxes(), api.dashboard()])
    sandboxes.value = sb
    dashboard.value = dash
    usageError.value = ''
  } catch (e: any) {
    usageError.value = e?.message || t('pages.settings.usageRefreshFailed')
  }
}

function failureText(cause: unknown, fallback: string): string {
  const message = cause instanceof Error ? cause.message : String((cause as { message?: string })?.message || cause || '')
  return message && message !== '[object Object]' ? message : fallback
}

function platformPatch(): Record<string, number | string> {
  const patch: Record<string, number | string> = {}
  for (const it of items.value) {
    if (it.locked) continue
    const next = Number(form[it.key])
    if (next !== it.value) patch[it.key] = next
  }
  if (
    isAdmin.value &&
    (brandForm.product_name !== savedBrand.product_name || brandForm.home_subtitle !== savedBrand.home_subtitle)
  ) {
    patch.brand_product_name = brandForm.product_name
    patch.brand_home_subtitle = brandForm.home_subtitle
  }
  return patch
}

const workspaceDirty = computed(
  () =>
    workspaceForm.name !== workspaceSaved.name ||
    workspaceForm.description !== workspaceSaved.description ||
    workspaceForm.unknownModelDisplayName !== workspaceSaved.unknownModelDisplayName,
)
const platformDirty = computed(() => Object.keys(platformPatch()).length > 0)
const dirty = computed(() => workspaceDirty.value || platformDirty.value)

function validateBeforeSave(): boolean {
  nameError.value = ''
  displayNameError.value = ''
  error.value = ''
  if (!workspaceForm.name.trim()) nameError.value = t('pages.workspace.nameRequired')
  if (normalizeUnknownModelDisplayNameInput(workspaceForm.unknownModelDisplayName).error) {
    displayNameError.value = t('pages.workspace.unknownModelDisplayNameTooLong')
  }
  const maxTest = itemOf('max_test_sandboxes')
  if (maxTest && !maxTest.locked) {
    const n = Number(form.max_test_sandboxes)
    if (!Number.isFinite(n) || n < 1) error.value = t('pages.settings.minTestSandboxes')
  }
  return !nameError.value && !displayNameError.value && !error.value
}

function clearUnknownModelDisplayName() {
  workspaceForm.unknownModelDisplayName = ''
  displayNameError.value = ''
  savedAt.value = 0
}

function restoreSavedDrafts() {
  workspaceForm.name = workspaceSaved.name
  workspaceForm.description = workspaceSaved.description
  workspaceForm.unknownModelDisplayName = workspaceSaved.unknownModelDisplayName
  nameError.value = ''
  displayNameError.value = ''
  for (const it of items.value) form[it.key] = it.value
  brandForm.product_name = savedBrand.product_name
  brandForm.home_subtitle = savedBrand.home_subtitle
  error.value = ''
  savedAt.value = 0
}

function resetDrafts() {
  restoreSavedDrafts()
  void loadSettings()
}

function retryLoad() {
  void loadSettings()
  void ensureLoaded()
}

async function save() {
  if (!workspace.value || !validateBeforeSave()) {
    savedAt.value = 0
    return
  }
  const saveWorkspace = workspaceDirty.value
  const savePlatform = platformDirty.value
  if (!saveWorkspace && !savePlatform) return
  const epoch = formEpoch
  saving.value = true
  error.value = ''
  savedAt.value = 0
  let workspaceFailure = ''
  let platformFailure = ''
  try {
    if (saveWorkspace) {
      const normalized = normalizeUnknownModelDisplayNameInput(workspaceForm.unknownModelDisplayName)
      try {
        const saved = await api.updateWorkspace({
          name: workspaceForm.name.trim(),
          description: workspaceForm.description,
          unknownModelDisplayName: normalized.value,
        })
        if (epoch === formEpoch) {
          workspace.value = saved
          hydrateWorkspace(saved)
        }
      } catch (cause) {
        if (epoch === formEpoch) workspaceFailure = failureText(cause, t('pages.settings.saveFailed'))
      }
    }
    if (savePlatform && epoch === formEpoch) {
      try {
        const res = await api.updateSettings(platformPatch())
        if (epoch === formEpoch) {
          hydrate(res.items)
          hydrateBrand(res.brand)
          if (res.execution_mode) {
            executionMode.value = res.execution_mode === 'local' ? 'local' : res.execution_mode === 'worktree' ? 'worktree' : 'sandbox'
            setExecutionMode(res.execution_mode)
          }
        }
      } catch (cause) {
        if (epoch === formEpoch) platformFailure = failureText(cause, t('pages.settings.saveFailed'))
      }
    }
    if (epoch !== formEpoch) return
    const parts: string[] = []
    if (workspaceFailure) parts.push(t('pages.settings.workspaceSaveFailed', { reason: workspaceFailure }))
    if (platformFailure) parts.push(t('pages.settings.platformSaveFailed', { reason: platformFailure }))
    error.value = parts.join(' ')
    if (!error.value) savedAt.value = Date.now()
  } finally {
    if (epoch === formEpoch) saving.value = false
  }
}

watch(workspace, (next, prev) => {
  if (!next) {
    clearWorkspaceDraft()
    return
  }
  if (saving.value) return
  if (prev && workspaceDirty.value) return
  hydrateWorkspace(next)
}, { immediate: true })

onMounted(() => {
  void loadSettings()
  void ensureLoaded()
  void pollSandboxes()
  poll = window.setInterval(pollSandboxes, 5000)
})

onBeforeUnmount(() => {
  formEpoch++
  if (poll) clearInterval(poll)
})
</script>

<template>
  <div
    class="settings-page"
    data-testid="settings-panel"
    :aria-busy="loading || saving ? 'true' : 'false'"
  >
    <Transition name="ui-fade" mode="out-in">
      <IntegrationsPanel v-if="showIntegrations" key="integrations" />
      <div v-else key="settings-main">
        <header class="mb-8">
          <h1 class="m-0 text-2xl font-semibold tracking-tight text-txt">{{ t('pages.settings.title') }}</h1>
          <p class="mb-0 mt-2 text-[13px] leading-5 text-txt3">{{ t('pages.settings.subtitle') }}</p>
        </header>

        <div
          v-if="error && items.length && !loadFailed && !loadDenied"
          role="alert"
          class="mb-5 rounded-xl border border-err/30 bg-err/10 px-4 py-3 text-sm text-err"
        >{{ error }}</div>

        <div
          v-if="showRefreshProgress"
          class="mb-3 h-[2px] overflow-hidden bg-line"
          data-testid="settings-thin-progress"
          aria-hidden="true"
        ><i class="admin-list-thin-bar bg-accent" /></div>

        <div
          v-if="showSkeleton"
          class="border-y border-line/60"
          data-testid="settings-form-skeleton"
          aria-hidden="true"
        >
          <div v-for="n in 4" :key="n" class="flex items-center justify-between gap-5 border-b border-line/60 py-5 last:border-b-0">
            <div class="flex-1">
              <div class="h-4 w-32 bg-elevated animate-pulse" />
              <div class="mt-2 h-3 w-2/3 bg-elevated animate-pulse" />
            </div>
            <div class="h-9 w-[88px] bg-elevated animate-pulse" />
          </div>
        </div>

        <div
          v-else-if="loadDenied"
          role="status"
          data-testid="settings-denied"
          class="border-y border-line/60 py-10 text-center"
        >
          <Icon name="lock" :size="22" class="mx-auto mb-3 text-txt3" />
          <h2 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h2>
          <p class="mt-1 text-xs text-txt3">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
          <AppButton class="mt-4" variant="outline" data-testid="settings-retry" @click="loadSettings">{{ t('common.buttons.retry') }}</AppButton>
        </div>

        <div
          v-else-if="loadFailed || workspaceUnavailable"
          role="status"
          data-testid="settings-failed"
          class="border-y border-line/60 py-10 text-center"
        >
          <h2 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h2>
          <p class="mt-1 text-xs text-txt3">{{ t('common.asyncState.loadFailedDesc') }}</p>
          <AppButton class="mt-4" variant="outline" data-testid="settings-retry" @click="retryLoad">{{ t('common.buttons.retry') }}</AppButton>
        </div>

        <template v-else>
          <div :class="showRefreshProgress ? 'opacity-[0.55]' : ''">
            <SettingsSection :title="t('pages.workspace.metaTitle')" :description="t('pages.workspace.metaHint')" data-testid="workspace-info-section">
              <SettingsRow :label="t('pages.workspace.nameLabel')" for="workspace-meta-name">
                <input
                  id="workspace-meta-name"
                  v-model="workspaceForm.name"
                  class="input w-full sm:w-[340px] disabled:cursor-not-allowed disabled:opacity-55"
                  autocomplete="off"
                  :disabled="saving"
                  @input="nameError = ''"
                />
                <template #details>
                  <p v-if="nameError" class="m-0 text-xs text-err" data-testid="workspace-meta-name-error">{{ nameError }}</p>
                </template>
              </SettingsRow>
              <SettingsRow :label="t('pages.workspace.descLabel')" for="workspace-meta-desc">
                <textarea
                  id="workspace-meta-desc"
                  v-model="workspaceForm.description"
                  rows="3"
                  class="input min-h-[88px] w-full resize-y sm:w-[340px] disabled:cursor-not-allowed disabled:opacity-55"
                  :placeholder="t('pages.workspace.metaDescPlaceholder')"
                  :disabled="saving"
                />
              </SettingsRow>
              <SettingsRow :label="t('pages.workspace.unknownModelDisplayNameLabel')" :description="t('pages.workspace.unknownModelDisplayNameHelp')" for="workspace-meta-unknown-display">
                <div class="flex w-full flex-col items-end gap-2 sm:w-[340px]">
                  <input
                    id="workspace-meta-unknown-display"
                    v-model="workspaceForm.unknownModelDisplayName"
                    class="input disabled:cursor-not-allowed disabled:opacity-55"
                    maxlength="80"
                    autocomplete="off"
                    data-testid="workspace-meta-unknown-display"
                    :placeholder="t('pages.workspace.unknownModelDisplayNamePlaceholder')"
                    :disabled="saving"
                    @input="displayNameError = ''"
                  />
                  <AppButton variant="ghost" size="sm" data-testid="workspace-meta-unknown-display-clear" :disabled="saving" @click="clearUnknownModelDisplayName">{{ t('pages.workspace.unknownModelDisplayNameClear') }}</AppButton>
                </div>
                <template #details>
                  <p v-if="displayNameError" class="m-0 text-xs text-err" data-testid="workspace-meta-unknown-display-error">{{ displayNameError }}</p>
                </template>
              </SettingsRow>
            </SettingsSection>

            <SettingsSection :title="t('pages.settings.brand.title')" :description="t('pages.settings.brand.desc')" data-testid="brand-settings-group">
              <SettingsRow for="setting-brand-product-name" :label="t('pages.settings.brand.productName')" :description="t('pages.settings.brand.productNameHint')">
                <input
                  id="setting-brand-product-name"
                  v-model="brandForm.product_name"
                  class="input settings-brand-input disabled:cursor-not-allowed disabled:opacity-55"
                  data-testid="brand-product-name"
                  type="text"
                  maxlength="40"
                  :placeholder="t('pages.settings.brand.productNamePlaceholder')"
                  :disabled="!isAdmin || saving"
                />
              </SettingsRow>
              <SettingsRow for="setting-brand-home-subtitle" :label="t('pages.settings.brand.homeSubtitle')" :description="t('pages.settings.brand.homeSubtitleHint')">
                <input
                  id="setting-brand-home-subtitle"
                  v-model="brandForm.home_subtitle"
                  class="input settings-brand-input disabled:cursor-not-allowed disabled:opacity-55"
                  data-testid="brand-home-subtitle"
                  type="text"
                  maxlength="80"
                  :placeholder="t('pages.settings.brand.homeSubtitlePlaceholder')"
                  :disabled="!isAdmin || saving"
                />
              </SettingsRow>
              <template v-if="!isAdmin" #footer>{{ t('pages.settings.brand.readOnly') }}</template>
            </SettingsSection>

            <SettingsSection v-for="group in groups" :key="group.id" :title="group.title" :description="group.desc">
              <SettingsRow
                v-if="group.id === 'resources'"
                :label="t('pages.settings.execution.label')"
                :description="executionHelp"
                data-testid="execution-mode"
              >
                <span class="chip border-accent/40 bg-accent-dim px-2.5 py-1 text-accent-2" data-testid="execution-mode-value">{{ executionLabel }}</span>
              </SettingsRow>
              <SettingsRow v-if="group.id === 'capacity'" :label="t('pages.settings.capacity.activeSandboxes')" :description="t('pages.settings.capacity.autoRefresh')" data-testid="settings-capacity-usage">
                <span class="text-xl font-semibold tabular-nums text-txt">{{ capacityTotal }}<span class="ml-1.5 text-xs font-normal text-txt3">{{ t('common.format.units') }}</span></span>
                <template #details>
                  <div class="space-y-2.5">
                    <div class="flex items-center gap-3">
                      <span class="w-12 shrink-0 text-xs text-txt3">{{ t('pages.settings.capacity.test') }}</span>
                      <div class="h-1 flex-1 overflow-hidden rounded-full bg-elevated">
                        <div class="h-full rounded-full transition-[width] duration-300 ease-out" :class="usageBarClass(testRatio, 'test')" :style="{ width: barWidth(testRatio) }" />
                      </div>
                      <span class="w-16 shrink-0 text-right text-xs tabular-nums text-txt2">{{ usage.testCount }}/{{ testLimit }} <span class="text-txt3">{{ t('pages.settings.capacity.limit') }}</span></span>
                    </div>
                    <div class="flex items-center gap-3">
                      <span class="w-12 shrink-0 text-xs text-txt3">{{ t('pages.settings.capacity.run') }}</span>
                      <div class="h-1 flex-1 overflow-hidden rounded-full bg-elevated">
                        <div class="h-full rounded-full transition-[width] duration-300 ease-out" :class="usageBarClass(runRatio, 'run')" :style="{ width: barWidth(runRatio) }" />
                      </div>
                      <span class="w-16 shrink-0 text-right text-xs tabular-nums" :class="runRatio >= 1 ? 'text-err' : 'text-txt2'">{{ runCount }}/{{ runLimit }} <span class="text-txt3">{{ t('pages.settings.capacity.concurrency') }}</span></span>
                    </div>
                  </div>
                  <p class="mb-0 mt-3 text-xs leading-5 text-txt3">{{ t('pages.settings.capacity.note') }}</p>
                  <p v-if="usageError" role="status" class="mb-0 mt-2 text-xs text-err">{{ usageError }}</p>
                </template>
              </SettingsRow>
              <template v-for="key in group.keys" :key="key">
                <SettingsRow v-if="itemOf(key)" :for="`setting-${key}`" :label="settingLabel(key)" :description="settingSummary(key)">
                  <div class="flex items-center gap-2">
                    <input
                      :id="`setting-${key}`"
                      v-model.number="form[key]"
                      type="number"
                      :min="itemOf(key)!.min"
                      :max="itemOf(key)!.max"
                      :disabled="itemOf(key)!.locked || saving"
                      class="input settings-number-input w-[88px] text-right disabled:cursor-not-allowed disabled:opacity-55"
                    />
                    <span v-if="settingUnit(key)" class="chip">{{ settingUnit(key) }}</span>
                  </div>
                  <template #meta>
                    <span :title="t('pages.settings.sourceTitle', { source: itemOf(key)!.source }) + ' · ' + key">{{ sourceLabelOf(itemOf(key)!.source) }}</span>
                    <span v-if="itemOf(key)!.locked" class="inline-flex items-center gap-1 text-warn" :title="t('pages.settings.envLockedTitle')"><Icon name="lock" :size="11" />{{ t('pages.settings.envLocked') }}</span>
                  </template>
                  <template v-if="detailedSettings.has(key)" #details>
                    <details class="settings-rule-details">
                      <summary class="w-fit cursor-pointer text-xs text-txt3 hover:text-txt">{{ t('common.buttons.details') }}</summary>
                      <p class="mb-0 mt-2 text-xs leading-5 text-txt3">{{ settingDescription(key) }}</p>
                    </details>
                  </template>
                </SettingsRow>
              </template>
            </SettingsSection>
            <div v-if="worktreeMode" data-testid="execution-mode-banner" class="mt-4 rounded-xl border border-line bg-surface px-3.5 py-3 text-[13px] leading-relaxed text-txt2">
              <p class="m-0">{{ t('pages.settings.execution.banner') }}</p>
              <ul class="mb-0 mt-2 list-disc pl-5">
                <li>{{ t('pages.settings.execution.bannerNoSwitch') }}</li>
                <li>{{ t('pages.settings.execution.bannerNoSave') }}</li>
              </ul>
            </div>
          </div>
          <p class="mb-0 mt-6 text-xs leading-5 text-txt3">{{ t('pages.settings.priorityNote') }}</p>
          <footer class="mt-7 flex flex-wrap items-center justify-end gap-2" data-testid="settings-save-actions">
            <span v-if="savedAt" role="status" class="mr-auto text-xs text-ok">{{ t('pages.settings.saved') }}</span>
            <AppButton class="min-h-11 sm:min-h-9" variant="ghost" size="md" icon="refresh" :disabled="loading || saving" @click="resetDrafts">{{ t('common.buttons.reset') }}</AppButton>
            <AppButton class="min-h-11 sm:min-h-9" variant="primary" size="md" icon="check" :disabled="loading || saving || !dirty" @click="save">{{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}</AppButton>
          </footer>
        </template>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.settings-page { width: 100%; max-width: 860px; margin-inline: auto; padding-top: 44px; padding-bottom: 80px; }
.settings-brand-input { width: 260px; max-width: 100%; }
@media (max-width: 639px) {
  .settings-page { padding-top: 28px; padding-bottom: 56px; }
  .settings-brand-input { width: 100%; }
}

/* Hide native number spinner — scoped to SettingsView number inputs only (g2.2 / f5) */
.settings-number-input[type='number']::-webkit-outer-spin-button,
.settings-number-input[type='number']::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.settings-number-input[type='number'] {
  -moz-appearance: textfield;
  appearance: textfield;
}
</style>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api, type BrandSettings, type DashboardStats, type SandboxView, type SettingItem } from '@/lib/api/api'
import { useAuth } from '@/lib/composables/useAuth'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import { isIntegrationsQuery } from '@/data/settingsNav'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import Icon from '@/components/ui/Icon.vue'
import IntegrationsPanel from '@/components/settings/IntegrationsPanel.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import { setBrandSettings } from '@/lib/composables/useBrandSettings'

const { t } = useI18n()
const route = useRoute()
const { user } = useAuth()

const isAdmin = computed(() => !!user.value?.isAdmin)

const groups = computed(() => [
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
    desc: t('pages.settings.groups.resources.desc'),
    keys: ['sandbox_memory_mb', 'agent_idle_timeout_minutes', 'shared_run_sandbox_enabled'],
  },
])

const SETTING_UNITS: Record<string, string> = {
  run_sandbox_ttl_minutes: 'common.minutes',
  run_sandbox_max_retention_hours: 'pages.settings.units.hours',
  test_sandbox_ttl_minutes: 'common.minutes',
  sandbox_memory_mb: 'pages.settings.units.mib',
  agent_idle_timeout_minutes: 'common.minutes',
}

// 0/1 knobs rendered as an on/off switch instead of a number input.
const SWITCH_SETTINGS = new Set(['shared_run_sandbox_enabled'])

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
const showSkeleton = computed(
  () => loading.value && items.value.length === 0 && !loadFailed.value && !loadDenied.value,
)

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

async function save() {
  const maxTest = itemOf('max_test_sandboxes')
  if (maxTest && !maxTest.locked) {
    const n = Number(form.max_test_sandboxes)
    if (!Number.isFinite(n) || n < 1) {
      error.value = t('pages.settings.minTestSandboxes')
      return
    }
  }

  saving.value = true
  error.value = ''
  try {
    const patch: Record<string, number | string> = {}
    for (const it of items.value) {
      if (!it.locked) patch[it.key] = Number(form[it.key])
    }
    if (isAdmin.value) {
      patch.brand_product_name = brandForm.product_name
      patch.brand_home_subtitle = brandForm.home_subtitle
    }
    const res = await api.updateSettings(patch)
    hydrate(res.items)
    hydrateBrand(res.brand)
    savedAt.value = Date.now()
  } catch (e: any) {
    error.value = e?.message || t('pages.settings.saveFailed')
  } finally {
    saving.value = false
  }
}

const dirty = () =>
  items.value.some((it) => !it.locked && Number(form[it.key]) !== it.value) ||
  (isAdmin.value &&
    (brandForm.product_name !== savedBrand.product_name ||
      brandForm.home_subtitle !== savedBrand.home_subtitle))

onMounted(() => {
  loadSettings()
  pollSandboxes()
  poll = window.setInterval(pollSandboxes, 5000)
})

onBeforeUnmount(() => {
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
          v-else-if="loadFailed"
          role="status"
          data-testid="settings-failed"
          class="border-y border-line/60 py-10 text-center"
        >
          <h2 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h2>
          <p class="mt-1 text-xs text-txt3">{{ t('common.asyncState.loadFailedDesc') }}</p>
          <AppButton class="mt-4" variant="outline" data-testid="settings-retry" @click="loadSettings">{{ t('common.buttons.retry') }}</AppButton>
        </div>

        <template v-else>
          <div :class="showRefreshProgress ? 'opacity-[0.55]' : ''">
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
                  <AppSwitch
                    v-if="SWITCH_SETTINGS.has(key)"
                    :id="`setting-${key}`"
                    :model-value="Number(form[key]) === 1"
                    :disabled="itemOf(key)!.locked || saving"
                    :aria-label="settingLabel(key)"
                    :data-testid="`setting-switch-${key}`"
                    @update:model-value="form[key] = $event ? 1 : 0"
                  />
                  <div v-else class="flex items-center gap-2">
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
          </div>
          <p class="mb-0 mt-6 text-xs leading-5 text-txt3">{{ t('pages.settings.priorityNote') }}</p>
        </template>

        <footer class="mt-7 flex flex-wrap items-center justify-end gap-2" data-testid="settings-save-actions">
          <span v-if="savedAt" role="status" class="mr-auto text-xs text-ok">{{ t('pages.settings.saved') }}</span>
          <AppButton class="min-h-11 sm:min-h-9" variant="ghost" size="md" icon="refresh" :disabled="loading || saving" @click="loadSettings">{{ t('common.buttons.reset') }}</AppButton>
          <AppButton class="min-h-11 sm:min-h-9" variant="primary" size="md" icon="check" :disabled="loading || saving || !dirty()" @click="save">{{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}</AppButton>
        </footer>
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

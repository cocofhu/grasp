<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api, type BrandSettings, type DashboardStats, type SandboxView, type SettingItem } from '@/lib/api/api'
import { useAuth } from '@/lib/composables/useAuth'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import { isIntegrationsQuery } from '@/data/settingsNav'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import IntegrationsPanel from '@/components/settings/IntegrationsPanel.vue'
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
    icon: 'runs',
    keys: ['max_concurrent_runs'],
  },
  {
    id: 'recycle',
    title: t('pages.settings.groups.recycle.title'),
    desc: t('pages.settings.groups.recycle.desc'),
    icon: 'trash',
    keys: ['run_sandbox_ttl_minutes', 'test_sandbox_ttl_minutes'],
  },
  {
    id: 'capacity',
    title: t('pages.settings.groups.capacity.title'),
    desc: t('pages.settings.groups.capacity.desc'),
    icon: 'flask',
    keys: ['max_test_sandboxes'],
  },
  {
    id: 'resources',
    title: t('pages.settings.groups.resources.title'),
    desc: t('pages.settings.groups.resources.desc'),
    icon: 'settings',
    keys: ['sandbox_memory_mb', 'agent_idle_timeout_minutes'],
  },
])

const SETTING_UNITS: Record<string, string> = {
  run_sandbox_ttl_minutes: 'common.minutes',
  test_sandbox_ttl_minutes: 'common.minutes',
  sandbox_memory_mb: 'pages.settings.units.mib',
  agent_idle_timeout_minutes: 'common.minutes',
}

function settingDescription(key: string): string {
  return t(`pages.settings.settings.${key}`)
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
  <!-- plan g1.2: fill-height + single overflow-y-auto scroll exit -->
  <div
    class="flex h-full min-h-0 flex-col"
    data-testid="settings-panel"
    :aria-busy="loading || saving ? 'true' : 'false'"
  >
    <Transition name="ui-fade" mode="out-in">
      <IntegrationsPanel v-if="showIntegrations" key="integrations" />
      <div v-else key="settings-main" class="flex min-h-0 flex-1 flex-col">
    <div class="mb-5 flex shrink-0 flex-col items-stretch gap-3 md:flex-row md:items-end md:justify-between">
      <div>
        <h2 class="text-lg font-semibold text-txt">{{ t('pages.settings.title') }}</h2>
        <p class="text-sm text-txt3">{{ t('pages.settings.subtitle') }}</p>
      </div>
      <div class="flex flex-col gap-2 md:flex-row md:items-center">
        <span v-if="savedAt" class="text-xs text-ok">{{ t('pages.settings.saved') }}</span>
        <AppButton class="min-h-11 w-full md:min-h-0 md:w-auto" variant="ghost" size="md" icon="refresh" :disabled="loading || saving" @click="loadSettings">
          {{ t('common.buttons.reset') }}
        </AppButton>
        <AppButton class="min-h-11 w-full md:min-h-0 md:w-auto" variant="primary" size="md" icon="check" :disabled="loading || saving || !dirty()" @click="save">
          {{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}
        </AppButton>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto">
    <div
      v-if="error && items.length && !loadFailed && !loadDenied"
      class="mb-4 rounded-lg border border-err/30 bg-err/10 px-3 py-2 text-sm text-err"
    >
      {{ error }}
    </div>

    <div
      v-if="showRefreshProgress"
      class="mb-2 h-[2px] overflow-hidden bg-line"
      data-testid="settings-thin-progress"
      aria-hidden="true"
    >
      <i class="admin-list-thin-bar bg-accent" />
    </div>

    <div
      v-if="showSkeleton"
      class="card"
      data-testid="settings-form-skeleton"
      aria-hidden="true"
    >
      <div v-for="n in 3" :key="'set-skel-' + n" class="border-b border-line px-4 py-3.5 last:border-b-0">
        <div class="mb-2 h-4 w-32 bg-elevated animate-pulse" />
        <div class="h-3 w-2/3 bg-elevated animate-pulse" />
        <div class="mt-3 flex justify-between gap-4">
          <div class="h-4 w-40 bg-elevated animate-pulse" />
          <div class="h-9 w-[88px] bg-elevated animate-pulse" />
        </div>
      </div>
    </div>

    <div
      v-else-if="loadDenied"
      role="status"
      data-testid="settings-denied"
      class="rounded-lg border border-warn/40 bg-warn/10 px-5 py-10 text-center"
    >
      <Icon name="lock" :size="22" class="mx-auto mb-3 text-warn" />
      <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h3>
      <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
      <AppButton class="mt-4" variant="outline" data-testid="settings-retry" @click="loadSettings">
        {{ t('common.buttons.retry') }}
      </AppButton>
    </div>

    <div
      v-else-if="loadFailed"
      role="status"
      data-testid="settings-failed"
      class="rounded-lg border border-err/40 bg-err/10 px-5 py-10 text-center"
    >
      <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
      <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
      <AppButton class="mt-4" variant="outline" data-testid="settings-retry" @click="loadSettings">
        {{ t('common.buttons.retry') }}
      </AppButton>
    </div>

    <template v-else>
      <div :class="showRefreshProgress ? 'opacity-[0.55]' : ''">
      <div class="card mb-4" data-testid="brand-settings-group">
        <div class="border-b border-line px-4 py-3.5">
          <h3 class="flex items-center gap-2 text-[13px] font-semibold text-txt">
            <span class="flex h-7 w-7 shrink-0 items-center justify-center bg-accent-dim text-accent-2">
              <Icon name="settings" :size="16" />
            </span>
            {{ t('pages.settings.brand.title') }}
          </h3>
          <p class="mt-1 text-xs leading-relaxed text-txt3">{{ t('pages.settings.brand.desc') }}</p>
          <p v-if="!isAdmin" class="mt-1 text-[11px] text-warn">{{ t('pages.settings.brand.readOnly') }}</p>
        </div>
        <div class="grid gap-4 px-4 py-3.5 md:grid-cols-2">
          <div>
            <label for="setting-brand-product-name" class="text-sm font-medium text-txt">
              {{ t('pages.settings.brand.productName') }}
            </label>
            <input
              id="setting-brand-product-name"
              v-model="brandForm.product_name"
              class="input mt-2 w-full disabled:cursor-not-allowed disabled:opacity-55"
              data-testid="brand-product-name"
              type="text"
              maxlength="40"
              :placeholder="t('pages.settings.brand.productNamePlaceholder')"
              :disabled="!isAdmin || saving"
            />
            <p class="mt-1.5 text-xs text-txt3">{{ t('pages.settings.brand.productNameHint') }}</p>
          </div>
          <div>
            <label for="setting-brand-home-subtitle" class="text-sm font-medium text-txt">
              {{ t('pages.settings.brand.homeSubtitle') }}
            </label>
            <input
              id="setting-brand-home-subtitle"
              v-model="brandForm.home_subtitle"
              class="input mt-2 w-full disabled:cursor-not-allowed disabled:opacity-55"
              data-testid="brand-home-subtitle"
              type="text"
              maxlength="80"
              :placeholder="t('pages.settings.brand.homeSubtitlePlaceholder')"
              :disabled="!isAdmin || saving"
            />
            <p class="mt-1.5 text-xs text-txt3">{{ t('pages.settings.brand.homeSubtitleHint') }}</p>
          </div>
        </div>
      </div>
      <div v-for="group in groups" :key="group.id" class="card mb-4">
        <div class="border-b border-line px-4 py-3.5">
          <h3 class="flex items-center gap-2 text-[13px] font-semibold text-txt">
            <span class="flex h-7 w-7 shrink-0 items-center justify-center bg-accent-dim text-accent-2">
              <Icon :name="group.icon" :size="16" />
            </span>
            {{ group.title }}
          </h3>
          <p class="mt-1 text-xs leading-relaxed text-txt3">{{ group.desc }}</p>
        </div>

        <div v-if="group.id === 'capacity'" class="border-b border-line px-4 py-3.5">
          <div class="rounded-lg border border-line bg-base px-3.5 py-3">
            <div class="mb-2.5 flex items-baseline justify-between gap-3">
              <span class="text-xs text-txt2">{{ t('pages.settings.capacity.activeSandboxes') }}</span>
              <span class="text-[22px] font-bold tabular-nums leading-none text-txt">
                {{ capacityTotal }}<span class="ml-1 text-xs font-normal text-txt3">{{ t('common.format.units') }}</span>
              </span>
            </div>

            <div class="mb-2.5 flex flex-wrap gap-2">
              <span class="rounded-md inline-flex items-center gap-1.5 border border-line bg-elevated px-2.5 py-1 text-[11px] text-txt2">
                <span class="h-1.5 w-1.5 shrink-0 bg-info" />
                {{ t('pages.settings.capacity.test') }} <strong class="tabular-nums text-txt">{{ usage.testCount }}</strong>
                <span class="text-[10px] text-txt3">/ {{ testLimit }} {{ t('pages.settings.capacity.limit') }}</span>
              </span>
              <span class="rounded-md inline-flex items-center gap-1.5 border border-line bg-elevated px-2.5 py-1 text-[11px] text-txt2">
                <span class="h-1.5 w-1.5 shrink-0 bg-accent-2" />
                {{ t('pages.settings.capacity.run') }} <strong class="tabular-nums text-txt">{{ runCount }}</strong>
                <span class="text-[10px] text-txt3">/ {{ runLimit }} {{ t('pages.settings.capacity.concurrency') }}</span>
              </span>
            </div>

            <div class="mb-1.5 flex items-center gap-2.5">
              <span class="w-14 shrink-0 text-[11px] text-txt3">{{ t('pages.settings.capacity.test') }}</span>
              <div class="flex flex-1 items-center gap-2">
                <div class="h-[3px] flex-1 overflow-hidden bg-elevated">
                  <div
                    class="h-full transition-[width] duration-300 ease-out"
                    :class="usageBarClass(testRatio, 'test')"
                    :style="{ width: barWidth(testRatio) }"
                  />
                </div>
                <span class="w-9 shrink-0 text-right text-[11px] tabular-nums text-txt2">
                  {{ usage.testCount }}/{{ testLimit }}
                </span>
              </div>
            </div>

            <div class="flex items-center gap-2.5">
              <span class="w-14 shrink-0 text-[11px] text-txt3">{{ t('pages.settings.capacity.run') }}</span>
              <div class="flex flex-1 items-center gap-2">
                <div class="h-[3px] flex-1 overflow-hidden bg-elevated">
                  <div
                    class="h-full transition-[width] duration-300 ease-out"
                    :class="usageBarClass(runRatio, 'run')"
                    :style="{ width: barWidth(runRatio) }"
                  />
                </div>
                <span
                  class="w-9 shrink-0 text-right text-[11px] tabular-nums"
                  :class="runRatio >= 1 ? 'font-semibold text-err' : 'text-txt2'"
                >
                  {{ runCount }}/{{ runLimit }}
                </span>
              </div>
            </div>

            <div class="mt-2.5 flex items-center justify-between border-t border-line pt-2 text-[10px] text-txt3">
              <span class="inline-flex items-center gap-1">
                <span class="h-[5px] w-[5px] animate-pulseglow bg-ok" />
                {{ t('pages.settings.capacity.autoRefresh') }}
              </span>
              <span>{{ t('pages.settings.capacity.note') }}</span>
            </div>

            <p v-if="usageError" class="mt-2 text-[11px] text-err">{{ usageError }}</p>
          </div>
        </div>

        <div
          v-for="key in group.keys"
          :key="key"
          class="border-b border-line px-4 py-3.5 last:border-b-0"
        >
          <template v-if="itemOf(key)">
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-1.5">
                  <label
                    :for="`setting-${key}`"
                    class="text-sm font-medium text-txt"
                  >{{ settingLabel(key) }}</label>
                  <span
                    class="rounded-full border border-line bg-elevated px-2 py-0.5 text-[10px] text-txt3"
                    :title="t('pages.settings.sourceTitle', { source: itemOf(key)!.source })"
                  >
                    {{ sourceLabelOf(itemOf(key)!.source) }}
                  </span>
                  <span
                    v-if="itemOf(key)!.locked"
                    class="inline-flex items-center gap-1 text-[10px] text-warn"
                    :title="t('pages.settings.envLockedTitle')"
                  >
                    <Icon name="gate" :size="11" /> {{ t('pages.settings.envLocked') }}
                  </span>
                </div>
                <div class="mt-0.5 font-mono text-[11px] text-txt3">{{ key }}</div>
                <p v-if="settingDescription(key)" class="mt-1.5 text-xs leading-relaxed text-txt3">
                  {{ settingDescription(key) }}
                </p>
              </div>
              <div class="flex shrink-0 items-center gap-2">
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
            </div>
          </template>
        </div>
      </div>
      </div>
    </template>

    <p class="mt-1 text-xs text-txt3">
      {{ t('pages.settings.priorityNote') }}
    </p>
    </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
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

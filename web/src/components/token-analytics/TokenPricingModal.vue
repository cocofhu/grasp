<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import { api } from '@/lib/api/api'
import type { TokenModelPrice } from '@/lib/shared/types'
import { useToast } from '@/lib/composables/useToast'

const props = defineProps<{
  open: boolean
  canEdit: boolean
  /** Model keys seen in the ledger, pre-listed so admins only fill numbers. */
  knownModels: string[]
}>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
}>()

const { t } = useI18n()
const toast = useToast()

type Row = { key: string } & TokenModelPrice
const currency = ref<'USD' | 'CNY'>('USD')
const rows = ref<Row[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const updatedAt = ref<string | undefined>()

const PRICE_FIELDS = ['input', 'output', 'cacheRead', 'cacheWrite'] as const

function emptyRow(key = ''): Row {
  return { key, input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const p = await api.getTokenPricing()
    currency.value = (p.currency || 'USD').toUpperCase() === 'CNY' ? 'CNY' : 'USD'
    updatedAt.value = p.updatedAt
    const priced = Object.entries(p.models || {}).map(([key, v]) => ({ ...emptyRow(key), ...v }))
    const seen = new Set(priced.map((r) => r.key))
    const unpriced = props.knownModels.filter((k) => k && !seen.has(k)).map((k) => emptyRow(k))
    rows.value = [...priced.sort((a, b) => a.key.localeCompare(b.key)), ...unpriced]
  } catch (e: unknown) {
    error.value = (e as Error)?.message || t('pages.tokenAnalytics.loadFailed')
  } finally {
    loading.value = false
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) void load()
  },
  { immediate: true },
)

const invalid = computed(() =>
  rows.value.some((r) => PRICE_FIELDS.some((f) => !Number.isFinite(Number(r[f])) || Number(r[f]) < 0)),
)

function addRow() {
  rows.value = [...rows.value, emptyRow()]
}

function removeRow(i: number) {
  rows.value = rows.value.filter((_, idx) => idx !== i)
}

async function save() {
  if (!props.canEdit || invalid.value) return
  saving.value = true
  error.value = ''
  const models: Record<string, TokenModelPrice> = {}
  for (const r of rows.value) {
    const key = r.key.trim()
    if (!key) continue
    const price = {
      input: Number(r.input) || 0,
      output: Number(r.output) || 0,
      cacheRead: Number(r.cacheRead) || 0,
      cacheWrite: Number(r.cacheWrite) || 0,
    }
    // All-zero rows are placeholders for known-but-unpriced models; keep them unpriced.
    if (PRICE_FIELDS.every((f) => price[f] === 0)) continue
    models[key] = price
  }
  try {
    await api.updateTokenPricing({ currency: currency.value, models })
    toast.show(t('pages.tokenAnalytics.pricing.saved'))
    emit('saved')
    emit('close')
  } catch (e: unknown) {
    error.value = (e as Error)?.message || t('pages.tokenAnalytics.pricing.saveFailed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <AppModal :open="open" :width="760" :title="t('pages.tokenAnalytics.pricing.title')" @close="emit('close')">
    <div class="flex flex-col gap-3 text-xs text-txt" data-testid="token-pricing-modal">
      <p class="m-0 text-txt3">{{ t('pages.tokenAnalytics.pricing.hint') }}</p>
      <p v-if="!canEdit" class="m-0 text-warn" data-testid="token-pricing-readonly">{{ t('pages.tokenAnalytics.pricing.readOnly') }}</p>
      <div class="flex items-center gap-2">
        <label class="text-txt2" for="token-pricing-currency">{{ t('pages.tokenAnalytics.pricing.currency') }}</label>
        <select
          id="token-pricing-currency"
          v-model="currency"
          class="rounded border border-line bg-surface px-2 py-1"
          :disabled="!canEdit"
          data-testid="token-pricing-currency"
        >
          <option value="USD">USD ($)</option>
          <option value="CNY">CNY (¥)</option>
        </select>
        <span v-if="updatedAt" class="ml-auto text-txt3">{{ t('pages.tokenAnalytics.pricing.updatedAt', { at: new Date(updatedAt).toLocaleString() }) }}</span>
      </div>
      <div v-if="loading" class="py-10 text-center text-txt3">{{ t('pages.tokenAnalytics.loading') }}</div>
      <div v-else class="token-analytics-table-scroll max-h-[48vh] overflow-auto">
        <table class="w-full border-collapse">
          <thead class="sticky top-0 bg-surface">
            <tr class="text-txt3">
              <th class="border-b border-line py-1.5 pr-2 text-left font-semibold">{{ t('pages.tokenAnalytics.tables.colModel') }}</th>
              <th v-for="f in PRICE_FIELDS" :key="f" class="border-b border-line px-1 py-1.5 text-right font-semibold">
                {{ t(`pages.tokenAnalytics.pricing.fields.${f}`) }}
              </th>
              <th v-if="canEdit" class="w-8 border-b border-line" />
            </tr>
          </thead>
          <tbody>
            <tr v-if="!rows.length">
              <td :colspan="canEdit ? 6 : 5" class="py-6 text-center text-txt3">{{ t('pages.tokenAnalytics.pricing.empty') }}</td>
            </tr>
            <tr v-for="(r, i) in rows" :key="i" data-testid="token-pricing-row">
              <td class="border-b border-line/60 py-1 pr-2">
                <input
                  v-model="r.key"
                  type="text"
                  class="w-full rounded border border-line bg-surface px-1.5 py-1 font-mono"
                  :disabled="!canEdit"
                  :placeholder="t('pages.tokenAnalytics.pricing.modelPh')"
                />
              </td>
              <td v-for="f in PRICE_FIELDS" :key="f" class="border-b border-line/60 px-1 py-1">
                <input
                  v-model.number="r[f]"
                  type="number"
                  min="0"
                  step="0.01"
                  class="w-24 rounded border border-line bg-surface px-1.5 py-1 text-right tabular-nums"
                  :class="Number(r[f]) < 0 ? 'border-err' : ''"
                  :disabled="!canEdit"
                  :data-testid="`token-pricing-${f}-${i}`"
                />
              </td>
              <td v-if="canEdit" class="border-b border-line/60 py-1 text-center">
                <button type="button" class="text-txt3 hover:text-err" :aria-label="t('pages.tokenAnalytics.pricing.remove')" @click="removeRow(i)">×</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="error" class="m-0 text-err" data-testid="token-pricing-error">{{ error }}</p>
    </div>
    <template #footer>
      <button
        v-if="canEdit"
        type="button"
        class="mr-auto rounded-md border border-line px-3 py-1.5 text-xs text-txt2 hover:bg-elevated"
        data-testid="token-pricing-add"
        @click="addRow"
      >{{ t('pages.tokenAnalytics.pricing.add') }}</button>
      <button type="button" class="rounded-md border border-line px-3 py-1.5 text-xs text-txt2 hover:bg-elevated" @click="emit('close')">
        {{ t('common.buttons.cancel') }}
      </button>
      <button
        v-if="canEdit"
        type="button"
        class="rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:opacity-50"
        :disabled="saving || invalid"
        data-testid="token-pricing-save"
        @click="save"
      >{{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}</button>
    </template>
  </AppModal>
</template>

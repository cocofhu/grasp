<script setup lang="ts">
import { computed, nextTick, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import VendorMark from '@/components/agent/VendorMark.vue'
import SoloPickerShell from './SoloPickerShell.vue'
import type { ConfiguredModelChoice } from '@/lib/solo/configuredModels'

const props = withDefaults(
  defineProps<{ models: ConfiguredModelChoice[]; modelValue: string; reasoningEffort?: string; disabled?: boolean; emptyAsLabel?: boolean; flat?: boolean }>(),
  { reasoningEffort: '', disabled: false, emptyAsLabel: false, flat: false },
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'update:reasoningEffort', v: string): void
}>()
const { t, te } = useI18n()
const view = ref<'models' | 'effort'>('models')
const effortSlider = ref<HTMLInputElement | null>(null)
const effortEntry = ref<HTMLButtonElement | null>(null)

const current = computed(() => props.models.find((model) => model.id === props.modelValue) ?? props.models[0] ?? null)
const label = computed(() => {
  if (!props.models.length && props.emptyAsLabel) return t('pages.solo.modelSelect.empty')
  return current.value?.name || t('pages.solo.modelSelect.placeholder')
})
const efforts = computed(() => [...new Set((current.value?.efforts || []).map((value) => value.trim()).filter(Boolean))])
const effortChoices = computed(() => ['', ...efforts.value])
const effortIndex = computed(() => Math.max(0, effortChoices.value.indexOf(props.reasoningEffort)))
const effortLabel = computed(() => levelLabel(props.reasoningEffort))
const effortBlocked = computed(() => props.disabled || !!current.value?.unavailable)
const dragging = ref(false)
const position = ref(0)
const effortPanel = ref<HTMLElement | null>(null)
const announced = ref(props.reasoningEffort || '')
watch(effortIndex, (value) => { if (!dragging.value) position.value = value }, { immediate: true })
watch(() => props.reasoningEffort, (value) => { announced.value = value || '' })
watchEffect(() => {
  const el = effortSlider.value
  if (!el || dragging.value) return
  const next = String(position.value)
  if (el.value !== next) el.value = next
})
const ratio = computed(() => efforts.value.length ? Math.min(1, Math.max(0, position.value / efforts.value.length)) : 0)
const previewLabel = computed(() => levelLabel(effortChoices.value[Math.min(efforts.value.length, Math.max(0, Math.round(position.value)))] || ''))
const shownLabel = computed(() => dragging.value ? previewLabel.value : effortLabel.value)
const effortColor = computed(() => (dragging.value ? effortChoices.value[Math.min(efforts.value.length, Math.max(0, Math.round(position.value)))] : props.reasoningEffort) === 'ultra' ? '#9b4dff' : '#3b82f6')

function tickPosition(index: number) {
  const stepRatio = efforts.value.length ? index / efforts.value.length : 0
  return `calc(14px + ${stepRatio} * (100% - 28px))`
}

function levelLabel(value: string) {
  if (!value) return t('pages.solo.modelSelect.effortAuto')
  const key = `pages.solo.modelSelect.effortLevels.${value}`
  return te(key) ? t(key) : value
}
function groups(q: string) {
  const query = q.trim().toLowerCase()
  return props.models.filter((model) => !query || model.name.toLowerCase().includes(query) || model.hint.toLowerCase().includes(query) || model.id.toLowerCase().includes(query))
}
function choose(model: ConfiguredModelChoice, close: () => void) {
  if (model.unavailable) return
  const primary = props.models[0]
  emit('update:modelValue', primary && model.id === primary.id ? '' : model.id)
  close()
}
function selected(model: ConfiguredModelChoice) {
  if (!props.modelValue) return model.id === (props.models[0]?.id || '')
  return model.id === props.modelValue
}
function setEffort(value: string) {
  if (effortBlocked.value || !effortChoices.value.includes(value) || value === announced.value) return
  announced.value = value
  emit('update:reasoningEffort', value)
}
function paintEffort(raw: number) {
  const max = efforts.value.length
  if (!max || effortBlocked.value || !Number.isFinite(raw)) return
  const clamped = Math.min(max, Math.max(0, raw))
  position.value = clamped
  effortPanel.value?.style.setProperty('--effort-ratio', String(clamped / max))
  const next = effortChoices.value[Math.round(clamped)]
  if (next !== undefined) setEffort(next)
}
function beginEffort() {
  if (!effortBlocked.value) dragging.value = true
}
function moveEffort(event: Event) {
  paintEffort(Number((event.target as HTMLInputElement).value))
}
function settleEffort(event: Event) {
  const raw = Number((event.target as HTMLInputElement).value)
  const max = efforts.value.length
  const clamped = Math.min(max, Math.max(0, Math.round(Number.isFinite(raw) ? raw : position.value)))
  const glide = Math.abs(position.value - clamped) > 0.001
  dragging.value = false
  const apply = () => {
    position.value = clamped
    const next = effortChoices.value[clamped]
    if (next !== undefined) setEffort(next)
  }
  if (glide) requestAnimationFrame(apply)
  else apply()
}
async function showEffort() {
  if (!efforts.value.length || effortBlocked.value) return
  view.value = 'effort'
  await nextTick()
  effortSlider.value?.focus()
}
async function showModels() {
  view.value = 'models'
  await nextTick()
  effortEntry.value?.focus()
}
async function onOpen() {
  view.value = 'models'
  if (props.models.length === 1 && efforts.value.length && !effortBlocked.value) await showEffort()
}
watch(efforts, (values) => { if (!values.length) view.value = 'models' })
</script>

<template>
  <SoloPickerShell
    :label="label"
    :placeholder="!current"
    :disabled="disabled || !models.length"
    :search-placeholder="view === 'models' && models.length > 5 ? t('pages.solo.modelSelect.search') : ''"
    :content-role="view === 'effort' ? 'group' : 'listbox'"
    :panel-width="view === 'effort' ? 250 : 240"
    :flat="flat"
    compact
    test-id="solo-model-select"
    @open="onOpen"
  >
    <template #trigger>
      <VendorMark v-if="current" :vendor="current.vendor" :backend="current.backend" :auto="current.backend && !current.id" :size="20" :class="{ 'solo-model-brand--flat': flat }" :style="flat ? { borderColor: 'transparent' } : undefined" />
      <Icon v-else name="sparkles" :size="14" class="shrink-0 text-accent" />
    </template>
    <template #label>
      <span class="solo-model-name" :title="label">{{ label }}</span>
      <span v-if="efforts.length" class="solo-model-effort" data-testid="solo-model-effort-label">{{ effortLabel }}</span>
    </template>
    <template v-if="view === 'models'" #header>
      <div class="solo-model-menu-title">{{ t('pages.solo.modelSelect.placeholder') }}</div>
    </template>
    <template #default="{ query, close }">
      <section v-if="view === 'effort'" ref="effortPanel" class="solo-effort" :class="{ 'solo-effort--dragging': dragging }" :style="{ '--effort-color': effortColor, '--effort-ratio': String(ratio) }" data-testid="solo-effort-panel" :aria-label="t('pages.solo.modelSelect.reasoningEffort')">
        <header class="solo-effort-heading">
          <Icon name="priority-bolt" :size="15" :interactive="false" class="solo-effort-glyph" />
          <strong class="solo-effort-current" data-testid="solo-effort-current">{{ shownLabel }}</strong>
          <button type="button" class="solo-effort-reset" :disabled="effortBlocked || !reasoningEffort" :aria-label="t('pages.solo.modelSelect.effortReset')" :title="t('pages.solo.modelSelect.effortReset')" data-testid="solo-effort-reset" @click="setEffort('')">
            <Icon name="refresh" :size="14" :interactive="false" />
          </button>
        </header>
        <button type="button" class="solo-effort-back" :aria-label="t('pages.solo.modelSelect.backToModels')" data-testid="solo-effort-back" @click="showModels">
          <span class="truncate">{{ label }}</span>
          <Icon name="chevron-right" :size="11" :interactive="false" />
        </button>
        <div class="solo-effort-control" :class="{ 'solo-effort-control--disabled': effortBlocked }">
          <div class="solo-effort-track" aria-hidden="true">
            <span class="solo-effort-fill" />
            <span v-for="(value, index) in effortChoices" :key="value || 'auto'" class="solo-effort-tick" :class="{ 'solo-effort-tick--filled': index <= position + 0.02 }" :style="{ left: tickPosition(index) }" />
          </div>
          <span class="solo-effort-thumb" />
          <input ref="effortSlider" class="solo-effort-slider" type="range" min="0" :max="efforts.length" step="any" :disabled="effortBlocked" :aria-label="t('pages.solo.modelSelect.reasoningEffort')" :aria-valuetext="shownLabel" :aria-valuenow="effortIndex" data-testid="solo-effort-slider" @pointerdown="beginEffort" @input="moveEffort" @change="settleEffort" @pointerup="settleEffort" @pointercancel="settleEffort" />
        </div>
      </section>
      <template v-else>
        <div v-if="!models.length" class="solo-picker-empty">{{ t('pages.solo.modelSelect.empty') }}</div>
        <template v-else>
          <button v-for="model in groups(query)" :key="model.id || 'auto'" type="button" role="option" data-picker-opt :disabled="model.unavailable" :aria-selected="selected(model)" :data-testid="`solo-model-option-${model.id || 'auto'}`" @click="choose(model, close)">
            <VendorMark class="solo-model-option-brand" :vendor="model.vendor" :backend="model.backend" :auto="model.backend && !model.id" :size="18" :unknown="model.unavailable" :style="{ borderColor: 'transparent' }" />
            <span class="min-w-0 flex-1 truncate" :title="model.name">{{ model.name }}</span>
            <span v-if="model.unavailable" class="shrink-0 text-[11px] text-txt3">{{ t('pages.solo.modelSelect.unavailable') }}</span>
            <Icon v-else-if="selected(model)" name="check" :size="14" class="shrink-0 text-accent" />
          </button>
          <div v-if="!groups(query).length" class="solo-picker-empty">{{ t('pages.solo.modelSelect.noMatch') }}</div>
        </template>
      </template>
    </template>
    <template v-if="view === 'models' && efforts.length" #footer>
      <button ref="effortEntry" type="button" data-picker-opt :disabled="effortBlocked" data-testid="solo-effort-open" @click="showEffort">
        <Icon name="priority-bolt" :size="13" :interactive="false" class="shrink-0 text-txt3" />
        <span class="min-w-0 flex-1">{{ t('pages.solo.modelSelect.reasoningEffort') }}</span>
        <span class="solo-model-effort">{{ effortLabel }}</span>
        <Icon name="chevron-right" :size="14" :interactive="false" />
      </button>
    </template>
  </SoloPickerShell>
</template>

<style scoped>
.solo-model-brand--flat:not(.vendor-mark--color) { background: transparent; }
.solo-model-option-brand:not(.vendor-mark--color) { background: transparent; }
.solo-model-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.solo-model-effort { flex-shrink: 0; color: rgb(var(--c-accent-2)); font-size: 12px; font-weight: 500; }
.solo-model-menu-title { padding: 4px 8px 5px; color: rgb(var(--c-txt3)); font-size: 11px; font-weight: 500; line-height: 16px; }
.solo-effort { padding: 1px 6px 6px; transition: --effort-ratio 200ms cubic-bezier(0.22, 1, 0.36, 1); }
.solo-effort--dragging { transition: none; }
.solo-effort-heading { display: grid; grid-template-columns: 26px minmax(0, 1fr) 26px; min-height: 24px; align-items: center; gap: 5px; }
.solo-effort-glyph { justify-self: start; color: var(--effort-color); }
.solo-effort-current { min-width: 0; overflow: hidden; color: var(--effort-color); font-size: 13px; font-weight: 500; line-height: 20px; text-align: center; text-overflow: ellipsis; white-space: nowrap; }
.solo-effort-back { display: flex; max-width: 100%; min-width: 0; align-items: center; justify-content: center; gap: 2px; margin: -1px auto 10px; padding: 0 3px; border-radius: 4px; color: rgb(var(--c-txt3)); font-size: 11px; line-height: 17px; }
.solo-effort-back:hover { color: rgb(var(--c-txt)); }
.solo-effort-reset { display: grid; height: 26px; width: 26px; place-items: center; border-radius: 6px; color: rgb(var(--c-txt3)); }
.solo-effort-reset:hover:not(:disabled) { background: rgb(var(--c-txt) / 0.05); color: rgb(var(--c-txt)); }
.solo-effort-reset:disabled { opacity: 0.4; }
.solo-effort-control { position: relative; height: 28px; }
.solo-effort-control--disabled { opacity: 0.45; }
.solo-effort-track { position: absolute; inset: 2px 0; overflow: hidden; border-radius: 999px; background: rgb(var(--c-txt) / 0.08); box-shadow: inset 0 0 0 1px rgb(var(--c-line) / 0.5); pointer-events: none; }
.solo-effort-fill { position: absolute; inset: 0 auto 0 0; width: calc(14px + var(--effort-ratio) * (100% - 28px)); border-radius: inherit; background: linear-gradient(110deg, #3b82f6, var(--effort-color)); }
.solo-effort-tick { position: absolute; top: 50%; width: 3px; height: 3px; border-radius: 50%; background: rgb(var(--c-txt3) / 0.38); transform: translate(-50%, -50%); }
.solo-effort-tick--filled { background: rgb(255 255 255 / 0.6); }
.solo-effort-thumb { position: absolute; z-index: 1; top: 0; left: calc(var(--effort-ratio) * (100% - 28px)); width: 28px; height: 28px; border: 1px solid rgb(0 0 0 / 0.09); border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgb(0 0 0 / 0.12); pointer-events: none; }
.solo-effort-slider { position: relative; z-index: 2; display: block; width: 100%; height: 28px; margin: 0; padding: 0; appearance: none; border-radius: 999px; background: transparent; cursor: grab; touch-action: none; }
.solo-effort--dragging .solo-effort-slider { cursor: grabbing; }
.solo-effort-slider::-webkit-slider-runnable-track { height: 28px; border-radius: 999px; background: transparent; }
.solo-effort-slider::-webkit-slider-thumb { width: 28px; height: 28px; appearance: none; border: 0; border-radius: 50%; background: transparent; box-shadow: none; }
.solo-effort-slider::-moz-range-track { height: 28px; border: 0; border-radius: 999px; background: transparent; }
.solo-effort-slider::-moz-range-thumb { width: 28px; height: 28px; border: 0; border-radius: 50%; background: transparent; box-shadow: none; }
.solo-effort-slider:disabled { cursor: default; }
.solo-effort-slider:focus-visible, .solo-effort-back:focus-visible, .solo-effort-reset:focus-visible { outline: 2px solid rgb(var(--c-accent) / 0.65); outline-offset: 3px; }
</style>

<style>
@property --effort-ratio {
  syntax: '<number>';
  inherits: true;
  initial-value: 0;
}
</style>

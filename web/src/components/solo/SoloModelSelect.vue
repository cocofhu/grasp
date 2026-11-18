<script setup lang="ts">
import { computed, nextTick, ref, useId, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import VendorMark from '@/components/agent/VendorMark.vue'
import SoloPickerShell from './SoloPickerShell.vue'
import { defaultReasoningEffort, FAST_SERVICE_TIER, listedEfforts, type ConfiguredModelChoice } from '@/lib/solo/configuredModels'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
  defineProps<{ models: ConfiguredModelChoice[]; modelValue: string; reasoningEffort?: string; serviceTier?: string; disabled?: boolean; emptyAsLabel?: boolean; flat?: boolean }>(),
  { reasoningEffort: '', serviceTier: '', disabled: false, emptyAsLabel: false, flat: false },
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'update:reasoningEffort', v: string): void
  (e: 'update:serviceTier', v: string): void
}>()
const { t, te } = useI18n()
const view = ref<'models' | 'effort'>('models')
const effortSlider = ref<HTMLInputElement | null>(null)
const effortEntry = ref<HTMLButtonElement | null>(null)
const fastButton = ref<HTMLButtonElement | null>(null)
const fastTooltip = ref(false)
const fastTooltipId = `solo-fast-${useId().replace(/:/g, '')}`
const fastTooltipStyle = ref<Record<string, string>>({})

const current = computed(() => props.models.find((model) => model.id === props.modelValue) ?? (!props.modelValue ? props.models[0] : null) ?? null)
const multipleGroups = computed(() => new Set(props.models.map(model => model.configurationId).filter(Boolean)).size > 1)
function optionNote(model: ConfiguredModelChoice) { return model.source || model.hint || '' }
const label = computed(() => {
  if (!props.models.length && props.emptyAsLabel) return t('pages.solo.modelSelect.empty')
  return current.value?.name || t('pages.solo.modelSelect.placeholder')
})
const efforts = computed(() => listedEfforts(current.value?.efforts))
const effortMax = computed(() => Math.max(0, efforts.value.length - 1))
const defaultEffort = computed(() => defaultReasoningEffort(efforts.value))
const restoreCount = ref(0)
const resolvedEffort = computed(() => efforts.value.includes(props.reasoningEffort) ? props.reasoningEffort : defaultEffort.value)
const effortIndex = computed(() => Math.max(0, efforts.value.indexOf(resolvedEffort.value)))
const effortLabel = computed(() => levelLabel(resolvedEffort.value))
const effortBlocked = computed(() => props.disabled || !!current.value?.unavailable)
const fastModeSupported = computed(() => current.value?.serviceTiers?.includes(FAST_SERVICE_TIER) === true)
const fastMode = computed(() => fastModeSupported.value && props.serviceTier === FAST_SERVICE_TIER)
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
const ratio = computed(() => effortMax.value ? Math.min(1, Math.max(0, position.value / effortMax.value)) : 0)
const previewLabel = computed(() => levelLabel(efforts.value[Math.min(effortMax.value, Math.max(0, Math.round(position.value)))] || ''))
const shownLabel = computed(() => dragging.value ? previewLabel.value : effortLabel.value)
const effortColor = computed(() => (dragging.value ? efforts.value[Math.min(effortMax.value, Math.max(0, Math.round(position.value)))] : resolvedEffort.value) === 'ultra' ? '#9b4dff' : '#3b82f6')

function tickPosition(index: number) {
  const stepRatio = effortMax.value ? index / effortMax.value : 0
  return `calc(14px + ${stepRatio} * (100% - 28px))`
}

function levelLabel(value: string) {
  if (!value) return ''
  const key = `pages.solo.modelSelect.effortLevels.${value}`
  return te(key) ? t(key) : value
}
function groups(q: string) {
  const query = q.trim().toLowerCase()
  return props.models.filter((model) => !query || [model.name, model.hint, model.id, model.configurationName, model.tool, model.source].some(value => value?.toLowerCase().includes(query)))
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
  if (effortBlocked.value || !efforts.value.includes(value) || value === announced.value) return
  announced.value = value
  emit('update:reasoningEffort', value)
}
function restoreEffort() {
  restoreCount.value++
  setEffort(defaultEffort.value)
}
function toggleFastMode() {
  if (effortBlocked.value || !fastModeSupported.value) return
  emit('update:serviceTier', fastMode.value ? '' : FAST_SERVICE_TIER)
}
function showFastTooltip() {
  const box = fastButton.value?.getBoundingClientRect()
  if (!box) return
  fastTooltipStyle.value = {
    left: `${Math.max(8, Math.min(box.left, window.innerWidth - 200))}px`,
    top: `${Math.max(8, box.top - 60)}px`,
  }
  fastTooltip.value = true
}
function paintEffort(raw: number) {
  const max = effortMax.value
  if (!efforts.value.length || effortBlocked.value || !Number.isFinite(raw)) return
  const clamped = Math.min(max, Math.max(0, raw))
  position.value = clamped
  effortPanel.value?.style.setProperty('--effort-ratio', String(max ? clamped / max : 0))
  const next = efforts.value[Math.round(clamped)]
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
  const max = effortMax.value
  const clamped = Math.min(max, Math.max(0, Math.round(Number.isFinite(raw) ? raw : position.value)))
  const glide = Math.abs(position.value - clamped) > 0.001
  dragging.value = false
  const apply = () => {
    position.value = clamped
    const next = efforts.value[clamped]
    if (next !== undefined) setEffort(next)
  }
  if (glide) requestAnimationFrame(apply)
  else apply()
}
async function showEffort() {
  if ((!efforts.value.length && !fastModeSupported.value) || effortBlocked.value) return
  view.value = 'effort'
  await nextTick()
  const control = effortSlider.value || fastButton.value
  control?.focus()
}
async function showModels() {
  view.value = 'models'
  await nextTick()
  effortEntry.value?.focus()
}
async function onOpen() {
  view.value = 'models'
  fastTooltip.value = false
  if (props.models.length === 1 && (efforts.value.length || fastModeSupported.value) && !effortBlocked.value) await showEffort()
}
watch([efforts, fastModeSupported], ([values, supportsFast]) => {
  if (!supportsFast) fastTooltip.value = false
  if (!values.length && !supportsFast) view.value = 'models'
})
</script>

<template>
  <SoloPickerShell
    v-bind="$attrs"
    :label="label"
    :placeholder="!current"
    :disabled="disabled || !models.length"
    :search-placeholder="view === 'models' && models.length > 0 ? t('pages.solo.modelSelect.search') : ''"
    :content-role="view === 'effort' ? 'group' : 'listbox'"
    :panel-width="view === 'effort' ? 250 : multipleGroups ? 320 : 240"
    :flat="flat"
    compact
    test-id="solo-model-select"
    @open="onOpen"
    @close="fastTooltip = false"
  >
    <template #trigger>
      <VendorMark v-if="current" :vendor="current.vendor" :backend="current.backend" :auto="current.backend && !(current.modelId ?? current.id)" :size="20" :flat="flat" />
      <Icon v-else name="sparkles" :size="14" class="shrink-0 text-accent" />
    </template>
    <template #label>
      <span class="solo-model-name" :title="label">{{ label }}</span>
      <span v-if="multipleGroups && current?.configurationName" class="truncate text-[10px] text-txt3">{{ current.configurationName }}</span>
      <span v-if="efforts.length" class="solo-model-effort" data-testid="solo-model-effort-label">{{ effortLabel }}</span>
      <span v-if="fastMode" class="solo-model-fast-label" data-testid="solo-model-fast-label">Fast</span>
    </template>
    <template v-if="view === 'models'" #header>
      <div class="solo-model-menu-title">{{ t('pages.solo.modelSelect.placeholder') }}</div>
    </template>
    <template #default="{ query, close }">
      <section v-if="view === 'effort'" ref="effortPanel" class="solo-effort" :class="{ 'solo-effort--dragging': dragging }" :style="{ '--effort-color': effortColor, '--effort-ratio': String(ratio) }" data-testid="solo-effort-panel" :aria-label="t('pages.solo.modelSelect.reasoningEffort')">
        <header class="solo-effort-heading">
          <button v-if="fastModeSupported" ref="fastButton" type="button" class="solo-effort-fast" :class="{ 'solo-effort-fast--active': fastMode }" :disabled="effortBlocked" :aria-label="t('pages.solo.modelSelect.fastMode')" :aria-pressed="fastMode" :aria-describedby="fastTooltip ? fastTooltipId : undefined" data-testid="solo-fast-toggle" @click="toggleFastMode" @pointerenter="showFastTooltip" @pointerleave="fastTooltip = false" @focus="showFastTooltip" @blur="fastTooltip = false">
            <svg class="solo-effort-bolt" width="15" height="15" viewBox="0 0 24 24" aria-hidden="true" fill="currentColor" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round">
              <path d="M13.5 2.55c.48-.6 1.43-.16 1.28.6L13.5 9.2h6.22c.78 0 1.18.89.68 1.49l-9.57 11.05c-.5.6-1.4.14-1.26-.61l1.31-6.05H4.6c-.77 0-1.17-.9-.67-1.49Z" />
            </svg>
          </button>
          <span v-else class="solo-effort-glyph"><Icon name="priority-bolt" :size="28" :interactive="false" /></span>
          <strong class="solo-effort-current" data-testid="solo-effort-current">{{ shownLabel }}</strong>
          <button type="button" class="solo-effort-reset" :disabled="effortBlocked || resolvedEffort === defaultEffort" :aria-label="t('pages.solo.modelSelect.effortReset')" :title="t('pages.solo.modelSelect.effortReset')" data-testid="solo-effort-reset" @click="restoreEffort">
            <Icon name="refresh" :size="14" :interactive="false" class="solo-effort-reset-icon" :style="{ '--restore-count': restoreCount }" />
          </button>
        </header>
        <button type="button" class="solo-effort-back" :aria-label="t('pages.solo.modelSelect.backToModels')" data-testid="solo-effort-back" @click="showModels">
          <span class="truncate">{{ label }}</span>
          <Icon name="chevron-right" :size="11" :interactive="false" />
        </button>
        <div v-if="efforts.length" class="solo-effort-control" :class="{ 'solo-effort-control--disabled': effortBlocked }">
          <div class="solo-effort-track" aria-hidden="true">
            <span class="solo-effort-fill" />
            <span v-for="(value, index) in efforts" :key="value" class="solo-effort-tick" :class="{ 'solo-effort-tick--filled': index <= position + 0.02 }" :style="{ left: tickPosition(index) }" />
          </div>
          <span class="solo-effort-thumb" />
          <input ref="effortSlider" class="solo-effort-slider" type="range" min="0" :max="effortMax" step="any" :disabled="effortBlocked" :aria-label="t('pages.solo.modelSelect.reasoningEffort')" :aria-valuetext="shownLabel" :aria-valuenow="effortIndex" data-testid="solo-effort-slider" @pointerdown="beginEffort" @input="moveEffort" @change="settleEffort" @pointerup="settleEffort" @pointercancel="settleEffort" />
        </div>
      </section>
      <template v-else>
        <div v-if="!models.length" class="solo-picker-empty">{{ t('pages.solo.modelSelect.empty') }}</div>
        <template v-else>
          <template v-for="(model, index) in groups(query)" :key="model.id || 'auto'">
          <div v-if="multipleGroups && (index === 0 || groups(query)[index - 1]?.configurationId !== model.configurationId)" class="solo-model-menu-title">{{ model.configurationName }}</div>
          <button type="button" role="option" class="solo-model-option" data-picker-opt :disabled="model.unavailable" :aria-selected="selected(model)" :data-testid="`solo-model-option-${model.id || 'auto'}`" @click="choose(model, close)">
            <VendorMark :vendor="model.vendor" :backend="model.backend" :auto="model.backend && !(model.modelId ?? model.id)" :size="18" :unknown="model.unavailable" flat />
            <span class="solo-model-option-text">
              <span class="solo-model-option-name" :title="model.name">{{ model.name }}</span>
              <span class="solo-model-option-note" :title="optionNote(model) || undefined">{{ optionNote(model) }}</span>
            </span>
            <span v-if="model.unavailable" class="shrink-0 text-[11px] text-txt3">{{ t('pages.solo.modelSelect.unavailable') }}</span>
            <Icon v-else-if="selected(model)" name="check" :size="14" class="shrink-0 text-accent" />
          </button>
          </template>
          <div v-if="!groups(query).length" class="solo-picker-empty">{{ t('pages.solo.modelSelect.noMatch') }}</div>
        </template>
      </template>
    </template>
    <template v-if="view === 'models' && (efforts.length || fastModeSupported)" #footer>
      <button ref="effortEntry" type="button" data-picker-opt :disabled="effortBlocked" data-testid="solo-effort-open" @click="showEffort">
        <span class="solo-effort-entry-glyph"><Icon name="priority-bolt" :size="24" :interactive="false" /></span>
        <span class="min-w-0 flex-1">{{ t('pages.solo.modelSelect.reasoningEffort') }}</span>
        <span class="solo-model-effort">{{ effortLabel }}</span>
        <Icon name="chevron-right" :size="14" :interactive="false" />
      </button>
    </template>
  </SoloPickerShell>
  <Teleport to="body">
    <div v-if="fastTooltip" :id="fastTooltipId" role="tooltip" class="solo-fast-tooltip" :style="fastTooltipStyle">
      <strong>{{ t('pages.solo.modelSelect.fastMode') }}</strong>
      <span>{{ t('pages.solo.modelSelect.fastModeHint') }}</span>
    </div>
  </Teleport>
</template>

<style scoped>
.solo-model-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.solo-model-option-text { min-width: 0; flex: 1 1 auto; }
.solo-model-option-name,
.solo-model-option-note { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.solo-model-option-name { font-size: 13px; line-height: 18px; }
.solo-model-option-note { min-height: 14px; color: rgb(var(--c-txt3)); font-size: 10px; line-height: 14px; }
.solo-model-effort { flex-shrink: 0; color: rgb(var(--c-accent-2)); font-size: 12px; font-weight: 500; }
.solo-model-fast-label { flex-shrink: 0; padding: 1px 5px; border-radius: 5px; background: rgb(var(--c-accent) / 0.12); color: rgb(var(--c-accent-2)); font-size: 10px; font-weight: 600; }
.solo-model-menu-title { padding: 4px 8px 5px; color: rgb(var(--c-txt3)); font-size: 11px; font-weight: 500; line-height: 16px; }
.solo-effort { padding: 1px 6px 6px; transition: --effort-ratio 200ms cubic-bezier(0.22, 1, 0.36, 1); }
.solo-effort--dragging { transition: none; }
.solo-effort-heading { display: grid; grid-template-columns: 36px minmax(0, 1fr) 36px; min-height: 36px; align-items: center; gap: 5px; }
.solo-effort-glyph { display: grid; width: 36px; height: 36px; place-items: center; color: var(--effort-color); }
.solo-effort-entry-glyph { display: grid; width: 24px; height: 24px; flex-shrink: 0; place-items: center; color: rgb(var(--c-txt2)); }
.solo-effort-fast { display: grid; width: 36px; height: 36px; place-items: center; border: 0; border-radius: 9px; background: transparent; }
.solo-effort-fast:disabled { opacity: 0.45; }
.solo-effort-bolt { --hover-ink: light-dark(#525357, #d5d5d8); color: light-dark(#8d8e8f, #aeafb2); fill-opacity: 0; }
.solo-effort-fast--active .solo-effort-bolt { --hover-ink: light-dark(#303034, #dddde0); color: light-dark(#000, #f7f7f8); fill-opacity: 1; }
.solo-fast-tooltip { position: fixed; z-index: 10000; display: grid; gap: 2px; max-width: 192px; padding: 9px 12px; border-radius: 10px; background: rgb(var(--c-txt)); color: rgb(var(--c-surface)); box-shadow: 0 6px 22px rgb(0 0 0 / 0.18); font-size: 12px; line-height: 17px; pointer-events: none; }
.solo-fast-tooltip strong { font-weight: 600; }
.solo-fast-tooltip span { opacity: 0.75; }
.solo-effort-current { min-width: 0; overflow: hidden; color: var(--effort-color); font-size: 13px; font-weight: 500; line-height: 20px; text-align: center; text-overflow: ellipsis; white-space: nowrap; }
.solo-effort-back { display: flex; max-width: 100%; min-width: 0; align-items: center; justify-content: center; gap: 2px; margin: -1px auto 10px; padding: 0 3px; border-radius: 4px; color: rgb(var(--c-txt3)); font-size: 11px; line-height: 17px; }
.solo-effort-back:hover { color: rgb(var(--c-txt)); }
.solo-effort-reset { display: grid; height: 26px; width: 26px; place-items: center; border-radius: 6px; color: rgb(var(--c-txt3)); }
.solo-effort-reset:hover:not(:disabled) { background: rgb(var(--c-txt) / 0.05); color: rgb(var(--c-txt)); }
.solo-effort-reset:disabled { opacity: 0.4; }
.solo-effort-fast:enabled:focus-visible .solo-effort-bolt { color: var(--hover-ink); }
@media (hover: hover) {
  .solo-effort-fast:enabled:hover .solo-effort-bolt { color: var(--hover-ink); }
}
.solo-effort-fast:enabled:active .solo-effort-bolt,
.solo-effort-reset:enabled:active .solo-effort-reset-icon { opacity: 0.7; }
@media (prefers-reduced-motion: no-preference) {
  .solo-effort-bolt {
    transition: color var(--dur-ui) ease-in-out, fill-opacity var(--dur-ui) ease-in-out, opacity var(--dur-press) ease;
  }
  .solo-effort-reset-icon { transition: opacity var(--dur-press) ease; }
  .solo-effort-reset-icon :deep(path:first-child) {
    /* The dash covers the arc at rest. Each click sweeps one gap toward the fixed arrowhead. */
    --arc-dash: 60px;
    --arc-gap: 6px;
    stroke-dasharray: var(--arc-dash) var(--arc-gap);
    stroke-dashoffset: calc(-1 * var(--restore-count) * (var(--arc-dash) + var(--arc-gap)));
    transition: stroke-dashoffset 360ms ease-in-out;
  }
  .solo-effort-reset { transition: background-color var(--dur-ui) ease, color var(--dur-ui) ease, opacity var(--dur-ui) ease; }
}
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
.solo-effort-slider:focus-visible, .solo-effort-back:focus-visible, .solo-effort-reset:focus-visible, .solo-effort-fast:focus-visible { outline: 2px solid rgb(var(--c-accent) / 0.65); outline-offset: 3px; }
</style>

<style>
@property --effort-ratio {
  syntax: '<number>';
  inherits: true;
  initial-value: 0;
}
</style>

<script setup lang="ts">
import { computed, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { effortLabel, validEffortToken } from '@/lib/agent/fixedEffort'

const props = defineProps<{ modelValue: string; efforts: string[]; modelName?: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()
const { locale } = useI18n()
const zh = computed(() => locale.value.toLowerCase().startsWith('zh'))
const levels = computed(() => [...new Set(props.efforts.map((value) => value.trim()).filter(Boolean))])
const choices = computed(() => ['', ...levels.value])
const labels = computed(() => choices.value.map((value) => value ? effortLabel(value, locale.value) : (zh.value ? '默认' : 'Default')))
const knownValue = computed(() => choices.value.includes(props.modelValue))
const index = computed(() => knownValue.value ? choices.value.indexOf(props.modelValue) : 0)
const dragging = ref(false)
const position = ref(0)
const panel = ref<HTMLElement | null>(null)
const sliderEl = ref<HTMLInputElement | null>(null)
watch(index, (value) => {
  if (!dragging.value) position.value = value
}, { immediate: true })
watchEffect(() => {
  const el = sliderEl.value
  if (!el || dragging.value) return
  const next = String(position.value)
  if (el.value !== next) el.value = next
})
const ratio = computed(() => levels.value.length ? Math.min(1, Math.max(0, position.value / levels.value.length)) : 0)
const currentLabel = computed(() => knownValue.value || !props.modelValue ? (labels.value[index.value] || labels.value[0]) : effortLabel(props.modelValue, locale.value))
const color = computed(() => props.modelValue === 'ultra' ? '#9b4dff' : '#3b82f6')
const known = computed(() => new Set(choices.value))
const draft = ref('')
const preferFixed = ref(false)
const fixed = computed(() => preferFixed.value || draft.value.trim() !== '' || (!!props.modelValue && !knownValue.value))
watch(() => [props.modelName || '', props.efforts.join('\0'), props.modelValue] as const, ([name, effortKey, value], previous) => {
  const modelChanged = !previous || previous[0] !== name || previous[1] !== effortKey
  const custom = !!value && !choices.value.includes(value)
  if (modelChanged) {
    preferFixed.value = custom
    draft.value = custom ? value : ''
    return
  }
  if (custom) {
    draft.value = value
    preferFixed.value = true
  } else if (!preferFixed.value) draft.value = ''
}, { immediate: true })
function useLevels() {
  preferFixed.value = false
  draft.value = ''
  if (props.modelValue) emit('update:modelValue', '')
}
function useFixed() {
  preferFixed.value = true
  if (props.modelValue && known.value.has(props.modelValue)) emit('update:modelValue', '')
}

function tickPosition(step: number) {
  const stepRatio = levels.value.length ? step / levels.value.length : 0
  return `calc(14px + ${stepRatio} * (100% - 28px))`
}
function choose(value: string) {
  if (!choices.value.includes(value) || value === props.modelValue) return
  emit('update:modelValue', value)
}
function paint(raw: number) {
  const max = levels.value.length
  if (!max || !Number.isFinite(raw)) return
  const clamped = Math.min(max, Math.max(0, raw))
  position.value = clamped
  panel.value?.style.setProperty('--cfg-effort-ratio', String(clamped / max))
  const next = choices.value[Math.round(clamped)]
  if (next !== undefined) choose(next)
}
function begin() {
  dragging.value = true
}
function move(event: Event) {
  paint(Number((event.target as HTMLInputElement).value))
}
function settle(event: Event) {
  const raw = Number((event.target as HTMLInputElement).value)
  const max = levels.value.length
  const clamped = Math.min(max, Math.max(0, Math.round(Number.isFinite(raw) ? raw : position.value)))
  const glide = Math.abs(position.value - clamped) > 0.001
  dragging.value = false
  const apply = () => {
    position.value = clamped
    const next = choices.value[clamped]
    if (next !== undefined) choose(next)
  }
  if (glide) requestAnimationFrame(apply)
  else apply()
}
function onKey(event: KeyboardEvent) {
  const max = levels.value.length
  let next = index.value
  if (event.key === 'ArrowRight' || event.key === 'ArrowUp') next = Math.min(max, next + 1)
  else if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') next = Math.max(0, next - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = max
  else return
  event.preventDefault()
  dragging.value = false
  position.value = next
  const value = choices.value[next]
  if (value !== undefined) choose(value)
}
function onCustom(event: Event) {
  const value = (event.target as HTMLInputElement).value
  draft.value = value
  const trimmed = value.trim()
  if (trimmed === '' || validEffortToken(trimmed)) emit('update:modelValue', trimmed)
}
</script>

<template>
  <div class="effort" :data-mode="fixed ? 'fixed' : 'levels'">
    <div v-if="levels.length" class="effort-switch" role="radiogroup" :aria-label="zh ? '思考强度' : 'Reasoning effort'">
      <button type="button" role="radio" :aria-checked="!fixed" data-testid="model-effort-mode-levels" @click="useLevels">{{ zh ? '思考强度' : 'Reasoning effort' }}</button>
      <button type="button" role="radio" :aria-checked="fixed" data-testid="model-effort-mode-fixed" @click="useFixed">{{ zh ? '固定强度' : 'Fixed effort' }}</button>
    </div>
    <section v-if="levels.length && !fixed" ref="panel" class="cfg-effort" :class="{ 'cfg-effort--dragging': dragging }" :style="{ '--effort-color': color, '--cfg-effort-ratio': String(ratio) }" data-testid="model-configuration-effort" :data-levels="labels.join('|')" :aria-label="zh ? '思考强度' : 'Reasoning effort'">
      <header class="cfg-effort-heading">
        <Icon name="priority-bolt" :size="15" :interactive="false" class="cfg-effort-glyph" />
        <strong class="cfg-effort-current" data-testid="model-effort-current">{{ currentLabel }}</strong>
        <button type="button" class="cfg-effort-reset" :disabled="!modelValue" :aria-label="zh ? '恢复默认强度' : 'Reset reasoning effort'" data-testid="model-effort-reset" @click="choose('')">
          <Icon name="refresh" :size="14" :interactive="false" />
        </button>
      </header>
      <div v-if="modelName" class="cfg-effort-model">
        <span class="truncate">{{ modelName }}</span>
        <Icon name="chevron-right" :size="11" :interactive="false" />
      </div>
      <div class="cfg-effort-control">
        <div class="cfg-effort-track" aria-hidden="true">
          <span class="cfg-effort-fill" />
          <span v-for="(value, step) in choices" :key="value || 'auto'" class="cfg-effort-tick" :class="{ 'cfg-effort-tick--filled': step <= position + 0.02 }" :style="{ left: tickPosition(step) }" />
        </div>
        <span class="cfg-effort-thumb" />
        <input ref="sliderEl" class="cfg-effort-slider" type="range" min="0" :max="levels.length" step="any" :aria-label="zh ? '思考强度' : 'Reasoning effort'" :aria-valuetext="currentLabel" :aria-valuenow="index" data-testid="model-effort-slider" @pointerdown="begin" @input="move" @change="settle" @pointerup="settle" @pointercancel="settle" @keydown="onKey" />
      </div>
    </section>
    <label v-if="!levels.length || fixed" class="effort-custom">
      <input class="input" data-testid="model-configuration-effort-custom" :value="draft" :placeholder="zh ? '固定强度例如 high' : 'Fixed effort e.g. high'" :aria-label="zh ? '固定强度' : 'Fixed effort'" @input="onCustom" />
    </label>
  </div>
</template>

<style scoped>
.effort { display: flex; flex-direction: column; gap: 8px; margin-top: 8px; }
.effort-switch { display: grid; grid-template-columns: 1fr 1fr; gap: 2px; padding: 3px; border-radius: 10px; background: rgb(var(--c-txt) / 0.05); }
.effort-switch button { min-height: 28px; border-radius: 8px; color: rgb(var(--c-txt3)); font-size: 12px; font-weight: 500; line-height: 16px; }
.effort-switch button[aria-checked="true"] { background: rgb(var(--c-surface)); color: rgb(var(--c-txt)); box-shadow: 0 1px 2px rgb(0 0 0 / 0.08); }
.effort-switch button:focus-visible { outline: 2px solid rgb(var(--c-accent) / 0.65); outline-offset: 2px; }
.effort-custom { display: flex; flex-direction: column; gap: 4px; }
.cfg-effort { padding: 8px 8px 6px; border: 1px solid rgb(var(--c-line)); border-radius: 12px; transition: --cfg-effort-ratio 200ms cubic-bezier(0.22, 1, 0.36, 1); }
.cfg-effort--dragging { transition: none; }
.cfg-effort-heading { display: grid; grid-template-columns: 26px minmax(0, 1fr) 26px; min-height: 24px; align-items: center; gap: 5px; }
.cfg-effort-glyph { justify-self: start; color: var(--effort-color); }
.cfg-effort-current { min-width: 0; overflow: hidden; color: var(--effort-color); font-size: 13px; font-weight: 500; line-height: 20px; text-align: center; text-overflow: ellipsis; white-space: nowrap; }
.cfg-effort-model { display: flex; max-width: 100%; min-width: 0; align-items: center; justify-content: center; gap: 2px; margin: 0 auto 10px; color: rgb(var(--c-txt3)); font-size: 11px; line-height: 17px; }
.cfg-effort-reset { display: grid; height: 26px; width: 26px; place-items: center; border-radius: 6px; color: rgb(var(--c-txt3)); }
.cfg-effort-reset:hover:not(:disabled) { background: rgb(var(--c-txt) / 0.05); color: rgb(var(--c-txt)); }
.cfg-effort-reset:disabled { opacity: 0.4; }
.cfg-effort-control { position: relative; height: 28px; }
.cfg-effort-track { position: absolute; inset: 2px 0; overflow: hidden; border-radius: 999px; background: rgb(var(--c-txt) / 0.08); box-shadow: inset 0 0 0 1px rgb(var(--c-line) / 0.5); pointer-events: none; }
.cfg-effort-fill { position: absolute; inset: 0 auto 0 0; width: calc(14px + var(--cfg-effort-ratio) * (100% - 28px)); border-radius: inherit; background: linear-gradient(110deg, #3b82f6, var(--effort-color)); }
.cfg-effort-tick { position: absolute; top: 50%; width: 3px; height: 3px; border-radius: 50%; background: rgb(var(--c-txt3) / 0.38); transform: translate(-50%, -50%); }
.cfg-effort-tick--filled { background: rgb(255 255 255 / 0.6); }
.cfg-effort-thumb { position: absolute; z-index: 1; top: 0; left: calc(var(--cfg-effort-ratio) * (100% - 28px)); width: 28px; height: 28px; border: 1px solid rgb(0 0 0 / 0.09); border-radius: 50%; background: #fff; box-shadow: 0 1px 3px rgb(0 0 0 / 0.12); pointer-events: none; }
.cfg-effort-slider { position: relative; z-index: 2; display: block; width: 100%; height: 28px; margin: 0; padding: 0; appearance: none; border-radius: 999px; background: transparent; cursor: grab; touch-action: none; }
.cfg-effort--dragging .cfg-effort-slider { cursor: grabbing; }
.cfg-effort-slider::-webkit-slider-runnable-track { height: 28px; border-radius: 999px; background: transparent; }
.cfg-effort-slider::-webkit-slider-thumb { width: 28px; height: 28px; appearance: none; border: 0; border-radius: 50%; background: transparent; box-shadow: none; }
.cfg-effort-slider::-moz-range-track { height: 28px; border: 0; border-radius: 999px; background: transparent; }
.cfg-effort-slider::-moz-range-thumb { width: 28px; height: 28px; border: 0; border-radius: 50%; background: transparent; box-shadow: none; }
.cfg-effort-slider:focus-visible, .cfg-effort-reset:focus-visible { outline: 2px solid rgb(var(--c-accent) / 0.65); outline-offset: 3px; }
</style>

<style>
@property --cfg-effort-ratio {
  syntax: '<number>';
  inherits: true;
  initial-value: 0;
}
</style>

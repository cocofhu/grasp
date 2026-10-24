<script setup lang="ts">
import { nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import { ACP_BACKENDS, type BackendId, type RegionPolicy } from '@/lib/shared/regionPolicy'

const props = withDefaults(defineProps<{
  modelValue: BackendId
  regionPolicy?: RegionPolicy | null
  currentRegion?: string
  specialRegion?: string
  testIdPrefix?: string
  disabled?: boolean
}>(), { currentRegion: '', specialRegion: '', testIdPrefix: 'acp', disabled: false })
const emit = defineEmits<{
  'update:modelValue': [value: BackendId]
  selectRegion: [value: string]
}>()
const { t } = useI18n()

function nextIndex(event: KeyboardEvent, index: number, length: number) {
  if (event.key === 'Home') return 0
  if (event.key === 'End') return length - 1
  if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') return (index + length - 1) % length
  if (event.key === 'ArrowDown' || event.key === 'ArrowRight') return (index + 1) % length
  return -1
}
async function toolKeydown(event: KeyboardEvent, index: number) {
  const next = nextIndex(event, index, ACP_BACKENDS.length)
  if (props.disabled || next < 0) return
  event.preventDefault()
  const group = (event.currentTarget as HTMLElement).parentElement
  emit('update:modelValue', ACP_BACKENDS[next]!.id)
  await nextTick()
  group?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus()
}
async function regionKeydown(event: KeyboardEvent, index: number) {
  const options = props.regionPolicy?.options || []
  const next = nextIndex(event, index, options.length)
  if (props.disabled || next < 0 || !options[next]) return
  event.preventDefault()
  const group = (event.currentTarget as HTMLElement).parentElement
  emit('selectRegion', options[next]!.id)
  await nextTick()
  group?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus()
}
</script>

<template>
  <div class="tool-picker" data-testid="coding-tool-picker">
    <div class="tool-picker-list" role="radiogroup" :aria-label="t('pages.agentStudio.toolPicker.title')" :aria-disabled="disabled">
      <button
        v-for="(tool, index) in ACP_BACKENDS"
        :key="tool.id"
        type="button"
        role="radio"
        class="tool-picker-option"
        :class="{ 'tool-picker-option--selected': modelValue === tool.id }"
        :aria-checked="modelValue === tool.id"
        :tabindex="modelValue === tool.id ? 0 : -1"
        :disabled="disabled"
        :data-testid="`${testIdPrefix}-backend-${tool.id}`"
        @click="emit('update:modelValue', tool.id)"
        @keydown="toolKeydown($event, index)"
      >
        <CredentialProviderLogo :provider="tool.id" match="provider" class="!h-7 !w-7 !rounded-md !border-0 !bg-transparent" aria-hidden="true" />
        <span class="tool-picker-name">{{ tool.label }}</span>
        <span class="tool-picker-radio" aria-hidden="true"><span v-if="modelValue === tool.id" /></span>
      </button>
    </div>
    <div v-if="regionPolicy" class="tool-picker-site" data-testid="coding-tool-site">
      <span class="tool-picker-site-label">{{ t('pages.agentStudio.toolPicker.site') }}</span>
      <div class="tool-picker-site-options" role="radiogroup" :aria-label="t('pages.agentStudio.region.title')">
        <button
          v-for="(option, index) in regionPolicy.options"
          :key="option.id"
          type="button"
          role="radio"
          :aria-checked="currentRegion === option.id"
          :aria-label="`${t(option.labelKey)} (${option.id})`"
          :tabindex="currentRegion === option.id || (!currentRegion && index === 0) ? 0 : -1"
          :disabled="disabled"
          class="tool-picker-site-option"
          :class="{ 'tool-picker-site-option--selected': currentRegion === option.id }"
          :data-testid="`${testIdPrefix}-region-${option.id}`"
          @click="emit('selectRegion', option.id)"
          @keydown="regionKeydown($event, index)"
        >{{ t(option.labelKey) }}</button>
      </div>
      <p v-if="specialRegion" class="tool-picker-special" role="status">{{ t('pages.agentStudio.region.special', { value: specialRegion }) }}</p>
    </div>
  </div>
</template>

<style scoped>
.tool-picker {
  max-width: 600px;
  min-width: 0;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line) / .85);
  border-radius: 12px;
  background: rgb(var(--c-surface));
}
.tool-picker-list {
  padding: 5px;
}
.tool-picker-option {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
  min-height: 43px;
  padding: 7px 11px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: rgb(var(--c-txt2));
  text-align: left;
  cursor: pointer;
}
.tool-picker-option:hover:not(:disabled) {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
}
.tool-picker-option--selected {
  background: rgb(var(--c-accent) / .055);
  color: rgb(var(--c-txt));
}
.tool-picker-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  line-height: 20px;
}
.tool-picker-option--selected .tool-picker-name {
  font-weight: 500;
}
.tool-picker-radio {
  display: grid;
  flex-shrink: 0;
  width: 15px;
  height: 15px;
  place-items: center;
  border: 1px solid rgb(var(--c-line-strong));
  border-radius: 50%;
}
.tool-picker-option--selected .tool-picker-radio {
  border-color: rgb(var(--c-accent-2));
}
.tool-picker-radio > span {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: rgb(var(--c-accent-2));
}
.tool-picker-site {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 14px;
  min-height: 57px;
  border-top: 1px solid rgb(var(--c-line) / .7);
  padding: 11px 16px;
}
.tool-picker-site-label {
  font-size: 12px;
  color: rgb(var(--c-txt3));
}
.tool-picker-site-options {
  display: inline-flex;
  gap: 2px;
  border-radius: 7px;
  padding: 3px;
  background: rgb(var(--c-elevated));
}
.tool-picker-site-option {
  min-height: 28px;
  border: 0;
  border-radius: 5px;
  padding: 4px 10px;
  background: transparent;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  line-height: 18px;
  cursor: pointer;
}
.tool-picker-site-option:hover:not(:disabled) {
  color: rgb(var(--c-txt));
}
.tool-picker-site-option--selected {
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt));
  box-shadow: 0 1px 3px rgb(0 0 0 / .08);
}
.tool-picker-option:focus-visible,
.tool-picker-site-option:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .5);
  outline-offset: -2px;
}
.tool-picker-option:disabled,
.tool-picker-site-option:disabled {
  cursor: default;
  opacity: .5;
}
.tool-picker-special {
  flex-basis: 100%;
  margin: 0;
  color: rgb(var(--c-warn));
  font-size: 11px;
  line-height: 18px;
  overflow-wrap: anywhere;
}
@media (max-width: 360px) {
  .tool-picker-site {
    padding-inline: 12px;
  }
  .tool-picker-site-options {
    width: 100%;
  }
  .tool-picker-site-option {
    flex: 1;
  }
}
</style>

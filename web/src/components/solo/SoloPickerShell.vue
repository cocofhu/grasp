<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import Icon from '@/components/ui/Icon.vue'

/**
 * Pill trigger plus a teleported, searchable panel. Options are any element
 * with [data-picker-opt]; arrow keys move focus between them.
 */
const props = withDefaults(
  defineProps<{
    label: string
    icon?: string
    disabled?: boolean
    searchPlaceholder?: string
    testId?: string
    panelWidth?: number
    /** Dims the label when nothing is chosen yet. */
    placeholder?: boolean
    invalid?: boolean
    multiple?: boolean
  }>(),
  { icon: '', disabled: false, searchPlaceholder: '', testId: 'solo-picker', panelWidth: 340, placeholder: false, invalid: false, multiple: false },
)

const panelId = `solo-picker-${useId().replace(/:/g, '')}`
const open = ref(false)
const query = ref('')
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const panelStyle = ref<Record<string, string>>({})

function place() {
  const el = trigger.value
  if (!el) return
  const r = el.getBoundingClientRect()
  const width = Math.min(props.panelWidth, window.innerWidth - 16)
  const left = Math.max(8, Math.min(r.left, window.innerWidth - width - 8))
  const below = window.innerHeight - r.bottom
  const style: Record<string, string> = { position: 'fixed', left: `${Math.round(left)}px`, width: `${Math.round(width)}px` }
  if (below < 280 && r.top > below) style.bottom = `${Math.round(window.innerHeight - r.top + 6)}px`
  else style.top = `${Math.round(r.bottom + 6)}px`
  panelStyle.value = style
}

async function show() {
  if (props.disabled) return
  open.value = true
  query.value = ''
  await nextTick()
  place()
  if (search.value) search.value.focus()
  else focusOpt(0)
}

function close(refocus = true) {
  if (!open.value) return
  open.value = false
  if (refocus) trigger.value?.focus()
}

function toggle() {
  if (open.value) close()
  else void show()
}

function opts(): HTMLElement[] {
  return panel.value ? Array.from(panel.value.querySelectorAll<HTMLElement>('[data-picker-opt]:not([disabled])')) : []
}

function focusOpt(i: number) {
  const list = opts()
  if (!list.length) return
  list[Math.max(0, Math.min(i, list.length - 1))]!.focus()
}

function onPanelKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
    return
  }
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
  e.preventDefault()
  const list = opts()
  const at = list.indexOf(document.activeElement as HTMLElement)
  if (e.key === 'ArrowDown') focusOpt(at < 0 ? 0 : at + 1)
  else if (at <= 0) search.value?.focus()
  else focusOpt(at - 1)
}

function onSearchEnter() {
  opts()[0]?.click()
}

function onDocPointer(e: Event) {
  if (!open.value) return
  const target = e.target as Node
  if (root.value?.contains(target) || panel.value?.contains(target)) return
  close(false)
}

function onReflow() {
  if (open.value) place()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocPointer)
  window.addEventListener('resize', onReflow)
  window.addEventListener('scroll', onReflow, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointer)
  window.removeEventListener('resize', onReflow)
  window.removeEventListener('scroll', onReflow, true)
})

defineExpose({ close, show })
</script>

<template>
  <div ref="root" class="solo-picker" :class="{ 'solo-picker--open': open }" :data-testid="testId">
    <button
      ref="trigger"
      type="button"
      class="solo-picker__trigger"
      :class="{ 'solo-picker__trigger--placeholder': placeholder, 'solo-picker__trigger--invalid': invalid }"
      :disabled="disabled"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="panelId"
      :data-testid="`${testId}-trigger`"
      @click="toggle"
      @keydown.down.prevent="show"
    >
      <slot name="trigger">
        <Icon v-if="icon" :name="icon" :size="14" class="solo-picker__glyph" />
      </slot>
      <span class="solo-picker__label" :title="label">{{ label }}</span>
      <Icon name="chevron-down" :size="12" class="solo-picker__chev" />
    </button>
    <Teleport to="body">
      <div
        v-if="open"
        :id="panelId"
        ref="panel"
        class="solo-picker__panel"
        :style="panelStyle"
        :data-testid="`${testId}-panel`"
        @keydown="onPanelKeydown"
      >
        <div v-if="searchPlaceholder" class="solo-picker__search-wrap">
          <Icon name="search" :size="14" class="solo-picker__search-icon" />
          <input
            ref="search"
            v-model="query"
            type="search"
            class="solo-picker__search"
            autocomplete="off"
            :placeholder="searchPlaceholder"
            :aria-label="searchPlaceholder"
            :data-testid="`${testId}-search`"
            @keydown.enter.prevent="onSearchEnter"
          />
        </div>
        <div class="solo-picker__list scroll-area" role="listbox" :aria-multiselectable="multiple || undefined">
          <slot :query="query.trim().toLowerCase()" :close="close" />
        </div>
        <div v-if="$slots.footer" class="solo-picker__footer">
          <slot name="footer" :close="close" />
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.solo-picker {
  position: relative;
  min-width: 0;
  max-width: 15rem;
}
.solo-picker__trigger {
  display: flex;
  width: 100%;
  height: 32px;
  align-items: center;
  gap: 6px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  /* Keep inline selectors integrated with the composer toolbar instead of making
   * each control look like a separate raised card. */
  background: transparent;
  color: rgb(var(--c-txt));
  padding: 0 8px;
  font-size: 12px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.18s ease, background-color 0.18s ease;
}
.solo-picker__trigger:hover:not(:disabled),
.solo-picker--open .solo-picker__trigger {
  border-color: rgb(var(--c-accent) / 0.3);
  background: rgb(var(--c-accent) / 0.045);
}
.solo-picker__trigger:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / 0.65);
  outline-offset: 3px;
}
.solo-picker__trigger:disabled {
  cursor: default;
  opacity: 0.6;
}
.solo-picker__trigger--placeholder .solo-picker__label {
  color: rgb(var(--c-txt3));
}
.solo-picker__trigger--invalid {
  border-color: rgb(var(--c-err) / 0.5);
}
.solo-picker__glyph {
  flex-shrink: 0;
  color: rgb(var(--c-accent));
}
.solo-picker__label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.solo-picker__chev {
  flex-shrink: 0;
  color: rgb(var(--c-txt3));
  transition: transform 0.18s ease;
}
.solo-picker--open .solo-picker__chev {
  transform: rotate(180deg);
}
.solo-picker__panel {
  z-index: 70;
  display: flex;
  max-height: min(420px, calc(100vh - 24px));
  flex-direction: column;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line) / 0.9);
  border-radius: 14px;
  background: rgb(var(--c-elevated));
  padding: 8px;
  box-shadow: 0 16px 44px rgb(0 0 0 / 0.22), 0 3px 10px rgb(0 0 0 / 0.08);
  animation: solo-picker-in 0.16s cubic-bezier(0.2, 0.7, 0.2, 1) both;
}
:global(html.light) .solo-picker__panel {
  background: rgb(var(--c-surface));
  box-shadow: 0 16px 44px rgb(48 36 85 / 0.09), 0 3px 10px rgb(48 36 85 / 0.035);
}
.solo-picker__search-wrap {
  position: relative;
  margin-bottom: 6px;
  flex-shrink: 0;
}
.solo-picker__search-icon {
  position: absolute;
  left: 10px;
  top: 9px;
  color: rgb(var(--c-txt3));
  pointer-events: none;
}
.solo-picker__search {
  width: 100%;
  height: 32px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt));
  padding: 0 10px 0 32px;
  font-size: 13px;
  outline: none;
}
.solo-picker__search:focus {
  border-color: rgb(var(--c-accent));
}
.solo-picker__list {
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
}
.solo-picker__footer {
  margin-top: 4px;
  flex-shrink: 0;
  border-top: 1px solid rgb(var(--c-line) / 0.9);
  padding-top: 4px;
}
.solo-picker__list :deep([data-picker-opt]),
.solo-picker__footer :deep([data-picker-opt]) {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 9px;
  min-height: 38px;
  border: 1px solid transparent;
  border-radius: 9px;
  background: transparent;
  padding: 6px 9px;
  color: rgb(var(--c-txt));
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  outline: none;
  transition: background 0.14s ease;
}
.solo-picker__list :deep([data-picker-opt]:hover),
.solo-picker__list :deep([data-picker-opt]:focus-visible),
.solo-picker__footer :deep([data-picker-opt]:hover),
.solo-picker__footer :deep([data-picker-opt]:focus-visible) {
  background: rgb(var(--c-accent) / 0.07);
}
.solo-picker__list :deep([data-picker-opt][aria-selected='true']) {
  border-color: rgb(var(--c-accent) / 0.08);
  background: rgb(var(--c-accent) / 0.09);
}
.solo-picker__list :deep(.solo-picker-group) {
  padding: 8px 9px 4px;
  color: rgb(var(--c-txt3));
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.solo-picker__list :deep(.solo-picker-empty) {
  padding: 18px 10px;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  text-align: center;
}
@keyframes solo-picker-in {
  from {
    opacity: 0;
    transform: translateY(-4px) scale(0.985);
  }
  to {
    opacity: 1;
    transform: none;
  }
}
@media (prefers-reduced-motion: reduce) {
  .solo-picker__panel {
    animation: none;
  }
}
@media (max-width: 520px) {
  .solo-picker {
    max-width: none;
  }
}
</style>

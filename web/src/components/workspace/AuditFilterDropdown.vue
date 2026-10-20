<script lang="ts">
let closeOpenAuditPanel: (() => void) | null = null
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export type AuditDdOption = {
  value: string
  label: string
  sub?: string
  short?: string
  dot?: string
}

const props = withDefaults(
  defineProps<{
    labelKey?: string
    modelValue: string
    options: AuditDdOption[]
    searchable?: boolean
    emptyLabel?: string
    width?: number
    right?: boolean
    /** Full-width trigger (mobile filter editor); lifts max-width:220px. */
    block?: boolean
    groupBy?: (o: AuditDdOption) => string
    disabled?: boolean
    testId?: string
  }>(),
  {
    labelKey: '',
    searchable: false,
    emptyLabel: '',
    width: 260,
    right: false,
    block: false,
    disabled: false,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const { t } = useI18n()
const controlId = `audit-filter-${useId().replace(/:/g, '')}`
const listboxId = `${controlId}-listbox`
const optionId = (index: number) => `${controlId}-option-${index}`
const open = ref(false)
const find = ref('')
const activeIndex = ref(0)
const trigEl = ref<HTMLButtonElement | null>(null)
const panelEl = ref<HTMLDivElement | null>(null)
const searchEl = ref<HTMLInputElement | null>(null)
const panelStyle = ref<Record<string, string>>({})
const placement = ref<'above' | 'below'>('below')
let positioned = false

const current = computed(() => props.options.find((o) => o.value === props.modelValue) || null)

const displayValue = computed(() => {
  const c = current.value
  if (!c || c.value === '') {
    return c?.label || props.emptyLabel || '全部'
  }
  return c.short || c.label
})

const muted = computed(() => !current.value || current.value.value === '')

const filtered = computed(() => {
  const q = find.value.trim().toLowerCase()
  if (!q) return props.options
  return props.options.filter((o) =>
    `${o.label || ''}${o.value || ''}${o.sub || ''}${o.short || ''}`.toLowerCase().includes(q),
  )
})
const activeDescendant = computed(() =>
  open.value && filtered.value[activeIndex.value] ? optionId(activeIndex.value) : undefined,
)

const MARGIN = 8
const GAP = 6
const MAX_PANEL_HEIGHT = 288

function anchorVisible(trig: HTMLElement, rect: DOMRect): boolean {
  const measured = rect.width > 0 && rect.height > 0
  let left = Math.max(0, rect.left)
  let right = Math.min(window.innerWidth, rect.right)
  let top = Math.max(0, rect.top)
  let bottom = Math.min(window.innerHeight, rect.bottom)
  const clips = /^(auto|scroll|hidden|clip)$/
  for (let ancestor: HTMLElement | null = trig; ancestor; ancestor = ancestor.parentElement) {
    const style = window.getComputedStyle(ancestor)
    if (style.display === 'none' || ['hidden', 'collapse'].includes(style.visibility)) return false
    // Geometry-less DOM tests can still exercise opening and keyboard behavior.
    if (!measured || ancestor === trig) continue
    if (left >= right || top >= bottom) return false
    const overflow = style.overflow.split(/\s+/)
    const clipX = clips.test(style.overflowX || overflow[0])
    const clipY = clips.test(style.overflowY || overflow[1] || overflow[0])
    if (!clipX && !clipY) continue
    const box = ancestor.getBoundingClientRect()
    if (clipX) {
      const clipLeft = box.left + (ancestor.clientLeft || Number.parseFloat(style.borderLeftWidth) || 0)
      left = Math.max(left, clipLeft)
      right = Math.min(right, ancestor.clientWidth > 0 ? clipLeft + ancestor.clientWidth : box.right - (Number.parseFloat(style.borderRightWidth) || 0))
    }
    if (clipY) {
      const clipTop = box.top + (ancestor.clientTop || Number.parseFloat(style.borderTopWidth) || 0)
      top = Math.max(top, clipTop)
      bottom = Math.min(bottom, ancestor.clientHeight > 0 ? clipTop + ancestor.clientHeight : box.bottom - (Number.parseFloat(style.borderBottomWidth) || 0))
    }
  }
  return !measured || (left < right && top < bottom)
}

function place() {
  const trig = trigEl.value
  if (!trig) return
  const r = trig.getBoundingClientRect()
  if (!anchorVisible(trig, r)) {
    close()
    return
  }
  const maxW = Math.max(0, window.innerWidth - MARGIN * 2)
  const pw = props.block
    ? Math.min(Math.max(r.width, 160), maxW)
    : Math.min(Math.max(0, props.width), maxW)
  let left = props.right ? r.right - pw : r.left
  left = Math.max(MARGIN, Math.min(left, window.innerWidth - pw - MARGIN))
  const height = Math.min(panelEl.value?.offsetHeight || MAX_PANEL_HEIGHT, MAX_PANEL_HEIGHT)
  const above = r.top - GAP - MARGIN
  const below = window.innerHeight - r.bottom - GAP - MARGIN
  const flipUp = below < height && above > below
  placement.value = flipUp ? 'above' : 'below'
  const maxHeight = Math.max(0, Math.min(MAX_PANEL_HEIGHT, flipUp ? above : below, window.innerHeight - MARGIN * 2))
  const visibleHeight = Math.min(height, maxHeight)
  const top = Math.max(MARGIN, Math.min(flipUp ? r.top - GAP - visibleHeight : r.bottom + GAP, window.innerHeight - MARGIN - visibleHeight))
  panelStyle.value = {
    left: `${Math.round(left)}px`,
    top: `${Math.round(top)}px`,
    width: `${Math.round(pw)}px`,
    maxHeight: `${Math.round(maxHeight)}px`,
  }
}

function close() {
  if (closeOpenAuditPanel === close) closeOpenAuditPanel = null
  open.value = false
  positioned = false
  find.value = ''
}

function revealActive() {
  if (!open.value || !positioned) return
  panelEl.value?.querySelectorAll<HTMLElement>('[role="option"]')[activeIndex.value]
    ?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
}

async function show(edge?: 'first' | 'last') {
  if (props.disabled) return
  if (closeOpenAuditPanel && closeOpenAuditPanel !== close) closeOpenAuditPanel()
  closeOpenAuditPanel = close
  positioned = false
  open.value = true
  find.value = ''
  activeIndex.value = Math.max(0, props.options.findIndex(o => o.value === props.modelValue))
  if (edge) activeIndex.value = edge === 'first' ? 0 : Math.max(0, filtered.value.length - 1)
  await nextTick()
  if (!open.value || props.disabled) return
  place()
  await nextTick()
  if (!open.value || props.disabled) return
  positioned = true
  revealActive()
  searchEl.value?.focus({ preventScroll: true })
  await nextTick()
  if (open.value) place()
}

function toggle(e: MouseEvent) {
  e.stopPropagation()
  if (props.disabled) return
  if (open.value) close()
  else void show()
}

function pick(v: string) {
  if (!open.value || props.disabled) return
  emit('update:modelValue', v)
  close()
  trigEl.value?.focus({ preventScroll: true })
}

function onDocClick(e: MouseEvent) {
  const t = e.target as Node
  if (trigEl.value?.contains(t) || panelEl.value?.contains(t)) return
  close()
}

function handleListKey(e: KeyboardEvent) {
  if (!open.value || props.disabled || e.isComposing || e.keyCode === 229 || e.ctrlKey || e.metaKey || e.altKey) return
  if (e.key === 'Tab') {
    if (panelEl.value?.contains(document.activeElement)) trigEl.value?.focus({ preventScroll: true })
    close()
    return
  }
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    close()
    trigEl.value?.focus({ preventScroll: true })
    return
  }
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    e.stopPropagation()
    const count = filtered.value.length
    if (count) activeIndex.value = (activeIndex.value + (e.key === 'ArrowDown' ? 1 : count - 1)) % count
    return
  }
  if ((e.key === 'Home' || e.key === 'End') && e.target !== searchEl.value) {
    e.preventDefault()
    e.stopPropagation()
    activeIndex.value = e.key === 'Home' ? 0 : Math.max(0, filtered.value.length - 1)
    return
  }
  if (e.key === 'Enter' || (e.key === ' ' && e.target !== searchEl.value)) {
    e.preventDefault()
    e.stopPropagation()
    const option = filtered.value[activeIndex.value]
    if (option) pick(option.value)
  }
}

function onKey(e: KeyboardEvent) {
  const target = e.target as Node | null
  if (target !== document && !trigEl.value?.contains(target) && !panelEl.value?.contains(target)) return
  handleListKey(e)
}

function onTriggerKey(e: KeyboardEvent) {
  if (props.disabled || open.value || e.isComposing || e.keyCode === 229 || e.ctrlKey || e.metaKey || e.altKey) return
  if (['ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' '].includes(e.key)) {
    e.preventDefault()
    e.stopPropagation()
    void show(e.key === 'Home' ? 'first' : e.key === 'End' ? 'last' : undefined)
  }
}

function onScrollOrResize() {
  if (open.value) place()
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey, true)
  window.addEventListener('resize', onScrollOrResize)
  window.addEventListener('scroll', onScrollOrResize, true)
})

onBeforeUnmount(() => {
  if (closeOpenAuditPanel === close) closeOpenAuditPanel = null
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey, true)
  window.removeEventListener('resize', onScrollOrResize)
  window.removeEventListener('scroll', onScrollOrResize, true)
})

watch(filtered, rows => { activeIndex.value = Math.min(activeIndex.value, Math.max(0, rows.length - 1)) })
watch([activeIndex, filtered], () => { if (open.value) void nextTick(() => { place(); revealActive() }) })
watch(() => props.disabled, disabled => { if (disabled) close() }, { flush: 'sync' })

defineExpose({ close })
</script>

<template>
  <div class="audit-dd" :class="{ open, block, 'is-hide': false }" :data-testid="testId">
    <button
      ref="trigEl"
      type="button"
      class="audit-dd-trig"
      :disabled="disabled"
      :role="searchable ? undefined : 'combobox'"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="open ? listboxId : undefined"
      :aria-activedescendant="searchable ? undefined : activeDescendant"
      :title="current?.label"
      @click="toggle"
      @keydown="onTriggerKey"
    >
      <span v-if="labelKey" class="k">{{ labelKey }} · </span>
      <span class="v" :class="{ muted }">{{ displayValue }}</span>
      <svg class="caret" viewBox="0 0 12 12" fill="none" aria-hidden="true">
        <path d="M3 4.5L6 7.5L9 4.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
      </svg>
    </button>
    <Teleport to="body">
      <div
        v-show="open"
        ref="panelEl"
        class="audit-dd-panel"
        :style="panelStyle"
        :aria-hidden="!open"
        :inert="!open"
        :data-placement="placement"
        @click.stop
      >
        <div v-if="searchable" class="audit-dd-find">
          <input
            ref="searchEl"
            v-model="find"
            type="search"
            role="combobox"
            aria-autocomplete="list"
            :aria-label="labelKey || t('common.search.optionPlaceholder')"
            :aria-expanded="open"
            :aria-controls="listboxId"
            :aria-activedescendant="activeDescendant"
            :placeholder="t('common.search.optionPlaceholder')"
            autocomplete="off"
            @input="activeIndex = 0"
            @keydown="handleListKey"
          />
        </div>
        <div :id="listboxId" class="audit-dd-list" role="listbox" :aria-label="labelKey || emptyLabel || undefined">
          <template v-if="filtered.length">
            <template v-for="(o, i) in filtered" :key="`${o.value}-${i}`">
              <div
                v-if="groupBy && (i === 0 || groupBy(o) !== groupBy(filtered[i - 1]!))"
                class="audit-dd-group"
                role="presentation"
              >
                {{ groupBy(o) }}
              </div>
              <button
                type="button"
                :id="optionId(i)"
                class="audit-dd-opt"
                :class="{ on: o.value === modelValue, active: i === activeIndex }"
                role="option"
                tabindex="-1"
                :aria-selected="o.value === modelValue"
                @click="pick(o.value)"
              >
                <span class="dot" :class="o.dot || ''" />
                <span class="main">
                  <span class="t">{{ o.label }}</span>
                  <span v-if="o.sub" class="s">{{ o.sub }}</span>
                </span>
                <span class="tick" aria-hidden="true">✓</span>
              </button>
            </template>
          </template>
          <div v-else class="audit-dd-empty">{{ t('common.empty.noMatchingOptions') }}</div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.audit-dd {
  position: relative;
  display: inline-flex;
  flex: 0 0 auto;
  min-width: 0;
  max-width: 100%;
  vertical-align: middle;
}
.audit-dd.block {
  display: block;
  width: 100%;
  flex: 1 1 auto;
}
.audit-dd-trig {
  height: 34px;
  min-height: 34px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 280px;
  border: 1px solid rgb(var(--c-line) / .85);
  background: rgb(var(--c-surface));
  padding: 0 8px 0 10px;
  font: inherit;
  font-size: 12px;
  color: rgb(var(--c-txt));
  cursor: pointer;
  text-align: left;
  white-space: nowrap;
  border-radius: 8px;
  outline: none;
  transition: border-color var(--dur-ui), background var(--dur-ui), box-shadow var(--dur-ui);
}
.audit-dd.block .audit-dd-trig {
  display: flex;
  width: 100%;
  max-width: none;
  min-height: 44px;
  height: auto;
  padding: 10px 12px;
  white-space: normal;
}
.audit-dd-trig:hover:not(:disabled) {
  border-color: rgb(var(--c-line-strong));
  background: rgb(var(--c-elevated) / .6);
}
.audit-dd.open .audit-dd-trig {
  border-color: rgb(var(--c-accent-2) / .4);
  background: rgb(var(--c-accent-dim) / .4);
}
.audit-dd-trig:focus-visible {
  border-color: rgb(var(--c-accent-2) / .5);
  box-shadow: 0 0 0 2px rgb(var(--c-accent-2) / .1);
}
.audit-dd-trig:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.audit-dd-trig .k {
  color: rgb(var(--c-txt2));
  font-weight: 500;
  flex: 0 0 auto;
}
.audit-dd-trig .v {
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 500;
  max-width: 200px;
  min-width: 0;
}
.audit-dd.block .audit-dd-trig .v {
  flex: 1 1 auto;
  min-width: 0;
  max-width: none;
}
.audit-dd-trig .v.muted {
  color: rgb(var(--c-txt2));
  font-weight: 400;
}
.caret {
  width: 12px;
  height: 12px;
  color: rgb(var(--c-txt2));
  flex: 0 0 auto;
  margin-left: auto;
  transition: transform 0.12s;
}
.audit-dd.open .caret {
  transform: rotate(180deg);
  color: rgb(var(--c-accent-2));
}
@media (max-width: 767px) {
  .audit-dd-trig { min-height: 44px; height: 44px; }
}
</style>

<style>
.audit-dd-panel {
  position: fixed;
  z-index: 1000;
  display: flex;
  flex-direction: column;
  max-width: calc(100vw - 16px);
  overflow: hidden;
  background: rgb(var(--c-surface));
  border: 1px solid rgb(var(--c-line));
  box-shadow: 0 6px 20px rgb(0 0 0 / .1);
  border-radius: 8px;
}
.audit-dd-find {
  padding: 8px;
  border-bottom: 1px solid rgb(var(--c-line));
  flex: 0 0 auto;
}
.audit-dd-find input {
  width: 100%;
  height: 30px;
  border: 1px solid rgb(var(--c-line));
  padding: 0 9px;
  font: inherit;
  font-size: 12px;
  outline: none;
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt));
  border-radius: 8px;
}
.audit-dd-find input:focus {
  border-color: rgb(var(--c-accent-2) / .5);
  background: rgb(var(--c-surface));
}
.audit-dd-list {
  overflow-y: auto;
  overflow-x: hidden;
  padding: 4px;
  flex: 1 1 auto;
  min-height: 0;
}
.audit-dd-group {
  padding: 8px 8px 4px;
  font-size: 11px;
  font-weight: 500;
  color: rgb(var(--c-txt3));
}
.audit-dd-opt {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 34px;
  border: 0;
  background: transparent;
  padding: 7px 8px;
  font: inherit;
  font-size: 12px;
  text-align: left;
  cursor: pointer;
  color: rgb(var(--c-txt));
  border-radius: 8px;
}
.audit-dd-opt:hover {
  background: rgb(var(--c-elevated) / .6);
}
.audit-dd-opt.active {
  background: rgb(var(--c-accent-dim) / .6);
}
.audit-dd-opt.on {
  background: rgb(var(--c-accent-dim));
  color: rgb(var(--c-accent-2));
}
.audit-dd-opt .dot {
  width: 6px;
  height: 6px;
  flex: 0 0 auto;
  border-radius: 999px;
  background: rgb(var(--c-txt3));
}
.audit-dd-opt .dot.mcp {
  background: rgb(var(--c-accent));
}
.audit-dd-opt .dot.run {
  background: rgb(var(--c-txt2));
}
.audit-dd-opt .dot.gate {
  background: rgb(var(--c-warn));
}
.audit-dd-opt .dot.wf {
  background: rgb(var(--c-info));
}
.audit-dd-opt .dot.prj {
  background: rgb(var(--c-ok));
}
.audit-dd-opt .dot.aud {
  background: rgb(var(--c-err));
}
.audit-dd-opt .main {
  flex: 1;
  min-width: 0;
}
.audit-dd-opt .main .t {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}
.audit-dd-opt .main .s {
  display: block;
  font-size: 11px;
  color: rgb(var(--c-txt3));
  margin-top: 1px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.audit-dd-opt .tick {
  width: 14px;
  opacity: 0;
  color: rgb(var(--c-accent-2));
  font-size: 12px;
  font-weight: 700;
}
.audit-dd-opt.on .tick {
  opacity: 1;
}
.audit-dd-empty {
  padding: 20px 12px;
  text-align: center;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}
@media (max-width: 767px) {
  .audit-dd-opt { min-height: 44px; }
  .audit-dd-find input { height: 44px; }
}
</style>

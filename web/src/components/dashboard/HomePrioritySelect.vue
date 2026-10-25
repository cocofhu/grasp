<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RunPriority } from '@/components/ui/PrioritySegmented.vue'

const PRIORITIES: RunPriority[] = ['high', 'normal', 'low']
const PANEL_WIDTH = 232

const props = withDefaults(
  defineProps<{
    modelValue: RunPriority
    disabled?: boolean
  }>(),
  { disabled: false },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: RunPriority): void
}>()

const { t } = useI18n()

const open = ref(false)
const activeIndex = ref(0)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
/** Fixed position near trigger (Teleport escapes .home-composer overflow-hidden). */
const panelStyle = ref<Record<string, string>>({})
const panelPlacement = ref<'below' | 'above'>('below')

const selectedLabel = computed(() => t(`common.priority.${props.modelValue}`))

function placePanelBelow() {
  const trig = trigger.value
  if (!trig) return
  const r = trig.getBoundingClientRect()
  const width = Math.min(PANEL_WIDTH, window.innerWidth - 16)
  let left = r.left
  left = Math.max(8, Math.min(left, window.innerWidth - width - 8))
  const belowTop = r.bottom + 6
  const spaceBelow = Math.max(0, window.innerHeight - belowTop - 8)
  const spaceAbove = Math.max(0, r.top - 14)
  const panelHeight = (panel.value?.scrollHeight || 182) + 2
  const placeAbove = spaceBelow < Math.min(panelHeight, 120) && spaceAbove > spaceBelow
  const maxHeight = Math.min(panelHeight, placeAbove ? spaceAbove : spaceBelow)
  const top = placeAbove ? Math.max(8, r.top - 6 - maxHeight) : belowTop
  panelPlacement.value = placeAbove ? 'above' : 'below'
  panelStyle.value = {
    position: 'fixed',
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
    maxHeight: `${Math.floor(maxHeight)}px`,
    zIndex: '60',
  }
}

function onScrollOrResize() {
  if (!open.value) return
  placePanelBelow()
  void nextTick(keepActiveOptionVisible)
}

function keepActiveOptionVisible() {
  const list = panel.value
  if (!open.value || !list || list.clientHeight <= 0) return
  const option = list.querySelector<HTMLElement>(
    `[data-testid="home-priority-select-option-${PRIORITIES[activeIndex.value]}"]`,
  )
  if (!option) return
  const top = option.offsetTop
  const bottom = top + option.offsetHeight
  if (top < list.scrollTop) list.scrollTop = top
  else if (bottom > list.scrollTop + list.clientHeight) {
    list.scrollTop = bottom - list.clientHeight
  }
}

watch(activeIndex, () => {
  if (open.value) void nextTick(keepActiveOptionVisible)
})

async function openPanel() {
  if (props.disabled) return
  open.value = true
  activeIndex.value = Math.max(0, PRIORITIES.indexOf(props.modelValue))
  await nextTick()
  placePanelBelow()
  await nextTick()
  keepActiveOptionVisible()
}

function closePanel() {
  open.value = false
  panelStyle.value = {}
}

function togglePanel(e: MouseEvent) {
  e.stopPropagation()
  if (props.disabled) return
  if (open.value) closePanel()
  else void openPanel()
}

function choose(value: RunPriority) {
  emit('update:modelValue', value)
  closePanel()
  trigger.value?.focus()
}

function onDocClick(e: MouseEvent) {
  if (!open.value) return
  const target = e.target as Node
  if (root.value?.contains(target) || panel.value?.contains(target)) return
  closePanel()
}

function onDocKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    closePanel()
    trigger.value?.focus()
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    activeIndex.value = (activeIndex.value + 1) % PRIORITIES.length
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    activeIndex.value = (activeIndex.value - 1 + PRIORITIES.length) % PRIORITIES.length
    return
  }
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    choose(PRIORITIES[activeIndex.value])
  }
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (props.disabled) return
  if (open.value) return
  if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    e.stopPropagation()
    void openPanel()
  }
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onDocKeydown)
  window.addEventListener('resize', onScrollOrResize)
  window.addEventListener('scroll', onScrollOrResize, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onDocKeydown)
  window.removeEventListener('resize', onScrollOrResize)
  window.removeEventListener('scroll', onScrollOrResize, true)
})
</script>

<template>
  <div
    ref="root"
    class="home-priority-select"
    data-testid="home-priority-select"
    :class="{ 'home-priority-select--open': open }"
  >
    <button
      ref="trigger"
      id="home-priority-select"
      type="button"
      class="home-priority-select__trigger"
      :class="{
        'home-priority-select__trigger--high': modelValue === 'high',
        'home-priority-select__trigger--low': modelValue === 'low',
      }"
      data-testid="home-priority-select-trigger"
      :disabled="disabled"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-label="t('common.priority.label')"
      aria-controls="home-priority-select-panel"
      @click="togglePanel"
      @keydown="onTriggerKeydown"
    >
      <Icon name="priority-bolt" :size="16" class="home-priority-select__bolt" />
      <span class="home-priority-select__label">{{ selectedLabel }}</span>
      <Icon name="chevron-down" :size="16" :interactive="false" class="home-priority-select__chev" />
    </button>

    <!-- Teleport to body so .home-composer overflow-hidden cannot clip the panel (plan g1.2) -->
    <Teleport to="body">
      <div
        v-if="open"
        ref="panel"
        id="home-priority-select-panel"
        class="home-priority-select__panel"
        role="listbox"
        data-testid="home-priority-select-panel"
        :data-placement="panelPlacement"
        :data-priority="modelValue"
        :aria-label="t('common.priority.label')"
        :style="panelStyle"
        @click.stop
      >
        <div class="home-priority-select__heading" aria-hidden="true">
          <span class="home-priority-select__heading-icon">
            <Icon name="priority-bolt" :size="16" :interactive="false" />
          </span>
          <span>{{ t('common.priority.label') }}</span>
          <span class="home-priority-select__current">{{ selectedLabel }}</span>
        </div>
        <div class="home-priority-select__spectrum" aria-hidden="true">
          <span
            v-for="value in PRIORITIES"
            :key="value"
            class="home-priority-select__spectrum-segment"
            :class="{ 'home-priority-select__spectrum-segment--current': value === modelValue }"
            :data-priority="value"
          />
        </div>
        <button
          v-for="(value, i) in PRIORITIES"
          :key="value"
          type="button"
          class="home-priority-select__opt"
          role="option"
          :class="{
            'home-priority-select__opt--current': value === modelValue,
            'home-priority-select__opt--active': i === activeIndex,
          }"
          :data-priority="value"
          :aria-selected="value === modelValue"
          :data-testid="`home-priority-select-option-${value}`"
          @click.stop="choose(value)"
        >
          <Icon :name="value === 'normal' ? 'priority-medium' : `priority-${value}`" :size="20" class="home-priority-select__strength" />
          <span>{{ t(`common.priority.${value}`) }}</span>
          <Icon name="check" :size="16" :interactive="false" :motion-key="value === modelValue ? 1 : 0" class="home-priority-select__check" />
        </button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.home-priority-select {
  position: relative;
  max-width: 7.5rem;
  min-width: 0;
  flex-shrink: 0;
}

.home-priority-select__trigger {
  width: 100%;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: transparent;
  color: rgb(var(--c-txt));
  padding: 0 8px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  text-align: left;
  box-shadow: none;
  transition: border-color 0.18s ease, color 0.18s ease, background 0.18s ease;
}

.home-priority-select__trigger:hover:not(:disabled),
.home-priority-select--open .home-priority-select__trigger {
  border-color: rgb(var(--c-line-strong));
  background: rgb(var(--c-txt) / 0.035);
  box-shadow: none;
}

.home-priority-select__trigger:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / 0.65);
  outline-offset: 3px;
}

.home-priority-select__trigger:disabled {
  cursor: default;
  color: rgb(var(--c-txt3));
  opacity: 0.65;
}

.home-priority-select__trigger--high:not(:disabled),
.home-priority-select__trigger--high:hover:not(:disabled),
.home-priority-select--open .home-priority-select__trigger--high:not(:disabled) {
  color: rgb(var(--c-accent-2));
  border-color: rgb(var(--c-accent) / 0.28);
  background: rgb(var(--c-accent) / 0.075);
}

.home-priority-select__trigger--low:not(:disabled) {
  color: rgb(var(--c-txt3));
}

.home-priority-select__label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.home-priority-select__bolt {
  width: 14px;
  height: 14px;
  flex-shrink: 0;
  color: rgb(var(--c-txt2));
  transition: filter 0.18s ease;
}

.home-priority-select__trigger--low .home-priority-select__bolt {
  opacity: 0.6;
}

.home-priority-select__trigger--high .home-priority-select__bolt {
  color: rgb(var(--c-accent));
  filter: drop-shadow(0 0 3px rgb(var(--c-accent) / 0.22));
}

.home-priority-select__chev {
  width: 12px;
  height: 12px;
  color: rgb(var(--c-txt3));
  flex-shrink: 0;
  transition: transform 0.18s ease;
}

.home-priority-select--open .home-priority-select__chev {
  transform: rotate(180deg);
}

.home-priority-select__panel {
  --c-accent: 184 168 223;
  --c-accent-2: 198 183 233;
  --priority-surface: rgb(var(--c-elevated));
  --priority-border: rgb(var(--c-line) / 0.9);
  --priority-shadow: 0 16px 44px rgb(0 0 0 / 0.22), 0 3px 10px rgb(0 0 0 / 0.08), inset 0 1px 0 rgb(255 255 255 / 0.05);
  --priority-band-low: #92b5ff;
  --priority-band-mid: #a28cfa;
  --priority-band-high: #8255e9;
  position: fixed;
  left: 0;
  top: calc(100% + 6px);
  width: 232px;
  box-sizing: border-box;
  border: 1px solid var(--priority-border);
  border-radius: 16px;
  overflow-x: hidden;
  overflow-y: auto;
  background: var(--priority-surface);
  box-shadow: var(--priority-shadow);
  z-index: 60;
  padding: 8px;
  transform-origin: 28px 0;
  animation: home-priority-enter 0.18s cubic-bezier(0.2, 0.7, 0.2, 1) both;
}

html.light .home-priority-select__panel {
  --c-accent: 83 72 139;
  --c-accent-2: 83 72 139;
  --priority-surface: rgb(var(--c-surface));
  --priority-border: rgb(232 230 240);
  --priority-shadow: 0 16px 44px rgb(48 36 85 / 0.09), 0 3px 10px rgb(48 36 85 / 0.035), inset 0 1px 0 rgb(255 255 255 / 0.9);
}

.home-priority-select__heading {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 5px 0;
  color: rgb(var(--c-txt));
  font-size: 12px;
  font-weight: 600;
}

.home-priority-select__heading-icon {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border: 1px solid rgb(var(--c-accent) / 0.075);
  border-radius: 8px;
  color: rgb(var(--c-accent));
  background: linear-gradient(145deg, rgb(var(--c-accent) / 0.1), rgb(var(--c-accent-2) / 0.035));
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.15);
}

.home-priority-select__heading-icon svg {
  width: 18px;
  height: 18px;
}

.home-priority-select__current {
  margin-left: auto;
  padding: 3px 7px;
  border-radius: 5px;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.065);
  font-size: 10px;
  font-weight: 500;
}

.home-priority-select__panel[data-priority='normal'] .home-priority-select__current {
  color: rgb(var(--c-txt2));
  background: rgb(var(--c-txt) / 0.05);
}

.home-priority-select__spectrum {
  display: flex;
  gap: 3px;
  height: 4px;
  margin: 12px 7px 13px;
}

.home-priority-select__spectrum-segment {
  flex: 1;
  border-radius: 3px;
  background: linear-gradient(90deg, var(--priority-band-mid), var(--priority-band-high));
  opacity: 0.25;
}

.home-priority-select__spectrum-segment[data-priority='normal'] {
  background: linear-gradient(90deg, var(--priority-band-low), var(--priority-band-mid));
}

.home-priority-select__spectrum-segment[data-priority='low'] {
  background: linear-gradient(90deg, var(--priority-band-low), var(--priority-band-low));
}

.home-priority-select__spectrum-segment--current {
  opacity: 1;
  box-shadow: 0 1px 5px rgb(var(--c-accent) / 0.12), inset 0 1px 0 rgb(255 255 255 / 0.25);
}

.home-priority-select__spectrum-segment[data-priority='normal'].home-priority-select__spectrum-segment--current {
  box-shadow: none;
}

.home-priority-select__opt {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  min-height: 35px;
  text-align: left;
  border: 1px solid transparent;
  border-radius: 9px;
  background: transparent;
  color: rgb(var(--c-txt));
  padding: 6px 9px;
  font-size: 12px;
  cursor: pointer;
  transition: background 0.14s ease, color 0.14s ease, border-color 0.14s ease;
}

.home-priority-select__opt:hover,
.home-priority-select__opt--active {
  background: rgb(var(--c-accent) / 0.045);
  color: rgb(var(--c-txt));
}

.home-priority-select__opt--current {
  color: rgb(var(--c-accent-2));
  border-color: rgb(var(--c-accent) / 0.06);
  background: rgb(var(--c-accent) / 0.075);
  font-weight: 600;
}

.home-priority-select__opt[data-priority='normal'].home-priority-select__opt--current {
  color: rgb(var(--c-txt));
  border-color: transparent;
  background: rgb(var(--c-txt) / 0.04);
  font-weight: 500;
}

.home-priority-select__opt:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / 0.6);
  outline-offset: -2px;
}

.home-priority-select__strength {
  width: 19px;
  height: 19px;
  flex-shrink: 0;
  color: rgb(var(--c-accent));
}

.home-priority-select__opt[data-priority='normal'] .home-priority-select__strength,
.home-priority-select__opt[data-priority='low'] .home-priority-select__strength {
  color: rgb(var(--c-txt2));
}

.home-priority-select__check {
  width: 15px;
  height: 15px;
  margin-left: auto;
  opacity: 0;
  color: rgb(var(--c-accent));
}

.home-priority-select__opt--current .home-priority-select__check {
  opacity: 1;
}

.home-priority-select__opt[data-priority='normal'].home-priority-select__opt--current .home-priority-select__check {
  color: rgb(var(--c-txt2));
}

@keyframes home-priority-enter {
  from {
    opacity: 0;
    transform: translateY(-4px) scale(0.985);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .home-priority-select__panel {
    animation: none;
  }

  .home-priority-select__trigger,
  .home-priority-select__bolt,
  .home-priority-select__chev,
  .home-priority-select__opt {
    transition: none;
  }
}

@media (max-width: 520px) {
  .home-priority-select {
    max-width: 6.5rem;
  }
}
</style>

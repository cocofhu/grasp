<script lang="ts">
/**
 * Closes whichever panel is open. Module scope on purpose: instances have to see
 * each other to keep at most one panel open.
 */
let closeOpenPanel: (() => void) | null = null
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    options: { value: string; label: string; hint?: string }[]
    disabled?: boolean
    invalid?: boolean
    placeholder?: string
    /** Overrides the empty menu message without adding a selectable option. */
    emptyText?: string
    ariaLabel?: string
    /** Adds a filter box on top of the list; matches are highlighted. */
    searchable?: boolean
    searchPlaceholder?: string
    /** Lets the typed query be used verbatim, so the list stays a suggestion. */
    allowCustom?: boolean
    /** A model directory can put known matches before the manually entered ID. */
    customPosition?: 'first' | 'last'
    /** Says the options are still on their way, so "no matches" stays unsaid. */
    loading?: boolean
    /** sm matches compact 12px form rows; md matches .input sizing. */
    size?: 'sm' | 'md'
    /** Quiet trigger and compact menu for controls embedded in a list. */
    variant?: 'outline' | 'ghost'
  }>(),
  {
    disabled: false,
    invalid: false,
    searchable: false,
    allowCustom: false,
    customPosition: 'first',
    loading: false,
    size: 'md',
    variant: 'outline',
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

const { t } = useI18n()
const selectId = `app-select-${useId().replace(/:/g, '')}`
const listboxId = `${selectId}-listbox`
const optionId = (index: number) => `${selectId}-option-${index}`

const open = ref(false)
const search = ref('')
const activeIndex = ref(0)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
/** Fixed position: Teleport escapes modal/scroll-area overflow clipping. */
const panelStyle = ref<Record<string, string>>({})
let panelPositioned = false

const matched = computed(() => props.options.find((o) => o.value === props.modelValue))
const selectedLabel = computed(() => {
  if (matched.value) return matched.value.label
  // A custom value is its own label; only a truly empty field shows the placeholder.
  if (props.allowCustom && props.modelValue) return props.modelValue
  return props.placeholder || ''
})
const isPlaceholder = computed(
  () => !matched.value && !(props.allowCustom && props.modelValue),
)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q || !props.searchable) return props.options
  return props.options.filter(
    (o) =>
      o.label.toLowerCase().includes(q) ||
      o.value.toLowerCase().includes(q) ||
      (o.hint || '').toLowerCase().includes(q),
  )
})

/** The typed query as a pickable row, unless it already is an option. */
const customValue = computed(() => {
  if (!props.allowCustom || !props.searchable) return ''
  const q = search.value.trim()
  if (!q) return ''
  return props.options.some((o) => o.value === q) ? '' : q
})

/** Keyboard navigation follows the visible row order. */
const rows = computed<{ value: string; label: string; hint: string; custom: boolean }[]>(() => {
  const custom = customValue.value ? [{ value: customValue.value, label: customValue.value, hint: '', custom: true }] : []
  const options = filtered.value.map((o) => ({ value: o.value, label: o.label, hint: o.hint || '', custom: false }))
  return props.customPosition === 'last' ? [...options, ...custom] : [...custom, ...options]
})
const activeDescendant = computed(() =>
  open.value && rows.value[activeIndex.value] ? optionId(activeIndex.value) : undefined,
)

function escapeHtml(s: string): string {
  return s.replace(
    /[&<>"']/g,
    (c) =>
      ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;',
      })[c] as string,
  )
}

function highlight(text: string): string {
  const q = search.value.trim()
  if (!q || !props.searchable) return escapeHtml(text)
  const i = text.toLowerCase().indexOf(q.toLowerCase())
  if (i < 0) return escapeHtml(text)
  return (
    escapeHtml(text.slice(0, i)) +
    '<mark>' +
    escapeHtml(text.slice(i, i + q.length)) +
    '</mark>' +
    escapeHtml(text.slice(i + q.length))
  )
}

const MARGIN = 8
const GAP = 6
const MAX_PANEL_HEIGHT = 288

function ghostAnchorVisible(trig: HTMLElement, rect: DOMRect): boolean {
  // DOM-only tests and a just-mounted trigger can still have no measured box.
  if (rect.width <= 0 || rect.height <= 0) return true
  let left = Math.max(0, rect.left)
  let right = Math.min(window.innerWidth, rect.right)
  let top = Math.max(0, rect.top)
  let bottom = Math.min(window.innerHeight, rect.bottom)
  const clips = /^(auto|scroll|hidden|clip)$/
  for (let ancestor = trig.parentElement; ancestor; ancestor = ancestor.parentElement) {
    if (left >= right || top >= bottom) return false
    const style = window.getComputedStyle(ancestor)
    const overflow = style.overflow.split(/\s+/)
    const clipX = clips.test(style.overflowX || overflow[0])
    const clipY = clips.test(style.overflowY || overflow[1] || overflow[0])
    if (!clipX && !clipY) continue
    const box = ancestor.getBoundingClientRect()
    if (clipX) {
      const clipLeft = box.left + (ancestor.clientLeft || Number.parseFloat(style.borderLeftWidth) || 0)
      const clipRight = ancestor.clientWidth > 0
        ? clipLeft + ancestor.clientWidth
        : box.right - (Number.parseFloat(style.borderRightWidth) || 0)
      left = Math.max(left, clipLeft)
      right = Math.min(right, clipRight)
    }
    if (clipY) {
      const clipTop = box.top + (ancestor.clientTop || Number.parseFloat(style.borderTopWidth) || 0)
      const clipBottom = ancestor.clientHeight > 0
        ? clipTop + ancestor.clientHeight
        : box.bottom - (Number.parseFloat(style.borderBottomWidth) || 0)
      top = Math.max(top, clipTop)
      bottom = Math.min(bottom, clipBottom)
    }
  }
  return left < right && top < bottom
}

function placePanel() {
  const trig = trigger.value
  if (!trig) return
  const r = trig.getBoundingClientRect()
  if (props.variant === 'ghost' && !ghostAnchorVisible(trig, r)) {
    // Scrolling a row away dismisses its menu without moving focus back to the
    // offscreen trigger, which would scroll the list to the old row again.
    closePanel()
    return
  }
  const height = Math.min(panel.value?.offsetHeight || MAX_PANEL_HEIGHT, MAX_PANEL_HEIGHT)
  const below = window.innerHeight - r.bottom - GAP - MARGIN
  const flipUp = below < height && r.top - GAP - MARGIN > below
  let top = flipUp ? Math.max(MARGIN, r.top - GAP - height) : r.bottom + GAP
  let maxHeight = Math.max(0, Math.min(MAX_PANEL_HEIGHT, flipUp ? r.top - GAP - MARGIN : below))
  if (props.variant === 'ghost') {
    maxHeight = Math.min(maxHeight, Math.max(0, window.innerHeight - MARGIN * 2))
    const visibleHeight = Math.min(height, maxHeight)
    top = Math.max(MARGIN, Math.min(top, window.innerHeight - MARGIN - visibleHeight))
  }
  // Form menus retain their trigger width; list menus need room for their labels
  // and must stay inside the viewport even when the trigger sits at the row end.
  const width = props.variant === 'ghost'
    ? Math.min(Math.max(168, r.width), Math.max(0, window.innerWidth - MARGIN * 2))
    : r.width
  const left = props.variant === 'ghost'
    ? Math.max(MARGIN, Math.min(r.left, window.innerWidth - width - MARGIN))
    : r.left
  panelStyle.value = {
    position: 'fixed',
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
    maxHeight: `${Math.round(maxHeight)}px`,
    zIndex: '60',
  }
}

function onScrollOrResize() {
  if (open.value) placePanel()
}

function revealActiveOption() {
  if (!open.value || !panelPositioned) return
  panel.value?.querySelectorAll<HTMLElement>('[role="option"]')[activeIndex.value]
    ?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
}

async function openPanel(edge?: 'first' | 'last') {
  if (props.disabled) return
  // A trigger click stops propagating, so a parent row or menu does not react to
  // it — which also hides it from every other select's document listener. Panels
  // therefore hand over here, keeping at most one open.
  if (closeOpenPanel && closeOpenPanel !== closePanel) closeOpenPanel()
  closeOpenPanel = closePanel
  panelPositioned = false
  open.value = true
  search.value = ''
  activeIndex.value = Math.max(
    0,
    props.options.findIndex((o) => o.value === props.modelValue),
  )
  if (edge) activeIndex.value = edge === 'first' ? 0 : Math.max(0, rows.value.length - 1)
  await nextTick()
  if (!open.value || props.disabled) return
  placePanel()
  // Only scroll after Vue applies the fixed positioning. A newly teleported
  // list still in body flow must never scroll the surrounding page.
  await nextTick()
  if (!open.value || props.disabled) return
  panelPositioned = true
  revealActiveOption()
  searchInput.value?.focus({ preventScroll: true })
  await nextTick()
  if (open.value) placePanel()
}

function closePanel() {
  if (closeOpenPanel === closePanel) closeOpenPanel = null
  open.value = false
  panelPositioned = false
  search.value = ''
  panelStyle.value = {}
}

function hideLeavingPanel(element: Element) {
  // A fading-out menu must not remain in the accessibility tree when another
  // select opens, or receive a late click while the replacement is active.
  element.setAttribute('aria-hidden', 'true')
  ;(element as HTMLElement).inert = true
}

function togglePanel(e: MouseEvent) {
  e.stopPropagation()
  if (props.disabled) return
  if (open.value) closePanel()
  else void openPanel()
}

function choose(value: string) {
  if (props.disabled || !open.value) return
  emit('update:modelValue', value)
  closePanel()
  trigger.value?.focus({ preventScroll: true })
}

function onDocClick(e: MouseEvent) {
  if (!open.value) return
  const target = e.target as Node
  if (root.value?.contains(target) || panel.value?.contains(target)) return
  closePanel()
}

/** Shared by the panel search box and the document-level fallback. */
function handleListKeydown(e: KeyboardEvent): void {
  if (!open.value || props.disabled || e.isComposing || e.keyCode === 229) return
  if (e.ctrlKey || e.metaKey || e.altKey) return
  const items = rows.value
  if (e.key === 'Tab') {
    // The search field lives at the end of body via Teleport. Return to the
    // trigger synchronously so native Tab continues in the original form order.
    if (panel.value?.contains(document.activeElement)) trigger.value?.focus({ preventScroll: true })
    closePanel()
    return
  }
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    closePanel()
    trigger.value?.focus({ preventScroll: true })
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    e.stopPropagation()
    if (items.length) activeIndex.value = (activeIndex.value + 1) % items.length
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    e.stopPropagation()
    if (items.length) activeIndex.value = (activeIndex.value - 1 + items.length) % items.length
    return
  }
  if ((e.key === 'Home' || e.key === 'End') && e.target !== searchInput.value) {
    e.preventDefault()
    e.stopPropagation()
    activeIndex.value = e.key === 'Home' ? 0 : Math.max(0, items.length - 1)
    return
  }
  if (e.key === 'Enter' || (e.key === ' ' && !props.searchable)) {
    e.preventDefault()
    e.stopPropagation()
    const opt = items[activeIndex.value]
    if (opt) choose(opt.value)
  }
}

function onDocKeydown(e: KeyboardEvent) {
  if (!open.value) return
  const target = e.target as Node | null
  if (target !== document && !root.value?.contains(target) && !panel.value?.contains(target)) return
  handleListKeydown(e)
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (props.disabled || open.value || e.isComposing || e.keyCode === 229) return
  if (e.ctrlKey || e.metaKey || e.altKey) return
  if (['ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' '].includes(e.key)) {
    e.preventDefault()
    e.stopPropagation()
    void openPanel(e.key === 'Home' ? 'first' : e.key === 'End' ? 'last' : undefined)
  }
}

watch(
  () => rows.value.length,
  (len) => {
    if (activeIndex.value >= len) activeIndex.value = Math.max(0, len - 1)
  },
)
watch([activeIndex, rows], () => { if (open.value) void nextTick(revealActiveOption) })
watch(() => props.disabled, disabled => { if (disabled) closePanel() }, { flush: 'sync' })

onMounted(() => {
  document.addEventListener('click', onDocClick)
  // Capture sees the opening key while still closed, before the trigger opens;
  // a bubble listener would immediately select again with that same Enter.
  document.addEventListener('keydown', onDocKeydown, true)
  window.addEventListener('resize', onScrollOrResize)
  window.addEventListener('scroll', onScrollOrResize, true)
})
onBeforeUnmount(() => {
  if (closeOpenPanel === closePanel) closeOpenPanel = null
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onDocKeydown, true)
  window.removeEventListener('resize', onScrollOrResize)
  window.removeEventListener('scroll', onScrollOrResize, true)
})
</script>

<template>
  <div ref="root" class="relative">
    <button
      ref="trigger"
      type="button"
      class="app-select-trigger flex w-full items-center gap-2 rounded-md border text-left transition"
      :class="[
        variant === 'ghost' ? 'app-select-trigger--ghost px-2 text-txt2' : 'bg-base px-3 py-2 text-txt',
        size === 'sm' ? 'app-select-trigger--sm' : '',
        size === 'sm' ? 'text-[12px]' : 'text-sm',
        invalid ? 'border-err' : open ? variant === 'ghost' ? 'border-accent/35 bg-accent-dim/40' : 'border-accent' : variant === 'ghost' ? 'border-transparent bg-transparent hover:border-line hover:bg-elevated/60' : 'border-line hover:border-line-strong',
        disabled ? 'cursor-default text-txt3' : '',
      ]"
      :disabled="disabled"
      :role="searchable ? undefined : 'combobox'"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="open ? listboxId : undefined"
      :aria-activedescendant="searchable ? undefined : activeDescendant"
      :aria-invalid="invalid || undefined"
      :aria-label="ariaLabel"
      data-test="app-select-trigger"
      @click="togglePanel"
      @keydown="onTriggerKeydown"
    >
      <slot name="leading" :value="modelValue" />
      <span class="min-w-0 flex-1 truncate" :class="isPlaceholder ? 'text-txt3' : ''">
        {{ selectedLabel }}
      </span>
      <Icon :interactive="false"
        name="chevron-down"
        :size="14"
        class="app-select-chevron shrink-0 text-txt3"
        :class="{ 'is-open': open }"
      />
    </button>

    <Teleport to="body">
      <Transition name="overlay-pop" @before-leave="hideLeavingPanel">
        <div
          v-if="open"
          ref="panel"
          class="app-select-panel flex flex-col overflow-hidden"
          :class="variant === 'ghost' ? 'app-select-panel--ghost rounded-md border border-line bg-surface' : 'card'"
          :aria-label="ariaLabel"
          data-test="app-select-panel"
          :style="panelStyle"
          @click.stop
        >
          <div v-if="searchable" class="shrink-0 border-b border-line/70 p-2">
            <input
              ref="searchInput"
              v-model="search"
              type="search"
              role="combobox"
              aria-autocomplete="list"
              :aria-expanded="open"
              :aria-controls="listboxId"
              :aria-activedescendant="activeDescendant"
              class="w-full rounded-md border border-line bg-surface px-2.5 py-1.5 text-[12px] text-txt outline-none transition placeholder:text-txt3 focus:border-accent"
              :placeholder="searchPlaceholder || t('common.search.optionPlaceholder')"
              :aria-label="searchPlaceholder || t('common.search.optionPlaceholder')"
              autocomplete="off"
              data-test="app-select-search"
              @click.stop
              @input="activeIndex = 0"
              @keydown="handleListKeydown"
            />
          </div>

          <div
            :id="listboxId"
            class="scroll-area min-h-0 flex-1 overflow-y-auto p-1"
            role="listbox"
            :aria-label="ariaLabel"
          >
            <button
              v-for="(row, i) in rows"
              :key="(row.custom ? 'custom:' : 'opt:') + row.value"
              :id="optionId(i)"
              type="button"
              role="option"
              tabindex="-1"
              class="app-select-option flex w-full items-center gap-2 rounded-md px-2.5 text-left transition"
              :class="[
                variant === 'ghost' ? 'app-select-option--ghost py-1.5' : 'py-2',
                size === 'sm' ? 'text-[12px]' : 'text-sm',
                row.value === modelValue ? 'text-accent-2' : 'text-txt2',
                i === activeIndex ? variant === 'ghost' ? 'bg-accent-dim' : 'bg-accent-dim text-txt' : 'hover:bg-elevated',
              ]"
              :aria-selected="row.value === modelValue"
              :data-test="row.custom ? 'app-select-custom' : `app-select-option-${row.value}`"
              @click.stop="choose(row.value)"
            >
              <slot name="option-leading" :option="row" />
              <span v-if="row.custom" class="min-w-0 flex-1 truncate">
                {{ t('common.search.useTypedValue', { value: row.value }) }}
              </span>
              <span v-else class="min-w-0 flex-1 truncate" v-html="highlight(row.label)" />
              <span
                v-if="row.hint"
                class="min-w-0 shrink-0 truncate text-[11px] text-txt3"
                v-html="highlight(row.hint)"
              />
              <Icon
                v-if="row.value === modelValue"
                name="check"
                :size="14"
                class="shrink-0 text-accent-2"
              />
            </button>
            <p
              v-if="!rows.length"
              class="px-2.5 py-4 text-center text-[12px] text-txt3"
              :data-test="loading ? 'app-select-loading' : 'app-select-empty'"
            >
              {{ loading ? t('common.search.loadingOptions') : emptyText || t('common.empty.noMatchingOptions') }}
            </p>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.app-select-panel :deep(mark) {
  background: rgb(var(--c-accent) / 0.35);
  color: inherit;
  padding: 0 1px;
}
.app-select-trigger--ghost {
  min-height: 36px;
  padding-block: 5px;
  outline: none;
}
.app-select-trigger--ghost.app-select-trigger--sm {
  min-height: 30px;
  padding-block: 4px;
}
.app-select-trigger--ghost:focus-visible {
  border-color: rgb(var(--c-accent-2) / .5);
  box-shadow: 0 0 0 2px rgb(var(--c-accent-2) / .1);
}
.app-select-trigger--ghost:disabled {
  opacity: .6;
}
.app-select-panel--ghost {
  box-shadow: 0 6px 20px rgb(0 0 0 / .1);
}
.app-select-option--ghost {
  min-height: 32px;
}
@media (max-width: 767px) {
  .app-select-trigger--ghost,
  .app-select-trigger--ghost.app-select-trigger--sm,
  .app-select-option--ghost {
    min-height: 44px;
  }
}
.app-select-chevron {
  transition: transform var(--dur-ui) var(--ease-out-expo);
}
.app-select-chevron.is-open {
  transform: rotate(180deg);
}
</style>

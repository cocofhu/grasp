<script setup lang="ts">
/**
 * Template picker for Agent create wizard basics.
 * Interaction mirrors HomePipelineSelect: Teleport to body, pinned search,
 * list-only scroll (max-height 220px), 4px ghost scrollbar — no create footer.
 * Selecting a row never writes draft.name (plan g1.4).
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export type AgentTemplateOption = {
  id: string
  name: string
  subtitle?: string
}

const props = withDefaults(
  defineProps<{
    options: AgentTemplateOption[]
    modelValue: string
    disabled?: boolean
  }>(),
  { disabled: false },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

const { t } = useI18n()

const open = ref(false)
const search = ref('')
const activeIndex = ref(0)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const listEl = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const panelStyle = ref<Record<string, string>>({})
let scrollIdleTimer: ReturnType<typeof setTimeout> | null = null

function triggerLabel(opt: AgentTemplateOption | undefined): string {
  if (!opt) return ''
  if (opt.id === 'blank') return opt.name
  const sub = (opt.subtitle || '').trim()
  return sub ? `${opt.name} · ${sub}` : opt.name
}

const selected = computed(() => props.options.find((o) => o.id === props.modelValue))

const selectedName = computed(() => {
  if (!props.options.length) return t('pages.agentStudio.wizard.basics.templateBlank')
  return triggerLabel(selected.value) || t('pages.agentStudio.wizard.basics.templateBlank')
})

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return props.options
  return props.options.filter(
    (o) =>
      o.name.toLowerCase().includes(q) ||
      (o.subtitle || '').toLowerCase().includes(q) ||
      o.id.toLowerCase().includes(q),
  )
})

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) =>
    ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;',
    })[c] as string,
  )
}

function highlightName(name: string, q: string): string {
  if (!q) return escapeHtml(name)
  const lower = name.toLowerCase()
  const iq = q.toLowerCase()
  const i = lower.indexOf(iq)
  if (i < 0) return escapeHtml(name)
  return (
    escapeHtml(name.slice(0, i)) +
    '<mark>' +
    escapeHtml(name.slice(i, i + q.length)) +
    '</mark>' +
    escapeHtml(name.slice(i + q.length))
  )
}

/** Place panel directly below the trigger (gap 6px). Teleport escapes wizard overflow. */
function placePanelBelow() {
  const trig = trigger.value
  if (!trig) return
  const r = trig.getBoundingClientRect()
  const width = Math.min(320, Math.min(window.innerWidth * 0.78, window.innerWidth - 16))
  let left = r.left
  left = Math.max(8, Math.min(left, window.innerWidth - width - 8))
  const top = r.bottom + 6
  panelStyle.value = {
    position: 'fixed',
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
  }
}

function onScrollOrResize() {
  if (open.value) placePanelBelow()
}

async function openPanel() {
  if (props.disabled) return
  open.value = true
  search.value = ''
  const idx = props.options.findIndex((o) => o.id === props.modelValue)
  activeIndex.value = Math.max(0, idx)
  await nextTick()
  placePanelBelow()
  searchInput.value?.focus()
}

function closePanel() {
  open.value = false
  panelStyle.value = {}
  if (listEl.value) listEl.value.classList.remove('is-scrolling')
}

function togglePanel(e: MouseEvent) {
  e.preventDefault()
  e.stopPropagation()
  if (props.disabled) return
  if (open.value) closePanel()
  else void openPanel()
}

/** plan g1.4 — only emit template id; never touch agent name */
function choose(id: string) {
  emit('update:modelValue', id)
  closePanel()
  trigger.value?.focus()
}

function onDocMouseDown(e: MouseEvent) {
  if (!open.value) return
  const target = e.target as Node
  if (root.value?.contains(target) || panel.value?.contains(target)) return
  closePanel()
}

function onSearchInput() {
  activeIndex.value = 0
}

/** plan g1.3 — keyboard highlight scrolls only the list container, never ancestors */
function ensureActiveVisible() {
  const list = listEl.value
  if (!list) return
  const el = list.querySelector('.agent-template-select__opt--active') as HTMLElement | null
  if (!el) return
  const top = el.offsetTop
  const bottom = top + el.offsetHeight
  if (top < list.scrollTop) list.scrollTop = top
  else if (bottom > list.scrollTop + list.clientHeight) list.scrollTop = bottom - list.clientHeight
}

function onListScroll() {
  const list = listEl.value
  if (!list) return
  list.classList.add('is-scrolling')
  if (scrollIdleTimer) clearTimeout(scrollIdleTimer)
  scrollIdleTimer = setTimeout(() => {
    list.classList.remove('is-scrolling')
  }, 400)
}

function onSearchKeydown(e: KeyboardEvent) {
  const items = filtered.value
  if (e.key === 'Escape') {
    e.preventDefault()
    closePanel()
    trigger.value?.focus()
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (!items.length) return
    activeIndex.value = Math.min(activeIndex.value + 1, items.length - 1)
    void nextTick(() => ensureActiveVisible())
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (!items.length) return
    activeIndex.value = Math.max(0, activeIndex.value - 1)
    void nextTick(() => ensureActiveVisible())
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    if (items[activeIndex.value]) choose(items[activeIndex.value].id)
  }
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (props.disabled) return
  if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    if (!open.value) void openPanel()
  }
  if (e.key === 'Escape' && open.value) {
    e.preventDefault()
    closePanel()
  }
}

function onOptMouseDown(e: MouseEvent, id: string) {
  e.preventDefault()
  e.stopPropagation()
  choose(id)
}

watch(
  () => filtered.value.length,
  (len) => {
    if (activeIndex.value >= len) activeIndex.value = Math.max(0, len - 1)
  },
)

onMounted(() => {
  document.addEventListener('mousedown', onDocMouseDown)
  window.addEventListener('resize', onScrollOrResize)
  window.addEventListener('scroll', onScrollOrResize, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocMouseDown)
  window.removeEventListener('resize', onScrollOrResize)
  window.removeEventListener('scroll', onScrollOrResize, true)
  if (scrollIdleTimer) clearTimeout(scrollIdleTimer)
})
</script>

<template>
  <div
    ref="root"
    class="agent-template-select"
    data-testid="agent-template-select"
    :class="{ 'agent-template-select--open': open }"
  >
    <button
      ref="trigger"
      id="agent-template-select"
      type="button"
      class="agent-template-select__trigger"
      data-testid="agent-template-select-trigger"
      :disabled="disabled"
      aria-haspopup="listbox"
      :aria-expanded="open"
      aria-controls="agent-template-select-panel"
      @mousedown.prevent
      @click="togglePanel"
      @keydown="onTriggerKeydown"
    >
      <span class="agent-template-select__label" :title="selectedName">{{ selectedName }}</span>
      <span class="agent-template-select__chev" aria-hidden="true" />
    </button>

    <!-- plan g1.2 — Teleport to body so wizard pane overflow cannot clip or steal scroll -->
    <Teleport to="body">
      <div
        v-if="open"
        ref="panel"
        id="agent-template-select-panel"
        class="agent-template-select__panel"
        role="presentation"
        data-testid="agent-template-select-panel"
        data-placement="below"
        :style="panelStyle"
        @mousedown.stop
        @click.stop
      >
        <div class="agent-template-select__search-wrap">
          <input
            ref="searchInput"
            v-model="search"
            type="search"
            class="agent-template-select__search"
            data-testid="agent-template-select-search"
            :placeholder="t('pages.agentStudio.wizard.basics.templateSearch')"
            autocomplete="off"
            :aria-label="t('pages.agentStudio.wizard.basics.templateSearch')"
            @input="onSearchInput"
            @keydown="onSearchKeydown"
            @click.stop
          />
        </div>
        <div
          ref="listEl"
          class="agent-template-select__list"
          role="listbox"
          data-testid="agent-template-select-list"
          :aria-label="t('pages.agentStudio.wizard.basics.templateLabel')"
          @scroll="onListScroll"
        >
          <template v-if="filtered.length">
            <button
              v-for="(o, i) in filtered"
              :key="o.id"
              type="button"
              class="agent-template-select__opt"
              role="option"
              :class="{
                'agent-template-select__opt--current': o.id === modelValue,
                'agent-template-select__opt--active': i === activeIndex,
              }"
              :aria-selected="i === activeIndex"
              :data-testid="`agent-template-select-option-${o.id}`"
              @mousedown="onOptMouseDown($event, o.id)"
              @mouseover="activeIndex = i"
            >
              <span
                class="agent-template-select__opt-name"
                v-html="highlightName(o.name, search.trim())"
              />
              <span
                v-if="o.subtitle"
                class="agent-template-select__opt-sub"
                v-html="highlightName(o.subtitle, search.trim())"
              />
            </button>
          </template>
          <div
            v-else
            class="agent-template-select__empty"
            data-testid="agent-template-select-empty"
          >
            {{ t('pages.agentStudio.wizard.basics.templateEmpty') }}
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.agent-template-select {
  position: relative;
  max-width: 38rem;
  min-width: 0;
  z-index: 30;
}

.agent-template-select__trigger {
  width: 100%;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: transparent;
  color: rgb(var(--c-accent-2));
  padding: 0 10px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  text-align: left;
  transition: border-color 0.15s ease;
}

.agent-template-select__trigger:hover:not(:disabled),
.agent-template-select--open .agent-template-select__trigger {
  border-color: rgb(var(--c-line-strong));
}

.agent-template-select__trigger:disabled {
  cursor: default;
  color: rgb(var(--c-txt3));
}

.agent-template-select__label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.agent-template-select__chev {
  width: 0;
  height: 0;
  border: 4px solid transparent;
  border-top-color: rgb(var(--c-txt3));
  flex-shrink: 0;
}

/* plan g1.2 / g1.3 — fixed Teleport panel; search pinned; list scrolls alone */
.agent-template-select__panel {
  position: fixed;
  left: 0;
  top: calc(100% + 6px);
  width: min(320px, 78vw);
  border: 1px solid rgb(var(--c-line-strong));
  border-radius: 12px;
  overflow: hidden;
  background: rgb(var(--c-elevated));
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.45);
  z-index: 60;
  display: flex;
  flex-direction: column;
  max-height: min(360px, calc(100vh - 24px));
  pointer-events: auto;
}

.agent-template-select__search-wrap {
  padding: 8px;
  border-bottom: 1px solid rgb(var(--c-line) / 0.7);
  flex-shrink: 0;
}

.agent-template-select__search {
  width: 100%;
  height: 32px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt));
  padding: 0 10px;
  font-size: 13px;
  outline: none;
}

.agent-template-select__search::placeholder {
  color: rgb(var(--c-txt3));
}

.agent-template-select__search:focus {
  border-color: rgb(var(--c-accent));
}

/* plan g1.3 — list-only scroll max-height 220px; inherits 4px ghost scrollbar */
.agent-template-select__list {
  position: relative;
  max-height: 220px;
  overflow-x: hidden;
  overflow-y: auto;
  padding: 4px;
  flex: 1 1 auto;
  min-height: 0;
  overscroll-behavior: contain;
}

.agent-template-select__opt {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 2px;
  text-align: left;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: rgb(var(--c-txt));
  padding: 8px 10px;
  font-size: 13px;
  cursor: pointer;
}

.agent-template-select__opt-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-template-select__opt-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  color: rgb(var(--c-txt2));
}

.agent-template-select__opt:hover,
.agent-template-select__opt--active {
  background: rgb(var(--c-accent) / 0.16);
  color: rgb(var(--c-txt));
}

.agent-template-select__opt--current {
  color: rgb(var(--c-accent-2));
}

.agent-template-select__opt :deep(mark) {
  background: rgb(var(--c-accent) / 0.35);
  color: inherit;
  padding: 0 1px;
}

.agent-template-select__empty {
  padding: 16px 10px;
  text-align: center;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}

@media (max-width: 520px) {
  .agent-template-select {
    max-width: none;
  }
}
</style>

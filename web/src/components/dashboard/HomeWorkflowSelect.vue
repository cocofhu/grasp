<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export type HomeWorkflowOption = { id: string; name: string; projectName?: string }

const props = withDefaults(
  defineProps<{
    workflows: HomeWorkflowOption[]
    modelValue: string
    disabled?: boolean
  }>(),
  { disabled: false },
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'create'): void
}>()

const { t } = useI18n()

const open = ref(false)
const search = ref('')
const activeIndex = ref(0)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
/** Fixed position below trigger (Teleport escapes .home-shell__content overflow). */
const panelStyle = ref<Record<string, string>>({})

function triggerLabel(p: HomeWorkflowOption | undefined): string {
  if (!p) return ''
  const project = (p.projectName || '').trim()
  // No empty " · 工作流名" prefix when project name is missing (g1.3 / F4).
  return project ? `${project} · ${p.name}` : p.name
}

const selectedName = computed(() => {
  if (!props.workflows.length) return t('pages.dashboard.noWorkflowShort')
  const hit = props.workflows.find((p) => p.id === props.modelValue)
  if (!hit) return t('pages.dashboard.noWorkflowShort')
  return triggerLabel(hit)
})

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return props.workflows
  return props.workflows.filter(
    (p) =>
      p.name.toLowerCase().includes(q) || (p.projectName || '').toLowerCase().includes(q),
  )
})

/** Create footer is the last keyboard target (index === filtered.length). */
const createIndex = computed(() => filtered.value.length)
const createActive = computed(() => activeIndex.value >= createIndex.value)

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) =>
  ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[c] as string)
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

/** Place panel directly below the trigger (gap 6px). No upward flip. */
function placePanelBelow() {
  const trig = trigger.value
  if (!trig) return
  const r = trig.getBoundingClientRect()
  const width = Math.min(320, Math.min(window.innerWidth * 0.78, window.innerWidth - 16))
  let left = r.left
  left = Math.max(8, Math.min(left, window.innerWidth - width - 8))
  // plan g1.1 / g1.2 — top = trigger bottom + 6px (not bottom: calc(100% + 6px))
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
  // plan g1.3 — empty workflows still open (create-only panel); sending disables via prop
  if (props.disabled) return
  open.value = true
  search.value = ''
  const idx = props.workflows.findIndex((p) => p.id === props.modelValue)
  activeIndex.value = props.workflows.length ? Math.max(0, idx) : createIndex.value
  await nextTick()
  placePanelBelow()
  searchInput.value?.focus()
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

function choose(id: string) {
  emit('update:modelValue', id)
  closePanel()
  trigger.value?.focus()
}

/** plan g2.1 — emit create; close panel so Dashboard can open HomeCreateBaselineModal */
function emitCreate() {
  closePanel()
  emit('create')
  trigger.value?.focus()
}

function onDocClick(e: MouseEvent) {
  if (!open.value) return
  const target = e.target as Node
  if (root.value?.contains(target) || panel.value?.contains(target)) return
  closePanel()
}

function onSearchInput() {
  // Prefer first match; if none, land on create footer (plan g1.2 / g1.3)
  activeIndex.value = filtered.value.length ? 0 : createIndex.value
}

function onSearchKeydown(e: KeyboardEvent) {
  const items = filtered.value
  const maxIdx = createIndex.value // create footer
  if (e.key === 'Escape') {
    e.preventDefault()
    closePanel()
    trigger.value?.focus()
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (!items.length) {
      activeIndex.value = maxIdx
      return
    }
    activeIndex.value = Math.min(activeIndex.value + 1, maxIdx)
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (!items.length) {
      activeIndex.value = maxIdx
      return
    }
    activeIndex.value = Math.max(0, activeIndex.value - 1)
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    // plan g1.2 — no matches: Enter triggers create; last arrow target is create
    if (!items.length || activeIndex.value >= items.length) {
      emitCreate()
      return
    }
    choose(items[activeIndex.value].id)
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

watch(
  () => filtered.value.length,
  (len) => {
    // Allow activeIndex === len (create footer)
    if (activeIndex.value > len) activeIndex.value = len
  },
)

onMounted(() => {
  document.addEventListener('click', onDocClick)
  window.addEventListener('resize', onScrollOrResize)
  window.addEventListener('scroll', onScrollOrResize, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  window.removeEventListener('resize', onScrollOrResize)
  window.removeEventListener('scroll', onScrollOrResize, true)
})
</script>

<template>
  <div
    ref="root"
    class="home-workflow-select"
    data-testid="home-workflow-select"
    :class="{ 'home-workflow-select--open': open }"
  >
    <button
      ref="trigger"
      id="home-workflow-select"
      type="button"
      class="home-workflow-select__trigger"
      data-testid="home-workflow-select-trigger"
      :disabled="disabled"
      aria-haspopup="listbox"
      :aria-expanded="open"
      aria-controls="home-workflow-select-panel"
      @click="togglePanel"
      @keydown="onTriggerKeydown"
    >
      <span class="home-workflow-select__label" :title="selectedName">{{ selectedName }}</span>
      <span class="home-workflow-select__chev" aria-hidden="true" />
    </button>

    <!-- Teleport to body so .home-shell__content overflow-y-auto cannot clip the downward panel (g1.2) -->
    <Teleport to="body">
      <div
        v-if="open"
        ref="panel"
        id="home-workflow-select-panel"
        class="home-workflow-select__panel"
        role="presentation"
        data-testid="home-workflow-select-panel"
        data-placement="below"
        :style="panelStyle"
        @click.stop
      >
        <div class="home-workflow-select__search-wrap">
          <input
            ref="searchInput"
            v-model="search"
            type="search"
            class="home-workflow-select__search"
            data-testid="home-workflow-select-search"
            :placeholder="t('common.search.workflowPlaceholder')"
            autocomplete="off"
            :aria-label="t('common.search.workflowPlaceholder')"
            @input="onSearchInput"
            @keydown="onSearchKeydown"
            @click.stop
          />
        </div>
        <div
          class="home-workflow-select__list"
          role="listbox"
          :aria-label="t('pages.dashboard.pickWorkflow')"
        >
          <template v-if="filtered.length">
            <button
              v-for="(p, i) in filtered"
              :key="p.id"
              type="button"
              class="home-workflow-select__opt"
              role="option"
              :class="{
                'home-workflow-select__opt--current': p.id === modelValue,
                'home-workflow-select__opt--active': i === activeIndex,
              }"
              :aria-selected="i === activeIndex"
              :data-testid="`home-workflow-select-option-${p.id}`"
              @click.stop="choose(p.id)"
            >
              <span
                class="home-workflow-select__opt-name"
                v-html="highlightName(p.name, search.trim())"
              />
              <span
                v-if="p.projectName"
                class="home-workflow-select__opt-project"
                v-html="highlightName(p.projectName, search.trim())"
              />
            </button>
          </template>
          <div
            v-else
            class="home-workflow-select__empty"
            data-testid="home-workflow-select-empty"
          >
            {{ t('common.empty.noMatchingWorkflows') }}
          </div>
        </div>
        <!-- plan g1.1 — sticky create footer; not a selectable workflow option -->
        <button
          type="button"
          class="home-workflow-select__create"
          data-testid="home-workflow-select-create"
          :class="{ 'home-workflow-select__create--active': createActive }"
          @click.stop="emitCreate"
        >
          <span class="home-workflow-select__create-plus" aria-hidden="true">+</span>
          <span>{{ t('pages.dashboard.create.addCard') }}</span>
        </button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.home-workflow-select {
  position: relative;
  max-width: 14rem;
  min-width: 0;
}

.home-workflow-select__trigger {
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

.home-workflow-select__trigger:hover:not(:disabled),
.home-workflow-select--open .home-workflow-select__trigger {
  border-color: rgb(var(--c-line-strong));
}

.home-workflow-select__trigger:disabled {
  cursor: default;
  color: rgb(var(--c-txt3));
}

.home-workflow-select__label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.home-workflow-select__chev {
  width: 0;
  height: 0;
  border: 4px solid transparent;
  border-top-color: rgb(var(--c-txt3));
  flex-shrink: 0;
}

/* plan g1.1 — panel opens below trigger (top), never bottom/upward.
   Teleported panel uses fixed coords from placePanelBelow(); keep top as
   the documented downward default if styles apply without inline overrides.
   Flex column so create footer stays pinned while the list scrolls. */
.home-workflow-select__panel {
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
}

.home-workflow-select__search-wrap {
  padding: 8px;
  border-bottom: 1px solid rgb(var(--c-line) / 0.7);
  flex-shrink: 0;
}

.home-workflow-select__search {
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

.home-workflow-select__search::placeholder {
  color: rgb(var(--c-txt3));
}

.home-workflow-select__search:focus {
  border-color: rgb(var(--c-accent));
}

.home-workflow-select__list {
  max-height: 220px;
  overflow: auto;
  padding: 4px;
  flex: 1 1 auto;
  min-height: 0;
}

.home-workflow-select__opt {
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

.home-workflow-select__opt-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.home-workflow-select__opt-project {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  color: rgb(var(--c-txt2));
}

.home-workflow-select__opt:hover,
.home-workflow-select__opt--active {
  background: rgb(var(--c-accent) / 0.16);
  color: rgb(var(--c-txt));
}

.home-workflow-select__opt--current {
  color: rgb(var(--c-accent-2));
}

.home-workflow-select__opt :deep(mark) {
  background: rgb(var(--c-accent) / 0.35);
  color: inherit;
  padding: 0 1px;
}

.home-workflow-select__empty {
  padding: 16px 10px;
  text-align: center;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}

/* plan g1.1 — divider + plus/label; pinned under scrollable list; no aria-selected */
.home-workflow-select__create {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  flex-shrink: 0;
  border: 0;
  border-top: 1px solid rgb(var(--c-line) / 0.7);
  background: transparent;
  color: rgb(var(--c-txt));
  padding: 10px 14px;
  font-size: 13px;
  cursor: pointer;
  text-align: left;
}

.home-workflow-select__create:hover,
.home-workflow-select__create--active {
  background: rgb(var(--c-accent) / 0.1);
}

.home-workflow-select__create-plus {
  width: 18px;
  height: 18px;
  border-radius: 6px;
  border: 1px solid rgb(var(--c-line));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  line-height: 1;
  color: rgb(var(--c-txt2));
  flex-shrink: 0;
}

@media (max-width: 520px) {
  .home-workflow-select {
    max-width: none;
  }
}
</style>

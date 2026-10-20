<script lang="ts">
let closeOpenPanel: (() => void) | null = null
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import VendorMark from './VendorMark.vue'
import type { CliCatalogModel, CliModelTag } from '@/lib/api/apiTypes'
import { cliCatalogFailed, loadCliModels } from '@/lib/agent/cliCatalog'
import {
  TAG_ORDER,
  buildView,
  contextRatio,
  formatContext,
  highlight,
  launchCommand,
  strengthKeys,
  vendorLabel,
} from '@/lib/agent/cliModelView'
import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'

const props = withDefaults(
  defineProps<{
    /** Model id passed as `--model`; empty means the CLI's own default. */
    modelValue: string
    backend: string
    /** full: list plus a detail pane. compact: list only, details inline. */
    variant?: 'full' | 'compact'
    disabled?: boolean
    /**
     * Hide the trigger subtitle under 「自动」.
     * Onboarding uses this so the default state shows only that word.
     * Agent studio and runtime config leave it off and keep the subtitle.
     */
    hideAutoSub?: boolean
  }>(),
  { variant: 'full', disabled: false, hideAutoSub: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const { t } = useI18n()

const TAG_ICON: Record<CliModelTag, string> = { vision: 'eye', reasoning: 'brain', fast: 'trigger' }
const FULL_WIDTH = 600
const MARGIN = 8
const GAP = 6
const MAX_HEIGHT = 440

const models = ref<CliCatalogModel[]>([])
const loading = ref(false)
const failed = ref(false)
const open = ref(false)
const query = ref('')
const vendor = ref('')
const active = ref(0)
const flash = ref(false)
const copied = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const panelStyle = ref<Record<string, string>>({})
const narrow = ref(false)

watch(
  () => props.backend,
  async (backend) => {
    models.value = []
    failed.value = false
    if (!backend) return
    loading.value = true
    const list = await loadCliModels(backend)
    if (backend !== props.backend) return
    models.value = list
    failed.value = cliCatalogFailed(backend)
    loading.value = false
  },
  { immediate: true },
)

const cliLabel = computed(() => ACP_BACKENDS.find((b) => b.id === props.backend)?.label || props.backend)
const current = computed(() => models.value.find((m) => m.id === props.modelValue) ?? null)
const unlisted = computed(() => !!props.modelValue && !current.value && !loading.value && models.value.length > 0)
const command = computed(() => launchCommand(props.backend, props.modelValue))
const split = computed(() => props.variant === 'full' && !narrow.value)

const view = computed(() => buildView(models.value, query.value, vendor.value))
const typed = computed(() => {
  const q = query.value.trim()
  return q && !view.value.exact ? q : ''
})

type Row = { kind: 'auto' } | { kind: 'model'; model: CliCatalogModel } | { kind: 'typed'; id: string }
const rows = computed<Row[]>(() => {
  const out: Row[] = []
  if (!query.value.trim()) out.push({ kind: 'auto' })
  for (const m of view.value.flat) out.push({ kind: 'model', model: m })
  if (typed.value) out.push({ kind: 'typed', id: typed.value })
  return out
})
const activeRow = computed<Row | null>(() => rows.value[active.value] ?? null)
const noResults = computed(() => !!query.value.trim() && !view.value.flat.length)

function rowIndexOf(id: string): number {
  return rows.value.findIndex((r) => (id ? r.kind === 'model' && r.model.id === id : r.kind === 'auto'))
}

/** A vendor heading goes above the first row of each vendor, unless a query ranks across them. */
function startsVendorGroup(i: number): boolean {
  if (query.value.trim()) return false
  const r = rows.value[i]
  const prev = rows.value[i - 1]
  if (r?.kind !== 'model') return false
  return prev?.kind !== 'model' || prev.model.vendor !== r.model.vendor
}

function rowValue(r: Row): string {
  return r.kind === 'auto' ? '' : r.kind === 'model' ? r.model.id : r.id
}

function tagsOf(m: CliCatalogModel | null): CliModelTag[] {
  return TAG_ORDER.filter((tag) => m?.tags?.includes(tag))
}

function placePanel() {
  const trig = trigger.value
  if (!trig) return
  const r = trig.getBoundingClientRect()
  narrow.value = window.innerWidth < FULL_WIDTH + MARGIN * 2
  const width = split.value ? FULL_WIDTH : Math.max(r.width, 320)
  const height = Math.min(panel.value?.offsetHeight || MAX_HEIGHT, MAX_HEIGHT)
  const below = window.innerHeight - r.bottom - GAP - MARGIN
  const above = r.top - GAP - MARGIN
  const flipUp = below < height && above > below
  const top = flipUp ? Math.max(MARGIN, r.top - GAP - height) : r.bottom + GAP
  const left = Math.max(MARGIN, Math.min(r.left, window.innerWidth - width - MARGIN))
  panelStyle.value = {
    position: 'fixed',
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(Math.min(width, window.innerWidth - MARGIN * 2))}px`,
    maxHeight: `${Math.round(Math.min(MAX_HEIGHT, flipUp ? above : below))}px`,
    zIndex: '60',
  }
}

async function openPanel() {
  if (props.disabled) return
  if (closeOpenPanel && closeOpenPanel !== closePanel) closeOpenPanel()
  closeOpenPanel = closePanel
  query.value = ''
  vendor.value = ''
  open.value = true
  active.value = Math.max(0, rowIndexOf(props.modelValue))
  await nextTick()
  placePanel()
  search.value?.focus()
  scrollActive()
}

function closePanel(refocus = false) {
  if (closeOpenPanel === closePanel) closeOpenPanel = null
  open.value = false
  if (refocus) trigger.value?.focus()
}

function toggle(e: MouseEvent) {
  e.stopPropagation()
  if (open.value) closePanel()
  else void openPanel()
}

let flashTimer: ReturnType<typeof setTimeout> | undefined
function pick(value: string) {
  const next = value.trim()
  if (next !== props.modelValue) {
    emit('update:modelValue', next)
    flash.value = false
    clearTimeout(flashTimer)
    void nextTick(() => {
      flash.value = true
      flashTimer = setTimeout(() => (flash.value = false), 650)
    })
  }
  closePanel(true)
}

function scrollActive() {
  void nextTick(() => {
    const row = panel.value?.querySelector(`[data-row="${active.value}"]`)
    row?.scrollIntoView({ block: 'nearest' })
    panel.value?.querySelector('[data-testid="cli-model-inline-detail"]')?.scrollIntoView({ block: 'nearest' })
  })
}

function cycleVendor(step: number) {
  const ids = ['', ...view.value.vendors.map((v) => v.id)]
  if (ids.length < 2) return
  const at = ids.indexOf(vendor.value)
  vendor.value = ids[(at + step + ids.length) % ids.length]!
}

function onKey(e: KeyboardEvent) {
  const n = rows.value.length
  switch (e.key) {
    case 'ArrowDown':
      if (n) active.value = (active.value + 1) % n
      break
    case 'ArrowUp':
      if (n) active.value = (active.value - 1 + n) % n
      break
    case 'ArrowLeft':
    case 'ArrowRight':
      if (query.value) return
      cycleVendor(e.key === 'ArrowRight' ? 1 : -1)
      break
    case 'Enter': {
      const r = activeRow.value
      if (r) pick(rowValue(r))
      break
    }
    case 'Escape':
      closePanel(true)
      break
    case 'Tab':
      break
    default:
      return
  }
  e.preventDefault()
  e.stopPropagation()
  scrollActive()
}

function onTriggerKey(e: KeyboardEvent) {
  if (open.value || props.disabled) return
  if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    void openPanel()
  }
}

watch([query, vendor], () => {
  active.value = query.value.trim() ? 0 : Math.max(0, rowIndexOf(props.modelValue))
  void nextTick(placePanel)
})

let copyTimer: ReturnType<typeof setTimeout> | undefined
async function copyFlag(id: string) {
  try {
    await navigator.clipboard.writeText(`--model ${id}`)
    copied.value = true
    clearTimeout(copyTimer)
    copyTimer = setTimeout(() => (copied.value = false), 1400)
  } catch {
    /* clipboard denied; the flag stays visible to copy by hand */
  }
}

function onDocDown(e: MouseEvent) {
  if (!open.value) return
  const target = e.target as Node
  if (root.value?.contains(target) || panel.value?.contains(target)) return
  closePanel()
}
function onViewport() {
  if (open.value) placePanel()
}
onMounted(() => {
  document.addEventListener('mousedown', onDocDown, true)
  window.addEventListener('resize', onViewport)
  window.addEventListener('scroll', onViewport, true)
})
onBeforeUnmount(() => {
  if (closeOpenPanel === closePanel) closeOpenPanel = null
  clearTimeout(flashTimer)
  clearTimeout(copyTimer)
  document.removeEventListener('mousedown', onDocDown, true)
  window.removeEventListener('resize', onViewport)
  window.removeEventListener('scroll', onViewport, true)
})

defineExpose({ focus: () => trigger.value?.focus() })
</script>

<template>
  <div ref="root" class="cli-model-select" data-testid="cli-model-select">
    <!-- Trigger -->
    <button
      ref="trigger"
      type="button"
      class="cms-trigger group flex w-full items-center gap-3 rounded-xl border bg-base px-3 text-left transition"
      :class="[
        variant === 'compact' ? 'min-h-[52px] py-2' : 'min-h-[56px] py-2.5',
        unlisted ? 'border-warn/60' : open ? 'border-accent' : 'border-line hover:border-line-strong',
        flash ? 'is-flash' : '',
        disabled ? 'cursor-not-allowed opacity-60' : '',
      ]"
      :disabled="disabled"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-label="t('pages.agentStudio.cliModel.label')"
      data-testid="cli-model-trigger"
      @click="toggle"
      @keydown="onTriggerKey"
    >
      <VendorMark v-if="!modelValue" :vendor="backend" backend auto :size="32" />
      <VendorMark v-else-if="unlisted" :vendor="backend" backend unknown :size="32" />
      <VendorMark v-else :vendor="current?.vendor || ''" :size="32" />

      <span class="min-w-0 flex-1">
        <template v-if="!modelValue">
          <span class="block truncate text-[13px] font-medium text-txt">{{ t('pages.agentStudio.cliModel.auto') }}</span>
          <span v-if="!hideAutoSub" class="block truncate text-[11px] text-txt3">{{ t('pages.agentStudio.cliModel.autoSub', { cli: cliLabel }) }}</span>
        </template>
        <template v-else-if="unlisted">
          <span class="block truncate text-[13px] font-medium text-txt">{{ t('pages.agentStudio.cliModel.unlistedName') }}</span>
          <span class="block truncate text-[11px] text-warn" data-testid="cli-model-unlisted">
            <span class="font-mono">{{ modelValue }}</span> · {{ t('pages.agentStudio.cliModel.unlisted') }}
          </span>
        </template>
        <template v-else>
          <span class="flex items-center gap-1.5">
            <span class="truncate text-[13px] font-medium text-txt">{{ current?.name || modelValue }}</span>
            <span v-if="current?.recommended" class="cms-badge">{{ t('pages.agentStudio.cliModel.recommended') }}</span>
          </span>
          <span class="block truncate text-[11px] text-txt3">
            <span class="font-mono">{{ modelValue }}</span>
            <template v-if="current?.context"> · {{ t('pages.agentStudio.cliModel.contextShort', { n: formatContext(current.context) }) }}</template>
          </span>
        </template>
      </span>

      <span v-if="current && variant === 'full'" class="hidden shrink-0 items-center gap-1 sm:flex">
        <span v-for="tag in tagsOf(current)" :key="tag" class="cms-tag-icon" :title="t(`pages.agentStudio.cliModel.tags.${tag}`)">
          <Icon :name="TAG_ICON[tag]" :size="12" />
        </span>
      </span>
      <Icon name="chevron-down" :size="14" class="cms-chevron shrink-0 text-txt3" :class="{ 'is-open': open }" />
    </button>

    <!-- What the CLI will be launched with -->
    <div class="mt-1.5 flex min-h-[20px] items-center gap-2 px-0.5 text-[11px] text-txt3">
      <Transition name="ui-fade" mode="out-in">
        <span :key="command" class="flex min-w-0 flex-1 items-center gap-1.5" data-testid="cli-model-command">
          <Icon name="terminal" :size="12" class="shrink-0" />
          <code class="truncate rounded bg-elevated px-1.5 py-0.5 font-mono text-[10.5px] text-txt2">{{ command }}</code>
          <span v-if="!modelValue" class="hidden truncate sm:inline">{{ t('pages.agentStudio.cliModel.commandAutoNote') }}</span>
        </span>
      </Transition>
      <button
        v-if="modelValue && !disabled"
        type="button"
        class="shrink-0 font-medium text-accent-2 transition hover:underline"
        data-testid="cli-model-reset"
        @click="pick('')"
      >
        {{ t('pages.agentStudio.cliModel.resetAuto') }}
      </button>
    </div>

    <!-- Panel -->
    <Teleport to="body">
      <Transition name="overlay-pop">
        <div
          v-if="open"
          ref="panel"
          class="card cms-panel flex flex-col overflow-hidden p-0 shadow-xl"
          :style="panelStyle"
          data-testid="cli-model-panel"
          @mousedown.stop
        >
          <!-- Search + vendor chips -->
          <div class="shrink-0 border-b border-line/70 px-3 pb-2 pt-2.5">
            <div class="flex items-center gap-2 rounded-lg border border-line bg-surface px-2.5 transition focus-within:border-accent">
              <Icon name="search" :size="13" class="shrink-0 text-txt3" />
              <input
                ref="search"
                v-model="query"
                class="min-w-0 flex-1 bg-transparent py-1.5 text-[12.5px] text-txt outline-none placeholder:text-txt3"
                :placeholder="t('pages.agentStudio.cliModel.searchPlaceholder')"
                role="combobox"
                aria-autocomplete="list"
                :aria-expanded="true"
                aria-controls="cli-model-listbox"
                :aria-activedescendant="`cli-model-row-${active}`"
                autocomplete="off"
                spellcheck="false"
                data-testid="cli-model-search"
                @keydown="onKey"
              />
              <kbd class="cms-kbd">Esc</kbd>
            </div>
            <div v-if="view.vendors.length" class="-mx-0.5 mt-2 flex flex-wrap gap-1.5 px-0.5 pb-0.5" data-testid="cli-model-vendors">
              <button
                type="button"
                class="cms-chip"
                :class="{ 'is-on': !vendor }"
                tabindex="-1"
                data-testid="cli-model-vendor-all"
                @click="vendor = ''"
              >
                {{ t('pages.agentStudio.cliModel.allVendors') }}
              </button>
              <button
                v-for="v in view.vendors"
                :key="v.id"
                type="button"
                class="cms-chip"
                :class="{ 'is-on': vendor === v.id }"
                tabindex="-1"
                :data-testid="`cli-model-vendor-${v.id}`"
                @click="vendor = vendor === v.id ? '' : v.id"
              >
                <VendorMark :vendor="v.id" :size="16" />
                {{ vendorLabel(v.id) }}
                <span class="text-txt3">{{ v.count }}</span>
              </button>
            </div>
          </div>

          <div class="flex min-h-0 flex-1">
            <!-- List -->
            <div
              id="cli-model-listbox"
              class="scroll-area min-h-0 flex-1 overflow-y-auto p-1.5"
              :class="split ? 'w-[320px] flex-none border-r border-line/70' : ''"
              role="listbox"
              :aria-label="t('pages.agentStudio.cliModel.label')"
            >
              <p v-if="failed" class="mx-1.5 mb-1.5 rounded-md bg-elevated px-2.5 py-1.5 text-[11px] leading-5 text-txt3" data-testid="cli-model-failed">
                {{ t('pages.agentStudio.cliModel.catalogFailed') }}
              </p>

              <!-- Loading -->
              <div v-if="loading" class="space-y-1.5 p-1" data-testid="cli-model-loading">
                <div v-for="i in 4" :key="i" class="app-skeleton__block h-9 rounded-lg" :style="{ opacity: 1 - i * 0.15 }" />
              </div>

              <template v-else>
                <!-- Recommended cards -->
                <template v-if="view.recommended.length">
                  <p class="cms-section">{{ t('pages.agentStudio.cliModel.recommended') }}</p>
                  <div class="grid grid-cols-2 gap-1.5 px-1 pb-1.5">
                    <button
                      v-for="m in view.recommended"
                      :key="m.id"
                      type="button"
                      tabindex="-1"
                      class="cms-card"
                      :class="{ 'is-selected': m.id === modelValue, 'is-active': activeRow?.kind === 'model' && activeRow.model.id === m.id }"
                      :data-testid="`cli-model-card-${m.id}`"
                      @mouseenter="active = rowIndexOf(m.id)"
                      @click="pick(m.id)"
                    >
                      <span class="flex items-start gap-2">
                        <VendorMark :vendor="m.vendor" :size="22" />
                        <span class="cms-card-name min-w-0 flex-1 text-[12.5px] font-medium leading-snug text-txt">{{ m.name }}</span>
                        <Icon v-if="m.id === modelValue" name="check" :size="13" class="shrink-0 text-accent-2" />
                      </span>
                      <span class="mt-auto flex items-center gap-1 pt-1.5 text-txt3">
                        <span v-for="tag in tagsOf(m)" :key="tag" class="cms-tag-icon is-sm" :title="t(`pages.agentStudio.cliModel.tags.${tag}`)">
                          <Icon :name="TAG_ICON[tag]" :size="11" />
                        </span>
                        <span v-if="m.context" class="ml-auto font-mono text-[10.5px]">{{ formatContext(m.context) }}</span>
                      </span>
                    </button>
                  </div>
                </template>

                <!-- Rows -->
                <template v-for="(r, i) in rows" :key="r.kind === 'model' ? `m:${r.model.id}` : r.kind">
                  <p
                    v-if="r.kind === 'model' && startsVendorGroup(i)"
                    class="cms-section flex items-center gap-1.5"
                  >
                    {{ vendorLabel(r.model.vendor) }}
                  </p>

                  <div
                    v-if="r.kind === 'auto'"
                    :id="`cli-model-row-${i}`"
                    :data-row="i"
                    role="option"
                    :aria-selected="!modelValue"
                    class="cms-row cms-row-auto"
                    :class="{ 'is-active': active === i, 'is-selected': !modelValue }"
                    data-testid="cli-model-option-auto"
                    @mouseenter="active = i"
                    @click="pick('')"
                  >
                    <span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-md bg-accent-dim text-accent-2">
                      <Icon name="sparkles" :size="12" />
                    </span>
                    <span class="min-w-0 flex-1 truncate text-[12.5px] text-txt">{{ t('pages.agentStudio.cliModel.autoRow') }}</span>
                    <Icon v-if="!modelValue" name="check" :size="14" class="shrink-0 text-accent-2" />
                  </div>

                  <div
                    v-else-if="r.kind === 'model'"
                    :id="`cli-model-row-${i}`"
                    :data-row="i"
                    role="option"
                    :aria-selected="r.model.id === modelValue"
                    class="cms-row"
                    :class="{ 'is-active': active === i, 'is-selected': r.model.id === modelValue }"
                    :data-testid="`cli-model-option-${r.model.id}`"
                    @mouseenter="active = i"
                    @click="pick(r.model.id)"
                  >
                    <VendorMark :vendor="r.model.vendor" :size="18" />
                    <span class="min-w-0 flex-1">
                      <span class="block truncate text-[12.5px] text-txt">
                        <template v-for="(s, k) in highlight(r.model.name, query)" :key="k"><mark v-if="s.hit" class="cms-hit">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template>
                      </span>
                      <span v-if="query.trim() || !split" class="block truncate font-mono text-[10.5px] text-txt3">
                        <template v-for="(s, k) in highlight(r.model.id, query)" :key="k"><mark v-if="s.hit" class="cms-hit">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template>
                      </span>
                    </span>
                    <span class="flex shrink-0 items-center gap-1 text-txt3">
                      <span v-for="tag in tagsOf(r.model)" :key="tag" class="cms-tag-icon is-sm" :title="t(`pages.agentStudio.cliModel.tags.${tag}`)">
                        <Icon :name="TAG_ICON[tag]" :size="11" />
                      </span>
                      <span v-if="r.model.context" class="w-9 text-right font-mono text-[10.5px]">{{ formatContext(r.model.context) }}</span>
                    </span>
                    <Icon v-if="r.model.id === modelValue" name="check" :size="14" class="shrink-0 text-accent-2" />
                    <span v-else class="w-[14px] shrink-0" />
                  </div>

                  <!-- Typed id, below the matches -->
                  <div
                    v-else-if="!noResults"
                    :id="`cli-model-row-${i}`"
                    :data-row="i"
                    role="option"
                    :aria-selected="false"
                    class="cms-row mt-1 border-t border-dashed border-line/80 pt-2"
                    :class="{ 'is-active': active === i }"
                    data-testid="cli-model-option-typed"
                    @mouseenter="active = i"
                    @click="pick(r.id)"
                  >
                    <span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-md bg-elevated text-accent-2">
                      <Icon name="edit" :size="11" />
                    </span>
                    <span class="min-w-0 flex-1 truncate text-[12.5px] text-accent-2">{{ t('pages.agentStudio.cliModel.useTyped', { id: r.id }) }}</span>
                  </div>

                  <!-- Compact: details of the active row, inline -->
                  <div
                    v-if="!split && active === i && r.kind === 'model'"
                    class="mx-2 mb-1 mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-elevated/60 px-2.5 py-1.5 text-[10.5px] text-txt3"
                    data-testid="cli-model-inline-detail"
                  >
                    <span>{{ strengthKeys(r.model).map((k) => t(`pages.agentStudio.cliModel.strengths.${k}`)).join(' · ') }}</span>
                    <code class="font-mono text-txt2">--model {{ r.model.id }}</code>
                  </div>
                </template>

                <!-- Empty search -->
                <div v-if="noResults" class="flex flex-col items-center px-4 py-7 text-center" data-testid="cli-model-empty">
                  <span class="flex h-10 w-10 items-center justify-center rounded-full bg-elevated text-txt3">
                    <Icon name="search" :size="16" />
                  </span>
                  <p class="mt-3 text-[12.5px] text-txt2">{{ t('pages.agentStudio.cliModel.noMatch', { q: query.trim() }) }}</p>
                  <button
                    :id="`cli-model-row-${active}`"
                    type="button"
                    class="mt-3 inline-flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-[12px] font-medium text-white shadow-sm transition hover:bg-accent-hover"
                    data-testid="cli-model-empty-use"
                    @click="pick(query)"
                  >
                    <Icon name="edit" :size="12" />
                    {{ t('pages.agentStudio.cliModel.useTyped', { id: query.trim() }) }}
                  </button>
                  <p class="mt-1.5 text-[10.5px] text-txt3">{{ t('pages.agentStudio.cliModel.enterHint') }}</p>
                </div>

                <p v-if="!models.length && !failed && !query.trim()" class="px-3 py-2 text-[11px] leading-5 text-txt3" data-testid="cli-model-no-catalog">
                  {{ t('pages.agentStudio.cliModel.noCatalog') }}
                </p>
              </template>
            </div>

            <!-- Detail pane -->
            <aside v-if="split" class="cms-detail hidden w-[280px] flex-none flex-col overflow-y-auto p-4 sm:flex" data-testid="cli-model-detail">
              <Transition name="ui-fade" mode="out-in">
                <div v-if="activeRow?.kind === 'model'" :key="activeRow.model.id" class="flex flex-col">
                  <div class="flex items-center gap-3">
                    <VendorMark :vendor="activeRow.model.vendor" :size="40" />
                    <div class="min-w-0">
                      <p class="m-0 truncate text-[14px] font-semibold text-txt">{{ activeRow.model.name }}</p>
                      <p class="m-0 truncate text-[11px] text-txt3">{{ vendorLabel(activeRow.model.vendor) }}</p>
                    </div>
                  </div>

                  <dl class="mt-4 space-y-3 text-[11.5px]">
                    <div v-if="activeRow.model.context">
                      <dt class="cms-dt">{{ t('pages.agentStudio.cliModel.contextLabel') }}</dt>
                      <dd class="m-0 flex items-center gap-2">
                        <span class="h-1.5 flex-1 overflow-hidden rounded-full bg-elevated">
                          <span class="cms-bar block h-full rounded-full" :style="{ width: `${Math.max(6, contextRatio(activeRow.model.context) * 100)}%` }" />
                        </span>
                        <span class="w-10 text-right font-mono text-txt2">{{ formatContext(activeRow.model.context) }}</span>
                      </dd>
                    </div>
                    <div v-if="tagsOf(activeRow.model).length">
                      <dt class="cms-dt">{{ t('pages.agentStudio.cliModel.capsLabel') }}</dt>
                      <dd class="m-0 flex flex-wrap gap-1.5">
                        <span v-for="tag in tagsOf(activeRow.model)" :key="tag" class="cms-pill">
                          <Icon :name="TAG_ICON[tag]" :size="11" />
                          {{ t(`pages.agentStudio.cliModel.tags.${tag}`) }}
                        </span>
                      </dd>
                    </div>
                    <div>
                      <dt class="cms-dt">{{ t('pages.agentStudio.cliModel.suitsLabel') }}</dt>
                      <dd class="m-0 leading-5 text-txt2">
                        {{ strengthKeys(activeRow.model).map((k) => t(`pages.agentStudio.cliModel.strengths.${k}`)).join('、') }}
                      </dd>
                    </div>
                  </dl>

                  <div class="mt-4 border-t border-line/70 pt-3">
                    <p class="cms-dt">{{ t('pages.agentStudio.cliModel.flagLabel') }}</p>
                    <div class="flex items-start gap-1.5 rounded-md border border-line bg-surface py-1 pl-2 pr-1">
                      <code class="min-w-0 flex-1 break-all pt-px font-mono text-[11px] leading-[18px] text-txt">--model {{ activeRow.model.id }}</code>
                      <button
                        type="button"
                        tabindex="-1"
                        class="inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[10.5px] text-txt3 transition hover:bg-elevated hover:text-txt"
                        data-testid="cli-model-copy"
                        @click.stop="copyFlag(activeRow.model.id)"
                      >
                        <Icon :name="copied ? 'check' : 'copy'" :size="11" />
                        {{ copied ? t('pages.agentStudio.cliModel.copied') : t('pages.agentStudio.cliModel.copy') }}
                      </button>
                    </div>
                  </div>
                </div>

                <div v-else-if="activeRow?.kind === 'typed' || noResults" key="typed" class="flex flex-col">
                  <div class="flex items-center gap-3">
                    <VendorMark :vendor="backend" backend unknown :size="40" />
                    <div class="min-w-0">
                      <p class="m-0 truncate text-[14px] font-semibold text-txt">{{ t('pages.agentStudio.cliModel.unlistedName') }}</p>
                      <p class="m-0 truncate font-mono text-[11px] text-txt3">{{ query.trim() }}</p>
                    </div>
                  </div>
                  <p class="mt-4 text-[11.5px] leading-5 text-txt2">{{ t('pages.agentStudio.cliModel.typedDetail', { cli: cliLabel }) }}</p>
                  <code class="mt-3 block break-all rounded-md border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt">--model {{ query.trim() }}</code>
                </div>

                <div v-else key="auto" class="flex flex-col">
                  <div class="flex items-center gap-3">
                    <VendorMark :vendor="backend" backend auto :size="40" />
                    <div class="min-w-0">
                      <p class="m-0 truncate text-[14px] font-semibold text-txt">{{ t('pages.agentStudio.cliModel.auto') }}</p>
                      <p class="m-0 truncate text-[11px] text-txt3">{{ cliLabel }}</p>
                    </div>
                  </div>
                  <p class="mt-4 text-[11.5px] leading-5 text-txt2">{{ t('pages.agentStudio.cliModel.autoDetail', { cli: cliLabel }) }}</p>
                  <code class="mt-3 block break-all rounded-md border border-line bg-surface px-2 py-1 font-mono text-[11px] text-txt">{{ launchCommand(backend, '') }}</code>
                </div>
              </Transition>
            </aside>
          </div>

          <!-- Keyboard legend -->
          <div v-if="variant === 'full'" class="hidden shrink-0 items-center gap-3 border-t border-line/70 px-3 py-1.5 text-[10.5px] text-txt3 sm:flex">
            <span><kbd class="cms-kbd">↑</kbd><kbd class="cms-kbd">↓</kbd> {{ t('pages.agentStudio.cliModel.kbd.move') }}</span>
            <span v-if="view.vendors.length"><kbd class="cms-kbd">←</kbd><kbd class="cms-kbd">→</kbd> {{ t('pages.agentStudio.cliModel.kbd.vendor') }}</span>
            <span><kbd class="cms-kbd">Enter</kbd> {{ t('pages.agentStudio.cliModel.kbd.pick') }}</span>
            <span class="ml-auto"><kbd class="cms-kbd">Esc</kbd> {{ t('pages.agentStudio.cliModel.kbd.close') }}</span>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.cms-trigger:focus-visible {
  outline: none;
  border-color: rgb(var(--c-accent-2));
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.22);
}
.cms-trigger.is-flash {
  animation: cms-flash 650ms var(--ease-out-expo);
}
@keyframes cms-flash {
  0% {
    border-color: rgb(var(--c-accent));
    box-shadow: 0 0 0 4px rgb(var(--c-accent) / 0.28);
  }
  100% {
    box-shadow: 0 0 0 0 rgb(var(--c-accent) / 0);
  }
}
.cms-chevron {
  transition: transform var(--dur-ui) var(--ease-out-expo);
}
.cms-chevron.is-open {
  transform: rotate(180deg);
}
.cms-badge {
  display: inline-flex;
  height: 17px;
  flex-shrink: 0;
  align-items: center;
  border-radius: 999px;
  border: 1px solid rgb(var(--c-accent) / 0.35);
  background: rgb(var(--c-accent-dim));
  padding: 0 6px;
  font-size: 10px;
  line-height: 1;
  color: rgb(var(--c-accent-2));
}
.cms-tag-icon {
  display: inline-flex;
  height: 20px;
  width: 20px;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt2));
}
.cms-tag-icon.is-sm {
  height: 16px;
  width: 16px;
  border-radius: 4px;
  background: transparent;
}
.cms-panel {
  min-height: 0;
}
.cms-section {
  margin: 8px 8px 4px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: rgb(var(--c-txt3));
}
.cms-chip {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 6px;
  height: 26px;
  border-radius: 999px;
  border: 1px solid rgb(var(--c-line));
  padding: 0 10px 0 8px;
  font-size: 11.5px;
  color: rgb(var(--c-txt2));
  transition:
    border-color var(--dur-ui) ease,
    background-color var(--dur-ui) ease,
    color var(--dur-ui) ease;
}
.cms-chip:first-child {
  padding-left: 10px;
}
.cms-chip:hover {
  border-color: rgb(var(--c-line-strong));
}
.cms-chip.is-on {
  border-color: rgb(var(--c-accent) / 0.55);
  background: rgb(var(--c-accent-dim));
  color: rgb(var(--c-txt));
}
.cms-card {
  display: flex;
  flex-direction: column;
  border-radius: 10px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 8px 10px;
  text-align: left;
  transition:
    border-color var(--dur-ui) ease,
    transform var(--dur-ui) var(--ease-out-expo),
    box-shadow var(--dur-ui) ease;
}
.cms-card:hover,
.cms-card.is-active {
  border-color: rgb(var(--c-line-strong));
  transform: translateY(-1px);
  box-shadow: 0 4px 14px -6px rgb(0 0 0 / 0.25);
}
.cms-card-name {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  word-break: break-word;
}
.cms-card.is-selected {
  border-color: rgb(var(--c-accent) / 0.6);
  background: rgb(var(--c-accent-dim));
}
.cms-row {
  position: relative;
  display: flex;
  min-height: 40px;
  cursor: pointer;
  align-items: center;
  gap: 10px;
  border-radius: 8px;
  padding: 6px 10px;
  transition: background-color 90ms ease;
}
.cms-row.is-active {
  background: rgb(var(--c-elevated));
}
.cms-row.is-selected::before {
  content: '';
  position: absolute;
  left: 0;
  top: 9px;
  bottom: 9px;
  width: 2px;
  border-radius: 2px;
  background: rgb(var(--c-accent));
}
.cms-row-auto {
  margin: 2px 0 4px;
  border: 1px dashed rgb(var(--c-line));
}
.cms-hit {
  border-radius: 2px;
  background: rgb(var(--c-accent) / 0.3);
  color: rgb(var(--c-accent-2));
  padding: 0 1px;
}
.cms-kbd {
  display: inline-flex;
  min-width: 18px;
  height: 18px;
  align-items: center;
  justify-content: center;
  margin-right: 2px;
  border-radius: 4px;
  border: 1px solid rgb(var(--c-line));
  border-bottom-width: 2px;
  background: rgb(var(--c-surface));
  padding: 0 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 10px;
  color: rgb(var(--c-txt3));
}
.cms-detail {
  background: linear-gradient(180deg, rgb(var(--c-accent) / 0.05), transparent 45%);
}
.cms-dt {
  margin: 0 0 6px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgb(var(--c-txt3));
}
.cms-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  border-radius: 999px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 0 8px;
  font-size: 11px;
  color: rgb(var(--c-txt));
}
.cms-bar {
  background: linear-gradient(90deg, rgb(var(--c-accent)), rgb(var(--c-accent-2)));
  transition: width 260ms var(--ease-out-expo);
}
@media (prefers-reduced-motion: reduce) {
  .cms-trigger.is-flash {
    animation: none;
  }
  .cms-card:hover,
  .cms-card.is-active {
    transform: none;
  }
  .cms-bar {
    transition: none;
  }
}
</style>

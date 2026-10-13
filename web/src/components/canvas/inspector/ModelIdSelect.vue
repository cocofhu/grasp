<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import type { OpenCodeCatalogModel } from '@/lib/api/apiTypes'
import { loadOpenCodeModels } from '@/lib/agent/openCodeCatalog'
import { OPENCODE_CUSTOM_PROVIDER, openCodeModelID, openCodeModelWithProvider } from '@/lib/agent/openCodeProvider'

const props = defineProps<{
  /** `provider/model` id picked for this node; empty uses the credential's default. */
  modelValue: string
  /** Vendor of the effective credential; empty disables the picker. */
  vendor: string
  /** The credential's own model (`provider/model`). */
  defaultModel: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const MAX_LISTED = 80

const { t } = useI18n()
const open = ref(false)
const query = ref('')
const active = ref(0)
const root = ref<HTMLElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const catalog = ref<OpenCodeCatalogModel[]>([])
const catalogLoading = ref(false)

const disabled = computed(() => !props.vendor)
const ownEndpoint = computed(() => props.vendor === OPENCODE_CUSTOM_PROVIDER)

watch(
  () => props.vendor,
  async (vendor) => {
    catalog.value = []
    if (!vendor || vendor === OPENCODE_CUSTOM_PROVIDER) return
    catalogLoading.value = true
    const models = await loadOpenCodeModels(vendor)
    if (vendor === props.vendor) catalog.value = models
    catalogLoading.value = false
  },
  { immediate: true },
)

function shortId(full: string): string {
  return openCodeModelID(full, props.vendor)
}

function displayName(full: string): string {
  const id = shortId(full)
  return catalog.value.find((m) => m.id === id)?.name || id
}

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  const all = q ? catalog.value.filter((m) => m.id.toLowerCase().includes(q) || (m.name || '').toLowerCase().includes(q)) : catalog.value
  return { shown: all.slice(0, MAX_LISTED), hidden: Math.max(0, all.length - MAX_LISTED) }
})

/** A typed id the catalog does not list; OpenCode still accepts it. */
const typed = computed(() => {
  const q = query.value.trim()
  if (!q) return ''
  const id = openCodeModelID(q, props.vendor)
  return catalog.value.some((m) => m.id === id) ? '' : openCodeModelWithProvider(q, props.vendor)
})

type Option = { value: string; kind: 'default' | 'catalog' | 'typed'; label: string; sub: string }
const options = computed<Option[]>(() => {
  const out: Option[] = [
    { value: '', kind: 'default', label: t('canvas.inspector.model.useDefault'), sub: shortId(props.defaultModel) || '—' },
  ]
  for (const m of filtered.value.shown) {
    out.push({ value: openCodeModelWithProvider(m.id, props.vendor), kind: 'catalog', label: m.name || m.id, sub: m.id })
  }
  if (typed.value) out.push({ value: typed.value, kind: 'typed', label: t('canvas.inspector.model.useTyped', { id: shortId(typed.value) }), sub: '' })
  return out
})

const unlisted = computed(
  () => !!props.modelValue && !ownEndpoint.value && !catalogLoading.value && catalog.value.length > 0 && !catalog.value.some((m) => m.id === shortId(props.modelValue)),
)

async function toggle() {
  if (disabled.value) return
  open.value = !open.value
  if (!open.value) return
  query.value = ''
  active.value = Math.max(0, options.value.findIndex((o) => o.value === props.modelValue))
  await nextTick()
  search.value?.focus()
  root.value?.querySelector(`#model-id-opt-${active.value}`)?.scrollIntoView({ block: 'nearest' })
}

function pick(value: string) {
  const next = value === props.defaultModel ? '' : value
  if (next !== props.modelValue) emit('update:modelValue', next)
  open.value = false
}

watch(query, () => {
  active.value = options.value.length > 1 ? 1 : 0
})

async function onKey(e: KeyboardEvent) {
  const n = options.value.length
  if (e.key === 'ArrowDown') active.value = (active.value + 1) % n
  else if (e.key === 'ArrowUp') active.value = (active.value - 1 + n) % n
  else if (e.key === 'Enter') pick(options.value[active.value]!.value)
  else if (e.key === 'Escape') open.value = false
  else return
  e.preventDefault()
  e.stopPropagation()
  await nextTick()
  root.value?.querySelector(`#model-id-opt-${active.value}`)?.scrollIntoView({ block: 'nearest' })
}

function onDocDown(ev: MouseEvent) {
  if (open.value && root.value && !root.value.contains(ev.target as Node)) open.value = false
}
onMounted(() => document.addEventListener('mousedown', onDocDown, true))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown, true))
</script>

<template>
  <div ref="root" class="relative" data-testid="model-id-select">
    <button
      type="button"
      class="input flex h-auto min-h-[44px] items-center gap-2.5 py-1.5 text-left disabled:cursor-not-allowed disabled:opacity-60"
      :class="unlisted ? '!border-warn/60' : ''"
      :disabled="disabled"
      :aria-expanded="open"
      aria-haspopup="listbox"
      :aria-label="t('canvas.inspector.model.modelLabel')"
      data-testid="model-id-trigger"
      @click="toggle"
    >
      <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-line bg-surface text-txt3"><Icon name="sparkles" :size="14" /></span>
      <span v-if="disabled" class="min-w-0 flex-1 text-[12px] text-txt3">{{ t('canvas.inspector.model.needCredential') }}</span>
      <span v-else-if="!modelValue" class="min-w-0 flex-1">
        <span class="block truncate text-[13px] text-txt">{{ t('canvas.inspector.model.useDefault') }}</span>
        <span class="block truncate font-mono text-[11px] text-txt3">{{ shortId(defaultModel) || '—' }}</span>
      </span>
      <span v-else class="min-w-0 flex-1">
        <span class="flex items-center gap-1.5">
          <span class="truncate text-[13px] font-medium text-txt">{{ displayName(modelValue) }}</span>
          <span class="inline-flex h-[18px] shrink-0 items-center rounded border border-accent/35 bg-accent-dim px-1.5 text-[10px] leading-none text-accent-2" data-testid="model-id-node-chip">{{ t('canvas.inspector.model.nodeChip') }}</span>
        </span>
        <span class="block truncate font-mono text-[11px]" :class="unlisted ? 'text-warn' : 'text-txt3'" data-testid="model-id-sub">
          {{ unlisted ? t('canvas.inspector.model.unlisted') : shortId(modelValue) }}
        </span>
      </span>
      <Icon v-if="!disabled" name="chevron-down" :size="14" class="shrink-0 text-txt3" />
    </button>

    <div v-if="open" class="cchrome absolute left-0 right-0 z-30 mt-1 flex max-h-[22rem] flex-col p-1" data-testid="model-id-list">
      <div class="flex items-center gap-2 border-b border-line px-2 pb-1.5 pt-1">
        <Icon name="search" :size="13" class="shrink-0 text-txt3" />
        <input
          ref="search"
          v-model="query"
          class="min-w-0 flex-1 bg-transparent py-1 text-[12px] text-txt outline-none placeholder:text-txt3"
          :placeholder="ownEndpoint || (!catalogLoading && !catalog.length) ? t('canvas.inspector.model.typePlaceholder') : t('canvas.inspector.model.searchPlaceholder')"
          role="combobox"
          aria-autocomplete="list"
          :aria-activedescendant="`model-id-opt-${active}`"
          data-testid="model-id-search"
          @keydown="onKey"
        />
      </div>
      <div class="scroll-area min-h-0 flex-1 overflow-y-auto pt-1" role="listbox">
        <template v-for="(o, i) in options" :key="o.kind + o.value">
          <p
            v-if="i === 1 && o.kind === 'catalog'"
            class="mx-2 mb-1 mt-1.5 border-t border-line pt-2 text-[10px] font-semibold uppercase tracking-[0.12em] text-txt3"
          >
            {{ t('canvas.inspector.model.catalogTitle', { vendor }) }}
          </p>
          <button
            :id="`model-id-opt-${i}`"
            type="button"
            role="option"
            :aria-selected="o.value === modelValue"
            class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left"
            :class="i === active ? 'bg-elevated' : ''"
            :data-testid="o.kind === 'default' ? 'model-id-option-default' : o.kind === 'typed' ? 'model-id-option-typed' : `model-id-option-${o.sub}`"
            @mouseenter="active = i"
            @click="pick(o.value)"
          >
            <Icon v-if="o.kind === 'typed'" name="edit" :size="13" class="shrink-0 text-accent-2" />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-[13px]" :class="o.kind === 'typed' ? 'text-accent-2' : 'text-txt'">{{ o.label }}</span>
              <span v-if="o.sub && o.sub !== o.label" class="block truncate font-mono text-[11px] text-txt3">{{ o.sub }}</span>
            </span>
            <Icon v-if="o.value === modelValue" name="check" :size="14" class="shrink-0 text-accent-2" />
          </button>
        </template>
        <p v-if="catalogLoading" class="px-3 py-2 text-[12px] text-txt3">{{ t('common.loading.inProgress') }}</p>
        <p v-else-if="ownEndpoint || !catalog.length" class="px-3 py-2 text-[11px] leading-5 text-txt3" data-testid="model-id-no-catalog">
          {{ t('canvas.inspector.model.noCatalog') }}
        </p>
        <p v-else-if="filtered.hidden" class="px-3 py-2 text-[11px] text-txt3">{{ t('canvas.inspector.model.moreHidden', { n: filtered.hidden }) }}</p>
      </div>
    </div>
  </div>
</template>

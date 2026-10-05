<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import { agentHueVar, agentInitial } from '../composables/agentAvatar'
import { filterPaletteItems, type PaletteGroup, type PaletteItem } from '../composables/paletteItems'

const props = defineProps<{
  items: PaletteItem[]
  title: string
  /** Host-relative anchor; the panel is clamped inside `bounds`. */
  x: number
  y: number
  bounds: { width: number; height: number }
  centered?: boolean
}>()

const emit = defineEmits<{
  (e: 'pick', item: PaletteItem): void
  (e: 'close'): void
}>()

const { t } = useI18n()
const q = ref('')
const active = ref(0)
const root = ref<HTMLElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const list = ref<HTMLElement | null>(null)

const GROUPS: PaletteGroup[] = ['agent', 'control', 'collab']
const GROUP_KEYS: Record<PaletteGroup, string> = {
  agent: 'canvas.palette.agents',
  control: 'canvas.palette.control',
  collab: 'canvas.palette.collab',
}

const filtered = computed(() => filterPaletteItems(props.items, q.value))
const grouped = computed(() =>
  GROUPS.map((g) => ({ group: g, items: filtered.value.filter((i) => i.group === g) })).filter((g) => g.items.length),
)
const flat = computed(() => grouped.value.flatMap((g) => g.items))

watch(q, () => {
  active.value = 0
})

const WIDTH = 320
const MAX_H = 380
const style = computed(() => {
  if (props.centered) return { left: '50%', top: '14%', width: `${WIDTH + 80}px`, transform: 'translateX(-50%)' }
  const left = Math.max(8, Math.min(props.x, props.bounds.width - WIDTH - 8))
  const top = Math.max(8, Math.min(props.y, props.bounds.height - MAX_H - 8))
  return { left: `${left}px`, top: `${top}px`, width: `${WIDTH}px` }
})

function move(delta: number) {
  if (!flat.value.length) return
  active.value = (active.value + delta + flat.value.length) % flat.value.length
  void nextTick(() => {
    list.value?.querySelector<HTMLElement>('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  })
}

function choose(item?: PaletteItem) {
  const it = item ?? flat.value[active.value]
  if (it) emit('pick', it)
}

function onDocDown(ev: MouseEvent) {
  if (root.value && !root.value.contains(ev.target as Node)) emit('close')
}
onMounted(async () => {
  document.addEventListener('mousedown', onDocDown, true)
  await nextTick()
  input.value?.focus()
})
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown, true))

function indexOf(item: PaletteItem) {
  return flat.value.indexOf(item)
}
</script>

<template>
  <div
    ref="root"
    class="cchrome absolute z-30 flex flex-col overflow-hidden"
    :style="{ ...style, maxHeight: `${MAX_H}px` }"
    role="dialog"
    :aria-label="title"
    data-testid="quick-add"
  >
    <div class="border-b border-line px-3 pb-2 pt-2.5">
      <div class="mb-1.5 text-[11px] font-medium text-txt3">{{ title }}</div>
      <div class="relative">
        <Icon name="search" :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-txt3" />
        <input
          ref="input"
          v-model="q"
          class="input pl-8 text-[13px]"
          :placeholder="t('canvas.quickAdd.placeholder')"
          role="combobox"
          aria-autocomplete="list"
          aria-controls="quick-add-list"
          :aria-activedescendant="flat[active] ? `qa-${flat[active].key}` : undefined"
          data-testid="quick-add-input"
          @keydown.down.prevent="move(1)"
          @keydown.up.prevent="move(-1)"
          @keydown.enter.prevent="choose()"
          @keydown.esc.stop.prevent="emit('close')"
        />
      </div>
    </div>
    <div id="quick-add-list" ref="list" class="scroll-area min-h-0 flex-1 overflow-y-auto p-1.5" role="listbox">
      <p v-if="!flat.length" class="px-3 py-6 text-center text-[12px] text-txt3">{{ t('canvas.quickAdd.empty') }}</p>
      <div v-for="g in grouped" :key="g.group" class="mb-1">
        <div class="px-2 pb-1 pt-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t(GROUP_KEYS[g.group]) }}</div>
        <button
          v-for="it in g.items"
          :id="`qa-${it.key}`"
          :key="it.key"
          type="button"
          role="option"
          :aria-selected="indexOf(it) === active"
          :data-active="indexOf(it) === active ? 'true' : undefined"
          class="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left"
          :class="indexOf(it) === active ? 'bg-elevated' : 'hover:bg-elevated/60'"
          :data-testid="`quick-add-item-${it.key}`"
          @mouseenter="active = indexOf(it)"
          @click="choose(it)"
        >
          <span
            v-if="it.agent"
            class="cnode-avatar !h-6 !w-6 text-[11px]"
            :style="{ background: `rgb(var(${agentHueVar(it.agent)}) / 0.16)`, color: `rgb(var(${agentHueVar(it.agent)}))` }"
            aria-hidden="true"
          >{{ agentInitial(it.agent) }}</span>
          <span v-else class="cnode-icon !h-6 !w-6" aria-hidden="true"><Icon :name="it.icon || 'robot'" :size="13" /></span>
          <span class="min-w-0 flex-1">
            <span class="block truncate text-[13px] text-txt">{{ it.label }}</span>
            <span class="block truncate text-[11px] text-txt3">{{ it.desc }}</span>
          </span>
        </button>
      </div>
    </div>
    <div class="border-t border-line px-3 py-1.5 text-[10.5px] text-txt3">{{ t('canvas.quickAdd.hint') }}</div>
  </div>
</template>

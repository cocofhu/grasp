<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '../ui/Icon.vue'
import { agentHueVar, agentInitial } from './composables/agentAvatar'
import {
  encodePaletteDrag,
  filterPaletteItems,
  PALETTE_MIME,
  type PaletteGroup,
  type PaletteItem,
} from './composables/paletteItems'
import type { NodeSpec } from './composables/graphOps'

const props = defineProps<{
  items: PaletteItem[]
  agentsLoading?: boolean
  /** Key of the item currently being placed on the canvas. */
  placingKey?: string | null
}>()

const emit = defineEmits<{
  /** Click: start (or cancel) click-to-place on the canvas. */
  (e: 'place', spec: NodeSpec): void
  /** Keyboard: add right away at a free spot in view. */
  (e: 'add', spec: NodeSpec): void
}>()

const { t } = useI18n()
const COLLAPSE_KEY = 'grasp.canvas.paletteCollapsed'
const collapsed = ref(readCollapsed())
const q = ref('')

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === '1'
  } catch {
    return false
  }
}
watch(collapsed, (v) => {
  try {
    localStorage.setItem(COLLAPSE_KEY, v ? '1' : '0')
  } catch {
    /* storage unavailable */
  }
})

const GROUPS: { id: PaletteGroup; key: string }[] = [
  { id: 'agent', key: 'canvas.palette.agents' },
  { id: 'control', key: 'canvas.palette.control' },
  { id: 'collab', key: 'canvas.palette.collab' },
]

const filtered = computed(() => filterPaletteItems(props.items, q.value))
const groups = computed(() =>
  GROUPS.map((g) => ({ ...g, items: filtered.value.filter((i) => i.group === g.id) })).filter(
    (g) => g.items.length || (g.id === 'agent' && !q.value),
  ),
)
const projectAgentCount = computed(() => props.items.filter((i) => i.agent).length)

function onDragStart(ev: DragEvent, item: PaletteItem) {
  if (!ev.dataTransfer) return
  ev.dataTransfer.setData(PALETTE_MIME, encodePaletteDrag(item.spec))
  ev.dataTransfer.effectAllowed = 'copy'
}
</script>

<template>
  <aside
    v-if="collapsed"
    class="flex h-full w-11 shrink-0 flex-col items-center gap-1 border-r border-line bg-surface py-2"
    data-testid="node-palette"
    :aria-label="t('canvas.aria.palette')"
  >
    <button
      type="button"
      class="cchrome-btn"
      :aria-label="t('canvas.palette.expand')"
      :title="t('canvas.palette.expand')"
      data-testid="palette-toggle"
      @click="collapsed = false"
    >
      <Icon name="panel-left" :size="15" />
    </button>
  </aside>
  <aside
    v-else
    class="flex h-full w-[248px] shrink-0 flex-col border-r border-line bg-surface"
    data-testid="node-palette"
    :aria-label="t('canvas.aria.palette')"
  >
    <div class="flex items-center gap-1.5 border-b border-line p-2.5">
      <div class="relative min-w-0 flex-1">
        <Icon name="search" :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-txt3" />
        <input
          v-model="q"
          type="search"
          class="input h-8 pl-8 text-[12.5px]"
          :placeholder="t('canvas.palette.search')"
          :aria-label="t('canvas.palette.search')"
          data-testid="palette-search"
        />
      </div>
      <button
        type="button"
        class="cchrome-btn shrink-0"
        :aria-label="t('canvas.palette.collapse')"
        :title="t('canvas.palette.collapse')"
        data-testid="palette-toggle"
        @click="collapsed = true"
      >
        <Icon name="panel-left" :size="15" />
      </button>
    </div>
    <div class="scroll-area min-h-0 flex-1 overflow-y-auto px-2.5 pb-3 pt-2">
      <p v-if="!groups.length" class="px-1 py-6 text-center text-[12px] text-txt3">{{ t('canvas.palette.noMatch') }}</p>
      <section v-for="g in groups" :key="g.id" class="mb-3" :data-testid="`palette-group-${g.id}`">
        <div class="flex items-center justify-between px-1 pb-1.5 pt-1">
          <h3 class="text-[10.5px] font-semibold uppercase tracking-wider text-txt3">{{ t(g.key) }}</h3>
          <RouterLink
            v-if="g.id === 'agent'"
            to="/agents"
            class="inline-flex items-center gap-0.5 text-[11px] text-txt3 hover:text-accent-2"
            data-testid="palette-from-template"
          >
            <Icon name="plus" :size="11" />{{ t('canvas.palette.fromTemplate') }}
          </RouterLink>
        </div>
        <p
          v-if="g.id === 'agent' && !agentsLoading && !projectAgentCount && !q"
          class="mb-1.5 px-1 text-[11.5px] text-txt3"
        >
          {{ t('canvas.palette.noAgents') }}
        </p>
        <div
          v-if="g.id === 'agent' && agentsLoading"
          class="mb-1.5 h-11 animate-pulse rounded-lg bg-elevated"
          aria-hidden="true"
        />
        <div
          v-for="it in g.items"
          :key="it.key"
          class="group mb-1 flex cursor-grab items-center gap-2.5 rounded-lg border px-2 py-1.5 transition-colors active:cursor-grabbing"
          :class="placingKey === it.key ? 'border-accent/60 bg-accent-dim' : 'border-transparent hover:border-line hover:bg-elevated'"
          draggable="true"
          role="button"
          tabindex="0"
          :title="t('canvas.palette.dragHint')"
          :aria-label="`${it.label}. ${it.desc}`"
          :aria-pressed="placingKey === it.key"
          :data-testid="`palette-item-${it.key}`"
          :data-placing="placingKey === it.key ? 'true' : undefined"
          @dragstart="onDragStart($event, it)"
          @click="emit('place', it.spec)"
          @keydown.enter.prevent="emit('add', it.spec)"
          @keydown.space.prevent="emit('add', it.spec)"
        >
          <span
            v-if="it.agent"
            class="cnode-avatar"
            :style="{ background: `rgb(var(${agentHueVar(it.agent)}) / 0.16)`, color: `rgb(var(${agentHueVar(it.agent)}))` }"
            aria-hidden="true"
          >{{ agentInitial(it.agent) }}</span>
          <span v-else class="cnode-icon" aria-hidden="true"><Icon :name="it.icon || 'robot'" :size="14" /></span>
          <span class="min-w-0 flex-1">
            <span class="block truncate text-[12.5px] font-medium text-txt">{{ it.label }}</span>
            <span class="block truncate text-[11px] text-txt3">{{ it.desc }}</span>
          </span>
          <Icon name="plus" :size="13" class="shrink-0 text-txt3 opacity-0 transition-opacity group-hover:opacity-100" />
        </div>
      </section>
    </div>
  </aside>
</template>

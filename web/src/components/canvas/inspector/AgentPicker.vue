<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import { agentHueVar, agentInitial } from '../composables/agentAvatar'
import { capabilitySummary, type CanvasAgent } from '../composables/outlets'

const props = defineProps<{
  modelValue: string
  agents: CanvasAgent[]
  loading?: boolean
  invalid?: boolean
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const { t } = useI18n()
const tr = (key: string, named?: Record<string, unknown>) => (named ? t(key, named) : t(key))
const open = ref(false)
const active = ref(0)
const root = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)

const current = computed(() => props.agents.find((a) => a.name === props.modelValue) ?? null)
const options = computed(() => props.agents.map((a) => ({ agent: a, summary: capabilitySummary(a.capabilities, tr) })))

function hue(name: string) {
  const v = agentHueVar(name)
  return { background: `rgb(var(${v}) / 0.16)`, color: `rgb(var(${v}))` }
}

async function toggle() {
  open.value = !open.value
  if (!open.value) return
  active.value = Math.max(0, options.value.findIndex((o) => o.agent.name === props.modelValue))
  await nextTick()
  list.value?.focus()
}

function pick(name: string) {
  emit('update:modelValue', name)
  open.value = false
}

function onKey(e: KeyboardEvent) {
  const n = options.value.length
  if (e.key === 'ArrowDown' && n) active.value = (active.value + 1) % n
  else if (e.key === 'ArrowUp' && n) active.value = (active.value - 1 + n) % n
  else if (e.key === 'Enter' && n) pick(options.value[active.value]!.agent.name)
  else if (e.key === 'Escape') open.value = false
  else return
  e.preventDefault()
  e.stopPropagation()
}

function onDocDown(ev: MouseEvent) {
  if (open.value && root.value && !root.value.contains(ev.target as Node)) open.value = false
}
onMounted(() => document.addEventListener('mousedown', onDocDown, true))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown, true))
</script>

<template>
  <div ref="root" class="relative">
    <button
      type="button"
      class="input flex h-auto min-h-[44px] items-center gap-2.5 py-1.5 text-left"
      :class="invalid ? '!border-warn/60' : ''"
      :aria-expanded="open"
      aria-haspopup="listbox"
      :aria-label="t('canvas.inspector.sections.agent')"
      data-testid="agent-picker"
      @click="toggle"
    >
      <template v-if="modelValue">
        <span class="cnode-avatar shrink-0" :style="hue(modelValue)" aria-hidden="true">{{ agentInitial(modelValue) }}</span>
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[13px] font-medium text-txt">{{ modelValue }}</span>
          <span class="block truncate text-[11px] text-txt3">
            {{ current ? capabilitySummary(current.capabilities, tr) : t('canvas.node.agentMissing') }}
          </span>
        </span>
      </template>
      <span v-else class="flex-1 text-[13px] text-txt3">{{ loading ? t('common.loading') : t('canvas.inspector.agentPlaceholder') }}</span>
      <Icon name="chevron-down" :size="14" class="shrink-0 text-txt3" />
    </button>
    <div
      v-if="open"
      ref="list"
      class="cchrome scroll-area absolute left-0 right-0 z-30 mt-1 max-h-72 overflow-y-auto p-1 outline-none"
      role="listbox"
      tabindex="-1"
      :aria-activedescendant="options[active] ? `agent-opt-${active}` : undefined"
      data-testid="agent-picker-list"
      @keydown="onKey"
    >
      <p v-if="!options.length" class="px-3 py-4 text-center text-[12px] text-txt3">{{ t('canvas.palette.noAgents') }}</p>
      <button
        v-for="(o, i) in options"
        :id="`agent-opt-${i}`"
        :key="o.agent.name"
        type="button"
        role="option"
        :aria-selected="o.agent.name === modelValue"
        class="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left"
        :class="i === active ? 'bg-elevated' : ''"
        :data-testid="`agent-option-${o.agent.name}`"
        @mouseenter="active = i"
        @click="pick(o.agent.name)"
      >
        <span class="cnode-avatar shrink-0" :style="hue(o.agent.name)" aria-hidden="true">{{ agentInitial(o.agent.name) }}</span>
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[13px] text-txt">{{ o.agent.name }}</span>
          <span class="block truncate text-[11px] text-txt3">{{ o.summary }}</span>
        </span>
        <Icon v-if="o.agent.name === modelValue" name="check" :size="14" class="shrink-0 text-accent-2" />
      </button>
    </div>
  </div>
</template>

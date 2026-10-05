<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export interface TemplateToken {
  token: string
  label: string
  group: 'vars' | 'upstream'
}

const props = defineProps<{
  modelValue: string
  tokens: TemplateToken[]
  placeholder?: string
  focusTick?: number
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const { t } = useI18n()
const el = ref<HTMLTextAreaElement | null>(null)
/** Index of the `{{` that opened the suggestion list, or -1. */
const openAt = ref(-1)
const query = ref('')
const active = ref(0)

const matches = computed(() => {
  const q = query.value.toLowerCase()
  return props.tokens.filter((x) => !q || x.token.toLowerCase().includes(q) || x.label.toLowerCase().includes(q)).slice(0, 30)
})

function readTrigger() {
  const ta = el.value
  if (!ta) return
  const caret = ta.selectionStart ?? 0
  const before = ta.value.slice(0, caret)
  const i = before.lastIndexOf('{{')
  if (i < 0 || before.slice(i).includes('}}') || /\s/.test(before.slice(i + 2))) {
    openAt.value = -1
    return
  }
  openAt.value = i
  query.value = before.slice(i + 2)
  active.value = 0
}

function onInput(e: Event) {
  emit('update:modelValue', (e.target as HTMLTextAreaElement).value)
  readTrigger()
}

async function insert(tok: TemplateToken) {
  const ta = el.value
  if (!ta || openAt.value < 0) return
  const caret = ta.selectionStart ?? ta.value.length
  let end = caret
  if (ta.value.slice(caret, caret + 2) === '}}') end += 2
  const next = ta.value.slice(0, openAt.value) + tok.token + ta.value.slice(end)
  const pos = openAt.value + tok.token.length
  openAt.value = -1
  emit('update:modelValue', next)
  await nextTick()
  ta.focus()
  ta.setSelectionRange(pos, pos)
}

function onKeydown(e: KeyboardEvent) {
  if (openAt.value < 0) return
  const n = matches.value.length
  if (e.key === 'Escape') openAt.value = -1
  else if (!n) return
  else if (e.key === 'ArrowDown') active.value = (active.value + 1) % n
  else if (e.key === 'ArrowUp') active.value = (active.value - 1 + n) % n
  else if (e.key === 'Enter' || e.key === 'Tab') void insert(matches.value[active.value]!)
  else return
  e.preventDefault()
  e.stopPropagation()
}

watch(
  () => props.focusTick,
  async (v, old) => {
    if (!v || v === old) return
    await nextTick()
    el.value?.focus()
    const len = el.value?.value.length ?? 0
    el.value?.setSelectionRange(len, len)
  },
  { immediate: true },
)

const GROUP_KEYS = { vars: 'canvas.inspector.suggest.vars', upstream: 'canvas.inspector.suggest.upstream' } as const
</script>

<template>
  <div class="relative">
    <textarea
      ref="el"
      :value="modelValue"
      class="input min-h-[120px] resize-y text-[13px] leading-relaxed"
      :placeholder="placeholder"
      role="combobox"
      aria-autocomplete="list"
      :aria-expanded="openAt >= 0"
      data-testid="goal-input"
      @input="onInput"
      @keydown="onKeydown"
      @click="readTrigger"
      @blur="openAt = -1"
    />
    <div
      v-if="openAt >= 0"
      class="cchrome scroll-area absolute left-0 right-0 top-full z-30 mt-1 max-h-60 overflow-y-auto p-1"
      role="listbox"
      data-testid="goal-suggest"
      @mousedown.prevent
    >
      <p v-if="!matches.length" class="px-3 py-3 text-[12px] text-txt3">{{ t('canvas.inspector.suggest.empty') }}</p>
      <template v-for="(m, i) in matches" :key="m.token">
        <div v-if="i === 0 || matches[i - 1]!.group !== m.group" class="px-2 pb-0.5 pt-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
          {{ t(GROUP_KEYS[m.group]) }}
        </div>
        <button
          type="button"
          role="option"
          :aria-selected="i === active"
          class="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1 text-left"
          :class="i === active ? 'bg-elevated' : ''"
          @mouseenter="active = i"
          @click="insert(m)"
        >
          <span class="truncate text-[12px] text-txt">{{ m.label }}</span>
          <code class="shrink-0 truncate font-mono text-[10.5px] text-accent-2">{{ m.token }}</code>
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import AppInlineError from './AppInlineError.vue'
import AppSpinner from './AppSpinner.vue'
import { api } from '@/lib/api/api'
import { MAX_TAG_RUNES, validateRunTag } from '@/lib/run/runTags'

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    open?: boolean
  }>(),
  {
    modelValue: () => [],
    open: undefined,
  },
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: string[]): void
  (e: 'update:open', v: boolean): void
}>()

const { t } = useI18n()
const internalOpen = ref(false)
const root = ref<HTMLElement | null>(null)
const inputEl = ref<HTMLInputElement | null>(null)
const draft = ref('')
const submitError = ref('')
const stockTags = ref<string[]>([])
const stockLoading = ref(false)
const stockError = ref<string | null>(null)

const isControlled = computed(() => props.open !== undefined)
const open = computed({
  get: () => (isControlled.value ? props.open! : internalOpen.value),
  set: (v: boolean) => {
    if (isControlled.value) emit('update:open', v)
    else internalOpen.value = v
  },
})

const hasFilter = computed(() => props.modelValue.length > 0)
const selectedSet = computed(() => new Set(props.modelValue))

const filteredStock = computed(() => {
  const q = draft.value.trim().toLowerCase()
  if (!q) return stockTags.value
  return stockTags.value.filter((tag) => tag.toLowerCase().includes(q))
})

const emptyHint = computed(() => {
  if (stockLoading.value || stockError.value) return ''
  const q = draft.value.trim()
  if (!filteredStock.value.length) {
    if (q) return t('common.tagFilter.noMatchHint', { tag: q })
    return t('common.tagFilter.noStock')
  }
  return ''
})

function validationMessage(code: ReturnType<typeof validateRunTag>): string {
  if (code === 'too_long') return t('common.tagFilter.tagTooLong', { max: MAX_TAG_RUNES })
  if (code === 'invalid') return t('common.tagFilter.tagInvalid')
  return ''
}

async function loadStock() {
  stockLoading.value = true
  stockError.value = null
  try {
    const res = await api.listRunTags()
    stockTags.value = Array.isArray(res.tags) ? res.tags : []
  } catch (err) {
    stockTags.value = []
    stockError.value =
      err instanceof Error && err.message
        ? err.message
        : String(t('common.tagFilter.loadFailed'))
  } finally {
    stockLoading.value = false
  }
}

onMounted(() => {
  void loadStock()
})

watch(open, async (v) => {
  if (!v) {
    draft.value = ''
    submitError.value = ''
    return
  }
  await nextTick()
  inputEl.value?.focus()
})

function setSelected(next: string[]) {
  emit('update:modelValue', next)
}

function toggleTag(tag: string) {
  const trimmed = tag.trim()
  if (!trimmed) return
  submitError.value = ''
  if (selectedSet.value.has(trimmed)) {
    setSelected(props.modelValue.filter((item) => item !== trimmed))
  } else {
    const code = validateRunTag(trimmed)
    if (code) {
      submitError.value = validationMessage(code)
      return
    }
    setSelected([...props.modelValue, trimmed])
  }
  draft.value = ''
}

function removeTag(tag: string) {
  setSelected(props.modelValue.filter((item) => item !== tag))
}

function addDraft() {
  const trimmed = draft.value.trim()
  if (!trimmed) return
  const code = validateRunTag(trimmed)
  if (code) {
    submitError.value = validationMessage(code) || t('common.tagFilter.tagInvalid')
    return
  }
  if (!selectedSet.value.has(trimmed)) {
    setSelected([...props.modelValue, trimmed])
  }
  draft.value = ''
  submitError.value = ''
}

function onDraftInput() {
  submitError.value = ''
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    addDraft()
  } else if (e.key === 'Escape') {
    e.preventDefault()
    open.value = false
  }
}

function toggle() {
  open.value = !open.value
}

function onDocClick(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}

onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div ref="root" class="relative w-full md:w-auto" data-testid="tag-filter">
    <button
      type="button"
      class="toolbar-control flex w-full items-center gap-2 border border-line bg-surface text-sm text-txt2 transition hover:bg-elevated md:w-auto"
      :class="{ 'border-accent/60 text-txt': hasFilter || open }"
      :aria-expanded="open"
      aria-haspopup="true"
      data-testid="tag-filter-trigger"
      @click.stop="toggle"
    >
      <Icon name="tag" :size="15" class="text-txt3" />
      <span class="min-w-0 flex-1 truncate text-left md:flex-none">{{ t('common.tagFilter.label') }}</span>
      <span v-if="hasFilter" class="chip" data-testid="tag-filter-count">{{ modelValue.length }}</span>
      <Icon
        name="chevron-down"
        :size="14"
        class="tag-filter-chevron shrink-0 text-txt3"
        :class="{ 'is-open': open }"
      />
    </button>

    <Transition name="overlay-pop">
    <div
      v-if="open"
      class="card absolute left-0 right-0 z-30 mt-1.5 overflow-hidden p-2.5 md:left-auto md:right-0 md:w-72"
      role="dialog"
      :aria-label="t('common.tagFilter.label')"
      data-testid="tag-filter-panel"
      @click.stop
    >
      <div class="relative">
        <Icon
          name="search"
          :size="14"
          class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-txt3"
        />
        <input
          ref="inputEl"
          v-model="draft"
          type="text"
          class="input pl-8"
          :placeholder="t('common.tagFilter.searchPlaceholder')"
          autocomplete="off"
          data-testid="tag-filter-input"
          @input="onDraftInput"
          @keydown="onKeydown"
        />
      </div>

      <div
        v-if="submitError"
        class="mt-1.5 rounded-md bg-err/10 px-2 py-1.5 text-[11px] text-err"
        data-testid="tag-filter-error"
      >
        {{ submitError }}
      </div>

      <template v-if="modelValue.length">
        <div class="mt-2 text-[11px] font-semibold uppercase tracking-wide text-txt3">
          {{ t('common.tagFilter.selectedLabel') }}
        </div>
        <div class="mt-1.5 flex flex-wrap gap-1.5" data-testid="tag-filter-selected">
          <button
            v-for="tag in modelValue"
            :key="tag"
            type="button"
            class="chip"
            :aria-label="t('common.tagFilter.removeTag', { tag })"
            @click="removeTag(tag)"
          >
            {{ tag }}
            <Icon name="close" :size="11" class="opacity-70" />
          </button>
        </div>
      </template>

      <div class="mt-2 text-[11px] font-semibold uppercase tracking-wide text-txt3">
        {{ t('common.tagFilter.suggestionsLabel') }}
      </div>

      <div
        v-if="stockError"
        class="mt-1.5"
        data-testid="tag-filter-stock-error"
      >
        <AppInlineError
          :title="t('common.tagFilter.loadFailed')"
          :message="stockError"
          @retry="loadStock()"
        />
      </div>

      <div
        v-else-if="stockLoading"
        class="mt-1.5 flex flex-col items-center justify-center gap-2 rounded-md bg-elevated px-2.5 py-6 text-[12px] text-txt3"
        role="status"
        aria-busy="true"
        data-testid="tag-filter-loading"
      >
        <AppSpinner :size="16" class="text-accent" />
        <span>{{ t('common.loading.inProgress') }}</span>
      </div>

      <div
        v-else-if="emptyHint"
        class="mt-1.5 rounded-md bg-elevated px-2.5 py-2 text-[12px] text-txt3"
        data-testid="tag-filter-empty"
      >
        {{ emptyHint }}
      </div>

      <div
        v-else
        class="scroll-area mt-1 max-h-40 overflow-y-auto"
        data-testid="tag-filter-suggestions"
      >
        <button
          v-for="tag in filteredStock"
          :key="tag"
          type="button"
          class="flex w-full items-center justify-between gap-2 rounded-md px-2 py-2 text-left text-sm transition hover:bg-elevated md:py-1.5"
          :class="selectedSet.has(tag) ? 'bg-accent-dim text-txt' : 'text-txt2'"
          @click="toggleTag(tag)"
        >
          <span class="min-w-0 truncate">{{ tag }}</span>
          <span v-if="selectedSet.has(tag)" class="shrink-0 text-[11px] text-accent-2">
            {{ t('common.tagFilter.selectedMark') }}
          </span>
        </button>
      </div>
    </div>
    </Transition>
  </div>
</template>

<style scoped>
.tag-filter-chevron {
  transition: transform var(--dur-ui) var(--ease-out-expo);
}
.tag-filter-chevron.is-open {
  transform: rotate(180deg);
}
</style>

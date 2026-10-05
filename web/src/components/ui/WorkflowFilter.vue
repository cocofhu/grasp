<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import AppInlineError from './AppInlineError.vue'
import AppSpinner from './AppSpinner.vue'
import { api } from '@/lib/api/api'
import { createTimeoutController, isAbortError } from '@/lib/shared/loadingRequest'
import { DEFAULT_LOADING_TIMEOUT_MS } from '@/lib/shared/loadingTypes'
import { WORKFLOW_FILTER_KEYS } from '@/lib/composables/useWorkflowFilter'
import type { Workflow } from '@/lib/shared/types'

// A polished, searchable workflow selector. `modelValue` is the
// selected workflow id ('' = 全部工作流). `count` optionally shows how many
// items on the host view match the current selection.
const props = withDefaults(
  defineProps<{ modelValue: string; count?: number; open?: boolean }>(),
  {
    modelValue: '',
    open: undefined,
  },
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'update:open', v: boolean): void
}>()

const { t } = useI18n()
const workflows = ref<Workflow[]>([])
const loading = ref(false)
const loadError = ref<string | null>(null)
const internalOpen = ref(false)
const search = ref('')
const root = ref<HTMLElement | null>(null)

const isControlled = computed(() => props.open !== undefined)
const open = computed({
  get: () => (isControlled.value ? props.open! : internalOpen.value),
  set: (v: boolean) => {
    if (isControlled.value) emit('update:open', v)
    else internalOpen.value = v
  },
})

const selectedName = computed(() => {
  if (!props.modelValue) return t(WORKFLOW_FILTER_KEYS.all)
  return workflows.value.find((w) => w.id === props.modelValue)?.name ?? t(WORKFLOW_FILTER_KEYS.all)
})

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return workflows.value
  return workflows.value.filter((w) => w.name.toLowerCase().includes(q))
})

function choose(id: string) {
  emit('update:modelValue', id)
  open.value = false
  search.value = ''
}

function toggle() {
  open.value = !open.value
  if (open.value) search.value = ''
}

function onDocClick(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}

async function loadWorkflows() {
  loading.value = true
  loadError.value = null
  const tc = createTimeoutController(DEFAULT_LOADING_TIMEOUT_MS)
  try {
    workflows.value = await api.listWorkflows({ signal: tc.signal })
  } catch (err) {
    if (isAbortError(err) && !tc.timedOut) return
    // Do not swallow into empty list — surface error + retry (plan g4.2).
    loadError.value = tc.timedOut
      ? String(t('common.loading.timeout'))
      : err instanceof Error && err.message
        ? err.message
        : String(t('common.workflowFilter.loadFailed'))
  } finally {
    tc.clear()
    loading.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  void loadWorkflows()
})
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div ref="root" class="relative w-full md:w-auto" data-testid="workflow-filter">
    <button
      type="button"
      class="toolbar-control flex w-full items-center gap-2 border border-line bg-surface text-sm text-txt2 transition hover:bg-elevated md:w-auto"
      :class="{ 'border-accent/60 text-txt': modelValue }"
      @click.stop="toggle"
    >
      <Icon name="branch" :size="15" class="text-txt3" />
      <span class="min-w-0 flex-1 truncate text-left md:max-w-[160px] md:flex-none">{{ selectedName }}</span>
      <span v-if="typeof count === 'number'" class="chip">{{ count }}</span>
      <Icon name="chevron-down" :size="14" class="shrink-0 text-txt3" />
    </button>

    <Transition name="overlay-pop">
      <div
        v-if="open"
        class="card absolute left-0 right-0 z-30 mt-1.5 overflow-hidden md:left-auto md:right-0 md:w-64"
        data-testid="workflow-filter-panel"
      >
        <template v-if="loadError">
          <div class="p-2">
            <AppInlineError
              :title="t('common.workflowFilter.loadFailed')"
              :message="loadError"
              @retry="loadWorkflows"
            />
          </div>
        </template>
        <div
          v-else-if="loading"
          class="flex flex-col items-center justify-center gap-2 px-2 py-8 text-[12px] text-txt3"
          role="status"
          aria-busy="true"
          data-testid="workflow-filter-loading"
        >
          <AppSpinner :size="16" class="animate-spin text-accent" />
          <span>{{ t('common.loading.inProgress') }}</span>
        </div>
        <template v-else>
          <div class="border-b border-line p-2">
            <div class="relative">
              <Icon name="search" :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-txt3" />
              <input v-model="search" class="input pl-8" :placeholder="t(WORKFLOW_FILTER_KEYS.searchPlaceholder)" @click.stop />
            </div>
          </div>
          <div class="scroll-area max-h-64 overflow-y-auto p-1">
            <button
              type="button"
              class="flex w-full items-center gap-2 px-2.5 py-2.5 text-left text-sm transition hover:bg-elevated md:py-2"
              :class="!modelValue ? 'bg-accent-dim text-txt' : 'text-txt2'"
              @click.stop="choose('')"
            >
              <Icon name="branch" :size="14" class="text-txt3" />
              <span class="flex-1 truncate">{{ t(WORKFLOW_FILTER_KEYS.all) }}</span>
              <Icon v-if="!modelValue" name="check" :size="14" class="text-accent-2" />
            </button>
            <button
              v-for="w in filtered"
              :key="w.id"
              type="button"
              class="flex w-full items-center gap-2 px-2.5 py-2.5 text-left text-sm transition hover:bg-elevated md:py-2"
              :class="modelValue === w.id ? 'bg-accent-dim text-txt' : 'text-txt2'"
              @click.stop="choose(w.id)"
            >
              <Icon name="branch" :size="14" class="text-txt3" />
              <span class="flex-1 truncate">{{ w.name }}</span>
              <Icon v-if="modelValue === w.id" name="check" :size="14" class="text-accent-2" />
            </button>
            <div v-if="!filtered.length" class="px-2.5 py-6 text-center text-[12px] text-txt3">{{ t(WORKFLOW_FILTER_KEYS.noMatch) }}</div>
          </div>
        </template>
      </div>
    </Transition>
  </div>
</template>

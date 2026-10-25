<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import { STATUS_FILTER_OPTIONS, normalizeStatuses } from '@/lib/composables/useStatusFilter'

const props = withDefaults(
  defineProps<{ modelValue: string[]; count?: number; open?: boolean }>(),
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

const isControlled = computed(() => props.open !== undefined)
const open = computed({
  get: () => (isControlled.value ? props.open! : internalOpen.value),
  set: (v: boolean) => {
    if (isControlled.value) emit('update:open', v)
    else internalOpen.value = v
  },
})

const allSelected = computed(() => props.modelValue.length === 0)

const options = computed(() =>
  STATUS_FILTER_OPTIONS.map((s) => ({ ...s, label: t(s.labelKey) })),
)

const buttonLabel = computed(() => {
  if (allSelected.value) return t('common.status.all')
  const n = props.modelValue.length
  if (n === 1) {
    const opt = STATUS_FILTER_OPTIONS.find((s) => s.id === props.modelValue[0])
    return opt ? t(opt.labelKey) : t('common.status.all')
  }
  return t('common.statusFilter.selectedCount', { n })
})

const hasFilter = computed(() => props.modelValue.length > 0)

function isChecked(id: string): boolean {
  if (id === '') return allSelected.value
  return props.modelValue.includes(id)
}

function isRowSelected(id: string): boolean {
  return id !== '' && props.modelValue.includes(id)
}

function toggleStatus(id: string) {
  if (id === '') {
    emit('update:modelValue', [])
    return
  }

  let next = [...props.modelValue]
  if (next.includes(id)) {
    next = next.filter((s) => s !== id)
    if (next.length === 0) {
      emit('update:modelValue', [])
      return
    }
  } else {
    next.push(id)
  }

  emit('update:modelValue', normalizeStatuses(next))
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
  <div ref="root" class="relative w-full md:w-auto">
    <button
      type="button"
      class="toolbar-control flex w-full items-center gap-2 border border-line bg-surface text-sm text-txt2 transition hover:bg-elevated md:w-auto"
      :class="{ 'border-accent/60 text-txt': hasFilter }"
      @click.stop="toggle"
    >
      <Icon name="clock" :size="15" class="text-txt3" />
      <span class="min-w-0 flex-1 truncate text-left md:max-w-[160px] md:flex-none">{{ buttonLabel }}</span>
      <span v-if="typeof count === 'number'" class="chip">{{ count }}</span>
      <Icon name="chevron-down" :size="14" class="shrink-0 text-txt3" />
    </button>

    <Transition name="overlay-pop">
      <div
        v-if="open"
        class="card absolute left-0 right-0 z-30 mt-1.5 overflow-hidden md:left-auto md:right-0 md:w-56"
      >
        <div class="scroll-area max-h-72 overflow-y-auto p-1">
          <button
            v-for="s in options"
            :key="s.id || 'all'"
            type="button"
            class="flex w-full items-center gap-2 px-2.5 py-2.5 text-left text-sm transition hover:bg-elevated md:py-2"
            :class="isRowSelected(s.id) ? 'bg-accent-dim text-txt' : 'text-txt2'"
            @click.stop="toggleStatus(s.id)"
          >
            <span
              class="status-filter-cb flex h-[15px] w-[15px] shrink-0 items-center justify-center border border-line-strong bg-base transition"
              :class="{ 'status-filter-cb--checked': isChecked(s.id) }"
            >
              <Icon name="check" :size="10" :interactive="false" :motion-key="isChecked(s.id) ? 1 : 0" :style="{ opacity: isChecked(s.id) ? 1 : 0 }" class="text-white" />
            </span>
            <Icon
              :name="s.icon"
              :size="14"
              :class="s.cls"
            />
            <span class="flex-1 truncate">{{ s.label }}</span>
          </button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.status-filter-cb--checked {
  border-color: #7b61ff;
  background: #7b61ff;
  box-shadow: 0 0 0 1px #7b61ff;
}

html.light .status-filter-cb--checked {
  border-color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent-2));
  box-shadow: 0 0 0 1px rgb(var(--c-accent-2));
}
</style>

<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'

/** Inline branch override; empty means the project's default branch. */
const props = withDefaults(defineProps<{ modelValue: string; defaultBranch?: string; disabled?: boolean }>(), {
  defaultBranch: '',
  disabled: false,
})
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()
const { t } = useI18n()

const editing = ref(false)
const draft = ref('')
const input = ref<HTMLInputElement | null>(null)

async function edit() {
  if (props.disabled) return
  draft.value = props.modelValue
  editing.value = true
  await nextTick()
  input.value?.select()
}

function commit() {
  if (!editing.value) return
  editing.value = false
  emit('update:modelValue', draft.value.trim().replace(/[\s|,]/g, ''))
}

function cancel() {
  editing.value = false
}
</script>

<template>
  <div class="solo-branch" data-testid="solo-branch-chip">
    <Icon name="branch" :size="13" class="shrink-0 text-txt3" />
    <input
      v-if="editing"
      ref="input"
      v-model="draft"
      class="solo-branch__input"
      :placeholder="defaultBranch || t('pages.solo.create.branchPlaceholder')"
      :aria-label="t('pages.solo.create.branch')"
      data-testid="solo-branch-input"
      @keydown.enter.prevent="commit"
      @keydown.esc.prevent="cancel"
      @blur="commit"
    />
    <button
      v-else
      type="button"
      class="solo-branch__value"
      :class="{ 'solo-branch__value--default': !modelValue }"
      :disabled="disabled"
      :title="t('pages.solo.create.branch')"
      @click="edit"
    >
      {{ modelValue || defaultBranch || t('pages.solo.create.branchPlaceholder') }}
    </button>
  </div>
</template>

<style scoped>
.solo-branch {
  display: inline-flex;
  height: 32px;
  min-width: 0;
  max-width: 11rem;
  align-items: center;
  gap: 5px;
  border: 1px dashed rgb(var(--c-line));
  border-radius: 8px;
  padding: 0 8px;
}
.solo-branch:focus-within,
.solo-branch:hover {
  border-color: rgb(var(--c-accent) / 0.35);
}
.solo-branch__value,
.solo-branch__input {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  background: transparent;
  color: rgb(var(--c-txt));
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 11.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
  outline: none;
}
.solo-branch__value {
  text-align: left;
}
.solo-branch__value--default {
  color: rgb(var(--c-txt3));
}
.solo-branch__input {
  width: 7.5rem;
}
</style>

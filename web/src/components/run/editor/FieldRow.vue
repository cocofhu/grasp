<script setup lang="ts">
import { computed } from 'vue'
import { useEditorForm } from './editorContext'

const props = withDefaults(
  defineProps<{
    label: string
    path: string
    required?: boolean
    hint?: string
    /** Stack label above the control (lists, long text). */
    stacked?: boolean
  }>(),
  { required: false, hint: '', stacked: false },
)

const form = useEditorForm()
const error = computed(() => (form?.errorPath.value === props.path ? form.errorMessage.value : ''))
</script>

<template>
  <div
    class="ed-row"
    :class="{ 'ed-row--stacked': stacked || !label, 'ed-row--invalid': !!error }"
    :data-field-path="path"
  >
    <span v-if="label" class="ed-row__label">
      {{ label }}<span v-if="required" class="ed-req" aria-hidden="true">*</span>
    </span>
    <div class="ed-row__control">
      <slot />
      <p v-if="hint" class="ed-row__hint">{{ hint }}</p>
      <p v-if="error" class="ed-row__error" role="alert" data-testid="artifact-editor-field-error">{{ error }}</p>
    </div>
  </div>
</template>

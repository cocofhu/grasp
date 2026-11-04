<script setup lang="ts">
import { computed } from 'vue'
import { artifactFormFor, type ArtifactEditorMode } from '@/lib/run/artifactForms'
import CodeArtifactEditor from './CodeArtifactEditor.vue'
import StructuredArtifactForm from './StructuredArtifactForm.vue'
import './artifactEditor.css'

const props = withDefaults(
  defineProps<{
    name: string
    kind: string
    content: string
    /** Structured artifacts only: form view or raw JSON. */
    mode?: ArtifactEditorMode
    fieldErrors?: Record<string, string>
    disabled?: boolean
  }>(),
  { mode: 'form', fieldErrors: () => ({}), disabled: false },
)
const emit = defineEmits<{ (e: 'update:content', v: string): void }>()

const desc = computed(() => artifactFormFor(props.name))
const showForm = computed(() => !!desc.value && props.mode === 'form')
</script>

<template>
  <div class="artifact-editor" data-testid="artifact-editor" :data-mode="showForm ? 'form' : 'code'">
    <StructuredArtifactForm
      v-if="showForm && desc"
      :desc="desc"
      :content="content"
      :field-errors="fieldErrors"
      :disabled="disabled"
      @update:content="emit('update:content', $event)"
    />
    <CodeArtifactEditor
      v-else
      :kind="kind"
      :name="name"
      :content="content"
      :disabled="disabled"
      @update:content="emit('update:content', $event)"
    />
  </div>
</template>

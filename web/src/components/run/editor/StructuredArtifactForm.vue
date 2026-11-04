<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  matchFieldPath,
  parseFormContent,
  serializeForm,
  type ArtifactFormDesc,
  type FieldDesc,
} from '@/lib/run/artifactForms'
import EditorSection from './EditorSection.vue'
import FormFields from './FormFields.vue'
import { provideEditorForm, useFieldLabel } from './editorContext'

const props = withDefaults(
  defineProps<{
    desc: ArtifactFormDesc
    content: string
    /** Server field path → message (one entry from the last failed save). */
    fieldErrors?: Record<string, string>
    disabled?: boolean
  }>(),
  { fieldErrors: () => ({}), disabled: false },
)
const emit = defineEmits<{ (e: 'update:content', v: string): void }>()
const { t } = useI18n()
const label = useFieldLabel()

const root = ref<HTMLElement | null>(null)
const form = ref<Record<string, unknown> | null>(null)
let lastEmitted: string | null = null

watch(
  () => props.content,
  (c) => {
    if (c === lastEmitted) return
    form.value = parseFormContent(props.desc, c)
  },
  { immediate: true },
)

watch(
  form,
  (f) => {
    if (!f) return
    const next = serializeForm(props.desc, f)
    if (next === lastEmitted) return
    lastEmitted = next
    emit('update:content', next)
  },
  { deep: true },
)

const scalarKinds = new Set<FieldDesc['kind']>(['text', 'textarea', 'code', 'enum', 'bool', 'tri', 'number', 'auto'])
const overviewFields = computed(() => props.desc.fields.filter((f) => scalarKinds.has(f.kind)))
const groupFields = computed(() => props.desc.fields.filter((f) => !scalarKinds.has(f.kind)))

const errorPath = ref<string | null>(null)
const errorMessage = ref('')
provideEditorForm({
  errorPath: computed(() => errorPath.value),
  errorMessage: computed(() => errorMessage.value),
  disabled: computed(() => props.disabled),
})

watch(
  () => props.fieldErrors,
  async (errs) => {
    const [field, msg] = Object.entries(errs ?? {})[0] ?? []
    if (!field) {
      errorPath.value = null
      errorMessage.value = ''
      return
    }
    await nextTick()
    const nodes = root.value ? Array.from(root.value.querySelectorAll<HTMLElement>('[data-field-path]')) : []
    const paths = nodes.map((n) => n.dataset.fieldPath || '')
    const hit = matchFieldPath(field, paths)
    errorPath.value = hit ?? field.split(/[.[]/)[0] ?? null
    errorMessage.value = msg ?? ''
    await nextTick()
    const el = nodes.find((n) => n.dataset.fieldPath === errorPath.value && n.classList.contains('ed-row'))
      ?? nodes.find((n) => n.dataset.fieldPath === errorPath.value)
    el?.scrollIntoView?.({ block: 'center', behavior: 'smooth' })
  },
  { immediate: true, deep: true },
)

function sectionCount(f: FieldDesc): number | null {
  const v = form.value?.[f.key]
  return Array.isArray(v) ? v.length : null
}

function sectionInvalid(f: FieldDesc): boolean {
  const p = errorPath.value
  return !!p && (p === f.key || p.startsWith(`${f.key}.`) || p.startsWith(`${f.key}[`))
}
</script>

<template>
  <div ref="root" class="ed-form" data-testid="artifact-editor-form">
    <p v-if="!form" class="ed-banner ed-banner--err" role="alert">{{ t('pages.artifactEditor.formUnavailable') }}</p>
    <template v-else>
      <EditorSection
        v-if="overviewFields.length"
        :title="t('pages.artifactEditor.overview')"
        :invalid="overviewFields.some(sectionInvalid)"
      >
        <FormFields v-model="form" :fields="overviewFields" path="" />
      </EditorSection>
      <EditorSection
        v-for="f in groupFields"
        :key="f.key"
        :title="label(f.key)"
        :path="f.key"
        :count="sectionCount(f)"
        :required="f.required"
        :invalid="sectionInvalid(f)"
      >
        <FormFields v-model="form" :fields="[f]" path="" bare />
      </EditorSection>
    </template>
  </div>
</template>

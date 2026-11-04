<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import ParagraphInput from '@/components/ui/ParagraphInput.vue'
import AppButton from '@/components/ui/AppButton.vue'
import { emptyFieldValue, type FieldDesc } from '@/lib/run/artifactForms'
import EnumSelect from './EnumSelect.vue'
import FieldRow from './FieldRow.vue'
import ObjectListField from './ObjectListField.vue'
import DiagramListField from './DiagramListField.vue'
import StringListField from './StringListField.vue'
import { joinPath, useEditorForm, useFieldLabel } from './editorContext'

const props = defineProps<{ fields: readonly FieldDesc[]; path: string; bare?: boolean }>()
const model = defineModel<Record<string, unknown>>({ required: true })
const { t } = useI18n()
const label = useFieldLabel()
const form = useEditorForm()

function rowLabel(key: string): string {
  return props.bare ? '' : label(key)
}

function str(key: string): string {
  const v = model.value[key]
  return v == null ? '' : String(v)
}

function set(key: string, v: unknown) {
  model.value[key] = v
}

function triValue(key: string): string {
  const v = model.value[key]
  return v === true ? 'true' : v === false ? 'false' : ''
}

function setTri(key: string, v: string) {
  if (v === '') delete model.value[key]
  else model.value[key] = v === 'true'
}

function isDiagramList(f: FieldDesc): boolean {
  return f.kind === 'list' && f.key === 'diagrams'
}

function addObject(f: FieldDesc) {
  model.value[f.key] = emptyFieldValue({ ...f, optional: false })
}
</script>

<template>
  <div class="ed-fields">
    <template v-for="f in props.fields" :key="f.key">
      <FieldRow
        v-if="f.kind === 'text' || f.kind === 'number'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :required="f.required"
      >
        <input
          class="ed-input"
          :type="f.kind === 'number' ? 'number' : 'text'"
          :value="str(f.key)"
          :disabled="form?.disabled.value"
          :aria-label="label(f.key)"
          @input="set(f.key, f.kind === 'number'
            ? (($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value))
            : ($event.target as HTMLInputElement).value)"
        >
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'textarea'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :required="f.required"
        stacked
      >
        <ParagraphInput
          :text="str(f.key)"
          text-only
          compact
          :disabled="form?.disabled.value"
          :placeholder="label(f.key)"
          @update:text="set(f.key, $event)"
        />
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'code'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :required="f.required"
        stacked
      >
        <textarea
          class="ed-input ed-code"
          rows="6"
          spellcheck="false"
          :value="str(f.key)"
          :disabled="form?.disabled.value"
          :aria-label="label(f.key)"
          @input="set(f.key, ($event.target as HTMLTextAreaElement).value)"
        />
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'enum'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :required="f.required"
      >
        <EnumSelect
          :model-value="str(f.key)"
          :options="f.options ?? []"
          :required="f.required"
          :disabled="form?.disabled.value"
          :aria-label="label(f.key)"
          @update:model-value="set(f.key, $event)"
        />
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'bool'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :required="f.required"
      >
        <label class="ed-check">
          <input
            type="checkbox"
            :checked="model[f.key] === true"
            :disabled="form?.disabled.value"
            @change="set(f.key, ($event.target as HTMLInputElement).checked)"
          >
          <span>{{ label(f.key) }}</span>
        </label>
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'tri'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
      >
        <EnumSelect
          :model-value="triValue(f.key)"
          :options="['true', 'false']"
          :disabled="form?.disabled.value"
          :aria-label="label(f.key)"
          @update:model-value="setTri(f.key, $event)"
        />
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'auto'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :hint="t('pages.artifactEditor.autoHint')"
      >
        <span class="ed-auto">{{ str(f.key) || '0' }}</span>
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'strings'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :required="f.required"
        stacked
      >
        <StringListField
          :model-value="(model[f.key] as string[]) ?? []"
          :path="joinPath(props.path, f.key)"
          :label="label(f.key)"
          @update:model-value="set(f.key, $event)"
        />
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'list'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        :required="f.required"
        :hint="f.hint ? t(`pages.artifactEditor.${f.hint}`) : ''"
        stacked
      >
        <DiagramListField
          v-if="isDiagramList(f)"
          :model-value="(model[f.key] as Record<string, unknown>[]) ?? []"
          :desc="f"
          :path="joinPath(props.path, f.key)"
          @update:model-value="set(f.key, $event)"
        />
        <ObjectListField
          v-else
          :model-value="(model[f.key] as Record<string, unknown>[]) ?? []"
          :desc="f"
          :path="joinPath(props.path, f.key)"
          @update:model-value="set(f.key, $event)"
        />
      </FieldRow>
      <FieldRow
        v-else-if="f.kind === 'object'"
        :label="rowLabel(f.key)"
        :path="joinPath(props.path, f.key)"
        stacked
      >
        <div v-if="model[f.key] && typeof model[f.key] === 'object'" class="ed-object">
          <FormFields
            :model-value="model[f.key] as Record<string, unknown>"
            :fields="f.fields ?? []"
            :path="joinPath(props.path, f.key)"
          />
        </div>
        <AppButton
          v-else
          size="sm"
          variant="ghost"
          icon="plus"
          :disabled="form?.disabled.value"
          @click="addObject(f)"
        >
          {{ t('pages.artifactEditor.addSection', { name: label(f.key) }) }}
        </AppButton>
      </FieldRow>
    </template>
  </div>
</template>

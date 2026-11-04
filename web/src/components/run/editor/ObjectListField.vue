<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import { emptyListItem, type FieldDesc } from '@/lib/run/artifactForms'
import FormFields from './FormFields.vue'
import { joinPath, useEditorForm, useFieldLabel } from './editorContext'

type Item = Record<string, unknown>

const props = withDefaults(defineProps<{ desc: FieldDesc; path: string; variant?: 'default' | 'diagram' }>(), {
  variant: 'default',
})
const model = defineModel<Item[]>({ default: () => [] })
const { t } = useI18n()
const label = useFieldLabel()
const form = useEditorForm()
const undo = ref<{ index: number; value: Item; title: string } | null>(null)

function itemTitle(item: Item, i: number): string {
  const key = props.desc.titleKey
  const v = key ? String(item[key] ?? '').trim() : ''
  return v || t('pages.artifactEditor.untitledItem', { n: i + 1 })
}

function itemId(item: Item): string {
  const key = props.desc.idKey
  return key ? String(item[key] ?? '').trim() : ''
}

function add() {
  model.value = [...model.value, emptyListItem(props.desc)]
  undo.value = null
}

function remove(i: number) {
  const value = model.value[i]
  if (!value) return
  undo.value = { index: i, value, title: itemTitle(value, i) }
  model.value = model.value.filter((_, j) => j !== i)
}

function restore() {
  const u = undo.value
  if (!u) return
  const next = [...model.value]
  next.splice(Math.min(u.index, next.length), 0, u.value)
  model.value = next
  undo.value = null
}

function move(i: number, d: -1 | 1) {
  const j = i + d
  if (j < 0 || j >= model.value.length) return
  const next = [...model.value]
  ;[next[i], next[j]] = [next[j]!, next[i]!]
  model.value = next
}
</script>

<template>
  <div class="ed-list" :class="`ed-list--${variant}`" data-testid="artifact-editor-list" :data-list-key="desc.key">
    <article
      v-for="(item, i) in model"
      :key="i"
      class="ed-item"
      :data-field-path="joinPath(props.path, i)"
      data-testid="artifact-editor-item"
    >
      <header class="ed-item__head">
        <span v-if="desc.idKey" class="ed-item__id" :title="t('pages.artifactEditor.idReadonly')">
          {{ itemId(item) || t('pages.artifactEditor.newItem') }}
        </span>
        <span class="ed-item__title">{{ itemTitle(item, i) }}</span>
        <span class="ed-tools">
          <button
            type="button"
            class="ed-tool"
            :disabled="i === 0 || form?.disabled.value"
            :aria-label="t('pages.artifactEditor.moveUp')"
            @click="move(i, -1)"
          >
            <Icon name="arrow-up" :size="14" />
          </button>
          <button
            type="button"
            class="ed-tool ed-tool--down"
            :disabled="i === model.length - 1 || form?.disabled.value"
            :aria-label="t('pages.artifactEditor.moveDown')"
            @click="move(i, 1)"
          >
            <Icon name="arrow-up" :size="14" />
          </button>
          <button
            type="button"
            class="ed-tool ed-tool--danger"
            :disabled="form?.disabled.value"
            :aria-label="t('pages.artifactEditor.removeItem', { name: itemTitle(item, i) })"
            data-testid="artifact-editor-item-remove"
            @click="remove(i)"
          >
            <Icon name="trash" :size="14" />
          </button>
        </span>
      </header>
      <FormFields :model-value="item" :fields="desc.fields ?? []" :path="joinPath(props.path, i)" />
    </article>
    <div v-if="undo" class="ed-undo" role="status" data-testid="artifact-editor-undo-bar">
      <span>{{ t('pages.artifactEditor.removedItem', { name: undo.title }) }}</span>
      <button type="button" class="ed-undo__btn" data-testid="artifact-editor-undo" @click="restore">
        {{ t('pages.artifactEditor.undo') }}
      </button>
    </div>
    <AppButton
      size="sm"
      variant="ghost"
      icon="plus"
      :disabled="form?.disabled.value"
      data-testid="artifact-editor-item-add"
      @click="add"
    >
      {{ t('pages.artifactEditor.addNamed', { name: label(desc.key) }) }}
    </AppButton>
  </div>
</template>

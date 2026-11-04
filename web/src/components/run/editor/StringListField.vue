<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import { joinPath, useEditorForm } from './editorContext'

const props = defineProps<{ path: string; label: string }>()
const model = defineModel<string[]>({ default: () => [] })
const { t } = useI18n()
const form = useEditorForm()
const root = ref<HTMLElement | null>(null)
const undo = ref<{ index: number; value: string } | null>(null)

function setAt(i: number, v: string) {
  const next = [...model.value]
  next[i] = v
  model.value = next
}

async function add() {
  model.value = [...model.value, '']
  undo.value = null
  await nextTick()
  const inputs = root.value?.querySelectorAll<HTMLInputElement>('input.ed-input')
  inputs?.[inputs.length - 1]?.focus()
}

function remove(i: number) {
  undo.value = { index: i, value: model.value[i] ?? '' }
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
  <div ref="root" class="ed-strings" data-testid="artifact-editor-strings">
    <div
      v-for="(item, i) in model"
      :key="i"
      class="ed-strings__item"
      :data-field-path="joinPath(props.path, i)"
    >
      <input
        class="ed-input"
        :value="item"
        :disabled="form?.disabled.value"
        :aria-label="`${label} ${i + 1}`"
        @input="setAt(i, ($event.target as HTMLInputElement).value)"
      >
      <span class="ed-tools">
        <button type="button" class="ed-tool" :disabled="i === 0" :aria-label="t('pages.artifactEditor.moveUp')" @click="move(i, -1)">
          <Icon name="arrow-up" :size="14" />
        </button>
        <button
          type="button"
          class="ed-tool ed-tool--down"
          :disabled="i === model.length - 1"
          :aria-label="t('pages.artifactEditor.moveDown')"
          @click="move(i, 1)"
        >
          <Icon name="arrow-up" :size="14" />
        </button>
        <button type="button" class="ed-tool ed-tool--danger" :aria-label="t('pages.artifactEditor.remove')" @click="remove(i)">
          <Icon name="trash" :size="14" />
        </button>
      </span>
    </div>
    <div v-if="undo" class="ed-undo" role="status">
      <span>{{ t('pages.artifactEditor.removed') }}</span>
      <button type="button" class="ed-undo__btn" data-testid="artifact-editor-undo" @click="restore">
        {{ t('pages.artifactEditor.undo') }}
      </button>
    </div>
    <AppButton size="sm" variant="ghost" icon="plus" :disabled="form?.disabled.value" @click="add">
      {{ t('pages.artifactEditor.addItem') }}
    </AppButton>
  </div>
</template>

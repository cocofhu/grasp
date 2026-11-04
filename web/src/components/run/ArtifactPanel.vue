<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import ArtifactList from './ArtifactList.vue'
import ArtifactPreview from './ArtifactPreview.vue'
import type { Artifact, Run } from '@/lib/shared/types'
import { useArtifactEditor } from '@/lib/run/useArtifactEditor'

const props = withDefaults(
  defineProps<{
    artifacts: Artifact[]
    scope?: 'run' | 'platform'
    /** Run detail: allow editing this run's artifacts. */
    editable?: boolean
    run?: Run | null
  }>(),
  { scope: 'run', editable: false, run: null },
)

const emit = defineEmits<{
  deleted: [id: string]
}>()

const active = ref<Artifact | null>(null)
/** Prefer this index after parent removes a deleted artifact from the list. */
const preferNextIndex = ref<number | null>(null)

const editor = useArtifactEditor(() => props.run?.id || active.value?.runId || '')
/** The drawer is narrow; while editing the form gets the full width. */
const editingActive = computed(() => props.editable && !!active.value && !!editor.draft(active.value.name))

/** Drop the open draft (asking first when it has edits); false keeps the user here. */
function releaseActive(): boolean {
  const name = active.value?.name
  if (!props.editable || !name || !editor.draft(name)) return true
  if (!editor.confirmLeave([name])) return false
  editor.discard(name)
  return true
}

function select(a: Artifact) {
  if (a.id !== active.value?.id && !releaseActive()) return
  active.value = a
}

defineExpose({ releaseActive })

function onDeleted(id: string) {
  const idx = props.artifacts.findIndex((a) => a.id === id)
  preferNextIndex.value = idx >= 0 ? idx : null
  emit('deleted', id)
}

onMounted(() => {
  if (!active.value && props.artifacts.length) active.value = props.artifacts[0]
})

watch(
  () => props.artifacts.map((a) => a.id).join(','),
  () => {
    if (active.value && !props.artifacts.some((a) => a.id === active.value!.id)) {
      if (!props.artifacts.length) {
        active.value = null
        preferNextIndex.value = null
        return
      }
      if (preferNextIndex.value != null) {
        const i = Math.min(preferNextIndex.value, props.artifacts.length - 1)
        active.value = props.artifacts[i]
        preferNextIndex.value = null
      } else {
        active.value = props.artifacts[0]
      }
    }
  },
)
</script>

<template>
  <div class="flex h-full">
    <div
      v-show="!editingActive"
      class="flex w-[40%] min-w-[180px] flex-col border-r border-line"
      data-testid="artifact-panel-list"
    >
      <ArtifactList
        :artifacts="artifacts"
        :active-id="active?.id"
        :scope="scope"
        @select="select"
      />
    </div>
    <ArtifactPreview
      :artifact="active"
      :scope="scope"
      :artifacts="artifacts"
      :run-id="active?.runId"
      :run="run"
      :editable="editable"
      @deleted="onDeleted"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Pagination from '@/components/ui/Pagination.vue'
import ArtifactPreview from '@/components/run/ArtifactPreview.vue'
import { api } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import type { Artifact, Run } from '@/lib/shared/types'
import type { ConversationArtifactTarget } from '@/lib/chat/conversationArtifacts'

const props = defineProps<{ target: ConversationArtifactTarget | null }>()
const { t } = useI18n()
const candidates = ref<Artifact[]>([])
const selected = ref<Artifact | null>(null)
const run = ref<Run | null>(null)
const loading = ref(false)
const failed = ref(false)
const deleted = ref(false)
const page = ref(1)
const total = ref(0)
const PAGE_SIZE = 20
let generation = 0
let selectionGeneration = 0
const searchingByName = computed(() => !!props.target?.name && !props.target?.id)

async function selectArtifact(artifact: Artifact) {
  const gen = ++selectionGeneration
  selected.value = artifact
  run.value = null
  if (!artifact.runId) return
  try {
    const full = await api.getRun(artifact.runId)
    if (gen === selectionGeneration && selected.value?.id === artifact.id) run.value = full
  } catch { /* The file remains readable when its run metadata is unavailable. */ }
}

async function loadTarget() {
  const gen = ++generation
  ++selectionGeneration
  const target = props.target
  candidates.value = []
  selected.value = null
  run.value = null
  total.value = 0
  failed.value = false
  deleted.value = false
  loading.value = !!target
  if (!target) return
  try {
    if (target.id) {
      const artifact = await api.artifactContent(target.id)
      if (gen !== generation) return
      if (artifact.id !== target.id) throw new Error('artifact identity mismatch')
      if (target.runId && artifact.runId !== target.runId) throw new Error('artifact scope mismatch')
      void selectArtifact(artifact)
    } else if (target.name && target.runId) {
      const full = await api.getRun(target.runId)
      if (gen !== generation) return
      candidates.value = (full.artifacts || []).filter(file => file.name.toLowerCase() === target.name?.toLowerCase())
      total.value = candidates.value.length
      if (candidates.value.length === 1) {
        selected.value = candidates.value[0]!
        run.value = full
      }
    } else if (target.name) {
      const result = await api.listArtifacts({ q: target.name, page: page.value, pageSize: PAGE_SIZE })
      if (gen !== generation) return
      const lastPage = Math.max(1, Math.ceil(result.total / PAGE_SIZE))
      if (page.value > lastPage) { page.value = lastPage; return }
      candidates.value = result.items.filter(file => file.name.toLowerCase() === target.name?.toLowerCase())
      total.value = result.total
      // A bare filename does not identify a run. Require a deliberate source
      // choice even when this page contains a single match.
    }
  } catch {
    if (gen === generation) failed.value = true
  } finally {
    if (gen === generation) loading.value = false
  }
}

function clearSelection() { ++selectionGeneration; selected.value = null; run.value = null }
async function onDeleted(id: string) {
  candidates.value = candidates.value.filter(file => file.id !== id)
  clearSelection()
  if (searchingByName.value) await loadTarget()
  else deleted.value = true
}
watch(() => props.target, () => { if (page.value !== 1) page.value = 1; else void loadTarget() }, { deep: true, immediate: true })
watch(page, () => void loadTarget())
onBeforeUnmount(() => { ++generation; ++selectionGeneration })
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col" data-testid="conversation-artifact-browser">
    <div v-if="!target" class="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-8 text-center">
      <Icon name="artifact" :size="24" class="text-txt3" />
      <h3 class="text-sm font-medium text-txt">{{ t('pages.conversationWorkspace.chooseArtifact') }}</h3>
      <p class="max-w-xs text-xs leading-6 text-txt3">{{ t('pages.conversationWorkspace.chooseArtifactHint') }}</p>
    </div>
    <div v-else-if="loading" class="flex flex-1 items-center justify-center gap-2 text-xs text-txt3" role="status" data-testid="conversation-artifact-loading"><Icon name="spinner" :size="16" />{{ t('pages.conversationWorkspace.loading') }}</div>
    <div v-else-if="failed" class="flex flex-1 flex-col items-center justify-center gap-3 px-8 text-center" role="status" data-testid="conversation-artifact-error">
      <p class="text-sm text-txt2">{{ t('common.asyncState.loadFailedTitle') }}</p>
      <AppButton size="sm" variant="outline" @click="loadTarget">{{ t('common.buttons.retry') }}</AppButton>
    </div>
    <p v-else-if="deleted" class="flex flex-1 items-center justify-center px-6 text-center text-sm text-txt3" role="status" data-testid="conversation-artifact-deleted">{{ t('pages.conversationWorkspace.deletedArtifact') }}</p>
    <template v-else-if="selected">
      <div v-if="searchingByName" class="flex min-h-9 shrink-0 items-center border-b border-line px-3"><button type="button" class="inline-flex min-h-9 items-center gap-1.5 text-xs text-txt3 hover:text-txt" data-testid="conversation-artifact-back" @click="clearSelection"><Icon name="arrow-left" :size="13" />{{ t('pages.conversationWorkspace.fileSources') }}</button></div>
      <ArtifactPreview :artifact="selected" :run="run" :run-id="selected.runId" :artifacts="run?.artifacts || candidates" scope="platform" @deleted="onDeleted" />
    </template>
    <template v-else>
      <div class="shrink-0 border-b border-line px-4 py-3"><h3 class="truncate text-sm font-medium text-txt" :title="target.name">{{ target.name }}</h3><p class="mt-1 text-xs leading-5 text-txt3">{{ t('pages.conversationWorkspace.selectSource') }}</p></div>
      <div class="scroll-area min-h-0 flex-1 overflow-y-auto px-4" data-testid="conversation-artifact-sources">
        <button v-for="file in candidates" :key="file.id" type="button" class="flex min-h-16 w-full items-center gap-3 border-b border-line/60 py-3 text-left hover:bg-elevated/40" :data-artifact-id="file.id" @click="selectArtifact(file)">
          <Icon name="artifact" :size="17" class="shrink-0 text-txt3" />
          <span class="min-w-0 flex-1"><span class="block truncate text-[13px] font-medium text-txt">{{ file.runTitle || file.workflowName || file.runId }}</span><span class="mt-1 block truncate text-[11px] text-txt3">{{ file.nodeId }} · {{ fmtTime(file.createdAt) }}</span></span>
          <Icon name="chevron-right" :size="13" class="shrink-0 text-txt3" />
        </button>
        <p v-if="!candidates.length" class="py-8 text-center text-xs leading-6 text-txt3" data-testid="conversation-artifact-no-match">{{ t('pages.conversationWorkspace.noMatchingArtifact') }}</p>
      </div>
      <Pagination v-if="total > PAGE_SIZE && !target.runId" v-model:page="page" :total="total" :page-size="PAGE_SIZE" class="shrink-0" />
    </template>
  </div>
</template>

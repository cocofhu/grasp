<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { api, type WorkspaceRevision } from '@/lib/api/api'
import { relTime } from '@/lib/shared/format'
import {
  classifyDiffLine,
  DIFF_LINE_CLASS,
  revisionMatchesFile,
} from '@/lib/agent/workspaceHistoryDiff'

const props = defineProps<{
  agentName: string
  isMobile: boolean
  filePath?: string
  collapsed?: boolean
  refreshKey?: number
}>()

const emit = defineEmits<{
  (e: 'restored'): void
  (e: 'toggle-collapse'): void
}>()

const { t } = useI18n()
const revisions = ref<WorkspaceRevision[]>([])
const loading = ref(false)
const selectedSha = ref('')
const diff = ref('')
const diffLoading = ref(false)
const showDiffModal = ref(false)
const showRestore = ref(false)
const restoreReason = ref('')
const restoring = ref(false)
const error = ref('')

const filteredRevisions = computed(() => {
  if (!props.filePath) return []
  return revisions.value.filter((rev) => revisionMatchesFile(rev.changes, props.filePath!))
})

const diffLines = computed(() => (diff.value ? diff.value.split('\n') : []))

async function loadHistory() {
  if (!props.agentName) return
  loading.value = true
  error.value = ''
  try {
    const res = await api.listAgentWorkspaceRevisions(props.agentName)
    revisions.value = res.revisions || []
  } catch (e: unknown) {
    error.value = String((e as Error)?.message || e)
  } finally {
    loading.value = false
  }
}

async function loadDiff(sha: string) {
  if (!props.agentName || !sha) return
  diffLoading.value = true
  try {
    const res = await api.getAgentWorkspaceRevisionDiff(props.agentName, sha)
    diff.value = res.diff || ''
  } catch {
    diff.value = ''
  } finally {
    diffLoading.value = false
  }
}

function closeDiffModal() {
  showDiffModal.value = false
  selectedSha.value = ''
  diff.value = ''
}

async function openRevision(rev: WorkspaceRevision) {
  selectedSha.value = rev.sha
  showDiffModal.value = true
  diff.value = ''
  await loadDiff(rev.sha)
  restoreReason.value = t('pages.agentStudio.workspaceHistory.restoreDefault', { sha: rev.sha.slice(0, 7) })
}

function openRestore() {
  if (!selectedSha.value) return
  restoreReason.value = t('pages.agentStudio.workspaceHistory.restoreDefault', { sha: selectedSha.value.slice(0, 7) })
  showRestore.value = true
}

async function confirmRestore() {
  if (!selectedSha.value || restoring.value) return
  restoring.value = true
  error.value = ''
  try {
    await api.restoreAgentWorkspaceRevision(props.agentName, selectedSha.value, restoreReason.value.trim() || undefined)
    showRestore.value = false
    closeDiffModal()
    await loadHistory()
    emit('restored')
  } catch (e: unknown) {
    error.value = String((e as Error)?.message || e)
  } finally {
    restoring.value = false
  }
}

function sourceLabel(src: string) {
  const key = `pages.agentStudio.workspaceHistory.source.${src}`
  const label = t(key)
  return label === key ? src : label
}

watch(() => props.agentName, () => {
  closeDiffModal()
  void loadHistory()
}, { immediate: true })

watch(() => props.refreshKey, () => { void loadHistory() })

watch(() => props.filePath, () => {
  closeDiffModal()
})

watch(() => props.collapsed, (collapsed) => {
  if (collapsed) closeDiffModal()
})

defineExpose({ reload: loadHistory })
</script>

<template>
  <div
    v-if="!isMobile"
    class="flex min-h-0 min-w-0 flex-col border-l border-line/60"
    data-test="workspace-history-panel"
  >
  <template v-if="collapsed">
    <div class="flex shrink-0 items-start justify-center border-b border-line pt-1.5">
      <button
        type="button"
        :class="[
          'flex shrink-0 items-center justify-center rounded w-[22px] h-[22px] text-txt3 transition',
          'hover:bg-elevated hover:text-accent-2 focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)] outline-none',
        ]"
        :title="t('pages.agentStudio.workspaceHistory.expand')"
        :aria-label="t('pages.agentStudio.workspaceHistory.expand')"
        data-test="history-expand"
        @click="emit('toggle-collapse')"
      >
        <Icon :interactive="false" name="chevron-right" :size="14" class="rotate-180" />
      </button>
    </div>
  </template>
  <template v-else>
    <div class="flex shrink-0 items-center justify-between border-b border-line px-2.5 py-2">
      <span class="text-[10.5px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.agentStudio.workspaceHistory.title') }}
      </span>
      <button
        type="button"
        :class="[
          'flex shrink-0 items-center justify-center rounded w-[22px] h-[22px] text-txt3 transition',
          'hover:bg-elevated hover:text-accent-2 focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)] outline-none',
        ]"
        :title="t('pages.agentStudio.workspaceHistory.collapse')"
        :aria-label="t('pages.agentStudio.workspaceHistory.collapse')"
        data-test="history-collapse"
        @click="emit('toggle-collapse')"
      >
        <Icon name="chevron-right" :size="14" />
      </button>
    </div>
    <div
      class="shrink-0 border-b border-line px-2.5 py-1.5 font-mono text-[11px] text-txt2"
      data-test="history-file-hint"
    >
      {{ filePath || t('pages.agentStudio.workspaceHistory.noFileOpen') }}
    </div>
    <div v-if="error" class="px-2 py-1 text-[11px] text-err">{{ error }}</div>
    <div v-if="loading" class="px-3 py-4 text-[12px] text-txt3">{{ t('common.loading') }}</div>
    <div v-else class="scroll-area min-h-0 flex-1 overflow-y-auto p-2">
      <div
        v-if="!filePath"
        class="px-2 py-7 text-center text-[12px] leading-relaxed text-txt3"
        data-test="history-empty-no-file"
      >
        {{ t('pages.agentStudio.workspaceHistory.emptyNoFile') }}
      </div>
      <div
        v-else-if="!filteredRevisions.length"
        class="px-2 py-7 text-center text-[12px] leading-relaxed text-txt3"
        data-test="history-empty-no-revisions"
      >
        {{ t('pages.agentStudio.workspaceHistory.emptyNoRevisions') }}
      </div>
      <button
        v-for="rev in filteredRevisions"
        :key="rev.sha"
        type="button"
        class="w-full border-b border-line/60 px-2 py-3 text-left transition-colors duration-[var(--dur-ui)] ease-out"
        :class="rev.sha === selectedSha && showDiffModal
          ? 'bg-elevated text-txt'
          : 'hover:bg-elevated/50'"
        :title="rev.reason"
        data-test="history-revision-row"
        :data-sha="rev.sha"
        @click="openRevision(rev)"
      >
        <div class="truncate text-[12.5px] font-medium text-txt">{{ rev.reason }}</div>
        <div class="mt-1 text-[11px] text-txt3">
          {{ rev.author }}
          <template v-if="rev.createdAt"> · {{ relTime(rev.createdAt) }}</template>
          · {{ t('pages.agentStudio.workspaceHistory.clickToViewDiff') }}
        </div>
        <div class="mt-1.5 flex flex-wrap items-center gap-1">
          <span class="chip border-line text-[10px] text-txt3">{{ sourceLabel(rev.source) }}</span>
          <span class="font-mono text-[10px] text-txt3">{{ rev.sha.slice(0, 7) }}</span>
        </div>
      </button>
    </div>
  </template>

  <AppModal
    :open="showDiffModal"
    :width="760"
    :body-overflow="'hidden'"
    :body-min-height="280"
    close-on-esc
    @close="closeDiffModal"
  >
    <template #header>
      <span class="text-[13px] text-txt">
        {{ t('pages.agentStudio.workspaceHistory.modalDiffTitle', { sha: selectedSha.slice(0, 7) }) }}
        <template v-if="filePath">
          <span class="text-txt3"> · </span>
          <span class="font-mono text-[12px] text-txt2">{{ filePath }}</span>
        </template>
      </span>
    </template>
    <div v-if="diffLoading" class="text-[12px] text-txt3">{{ t('common.loading') }}</div>
    <pre
      v-else
      class="scroll-area max-h-[min(56vh,480px)] overflow-auto whitespace-pre bg-base p-3 font-mono text-[12px] leading-relaxed"
      data-test="history-diff-body"
    >
      <template v-if="!diffLines.length">{{ t('pages.agentStudio.workspaceHistory.noDiff') }}</template>
      <div
        v-for="(line, i) in diffLines"
        :key="i"
        :class="DIFF_LINE_CLASS[classifyDiffLine(line)]"
      >{{ line }}</div>
    </pre>
    <template #footer>
      <AppButton size="sm" variant="ghost" @click="closeDiffModal">{{ t('common.buttons.close') }}</AppButton>
      <AppButton size="sm" variant="outline" class="text-err border-err/35" @click="openRestore">
        {{ t('pages.agentStudio.workspaceHistory.restore') }}
      </AppButton>
    </template>
  </AppModal>

  <AppModal
    :open="showRestore"
    :title="t('pages.agentStudio.workspaceHistory.restoreConfirmTitle')"
    :width="420"
    @close="showRestore = false"
  >
    <p class="text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.workspaceHistory.restoreConfirmBody') }}</p>
    <label class="mb-1 mt-2 block text-[12px] text-txt2">{{ t('pages.agentStudio.workspaceHistory.restoreReason') }}</label>
    <input
      v-model="restoreReason"
      class="w-full rounded border border-line bg-surface px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
    />
    <div class="mt-4 flex justify-end gap-2">
      <AppButton size="sm" variant="ghost" @click="showRestore = false">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton size="sm" variant="danger" :disabled="restoring" @click="confirmRestore">
        {{ restoring ? t('common.buttons.saving') : t('pages.agentStudio.workspaceHistory.restoreConfirm') }}
      </AppButton>
    </div>
  </AppModal>
  </div>
</template>

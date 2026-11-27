<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import AppSpinner from '@/components/ui/AppSpinner.vue'
import Icon from '@/components/ui/Icon.vue'
import { renderMarkdown } from '@/lib/shared/markdown'
import {
  workspaceChangesClient, type WorkspaceChangeFile, type WorkspaceChangesTarget, type WorkspaceChangesView,
} from '@/lib/api/clients/workspaceChangesClient'
import { workspaceDiffLines } from './workspaceDiffLines'
import { highlightWorkspaceDiff, type HighlightedDiffLine } from './workspaceDiffHighlight'

const CodeEditor = defineAsyncComponent(() => import('@/components/ui/CodeEditor.vue'))
const props = defineProps<{
  target: WorkspaceChangesTarget
  view?: WorkspaceChangesView
  loading: boolean
  workspaceLoading: boolean
  error: boolean
  initialRepository?: string
  workspaceLabels?: Record<string, string>
}>()
const emit = defineEmits<{ close: []; refresh: []; 'select-workspace': [id: string]; retry: [] }>()
const { t, locale } = useI18n()
const word = (key: string) => t('pages.workspaceChanges.' + key)
const numerals = computed(() => new Intl.NumberFormat(locale.value))
const turn = computed(() => !!props.target.turnId)
const title = computed(() => word(turn.value ? 'turn' : props.target.kind === 'solo' ? 'sessionTitle' : 'runTitle'))
const snapshot = computed(() => props.view?.snapshot)
const repositories = computed(() => snapshot.value?.repos ?? [])
const totals = computed(() => repositories.value.reduce((total, repo) => ({
  count: total.count + repo.fileCount,
  added: total.added + repo.added,
  deleted: total.deleted + repo.deleted,
}), { count: 0, added: 0, deleted: 0 }))
const available = computed(() => repositories.value.some(repo => !repo.unavailable))
const partial = computed(() => repositories.value.some(repo => repo.unavailable))
const files = computed(() => repositories.value.flatMap(repo =>
  (repo.files ?? []).map(file => ({ repo, file, key: repo.directory + '/' + file.path }))))
const chosen = ref('')
const compactFile = ref(false)
const selected = computed(() => files.value.find(row => row.key === chosen.value))
const selectedFileId = computed(() => selected.value?.file.id)
const payload = shallowRef<WorkspaceChangeFile>()
const fileState = ref<'loading' | 'ready' | 'error'>('ready')
const fileRetry = ref(0)
const mode = ref<'diff' | 'preview' | 'source'>('diff')
const filePane = ref<HTMLElement>()
const fileList = ref<HTMLElement>()

const source = computed(() => {
  if (selected.value?.file.status === 'D') return payload.value?.before
  return payload.value?.after
})
const markdown = computed(() => /\.(?:md|markdown)$/i.test(selected.value?.file.path || ''))
const markdownHTML = computed(() => mode.value === 'preview' && source.value !== undefined ? renderMarkdown(source.value) : '')
const lines = computed(() => workspaceDiffLines(payload.value?.patch ?? ''))
const lineLimit = 3500
const visibleLines = computed<HighlightedDiffLine[]>(() => {
  const visible = lines.value.slice(0, lineLimit)
  const file = selected.value?.file
  return file && payload.value ? highlightWorkspaceDiff(visible, file, payload.value) : visible
})
const language = computed(() => {
  const ext = (selected.value?.file.path || '').split('.').at(-1)?.toLowerCase() ?? ''
  const modes: Record<string, string> = {
    md: 'markdown', markdown: 'markdown', yaml: 'yaml', yml: 'yaml', json: 'json', js: 'javascript',
    mjs: 'javascript', ts: 'typescript', py: 'python', sh: 'shell', bash: 'shell', html: 'html',
    toml: 'ini', ini: 'ini',
  }
  return modes[ext] || 'plaintext'
})
const dateFormat = computed(() => new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }))
function formatTimestamp(raw?: string) {
  if (!raw) return ''
  const date = new Date(raw)
  return Number.isFinite(date.getTime()) ? dateFormat.value.format(date) : ''
}
const timestamp = computed(() => formatTimestamp(snapshot.value?.capturedAt))
const workspaceOptions = computed(() => {
  const choices = turn.value ? [] : props.view?.workspaces ?? []
  const names = choices.map(({ nodeId }, index) =>
    (nodeId && (props.workspaceLabels?.[nodeId] || nodeId)) || t('pages.workspaceChanges.workspaceNumber', { n: index + 1 }))
  return choices.map(({ id, capturedAt }, index) => ({
    value: id,
    label: names[index],
    hint: names.indexOf(names[index]) !== names.lastIndexOf(names[index]) ? formatTimestamp(capturedAt) : undefined,
  }))
})
const currentWorkspace = computed(() => snapshot.value?.workspace || '')
const currentWorkspaceName = computed(() => workspaceOptions.value.find(option => option.value === currentWorkspace.value)?.label)

function selectWorkspace(id: string) {
  if (id !== currentWorkspace.value || props.workspaceLoading || props.error) emit('select-workspace', id)
}

watch(snapshot, () => {
  if (!files.value.some(row => row.key === chosen.value)) {
    chosen.value = files.value.find(row => row.repo.directory === props.initialRepository)?.key || files.value[0]?.key || ''
  }
  if (!chosen.value) compactFile.value = false
}, { immediate: true })

watch([() => snapshot.value?.id, selectedFileId, fileRetry], async ([snapshotId, fileId], _, onCleanup) => {
  payload.value = undefined
  mode.value = 'diff'
  fileState.value = 'ready'
  if (!snapshotId || !fileId || !selected.value) return
  const { file } = selected.value
  if (!file.hasPatch && !file.hasBefore && !file.hasAfter) return
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  fileState.value = 'loading'
  try {
    const result = await workspaceChangesClient.file(props.target, snapshotId, fileId, controller.signal)
    if (controller.signal.aborted) return
    payload.value = result
    fileState.value = 'ready'
  } catch {
    if (!controller.signal.aborted) fileState.value = 'error'
  }
}, { immediate: true })

function selectFile(key: string) {
  chosen.value = key
  compactFile.value = true
  if (window.matchMedia('(max-width: 700px)').matches) {
    void nextTick(() => filePane.value?.focus({ preventScroll: true }))
  }
}
function backToFiles() {
  compactFile.value = false
  void nextTick(() => fileList.value?.querySelector<HTMLElement>('[aria-current="true"]')?.focus({ preventScroll: true }))
}

const statuses: Record<string, string> = { A: 'added', M: 'modified', D: 'deleted', R: 'renamed', C: 'copied', T: 'modified' }
function fileStatus(status: string) {
  return word(statuses[status] ?? 'modified')
}
</script>

<template>
  <AppModal
    open
    :width="1160"
    :body-overflow="'hidden'"
    :close-label="t('common.buttons.close')"
    close-on-esc
    trap-focus
    @close="emit('close')"
  >
    <template #header>
      <div class="changes-heading">
        <Icon name="branch" :size="17" class="shrink-0 text-accent-2" />
        <span class="truncate text-sm font-semibold text-txt">{{ title }}</span>
        <span v-if="snapshot?.interrupted" class="shrink-0 rounded bg-warn/10 px-1.5 py-0.5 text-[11px] text-warn">{{ word('interrupted') }}</span>
      </div>
    </template>

    <div class="changes-dialog" data-testid="workspace-changes-dialog">
      <div v-if="snapshot || view?.canRefresh || workspaceOptions.length > 1" class="changes-overview">
        <div class="changes-context">
          <AppSelect
            v-if="workspaceOptions.length > 1"
            :model-value="currentWorkspace"
            :options="workspaceOptions"
            :aria-label="word('workspace')"
            :placeholder="word('chooseWorkspace')"
            :title="currentWorkspaceName"
            :menu-min-width="220"
            variant="ghost"
            size="sm"
            class="changes-workspace"
            @update:model-value="selectWorkspace"
          >
            <template #leading>
              <span class="changes-workspace-label" aria-hidden="true">
                <Icon name="layers" :size="14" :interactive="false" />
                {{ word('workspace') }}
              </span>
            </template>
            <template #trailing>
              <span v-if="workspaceLoading" role="status" class="inline-flex shrink-0 text-txt3">
                <AppSpinner :size="13" />
                <span class="sr-only">{{ t('common.loading') }}</span>
              </span>
            </template>
          </AppSelect>
          <div v-if="snapshot" class="changes-summary" data-testid="workspace-change-summary">
            <span v-if="available" class="changes-totals">
              <span class="font-semibold text-txt">{{ t('pages.workspaceChanges.fileCount', { n: totals.count }) }}</span>
              <span v-if="totals.count" class="text-ok">+{{ numerals.format(totals.added) }}</span>
              <span v-if="totals.count" class="text-err">−{{ numerals.format(totals.deleted) }}</span>
            </span>
            <span v-if="timestamp" class="changes-time" :title="timestamp">{{ view?.canRefresh ? timestamp : t('pages.workspaceChanges.archivedAt', { time: timestamp }) }}</span>
          </div>
        </div>
        <AppButton
          v-if="!turn && view?.canRefresh"
          variant="ghost"
          size="sm"
          icon="refresh"
          class="changes-refresh"
          :disabled="workspaceLoading"
          :loading="loading && !workspaceLoading"
          @click="emit('refresh')"
        >{{ t('common.buttons.refresh') }}</AppButton>
      </div>

      <div v-if="error" class="changes-banner" role="alert">
        <span>{{ word('loadError') }}</span>
        <AppButton size="sm" variant="ghost" @click="emit('retry')">{{ t('common.buttons.retry') }}</AppButton>
      </div>
      <div v-else-if="partial && available" class="changes-banner" role="status">{{ word('partial') }}</div>

      <div v-if="files.length" class="changes-layout" :class="{ 'changes-layout-file': compactFile }">
        <aside ref="fileList" class="changes-files scroll-area" :aria-label="word('files')" data-testid="workspace-change-file-list">
          <section v-for="repo in repositories" :key="repo.directory" class="changes-repo">
            <header class="changes-repo-header">
              <div class="changes-repo-info">
                <span class="changes-repo-name" :title="repo.name">{{ repo.name }}</span>
                <span v-if="repo.branch" class="changes-repo-ref" :title="repo.branch">{{ repo.branch }}</span>
              </div>
              <span v-if="repositories.length > 1 && repo.fileCount" class="changes-file-counts">
                <span class="text-ok">+{{ numerals.format(repo.added) }}</span>
                <span class="text-err">−{{ numerals.format(repo.deleted) }}</span>
              </span>
            </header>
            <p v-if="repo.unavailable" class="changes-repo-note">{{ word('unavailable') }}</p>
            <p v-else-if="!repo.fileCount" class="changes-repo-note">{{ word('empty') }}</p>
            <button
              v-for="file in (repo.files ?? [])"
              :key="file.id"
              type="button"
              class="changes-file"
              :aria-current="selected?.file.id === file.id ? 'true' : undefined"
              :title="file.path"
              data-testid="workspace-change-file-row"
              @click="selectFile(repo.directory + '/' + file.path)"
            >
              <span class="changes-file-status" :class="['changes-file-status-' + file.status]" :title="fileStatus(file.status)" :aria-label="fileStatus(file.status)">{{ file.status }}</span>
              <span class="changes-file-label">
                <span class="truncate text-txt">{{ file.path.split('/').at(-1) }}</span>
                <span v-if="file.path.includes('/')" class="truncate text-[10px] text-txt3">{{ file.path.slice(0, file.path.lastIndexOf('/')) }}</span>
              </span>
              <span v-if="file.added !== null" class="changes-file-counts">
                <span v-if="file.added" class="text-ok">+{{ numerals.format(file.added) }}</span>
                <span v-if="file.deleted" class="text-err">−{{ numerals.format(file.deleted) }}</span>
              </span>
            </button>
            <p v-if="repo.fileCount > (repo.files?.length ?? 0)" class="changes-repo-note">{{ word('listLimited') }}</p>
          </section>
        </aside>

        <section
          v-if="selected"
          ref="filePane"
          class="changes-file-pane"
          tabindex="-1"
          :aria-label="selected.file.path"
          data-testid="workspace-change-file-pane"
        >
          <div class="changes-file-title">
            <AppButton variant="ghost" size="sm" icon="chevron-left" class="changes-back" @click="backToFiles">{{ word('files') }}</AppButton>
            <div class="min-w-0 flex-1">
              <div class="truncate font-mono text-[12px] font-semibold text-txt" :title="selected.file.path">{{ selected.file.path }}</div>
              <div v-if="selected.file.previousPath" class="mt-1 truncate text-[11px] text-txt3" :title="selected.file.previousPath">{{ selected.file.previousPath }} → {{ selected.file.path }}</div>
              <div v-else-if="selected.repo.baseSha && !turn" class="mt-1 truncate text-[11px] text-txt3" :title="selected.repo.baseSha">
                {{ word('base') }} {{ selected.repo.baseBranch }} @ {{ selected.repo.baseSha.slice(0, 7) }}
              </div>
            </div>
          </div>
          <div class="changes-tabs" :aria-label="word('views')">
            <button type="button" :aria-pressed="mode === 'diff'" @click="mode = 'diff'">{{ word('diff') }}</button>
            <button v-if="markdown && (selected.file.hasAfter || selected.file.hasBefore)" type="button" :aria-pressed="mode === 'preview'" @click="mode = 'preview'">{{ word('preview') }}</button>
            <button v-if="selected.file.hasAfter || selected.file.hasBefore" type="button" :aria-pressed="mode === 'source'" @click="mode = 'source'">{{ word('source') }}</button>
            <span v-if="selected.file.status === 'D'" class="changes-deleted">{{ word('beforeDeletion') }}</span>
          </div>

          <div v-if="fileState === 'loading'" class="changes-empty" role="status">{{ t('common.loading') }}</div>
          <div v-else-if="fileState === 'error'" class="changes-empty" role="alert">
            <span>{{ word('fileError') }}</span>
            <AppButton size="sm" @click="fileRetry++">{{ t('common.buttons.retry') }}</AppButton>
          </div>
          <div v-else-if="mode === 'preview' && source !== undefined" class="changes-document scroll-area" data-testid="workspace-change-markdown">
            <div class="md changes-prose" v-html="markdownHTML" />
          </div>
          <div v-else-if="mode === 'source' && source !== undefined" class="min-h-0 flex-1" data-testid="workspace-change-source">
            <CodeEditor :model-value="source" :language="language" readonly class="h-full" />
          </div>
          <div v-else-if="mode === 'diff' && visibleLines.length" class="changes-diff scroll-area" data-testid="workspace-change-diff" tabindex="0">
            <div class="changes-diff-grid syntax-highlight">
              <div v-for="(line, index) in visibleLines" :key="index" class="changes-line" :class="'changes-line-' + line.kind">
                <span class="changes-line-number" aria-hidden="true">{{ line.before }}</span>
                <span class="changes-line-number" aria-hidden="true">{{ line.after }}</span>
                <span class="changes-line-mark" aria-hidden="true">{{ line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : '' }}</span>
                <span v-if="line.tokens" class="changes-line-text">
                  <span v-for="(token, tokenIndex) in line.tokens" :key="tokenIndex" :class="token.className">{{ token.text }}</span>
                </span>
                <span v-else class="changes-line-text">{{ line.text }}</span>
              </div>
              <p v-if="lines.length > lineLimit" class="changes-repo-note">{{ t('pages.workspaceChanges.linesLimited', { n: lineLimit }) }}</p>
            </div>
          </div>
          <div v-else class="changes-empty">
            {{ mode === 'diff' && selected.file.hasPatch ? word('metadataOnly') : selected.file.added === null ? word('binary') : word('previewUnavailable') }}
          </div>
        </section>
      </div>
      <div v-else class="changes-empty" role="status">
        <Icon v-if="!loading" name="branch" :size="22" class="text-txt3" />
        {{ loading ? t('common.loading') : available ? word('empty') : view?.status === 'pending' ? word('pending') : word('unavailable') }}
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.changes-heading { display: flex; align-items: center; gap: 0.55rem; min-width: 0; }
.changes-dialog { display: flex; flex-direction: column; height: min(67dvh, 680px); min-height: 0; }
.changes-overview { display: flex; align-items: flex-start; gap: 0.5rem; margin-bottom: 0.7rem; }
.changes-context { display: flex; flex: 1; flex-wrap: wrap; align-items: center; gap: 0.35rem 0.85rem; min-width: 0; }
.changes-summary { display: flex; flex-wrap: wrap; align-items: center; gap: 0.25rem 0.65rem; min-width: 0; min-height: 30px; font-size: 12px; font-variant-numeric: tabular-nums; }
.changes-totals { display: inline-flex; flex-wrap: wrap; gap: 0.45rem; }
.changes-time { color: rgb(var(--c-txt3)); font-size: 11px; }
.changes-refresh { min-height: 30px; flex: none; }
.changes-workspace { max-width: min(17rem, 100%); }
.changes-workspace :deep(.app-select-trigger) { border-radius: 8px; color: rgb(var(--c-txt)); font-weight: 600; }
.changes-workspace :deep(.app-select-trigger:not(:hover, :focus-visible, [aria-expanded="true"])) { background: rgb(var(--c-base)); border-color: rgb(var(--c-line)); }
.changes-workspace-label { display: inline-flex; flex: none; align-items: center; gap: 0.3rem; padding-inline-end: 0.45rem; border-inline-end: 1px solid rgb(var(--c-line)); color: rgb(var(--c-txt2)); font-weight: 400; white-space: nowrap; }
.changes-banner { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.45rem 0.65rem; margin-bottom: 0.55rem; border-radius: 7px; background: rgb(var(--c-warn) / 0.09); color: rgb(var(--c-txt2)); font-size: 12px; }
.changes-layout { min-height: 0; flex: 1; display: grid; grid-template-columns: minmax(165px, 245px) minmax(0, 1fr); border: 1px solid rgb(var(--c-line)); border-radius: 10px; overflow: hidden; }
.changes-files { overflow: auto; background: rgb(var(--c-base)); border-inline-end: 1px solid rgb(var(--c-line)); }
.changes-repo + .changes-repo { margin-top: 0.4rem; border-top: 1px solid rgb(var(--c-line)); }
.changes-repo-header { position: sticky; top: 0; z-index: 1; display: flex; align-items: center; gap: 0.45rem; padding: 0.55rem 0.75rem; background: rgb(var(--c-base)); border-bottom: 1px solid rgb(var(--c-line) / 0.65); }
.changes-repo-info { display: flex; flex: 1; flex-direction: column; gap: 0.1rem; min-width: 0; }
.changes-repo-name { max-width: 100%; overflow: hidden; text-overflow: ellipsis; color: rgb(var(--c-txt)); font-size: 12px; font-weight: 650; white-space: nowrap; }
.changes-repo-ref { max-width: 100%; overflow: hidden; text-overflow: ellipsis; color: rgb(var(--c-txt3)); font-size: 11px; white-space: nowrap; }
.changes-file { display: flex; align-items: center; width: 100%; min-height: 38px; gap: 0.45rem; padding: 0.3rem 0.6rem; border-inline-start: 2px solid transparent; text-align: left; }
.changes-file:hover { background: rgb(var(--c-elevated)); }
.changes-file[aria-current="true"] { background: rgb(var(--c-accent) / 0.09); border-inline-start-color: rgb(var(--c-accent)); }
.changes-file:focus-visible { outline: 2px solid rgb(var(--c-accent)); outline-offset: -2px; }
.changes-file-status { width: 1.2rem; flex: none; text-align: center; font: 600 11px ui-monospace, monospace; color: rgb(var(--c-txt3)); }
.changes-file-status-A { color: rgb(var(--c-ok)); }
.changes-file-status-D { color: rgb(var(--c-err)); }
.changes-file-status-R { color: rgb(var(--c-info)); }
.changes-file-label { display: flex; flex-direction: column; flex: 1; min-width: 0; font-size: 12px; }
.changes-file-counts { display: flex; flex: none; gap: 0.3rem; font-size: 10px; font-variant-numeric: tabular-nums; }
.changes-repo-note { padding: 0.45rem 0.75rem; color: rgb(var(--c-txt3)); font-size: 11px; }
.changes-file-pane { display: flex; flex-direction: column; min-width: 0; min-height: 0; outline: none; }
.changes-file-title { display: flex; align-items: center; gap: 0.35rem; min-height: 40px; padding: 0.6rem 0.85rem; }
.changes-back { display: none; }
.changes-tabs { display: flex; align-items: center; flex: none; min-height: 33px; gap: 0.5rem; padding-inline: 0.75rem; border-bottom: 1px solid rgb(var(--c-line)); }
.changes-tabs button { align-self: stretch; padding: 0.3rem 0.35rem; border-bottom: 2px solid transparent; color: rgb(var(--c-txt3)); font-size: 12px; white-space: nowrap; }
.changes-tabs button[aria-pressed="true"] { color: rgb(var(--c-accent-2)); border-color: rgb(var(--c-accent)); font-weight: 650; }
.changes-tabs button:focus-visible { outline: 2px solid rgb(var(--c-accent)); outline-offset: -2px; }
.changes-deleted { margin-inline-start: auto; color: rgb(var(--c-txt3)); font-size: 10px; text-align: right; }
.changes-document { min-height: 0; flex: 1; overflow: auto; padding: clamp(12px, 3vw, 30px); overflow-wrap: anywhere; }
.changes-prose { max-width: 760px; margin-inline: auto; }
.changes-diff { flex: 1; min-height: 0; overflow: auto; background: rgb(var(--c-base)); }
.changes-diff-grid { min-width: max-content; padding-block: 0.4rem; }
.changes-line { display: grid; grid-template-columns: 3.15rem 3.15rem 1.25rem 1fr; min-height: 21px; font: 12px/21px ui-monospace, monospace; color: rgb(var(--c-txt2)); }
.changes-line-add { background: rgb(var(--c-ok) / 0.115); color: rgb(var(--c-txt)); }
.changes-line-remove { background: rgb(var(--c-err) / 0.115); color: rgb(var(--c-txt)); }
.changes-line-hunk { margin-block: 0.2rem; background: rgb(var(--c-accent) / 0.06); color: rgb(var(--c-accent-2)); }
.changes-line-note { color: rgb(var(--c-txt3)); }
.changes-line-number { text-align: end; padding-inline-end: 0.5rem; color: rgb(var(--c-txt3)); user-select: none; }
.changes-line-mark { text-align: center; user-select: none; }
.changes-line-add .changes-line-mark { color: rgb(var(--c-ok)); }
.changes-line-remove .changes-line-mark { color: rgb(var(--c-err)); }
.changes-line-text { white-space: pre; tab-size: 4; padding-inline-end: 0.7rem; }
.changes-empty { display: flex; flex: 1; flex-direction: column; align-items: center; justify-content: center; gap: 0.65rem; padding: 0.8rem; text-align: center; color: rgb(var(--c-txt3)); font-size: 13px; }

@media (max-width: 700px) {
  .changes-dialog { height: min(69dvh, 590px); }
  .changes-workspace :deep(.app-select-trigger), .changes-refresh { min-height: 36px; }
  .changes-layout { grid-template-columns: minmax(0, 1fr); }
  .changes-files { border-inline-end: 0; }
  .changes-file-pane { display: none; }
  .changes-layout-file .changes-files { display: none; }
  .changes-layout-file .changes-file-pane { display: flex; }
  .changes-back { display: inline-flex; padding-inline-start: 0; }
  .changes-file { min-height: 44px; }
  .changes-line { grid-template-columns: 2.1rem 2.1rem 1rem 1fr; font-size: 11px; }
  .changes-line-number { padding-inline-end: 0.3rem; }
}
</style>

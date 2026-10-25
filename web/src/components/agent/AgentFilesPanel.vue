<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, toRefs, watch } from 'vue'
import Icon from '@/components/ui/Icon.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import CodeEditor from '@/components/ui/CodeEditor.vue'
import MarkdownSplitEditor from '@/components/agent/MarkdownSplitEditor.vue'
import MarkdownViewToggle from '@/components/agent/MarkdownViewToggle.vue'
import ExplorerContextMenu, { type CtxTarget } from '@/components/agent/ExplorerContextMenu.vue'
import AgentWorkspaceHistoryPanel from '@/components/agent/AgentWorkspaceHistoryPanel.vue'
export type { FilesStep, FilesOpenSnapshot } from '@/lib/agent/useAgentFilesPanel'
import { useAgentFilesPanel } from '@/lib/agent/useAgentFilesPanel'
import type { AgentFilesPanelProps, AgentFilesPanelEmit } from '@/lib/agent/useAgentFilesPanel'

const props = defineProps<AgentFilesPanelProps>()
const emit = defineEmits<AgentFilesPanelEmit>()

const panelEl = ref<HTMLElement | null>(null)
const panelWidth = ref(0)
const compactFiles = computed(() => props.isMobile || (panelWidth.value > 0 && panelWidth.value < 640))
// Version history opens as a dialog on desktop so the workspace stays explorer + editor.
const modalHistory = computed(() => !props.isMobile)
const showHistory = ref(false)
const mdEditorRef = ref<InstanceType<typeof MarkdownSplitEditor> | null>(null)
// Reuse the established list/edit flow when the surrounding directories leave
// too little room for both a file explorer and an editor.
const responsiveProps = reactive({ ...toRefs(props), isMobile: compactFiles })
let resizeObserver: ResizeObserver | undefined
function measurePanel() {
  const width = panelEl.value?.getBoundingClientRect().width ?? 0
  if (width > 0) panelWidth.value = width
}
onMounted(() => {
  measurePanel()
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(measurePanel)
    if (panelEl.value) resizeObserver.observe(panelEl.value)
  }
  window.addEventListener('resize', measurePanel)
})
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  window.removeEventListener('resize', measurePanel)
})
watch([() => props.agentName, modalHistory], () => { showHistory.value = false })

const {
  t,
  EXPLORER_COLLAPSED_KEY,
  SIDEBAR_EXPANDED_W,
  SIDEBAR_COLLAPSED_W,
  readCollapsedState,
  writeCollapsedState,
  explorerCollapsed,
  toggleExplorerCollapsed,
  collapseBtnClass,
  filesStep,
  activeFile,
  openTabs,
  expanded,
  emptyDirs,
  renamingPath,
  renameInput,
  creating,
  createInput,
  folderInput,
  uploadTargetDir,
  selectedTreeRow,
  ctxMenu,
  explorerMore,
  explorerMoreStyle,
  explorerMoreAnchor,
  confirmCfg,
  leaveConfirmCfg,
  currentFile,
  activePath,
  langForPath,
  isMdPath,
  hideCtxMenu,
  openCtxMenu,
  onExplorerBlankCtx,
  onCtxAction,
  onExplorerKeydown,
  joinPath,
  expandParents,
  tree,
  rows,
  openFile,
  openPath,
  openPathOrCreate,
  selectDefaultFile,
  resetForSelect,
  snapshot,
  restoreAfterDiscard,
  goFilesList,
  tryBackToList,
  leaveConfirmSave,
  leaveConfirmDiscard,
  leaveConfirmCancel,
  closeTab,
  startRename,
  cancelRename,
  commitRename,
  toggleDir,
  newFile,
  newFolder,
  startCreate,
  cancelCreate,
  commitCreate,
  isProtectedDir,
  deleteEntry,
  confirmOk,
  onFolderPick,
  closeExplorerMore,
  explorerMoreItemCount,
  placeExplorerMore,
  toggleExplorerMore,
  onExplorerMoreAction,
  onDocumentClick,
  onChromeReposition,
  onChromeKeydown,
} = useAgentFilesPanel(responsiveProps, emit)

const filesGridStyle = computed(() => {
  if (compactFiles.value) return undefined
  return { gridTemplateColumns: `${explorerCollapsed.value ? SIDEBAR_COLLAPSED_W : SIDEBAR_EXPANDED_W} minmax(0, 1fr)` }
})

defineExpose({
  filesStep, resetForSelect, snapshot, restoreAfterDiscard, closeExplorerMore, selectDefaultFile,
  openPath, openPathOrCreate,
})
</script>

<template>
  <div ref="panelEl" class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden" data-testid="agent-files-panel">
  <!-- compact (<640px) keeps its own row; wider desktop puts 版本历史 in the editor toolbar -->
  <div v-if="compactFiles && modalHistory && agentName" class="flex shrink-0 justify-end border-b border-line/60 px-3 py-1">
    <button
      type="button"
      data-testid="compact-history-open"
      class="min-h-9 text-[12px] text-txt2 hover:text-txt"
      @click="showHistory = true"
    >{{ t('pages.agentStudio.workspaceHistory.title') }}</button>
  </div>
  <div
    class="min-h-0 flex-1 overflow-hidden transition-[grid-template-columns] duration-[var(--dur-ui)] ease-[var(--ease-out-expo)]"
    :class="compactFiles ? 'flex flex-col' : 'grid'"
    :style="filesGridStyle"
  >
        <!-- explorer (list step on mobile) -->
        <div
          v-if="!compactFiles || filesStep === 'list'"
          class="flex min-h-0 min-w-0 flex-col"
          :class="compactFiles ? 'flex-1' : 'border-r border-line'"
        >
          <div
            class="flex shrink-0 items-center gap-0.5 border-b border-line px-2 py-1.5"
            :class="[
              compactFiles ? 'min-h-11' : 'min-h-8',
              explorerCollapsed && !compactFiles ? 'justify-center px-[3px]' : '',
            ]"
          >
            <span
              v-if="compactFiles"
              class="flex-1 truncate px-1 text-[10.5px] font-semibold uppercase tracking-wider text-txt3"
            >{{ t('pages.agentStudio.explorer.title') }}</span>
            <span v-else-if="!explorerCollapsed" class="flex-1" />
            <template v-if="!explorerCollapsed || compactFiles">
              <button
                class="rounded p-1 text-txt3 hover:bg-elevated hover:text-accent-2 focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)] outline-none"
                :class="compactFiles ? 'min-h-11 min-w-11' : ''"
                :title="t('pages.agentStudio.explorer.newFile')"
                @click="newFile('')"
              ><Icon name="doc" :size="13" /></button>
              <button
                class="rounded p-1 text-txt3 hover:bg-elevated hover:text-accent-2 focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)] outline-none"
                :class="compactFiles ? 'min-h-11 min-w-11' : ''"
                :title="t('pages.agentStudio.explorer.newFolder')"
                @click="newFolder('')"
              ><Icon name="folder" :size="13" /></button>
              <input ref="folderInput" type="file" webkitdirectory directory multiple class="hidden" @change="onFolderPick" />
              <button
                v-if="!compactFiles"
                type="button"
                :class="collapseBtnClass"
                :title="t('pages.agentStudio.explorer.collapse')"
                :aria-label="t('pages.agentStudio.explorer.collapse')"
                @click="toggleExplorerCollapsed"
              >
                <Icon :interactive="false" name="chevron-right" :size="14" class="rotate-180" />
              </button>
            </template>
            <button
              v-if="explorerCollapsed && !compactFiles"
              type="button"
              :class="collapseBtnClass"
              :title="t('pages.agentStudio.explorer.expand')"
              :aria-label="t('pages.agentStudio.explorer.expand')"
              @click="toggleExplorerCollapsed"
            >
              <Icon name="chevron-right" :size="14" />
            </button>
          </div>
          <div
            v-if="!explorerCollapsed || compactFiles"
            class="scroll-area min-h-0 flex-1 overflow-y-auto py-1"
            @contextmenu="onExplorerBlankCtx"
          >
            <div v-if="!rows.length && !creating" class="px-3 py-6 text-center text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.explorer.empty') }}</div>

            <!-- inline new entry at root (VSCode-style) -->
            <div v-if="creating && creating.dir === ''" class="flex w-full items-center gap-1 py-[3px] pr-1.5">
              <span class="flex shrink-0" :style="{ width: '8px' }" />
              <Icon :name="creating.kind === 'folder' ? 'folder' : 'doc'" :size="13" class="shrink-0" :class="creating.kind === 'folder' ? 'text-accent-2' : 'text-txt3'" />
              <input
                data-create
                v-model="createInput"
                class="min-w-0 flex-1 rounded border border-accent bg-surface px-1 py-0 font-mono text-[12px] text-txt outline-none"
                :placeholder="creating.kind === 'folder' ? t('pages.agentStudio.explorer.folderPlaceholder') : t('pages.agentStudio.explorer.filePlaceholder')"
                @keyup.enter="commitCreate"
                @keyup.esc="cancelCreate"
                @blur="commitCreate"
                @click.stop
              />
            </div>

            <template v-for="row in rows" :key="(row.dir ? 'd:' : 'f:') + row.path">
            <div
              class="group relative flex w-full items-center gap-1 pr-1.5 text-left text-[12px] transition"
              :class="[
                compactFiles ? 'min-h-11 py-2' : 'py-[3px]',
                !row.dir && activePath === row.path ? 'bg-accent-dim text-txt' : 'text-txt2 hover:bg-elevated',
                ctxMenu.target?.path === row.path ? 'bg-overlay outline outline-1 outline-accent/35' : '',
              ]"
              @dblclick="startRename(row)"
              @contextmenu="openCtxMenu($event, { dir: row.dir, path: row.path, name: row.name })"
            >
              <span
                v-if="!row.dir && activePath === row.path"
                class="absolute inset-y-0 left-0 w-0.5 bg-accent"
              />
              <!-- indent guides -->
              <span class="flex shrink-0" :style="{ width: 8 + row.depth * 12 + 'px' }">
                <span v-for="d in row.depth" :key="d" class="ml-[5px] w-[7px] border-l border-line/60" />
              </span>

              <template v-if="renamingPath === row.path">
                <Icon :name="row.dir ? 'folder' : 'doc'" :size="13" class="shrink-0" :class="row.dir ? 'text-accent-2' : 'text-txt3'" />
                <input
                  data-rename
                  v-model="renameInput"
                  class="min-w-0 flex-1 rounded border border-accent bg-surface px-1 py-0 font-mono text-[12px] text-txt outline-none"
                  @keyup.enter="commitRename(row)"
                  @keyup.esc="cancelRename"
                  @blur="commitRename(row)"
                  @click.stop
                />
              </template>

              <template v-else>
                <button v-if="row.dir" class="flex min-w-0 flex-1 items-center gap-1" @click="toggleDir(row.path)">
                  <Icon :name="expanded.has(row.path) ? 'chevron-down' : 'chevron-right'" :size="12" class="shrink-0 text-txt3" />
                  <Icon name="folder" :size="13" class="shrink-0 text-accent-2" />
                  <span class="truncate font-mono">{{ row.name }}</span>
                </button>
                <button v-else class="flex min-w-0 flex-1 items-center gap-1 pl-[15px]" @click="openPath(row.path); selectedTreeRow = row">
                  <Icon name="doc" :size="13" class="shrink-0 text-txt3" />
                  <span class="truncate font-mono">{{ row.name }}</span>
                </button>
                <button
                  v-if="compactFiles"
                  type="button"
                  data-test="file-row-more"
                  :data-path="row.path"
                  class="flex min-h-11 min-w-11 shrink-0 items-center justify-center text-txt3 hover:text-accent-2"
                  :title="t('pages.agentStudio.explorer.more')"
                  :aria-label="t('pages.agentStudio.explorer.more')"
                  :aria-expanded="explorerMore?.path === row.path"
                  @click.stop="toggleExplorerMore($event, row)"
                ><Icon name="more" :size="16" /></button>
                <template v-else>
                  <button
                    v-if="row.dir"
                    data-test="file-row-action"
                    class="shrink-0 text-txt3 opacity-0 hover:text-accent-2 group-hover:opacity-100"
                    :title="t('pages.agentStudio.explorer.newFileInFolder')"
                    @click.stop="newFile(row.path)"
                  ><Icon name="doc" :size="12" /></button>
                  <button
                    v-if="row.dir"
                    data-test="file-row-action"
                    class="shrink-0 text-txt3 opacity-0 hover:text-accent-2 group-hover:opacity-100"
                    :title="t('pages.agentStudio.explorer.newFolderInFolder')"
                    @click.stop="newFolder(row.path)"
                  ><Icon name="folder" :size="12" /></button>
                  <button
                    data-test="file-row-action"
                    class="shrink-0 text-txt3 opacity-0 hover:text-accent-2 group-hover:opacity-100"
                    :title="t('pages.agentStudio.explorer.rename')"
                    @click.stop="startRename(row)"
                  ><Icon name="edit" :size="12" /></button>
                  <button
                    v-if="!row.dir || !isProtectedDir(row.path)"
                    data-test="file-row-action"
                    class="shrink-0 text-txt3 opacity-0 hover:text-err group-hover:opacity-100"
                    :title="t('pages.agentStudio.explorer.delete')"
                    @click.stop="deleteEntry(row)"
                  ><Icon name="close" :size="12" /></button>
                </template>
              </template>
            </div>

            <!-- inline new entry under this folder (VSCode-style) -->
            <div v-if="creating && creating.dir === row.path" class="flex w-full items-center gap-1 py-[3px] pr-1.5">
              <span class="flex shrink-0" :style="{ width: 8 + (row.depth + 1) * 12 + 'px' }">
                <span v-for="d in (row.depth + 1)" :key="d" class="ml-[5px] w-[7px] border-l border-line/60" />
              </span>
              <Icon :name="creating.kind === 'folder' ? 'folder' : 'doc'" :size="13" class="shrink-0" :class="creating.kind === 'folder' ? 'text-accent-2' : 'text-txt3'" />
              <input
                data-create
                v-model="createInput"
                class="min-w-0 flex-1 rounded border border-accent bg-surface px-1 py-0 font-mono text-[12px] text-txt outline-none"
                :placeholder="creating.kind === 'folder' ? t('pages.agentStudio.explorer.folderName') : t('pages.agentStudio.explorer.fileName')"
                @keyup.enter="commitCreate"
                @keyup.esc="cancelCreate"
                @blur="commitCreate"
                @click.stop
              />
            </div>
            </template>
          </div>
        </div>

        <!-- editor pane (edit step on mobile) -->
        <div
          v-if="!compactFiles || filesStep === 'edit'"
          class="flex min-h-0 min-w-0 flex-col overflow-hidden"
          :class="compactFiles ? 'flex-1' : ''"
        >
          <!-- mobile edit chrome: back + path -->
          <div
            v-if="compactFiles"
            class="flex shrink-0 items-center gap-2 border-b border-line/60 px-2.5 py-2 min-h-12"
          >
            <button
              type="button"
              class="inline-flex min-h-11 items-center gap-1 px-2 text-[12px] text-txt2 hover:text-txt"
              @click="tryBackToList"
            >
              <Icon :interactive="false" name="chevron-right" :size="14" class="rotate-180" />
              {{ t('pages.agentStudio.mobile.back') }}
            </button>
            <span class="min-w-0 flex-1 truncate font-mono text-[12px] text-txt">{{ activePath }}</span>
          </div>

          <!-- editor toolbar (desktop): open-file tabs + 编辑/分栏/预览 + 版本历史 in ONE row -->
          <div
            v-if="!compactFiles"
            data-testid="files-editor-toolbar"
            class="flex min-h-8 shrink-0 items-stretch border-b border-line"
          >
            <div class="scroll-area flex min-w-0 flex-1 items-stretch overflow-x-auto">
              <div
                v-for="tabFile in openTabs"
                :key="tabFile.path"
                class="group flex shrink-0 cursor-pointer items-center gap-1.5 border-r border-line px-3 py-1.5 text-[12px] transition"
                :class="activeFile === tabFile ? 'bg-surface text-txt' : 'text-txt3 hover:bg-elevated'"
                :title="tabFile.path"
                @click="activeFile = tabFile"
              >
                <Icon name="doc" :size="12" class="shrink-0" />
                <span class="max-w-[160px] truncate font-mono">{{ tabFile.path.split('/').pop() }}</span>
                <button class="shrink-0 rounded text-txt3 opacity-0 hover:bg-overlay hover:text-txt group-hover:opacity-100" :class="activeFile === tabFile ? 'opacity-60' : ''" :title="t('pages.agentStudio.explorer.close')" @click.stop="closeTab(tabFile)"><Icon name="close" :size="12" /></button>
              </div>
            </div>
            <div class="flex shrink-0 items-center gap-2 px-2">
              <MarkdownViewToggle
                v-if="currentFile && isMdPath(currentFile.path) && mdEditorRef"
                :model-value="mdEditorRef.viewMode"
                @update:model-value="mdEditorRef.setViewMode"
              />
              <span
                v-if="currentFile && isMdPath(currentFile.path) && mdEditorRef && modalHistory && agentName"
                class="h-3.5 w-px bg-line"
                aria-hidden="true"
              />
              <button
                v-if="modalHistory && agentName"
                type="button"
                data-testid="compact-history-open"
                class="flex items-center gap-1 rounded px-1.5 py-1 text-[12px] text-txt3 transition hover:bg-elevated hover:text-txt"
                :title="t('pages.agentStudio.workspaceHistory.title')"
                @click="showHistory = true"
              ><Icon name="history" :size="13" />{{ t('pages.agentStudio.workspaceHistory.title') }}</button>
            </div>
          </div>

          <template v-if="currentFile">
            <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
              <MarkdownSplitEditor
                v-if="isMdPath(currentFile.path)"
                ref="mdEditorRef"
                :external-toolbar="!compactFiles"
                v-model="currentFile.content"
                :file-path="currentFile.path"
                :variant="compactFiles ? 'stack' : 'split'"
              />
              <CodeEditor v-else v-model="currentFile.content" :language="langForPath(currentFile.path)" />
            </div>
            <!-- status bar -->
            <div
              class="flex shrink-0 items-center gap-3 border-t border-line/60 px-3 py-1 text-[10.5px] text-txt3"
            >
              <span class="uppercase">{{ langForPath(currentFile.path) }}</span>
              <span>{{ t('common.format.lines', { n: currentFile.content.split('\n').length }) }}</span>
              <span>{{ t('common.format.chars', { n: currentFile.content.length }) }}</span>
              <span
                v-if="!compactFiles && isMdPath(currentFile.path)"
                class="rounded-md border border-line bg-elevated px-1.5 text-[10px] text-txt2"
              >{{ t('pages.agentStudio.explorer.markdownBadge') }}</span>
              <span class="ml-auto truncate font-mono">{{ currentFile.path }}</span>
            </div>
          </template>
          <div v-else class="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-txt3">
            <Icon name="doc" :size="28" class="text-line-strong" />
            {{ t('pages.agentStudio.explorer.selectOrCreate') }}
          </div>
        </div>

  </div>

    <AppModal
      :open="showHistory && modalHistory"
      :title="t('pages.agentStudio.workspaceHistory.title')"
      :width="640"
      body-overflow="hidden"
      @close="showHistory = false"
    >
      <div v-if="showHistory && agentName" class="flex h-[min(60dvh,520px)] min-h-0 flex-col">
        <AgentWorkspaceHistoryPanel
          class="flex-1 border-l-0"
          :agent-name="agentName"
          :is-mobile="false"
          :file-path="activePath"
          :collapsed="false"
          :refresh-key="historyRefreshKey"
          @toggle-collapse="showHistory = false"
          @restored="emit('restored')"
        />
      </div>
    </AppModal>

    <AppModal
      :open="!!leaveConfirmCfg"
      :title="leaveConfirmCfg?.title || ''"
      :width="420"
      @close="leaveConfirmCancel"
    >
      <p class="text-[13px] leading-6 text-txt2">{{ leaveConfirmCfg?.message }}</p>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="leaveConfirmCancel">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="outline" @click="leaveConfirmDiscard">{{ leaveConfirmCfg?.discardText }}</AppButton>
        <AppButton size="sm" variant="primary" @click="leaveConfirmSave">{{ leaveConfirmCfg?.saveText }}</AppButton>
      </template>
    </AppModal>

    <AppModal
      :open="!!confirmCfg"
      :title="confirmCfg?.title || ''"
      :width="420"
      @close="confirmCfg = null"
    >
      <p class="text-[13px] leading-6 text-txt2">{{ confirmCfg?.message }}</p>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="confirmCfg = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" :variant="confirmCfg?.danger ? 'danger' : 'primary'" @click="confirmOk">{{ confirmCfg?.confirmText || t('common.buttons.confirm') }}</AppButton>
      </template>
    </AppModal>

    <Teleport to="body">
      <div
        v-if="compactFiles && explorerMore"
        data-test="explorer-more-backdrop"
        class="fixed inset-0 z-[9998]"
        @click="closeExplorerMore"
      />
      <div
        v-if="compactFiles && explorerMore"
        data-test="explorer-more-menu"
        class="rounded-lg fixed z-[9999] min-w-[180px] border border-line bg-elevated py-1 shadow-card"
        :style="explorerMoreStyle"
        @click.stop
      >
        <button
          v-if="explorerMore.dir"
          type="button"
          data-test="explorer-more-item"
          data-action="newFile"
          class="flex min-h-11 w-full items-center px-3.5 text-left text-[13px] text-txt hover:bg-overlay"
          @click="onExplorerMoreAction('newFile')"
        >{{ t('pages.agentStudio.explorer.newFile') }}</button>
        <button
          v-if="explorerMore.dir"
          type="button"
          data-test="explorer-more-item"
          data-action="newFolder"
          class="flex min-h-11 w-full items-center px-3.5 text-left text-[13px] text-txt hover:bg-overlay"
          @click="onExplorerMoreAction('newFolder')"
        >{{ t('pages.agentStudio.explorer.newFolder') }}</button>
        <button
          type="button"
          data-test="explorer-more-item"
          data-action="rename"
          class="flex min-h-11 w-full items-center px-3.5 text-left text-[13px] text-txt hover:bg-overlay"
          @click="onExplorerMoreAction('rename')"
        >{{ t('pages.agentStudio.explorer.rename') }}</button>
        <button
          v-if="!explorerMore.dir || !isProtectedDir(explorerMore.path)"
          type="button"
          data-test="explorer-more-item"
          data-action="delete"
          class="flex min-h-11 w-full items-center px-3.5 text-left text-[13px] text-err hover:bg-err/10"
          @click="onExplorerMoreAction('delete')"
        >{{ t('pages.agentStudio.explorer.delete') }}</button>
      </div>
    </Teleport>

    <ExplorerContextMenu
      :open="ctxMenu.open"
      :x="ctxMenu.x"
      :y="ctxMenu.y"
      :target="ctxMenu.target"
      @close="hideCtxMenu"
      @action="onCtxAction"
    />
  </div>
</template>

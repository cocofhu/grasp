<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Workflow } from '@/lib/shared/types'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import RunLaunchModal from '@/components/workflow/RunLaunchModal.vue'
import ReposEditor from '@/components/ui/ReposEditor.vue'
import NewWorkflowMenu from '@/components/workflow/NewWorkflowMenu.vue'
import CopyWorkflowModal from '@/components/workflow/CopyWorkflowModal.vue'
import ExportVersionModal from '@/components/workflow/ExportVersionModal.vue'
import WorkspacePage from '@/components/shell/WorkspacePage.vue'
import { useWorkflows } from '@/lib/project/useWorkflows'
const {
  workflows,
  search,
  visibleWorkflows,
  loading,
  loadError,
  toggleWorkflowFavorite,
  runTarget,
  runFields,
  runInputs,
  runImages,
  draftRestored,
  openMenuId,
  menuPosition,
  newWorkflowMenuOpen,
  baselineModalOpen,
  baselineName,
  baselineRepos,
  creatingBaseline,
  baselineCreateError,
  deleteWfTarget,
  deletingWf,
  deleteWfError,
  copyPreviewLoading,
  copyModal,
  exportTarget,
  savingNotifyWfId,
  wfNotifyMode,
  wfNotifyHas,
  setWorkflowNotifyMode,
  toggleWorkflowNotifyEvent,
  savingHomeWfId,
  toggleWorkflowShowOnHome,
  existingNames,
  toggleMenu,
  toggleNewWorkflowMenu,
  menuIdFor,
  reloadWorkflows,
  newWorkflow,
  openBaselineModal,
  closeBaselineModal,
  hasValidBaselineRepo,
  createFromBaseline,
  openWorkflow,
  openEdit,
  openRun,
  saveRunDraftClick,
  closeRunModal,
  onRunStayed,
  onRunStarted,
  onViewRun,
  openCopy,
  closeCopyModal,
  onCopied,
  openExport,
  openDeleteWf,
  confirmDeleteWf,
  t,
  isFavorite,
  isMobile,
  fileInput,
  triggerImport,
  handleFileChange,
  fmtTime
} = useWorkflows()
const expandedNotifyId = ref<string | null>(null)
const notifyModes = computed(() => [
  { value: 'off', label: t('pages.projectDetail.notify.modeOff') },
  { value: 'inherit', label: t('pages.projectDetail.notify.modeInherit') },
  { value: 'custom', label: t('pages.projectDetail.notify.modeCustom') },
])
const notifyModesWithDefaults = computed(() => notifyModes.value.map(mode => mode.value === 'inherit' ? { ...mode, label: t('pages.workflowList.notifyInheritDefaults') } : mode))
function notifyModesFor(workflow: Workflow) {
  return workflow.defaults?.notifyPolicy ? notifyModesWithDefaults.value : notifyModes.value
}
const notifyEvents = [
  { value: 'waiting_human', labelKey: 'pages.projectDetail.notify.segWaitingHuman' },
  { value: 'failed', labelKey: 'pages.projectDetail.notify.segFailed' },
  { value: 'completed', labelKey: 'pages.projectDetail.notify.segCompleted' },
] as const

function onNotifyModeChange(workflow: Workflow, mode: string) {
  if (mode !== 'off' && mode !== 'inherit' && mode !== 'custom') return
  expandedNotifyId.value = mode === 'custom' ? workflow.id : null
  if (mode === wfNotifyMode(workflow)) return
  void setWorkflowNotifyMode(workflow, mode)
}
</script>

<template>
<WorkspacePage title-key="nav.workflows" description-key="nav.workflowsHint">
<div class="flex min-h-0 min-w-0 flex-1 flex-col" data-testid="workflows-page">
  <div class="workflow-toolbar">
    <label class="workflow-search">
      <Icon name="search" :size="15" />
      <input v-model="search" type="search" :aria-label="t('nav.searchWorkflows')" :placeholder="t('nav.searchWorkflows')" />
    </label>
    <div class="workflow-toolbar-actions">
      <AppButton variant="ghost" icon="input" @click="triggerImport">{{ t('common.buttons.import') }}</AppButton>
      <div class="relative" data-new-workflow-menu @click.stop>
        <AppButton variant="primary" icon="plus" data-testid="new-workflow-button" :aria-expanded="newWorkflowMenuOpen" aria-haspopup="menu" @click="toggleNewWorkflowMenu">
          {{ t('common.buttons.newWorkflow') }}<Icon name="chevron-down" :size="14" />
        </AppButton>
        <NewWorkflowMenu :open="newWorkflowMenuOpen" @scratch="newWorkflow" @baseline="openBaselineModal" />
      </div>
    </div>
  </div>

  <div v-if="loadError" class="mb-3 flex items-center justify-between gap-3 border-l-2 border-err/50 bg-err/5 px-3 py-2 text-sm" role="alert">
    <span>{{ loadError }}</span><AppButton variant="ghost" @click="reloadWorkflows">{{ t('nav.workspaceRetry') }}</AppButton>
  </div>
  <div v-if="loading && !workflows.length" class="py-3" role="status">
    <span class="sr-only">{{ t('nav.workspaceLoading') }}</span>
    <div v-for="index in 5" :key="index" class="flex h-16 items-center gap-8 border-b border-line/60" aria-hidden="true">
      <div class="h-3 w-2/5 animate-pulse rounded bg-elevated" /><div class="ml-auto h-3 w-20 animate-pulse rounded bg-elevated" />
    </div>
  </div>
  <div v-else-if="!loadError || workflows.length" class="workflow-directory-viewport scroll-area min-h-0 min-w-0 flex-1 overflow-y-auto" :aria-busy="loading">
    <div v-if="!visibleWorkflows.length" class="workflow-empty" data-testid="workflows-empty" :data-empty-kind="workflows.length ? 'search' : 'workspace'">
      <Icon :name="workflows.length ? 'search' : 'workflow'" :size="26" class="text-txt3" />
      <h2 class="text-base font-medium text-txt">{{ t(workflows.length ? 'nav.noMatchingWorkflows' : 'common.empty.noWorkflows') }}</h2>
      <p class="max-w-sm text-[13px] leading-6 text-txt3">{{ t(workflows.length ? 'pages.workflowList.noMatchesHint' : 'pages.workflowList.emptyHint') }}</p>
      <AppButton v-if="workflows.length" variant="outline" @click="search = ''">{{ t('pages.workflowList.clearSearch') }}</AppButton>
      <div v-else class="mt-1 flex items-center gap-2">
        <AppButton variant="primary" icon="plus" @click="newWorkflow">{{ t('common.buttons.newWorkflow') }}</AppButton>
        <AppButton variant="ghost" icon="input" @click="triggerImport">{{ t('common.buttons.import') }}</AppButton>
      </div>
    </div>
    <table v-else class="workflow-directory" data-testid="workflows-desktop-table">
      <colgroup><col /><col class="workflow-col-status" /><col class="workflow-col-notify" /><col class="workflow-col-home" /><col class="workflow-col-updated" /><col class="workflow-col-actions" /></colgroup>
      <thead>
        <tr>
          <th>{{ t('pages.projectDetail.colName') }}</th>
          <th>{{ t('pages.projectDetail.colStatus') }}</th>
          <th>{{ t('pages.projectDetail.notify.colPolicy') }}</th>
          <th class="text-center">{{ t('pages.projectDetail.homeVisibility.col') }}</th>
          <th>{{ t('pages.projectDetail.colUpdated') }}</th>
          <th class="text-right">{{ t('common.table.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="w in visibleWorkflows" :key="w.id" class="workflow-row" data-testid="workflow-row" :data-workflow-id="w.id" @click="openWorkflow(w)">
          <td class="workflow-name-cell">
            <button type="button" class="workflow-name" :title="w.name" @click.stop="openWorkflow(w)">{{ w.name }}</button>
            <p v-if="w.description" class="workflow-description" :title="w.description">{{ w.description }}</p>
          </td>
          <td class="workflow-status-cell">
            <span class="workflow-status" :data-status="w.status"><span aria-hidden="true" />{{ t(w.status === 'published' ? 'common.status.published' : 'common.status.draft') }}</span>
          </td>
          <td class="workflow-notify-cell" :data-testid="isMobile ? 'wf-notify-inline' : 'wf-notify-cell'" @click.stop>
            <div class="workflow-notify-controls">
              <AppSelect
                :model-value="wfNotifyMode(w)"
                :options="notifyModesFor(w)"
                :disabled="savingNotifyWfId === w.id"
                :aria-label="`${w.name} · ${t('pages.projectDetail.notify.colPolicy')}`"
                variant="ghost"
                size="sm"
                class="workflow-notify-select"
                data-testid="wf-notify-mode"
                @update:model-value="onNotifyModeChange(w, $event)"
              />
              <button v-if="wfNotifyMode(w) === 'custom'" type="button" class="workflow-notify-edit" :disabled="savingNotifyWfId === w.id" :aria-label="t('pages.workflowList.notificationEvents')" :title="t('pages.workflowList.notificationEvents')" :aria-expanded="expandedNotifyId === w.id" :aria-controls="`workflow-notify-events-${w.id}`" data-testid="wf-notify-edit-events" @click="expandedNotifyId = expandedNotifyId === w.id ? null : w.id"><Icon name="settings" :size="13" /></button>
              <span v-if="savingNotifyWfId === w.id" class="sr-only" role="status" data-testid="wf-notify-saving">{{ t('common.buttons.saving') }}</span>
            </div>
            <fieldset v-if="wfNotifyMode(w) === 'custom' && expandedNotifyId === w.id" :id="`workflow-notify-events-${w.id}`" class="workflow-notify-events" data-testid="wf-notify-events">
              <legend class="sr-only">{{ t('pages.workflowList.notificationEvents') }}</legend>
              <label v-for="event in notifyEvents" :key="event.value">
                <input type="checkbox" :checked="wfNotifyHas(w, event.value)" :disabled="savingNotifyWfId === w.id" @change="toggleWorkflowNotifyEvent(w, event.value)" />
                <span>{{ t(event.labelKey) }}</span>
              </label>
            </fieldset>
          </td>
          <td class="workflow-home-cell" :data-testid="isMobile ? 'wf-home-visibility-inline' : 'wf-home-visibility-cell'" @click.stop>
            <span class="workflow-mobile-label">{{ t('pages.projectDetail.homeVisibility.col') }}</span>
            <AppSwitch :model-value="!!w.showOnHome" :disabled="savingHomeWfId === w.id" :aria-label="t('pages.projectDetail.homeVisibility.label')" class="workflow-home-switch" data-testid="wf-home-visibility-switch" @update:model-value="toggleWorkflowShowOnHome(w, $event)" />
            <span v-if="savingHomeWfId === w.id" class="sr-only" role="status" data-testid="wf-home-visibility-saving">{{ t('common.buttons.saving') }}</span>
          </td>
          <td class="workflow-updated-cell"><time :datetime="w.updatedAt" :title="fmtTime(w.updatedAt)">{{ w.updatedAt ? fmtTime(w.updatedAt).slice(5, 10) : '—' }}</time></td>
          <td class="workflow-actions-cell" data-testid="wf-actions-cell" @click.stop>
            <div class="workflow-row-actions" data-testid="wf-actions-group">
              <button type="button" class="workflow-edit-action" @click="openEdit(w)">{{ t('common.buttons.edit') }}</button>
              <button type="button" class="workflow-run-action" @click="openRun(w)"><Icon name="play" :size="13" />{{ t('common.buttons.run') }}</button>
              <div class="relative" data-wf-menu>
                <button type="button" class="workflow-more-action" :aria-label="t('pages.workflowList.moreActions')" :aria-expanded="openMenuId === w.id" aria-haspopup="menu" :aria-controls="openMenuId === w.id ? menuIdFor(w.id) : undefined" @click="toggleMenu(w.id, $event)"><Icon name="more" :size="16" /></button>
                <Teleport to="body">
                  <div v-if="openMenuId === w.id && menuPosition" :id="menuIdFor(w.id)" :style="menuPosition" class="fixed z-[100] w-[168px] rounded-lg border border-line bg-surface p-1 shadow-lg" role="menu" data-wf-menu @click.stop>
                    <button type="button" role="menuitem" class="workflow-menu-action" :class="{ 'text-warn': isFavorite(w.id) }" data-testid="workflow-favorite-btn" @click="toggleWorkflowFavorite(w)"><Icon :name="isFavorite(w.id) ? 'star-filled' : 'star'" :size="14" />{{ isFavorite(w.id) ? t('common.buttons.unfavorite') : t('common.buttons.favorite') }}</button>
                    <button type="button" role="menuitem" class="workflow-menu-action" :disabled="copyPreviewLoading === w.id" @click="openCopy(w)"><Icon name="copy" :size="14" />{{ copyPreviewLoading === w.id ? t('common.buttons.copying') : t('common.buttons.copy') }}</button>
                    <button type="button" role="menuitem" class="workflow-menu-action" @click="openExport(w)"><Icon name="download" :size="14" />{{ t('common.buttons.export') }}</button>
                    <div class="my-1 h-px bg-line" />
                    <button type="button" role="menuitem" class="workflow-menu-action text-err" @click="openDeleteWf(w)"><Icon name="trash" :size="14" />{{ t('common.buttons.delete') }}</button>
                  </div>
                </Teleport>
              </div>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

    <input ref="fileInput" type="file" accept=".json" class="hidden" @change="handleFileChange" />

    <RunLaunchModal
      v-if="runTarget"
      :open="!!runTarget"
      :workflow-id="runTarget.id"
      :project-id="runTarget.projectId"
      :workflow-name="runTarget.name"
      :fields="runFields"
      :run-inputs="runInputs"
      :run-images="runImages"
      :draft-restored="draftRestored"
      @close="closeRunModal"
      @stayed="onRunStayed"
      @view-run="onViewRun"
      @save-draft="saveRunDraftClick"
      @started="onRunStarted"
    />

    <CopyWorkflowModal
      v-if="copyModal"
      :open="!!copyModal"
      :source-id="copyModal.sourceId"
      :source-name="copyModal.sourceName"
      :suggested-name="copyModal.suggestedName"
      :existing-names="existingNames()"
      @close="closeCopyModal"
      @copied="onCopied"
    />

    <ExportVersionModal
      v-if="exportTarget"
      :open="!!exportTarget"
      :workflow-id="exportTarget.id"
      :workflow-name="exportTarget.name"
      :description="exportTarget.description"
      :needs-repo="exportTarget.needsRepo"
      :status="exportTarget.status"
      :defaults="exportTarget.defaults"
      @close="exportTarget = null"
    />

    <AppModal
      :open="baselineModalOpen"
      :title="t('pages.projectDetail.newWorkflow.modalTitle')"
      close-on-esc
      :close-on-backdrop="!creatingBaseline"
      @close="closeBaselineModal"
    >
      <p class="mb-3 text-[13px] leading-relaxed text-txt2">
        {{ t('pages.projectDetail.newWorkflow.modalHint') }}
      </p>
      <div class="mb-4">
        <label class="label" for="baseline-workflow-name">
          {{ t('pages.projectDetail.newWorkflow.nameLabel') }}
        </label>
        <input
          id="baseline-workflow-name"
          v-model="baselineName"
          class="input"
          autocomplete="off"
          data-testid="baseline-workflow-name"
          :placeholder="t('pages.projectDetail.newWorkflow.namePlaceholder')"
        />
      </div>
      <ReposEditor
        :repos="baselineRepos"
        :min-rows="1"
        @update:repos="baselineRepos = $event"
      />
      <div
        v-if="baselineCreateError"
        class="mt-3 flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        role="alert"
      >
        <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
        {{ baselineCreateError }}
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="creatingBaseline" @click="closeBaselineModal">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton
          variant="primary"
          :loading="creatingBaseline"
          :disabled="!hasValidBaselineRepo"
          data-testid="create-baseline-workflow"
          @click="createFromBaseline"
        >
          {{ creatingBaseline ? t('pages.projectDetail.newWorkflow.creating') : t('pages.projectDetail.newWorkflow.create') }}
        </AppButton>
      </template>
    </AppModal>

    <AppModal
      :open="!!deleteWfTarget"
      :title="t('pages.workflowList.deleteTitle', { name: deleteWfTarget?.name || '' })"
      @close="deleteWfTarget = null"
    >
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.workflowList.deleteWarning') }}
        </div>
        <p>{{ t('pages.workflowList.deleteConfirm', { name: deleteWfTarget?.name }) }}</p>
        <div
          v-if="deleteWfError"
          class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        >
          <Icon name="alert" :size="14" class="mt-0.5" />{{ deleteWfError }}
        </div>
      </div>
      <template #footer>
        <AppButton variant="ghost" @click="deleteWfTarget = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" :disabled="deletingWf" @click="confirmDeleteWf">
          {{ deletingWf ? t('common.buttons.deleting') : t('common.buttons.confirmDelete') }}
        </AppButton>
      </template>
    </AppModal>


</div>
</WorkspacePage>
</template>

<style scoped>
.workflow-toolbar { display: flex; flex: none; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 4px 0 18px; }
.workflow-toolbar-actions { display: flex; align-items: center; gap: 6px; }
.workflow-search { display: flex; width: min(320px, 100%); min-width: 0; height: 36px; align-items: center; gap: 8px; border: 1px solid rgb(var(--c-line)); border-radius: 8px; padding: 0 10px; color: rgb(var(--c-txt3)); }
.workflow-search:focus-within { border-color: rgb(var(--c-accent) / .5); }
.workflow-search input { width: 100%; min-width: 0; background: transparent; color: rgb(var(--c-txt)); font-size: 13px; outline: none; }
.workflow-directory-viewport { container: workflow-directory / inline-size; }
.workflow-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 48px 16px; text-align: center; }
.workflow-directory { width: 100%; table-layout: fixed; border-collapse: collapse; text-align: left; }
.workflow-col-status { width: 82px; }
.workflow-col-notify { width: 176px; }
.workflow-col-home { width: 72px; }
.workflow-col-updated { width: 82px; }
.workflow-col-actions { width: 142px; }
.workflow-directory th { height: 38px; padding: 0 12px; border-bottom: 1px solid rgb(var(--c-line)); color: rgb(var(--c-txt3)); font-size: 11px; font-weight: 400; white-space: nowrap; }
.workflow-directory td { padding: 15px 12px; border-bottom: 1px solid rgb(var(--c-line) / .65); vertical-align: middle; }
.workflow-directory th:first-child, .workflow-directory td:first-child { padding-left: 0; }
.workflow-directory th:last-child, .workflow-directory td:last-child { padding-right: 0; }
.workflow-row { cursor: pointer; }
.workflow-row:hover { background: rgb(var(--c-txt) / .018); }
.workflow-name { display: block; max-width: 100%; overflow: hidden; color: rgb(var(--c-txt)); font-size: 13px; font-weight: 500; text-align: left; text-overflow: ellipsis; white-space: nowrap; }
.workflow-name:hover { color: rgb(var(--c-accent-2)); }
.workflow-description { margin-top: 4px; overflow: hidden; color: rgb(var(--c-txt3)); font-size: 12px; line-height: 18px; text-overflow: ellipsis; white-space: nowrap; }
.workflow-status { display: inline-flex; align-items: center; gap: 6px; color: rgb(var(--c-txt2)); font-size: 11px; white-space: nowrap; }
.workflow-status > span { width: 5px; height: 5px; border-radius: 50%; background: rgb(var(--c-txt3) / .6); }
.workflow-status[data-status="published"] > span { background: rgb(var(--c-ok) / .7); }
.workflow-notify-controls { display: flex; min-width: 0; align-items: center; gap: 4px; }
.workflow-notify-select { min-width: 0; width: 116px; flex: none; }
.workflow-notify-edit { display: flex; width: 24px; height: 28px; flex: none; align-items: center; justify-content: center; border-radius: 6px; color: rgb(var(--c-txt3)); }
.workflow-notify-edit:hover, .workflow-notify-edit[aria-expanded="true"] { background: rgb(var(--c-txt) / .045); color: rgb(var(--c-txt)); }
.workflow-notify-events { display: flex; flex-wrap: wrap; gap: 8px 12px; margin: 8px 0 0; border: 0; padding: 0; }
.workflow-notify-events label { display: inline-flex; align-items: center; gap: 5px; color: rgb(var(--c-txt2)); font-size: 11px; white-space: nowrap; }
.workflow-notify-events input { accent-color: rgb(var(--c-accent)); }
.workflow-home-cell { text-align: center; }
.workflow-home-switch { transform: scale(.85); transform-origin: center; }
.workflow-mobile-label { display: none; }
.workflow-updated-cell { color: rgb(var(--c-txt3)); font-size: 12px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.workflow-row-actions { display: flex; align-items: center; justify-content: flex-end; gap: 2px; white-space: nowrap; }
.workflow-row-actions > button, .workflow-more-action { display: inline-flex; min-height: 30px; align-items: center; justify-content: center; gap: 5px; border-radius: 6px; padding: 0 7px; color: rgb(var(--c-txt2)); font-size: 12px; }
.workflow-row-actions > .workflow-run-action { color: rgb(var(--c-accent-2)); }
.workflow-row-actions button:hover { background: rgb(var(--c-txt) / .04); }
.workflow-more-action { width: 28px; padding: 0; color: rgb(var(--c-txt3)); }
.workflow-menu-action { display: flex; width: 100%; min-height: 36px; align-items: center; gap: 8px; border-radius: 6px; padding: 8px 10px; color: rgb(var(--c-txt2)); text-align: left; font-size: 12px; }
.workflow-menu-action:hover { background: rgb(var(--c-txt) / .045); }
.workflow-menu-action.text-err { color: rgb(var(--c-err)); }
.workflow-menu-action:disabled { opacity: .5; }
@container workflow-directory (max-width: 740px) {
  .workflow-directory, .workflow-directory tbody { display: block; }
  .workflow-directory colgroup, .workflow-directory thead { display: none; }
  .workflow-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'name status' 'notify home' 'updated actions'; align-items: center; gap: 7px 14px; border-bottom: 1px solid rgb(var(--c-line) / .7); padding: 16px 0; }
  .workflow-directory .workflow-row td { min-width: 0; border: 0; padding: 0; }
  .workflow-name-cell { grid-area: name; }
  .workflow-status-cell { grid-area: status; justify-self: end; }
  .workflow-notify-cell { grid-area: notify; }
  .workflow-home-cell { grid-area: home; display: flex; align-items: center; justify-content: flex-end; gap: 4px; }
  .workflow-mobile-label { display: inline; color: rgb(var(--c-txt3)); font-size: 11px; }
  .workflow-updated-cell { grid-area: updated; }
  .workflow-actions-cell { grid-area: actions; }
}
@media (max-width: 767px) {
  .workflow-toolbar { gap: 10px; padding-bottom: 12px; }
  .workflow-search { width: 100%; height: 40px; }
  .workflow-toolbar-actions { justify-content: flex-end; width: 100%; }
  .workflow-row-actions > button, .workflow-more-action, .workflow-notify-edit, .workflow-menu-action { min-height: 44px; }
  .workflow-notify-edit { width: 32px; }
}
</style>

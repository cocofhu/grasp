<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import PriorityBadge from '@/components/ui/PriorityBadge.vue'
import PrioritySegmented, { type RunPriority } from '@/components/ui/PrioritySegmented.vue'
import TruncatedTextTooltip from '@/components/ui/TruncatedTextTooltip.vue'
import WorkflowCanvas from '@/components/canvas/WorkflowCanvas.vue'
import RunGatePanel from '@/components/run/RunGatePanel.vue'
import RunClarifyPanel from '@/components/run/RunClarifyPanel.vue'
import RunReviewPanel from '@/components/run/RunReviewPanel.vue'
import RunPreviewPanel from '@/components/run/RunPreviewPanel.vue'
import RunProductPanel from '@/components/run/RunProductPanel.vue'
import RunLogPanel from '@/components/run/RunLogPanel.vue'
import RunSandboxPanel from '@/components/run/RunSandboxPanel.vue'
import RunOutputPanel from '@/components/run/RunOutputPanel.vue'
import StateTracePanel from '@/components/run/StateTracePanel.vue'
import VariablesPanel from '@/components/run/VariablesPanel.vue'
import ArtifactPanel from '@/components/run/ArtifactPanel.vue'
import RunSandboxEnvPanel from '@/components/run/RunSandboxEnvPanel.vue'
import ExecutionTimeline from '@/components/run/ExecutionTimeline.vue'
import ExecutionStatsPanel from '@/components/run/ExecutionStatsPanel.vue'
import RunViewModeSwitcher from '@/components/run/RunViewModeSwitcher.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import RefreshStrip from '@/components/run/RefreshStrip.vue'
import HardLoadLayer from '@/components/run/HardLoadLayer.vue'
import AppTabs from '@/components/ui/AppTabs.vue'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import AppModal from '@/components/ui/AppModal.vue'

import { useRunDetail } from '@/lib/run/useRunDetail'


const {
  t,
  route,
  router,
  toast,
  runId,
  emptyRun,
  emptyWorkflow,
  run,
  wf,
  unknownModelDisplayName,
  loadUnknownModelDisplayName,
  runLoading,
  loadError,
  loadErrorKind,
  refreshing,
  manual,
  reviewChatRef,
  gateApprovalRef,
  ACTIVE,
  nowMs,
  elapsedSec,
  progressFrac,
  defaultNode,
  selected,
  selNode,
  selNodeDisplayLabel,
  selExecutions,
  selIterIdx,
  pendingIter,
  selExecIdx,
  viewingLatest,
  selRun,
  selStatus,
  selRunView,
  selection,
  runFailureReason,
  showRunFailureBanner,
  lastStagedAppPreviewPick,
  onAppPreviewStagedPick,
  onAppPreviewReviewPick,
  mergeStagedAppPreviewPick,
  live,
  isClarifySessionBusy,
  refreshArtifactPreviewState,
  wsApi,
  resetRunState,
  classifyRunLoadError,
  fetchRunData,
  loadRun,
  onFocusRefresh,
  onVisible,
  onArtifactDeleted,
  gateError,
  gateSubmitting,
  clarifyConfirmError,
  clarifyConfirmCanAbort,
  onGateResolve,
  onClarifySend,
  onClarifyRetryLast,
  onClarifyCancel,
  onClarifyQueueRemove,
  onClarifyQueueReorder,
  onClarifyFinish,
  onClarifyFinishAbort,
  canCancelRun,
  showCancelConfirm,
  cancellingRun,
  cancelRunError,
  openCancelConfirm,
  closeCancelConfirm,
  mapCancelRunError,
  confirmCancelRun,
  canDeleteRun,
  deleteRunHint,
  showDeleteConfirm,
  deletingRun,
  deleteRunError,
  openDeleteConfirm,
  closeDeleteConfirm,
  mapDeleteRunError,
  confirmDeleteRun,
  exportLogs,
  resuming,
  resumeError,
  onResume,
  priorityDraft,
  prioritySaving,
  priorityError,
  priorityOk,
  priorityPopoverOpen,
  priorityBadgeRef,
  priorityEditorRef,
  priorityPopoverStyle,
  priorityEditable,
  showPriorityChevron,
  committedPriority,
  priorityChevronClass,
  priorityTriggerTitle,
  priorityTriggerAria,
  priorityHint,
  clearPriorityOkTimer,
  placePriorityPopover,
  openPriorityPopover,
  closePriorityPopover,
  togglePriorityPopover,
  showPrioritySaved,
  syncPriorityDraft,
  onPriorityKeydown,
  onPriorityReposition,
  savePriority,
  canResume,
  canResumeSelected,
  statusMap,
  activePath,
  desktopOuterSashLayout,
  splitRootRef,
  workspacePx,
  outerRightPx,
  outerFullOpen,
  outerSashBooting,
  outerSashDragging,
  outerSashStartX,
  outerSashStartW,
  outerSashDidDrag,
  OUTER_SASH_DRAG_THRESHOLD_PX,
  measureWorkspace,
  applyOuterLayout,
  persistOuterLayout,
  setOuterSashDraggingUi,
  onOuterSashPointerDown,
  onOuterSashPointerMove,
  onOuterSashPointerUp,
  onOuterSashDblClick,
  onOuterSashWindowResize,
  reviewRightPanelStyle,
  outerAriaMin,
  outerAriaMax,
  leftPaneStyle,
  showDetail,
  detailTab,
  detailTabs,
  applyDetailArtifactsDeepLink,
  viewMode,
  statsTab,
  selectExecution,
  fmtTime,
  fmtDuration,
  formatTrigger,
  isMobile,
  mobileMainPanel,
  timelineScrollToken,
  mobileDetailPanelLabel,
  showMobileTimelinePanel,
  showMobileDetailPanel,
  backToMobileTimeline,
  selectNode,
  nodeTab,
  nodeTabs,
  onNodeTabDisabledClick,
  clarifyDraft,
  clarifyAttachments,
  clarifyAnnotations,
  clarifyInputActive,
  clarifySandboxFailed,
  selClarify,
  sandboxLookup,
  sbxLogLoading,
  sbxLog,
  fetchSandboxLog,
  retryRehydrate,
  selRehydrateStatus,
  loadEarlierEvents,
  logEvents,
  logHasMore,
  logLive,
  logBusy,
  selMcpCalls,
  panelSwitching,
  openSandboxConsole,
  goSandboxLogTab,
  currentLiveLogBootSession,
  onLiveLogBootSession,
} = useRunDetail()
</script>

<template>
  <div data-testid="run-detail-root" class="flex h-full min-w-0 flex-col overflow-x-hidden bg-base">
    <!-- top bar: ≤767 two rows (status full-text priority); md+ single row -->
    <header class="shrink-0 overflow-x-hidden border-b border-line bg-surface px-5 py-3">
      <div v-if="runLoading || loadError" class="flex min-w-0 items-center gap-3">
        <button class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt" @click="router.push('/runs')">
          <Icon name="arrow-left" :size="18" />
        </button>
        <h1 class="min-w-0 truncate text-[17px] font-semibold text-txt">Run #{{ runId.replace('run-', '') }}</h1>
        <span v-if="runLoading" class="chip shrink-0 text-txt3">{{ t('pages.runDetail.loadingChip') }}</span>
        <span
          v-else
          class="chip shrink-0 border-err/35 bg-err/8 text-err"
          data-testid="run-load-error-chip"
        >{{
          loadErrorKind === 'not_found'
            ? t('pages.runDetail.notFoundChip')
            : t('pages.runDetail.loadFailedChip')
        }}</span>
      </div>
      <template v-else>
        <div class="flex min-w-0 flex-col gap-2 md:flex-row md:items-center md:gap-3">
          <div data-testid="run-header-row1" class="flex min-w-0 flex-1 items-center gap-2 md:gap-3">
            <button class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt" @click="router.push('/runs')">
              <Icon name="arrow-left" :size="18" />
            </button>
            <h1 class="min-w-0 truncate text-[17px] font-semibold text-txt">Run #{{ run.id.replace('run-', '') }}</h1>
            <TruncatedTextTooltip
              :text="run.workflowName"
              data-testid="workflow-chip"
              class="chip hidden max-w-[9rem] truncate md:inline-flex"
            />
            <span
              v-if="run.workflowVersion"
              data-testid="version-chip"
              class="chip shrink-0"
              :title="t('common.format.pinnedVersionTitle')"
            >v{{ run.workflowVersion }}</span>
            <StatusPill data-testid="status-pill" :status="run.status" class="shrink-0" />
            <button
              v-if="priorityEditable"
              ref="priorityBadgeRef"
              type="button"
              data-testid="priority-badge"
              class="inline-flex shrink-0 items-center gap-1 border-0 bg-transparent p-0 text-left transition hover:opacity-90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-2"
              :title="priorityTriggerTitle"
              :aria-label="priorityTriggerAria"
              aria-haspopup="dialog"
              :aria-expanded="priorityPopoverOpen"
              aria-controls="run-priority-editor"
              @click="togglePriorityPopover"
            >
              <PriorityBadge :priority="run.priority" hide-title />
              <Icon
                v-if="showPriorityChevron"
                name="chevron-down"
                :size="10"
                class="shrink-0 opacity-75 transition-transform"
                :class="[priorityChevronClass, { 'rotate-180': priorityPopoverOpen }]"
                aria-hidden="true"
              />
            </button>
            <PriorityBadge
              v-else
              data-testid="priority-badge"
              :priority="run.priority"
              class="shrink-0"
            />
          </div>
          <div data-testid="run-header-actions" class="flex flex-wrap items-center gap-2 pl-10 md:ml-auto md:shrink-0 md:pl-0">
            <AppButton variant="ghost" size="sm" icon="edit" @click="router.push('/workflows/' + run.workflowId + '/edit')">{{ t('common.buttons.edit') }}</AppButton>
            <AppButton variant="ghost" size="sm" icon="doc" @click="showDetail = true">{{ t('common.buttons.details') }}</AppButton>
            <AppButton
              data-testid="export-logs-btn"
              variant="ghost"
              size="sm"
              icon="download"
              :disabled="runLoading"
              @click="exportLogs"
            >{{ t('pages.runDetail.exportLogs') }}</AppButton>
            <button
              class="inline-flex items-center gap-1.5 rounded-md border border-line bg-surface px-2.5 py-1.5 text-xs font-medium text-txt transition hover:border-line-strong hover:bg-elevated disabled:opacity-45"
              :disabled="refreshing || runLoading"
              @click="loadRun(false)"
            >
              <Icon name="refresh" :size="14" :class="{ 'animate-spin': refreshing }" />
              {{ t('common.buttons.refresh') }}
            </button>
            <AppButton
              v-if="canCancelRun"
              data-testid="cancel-run-btn"
              variant="danger"
              size="sm"
              :icon="cancellingRun ? 'spinner' : 'close'"
              :disabled="cancellingRun"
              :aria-busy="cancellingRun ? 'true' : 'false'"
              @click="openCancelConfirm"
            >{{ cancellingRun ? t('common.buttons.cancelling') : t('common.buttons.cancelRun') }}</AppButton>
            <AppButton
              data-testid="delete-run-btn"
              variant="danger"
              size="sm"
              icon="trash"
              :disabled="!canDeleteRun || deletingRun"
              :title="deleteRunHint || t('common.buttons.deleteRun')"
              @click="openDeleteConfirm"
            >{{ t('common.buttons.deleteRun') }}</AppButton>
            <span
              v-if="deleteRunHint"
              data-testid="delete-run-hint"
              class="text-[11px] text-txt3"
            >{{ deleteRunHint }}</span>
            <AppButton v-if="canResume" variant="primary" size="sm" icon="refresh" :disabled="resuming" @click="onResume('')">
              {{ resuming ? t('common.buttons.resuming') : t('common.buttons.resumeFromFail') }}
            </AppButton>
          </div>
        </div>
        <div v-if="resumeError" class="mt-1.5 pl-11 text-[12px] text-err">{{ resumeError }}</div>
        <div class="mt-2 flex min-w-0 max-w-full flex-wrap items-center gap-x-5 gap-y-1 pl-11 text-[12px] text-txt3">
          <span><Icon name="trigger" :size="12" class="mr-1 inline" />{{ formatTrigger(run.trigger) }}</span>
          <span><Icon name="clock" :size="12" class="mr-1 inline" />{{ fmtTime(run.startedAt) }}</span>
          <span>{{ t('pages.runDetail.duration') }} {{ fmtDuration(elapsedSec) }}</span>
          <span v-if="run.branch" class="min-w-0 max-w-full">{{ t('pages.runDetail.branch') }} <code class="inline-block max-w-full overflow-x-auto whitespace-nowrap align-bottom font-mono text-accent-2">{{ run.branch }}</code></span>
          <span v-if="run.git?.pushedSha" class="min-w-0 max-w-full">{{ t('pages.runDetail.sha') }} <code class="inline-block max-w-full overflow-x-auto whitespace-nowrap align-bottom font-mono text-accent-2">{{ run.git.pushedSha }}</code></span>
          <span v-else class="text-txt3">{{ t('pages.runDetail.noRepo') }}</span>
        </div>
        <div v-if="run.tags?.length" class="mt-2 flex flex-wrap items-center gap-1.5 pl-11">
          <span class="text-[12px] text-txt3">{{ t('pages.runDetail.tagsLabel') }}</span>
          <span v-for="tag in run.tags" :key="tag" class="chip text-txt2">{{ tag }}</span>
        </div>
        <div class="mt-2.5 flex items-center gap-3 pl-11">
          <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-elevated">
            <div class="h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-all" :style="{ width: progressFrac * 100 + '%' }" />
          </div>
          <span class="text-[11px] text-txt3">{{ Math.round(progressFrac * 100) }}%</span>
        </div>
        <div
          v-if="showRunFailureBanner"
          data-testid="run-failure-banner"
          class="mt-3 ml-11 mr-0 rounded-md border border-err/35 bg-err/8 px-3.5 py-2.5 text-[13px] leading-relaxed text-err"
          role="alert"
        >
          <strong class="mb-0.5 block text-[11px] font-semibold uppercase tracking-wide text-err/90">
            {{ t('pages.runDetail.failureBanner.title') }}
          </strong>
          <p class="m-0 whitespace-pre-wrap break-words text-txt">{{ runFailureReason }}</p>
        </div>
      </template>
    </header>

    <!-- Teleport avoids header overflow-x-hidden clipping; anchored to badge rect. -->
    <Teleport to="body">
      <div
        v-if="priorityPopoverOpen"
        data-testid="priority-popover-backdrop"
        class="fixed inset-0 z-30 bg-transparent"
        aria-hidden="true"
        @click="closePriorityPopover(true)"
      />
      <div
        v-if="priorityPopoverOpen"
        id="run-priority-editor"
        ref="priorityEditorRef"
        data-testid="run-priority-editor"
        role="dialog"
        tabindex="-1"
        :aria-label="t('pages.runDetail.priorityTitle')"
        class="rounded-lg fixed z-40 border border-line-strong bg-surface p-3.5 shadow-card outline-none"
        :style="priorityPopoverStyle"
      >
        <h3 class="mb-2.5 text-[13px] font-semibold text-txt">{{ t('pages.runDetail.priorityTitle') }}</h3>
        <PrioritySegmented v-model="priorityDraft" />
        <p class="mt-2.5 text-[12px] leading-relaxed text-txt3">{{ priorityHint }}</p>
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <AppButton
            variant="primary"
            size="sm"
            :disabled="prioritySaving || priorityDraft === committedPriority"
            @click="savePriority"
          >
            {{ prioritySaving ? t('common.buttons.saving') : t('common.buttons.save') }}
          </AppButton>
        </div>
        <div
          v-if="priorityError"
          class="rounded-lg mt-2.5 flex items-start gap-1.5 border border-err/30 bg-err/10 px-2.5 py-2 text-[12px] text-err"
        >
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          <span>{{ priorityError }}</span>
        </div>
        <div
          v-else-if="priorityOk"
          class="rounded-lg mt-2.5 border border-ok/30 bg-ok/10 px-2.5 py-2 text-[12px] text-ok"
        >
          {{ t('pages.runDetail.prioritySaved') }}
        </div>
      </div>
    </Teleport>

    <div data-testid="run-detail-main" class="relative flex min-h-0 min-w-0 w-full max-w-full flex-1">
      <div
        v-show="!runLoading && !loadError"
        ref="splitRootRef"
        data-testid="run-detail-content"
        class="flex min-h-0 min-w-0 w-full max-w-full flex-1 flex-col md:flex-row"
        :class="{ 'run-detail-outer-sash-booting': outerSashBooting && desktopOuterSashLayout }"
      >
      <!--
        View mode switcher placement (plan g1):
        - Mobile: document-flow top bar (always reachable for stats).
        - Desktop stats: absolute over full-width stats chrome (md:pt-12 keeps clearance).
        - Desktop canvas/timeline default split: inside left pane only (clipped by overflow).
        - Desktop full-open: in right panel flow ABOVE node tabs (clickable, does not cover AppTabs).
        Exactly one mount is active at a time → single data-testid instance.
      -->
      <div
        v-if="isMobile"
        data-testid="run-detail-view-mode-switcher"
        class="flex shrink-0 items-center gap-2 border-b border-line bg-surface px-3 py-2"
      >
        <RunViewModeSwitcher v-model:view-mode="viewMode" :is-mobile="isMobile" />
      </div>
      <div
        v-else-if="viewMode === 'stats'"
        data-testid="run-detail-view-mode-switcher"
        class="absolute left-3 top-3 z-10"
      >
        <RunViewModeSwitcher v-model:view-mode="viewMode" :is-mobile="isMobile" />
      </div>

      <!-- Stats mode: full-width single/multi tabs; single = timeline+panel (no click link); multi = full panel. -->
      <template v-if="viewMode === 'stats'">
        <div class="relative flex min-h-0 min-w-0 w-full max-w-full flex-1 flex-col md:pt-12">
          <div class="flex shrink-0 border-b border-line bg-surface px-3 sm:px-4">
            <button
              type="button"
              class="border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors"
              :class="
                statsTab === 'single'
                  ? 'border-accent-2 text-accent-2'
                  : 'border-transparent text-txt3 hover:text-txt2'
              "
              @click="statsTab = 'single'"
            >
              {{ t('pages.executionStats.tabSingle') }}
            </button>
            <button
              type="button"
              class="border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors"
              :class="
                statsTab === 'multi'
                  ? 'border-accent-2 text-accent-2'
                  : 'border-transparent text-txt3 hover:text-txt2'
              "
              @click="statsTab = 'multi'"
            >
              {{ t('pages.executionStats.tabMulti') }}
            </button>
          </div>
          <div
            data-testid="run-stats-split"
            class="relative flex min-h-0 min-w-0 w-full max-w-full flex-1 flex-col"
          >
            <Transition name="ui-fade" mode="out-in">
              <div
                v-if="statsTab === 'single'"
                key="stats-single"
                class="relative flex min-h-0 min-w-0 w-full max-w-full flex-1 flex-col md:flex-row"
              >
                <div class="relative min-h-[240px] min-w-0 flex-1 border-b border-line md:min-h-0 md:border-b-0 md:border-r">
                  <ExecutionTimeline
                    :run="run"
                    :nodes="wf.nodes"
                    :selected-node-id="null"
                    :selected-exec-idx="-1"
                    :interactive="false"
                    :now-ms="nowMs"
                  />
                </div>
                <div
                  data-testid="run-stats-panel-wrap"
                  class="flex min-h-[320px] min-w-0 w-full max-w-full shrink-0 flex-col bg-surface md:min-h-0 md:w-[min(520px,46%)]"
                >
                  <ExecutionStatsPanel
                    :run="run"
                    :nodes="wf.nodes"
                    :wall-sec="elapsedSec"
                    :now-ms="nowMs"
                    :stats-tab="statsTab"
                    :unknown-model-display-name="unknownModelDisplayName"
                    @update:stats-tab="statsTab = $event"
                  />
                </div>
              </div>
              <div
                v-else
                key="stats-multi"
                data-testid="run-stats-panel-wrap"
                class="flex min-h-[320px] min-w-0 w-full max-w-full flex-1 flex-col bg-surface md:min-h-0"
              >
                <ExecutionStatsPanel
                  :run="run"
                  :nodes="wf.nodes"
                  :wall-sec="elapsedSec"
                  :now-ms="nowMs"
                  :stats-tab="statsTab"
                  :unknown-model-display-name="unknownModelDisplayName"
                  @update:stats-tab="statsTab = $event"
                />
              </div>
            </Transition>
          </div>
        </div>
      </template>

      <template v-else>
      <!-- Canvas: desktop only (narrow viewMode is normalized away from canvas). -->
      <div
        v-if="viewMode === 'canvas'"
        data-testid="run-detail-left-pane"
        class="relative hidden min-w-0 flex-1 overflow-hidden border-r border-line md:block"
        :class="outerFullOpen ? 'pointer-events-none' : ''"
        :style="leftPaneStyle"
      >
        <!-- plan g1.1: switcher floats only inside left canvas when split is open -->
        <div
          v-if="!outerFullOpen"
          data-testid="run-detail-view-mode-switcher"
          class="absolute left-3 top-3 z-10"
        >
          <RunViewModeSwitcher v-model:view-mode="viewMode" :is-mobile="isMobile" />
        </div>
        <WorkflowCanvas
          :nodes="wf.nodes"
          :edges="wf.edges"
          mode="run"
          :status-map="statusMap"
          :selected-node="selected"
          :active-path="activePath"
          @select-node="selectNode"
        />
        <div class="pointer-events-none absolute right-3 top-3 rounded-md border border-line bg-surface/90 px-2.5 py-1 text-[11px] text-txt3 backdrop-blur">
          {{ t('pages.runDetail.canvasHint') }}
        </div>
      </div>

      <!-- Mobile ≤767: page-level timeline / detail tabs (Demo single-panel). -->
      <div
        v-if="isMobile && viewMode === 'timeline'"
        data-testid="mobile-main-panel-tabs"
        class="flex shrink-0 border-b border-line bg-surface"
      >
        <button
          type="button"
          data-testid="mobile-panel-timeline"
          class="flex-1 border-b-2 px-3 py-2.5 text-[12px] font-semibold transition-colors"
          :class="
            mobileMainPanel === 'timeline'
              ? 'border-accent text-accent'
              : 'border-transparent text-txt3'
          "
          @click="showMobileTimelinePanel"
        >
          {{ t('pages.runDetail.timeline') }}
        </button>
        <button
          type="button"
          data-testid="mobile-panel-detail"
          class="flex-1 border-b-2 px-3 py-2.5 text-[12px] font-semibold transition-colors"
          :class="
            mobileMainPanel === 'detail'
              ? 'border-accent text-accent'
              : 'border-transparent text-txt3'
          "
          @click="showMobileDetailPanel"
        >
          {{ mobileDetailPanelLabel }}
        </button>
      </div>

      <!-- Timeline: mobile single-panel (min-h-0 flex-1); desktop side pane. -->
      <div
        v-if="viewMode === 'timeline'"
        v-show="!isMobile || mobileMainPanel === 'timeline'"
        data-testid="run-timeline-pane"
        class="relative min-h-0 min-w-0 flex-1 border-b border-line md:border-b-0 md:border-r md:pt-12"
        :class="[isMobile ? 'border-b-0' : '', outerFullOpen ? 'pointer-events-none' : '']"
        :style="leftPaneStyle"
      >
        <!-- plan g1.1: switcher floats only inside left timeline when split is open -->
        <div
          v-if="!isMobile && !outerFullOpen"
          data-testid="run-detail-view-mode-switcher"
          class="absolute left-3 top-3 z-10"
        >
          <RunViewModeSwitcher v-model:view-mode="viewMode" :is-mobile="isMobile" />
        </div>
        <ExecutionTimeline
          :run="run"
          :nodes="wf.nodes"
          :selected-node-id="selected"
          :selected-exec-idx="selExecIdx"
          :now-ms="nowMs"
          :ensure-visible-token="timelineScrollToken"
          @select="selectExecution"
        />
      </div>

      <!-- Outer sash: desktop only. Hidden on narrow (no horizontal drag). -->
      <div
        v-if="desktopOuterSashLayout"
        class="run-detail-outer-sash relative hidden shrink-0 cursor-col-resize bg-line md:block"
        :class="[
          outerSashDragging || outerFullOpen ? 'bg-accent' : 'hover:bg-accent',
          outerFullOpen ? 'is-full' : '',
        ]"
        role="separator"
        aria-orientation="vertical"
        :aria-valuemin="outerAriaMin"
        :aria-valuemax="outerAriaMax"
        :aria-valuenow="outerRightPx"
        :aria-label="t('pages.runDetail.resizeOuterSash')"
        :title="t('pages.runDetail.resizeOuterSash')"
        data-testid="run-detail-outer-sash"
        @pointerdown="onOuterSashPointerDown"
        @pointermove="onOuterSashPointerMove"
        @pointerup="onOuterSashPointerUp"
        @pointercancel="onOuterSashPointerUp"
        @dblclick="onOuterSashDblClick"
      />

      <!-- right panel: scoped to the selected node; mobile fills main area when active -->
      <div
        v-show="!isMobile || mobileMainPanel === 'detail' || viewMode !== 'timeline'"
        data-testid="run-detail-right-panel"
        class="flex min-h-0 min-w-0 w-full max-w-full flex-col bg-surface"
        :class="[
          isMobile && viewMode === 'timeline' ? 'flex-1' : 'shrink-0 md:shrink-0',
        ]"
        :style="reviewRightPanelStyle"
      >
        <!-- plan g1.2: full-open keeps switcher clickable above node tabs, not over them -->
        <div
          v-if="!isMobile && outerFullOpen"
          data-testid="run-detail-view-mode-switcher"
          class="flex shrink-0 items-center gap-2 px-3 pb-1 pt-2"
        >
          <RunViewModeSwitcher v-model:view-mode="viewMode" :is-mobile="isMobile" />
        </div>
        <!-- Mobile detail chrome: back to timeline -->
        <div
          v-if="isMobile && viewMode === 'timeline'"
          data-testid="mobile-detail-back-bar"
          class="flex shrink-0 items-center gap-2 border-b border-line bg-surface px-3 py-2"
        >
          <button
            type="button"
            data-testid="mobile-back-to-timeline"
            class="inline-flex items-center gap-1 rounded-md border border-line bg-elevated px-2 py-1 text-[11px] font-semibold text-txt2 hover:bg-surface"
            @click="backToMobileTimeline"
          >
            <Icon name="arrow-left" :size="12" />
            {{ t('pages.runDetail.backToTimeline') }}
          </button>
          <StatusPill v-if="selStatus" :status="selStatus" size="sm" class="shrink-0" />
          <span v-if="selNode" class="min-w-0 truncate text-[11px] text-txt3">{{ selNodeDisplayLabel }}</span>
        </div>
        <template v-if="selNode && selRunView">
          <!-- Per-node execution history: a node re-run by a loop-back / gate
               revise / rollback keeps every past execution. Switch between them
               to trace each run's own output, log, and duration. -->
          <div v-if="selExecutions.length > 1" class="flex shrink-0 flex-wrap items-center gap-1.5 border-b border-line px-3 py-2">
            <span class="mr-0.5 text-[11px] text-txt3">{{ t('pages.runDetail.executionHistory') }}</span>
            <button
              v-for="(ex, i) in selExecutions"
              :key="i"
              class="rounded-md border px-2 py-0.5 text-[11px] transition-colors"
              :class="i === selExecIdx ? 'border-accent/50 bg-accent-dim text-accent' : 'border-line text-txt2 hover:bg-elevated'"
              :title="ex.durationSec != null ? t('pages.runDetail.duration') + ' ' + fmtDuration(ex.durationSec) : ''"
              @click="selIterIdx = i"
            >
              {{ t('pages.runDetail.executionN', { n: ex.iteration || i + 1 }) }}
            </button>
            <span v-if="!viewingLatest" class="ml-1 text-[11px] text-warn">{{ t('pages.runDetail.historicalReadonly') }}</span>
          </div>
          <div v-if="canResumeSelected" class="flex shrink-0 items-center gap-2 border-b border-line bg-err/5 px-3 py-2">
            <Icon name="alert" :size="13" class="text-err" />
            <span class="text-[12px] text-txt2">{{ t('pages.runDetail.nodeFailed') }}</span>
            <div class="flex-1" />
            <AppButton variant="primary" size="sm" icon="refresh" :disabled="resuming" @click="onResume(selected!)">
              {{ resuming ? t('common.buttons.resuming') : t('common.buttons.resumeFromNode') }}
            </AppButton>
          </div>
          <div class="shrink-0 px-3 pt-2">
            <AppTabs :tabs="nodeTabs" v-model="nodeTab" @disabled-click="onNodeTabDisabledClick" />
          </div>
          <div
            class="relative min-h-0 flex-1"
            data-testid="run-detail-node-panel"
            :aria-busy="panelSwitching || sbxLogLoading ? 'true' : 'false'"
          >
            <RefreshStrip
              v-if="panelSwitching && !(nodeTab === 'gate' && run.gate)"
              data-testid="run-detail-panel-switching"
              :message="t('common.buttons.refreshing')"
            />
            <RunGatePanel
              v-if="nodeTab === 'gate' && run.gate"
              ref="gateApprovalRef"
              :gate="run.gate"
              :run="run"
              :submit-error="gateError"
              @resolve="onGateResolve"
              @react-revised="loadRun(false)"
            />
            <RunClarifyPanel
              v-else-if="nodeTab === 'clarify'"
              ref="reviewChatRef"
              :sandbox-failed="clarifySandboxFailed"
              :node-label="selNodeDisplayLabel"
              :node-id="selNode!.id"
              :node-error="selRun?.error"
              :clarify="selClarify"
              :run-id="run.id"
              :run="run"
              :mobile="isMobile"
              v-model:draft="clarifyDraft"
              v-model:attachments="clarifyAttachments"
              v-model:annotations="clarifyAnnotations"
              :input-active="clarifyInputActive"
              :confirm-error="clarifyConfirmError"
              :confirm-can-abort="clarifyConfirmCanAbort"
              :sel-status="selStatus"
              @send="onClarifySend"
              @retry-last="onClarifyRetryLast"
              @finish="onClarifyFinish"
              @finish-abort="onClarifyFinishAbort"
              @cancel="onClarifyCancel"
              @queue-remove="(itemId) => onClarifyQueueRemove(itemId)"
              @queue-reorder="onClarifyQueueReorder"
            />
            <RunReviewPanel
              v-else-if="nodeTab === 'review' && selNode && selRunView"
              ref="reviewChatRef"
              :mobile="isMobile"
              :node="selNode"
              :node-run="selRunView"
              :run="run"
              :clarify="selClarify"
              v-model:draft="clarifyDraft"
              v-model:attachments="clarifyAttachments"
              v-model:annotations="clarifyAnnotations"
              :input-active="clarifyInputActive"
              :confirm-error="clarifyConfirmError"
              :confirm-can-abort="clarifyConfirmCanAbort"
              :sel-status="selStatus"
              @send="onClarifySend"
              @retry-last="onClarifyRetryLast"
              @finish="onClarifyFinish"
              @finish-abort="onClarifyFinishAbort"
              @cancel="onClarifyCancel"
              @queue-remove="(itemId) => onClarifyQueueRemove(itemId)"
              @queue-reorder="onClarifyQueueReorder"
              @pick="onAppPreviewReviewPick"
              @staged-pick="onAppPreviewStagedPick"
            />
            <RunPreviewPanel v-else-if="nodeTab === 'preview' && selNode" :run-id="runId" :node-id="selNode.id" />
            <RunProductPanel v-else-if="nodeTab === 'product'" :node="selNode!" :node-run="selRunView!" :run="run" />
            <!-- Keep both panels mounted across log ↔ sandbox so boot timeout dwell survives CTA. -->
            <div v-else-if="nodeTab === 'log' || nodeTab === 'sandbox'" class="flex h-full min-h-0 flex-col">
              <RunLogPanel
                :key="`${selected}:${selExecIdx}`"
                v-show="nodeTab === 'log'"
                :events="logEvents"
                :live="logLive"
                :busy="logBusy"
                :status="selStatus"
                :mcp-calls="selMcpCalls"
                :has-more="logHasMore"
                :show-console="!!sandboxLookup"
                :sandbox-status="sandboxLookup?.status"
                :sandbox-container-status="sandboxLookup?.containerStatus"
                :boot-session="currentLiveLogBootSession"
                :rehydrate-status="selRehydrateStatus"
                @load-earlier="selected && loadEarlierEvents(selected)"
                @console-click="openSandboxConsole"
                @go-sandbox-log="goSandboxLogTab"
                @boot-session="onLiveLogBootSession"
                @retry-rehydrate="retryRehydrate"
              />
              <RunSandboxPanel
                v-show="nodeTab === 'sandbox'"
                :loading="sbxLogLoading"
                :sbx-log="sbxLog"
                :sel-status="selStatus"
                :sandbox-lookup="sandboxLookup"
                @refresh="fetchSandboxLog(selected)"
                @console-click="openSandboxConsole"
              />
            </div>
            <RunOutputPanel v-else :node="selNode!" :node-run="selRunView!" :run="run" />
          </div>
        </template>
        <EmptyState v-else :title="t('common.empty.selectNode')" :desc="t('common.empty.selectNodeDesc')" />
      </div>
      </template>
      </div>

      <div v-if="runLoading || loadError" class="absolute inset-0 z-10 bg-surface">
        <HardLoadLayer
          v-if="runLoading"
          :overlay="false"
          :stuck-after-ms="10_000"
          :stage="t('pages.runDetail.loadingRun')"
          @retry="loadRun(true)"
        />
        <EmptyState
          v-else
          icon="alert"
          :title="loadErrorKind === 'not_found' ? t('pages.runDetail.notFoundTitle') : t('pages.runDetail.loadFailedTitle')"
          :desc="loadErrorKind === 'not_found' ? t('pages.runDetail.notFoundDesc') : t('pages.runDetail.loadFailedDesc')"
          data-testid="run-load-error"
        >
          <AppButton
            v-if="loadErrorKind === 'not_found'"
            variant="outline"
            size="sm"
            disabled
            data-testid="run-retry-unavailable"
          >
            {{ t('pages.runDetail.retryUnavailable') }}
          </AppButton>
          <AppButton
            v-else
            variant="primary"
            size="sm"
            icon="refresh"
            data-testid="run-retry"
            @click="loadRun(true)"
          >
            {{ t('pages.runDetail.retry') }}
          </AppButton>
        </EmptyState>
      </div>
    </div>

    <AppDrawer :open="showDetail" :title="t('pages.runDetail.detailTitle')" :width="480" @close="showDetail = false">
      <div class="flex h-full flex-col">
        <div class="px-3 pt-2"><AppTabs :tabs="detailTabs" v-model="detailTab" /></div>
        <div class="min-h-0 flex-1">
          <Transition name="ui-fade" mode="out-in">
            <StateTracePanel v-if="detailTab === 'trace'" key="trace" :trace="run.trace || []" />
            <VariablesPanel v-else-if="detailTab === 'vars'" key="vars" :vars="run.vars || []" />
            <RunSandboxEnvPanel
              v-else-if="detailTab === 'sandboxEnv'"
              key="sandboxEnv"
              :entries="run.sandboxEnv || []"
            />
            <ArtifactPanel
              v-else-if="detailTab === 'artifacts'"
              key="artifacts"
              :artifacts="run.artifacts"
              @deleted="onArtifactDeleted"
            />
          </Transition>
        </div>
      </div>
    </AppDrawer>

    <AppModal
      :open="showCancelConfirm"
      :title="t('pages.runDetail.cancelTitle')"
      :width="440"
      @close="closeCancelConfirm"
    >
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.runDetail.cancelWarning') }}
        </div>
        <p>{{ t('pages.runDetail.cancelConfirm') }}</p>
        <div
          v-if="cancelRunError"
          class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
          role="alert"
          data-testid="cancel-run-error"
        >
          <Icon name="alert" :size="14" class="mt-0.5" />{{ cancelRunError }}
        </div>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="cancellingRun" @click="closeCancelConfirm">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton
          data-testid="confirm-cancel-run-btn"
          variant="danger"
          :icon="cancellingRun ? 'spinner' : 'close'"
          :disabled="cancellingRun"
          :aria-busy="cancellingRun ? 'true' : 'false'"
          @click="confirmCancelRun"
        >
          {{ cancellingRun ? t('common.buttons.cancelling') : t('common.buttons.confirmCancelRun') }}
        </AppButton>
      </template>
    </AppModal>

    <AppModal
      :open="showDeleteConfirm"
      :title="t('pages.runDetail.deleteTitle')"
      :width="440"
      @close="closeDeleteConfirm"
    >
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.runDetail.deleteWarning') }}
        </div>
        <p>{{ t('pages.runDetail.deleteConfirm') }}</p>
        <div
          v-if="deleteRunError"
          class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        >
          <Icon name="alert" :size="14" class="mt-0.5" />{{ deleteRunError }}
        </div>
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="deletingRun" @click="closeDeleteConfirm">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton
          data-testid="confirm-delete-run-btn"
          variant="danger"
          icon="trash"
          :disabled="deletingRun"
          @click="confirmDeleteRun"
        >
          {{ deletingRun ? t('common.buttons.deleting') : t('common.buttons.confirmDelete') }}
        </AppButton>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.run-detail-outer-sash-booting {
  visibility: hidden;
}
.run-detail-outer-sash {
  width: 4px;
  flex-shrink: 0;
  touch-action: none;
  z-index: 20;
}
.run-detail-outer-sash::before {
  content: '';
  position: absolute;
  inset: 0 -4px;
}
.run-detail-outer-sash.is-full {
  box-shadow: 8px 0 12px rgb(var(--c-accent) / 0.45);
}
</style>

<style>
body.run-detail-outer-sash-dragging {
  cursor: col-resize;
  user-select: none;
}
</style>

<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import GateApproval from '@/components/run/GateApproval.vue'
import GateShareLinkPanel from '@/components/run/GateShareLinkPanel.vue'
import InboxStartFailedPane from '@/components/inbox/InboxStartFailedPane.vue'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ReviewComposer from '@/components/run/ReviewComposer.vue'
import ConfirmFlowOverlay from '@/components/run/ConfirmFlowOverlay.vue'
import ArtifactLoadingPane from '@/components/run/ArtifactLoadingPane.vue'
import ClarifyProductStage from '@/components/run/ClarifyProductStage.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ReactConnectingState from '@/components/run/ReactConnectingState.vue'
import AppInlineError from '@/components/ui/AppInlineError.vue'
import { computed } from 'vue'
import { useConversationToolbarRunName } from '@/lib/inbox/conversationToolbarTitle'
import { isInboxCancelDeparture } from '@/lib/inbox/inboxCancelDeparture'
import { useGatesInbox } from '@/lib/inbox/useGatesInbox'

const {
  dismissStartFailure,
  retryListLoad,
  onAppPreviewStagedPick,
  onAppPreviewReviewPick,
  retryActiveRun,
  onReactRevised,
  onResolve,
  patchShareStatus,
  openSharePanel,
  onClarifySend,
  onClarifyRetryLast,
  onClarifyFinish,
  onClarifyFinishAbort,
  onClarifyCancel,
  onClarifyQueueRemove,
  onClarifyQueueReorder,
  listErrorMessage,
  listTotal,
  listItems,
  active,
  activeHomeSeed,
  activeQueuedFirstMessage,
  gateApprovalRef,
  reviewChatRef,
  reviewShellRef,
  confirmFlowDeskHold,
  inboxConfirmFlowPhase,
  inboxConfirmFlowReduce,
  inboxConfirmFlowToken,
  showProcessedBanner,
  isEditing,
  showListSkeleton,
  showListError,
  showListRefresh,
  listPanelBusy,
  clarifyConfirmError,
  clarifyConfirmCanAbort,
  startFailedActive,
  activeStarting,
  startingSandboxPhase,
  startingBootKey,
  activeRun,
  activeRunLoading,
  activeClarify,
  inboxRemoteKind,
  inboxStageNode,
  showClarifyReviewShell,
  clarifyComposerNodeId,
  clarifyComposerIteration,
  clarifyComposerTurns,
  activeNodeTask,
  clarifyComposerDone,
  activeHandoff,
  activeChainSegments,
  clarifyComposerLocked,
  activeFollowTail,
  activeFollowProcess,
  followObserving,
  followStopping,
  takeoverFollow,
  inboxClarifyStageKind,
  composerMode,
  activeGate,
  clarifyInputActive,
  sharePanelOpen,
  shareTarget,
  t,
  clarifyDraft,
  clarifyAttachments,
  clarifyAnnotations,
  inboxShareKind,
  isHumanGateInboxItem,
  pageControlByNode,
} = useGatesInbox()

/** Keep the current conversation while the sidebar finishes cancelling it. */
const holdingCancelledConversation = computed(
  () => !!active.value && isInboxCancelDeparture(active.value.runId),
)

/** Page-control state for the chat in view; empty until this user has turned it on in a drawer. */
const inboxPageControl = computed(() => {
  const map = pageControlByNode.value
  return map[clarifyComposerNodeId.value || ''] ?? Object.values(map)[0] ?? undefined
})

const inboxAgentName = computed(() => String(inboxStageNode.value?.config?.agent_profile || '').trim())

/** Toolbar title: loaded run, else the inbox row patchRunTitle rewrites. */
const inboxConversationRunName = useConversationToolbarRunName(() => {
  const item = active.value
  const loaded = item && activeRun.value?.id === item.runId ? activeRun.value : null
  return { run: loaded, fallback: item }
})
</script>

<template>
  <div
    class="flex h-full min-h-0 flex-col"
    data-testid="gates-inbox-view"
    :aria-busy="listPanelBusy ? 'true' : 'false'"
  >
    <!-- f6: processed while editing -->
    <div
      v-if="showProcessedBanner && isEditing"
      class="mb-3 flex shrink-0 items-center gap-2.5 rounded-md border border-warn/35 bg-warn/10 px-3.5 py-2.5 text-sm text-warn"
    >
      <Icon name="alert" :size="18" class="shrink-0" />
      <span>{{ t('pages.gatesInbox.processedBanner') }}</span>
    </div>

    <!-- One conversation on every screen size; selection lives in the sidebar. -->
    <div
      v-if="listItems.length || confirmFlowDeskHold || activeHandoff || activeFollowTail || holdingCancelledConversation"
      class="flex min-h-0 flex-1 items-stretch"
      :class="showListRefresh ? 'opacity-[0.55]' : ''"
    >
      <div v-if="active || confirmFlowDeskHold" class="relative flex h-full min-h-0 min-w-0 flex-1 flex-col"
        data-testid="inbox-active-detail" :data-run-id="active?.runId || ''" :data-node-id="active?.nodeId || ''">
        <div class="relative flex h-full min-h-0 w-full flex-col overflow-hidden bg-surface">
          <template v-if="active">
          <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
            <ArtifactLoadingPane v-if="activeRunLoading && active.type === 'gate'" message-key="pages.gatesInbox.loadingRun" />
            <GateApproval
              v-else-if="active.type === 'gate'"
              ref="gateApprovalRef"
              :key="active.runId + active.nodeId"
              :gate="activeGate!"
              :run="activeRun || undefined"
              :fill-preview="true"
              :unified-preview-budget="true"
              class="min-h-0 flex-1"
              :share-link="isHumanGateInboxItem(active) ? active.shareLink ?? { state: 'none' } : null"
              @resolve="onResolve"
              @react-revised="onReactRevised"
              @open-share="openSharePanel(active)"
            />
            <InboxStartFailedPane v-else-if="startFailedActive" @dismiss="dismissStartFailure" />
            <ReviewShell
              v-else-if="showClarifyReviewShell"
              :scope-key="`inbox:${active.runId}:${active.nodeId}:${clarifyComposerIteration}`"
              :run-id="active.runId"
              :run-name="inboxConversationRunName"
              ref="reviewShellRef"
              :key="active.runId"
              class="min-h-0 flex-1"
              :host-confirm-flow="false"
              split-drawer
              remember-artifact-panel
            >
              <template #stage>
                <ReactConnectingState
                  v-if="activeStarting"
                  mode="stage"
                  hide-home-tab
                  :sandbox-phase="startingSandboxPhase"
                  :boot-key="startingBootKey"
                />
                <ArtifactLoadingPane v-else-if="activeRunLoading" message-key="pages.gatesInbox.loadingRun" />
                <ReactArtifactStage
                  v-else
                  editable
                  :artifacts="activeRun?.artifacts || []"
                  :preview-artifact="activeClarify?.previewArtifact"
                  :run-id="active.runId"
                  :run="activeRun || undefined"
                  :node-id="active.nodeId"
                  :node="inboxStageNode"
                  :annotatable="clarifyInputActive"
                  :remote-kind="inboxRemoteKind"
                  :probe-registered-preview="composerMode === 'review'"
                  @pick="onAppPreviewReviewPick"
                  @staged-pick="onAppPreviewStagedPick"
                />
              </template>
              <template #sidebar>
                <ReactConnectingState
              v-if="activeStarting"
              :sandbox-phase="startingSandboxPhase"
              :boot-key="startingBootKey"
              :seed-text="activeQueuedFirstMessage"
            />
                <ReviewComposer
                  v-else
                  :key="`${active.runId}:${active.nodeId}`"
                  ref="reviewChatRef"
                  :mode="composerMode"
                  :run-id="active.runId"
                  :node-id="clarifyComposerNodeId"
                  :iteration="clarifyComposerIteration"
                  v-model:draft="clarifyDraft"
                  v-model:attachments="clarifyAttachments"
                  v-model:annotations="clarifyAnnotations"
                  :turns="clarifyComposerTurns"
                  :node-task="activeNodeTask"
                  :seed-human-text="activeHomeSeed?.text"
                  :seed-human-images="activeHomeSeed?.images ?? []"
                  :done="clarifyComposerDone"
                  :handoff="activeHandoff"
                  :prior-segments="activeChainSegments"
                  :node-label="active?.type === 'clarify' ? active.label : undefined"
                  :follow-tail="activeFollowTail"
                  :follow-process="activeFollowProcess"
                  :observe="followObserving"
                  :stopping="followStopping"
                  :composer-locked="clarifyComposerLocked"
                  :active="clarifyInputActive"
                  :model-switch="!!inboxAgentName"
                  :agent-name="inboxAgentName"
                  :current-configuration-id="activeClarify?.currentConfigurationId"
                  :current-model-id="activeClarify?.currentModelId"
                  :current-model-set="activeClarify?.currentModelSet"
                  :reasoning-effort="activeClarify?.reasoningEffort"
                  :model-notice="activeClarify?.modelNotice"
                  :confirm-error="clarifyConfirmError"
                  :confirm-can-abort="clarifyConfirmCanAbort"
                  :page-control="inboxPageControl"
                  @send="(text, images, anns, clientMessageId, references) => onClarifySend(text, images, anns, false, false, false, clientMessageId, references)"
                  @retry-last="onClarifyRetryLast"
                  @finish="onClarifyFinish"
                  @finish-abort="onClarifyFinishAbort"
                  @cancel="onClarifyCancel"
                  @takeover="takeoverFollow"
                  @queue-remove="(itemId) => onClarifyQueueRemove(itemId)"
                  @queue-reorder="onClarifyQueueReorder"
                />
              </template>
            </ReviewShell>
            <ClarifyProductStage
              v-else-if="active.type === 'clarify'"
              :product-nodes="[]"
              :selected-product-id="null"
              :stage-kind="inboxClarifyStageKind"
              :selected-node="null"
              :selected-node-run="null"
              :run="null"
              :loading="activeRunLoading"
              @retry="retryActiveRun"
            />
          </div>
          </template>
          <ConfirmFlowOverlay
            :phase="inboxConfirmFlowPhase"
            :reduce-motion="inboxConfirmFlowReduce"
            :play-token="inboxConfirmFlowToken"
          />
        </div>
      </div>
    </div>

    <!-- Loading and errors stay in the conversation area. -->
    <div
      v-else-if="showListSkeleton"
      class="flex min-h-0 flex-1 items-stretch"
      data-testid="inbox-list-skeleton"
    >
      <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col">
        <div class="flex h-full min-h-0 w-full flex-col overflow-hidden bg-surface">
          <div class="flex min-h-0 flex-1 flex-col items-center justify-center px-6 text-center">
            <p class="text-sm text-txt2">{{ t('pages.gatesInbox.listLoadingHint') }}</p>
          </div>
        </div>
      </div>
    </div>

    <div
      v-else-if="showListError"
      class="card flex min-h-0 flex-1 flex-col items-stretch justify-center overflow-auto p-4"
      data-testid="inbox-list-failed"
    >
      <AppInlineError
        :title="t('common.asyncState.loadFailedTitle')"
        :message="listErrorMessage"
        retry-testid="inbox-list-retry"
        @retry="retryListLoad"
      />
    </div>

    <div v-else class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto">
      <EmptyState
        icon="gate"
        :title="listTotal ? t('common.empty.noPendingGatesForWorkflow') : t('common.empty.noPendingGates')"
        :desc="listTotal ? t('common.empty.noPendingGatesWorkflowDesc') : t('common.empty.noPendingGatesDesc')"
      />
    </div>

    <GateShareLinkPanel
      :open="sharePanelOpen"
      :target="shareTarget"
      :kind="shareTarget ? inboxShareKind(shareTarget) : 'human_gate'"
      @close="sharePanelOpen = false"
      @updated="(st) => shareTarget && patchShareStatus(shareTarget, st)"
      @revoked="
        (st) => {
          if (shareTarget) patchShareStatus(shareTarget, st)
          sharePanelOpen = false
        }
      "
    />
  </div>
</template>

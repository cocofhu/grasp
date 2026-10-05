<script setup lang="ts">
import { toRefs } from 'vue'
import { useGateApprovalCtx } from './gateApprovalContext'
import Icon from '../../ui/Icon.vue'
import ParagraphInput from '../../ui/ParagraphInput.vue'
import HtmlPreview from '../../ui/HtmlPreview.vue'
import RefreshStrip from '../RefreshStrip.vue'
import UpstreamRequirementContext from '../UpstreamRequirementContext.vue'
import PreviewFeedbackChat from '../PreviewFeedbackChat.vue'
import GateProductEditor from '../GateProductEditor.vue'
import ArtifactLoadingPane from '../ArtifactLoadingPane.vue'
import PlanView from '../PlanView.vue'
import ProposalSelectView from '../ProposalSelectView.vue'
import StructuredArtifactView from '../StructuredArtifactView.vue'
import GateApprovalComposer from './GateApprovalComposer.vue'
import GateApprovalColdActions from './GateApprovalColdActions.vue'
import CommentArtifactSidebar from '../CommentArtifactSidebar.vue'

const ctx = useGateApprovalCtx()
const s = ctx.s
const productEditorRef = ctx.productEditorRef
const gateStageEl = ctx.gateStageEl
const {
  gate,
  run,
  isMobile,
  formText,
  formImages,
  resolved,
  formError,
  actionSubmitting,
  productDirty,
  savedProductContent,
  savedProductMeta,
  primaryProducts,
  excludedProduces,
  productName,
  canEditProducts,
  isVisualBody,
  isProposalSelect,
  bodyTemplate,
  usesPreviewIssues,
  openPreviewIssueCount,
  previewIssuesLoading,
  previewIssuesError,
  pickedSelector,
  pickedElementImage,
  commentPins,
  commentPinBadges,
  commentPinSelectedId,
  commentArtifactCommitted,
  commentArtifactWriting,
  commentArtifactWriteError,
  annotateDraft,
  proposalsDoc,
  proposalsLoading,
  planDoc,
  planLoading,
  productDoc,
  productHtml,
  productLoading,
  productLoadError,
  productHasSavedContent,
  reviewingIteration,
  previewFromArtifactFallback,
  shouldFillPreview,
  shouldFitStructured,
  useFillLayout,
  useUnifiedPreviewBudget,
  contentFitChromeOffsetPx,
  CONTENT_FIT_PREVIEW_MAX_VH,
  REVIEW_SHELL_WIDTH_KEY_APPROVAL,
  shouldHideGateForm,
  footerActions,
  isColdSession,
  helpColdText,
  helpReviseDetailNoIssuesText,
  helpReviseWithIssuesText,
  canReactRevise,
  canRecordIssue,
  canSubmitReact,
  showHotPass,
  showHotReject,
  hotRejectAllowEmpty,
  composerRejectLabel,
  composerPassDisabled,
  passAction,
  reactText,
  reactImages,
  reactAnnotations,
  reactSending,
  reactError,
  reactQueued,
  reactThinking,
  reactStreamText,
  reactStreamThought,
  reactInterrupted,
  reactStreamCompletedAt,
} = toRefs(s)
const {
  t,
  isActionDisabled,
  actionPendingLabel,
  actionButtonTitle,
  onProductSaved,
  onProductRefresh,
  retryLoadProduct,
  onHtmlPreviewPick,
  clearHtmlPreviewPick,
  onAnnotateSave,
  onAnnotateSendChat,
  onAnnotateClose,
  onCommentPinSelect,
  onCommentPinDelete,
  onWriteCommentArtifact,
  loadPreviewIssues,
  choose,
  recordFeedbackIssue,
  sendHotReject,
  onComposerPass,
  onComposerReject,
  cancelReactRevise,
  onSidebarAction,
  renderMarkdown,
} = s
</script>

<template>
  <div class="contents">
      <div
        class="min-h-0 flex-1"
        :class="
          useFillLayout && !(isMobile && isVisualBody)
            ? 'flex flex-col overflow-hidden'
            : isMobile
              ? 'scroll-area overflow-y-auto overflow-x-hidden'
              : 'scroll-area overflow-y-auto p-4'
        "
      >
        <ArtifactLoadingPane
          v-if="proposalsLoading"
          message-key="pages.gateApproval.loadingArtifact"
          :class="useFillLayout ? 'min-h-0 flex-1' : ''"
        />
        <div
          v-else-if="isProposalSelect && proposalsDoc"
          :class="useFillLayout ? 'scroll-area min-h-0 flex-1 overflow-y-auto p-4' : ''"
        >
          <GateProductEditor
            v-if="canEditProducts && run"
            ref="productEditorRef"
            class="mb-4"
            :run-id="run.id"
            :gate-node-id="gate.nodeId"
            :products="primaryProducts"
            :saved-content="savedProductContent"
            :saved-meta="savedProductMeta"
            :artifacts="run.artifacts"
            :run-status="run.status"
            :can-edit="canEditProducts"
            :load-error="productLoadError"
            :excluded-names="excludedProduces"
            :enlargeable="!isMobile"
            @saved="onProductSaved"
            @dirty-change="productDirty = $event"
            @refresh-request="onProductRefresh"
            @retry-load="retryLoadProduct"
          />
          <ProposalSelectView
            :doc="proposalsDoc"
            :resolved-id="resolved"
            @select="choose"
          />
          <div v-if="canReactRevise" class="mt-4">
            <GateApprovalComposer />
          </div>
        </div>
        <div
          v-else-if="canEditProducts && run"
          ref="gateStageEl"
          data-review-annotate-stage
          :class="useFillLayout ? 'scroll-area min-h-0 flex-1 overflow-y-auto' : ''"
        >
          <div
            v-if="reviewingIteration != null"
            class="shrink-0 border-b border-line bg-elevated/60 px-3 py-1.5 text-[11px] text-txt3"
          >
            {{ t('pages.gateApproval.reviewingUpstream', { n: reviewingIteration }) }}
            <span v-if="previewFromArtifactFallback" class="ml-2 text-warn">
              {{ t('pages.gateApproval.reviewingUpstreamFallback') }}
            </span>
          </div>
          <RefreshStrip v-if="productLoading && productHasSavedContent" />
          <div
            :class="
              isVisualBody
                ? 'flex min-h-0 flex-col gap-0 lg:flex-row lg:items-stretch'
                : 'contents'
            "
            :data-testid="isVisualBody ? 'comment-pin-desktop-split' : undefined"
          >
            <div :class="isVisualBody ? 'min-w-0 flex-1' : 'contents'">
              <GateProductEditor
                ref="productEditorRef"
                :run-id="run.id"
                :gate-node-id="gate.nodeId"
                :products="primaryProducts"
                :saved-content="savedProductContent"
                :saved-meta="savedProductMeta"
                :artifacts="run.artifacts"
                :run-status="run.status"
                :can-edit="canEditProducts"
                :content-loading="productLoading"
                :load-error="productLoadError"
                :excluded-names="excludedProduces"
                :enlargeable="!isMobile"
                :inspectable="isVisualBody"
                :comment-pins="isVisualBody ? commentPinBadges : []"
                :annotate-draft="isVisualBody ? annotateDraft : null"
                @saved="onProductSaved"
                @dirty-change="productDirty = $event"
                @refresh-request="onProductRefresh"
                @retry-load="retryLoadProduct"
                @pick="onHtmlPreviewPick"
                @pin-select="onCommentPinSelect"
                @annotate-save="onAnnotateSave"
                @annotate-send-chat="onAnnotateSendChat"
                @annotate-close="onAnnotateClose"
              />
            </div>
            <aside
              v-if="isVisualBody"
              class="flex w-full shrink-0 flex-col border-t border-line lg:w-[300px] lg:border-l lg:border-t-0"
              data-testid="editable-visual-feedback"
            >
              <CommentArtifactSidebar
                class="min-h-[240px] flex-1 lg:min-h-[420px]"
                :pins="commentPins"
                :selected-id="commentPinSelectedId"
                :artifact-committed="commentArtifactCommitted"
                :writing="commentArtifactWriting"
                :write-error="commentArtifactWriteError"
                @select="onCommentPinSelect"
                @edit="onCommentPinSelect"
                @delete="onCommentPinDelete"
                @write="onWriteCommentArtifact"
              />
              <div class="shrink-0 border-t border-line px-3 pb-3 pt-2">
                <PreviewFeedbackChat
                  :run-id="run.id"
                  :node-id="gate.nodeId"
                  :selector="pickedSelector"
                  :element-image="pickedElementImage"
                  v-model:text="reactText"
                  v-model:images="reactImages"
                  copy-variant="review"
                  @clear-selector="clearHtmlPreviewPick"
                  @issues-changed="loadPreviewIssues()"
                />
              </div>
            </aside>
          </div>
        </div>
        <div
          v-else-if="isVisualBody && productHtml"
          class="rounded-lg overflow-x-hidden border border-line"
          data-testid="comment-pin-desktop-split"
        >
          <div
            v-if="reviewingIteration != null"
            class="shrink-0 border-b border-line bg-elevated/60 px-3 py-1.5 text-[11px] text-txt3"
          >
            {{ t('pages.gateApproval.reviewingUpstream', { n: reviewingIteration }) }}
            <span v-if="previewFromArtifactFallback" class="ml-2 text-warn">
              {{ t('pages.gateApproval.reviewingUpstreamFallback') }}
            </span>
          </div>
          <div class="flex min-h-0 flex-col lg:flex-row lg:items-stretch">
            <div class="min-w-0 flex-1">
              <HtmlPreview
                :html="productHtml"
                :mode="isMobile ? 'inline' : 'default'"
                :enlargeable="!isMobile"
                inspectable
                :comment-pins="commentPinBadges"
                :annotate-draft="annotateDraft"
                @pick="onHtmlPreviewPick"
                @pin-select="onCommentPinSelect"
                @annotate-save="onAnnotateSave"
                @annotate-send-chat="onAnnotateSendChat"
                @annotate-close="onAnnotateClose"
              />
            </div>
            <aside
              v-if="run"
              class="flex w-full shrink-0 flex-col border-t border-line lg:w-[300px] lg:border-l lg:border-t-0"
            >
              <CommentArtifactSidebar
                class="min-h-[240px] flex-1 lg:min-h-[420px]"
                :pins="commentPins"
                :selected-id="commentPinSelectedId"
                :artifact-committed="commentArtifactCommitted"
                :writing="commentArtifactWriting"
                :write-error="commentArtifactWriteError"
                @select="onCommentPinSelect"
                @edit="onCommentPinSelect"
                @delete="onCommentPinDelete"
                @write="onWriteCommentArtifact"
              />
              <div class="shrink-0 border-t border-line px-3 pb-3 pt-2">
                <PreviewFeedbackChat
                  :run-id="run.id"
                  :node-id="gate.nodeId"
                  :selector="pickedSelector"
                  :element-image="pickedElementImage"
                  v-model:text="reactText"
                  v-model:images="reactImages"
                  copy-variant="review"
                  @clear-selector="clearHtmlPreviewPick"
                  @issues-changed="loadPreviewIssues()"
                />
              </div>
            </aside>
          </div>
        </div>
        <ArtifactLoadingPane
          v-else-if="planLoading"
          message-key="pages.gateApproval.loadingArtifact"
          :class="useFillLayout ? 'min-h-0 flex-1' : ''"
        />
        <div
          v-else
          ref="gateStageEl"
          data-review-annotate-stage
          :class="[
            useFillLayout ? 'scroll-area min-h-0 flex-1 overflow-y-auto overflow-x-hidden p-4' : 'card p-4',
            isMobile && !useFillLayout ? 'overflow-x-hidden' : '',
          ]"
        >
          <template v-if="productName && productDoc">
            <div
              v-if="reviewingIteration != null"
              class="mb-2 text-[11px] text-txt3"
            >
              {{ t('pages.gateApproval.reviewingUpstream', { n: reviewingIteration }) }}
              <span v-if="previewFromArtifactFallback" class="ml-2 text-warn">
                {{ t('pages.gateApproval.reviewingUpstreamFallback') }}
              </span>
            </div>
            <StructuredArtifactView
              :name="productName"
              :doc="productDoc"
              :artifacts="run?.artifacts"
              :run-id="run?.id"
              :run-status="run?.status"
            />
          </template>
          <PlanView v-else-if="planDoc" :doc="planDoc" :artifacts="run?.artifacts" />
          <div v-else class="md max-md:text-[13px] max-md:leading-relaxed" v-html="renderMarkdown(gate.bodyMd)" />
        </div>
      </div>

      <UpstreamRequirementContext
        :artifacts="run?.artifacts"
        :run-id="run?.id"
        :run-status="run?.status"
        :product-name="productName"
        :body-template="bodyTemplate"
      />

      <div
        v-if="!(isProposalSelect && proposalsDoc)"
        class="shrink-0 border-t border-line p-4 safe-area-bottom"
        :class="isMobile ? 'sticky bottom-0 z-10 bg-surface/95 backdrop-blur' : ''"
      >
        <div v-if="gate.form?.length && !shouldHideGateForm" class="mb-3 space-y-3">
          <div v-for="f in gate.form" :key="f.key">
            <label class="label">{{ f.label }} <span v-if="f.required" class="text-err">*</span></label>
            <ParagraphInput
              v-model:text="formText[f.key]"
              v-model:images="formImages[f.key]"
              :text-only="isMobile"
              :placeholder="t('pages.gateApproval.formPlaceholder', { label: f.label })"
            />
          </div>
        </div>
        <div v-if="formError" class="mb-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-xs text-err" role="alert">
          {{ formError }}
        </div>
        <div v-if="resolved && !actionSubmitting" class="rounded-md border border-ok/30 bg-ok/10 px-3 py-2 text-xs text-ok">
          {{ t('pages.gateApproval.submitted', { label: gate.actions.find((a) => a.id === resolved)?.label }) }}
        </div>
        <div
          v-else-if="usesPreviewIssues && previewIssuesLoading"
          class="flex items-center justify-center gap-2 py-2 text-xs text-txt3"
        >
          <Icon name="spinner" :size="14" class="animate-spin text-accent" />
          {{ t('pages.gateApproval.loadingPreviewIssues') }}
        </div>
        <div v-else>
          <p
            v-if="isColdSession"
            class="mb-2 min-w-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]"
            data-testid="gate-cold-help"
          >
            {{ helpColdText }}
          </p>
          <p
            v-else-if="usesPreviewIssues && openPreviewIssueCount === 0"
            class="mb-2 min-w-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]"
          >
            <b class="font-medium text-txt2">{{ t('pages.clarify.confirmFlow') }}</b>
            {{ t('pages.gateApproval.helpApproveDetail') }}
            <span class="mx-1">·</span>
            <b class="font-medium text-txt2">{{ t('pages.reviewComposer.send') }}</b>
            {{ helpReviseDetailNoIssuesText }}
          </p>
          <p
            v-else-if="usesPreviewIssues && openPreviewIssueCount >= 1"
            class="mb-2 min-w-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]"
          >
            <template v-if="canReactRevise">
              <b class="font-medium text-txt2">{{ t('pages.reviewComposer.send') }}</b>
              {{ t('pages.gateApproval.helpReviseWithIssuesDetail') }}
              <span class="mx-1">·</span>
              {{ t('pages.reviewComposer.openIssuesConfirmHint') }}
            </template>
            <template v-else>{{ helpReviseWithIssuesText }}</template>
          </p>
          <p v-else-if="canEditProducts" class="mb-2 min-w-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]">
            <b class="font-medium text-txt2">{{ t('pages.clarify.confirmFlow') }}</b>
            {{ t('pages.gateApproval.helpApproveDetail') }}
            <span class="mx-1">·</span>
            <b class="font-medium text-txt2">{{ t('pages.reviewComposer.send') }}</b>
            {{ t('pages.gateApproval.helpReviseDetail') }}
          </p>
          <GateApprovalComposer v-if="canReactRevise" class="mb-3" />
          <GateApprovalColdActions v-else layout="desktop" />
        </div>
      </div>
  </div>
</template>

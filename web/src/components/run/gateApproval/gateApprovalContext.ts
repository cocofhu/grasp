import { inject, type InjectionKey, type Ref } from 'vue'
import type { AgentTool, ClarifyImage, Gate, ReactAnnotation, Run } from '@/lib/shared/types'
import type { GatePrimaryProductRef } from '@/lib/inbox/gateUpstream'
import type { CommentPin } from '@/lib/inbox/useCommentPins'
import type { PlanDoc } from '../PlanView.vue'

export type GateApprovalProductEditorExpose = {
  isDirty: { value: boolean }
  discard: () => void
}

export type GateApprovalFeedbackChatExpose = {
  flushDraft?: () => Promise<boolean>
  recordIssue?: (payload: {
    body: string
    images?: ClarifyImage[]
    selector?: string
  }) => Promise<unknown>
  reload?: () => Promise<void>
}

/**
 * Reactive gate-approval UI bag for subviews.
 * Provided via reactive() so nested refs/computeds auto-unwrap when read as s.xxx.
 */
export type GateApprovalState = {
  gate: Gate
  run: Run | undefined
  isMobile: boolean
  t: (key: string, params?: Record<string, unknown>) => string

  formText: Record<string, string>
  formImages: Record<string, ClarifyImage[]>
  resolved: string | null
  formError: string | null
  actionSubmitting: boolean
  productDirty: boolean
  savedProductContent: Record<string, string>
  savedProductMeta: Record<string, { etag?: string; updatedAt?: string; sizeBytes?: number }>

  primaryProducts: GatePrimaryProductRef[]
  excludedProduces: string[]
  productName: string | null
  canEditProducts: boolean
  isVisualBody: boolean
  bodyTemplate: string
  usesPreviewIssues: boolean
  openPreviewIssueCount: number
  previewIssuesLoading: boolean
  previewIssuesError: string | null
  pickedSelector: string
  pickedElementImage: ClarifyImage | null
  commentPins: CommentPin[]
  commentPinBadges: {
    id: string
    seq: number
    bounds?: { left: number; top: number; width: number; height: number }
    active?: boolean
  }[]
  commentPinSelectedId: string | null
  commentArtifactCommitted: boolean
  commentArtifactWriting: boolean
  commentArtifactWriteError: string | null
  annotateDraft: {
    selector: string
    imageDataUrl?: string
    screenshotMissing?: boolean
    initialComment?: string
    bounds?: { left: number; top: number; width: number; height: number } | null
    currentText?: string
    editingId?: string | null
    style?: { color?: string; fontSize?: string; fontWeight?: string; fontFamily?: string; lineHeight?: string } | null
  } | null

  planDoc: PlanDoc | null
  planLoading: boolean
  productDoc: unknown
  productHtml: string
  productLoading: boolean
  productLoadError: string | null
  productHasSavedContent: boolean

  reviewingIteration: number | null
  previewFromArtifactFallback: boolean
  shouldFillPreview: boolean
  shouldFitStructured: boolean
  useFillLayout: boolean
  useUnifiedPreviewBudget: boolean
  contentFitChromeOffsetPx: number
  CONTENT_FIT_PREVIEW_MAX_VH: number
  REVIEW_SHELL_WIDTH_KEY_APPROVAL: string

  shouldHideGateForm: boolean
  footerActions: NonNullable<Gate['actions']>
  isColdSession: boolean
  helpColdText: string
  helpReviseDetailNoIssuesText: string
  helpReviseWithIssuesText: string
  canReactRevise: boolean
  canRecordIssue: boolean
  canSubmitReact: boolean
  showHotPass: boolean
  showHotReject: boolean
  hotRejectAllowEmpty: boolean
  composerRejectLabel: string
  composerPassDisabled: boolean
  passAction: NonNullable<Gate['actions']>[number] | undefined

  reactText: string
  reactImages: ClarifyImage[]
  reactAnnotations: ReactAnnotation[]
  reactSending: boolean
  reactError: string | null
  reactQueued: { id?: string; text: string; images: ClarifyImage[]; annotations: ReactAnnotation[] }[]
  reactQueueNotice: string | null
  reactQueueToast: string | null
  cancelReactQueuedItem: (index: number) => void | Promise<void>
  reorderReactQueuedItems: (fromIndex: number, toIndex: number) => void | Promise<void>
  editReactQueuedItem: (index: number) => void | Promise<void>
  reactThinking: boolean
  reactStreamText: string
  reactStreamThought: string
  reactStreamTools: AgentTool[]
  reactInterrupted: boolean
  reactStreamCompletedAt: string | null

  isActionDisabled: (actionId: string) => boolean
  actionPendingLabel: (id: string) => string
  actionButtonTitle: (actionId: string) => string
  onProductSaved: (payload: {
    name: string
    content: string
    etag?: string
    updatedAt?: string
    sizeBytes?: number
  }) => void
  onProductRefresh: (name: string) => void | Promise<void>
  retryLoadProduct: () => void
  onHtmlPreviewPick: (payload: {
    selector: string
    tagName: string
    imageDataUrl: string
    bounds?: { left: number; top: number; width: number; height: number }
    currentText?: string
    style?: { color?: string; fontSize?: string; fontWeight?: string; fontFamily?: string; lineHeight?: string }
  }) => void
  onAppPreviewPick: (payload: {
    selector: string
    tagName: string
    outerHTML: string
    url?: string
  }) => void
  clearHtmlPreviewPick: () => void
  onAnnotateSave: (comment: string) => void
  onAnnotateSendChat: (comment: string) => void
  onAnnotateClose: () => void
  onCommentPinSelect: (pinId: string) => void
  onCommentPinDelete: (pinId: string) => void
  onWriteCommentArtifact: () => void | Promise<void>
  loadPreviewIssues: () => void | Promise<void>
  choose: (id: string) => void
  recordFeedbackIssue: () => void | Promise<void>
  sendHotReject: () => void | Promise<void>
  onComposerPass: () => void
  onComposerReject: () => void
  cancelReactRevise: () => void | Promise<void>
  onSidebarAction: (id: string) => void | Promise<void>
  renderMarkdown: (md: string) => string
}

export type GateApprovalContext = {
  s: GateApprovalState
  productEditorRef: Ref<GateApprovalProductEditorExpose | null>
  feedbackChatRef: Ref<GateApprovalFeedbackChatExpose | null>
  gateStageEl: Ref<HTMLElement | null>
}

export const gateApprovalKey: InjectionKey<GateApprovalContext> = Symbol('gateApproval')

export function useGateApprovalCtx(): GateApprovalContext {
  const ctx = inject(gateApprovalKey)
  if (!ctx) throw new Error('GateApproval context missing')
  return ctx
}

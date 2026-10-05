// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Gate, Run } from '@/lib/shared/types'

const isMobile = ref(false)

const mocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  listGatePrimaryArtifacts: vi.fn(),
  listPreviewIssues: vi.fn(),
  createPreviewIssue: vi.fn(),
  saveAnnotationArtifact: vi.fn(),
  gateReactRevise: vi.fn(),
  gateReactCancel: vi.fn(),
  gateReactQueueRemove: vi.fn(),
  gateReactQueueReorder: vi.fn(),
  toastSuccess: vi.fn(),
  toastWarn: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    warn: mocks.toastWarn,
    error: mocks.toastError,
    info: vi.fn(),
  }),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      artifactContent: mocks.artifactContent,
      listGatePrimaryArtifacts: mocks.listGatePrimaryArtifacts,
      listPreviewIssues: mocks.listPreviewIssues,
      createPreviewIssue: mocks.createPreviewIssue,
      saveAnnotationArtifact: mocks.saveAnnotationArtifact,
      gateReactRevise: mocks.gateReactRevise,
      gateReactCancel: mocks.gateReactCancel,
      gateReactQueueRemove: mocks.gateReactQueueRemove,
      gateReactQueueReorder: mocks.gateReactQueueReorder,
    },
  }
})

import { useGateApproval, type GateApprovalProps } from './useGateApproval'

const gate = (over: Record<string, unknown> = {}): Gate =>
  ({
    runId: 'run-1',
    nodeId: 'gate-1',
    iteration: 2,
    type: 'gate',
    title: 'Review',
    bodyMd: 'Please review',
    actions: [
      { id: 'pass', label: 'Pass' },
      { id: 'revise', label: 'Revise', requireForm: true },
    ],
    form: [{ key: 'note', label: 'Note', required: false }],
    reactSessionAlive: true,
    reactUpstreamNodeId: 'producer',
    ...over,
  }) as unknown as Gate

const run = (over: Record<string, unknown> = {}): Run =>
  ({
    id: 'run-1',
    status: 'waiting_human',
    nodes: [
      { id: 'gate-1', type: 'human_gate', config: {} },
      { id: 'producer', type: 'output', config: {} },
    ],
    artifacts: [],
    nodeExecutions: {},
    ...over,
  }) as unknown as Run

/** Gate whose primary product is a page.html (HtmlPreview + PreviewIssue path). */
function pageGateRun(): Run {
  mocks.listGatePrimaryArtifacts.mockResolvedValue({
    items: [{ name: 'page.html', kind: 'html', readonly: false, nodeId: 'producer', outputKey: 'page' }],
  })
  return run({
    nodes: [
      { id: 'gate-1', type: 'human_gate', config: { body_template: '{{nodes.producer.outputs.page}}' } },
      { id: 'producer', type: 'output', config: {} },
    ],
    artifacts: [{ id: 'page-art', name: 'page.html', sizeBytes: 10 }],
  })
}

function withApproval(over: Partial<GateApprovalProps> = {}) {
  let approval!: ReturnType<typeof useGateApproval>
  const emit = vi.fn()
  const props = reactive({
    gate: gate(),
    run: run(),
    compact: false,
    fillPreview: false,
    unifiedPreviewBudget: false,
    mobileFillRemaining: false,
    submitError: null,
    shareLink: null,
    ...over,
  }) as GateApprovalProps
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      approval = useGateApproval(props, emit as never)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { approval, app, emit, props }
}

describe('useGateApproval actions', () => {
  beforeEach(() => {
    localStorage.clear()
    isMobile.value = false
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()
    mocks.artifactContent.mockResolvedValue({ content: '', etag: 'v1' })
    mocks.listGatePrimaryArtifacts.mockResolvedValue({ items: [] })
    mocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    mocks.createPreviewIssue.mockResolvedValue({ id: 'issue-1' })
    mocks.saveAnnotationArtifact.mockResolvedValue({})
    mocks.gateReactRevise.mockResolvedValue({})
    mocks.gateReactCancel.mockResolvedValue({})
    mocks.gateReactQueueRemove.mockResolvedValue({})
    mocks.gateReactQueueReorder.mockResolvedValue({})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('validates forms, confirms dirty products and emits normalized submissions', async () => {
    const { approval, app, emit, props } = withApproval()
    await flushPromises()

    expect(approval.formSchemaKey(props.gate.form)).toContain('note')
    expect(approval.validate('revise')).toBeTruthy()
    approval.choose('revise')
    expect(emit).not.toHaveBeenCalled()

    approval.formText.value.note = ' explain '
    approval.formImages.value.note = [{ data: 'abc', mimeType: 'image/png' }]
    expect(approval.buildFormPayload().note).toBeTruthy()
    approval.productDirty.value = true
    approval.productEditorRef.value = { isDirty: ref(true), discard: vi.fn() }
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    approval.choose('revise')
    expect(emit).not.toHaveBeenCalled()

    confirm.mockReturnValue(true)
    approval.choose('revise')
    expect(approval.productEditorRef.value.discard).toHaveBeenCalled()
    expect(emit).toHaveBeenCalledWith('resolve', 'revise', expect.objectContaining({ note: expect.anything() }))
    expect(approval.isActionDisabled('pass')).toBe(true)
    expect(approval.actionPendingLabel('pass')).toBeTruthy()
    expect(approval.actionPendingLabel('revise')).toBeTruthy()
    expect(approval.actionButtonTitle('pass')).toBe('')

    props.submitError = 'backend rejected'
    await nextTick()
    expect(approval.resolved.value).toBeNull()
    expect(approval.formError.value).toBe('backend rejected')

    props.gate = gate({ form: [{ key: 'required', label: 'Required', required: true }] })
    await nextTick()
    expect(approval.formText.value).toEqual({ required: '' })
    expect(approval.validate('pass')).toBeTruthy()
    app.unmount()
  })

  it('discards an unsent unified draft when approving', async () => {
    const { approval, app, emit } = withApproval()
    await flushPromises()
    approval.reactText.value = 'unsent'
    approval.reactImages.value = [{ data: 'x', mimeType: 'image/png' }]
    approval.reactAnnotations.value = [{ selector: '#save' }]
    approval.pickedSelector.value = '#save'

    approval.choose('pass')
    expect(approval.reactText.value).toBe('')
    expect(approval.reactImages.value).toEqual([])
    expect(approval.reactAnnotations.value).toEqual([])
    expect(approval.pickedSelector.value).toBe('')
    expect(emit).toHaveBeenCalledWith('resolve', 'pass', expect.any(Object))
    app.unmount()
  })

  it('projects queue, turn and ACP frames into the hot-revise state', async () => {
    const { approval, app } = withApproval()
    await flushPromises()

    expect(approval.applyAcpEvents([{ kind: 'message', text: 'early' }])).toBe(false)
    approval.reactQueued.value = [{ id: 'q1', text: 'one', images: [], annotations: [] }]
    approval.applyReviewFrame({ event: 'turn_begin', nodeId: 'producer' })
    expect(approval.reactInFlight.value).toBe(true)
    expect(approval.applyAcpEvents([
      { kind: 'thought', text: 'thinking' },
      { kind: 'message', text: 'answer' },
      { kind: 'tool_call', title: 'read_file', status: 'completed', text: 'not-a-message' },
    ])).toBe(true)
    expect(approval.reactStreamThought.value).toBe('thinking')
    expect(approval.reactStreamText.value).toBe('answer')
    expect(approval.reactStreamTools.value).toEqual([{ title: 'read_file', status: 'completed' }])

    approval.applyReviewFrame({ event: 'turn_done', nodeId: 'producer' })
    expect(approval.reactStreamCompletedAt.value).toBeTruthy()
    approval.applyReviewFrame({ event: 'turn_begin', nodeId: 'producer' })
    expect(approval.reactStreamTools.value).toEqual([])
    approval.applyReviewFrame({ event: 'turn_done', nodeId: 'producer', interrupted: true })
    expect(approval.reactInterrupted.value).toBe(true)

    approval.applyReviewFrame({ event: 'error', nodeId: 'producer', message: 'model failed' })
    expect(approval.reactError.value).toBe('model failed')
    approval.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'producer',
      waiting: 2,
      busy: true,
      activeItem: { id: 'active', text: 'active' },
      items: [
        { id: 'q2', text: 'second', images: [{ data: 'i', mimeType: 'image/png' }] },
        { id: 'q3', text: 'third', annotations: [{ jsonPath: '$.name' }] },
      ],
    })
    expect(approval.reactQueued.value.map((q) => q.id)).toEqual(['q2', 'q3'])
    expect(approval.reactThinking.value).toBe(true)

    approval.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'producer',
      waiting: 0,
      busy: false,
      activeItem: null,
    })
    expect(approval.reactQueued.value).toEqual([])
    expect(approval.reactThinking.value).toBe(false)

    approval.reactThinking.value = true
    approval.applyReviewFrame({ event: 'turn_begin', nodeId: 'other' })
    expect(approval.reactInFlight.value).toBe(false)
    expect(approval.applyAcpEvents([])).toBe(true)
    app.unmount()
  })

  it('cancels, reorders and edits queued revisions, including API failures', async () => {
    const { approval, app } = withApproval()
    await flushPromises()
    approval.reactQueued.value = [
      { id: 'a', text: 'alpha', images: [], annotations: [] },
      { id: 'b', text: 'beta', images: [], annotations: [{ selector: '#b' }] },
    ]
    approval.reactThinking.value = true

    await approval.cancelReactQueuedItem(-1)
    await approval.cancelReactQueuedItem(0)
    expect(mocks.gateReactQueueRemove).toHaveBeenCalledWith('run-1', 'gate-1', 'a')
    expect(approval.reactQueueToast.value).toBeTruthy()

    approval.reactQueued.value = [
      { id: 'a', text: 'alpha', images: [], annotations: [] },
      { id: 'b', text: 'beta', images: [], annotations: [] },
    ]
    await approval.reorderReactQueuedItems(0, 1)
    expect(approval.reactQueued.value.map((q) => q.id)).toEqual(['b', 'a'])
    expect(mocks.gateReactQueueReorder).toHaveBeenCalledWith('run-1', 'gate-1', ['b', 'a'])
    await approval.reorderReactQueuedItems(0, 0)
    await approval.reorderReactQueuedItems(9, 0)

    mocks.gateReactQueueReorder.mockRejectedValueOnce(new Error('stale queue'))
    await approval.reorderReactQueuedItems(0, 1)
    expect(approval.reactError.value).toBe('stale queue')

    mocks.gateReactQueueRemove.mockRejectedValueOnce(new Error('already active'))
    await approval.editReactQueuedItem(0)
    expect(approval.reactText.value).toBeTruthy()
    expect(approval.reactError.value).toBe('already active')
    await approval.editReactQueuedItem(-1)
    app.unmount()
  })

  it('sends ordinary revisions and restores queue state after rejection', async () => {
    const { approval, app, emit } = withApproval()
    await flushPromises()
    approval.reactText.value = 'make it clearer'
    approval.reactImages.value = [{ data: 'img', mimeType: 'image/png' }]
    approval.reactAnnotations.value = [{ jsonPath: '$.title', label: 'Title' }]
    await approval.sendReactRevise()
    expect(mocks.gateReactRevise).toHaveBeenCalledWith(
      'run-1',
      'gate-1',
      'make it clearer',
      expect.any(Array),
      expect.any(Array),
    )
    expect(emit).toHaveBeenCalledWith('react-revised')
    expect(approval.reactText.value).toBe('')

    approval.reactText.value = 'retry me'
    mocks.gateReactRevise.mockRejectedValueOnce(new Error('session ended'))
    await approval.sendReactRevise()
    expect(approval.reactError.value).toBe('session ended')
    expect(approval.reactQueued.value.map((q) => q.text)).toEqual(['make it clearer'])

    await approval.cancelReactRevise()
    expect(mocks.gateReactCancel).toHaveBeenCalledWith('run-1', 'gate-1')
    mocks.gateReactCancel.mockRejectedValueOnce(new Error('cancel failed'))
    await approval.cancelReactRevise()
    expect(approval.reactError.value).toBe('cancel failed')
    app.unmount()
  })

  it('handles preview picks, duplicate annotations and preview issue failures', async () => {
    const { approval, app } = withApproval({
      gate: gate(),
      run: pageGateRun(),
    })
    await flushPromises()
    expect(approval.usesPreviewIssues.value).toBe(true)

    approval.onAppPreviewPick({ selector: '#buy', tagName: 'BUTTON', outerHTML: '<button />', url: ' https://app/ ' })
    expect(approval.pickedSelector.value).toBe('#buy')
    expect(approval.reactAnnotations.value[0]?.url).toBe('https://app/')
    approval.onAppPreviewPick({ selector: '#buy', tagName: 'BUTTON', outerHTML: '<button />', url: 'https://app/' })
    expect(mocks.toastWarn).toHaveBeenCalled()

    approval.onHtmlPreviewPick({
      selector: '#hero',
      tagName: 'DIV',
      imageDataUrl: 'not-a-data-url',
      bounds: { left: 1, top: 2, width: 3, height: 4 },
      currentText: 'Hero',
    })
    expect(mocks.toastWarn).toHaveBeenCalled()
    approval.clearHtmlPreviewPick()
    expect(approval.pickedSelector.value).toBe('')

    mocks.listPreviewIssues.mockRejectedValueOnce(new Error('offline'))
    await approval.loadPreviewIssues()
    expect(approval.previewIssuesError.value).toBe('load failed')
    expect(mocks.toastError).toHaveBeenCalled()

    approval.reactText.value = 'broken'
    mocks.createPreviewIssue.mockRejectedValueOnce(new Error('write failed'))
    expect(await approval.flushFeedbackDraft()).toBe(false)
    expect(approval.reactError.value).toBe('write failed')
    app.unmount()
  })

  it('records and sends preview feedback through the mounted chat contract', async () => {
    const { approval, app, emit } = withApproval({
      run: pageGateRun(),
    })
    await flushPromises()
    const flush = vi.fn(async () => true)
    const send = vi.fn(async () => true)
    const reload = vi.fn(async () => {})
    approval.feedbackChatRef.value = { flush, send, reload, clearDraft: vi.fn() }

    approval.reactText.value = 'visual issue'
    await approval.recordFeedbackIssue()
    expect(flush).toHaveBeenCalled()

    approval.reactText.value = ''
    approval.reactAnnotations.value = [{ selector: '#x', label: 'Button' }]
    expect(approval.annotationHistoryBody(approval.reactAnnotations.value)).toBeTruthy()
    await approval.sendHotReject()
    expect(send).toHaveBeenCalled()
    expect(mocks.gateReactRevise).toHaveBeenCalled()
    expect(emit).toHaveBeenCalledWith('react-revised')

    approval.previewIssues.value = [{ id: 'i1', status: 'open' } as never]
    approval.reactText.value = ''
    await approval.sendHotReject()
    expect(mocks.gateReactRevise).toHaveBeenCalledTimes(2)

    approval.actionSubmitting.value = false
    approval.resolved.value = null
    await approval.onSidebarAction('revise')
    expect(emit).toHaveBeenCalledWith('resolve', 'revise', expect.any(Object))
    app.unmount()
  })

  it('loads proposal and plan artifacts and tolerates malformed content', async () => {
    const proposalRun = run({
      nodes: [{ id: 'gate-1', type: 'proposal_select', config: { from: 'ideas.json' } }],
      artifacts: [
        { id: 'ideas', name: 'ideas.json' },
        { id: 'plan', name: 'plan.json' },
      ],
    })
    mocks.artifactContent.mockImplementation(async (id: string) =>
      id === 'ideas'
        ? { content: JSON.stringify({ proposals: [{ id: 'p1', title: 'One' }] }) }
        : { content: JSON.stringify({ goals: [{ id: 'g1' }] }) },
    )
    const { approval, app, props } = withApproval({
      gate: gate({ bodyMd: '- [ ] `g1` goal', actions: [{ id: 'p1', label: 'One' }] }),
      run: proposalRun,
    })
    await flushPromises()
    expect(approval.proposalsDoc.value?.proposals).toHaveLength(1)
    expect(approval.planDoc.value?.goals).toHaveLength(1)

    mocks.artifactContent.mockRejectedValue(new Error('bad artifact'))
    await approval.loadProposals()
    await approval.loadPlan()
    expect(approval.proposalsDoc.value).toBeNull()
    expect(approval.planDoc.value).toBeNull()

    props.gate = gate({ bodyMd: 'plain' })
    await nextTick()
    await approval.loadPlan()
    expect(approval.planDoc.value).toBeNull()
    app.unmount()
  })

  it('loads structured and visual products, refreshes saved content and reports failures', async () => {
    const productRun = run({
      nodes: [
        {
          id: 'gate-1',
          type: 'human_gate',
          config: { body_template: '{{nodes.producer.outputs.page}}' },
        },
        { id: 'producer', type: 'output', config: {} },
      ],
      artifacts: [{ id: 'page-art', name: 'page.html', sizeBytes: 10 }],
      nodeExecutions: {
        producer: [{ iteration: 1, status: 'completed', outputs: { page: '<p>snapshot</p>' } }],
      },
    })
    mocks.listGatePrimaryArtifacts.mockResolvedValue({
      items: [{ name: 'page.html', kind: 'html', readonly: false, nodeId: 'producer', outputKey: 'page' }],
    })
    mocks.artifactContent.mockResolvedValue({
      content: '<main>store</main>',
      etag: 'v2',
      updatedAt: 'now',
      sizeBytes: 18,
    })
    const { approval, app } = withApproval({ run: productRun, fillPreview: true })
    await flushPromises()
    expect(approval.productHtml.value).toBe('<main>store</main>')
    expect(approval.shouldFillPreview.value).toBe(true)
    expect(approval.buildProductLoadFingerprint()).toContain('page.html')
    expect(approval.hashContentFingerprint('abc')).toMatch(/^3-/)
    expect(approval.upstreamExecutionsFingerprint(productRun.nodeExecutions)).toContain('producer')
    expect(approval.upstreamOutputs().outputs?.page).toContain('snapshot')

    approval.onProductSaved({ name: 'page.html', content: '<h1>saved</h1>', etag: 'v3' })
    expect(approval.productHtml.value).toContain('saved')
    mocks.artifactContent.mockResolvedValueOnce({ content: '<h2>remote</h2>' })
    await approval.onProductRefresh('page.html')
    expect(approval.productHtml.value).toContain('remote')
    await approval.onProductRefresh('missing')

    mocks.artifactContent.mockRejectedValueOnce(new Error('artifact offline'))
    await approval.onProductRefresh('page.html')
    expect(approval.productLoadError.value).toContain('artifact offline')
    approval.retryLoadProduct()
    await flushPromises()
    app.unmount()
  })

  it('normalizes API primary products and skips requests for ineligible runs', async () => {
    const { approval, app, props } = withApproval({ run: run({ status: 'running' }) })
    await flushPromises()
    expect(approval.normalizeApiPrimaryItem({ name: 'data.json', kind: '' }).name).toBe('data.json')
    mocks.listGatePrimaryArtifacts.mockClear()
    await approval.loadPrimaryProductsFromApi()
    expect(mocks.listGatePrimaryArtifacts).not.toHaveBeenCalled()
    expect(approval.primaryProductsHydrated.value).toBe(true)

    props.run = run()
    mocks.listGatePrimaryArtifacts.mockRejectedValueOnce(new Error('old server'))
    await approval.loadPrimaryProductsFromApi()
    expect(approval.apiPrimaryProducts.value).toBeNull()
    app.unmount()
  })

  it('persists, writes, edits and deletes visual comment pins', async () => {
    mocks.listGatePrimaryArtifacts.mockResolvedValue({
      items: [{ name: 'page.html', kind: 'html', nodeId: 'producer', outputKey: 'page' }],
    })
    mocks.artifactContent.mockResolvedValue({ content: '<button>Buy</button>' })
    const visualRun = run({
      nodes: [
        { id: 'gate-1', type: 'human_gate', config: { body_template: '{{nodes.producer.outputs.page}}' } },
        { id: 'producer', type: 'output', config: {} },
      ],
      artifacts: [{ id: 'page', name: 'page.html' }],
      nodeExecutions: {
        producer: [{ iteration: 1, status: 'completed', outputs: { page: '<button>Buy</button>' } }],
      },
    })
    const { approval, app } = withApproval({ run: visualRun })
    await flushPromises()

    approval.onHtmlPreviewPick({
      selector: '#buy',
      tagName: 'BUTTON',
      imageDataUrl: 'data:image/png;base64,YQ==',
      bounds: { left: 1, top: 2, width: 30, height: 20 },
      currentText: 'Buy',
    })
    expect(approval.annotateDraft.value?.selector).toBe('#buy')
    approval.onAnnotateSave('Increase contrast')
    expect(approval.commentPins.value).toHaveLength(1)
    expect(mocks.toastSuccess).toHaveBeenCalled()

    const pinId = approval.commentPins.value[0]!.id
    approval.onCommentPinSelect(pinId)
    expect(approval.annotateDraft.value?.editingId).toBe(pinId)
    approval.onAnnotateSendChat('Use blue')
    expect(approval.reactText.value).toContain('Use blue')

    await approval.onWriteCommentArtifact()
    expect(mocks.saveAnnotationArtifact).toHaveBeenCalled()
    expect(approval.commentArtifactCommitted.value).toBe(true)

    approval.onCommentPinDelete(pinId)
    await flushPromises()
    expect(approval.commentPins.value).toEqual([])
    approval.onCommentPinDelete('missing')
    approval.onCommentPinSelect('missing')
    approval.onAnnotateClose()

    mocks.saveAnnotationArtifact.mockRejectedValueOnce(new Error('storage denied'))
    approval.onHtmlPreviewPick({
      selector: '#again',
      tagName: 'DIV',
      imageDataUrl: 'data:image/png;base64,Yg==',
    })
    approval.onAnnotateSave('Again')
    await approval.onWriteCommentArtifact()
    expect(approval.commentArtifactWriteError.value).toBe('storage denied')
    expect(mocks.toastError).toHaveBeenCalled()
    app.unmount()
  })

  it('stashes a local composer draft before editing a queued item', async () => {
    const { approval, app } = withApproval({ run: undefined })
    await flushPromises()
    approval.reactQueued.value = [
      {
        text: 'queued target',
        images: [{ data: 'old', mimeType: 'image/png' }],
        annotations: [{ selector: '#old' }],
      },
    ]
    approval.reactText.value = 'new draft'
    approval.reactImages.value = [{ data: 'new', mimeType: 'image/png' }]
    approval.reactAnnotations.value = [{ selector: '#new' }]
    await approval.editReactQueuedItem(0)
    expect(approval.reactText.value).toBe('queued target')
    expect(approval.reactImages.value[0]?.data).toBe('old')
    expect(approval.reactAnnotations.value[0]?.selector).toBe('#old')
    expect(approval.reactQueued.value.some((q) => q.text === 'new draft')).toBe(true)
    app.unmount()
  })

  it('keeps a preview draft when history succeeds but hot revise fails', async () => {
    const { approval, app } = withApproval({
      run: pageGateRun(),
    })
    await flushPromises()
    approval.feedbackChatRef.value = {
      flush: vi.fn(async () => true),
      send: vi.fn(async () => true),
      reload: vi.fn(async () => {}),
      clearDraft: vi.fn(),
    }
    approval.reactText.value = 'keep this'
    approval.reactImages.value = [{ data: 'img', mimeType: 'image/png' }]
    approval.reactAnnotations.value = [{ selector: '#x' }]
    mocks.gateReactRevise.mockRejectedValueOnce(new Error('producer stopped'))
    await approval.sendHotReject()
    expect(approval.hotRejectHistorySynced.value).toBe(true)
    expect(approval.reactText.value).toBe('keep this')
    expect(approval.reactImages.value).toHaveLength(1)
    expect(approval.reactAnnotations.value).toHaveLength(1)
    expect(approval.reactError.value).toBeTruthy()

    // Retry skips the already-synchronized history and succeeds.
    await approval.sendHotReject()
    expect(approval.reactText.value).toBe('')
    app.unmount()
  })

  it('handles queue authority transitions with completed and empty rails', async () => {
    const { approval, app } = withApproval()
    await flushPromises()
    approval.reactInFlight.value = true
    approval.reactThinking.value = true
    approval.reactStreamText.value = 'completed answer'
    approval.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'producer',
      waiting: 1,
      busy: false,
      items: [{ id: 'server-1', text: 'next' }],
    })
    expect(approval.reactInFlight.value).toBe(false)
    expect(approval.reactStreamCompletedAt.value).toBeTruthy()
    expect(approval.reactQueued.value[0]?.id).toBe('server-1')

    approval.reactInFlight.value = true
    approval.reactStreamText.value = ''
    approval.reactStreamThought.value = ''
    approval.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'producer',
      waiting: 1,
      busy: false,
      items: [{ text: 'anonymous' }],
    })
    expect(approval.reactInFlight.value).toBe(false)
    expect(approval.reactStreamCompletedAt.value).toBeNull()

    approval.reactQueued.value.push({ text: 'ghost', images: [], annotations: [] })
    approval.reactInFlight.value = true
    approval.reactStreamThought.value = 'thought'
    approval.forceReactAuthoritativeIdle()
    expect(approval.reactQueued.value).toEqual([])
    expect(approval.reactStreamCompletedAt.value).toBeTruthy()

    approval.reactQueued.value = [
      { text: 'ghost', images: [], annotations: [] },
      { id: 'real', text: 'real', images: [], annotations: [] },
    ]
    approval.reactInFlight.value = true
    approval.settleReactAfterTurnEnd()
    expect(approval.reactQueued.value.map((q) => q.id)).toEqual(['real'])
    app.unmount()
  })

  it('uses fallback preview issue APIs and composer action routing', async () => {
    const { approval, app, emit } = withApproval({
      run: pageGateRun(),
    })
    await flushPromises()
    approval.feedbackChatRef.value = null
    approval.reactText.value = 'fallback issue'
    approval.reactImages.value = [{ data: 'img', mimeType: 'image/png', name: 'shot.png' }]
    approval.pickedElementImage.value = { data: 'picked', mimeType: 'image/jpeg' }
    approval.pickedSelector.value = '#picked'
    expect(approval.collectUnifiedIssueImages()).toHaveLength(2)
    expect(await approval.flushFeedbackDraft()).toBe(true)
    expect(mocks.createPreviewIssue).toHaveBeenCalledWith(
      'run-1',
      'gate-1',
      'fallback issue',
      '#picked',
      0,
      expect.any(Array),
    )

    approval.reactAnnotations.value = [{ selector: '#only', label: 'Only annotation' }]
    expect(await approval.syncHotRejectHistory(approval.reactAnnotations.value)).toBe(true)
    expect(approval.hotRejectHistorySynced.value).toBe(true)

    approval.hotRejectHistorySynced.value = false
    mocks.createPreviewIssue.mockRejectedValueOnce(new Error('history denied'))
    expect(await approval.syncHotRejectHistory(approval.reactAnnotations.value)).toBe(false)
    expect(approval.reactError.value).toBe('history denied')

    approval.previewIssues.value = []
    approval.actionSubmitting.value = false
    approval.resolved.value = null
    approval.onComposerPass()
    expect(emit).toHaveBeenCalledWith('resolve', 'pass', expect.any(Object))

    approval.resolved.value = null
    approval.actionSubmitting.value = false
    approval.reactText.value = 'revise'
    approval.onComposerReject()
    await flushPromises()
    expect(mocks.gateReactRevise).toHaveBeenCalled()
    app.unmount()
  })

  it('evaluates fill layouts, action help and product guard branches', async () => {
    isMobile.value = true
    const productRun = run({
      nodes: [
        { id: 'gate-1', type: 'human_gate', config: { body_template: '{{nodes.producer.outputs.page}}' } },
        { id: 'producer', type: 'output', config: {} },
      ],
      artifacts: [],
      nodeExecutions: {},
    })
    const { approval, app, props } = withApproval({
      run: productRun,
      fillPreview: true,
      mobileFillRemaining: true,
      unifiedPreviewBudget: true,
    })
    await flushPromises()
    expect(approval.useMobileFillRemaining.value).toBe(true)
    expect(approval.useReviewShellLayout.value).toBe(false)
    expect(approval.useUnifiedPreviewBudget.value).toBe(false)
    expect(approval.helpColdText.value).toBeTruthy()
    expect(approval.helpReviseDetailNoIssuesText.value).toBeTruthy()
    expect(approval.helpReviseWithIssuesText.value).toBeTruthy()
    expect(approval.contentFitChromeOffsetPx.value).toBe(0)
    expect(approval.upstreamOutputs()).toEqual({ outputs: null, pointerMiss: false })

    approval.apiPrimaryProducts.value = [{ name: 'missing.json', kind: 'structured' } as never]
    expect(await approval.loadOneProductContent(approval.apiPrimaryProducts.value[0]!)).toBe('')
    await approval.loadProduct({ force: true })
    expect(approval.productDoc.value).toBeNull()

    props.gate = gate({ actions: [{ id: 'p1', label: 'Proposal' }] })
    await nextTick()
    expect(approval.isProposalSelect.value).toBe(true)
    expect(approval.composerPassDisabled.value).toBe(true)
    approval.onComposerPass()
    approval.onComposerReject()
    app.unmount()
  })

  it('requeues an existing draft before editing a server-backed queue item', async () => {
    const { approval, app } = withApproval()
    await flushPromises()
    approval.reactQueued.value = [
      {
        id: 'target',
        text: 'edit target',
        images: [{ data: 'old', mimeType: 'image/png' }],
        annotations: [{ selector: '#old' }],
      },
    ]
    approval.reactText.value = 'stash first'
    approval.reactImages.value = [{ data: 'new', mimeType: 'image/png' }]
    approval.reactAnnotations.value = [{ selector: '#new' }]
    expect(approval.isEditing.value).toBe(true)
    await approval.editReactQueuedItem(0)
    expect(mocks.gateReactRevise).toHaveBeenCalled()
    expect(mocks.gateReactQueueRemove).toHaveBeenCalledWith('run-1', 'gate-1', 'target')
    expect(approval.reactText.value).toBe('edit target')
    expect(approval.reactAnnotations.value[0]?.selector).toBe('#old')

    approval.reactQueued.value = [{ id: 'wait', text: 'wait', images: [], annotations: [] }]
    approval.reactThinking.value = true
    approval.applyReviewFrame({
      event: 'queue_state',
      nodeId: 'producer',
      waiting: 0,
      busy: true,
      activeItem: { id: 'active', text: 'active' },
    })
    expect(approval.reactQueued.value).toEqual([])
    expect(approval.reactThinking.value).toBe(true)

    approval.clearUnifiedDraft()
    approval.formText.value.note = 'form edit'
    expect(approval.isEditing.value).toBe(true)
    approval.formText.value.note = ''
    approval.formImages.value.note = [{ data: 'x', mimeType: 'image/png' }]
    expect(approval.isEditing.value).toBe(true)
    app.unmount()
  })

  it('updates structured saved products and reports annotation invalidation errors', async () => {
    const { approval, app } = withApproval()
    await flushPromises()
    approval.onProductSaved({ name: 'plan.json', content: '{"name":"new"}' })
    expect(approval.productDoc.value).toEqual({ name: 'new' })
    approval.onProductSaved({ name: 'plan.json', content: '{invalid' })
    expect(approval.productDoc.value).toEqual({ name: 'new' })

    mocks.saveAnnotationArtifact.mockRejectedValueOnce(new Error('invalidate denied'))
    await approval.invalidateServerAnnotationArtifact()
    expect(approval.commentArtifactWriteError.value).toBe('invalidate denied')
    expect(mocks.toastWarn).toHaveBeenCalled()

    const noProposalArtifact = withApproval({
      gate: gate({ actions: [{ id: 'p1', label: 'One' }] }),
      run: run({ nodes: [{ id: 'gate-1', type: 'proposal_select', config: {} }], artifacts: [] }),
    })
    await flushPromises()
    await noProposalArtifact.approval.loadProposals()
    expect(noProposalArtifact.approval.proposalsDoc.value).toBeNull()
    noProposalArtifact.app.unmount()
    app.unmount()
  })
})

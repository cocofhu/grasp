// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyImage, ReactAnnotation, ReactQuestion } from '@/lib/shared/types'

import { useClarifyChat } from './useClarifyChat'

const image = (name = 'shot.png', over: Partial<ClarifyImage> = {}): ClarifyImage => ({
  data: 'aGVsbG8=',
  mimeType: 'image/png',
  name,
  ...over,
})

const annotation = (over: Partial<ReactAnnotation> = {}) =>
  ({
    selector: '#button',
    url: 'http://app.test',
    tagName: 'BUTTON',
    ...over,
  }) as ReactAnnotation

const question = (over: Partial<ReactQuestion> = {}): ReactQuestion =>
  ({
    id: 'q1',
    prompt: '选择环境',
    allowMultiple: false,
    options: [
      { id: 'dev', label: '开发', recommended: true },
      { id: 'prod', label: '生产' },
    ],
    ...over,
  }) as ReactQuestion

function withChat(over: Record<string, unknown> = {}) {
  let chat!: ReturnType<typeof useClarifyChat>
  const emit = vi.fn()
  const props = reactive({
    runId: 'run-1',
    nodeId: 'node-1',
    iteration: 0,
    turns: [],
    done: false,
    active: true,
    reviewMode: false,
    annotateEnabled: false,
    nodeType: 'react',
    confirmError: null,
    ...over,
  })
  const models = {
    draft: ref(''),
    attachments: ref<ClarifyImage[]>([]),
    annotations: ref<ReactAnnotation[]>([]),
  }
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const Comp = defineComponent({
    setup() {
      chat = useClarifyChat(props as never, emit as never, models)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { chat, app, emit, props, models }
}

function scroller(top = 0) {
  const el = document.createElement('div')
  Object.defineProperties(el, {
    scrollTop: { value: top, writable: true },
    scrollHeight: { value: 300, configurable: true },
    clientHeight: { value: 100, configurable: true },
  })
  return el
}

describe('useClarifyChat actions', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('sends composer content with cloned attachments and annotations', async () => {
    const { chat, app, emit, models } = withChat()
    models.draft.value = '  investigate  '
    models.attachments.value = [image()]
    models.annotations.value = [annotation()]
    chat.sendFromComposer()

    expect(emit).toHaveBeenCalledWith(
      'send',
      'investigate',
      expect.arrayContaining([expect.objectContaining({ name: 'shot.png' })]),
      expect.arrayContaining([expect.objectContaining({ selector: '#button' })]),
    )
    expect(chat.queued.value).toHaveLength(1)
    expect(chat.thinking.value).toBe(true)
    expect(models.draft.value).toBe('')
    expect(models.attachments.value).toEqual([])
    expect(models.annotations.value).toEqual([])
    app.unmount()
  })

  it('wraps composer send as skip envelope while questions are open (text + image)', async () => {
    const q = question()
    const { chat, app, emit, models } = withChat({
      turns: [{ role: 'agent', text: 'ask', at: '1', questions: [q] }],
    })
    await nextTick()
    expect(chat.activeQuestions.value).toHaveLength(1)
    // Pre-check recommended (cards may already do this); skip must discard it.
    chat.applyRecommended()
    expect(chat.isSelected('q1', 'dev')).toBe(true)
    models.draft.value = '先按现有集群，文档这次先不动'
    models.attachments.value = [image('架构草图.png')]
    chat.sendFromComposer()

    const skipText = '回答已跳过\n用户回复\n先按现有集群，文档这次先不动'
    expect(emit).toHaveBeenCalledWith(
      'send',
      skipText,
      expect.arrayContaining([expect.objectContaining({ name: '架构草图.png' })]),
      [],
    )
    expect(chat.isSkipReply(skipText)).toBe(true)
    expect(chat.isChoiceReply(skipText)).toBe(false)
    expect(skipText).not.toContain('开发')
    expect(skipText).not.toContain('我的选择')
    expect(chat.sel.value).toEqual({})
    expect(chat.activeQuestions.value).toHaveLength(0)
    expect(chat.latestQuestionAnswered.value).toBe(true)
    app.unmount()
  })

  it('skips with image-only composer send and closes the question card', async () => {
    const q = question()
    const { chat, app, emit, models } = withChat({
      turns: [{ role: 'agent', text: 'ask', at: '1', questions: [q] }],
    })
    await nextTick()
    models.draft.value = ''
    models.attachments.value = [image('paste.png')]
    chat.sendFromComposer()

    expect(emit).toHaveBeenCalledWith(
      'send',
      '回答已跳过\n用户回复\n',
      expect.arrayContaining([expect.objectContaining({ name: 'paste.png' })]),
      [],
    )
    const sentImgs = emit.mock.calls.at(-1)![2] as ClarifyImage[]
    expect(sentImgs.length).toBeGreaterThanOrEqual(1)
    expect(chat.activeQuestions.value).toHaveLength(0)
    app.unmount()
  })

  it('rejects oversized images without skipping or submitting defaults', async () => {
    const q = question()
    const { chat, app, emit, models } = withChat({
      turns: [{ role: 'agent', text: 'ask', at: '1', questions: [q] }],
    })
    await nextTick()
    expect(chat.activeQuestions.value).toHaveLength(1)
    chat.applyRecommended()
    expect(chat.isSelected('q1', 'dev')).toBe(true)
    models.draft.value = 'should not send'
    models.attachments.value = [image('huge.png', { sizeBytes: 51 * 1024 * 1024 })]
    chat.sendFromComposer()
    expect(emit).not.toHaveBeenCalled()
    expect(chat.attachNotice.value).toBeTruthy()
    expect(models.draft.value).toBe('should not send')
    expect(chat.activeQuestions.value).toHaveLength(1)
    expect(chat.isSelected('q1', 'dev')).toBe(true)
    app.unmount()
  })

  it('keeps confirm-selection and adopt-recommendation on choicePrefix path', async () => {
    const q = question()
    const { chat, app, emit } = withChat({
      turns: [{ role: 'agent', text: 'ask', at: '1', questions: [q] }],
    })
    await nextTick()
    chat.submitRecommended()
    const sent = String(emit.mock.calls.at(-1)![1])
    expect(sent.startsWith('我的选择:')).toBe(true)
    expect(sent).toContain('开发')
    expect(chat.isSkipReply(sent)).toBe(false)
    app.unmount()
  })

  it('guards sends and rejects oversized composer attachments', () => {
    const { chat, app, emit, models, props } = withChat()
    chat.sendMessage(' ')
    props.active = false
    chat.sendMessage('inactive')
    props.active = true
    props.done = true
    chat.sendMessage('done')
    expect(emit).not.toHaveBeenCalled()

    props.done = false
    models.draft.value = 'with file'
    models.attachments.value = [image('huge.png', { sizeBytes: 51 * 1024 * 1024 })]
    chat.sendFromComposer()
    expect(chat.attachNotice.value).toBeTruthy()
    expect(models.draft.value).toBe('with file')
    app.unmount()
  })

  it('handles Enter, Shift+Enter and IME composition', () => {
    const { chat, app, emit, models } = withChat()
    models.draft.value = 'hello'
    const enter = new KeyboardEvent('keydown', { key: 'Enter', cancelable: true })
    chat.onComposerKeydown(enter)
    expect(enter.defaultPrevented).toBe(true)
    expect(emit).toHaveBeenCalledTimes(1)

    models.draft.value = 'shift'
    chat.onComposerKeydown(new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true }))
    chat.composing.value = true
    chat.onComposerKeydown(new KeyboardEvent('keydown', { key: 'Enter' }))
    chat.composing.value = false
    chat.onComposerKeydown({ key: 'Enter', isComposing: true, keyCode: 13 } as KeyboardEvent)
    chat.onComposerKeydown({ key: 'Enter', isComposing: false, keyCode: 229 } as KeyboardEvent)
    expect(emit).toHaveBeenCalledTimes(1)
    app.unmount()
  })

  it('cancels, reorders and edits queued items without sharing object references', async () => {
    vi.useFakeTimers()
    const { chat, app, emit, models } = withChat()
    chat.applyQueueState(2, [
      { id: 'a', text: 'first', images: [image('a.png')], annotations: [annotation()] },
      { id: 'b', text: 'second', images: [], annotations: [] },
    ])
    chat.reorderQueuedItems(0, 1)
    expect(chat.queued.value.map((q) => q.id)).toEqual(['b', 'a'])
    expect(emit).toHaveBeenCalledWith('queue-reorder', ['b', 'a'])

    chat.editQueuedItem(1)
    await nextTick()
    expect(models.draft.value).toBe('first')
    expect(models.attachments.value[0]).not.toBe(image('a.png'))
    expect(emit).toHaveBeenCalledWith('queue-remove', 'a', 1)

    chat.cancelQueuedItem(0)
    expect(chat.queued.value).toHaveLength(0)
    expect(chat.queueToast.value).toBeTruthy()
    vi.advanceTimersByTime(2200)
    expect(chat.queueToast.value).toBeNull()
    chat.cancelQueuedItem(-1)
    chat.reorderQueuedItems(0, 0)
    app.unmount()
  })

  it('re-enqueues an existing composer draft before editing a queued item', () => {
    const { chat, app, emit, models } = withChat()
    chat.applyQueueState(1, [{ id: 'server', text: 'server row' }])
    models.draft.value = 'local draft'
    models.annotations.value = [annotation()]
    chat.editQueuedItem(0)
    expect(emit).toHaveBeenCalledWith('send', 'local draft', [], expect.any(Array))
    expect(models.draft.value).toBe('server row')
    expect(chat.queued.value[0]?.text).toBe('local draft')
    app.unmount()
  })

  it('drives structured questions, recommendations and choice summaries', async () => {
    const q1 = question()
    const q2 = question({
      id: 'q2',
      prompt: '选择区域',
      allowMultiple: true,
      options: [
        { id: 'cn', label: '中国' },
        { id: 'us', label: '美国' },
      ],
    })
    const { chat, app, emit } = withChat({
      turns: [{ role: 'agent', text: 'questions', at: '1', questions: [q1, q2] }],
    })
    await nextTick()
    expect(chat.activeQuestions.value).toHaveLength(2)
    chat.applyRecommended()
    expect(chat.isSelected('q1', 'dev')).toBe(true)
    chat.pick(q1, 'prod')
    expect(chat.isSelected('q1', 'prod')).toBe(true)
    chat.nextCard()
    expect(chat.curQuestion.value?.id).toBe('q2')
    chat.prevCard()
    chat.toggle(q2, 'cn')
    chat.toggle(q2, 'us')
    chat.toggleOther(q2)
    chat.other.value.q2 = '其他地区'
    expect(chat.otherAnswered(q2)).toBe(true)
    chat.submitChoices()
    expect(emit).toHaveBeenCalledWith(
      'send',
      expect.stringContaining('选择区域'),
      [],
      [],
    )
    const sent = emit.mock.calls.at(-1)![1]
    expect(chat.parseChoiceSummary(sent)).toEqual(
      expect.arrayContaining([expect.objectContaining({ q: '选择区域' })]),
    )
    expect(chat.parseChoiceSummary('plain')).toBeNull()
    expect(chat.parseChoiceSummary('我的选择:\n- bad')).toBeNull()
    app.unmount()
  })

  it('applies recommended choices and maps selected demo options', async () => {
    const q = question({
      options: [
        { id: 'a', label: 'A', demoHtml: '<b>A</b>' },
        { id: 'b', label: 'B', recommended: true, demoHtml: '<b>B</b>' },
        { id: 'c', label: 'C', demoHtml: '<b>C</b>' },
        { id: 'd', label: 'D', demoHtml: '<b>D</b>' },
      ],
    })
    const { chat, app, emit } = withChat({
      turns: [{ role: 'agent', text: 'q', at: '1', questions: [q] }],
    })
    await nextTick()
    expect(chat.hasRecommended.value).toBe(true)
    chat.applyRecommended()
    expect(chat.autoPickId(q)).toBe('b')
    expect(chat.selectedDemoForInteractive(q)?.id).toBe('b')
    chat.submitRecommended()
    expect(emit).toHaveBeenCalled()

    const noOptions = question({ id: 'empty', options: [] })
    expect(chat.autoPickId(noOptions)).toBe('')
    app.unmount()
  })

  it('reconciles authoritative queue state and resumes an active item', () => {
    const { chat, app } = withChat()
    chat.sendMessage('optimistic', [image()], [annotation()])
    chat.applyQueueState(1, [{ id: 'q1', text: 'optimistic' }], false)
    expect(chat.queued.value[0]?.id).toBe('q1')
    expect(chat.queued.value[0]?.images).toHaveLength(1)

    chat.applyQueueState(0, [], true, {
      id: 'active',
      text: 'running',
      images: [image()],
      annotations: [annotation()],
    })
    expect(chat.liveTurns.value.map((t) => t.role)).toEqual(['human', 'agent'])
    expect(chat.liveAgentIdx.value).toBe(1)
    expect(chat.thinking.value).toBe(true)

    chat.applyQueueState(0, [], false, null)
    expect(chat.liveAgentIdx.value).toBe(-1)
    expect(chat.thinking.value).toBe(false)
    app.unmount()
  })

  it('materializes websocket turns, streams ACP content, and settles completion', () => {
    const { chat, app } = withChat()
    chat.applyQueueState(1, [{ id: 'item-1', text: 'hello' }])
    chat.applyReviewFrame({
      event: 'turn_begin',
      nodeId: 'node-1',
      item: { id: 'item-1', text: 'hello', images: [image()] },
    })
    expect(chat.liveAgentIdx.value).toBe(1)
    expect(chat.applyAcpEvents([{ kind: 'thought', text: 'thinking' }], 'other')).toBe(true)
    expect(chat.applyAcpEvents([
      { kind: 'thought', text: 'reasoning' },
      { kind: 'message', text: 'answer' },
    ])).toBe(true)
    const agent = chat.liveTurns.value[1]!
    expect(agent.text).toBe('answer')
    expect(agent.thought).toBe('reasoning')
    expect(chat.liveStreamHtml.value).toContain('answer')
    expect(chat.agentHasMessage(agent)).toBe(true)
    expect(chat.agentThoughtDisplay(agent, 1)).toBe('reasoning')

    chat.applyReviewFrame({ event: 'turn_done', interrupted: true })
    expect(agent.streaming).toBe(false)
    expect(agent.interrupted).toBe(true)
    expect(chat.liveAgentIdx.value).toBe(-1)
    expect(chat.showTurnCompleted(agent)).toBe(false)
    expect(chat.applyAcpEvents([{ kind: 'message', text: 'late' }])).toBe(false)
    app.unmount()
  })

  it('handles streamed errors, cancellation, ghost settlement and authoritative idle', () => {
    const { chat, app, emit } = withChat({ reviewMode: true })
    chat.sendMessage('queued')
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'queued' } })
    chat.applyReviewFrame({ event: 'error', message: 'failed', interrupted: true })
    expect(chat.liveTurns.value[1]?.text).toBe('failed')

    chat.sendMessage('next')
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'next' } })
    chat.cancelReview()
    expect(emit).toHaveBeenCalledWith('cancel')
    expect(chat.liveTurns.value.at(-1)?.interrupted).toBe(true)
    expect(chat.queued.value).toEqual([])

    chat.sendMessage('ghost')
    chat.discardLastQueued()
    expect(chat.thinking.value).toBe(false)
    chat.forceAuthoritativeIdle()
    expect(chat.liveAgentIdx.value).toBe(-1)
    app.unmount()
  })

  it('supports finish and confirm flows and releases validation on errors', async () => {
    const clarify = withChat({ nodeType: 'grasp' })
    clarify.chat.finishEarly()
    expect(clarify.emit).toHaveBeenCalledWith('finish')
    // Confirm-flow uses validating chrome, not thinking placeholder (plan g2.1).
    expect(clarify.chat.validating.value).toBe(true)
    expect(clarify.chat.thinking.value).toBe(false)
    clarify.chat.finishEarly()
    expect(clarify.emit).toHaveBeenCalledTimes(1)
    clarify.app.unmount()

    const review = withChat({ reviewMode: true })
    review.chat.finishEarly()
    expect(review.chat.validating.value).toBe(true)
    expect(review.emit).toHaveBeenCalledWith('finish')
    review.props.confirmError = 'validation failed'
    await nextTick()
    expect(review.chat.validating.value).toBe(false)
    review.props.done = true
    review.chat.finishEarly()
    expect(review.emit).toHaveBeenCalledTimes(1)
    review.app.unmount()
  })

  it('tracks scrolling and unread persisted turns', async () => {
    const { chat, app, props } = withChat()
    await flushPromises()
    const el = scroller(20)
    chat.scroller.value = el
    chat.onScrollerScroll()
    expect(chat.stickToBottom.value).toBe(false)
    props.turns = [{ role: 'human', text: 'new', at: '1' }] as never
    await nextTick()
    expect(chat.unreadCount.value).toBe(1)
    expect(chat.showUnreadFab.value).toBe(true)
    chat.onUnreadFabClick()
    await nextTick()
    expect(chat.unreadCount.value).toBe(0)
    expect(el.scrollTop).toBe(300)
    el.scrollTop = 210
    chat.onScrollerScroll()
    expect(chat.stickToBottom.value).toBe(true)
    app.unmount()
  })

  it('adds files, handles paste fallbacks, and opens only image previews', async () => {
    class Reader {
      result: string | ArrayBuffer | null = null
      onload: (() => void) | null = null
      readAsDataURL(file: File) {
        this.result = `data:${file.type};base64,YWJj`
        this.onload?.()
      }
    }
    vi.stubGlobal('FileReader', Reader)
    const { chat, app, models } = withChat()
    const good = new File(['abc'], 'good.png', { type: 'image/png' })
    const huge = new File(['x'], 'huge.bin')
    Object.defineProperty(huge, 'size', { value: 51 * 1024 * 1024 })
    chat.addFiles({ 0: good, 1: huge, length: 2, item: (i: number) => [good, huge][i] } as FileList)
    expect(models.attachments.value).toHaveLength(1)
    expect(chat.attachNotice.value).toBeTruthy()

    chat.onPaste({ clipboardData: null } as unknown as ClipboardEvent)
    await nextTick()
    chat.openImagePreview(models.attachments.value, 0)
    expect(chat.imagePreview.value?.src).toContain('data:image/png')
    chat.closeImagePreview()
    chat.openImagePreview([{ data: 'x', mimeType: 'text/plain', name: 'a.txt' }], 0)
    expect(chat.imagePreview.value).toBeNull()
    chat.removeAttachment(0)
    app.unmount()
  })

  it('derives seed turns and thought display behavior', () => {
    const seed = image('seed.png')
    const { chat, app } = withChat({ seedHumanText: 'seed', seedHumanImages: [seed] })
    expect(chat.seedHumanTurn.value?.text).toBe('seed')
    expect(chat.prependSeedHuman([])).toHaveLength(1)
    const existing = [{ role: 'human', text: 'seed', at: '1' }] as never
    expect(chat.prependSeedHuman(existing)[0]?.images).toEqual([seed])
    expect(chat.humanMatchesSeed({ role: 'agent', text: 'seed', at: '1' }, chat.seedHumanTurn.value!)).toBe(false)

    const agent = { role: 'agent', text: '', thought: 'why', streaming: true, at: '1' } as never
    expect(chat.isThoughtOpen(0, agent)).toBe(true)
    chat.onThoughtToggle(0, { target: { open: false } } as unknown as Event)
    expect(chat.isThoughtOpen(0, agent)).toBe(false)
    expect(chat.showTurnCompleted({ ...agent, streaming: false, interrupted: false })).toBe(true)
    app.unmount()
  })

  it('covers composer growth, attachment input, annotation removal and image labels', async () => {
    class Reader {
      result: string | ArrayBuffer | null = null
      onload: (() => void) | null = null
      readAsDataURL(file: File) {
        this.result = `data:${file.type};base64,YQ==`
        this.onload?.()
      }
    }
    vi.stubGlobal('FileReader', Reader)
    const { chat, app, models } = withChat()
    await flushPromises()
    const textarea = document.createElement('textarea')
    Object.defineProperty(textarea, 'scrollHeight', { value: 90 })
    chat.textareaRef.value = textarea
    chat.autoGrow()
    chat.onTextInput()
    expect(textarea.style.height).toBeTruthy()

    const file = new File(['a'], '', { type: 'image/png' })
    const input = document.createElement('input')
    Object.defineProperty(input, 'files', {
      value: { 0: file, length: 1, item: () => file },
    })
    chat.fileInput.value = input
    chat.onPickFiles({ target: input } as unknown as Event)
    expect(models.attachments.value[0]?.name).toBe('attachment-1')
    expect(input.value).toBe('')
    expect(chat.imagePreviewLabel(models.attachments.value, 0)).toBeTruthy()
    expect(chat.imagePreviewLabel([image(), image('')], 1)).toBeTruthy()

    models.annotations.value = [annotation(), annotation({ selector: '#two' })]
    chat.removeAnnotation(0)
    expect(models.annotations.value).toHaveLength(1)
    app.unmount()
  })

  it('parses persisted choice replies and locates replies for agent questions', () => {
    const q = question()
    const choice = '我的选择:\n- 选择环境 → 开发、其他'
    const turns = [
      { role: 'agent', text: 'ask', at: '1', questions: [q] },
      { role: 'human', text: choice, at: '2' },
    ]
    const { chat, app } = withChat({ turns })
    expect(chat.textHasChoicePrefix(choice)).toBe(true)
    expect(chat.stripChoicePrefix(choice)).toContain('选择环境')
    expect(chat.isChoiceReply(choice)).toBe(true)
    expect(chat.latestQuestionTurnIndex(turns as never)).toBe(0)
    expect(chat.hasHumanReplyAfter(turns as never, 0)).toBe(true)
    const rows = chat.choiceRowsForAgentTurn(0)
    expect(rows?.[0]?.answers).toEqual(['开发', '其他'])
    expect(chat.selectedLabelsForQuestion(q, rows)).toEqual(['开发', '其他'])
    expect(chat.isActiveTurn(0)).toBe(false)
    app.unmount()
  })

  it('covers queue-state trimming, live completion, and wrong-node frames', () => {
    const { chat, app } = withChat()
    chat.sendMessage('one')
    chat.sendMessage('two')
    chat.sendMessage('three')
    chat.applyQueueState(1, [{ id: 'one', text: 'one' }], true)
    expect(chat.queued.value).toHaveLength(1)

    chat.applyReviewFrame({ event: 'turn_begin', nodeId: 'wrong', item: { text: 'ignored' } })
    expect(chat.liveAgentIdx.value).toBe(-1)
    chat.applyReviewFrame({ event: 'turn_begin', item: { id: 'missing', text: 'active' } })
    chat.applyAcpEvents([{ kind: 'message', text: 'complete' }])
    chat.applyQueueState(0, [], false)
    expect(chat.liveTurns.value.at(-1)?.streaming).toBe(false)
    expect(chat.liveAgentIdx.value).toBe(-1)
    expect(chat.showTurnCompleted(chat.liveTurns.value.at(-1)!)).toBe(true)

    chat.forceAuthoritativeIdle()
    chat.discardLastQueued()
    app.unmount()
  })

  it('handles approve placeholders, done state and empty seed images', async () => {
    const { chat, app, props } = withChat({ nodeType: 'approve', seedHumanImages: [image()] })
    expect(chat.showApproveEmptyHint.value).toBe(false)
    expect(chat.useConfirmFlowAction.value).toBe(true)
    expect(chat.inputPlaceholder.value).toBeTruthy()
    expect(chat.seedHumanTurn.value?.images).toHaveLength(1)
    chat.thinking.value = true
    chat.validating.value = true
    props.done = true
    await nextTick()
    expect(chat.thinking.value).toBe(false)
    expect(chat.validating.value).toBe(false)
    app.unmount()
  })

  it('keeps the previous reply out of the next live bubble', () => {
    const { chat, app } = withChat({
      turns: [
        { role: 'human', text: 'first', at: '1' },
        { role: 'agent', text: '上一轮的回复', at: '2' },
      ],
    })
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'second' } })
    const agent = chat.liveTurns.value.at(-1)!
    chat.applyAcpEvents([
      { kind: 'thought', text: '旧思考' },
      { kind: 'message', text: '上一轮的回复' },
    ])
    expect(agent.text).toBe('')
    expect(agent.thought).toBe('')

    chat.applyAcpEvents([{ kind: 'thought', text: '新思考' }])
    chat.applyAcpEvents([{ kind: 'message', text: '新回复' }])
    chat.applyAcpEvents([{ kind: 'message', text: '上一轮的回复' }])
    expect(agent.thought).toBe('新思考')
    expect(agent.text).toBe('新回复')
    app.unmount()
  })

  it('clears live bubbles when persisted turns catch up', async () => {
    const { chat, app, props } = withChat()
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'hello' } })
    chat.applyAcpEvents([{ kind: 'message', text: 'answer' }])
    expect(chat.liveTurns.value).toHaveLength(2)
    props.turns = [
      { role: 'human', text: 'hello', at: '1' },
      { role: 'agent', text: 'answer', at: '2' },
    ] as never
    await nextTick()
    expect(chat.liveTurns.value).toEqual([])
    expect(chat.liveAgentIdx.value).toBe(-1)
    expect(chat.thinking.value).toBe(false)
    app.unmount()
  })

  it('extracts pasted files through DataTransfer and prevents native paste', async () => {
    class Reader {
      result = 'data:image/png;base64,YQ=='
      onload: (() => void) | null = null
      readAsDataURL() {
        this.onload?.()
      }
    }
    class Transfer {
      files: File[] = []
      items = { add: (f: File) => this.files.push(f) }
    }
    vi.stubGlobal('FileReader', Reader)
    vi.stubGlobal('DataTransfer', Transfer)
    const { chat, app, models } = withChat()
    const file = new File(['a'], 'paste.png', { type: 'image/png' })
    const preventDefault = vi.fn()
    chat.onPaste({
      preventDefault,
      clipboardData: {
        items: [
          { kind: 'string', getAsFile: () => null },
          { kind: 'file', getAsFile: () => file },
        ],
      },
    } as unknown as ClipboardEvent)
    await nextTick()
    expect(preventDefault).toHaveBeenCalled()
    expect(models.attachments.value[0]?.name).toBe('paste.png')
    app.unmount()
  })

  it('settles a non-empty live slot when queue authority becomes idle', () => {
    const { chat, app } = withChat()
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'hello' } })
    chat.applyAcpEvents([{ kind: 'message', text: 'answer' }])
    chat.applyQueueState(1, [{ id: 'wait', text: 'wait' }], false)
    expect(chat.liveTurns.value[1]?.text).toBe('answer')
    expect(chat.liveTurns.value[1]?.streaming).toBe(false)
    expect(chat.liveAgentIdx.value).toBe(-1)

    chat.queued.value.push({ text: 'ghost', images: [], annotations: [] })
    chat.settleAfterTurnEnd()
    expect(chat.queued.value.map((q) => q.id)).toEqual(['wait'])
    app.unmount()
  })

  it('keeps empty failed live slot on authoritative idle (plan g1.1)', () => {
    const { chat, app } = withChat()
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'goal' } })
    chat.applyReviewFrame({ event: 'turn_done' })
    expect(chat.liveTurns.value[1]?.text).toBe('')
    expect(chat.liveTurns.value[1]?.streaming).toBe(false)
    chat.applyQueueState(0, [], false)
    expect(chat.liveTurns.value).toHaveLength(2)
    expect(chat.isRetryableFailedAgent(chat.liveTurns.value[1]!)).toBe(true)
    expect(chat.showTurnCompleted(chat.liveTurns.value[1]!)).toBe(false)
    app.unmount()
  })

  it('retryLast emits retry-last without touching draft (plan g1.2)', async () => {
    const { chat, app, emit, models } = withChat()
    models.draft.value = 'keep-me'
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'goal' } })
    chat.applyReviewFrame({ event: 'turn_done' })
    chat.applyQueueState(0, [], false)
    expect(chat.failRetryDisabled.value).toBe(false)
    chat.retryLastFailed()
    expect(emit).toHaveBeenCalledWith('retry-last')
    expect(models.draft.value).toBe('keep-me')
    expect(chat.liveTurns.value[1]?.streaming).toBe(true)
    expect(chat.liveTurns.value.filter((t) => t.role === 'human')).toHaveLength(1)
    app.unmount()
  })

  it('does not show retry on success or interrupted turns (plan g2.3)', () => {
    const { chat, app } = withChat()
    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'ok' } })
    chat.applyAcpEvents([{ kind: 'message', text: '正文' }])
    chat.applyReviewFrame({ event: 'turn_done' })
    expect(chat.showTurnCompleted(chat.liveTurns.value[1]!)).toBe(true)
    expect(chat.showFailRetry(chat.liveTurns.value[1]!, 1)).toBe(false)

    chat.applyReviewFrame({ event: 'turn_begin', item: { text: 'cancel-me' } })
    chat.cancelReview()
    const agent = chat.liveTurns.value.at(-1)!
    expect(agent.interrupted).toBe(true)
    expect(agent.text).toBe('(已中断)')
    expect(chat.isRetryableFailedAgent(agent)).toBe(false)
    app.unmount()
  })
})

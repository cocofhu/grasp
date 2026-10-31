// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyImage, ClarifyTurn, DialogueOutcome, ReactAnnotation, ReactQuestion } from '@/lib/shared/types'

import { useClarifyChat } from './useClarifyChat'
import { clearDialogues } from '@/lib/chat/dialogueStore'
import type { DialogueTurn } from '@/lib/chat/dialogueState'
import { endedTurn, feedDialogue, orphanTurn, queuedTurn, runningTurn, type DialogueSpec, type RowInput } from '@/test/dialogueKit'
import { putClarifyOutbox, takeClarifyOutbox } from './clarifyDraftStash'
import { confirmFlowCeremonyKey, type ConfirmFlowCeremony } from './confirmFlowCeremony'
import { __resetSidebarHandoffForTests, isSidebarConfirmWindowHeld } from './sidebarHandoff'

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

afterEach(() => clearDialogues())

const feed = (spec: DialogueSpec) => feedDialogue('run-1', 'node-1', spec)

function sentIds(emit: ReturnType<typeof vi.fn>): string[] {
  return emit.mock.calls.filter((c) => c[0] === 'send').map((c) => c[4] as string)
}

type Human = { text: string; clientMessageId?: string; images?: ClarifyImage[]; annotations?: ReactAnnotation[] }

function turnRows(turnId: string, human: Human, replies: Partial<ClarifyTurn>[]): RowInput[] {
  return [
    { role: 'human', at: '1', turnId, ...human },
    ...replies.map((r) => ({ role: 'agent' as const, text: '', at: '2', turnId, clientMessageId: human.clientMessageId, ...r })),
  ]
}

/** The server running `turnId` with one streaming reply, then any waiters. */
function running(turnId: string, human: Human, reply: Partial<ClarifyTurn> = {}, queue: DialogueTurn[] = []) {
  const cmid = human.clientMessageId
  feed({
    busy: true,
    rows: turnRows(turnId, human, [{ streaming: true, ...reply }]),
    turns: [runningTurn(turnId, { text: human.text, clientMessageId: cmid }, { clientMessageId: cmid }), ...queue],
  })
}

/** The server after `turnId` ended with `reply`, then any waiters. */
function ended(turnId: string, human: Human, reply: Partial<ClarifyTurn>, outcome: DialogueOutcome = 'ok', queue: DialogueTurn[] = []) {
  feed({
    busy: queue.length > 0,
    rows: turnRows(turnId, human, [{ outcome: outcome === 'ok' ? undefined : outcome, ...reply }]),
    turns: [endedTurn(turnId, outcome, { clientMessageId: human.clientMessageId }), ...queue],
  })
}

const waiter = (id: string, pos: number, item: Omit<Human, 'text'> & { text?: string }) =>
  queuedTurn(id, pos, item, { clientMessageId: item.clientMessageId })

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
      expect.arrayContaining([expect.objectContaining({ selector: '#button' })]), expect.any(String),
    )
    expect(chat.queued.value).toHaveLength(1)
    expect(chat.thinking.value).toBe(true)
    expect(models.draft.value).toBe('')
    expect(models.attachments.value).toEqual([])
    expect(models.annotations.value).toEqual([])
    app.unmount()
  })

  it('drops the local send once the server runs it with host-enriched annotations', () => {
    const { chat, app, emit } = withChat()
    try {
      chat.sendMessage('hide these details', [], [annotation({ selector: '#details' })])
      const [cmid] = sentIds(emit)
      running('annotated-send', {
        text: 'hide these details',
        clientMessageId: cmid,
        // The host appends the current preview selection before enqueueing.
        annotations: [annotation({ selector: '#details' }), annotation({ selector: '#preview-pick' })],
      }, { thought: 'already processing the annotated request' })
      expect(chat.queued.value).toHaveLength(0)
      expect(chat.displayTurns.value.filter(turn => turn.role === 'human')).toHaveLength(1)
      expect(chat.displayTurns.value.at(-1)?.thought).toBe('already processing the annotated request')
    } finally { app.unmount() }
  })

  it('keeps the distinct same-text annotated waiter when the enriched active send starts', () => {
    const { chat, app, emit } = withChat()
    try {
      chat.sendMessage('same request', [], [annotation({ selector: '#first' })])
      chat.sendMessage('same request', [], [annotation({ selector: '#second' })])
      const [first] = sentIds(emit)
      running('first', {
        text: 'same request',
        clientMessageId: first,
        annotations: [annotation({ selector: '#first' }), annotation({ selector: '#staged' })],
      })
      expect(chat.queued.value).toHaveLength(1)
      expect(chat.queued.value[0]?.annotations[0]?.selector).toBe('#second')
      expect(chat.displayTurns.value.filter(turn => turn.role === 'human')).toHaveLength(1)
    } finally { app.unmount() }
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
      [], expect.any(String),
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
      [], expect.any(String),
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
    feed({
      busy: true,
      turns: [
        waiter('a', 0, { text: 'first', images: [image('a.png')], annotations: [annotation()] }),
        waiter('b', 1, { text: 'second' }),
      ],
    })
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
    feed({ busy: true, turns: [waiter('server', 0, { text: 'server row' })] })
    models.draft.value = 'local draft'
    models.annotations.value = [annotation()]
    chat.editQueuedItem(0)
    expect(emit).toHaveBeenCalledWith('send', 'local draft', [], expect.any(Array), expect.any(String))
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
      [], expect.any(String),
    )
    const sent = emit.mock.calls.at(-1)![1]
    expect(chat.parseChoiceSummary(sent)).toEqual(
      expect.arrayContaining([expect.objectContaining({ q: '选择区域' })]),
    )
    expect(chat.parseChoiceSummary('plain')).toBeNull()
    expect(chat.parseChoiceSummary('我的选择:\n- bad')).toBeNull()
    const sentMulti = String(emit.mock.calls.at(-1)![1])
    expect(sentMulti).toContain('美国')
    expect(sentMulti).toContain('其他地区')
    app.unmount()
  })

  it('single-select keeps one source, ignores blank or residual other text, and does not restore the other source', async () => {
    const q = question()
    const q2 = question({
      id: 'q2',
      prompt: '选择区域',
      options: [
        { id: 'cn', label: '中国' },
        { id: 'us', label: '美国' },
      ],
    })
    const { chat, app, emit } = withChat({
      turns: [{ role: 'agent', text: 'questions', at: '1', questions: [q, q2] }],
    })
    await nextTick()
    expect(chat.isSelected('q1', 'dev')).toBe(true)

    chat.toggleOther(q)
    expect(chat.isOtherSelected('q1')).toBe(true)
    expect(chat.isSelected('q1', 'dev')).toBe(false)
    expect(chat.sel.value.q1 ?? []).toEqual([])
    expect(chat.isOtherSelected('q2')).toBe(false)
    chat.other.value.q1 = '   '
    expect(chat.otherAnswered(q)).toBe(false)
    expect(chat.someAnswered.value).toBe(false)
    chat.submitChoices()
    expect(emit).not.toHaveBeenCalled()

    chat.other.value.q1 = '只要这句'
    chat.pick(q2, 'cn')
    expect(chat.isOtherSelected('q1')).toBe(true)
    expect(chat.isSelected('q2', 'cn')).toBe(true)
    chat.pick(q, 'prod')
    expect(chat.isSelected('q1', 'prod')).toBe(true)
    expect(chat.isSelected('q1', 'dev')).toBe(false)
    expect(chat.isOtherSelected('q1')).toBe(false)
    expect(chat.isSelected('q2', 'cn')).toBe(true)
    expect(chat.other.value.q1).toBe('只要这句')
    chat.pick(q, 'prod')
    expect(chat.isSelected('q1', 'prod')).toBe(false)
    expect(chat.isOtherSelected('q1')).toBe(false)
    expect(chat.other.value.q1).toBe('只要这句')
    expect(chat.someAnswered.value).toBe(true)

    chat.toggleOther(q)
    chat.toggleOther(q)
    expect(chat.isOtherSelected('q1')).toBe(false)
    expect(chat.sel.value.q1 ?? []).toEqual([])
    expect(chat.isSelected('q2', 'cn')).toBe(true)
    chat.submitChoices()
    expect(String(emit.mock.calls.at(-1)![1])).toBe('我的选择:\n- 选择区域 → 中国')
    app.unmount()
  })

  it('single-select apply recommended clears other and submits only that label', async () => {
    const q = question()
    const { chat, app, emit } = withChat({
      turns: [{ role: 'agent', text: 'q', at: '1', questions: [q] }],
    })
    await nextTick()
    chat.toggleOther(q)
    chat.other.value.q1 = '不要带上'
    expect(chat.someAnswered.value).toBe(true)
    chat.submitRecommended()
    expect(String(emit.mock.calls.at(-1)![1])).toBe('我的选择:\n- 选择环境 → 开发')
    app.unmount()
    clearDialogues()

    const plain = question({
      id: 'plain',
      options: [
        { id: 'a', label: '甲' },
        { id: 'b', label: '乙' },
      ],
    })
    const bare = withChat({
      turns: [{ role: 'agent', text: 'q', at: '1', questions: [plain] }],
    })
    await nextTick()
    bare.chat.toggleOther(plain)
    bare.chat.other.value.plain = '   '
    expect(bare.chat.someAnswered.value).toBe(false)
    bare.chat.submitChoices()
    expect(bare.emit).not.toHaveBeenCalled()
    bare.chat.other.value.plain = '补充'
    bare.chat.applyRecommended()
    expect(bare.chat.isSelected('plain', 'a')).toBe(true)
    expect(bare.chat.isOtherSelected('plain')).toBe(false)
    expect(bare.chat.other.value.plain).toBe('补充')
    bare.chat.submitChoices()
    expect(String(bare.emit.mock.calls.at(-1)![1])).toBe('我的选择:\n- 选择环境 → 甲')
    bare.app.unmount()
  })

  it('multi-select still submits several presets together with other text', async () => {
    const q = question({
      id: 'q2',
      prompt: '选择区域',
      allowMultiple: true,
      options: [
        { id: 'cn', label: '中国' },
        { id: 'us', label: '美国' },
        { id: 'eu', label: '欧洲' },
      ],
    })
    const { chat, app, emit } = withChat({
      turns: [{ role: 'agent', text: 'questions', at: '1', questions: [q] }],
    })
    await nextTick()
    chat.toggle(q, 'cn')
    chat.toggle(q, 'us')
    chat.toggleOther(q)
    chat.other.value.q2 = '其他地区'
    expect(chat.isSelected('q2', 'cn')).toBe(true)
    expect(chat.isSelected('q2', 'us')).toBe(true)
    expect(chat.isSelected('q2', 'eu')).toBe(false)
    expect(chat.isOtherSelected('q2')).toBe(true)
    chat.submitChoices()
    expect(String(emit.mock.calls.at(-1)![1])).toBe('我的选择:\n- 选择区域 → 中国、美国、其他地区')
    app.unmount()
  })

  it('multi-select apply recommended still fills one preset and keeps other', async () => {
    const q = question({
      allowMultiple: true,
      options: [
        { id: 'dev', label: '开发', recommended: true },
        { id: 'prod', label: '生产' },
      ],
    })
    const { chat, app, emit } = withChat({
      turns: [{ role: 'agent', text: 'q', at: '1', questions: [q] }],
    })
    await nextTick()
    chat.toggle(q, 'prod')
    chat.toggleOther(q)
    chat.other.value.q1 = '其他环境'
    chat.applyRecommended()
    expect(chat.isSelected('q1', 'dev')).toBe(true)
    expect(chat.isSelected('q1', 'prod')).toBe(false)
    expect(chat.isOtherSelected('q1')).toBe(true)
    chat.submitChoices()
    expect(String(emit.mock.calls.at(-1)![1])).toBe('我的选择:\n- 选择环境 → 开发、其他环境')
    app.unmount()
  })

  it('parses wrapped choice prompts without changing one-line rows', () => {
    const { chat, app } = withChat()
    expect(chat.parseChoiceSummary('我的选择:\n- 整行题干 → 方案 A、方案 B')).toEqual([
      { q: '整行题干', answers: ['方案 A', '方案 B'] },
    ])
    expect(chat.parseChoiceSummary('我的选择:\n- 第一段\n\n第二段 → 方案 A')).toEqual([
      { q: '第一段\n\n第二段', answers: ['方案 A'] },
    ])
    expect(chat.parseChoiceSummary('My choices:\n- Prompt\ncontinued → Alpha、Beta')).toEqual([
      { q: 'Prompt\ncontinued', answers: ['Alpha', 'Beta'] },
    ])
    expect(chat.parseChoiceSummary('我的选择:\n- 题一 → A\n- 题二\n续行 → B、C')).toEqual([
      { q: '题一', answers: ['A'] },
      { q: '题二\n续行', answers: ['B', 'C'] },
    ])
    expect(chat.parseChoiceSummary('我的选择:\n- 只有题干\n没有箭头')).toBeNull()
    expect(chat.parseChoiceSummary('plain')).toBeNull()
    app.unmount()
  })

  it('matches a wrapped prompt to the question that was asked', () => {
    const prompt = '第一段\n\n第二段'
    const q = question({ prompt, options: [{ id: 'a', label: '方案 A' }, { id: 'b', label: '方案 B' }] })
    const { chat, app } = withChat({
      turns: [
        { role: 'agent', text: 'ask', at: '1', questions: [q] },
        { role: 'human', text: `我的选择:\n- ${prompt} → 方案 A、其他说明`, at: '2' },
      ],
    })
    const rows = chat.choiceRowsForAgentTurn(0)
    expect(rows).toEqual([{ q: prompt, answers: ['方案 A', '其他说明'] }])
    expect(chat.selectedLabelsForQuestion(q, rows)).toEqual(['方案 A', '其他说明'])
    app.unmount()
  })

  it('preselects a recommended option that is already present on mount', async () => {
    const q = question({
      options: [
        { id: 'a', label: 'A', demoHtml: '<b>A</b>' },
        { id: 'b', label: 'B', recommended: true, demoHtml: '<b>B</b>' },
      ],
    })
    const { chat, app } = withChat({
      turns: [{ role: 'agent', text: 'q', at: '1', questions: [q] }],
    })
    await nextTick()
    expect(chat.isSelected(q.id, 'b')).toBe(true)
    expect(chat.isSelected(q.id, 'a')).toBe(false)
    expect(chat.selectedDemoForInteractive(q)?.id).toBe('b')
    app.unmount()
    clearDialogues()

    const plain = question({
      id: 'plain',
      options: [
        { id: 'a', label: 'A', demoHtml: '<b>A</b>' },
        { id: 'b', label: 'B' },
      ],
    })
    const bare = withChat({
      turns: [{ role: 'agent', text: 'q', at: '1', questions: [plain] }],
    })
    await nextTick()
    expect(bare.chat.sel.value[plain.id] ?? []).toEqual([])
    expect(bare.chat.selectedDemoForInteractive(plain)).toBeNull()
    bare.app.unmount()
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

  it('follows the server from queued to running to idle', () => {
    const { chat, app, emit } = withChat()
    chat.sendMessage('optimistic', [image()], [annotation()])
    const [cmid] = sentIds(emit)
    feed({ busy: true, turns: [waiter('q1', 0, { text: 'optimistic', clientMessageId: cmid, images: [image()] })] })
    expect(chat.queued.value.map((q) => q.id)).toEqual(['q1'])
    expect(chat.queued.value[0]?.images).toHaveLength(1)

    const human = { text: 'optimistic', clientMessageId: cmid, images: [image()], annotations: [annotation()] }
    running('q1', human)
    expect(chat.displayTurns.value.map((t) => t.role)).toEqual(['human', 'agent'])
    expect(chat.liveAgentIdx.value).toBe(1)
    expect(chat.thinking.value).toBe(true)
    expect(chat.queued.value).toEqual([])

    ended('q1', human, { text: 'done' })
    expect(chat.liveAgentIdx.value).toBe(-1)
    expect(chat.thinking.value).toBe(false)
    app.unmount()
  })

  it('shows each reply row of a turn with its own tools and timeline', () => {
    const { chat, app } = withChat()
    const parts = [
      { kind: 'thought' as const, text: 'look' },
      { kind: 'tool' as const, title: 'Shell', status: 'completed', summary: 'ls' },
      { kind: 'message' as const, text: 'second' },
    ]
    feed({
      busy: true,
      rows: turnRows('item-1', { text: 'hello' }, [
        { text: 'answer', handoff: true },
        { text: 'second', streaming: true, tools: [{ title: 'Shell', status: 'completed' }], parts },
      ]),
      turns: [runningTurn('item-1', { text: 'hello' })],
    })
    const rows = chat.displayTurns.value.filter((x) => x.role === 'agent')
    expect(rows.map((r) => r.text)).toEqual(['answer', 'second'])
    expect(rows[0]!.tools).toBeUndefined()
    expect(rows[0]!.parts).toBeUndefined()
    expect(rows[1]!.tools).toEqual([{ title: 'Shell', status: 'completed' }])
    expect(rows[1]!.parts).toEqual(parts)
    app.unmount()
  })

  it('streams a running reply and settles it when the server interrupts the turn', () => {
    const { chat, app } = withChat()
    const human = { text: 'hello', images: [image()] }
    running('item-1', human, { thought: 'reasoning', text: 'answer' })
    expect(chat.liveAgentIdx.value).toBe(1)
    let agent = chat.displayTurns.value[1]!
    expect(agent.text).toBe('answer')
    expect(agent.thought).toBe('reasoning')
    expect(chat.agentHasMessage(agent)).toBe(true)
    expect(chat.agentThoughtDisplay(agent, 1)).toBe('reasoning')
    expect(agent.tools).toBeUndefined()

    ended('item-1', human, { text: 'answer', thought: 'reasoning', interrupted: true }, 'interrupted')
    agent = chat.displayTurns.value[1]!
    expect(agent.streaming).toBeFalsy()
    expect(agent.interrupted).toBe(true)
    expect(chat.liveAgentIdx.value).toBe(-1)
    expect(chat.showTurnCompleted(agent)).toBe(false)
    app.unmount()
  })

  it('keeps painting an orphan sandbox turn until the server seals it', () => {
    const { chat, app } = withChat()
    const human = { text: 'hello' }
    const orphan = { runningOpId: 'q-d5d82a58-df0', desynced: false }
    feed({
      rows: turnRows('item-1', human, [{ text: '短回复', thought: '还在核对边界', streaming: true, phase: 'orphan' }]),
      turns: [orphanTurn(orphan.runningOpId)],
      orphan,
    })
    expect(chat.liveAgentIdx.value).toBeGreaterThanOrEqual(0)
    expect(chat.showSandboxOrphanBanner.value).toBe(true)
    const agents = chat.displayTurns.value.filter((t) => t.role === 'agent')
    expect(agents).toHaveLength(1)
    expect(agents[0]).toMatchObject({ text: '短回复', thought: '还在核对边界', streaming: true })
    expect(chat.showTurnCompleted(agents[0]!)).toBe(false)

    ended('item-1', human, { text: '短回复', thought: '还在核对边界，并补上后续步骤' })
    const settled = chat.displayTurns.value.find((t) => t.role === 'agent')
    expect(settled?.streaming).toBeFalsy()
    expect(settled?.thought).toContain('后续步骤')
    expect(chat.thinking.value).toBe(false)
    expect(chat.showTurnCompleted(settled!)).toBe(true)
    expect(chat.showSandboxOrphanBanner.value).toBe(false)
    app.unmount()
  })

  it('splits one user message into two agent rows when the turn continues', () => {
    const { chat, app } = withChat()
    feed({
      busy: true,
      rows: turnRows('item-1', { text: 'hello' }, [
        { text: '计划已经写好', thought: '先看现状', handoff: true },
        { text: '已经继续做完', thought: '接着把服务放到后台', streaming: true },
      ]),
      turns: [runningTurn('item-1', { text: 'hello' })],
    })
    const turns = chat.displayTurns.value
    expect(turns.map((t) => t.role)).toEqual(['human', 'agent', 'agent'])
    expect(turns[1]?.text).toBe('计划已经写好')
    expect(turns[1]?.handoff).toBe(true)
    expect(turns[1]?.streaming).toBeFalsy()
    expect(chat.showTurnCompleted(turns[1]!)).toBe(false)
    expect(turns[2]?.text).toBe('已经继续做完')
    expect(turns[2]?.thought).toBe('接着把服务放到后台')
    expect(turns[2]?.streaming).toBe(true)
    expect(chat.liveAgentIdx.value).toBe(2)
    app.unmount()
  })

  it('stops a running review turn and withdraws a failed local send', async () => {
    const { chat, app, emit } = withChat({ reviewMode: true })
    running('t1', { text: 'next' })
    chat.cancelReview()
    expect(emit).toHaveBeenCalledWith('cancel')
    expect(chat.stopping.value).toBe(true)
    ended('t1', { text: 'next' }, { text: '(已中断)', interrupted: true }, 'interrupted')
    await nextTick()
    expect(chat.displayTurns.value.at(-1)?.interrupted).toBe(true)
    expect(chat.stopping.value).toBe(false)
    expect(chat.queued.value).toEqual([])

    chat.sendMessage('ghost')
    expect(chat.thinking.value).toBe(true)
    chat.discardLastQueued()
    expect(chat.thinking.value).toBe(false)
    expect(chat.queued.value).toEqual([])
    app.unmount()
  })

  it('plan g1.2: unnamed sandbox desync keeps abort after a completed turn', () => {
    const { chat, app, emit } = withChat()
    feed({
      rows: turnRows('t1', { text: '继续' }, [{ text: '最新回复已落盘', opId: 'g-00fccbf7-0f2' }]),
      turns: [endedTurn('t1', 'ok'), orphanTurn()],
      orphan: { runningOpId: '', desynced: true },
    })
    expect(chat.showSandboxOrphanBanner.value).toBe(true)
    expect(chat.sandboxOrphanOpLabel.value).toBe('')
    const agent = chat.displayTurns.value.at(-1)!
    expect(chat.showTurnCompleted(agent)).toBe(true)
    expect(agent.interrupted).toBeFalsy()
    chat.cancelReview()
    expect(emit).toHaveBeenCalledWith('cancel')
    app.unmount()
  })

  it('plan g2.2: another unfinished sandbox turn keeps abort and completed history', () => {
    const { chat, app, emit } = withChat()
    feed({
      rows: turnRows('t1', { text: '继续' }, [{ text: '历史已完成', opId: 'g-done' }]),
      turns: [endedTurn('t1', 'ok'), orphanTurn('g-other')],
      orphan: { runningOpId: 'g-other', desynced: false },
    })
    expect(chat.showSandboxOrphanBanner.value).toBe(true)
    expect(chat.sandboxOrphanOpLabel.value).toContain('g-other')
    const agent = chat.displayTurns.value.at(-1)!
    expect(chat.showTurnCompleted(agent)).toBe(true)
    expect(agent.interrupted).toBeFalsy()
    expect(agent.text).toBe('历史已完成')
    chat.cancelReview()
    expect(emit).toHaveBeenCalledWith('cancel')
    app.unmount()
  })

  it('shows sandbox orphan banner when FIFO is idle but the bridge is busy', () => {
    const { chat, app, emit } = withChat()
    feed({ turns: [orphanTurn('oid-stuck')], orphan: { runningOpId: 'oid-stuck', desynced: false } })
    expect(chat.showSandboxOrphanBanner.value).toBe(true)
    expect(chat.sandboxOrphanOpLabel.value).toContain('oid-stuck')
    expect(chat.sessionBusy.value).toBe(false)
    chat.cancelReview()
    expect(emit).toHaveBeenCalledWith('cancel')
    expect(chat.showSandboxOrphanBanner.value).toBe(false)
    chat.finishAbort()
    expect(emit).not.toHaveBeenCalledWith('finish-abort')
    app.unmount()
  })

  it('paints orphan thought, tools and body without forcing scroll or touching the draft', async () => {
    const { chat, app, models } = withChat()
    const history = turnRows('t0', { text: '继续' }, [{ text: '上一句' }])
    feed({ rows: history, turns: [endedTurn('t0', 'ok')] })
    const el = scroller(20)
    chat.scroller.value = el
    chat.onScrollerScroll()
    expect(chat.stickToBottom.value).toBe(false)
    const ta = document.createElement('textarea')
    ta.value = '未发送草稿'
    document.body.append(ta)
    ta.focus()
    ta.setSelectionRange(2, 2)
    chat.textareaRef.value = ta
    models.draft.value = '未发送草稿'
    models.attachments.value = [image()]

    const orphanReply: RowInput = {
      role: 'agent', text: '新的正文', thought: '还在想', at: '3', streaming: true, phase: 'orphan',
      id: 'orphan:g-sess4c0e-8x8t:a0', tools: [{ title: 'read_file', status: 'completed' }],
    }
    feed({
      rows: [...history, orphanReply],
      turns: [endedTurn('t0', 'ok'), orphanTurn('g-sess4c0e-8x8t')],
      orphan: { runningOpId: 'g-sess4c0e-8x8t', desynced: false },
    })
    await flushPromises()

    const agent = [...chat.displayTurns.value].reverse().find((t) => t.role === 'agent')
    expect(agent?.thought).toBe('还在想')
    expect(agent?.text).toBe('新的正文')
    expect(agent?.streaming).toBe(true)
    expect(agent?.tools).toEqual([{ title: 'read_file', status: 'completed' }])
    expect(chat.liveAgentIdx.value).toBeGreaterThanOrEqual(0)
    expect(chat.showSandboxOrphanBanner.value).toBe(true)
    expect(chat.stickToBottom.value).toBe(false)
    expect(el.scrollTop).toBe(20)
    expect(models.draft.value).toBe('未发送草稿')
    expect(models.attachments.value).toHaveLength(1)
    expect(ta.selectionStart).toBe(2)
    expect(document.activeElement).toBe(ta)

    feed({ rows: [...history, { ...orphanReply, streaming: false, phase: 'sealed' }], turns: [endedTurn('t0', 'ok')] })
    expect(chat.showSandboxOrphanBanner.value).toBe(false)
    expect(chat.displayTurns.value.some((t) => t.text === '新的正文')).toBe(true)
    ta.remove()
    app.unmount()
  })

  it('emits finish-abort when the host allows it', () => {
    const { chat, app, emit, props } = withChat({ confirmCanAbort: true })
    chat.finishAbort()
    expect(chat.confirmFlowDialogOpen.value).toBe(true)
    expect(emit).not.toHaveBeenCalled()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledWith('finish-abort')
    props.confirmCanAbort = false
    chat.finishAbort()
    expect(chat.confirmFlowDialogOpen.value).toBe(false)
    expect(emit).toHaveBeenCalledTimes(1)
    app.unmount()
  })

  it.each(['text', 'attachment', 'annotation'])('preserves unsent %s when abort-confirm is requested', (kind) => {
    const { chat, models, emit, app } = withChat({ confirmCanAbort: true })
    if (kind === 'text') models.draft.value = '未发送内容'
    if (kind === 'attachment') models.attachments.value = [image()]
    if (kind === 'annotation') models.annotations.value = [annotation()]
    const original = {
      text: models.draft.value,
      images: [...models.attachments.value],
      annotations: [...models.annotations.value],
    }

    chat.finishAbort()
    expect(chat.confirmFlowDialogOpen.value).toBe(false)
    expect(emit).not.toHaveBeenCalled()
    expect(models.draft.value).toBe(original.text)
    expect(models.attachments.value).toEqual(original.images)
    expect(models.annotations.value).toEqual(original.annotations)
    expect(chat.validating.value).toBe(false)

    models.draft.value = ''
    models.attachments.value = []
    models.annotations.value = []
    chat.finishAbort()
    expect(chat.confirmFlowDialogOpen.value).toBe(true)
    expect(emit).not.toHaveBeenCalled()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish-abort')
    app.unmount()
  })

  it.each([false, true])('blocks abort-confirm during send acknowledgement even after clearing the draft (cold=%s)', async (coldSession) => {
    let complete!: (accepted: boolean) => void
    const sendRequest = vi.fn(() => new Promise<boolean>((resolve) => { complete = resolve }))
    const { chat, models, props, emit, app } = withChat({ confirmCanAbort: true, sendRequest })
    models.draft.value = '正在发送'
    const sending = chat.sendFromComposer()
    models.draft.value = ''
    props.coldSession = coldSession
    expect(chat.composerSending.value).toBe(true)
    chat.finishAbort()
    expect(emit).not.toHaveBeenCalled()

    complete(true)
    await sending
    chat.finishAbort()
    expect(emit).not.toHaveBeenCalled()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish-abort')
    app.unmount()
  })

  it('still allows abort-confirm while ordinary confirmation is blocked by a busy queue', () => {
    const { chat, emit, app } = withChat({ confirmCanAbort: true, finishDisabled: true })
    chat.thinking.value = true
    chat.queued.value.push({ text: '已排队', images: [], annotations: [] })
    expect(chat.confirmDisabled.value).toBe(true)
    chat.finishAbort()
    expect(chat.confirmFlowDialogOpen.value).toBe(true)
    expect(emit).not.toHaveBeenCalled()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish-abort')
    app.unmount()
  })

  it('allows cold abort-confirm without modifying hidden composer content', () => {
    const { chat, models, emit, app } = withChat({ coldSession: true, active: false, confirmCanAbort: true })
    models.draft.value = '隐藏的草稿'
    models.attachments.value = [image()]
    models.annotations.value = [annotation()]
    chat.finishAbort()
    expect(chat.confirmFlowDialogOpen.value).toBe(true)
    expect(chat.confirmFlowDiscard.value).toBe('none')
    expect(emit).not.toHaveBeenCalled()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish-abort')
    expect(models.draft.value).toBe('隐藏的草稿')
    expect(models.attachments.value).toEqual([image()])
    expect(models.annotations.value).toEqual([annotation()])
    app.unmount()
  })

  it('supports finish and confirm flows and releases validation on errors', async () => {
    const clarify = withChat()
    clarify.chat.finishEarly()
    expect(clarify.chat.confirmFlowDialogOpen.value).toBe(true)
    expect(clarify.chat.validating.value).toBe(false)
    expect(clarify.emit).not.toHaveBeenCalled()
    clarify.chat.acceptConfirmFlow()
    expect(clarify.emit).toHaveBeenCalledWith('finish')
    // Confirm-flow uses validating chrome, not thinking placeholder (plan g2.1).
    expect(clarify.chat.validating.value).toBe(true)
    expect(clarify.chat.thinking.value).toBe(false)
    clarify.chat.finishEarly()
    expect(clarify.emit).toHaveBeenCalledTimes(1)
    clarify.app.unmount()

    const review = withChat({ reviewMode: true })
    review.chat.finishEarly()
    expect(review.emit).not.toHaveBeenCalled()
    review.chat.acceptConfirmFlow()
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

  it('holds the sidebar on confirm click and releases when the window leaves', () => {
    __resetSidebarHandoffForTests()
    const dismissed = withChat()
    dismissed.chat.finishEarly()
    expect(isSidebarConfirmWindowHeld('run-1')).toBe(false)
    dismissed.chat.dismissConfirmFlow()
    expect(dismissed.emit).not.toHaveBeenCalled()
    expect(isSidebarConfirmWindowHeld('run-1')).toBe(false)
    dismissed.app.unmount()

    const accepted = withChat()
    accepted.chat.finishEarly()
    accepted.chat.acceptConfirmFlow()
    expect(accepted.emit).toHaveBeenCalledWith('finish')
    expect(isSidebarConfirmWindowHeld('run-1')).toBe(true)
    accepted.chat.releaseConfirmFlowWindow()
    expect(isSidebarConfirmWindowHeld('run-1')).toBe(false)
    accepted.app.unmount()

    const leftMounted = withChat()
    leftMounted.chat.finishEarly()
    leftMounted.chat.acceptConfirmFlow()
    expect(isSidebarConfirmWindowHeld('run-1')).toBe(true)
    leftMounted.app.unmount()
    expect(isSidebarConfirmWindowHeld('run-1')).toBe(false)
  })

  it.each(['text', 'image', 'annotation'])('keeps an unsent %s when confirmation is requested', (kind) => {
    const { chat, models, emit, app } = withChat({ annotateEnabled: true })
    if (kind === 'text') models.draft.value = '未发送的补充'
    if (kind === 'image') models.attachments.value = [image()]
    if (kind === 'annotation') models.annotations.value = [annotation()]
    const original = {
      text: models.draft.value,
      images: [...models.attachments.value],
      annotations: [...models.annotations.value],
    }

    expect(chat.confirmDisabled.value).toBe(true)
    chat.finishEarly()
    expect(emit).not.toHaveBeenCalled()
    expect(chat.validating.value).toBe(false)
    expect(models.draft.value).toBe(original.text)
    expect(models.attachments.value).toEqual(original.images)
    expect(models.annotations.value).toEqual(original.annotations)

    models.draft.value = ''
    models.attachments.value = []
    models.annotations.value = []
    expect(chat.confirmDisabled.value).toBe(false)
    chat.finishEarly()
    expect(chat.confirmFlowDialogOpen.value).toBe(true)
    expect(emit).not.toHaveBeenCalled()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish')
    app.unmount()
  })

  it('allows a cold session to confirm without touching hidden composer content', () => {
    const { chat, models, emit, props, app } = withChat({ coldSession: true, active: false, finishDisabled: true })
    models.draft.value = '隐藏的草稿'
    models.attachments.value = [image()]
    models.annotations.value = [annotation()]
    chat.finishEarly()
    expect(emit).not.toHaveBeenCalled()

    props.finishDisabled = false
    expect(chat.confirmDisabled.value).toBe(false)
    chat.finishEarly()
    expect(chat.confirmFlowDiscard.value).toBe('none')
    expect(models.draft.value).toBe('隐藏的草稿')
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish')
    expect(models.draft.value).toBe('隐藏的草稿')
    expect(models.attachments.value).toEqual([image()])
    expect(models.annotations.value).toEqual([annotation()])
    app.unmount()
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

  it('keeps attachment shape and notice semantics when selecting files', () => {
    class Reader {
      result: string | ArrayBuffer | null = null
      onload: (() => void) | null = null
      readAsDataURL(file: File) {
        this.result = `data:${file.type || 'application/octet-stream'};base64,YWJj`
        this.onload?.()
      }
    }
    vi.stubGlobal('FileReader', Reader)
    const { chat, app, models } = withChat()
    chat.addFiles(null)
    expect(models.attachments.value).toEqual([])
    expect(chat.attachNotice.value).toBeNull()

    const huge = new File(['x'], 'huge.bin')
    Object.defineProperty(huge, 'size', { value: 51 * 1024 * 1024 })
    chat.addFiles([huge] as unknown as FileList)
    expect(chat.attachNotice.value).toContain('huge.bin')
    expect(models.attachments.value).toEqual([])

    const plain = new File(['abc'], 'note', { type: '' })
    chat.addFiles([plain] as unknown as FileList)
    expect(chat.attachNotice.value).toBeNull()
    expect(models.attachments.value).toEqual([
      { data: 'YWJj', mimeType: 'application/octet-stream', name: 'note' },
    ])
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

  it('handles clarify placeholders, done state and empty seed images', async () => {
    const { chat, app, props } = withChat({ seedHumanImages: [image()] })
    expect(chat.showClarifyEmptyHint.value).toBe(false)
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

  it('settles a reply when the server ends its turn and keeps the waiter queued', () => {
    const { chat, app } = withChat()
    running('t1', { text: 'hello' }, { text: 'answer' })
    ended('t1', { text: 'hello' }, { text: 'answer' }, 'ok', [waiter('wait', 0, { text: 'wait' })])
    const agent = chat.displayTurns.value[1]!
    expect(agent.text).toBe('answer')
    expect(agent.streaming).toBeFalsy()
    expect(chat.liveAgentIdx.value).toBe(-1)
    expect(chat.queued.value.map((q) => q.id)).toEqual(['wait'])
    app.unmount()
  })

  it('keeps an empty reply as a failure once its turn ends (plan g1.1)', () => {
    const { chat, app } = withChat()
    running('t1', { text: 'goal' })
    ended('t1', { text: 'goal' }, { text: '' }, 'empty')
    const agent = chat.displayTurns.value[1]!
    expect(chat.displayTurns.value).toHaveLength(2)
    expect(agent.text).toBe('')
    expect(agent.streaming).toBeFalsy()
    expect(chat.isRetryableFailedAgent(agent)).toBe(true)
    expect(chat.showTurnCompleted(agent)).toBe(false)
    app.unmount()
  })

  it('retryLast emits retry-last without touching draft (plan g1.2)', async () => {
    const { chat, app, emit, models } = withChat()
    models.draft.value = 'keep-me'
    ended('t1', { text: 'goal' }, { text: '' }, 'empty')
    expect(chat.failRetryDisabled.value).toBe(false)
    chat.retryLastFailed()
    expect(emit).toHaveBeenCalledWith('retry-last')
    expect(models.draft.value).toBe('keep-me')
    expect(chat.thinking.value).toBe(true)
    expect(chat.displayTurns.value.filter((t) => t.role === 'human')).toHaveLength(1)
    app.unmount()
  })

  it('keeps failed Live operations on card recovery instead of text-only retry', () => {
    const { chat, app, emit } = withChat({
      turns: [
        { role: 'human', text: '采用当前变体', at: '2026-09-30T00:00:00Z', live: { sid: 'live-1', op: 'accept', variant: 2 } },
        { role: 'agent', text: '', at: '2026-09-30T00:00:01Z', outcome: 'empty' },
      ],
    })
    const failed = chat.displayTurns.value.at(-1)!
    expect(chat.isRetryableFailedAgent(failed)).toBe(true)
    expect(chat.showFailRetry(failed, 1)).toBe(false)
    chat.retryLastFailed()
    expect(emit).not.toHaveBeenCalledWith('retry-last')
    expect(chat.thinking.value).toBe(false)
    app.unmount()
  })

  it('does not show retry on success or interrupted turns (plan g2.3)', () => {
    const { chat, app } = withChat()
    ended('t1', { text: 'ok' }, { text: '正文' })
    const ok = chat.displayTurns.value[1]!
    expect(chat.showTurnCompleted(ok)).toBe(true)
    expect(chat.showFailRetry(ok, 1)).toBe(false)

    ended('t2', { text: 'cancel-me' }, { text: '(已中断)', interrupted: true }, 'interrupted')
    const agent = chat.displayTurns.value.at(-1)!
    expect(agent.interrupted).toBe(true)
    expect(agent.text).toBe('(已中断)')
    expect(chat.isRetryableFailedAgent(agent)).toBe(false)
    app.unmount()
  })

})

describe('acknowledged composer transport', () => {
  it('preserves draft, image and annotation on rejection without emitting an optimistic item', async () => {
    const sendRequest = vi.fn().mockResolvedValue(false)
    const { chat, models, emit, app } = withChat({ sendRequest })
    models.draft.value = 'three designs'
    models.attachments.value = [image()]
    models.annotations.value = [annotation()]
    expect(await chat.sendFromComposer()).toBe(false)
    expect(sendRequest).toHaveBeenCalledWith('three designs', [image()], [annotation()], expect.any(String))
    expect(models.draft.value).toBe('three designs')
    expect(models.attachments.value).toHaveLength(1)
    expect(models.annotations.value).toHaveLength(1)
    expect(chat.queued.value).toHaveLength(0)
    expect(emit).not.toHaveBeenCalledWith('send', expect.anything(), expect.anything(), expect.anything(), expect.any(String))
    expect(chat.confirmDisabled.value).toBe(true)
    chat.finishEarly()
    expect(emit).not.toHaveBeenCalledWith('finish')
    app.unmount()
  })

  it('blocks confirmation while awaiting send acknowledgement even if the editor is cleared', async () => {
    let resolve!: (accepted: boolean) => void
    const sendRequest = vi.fn(() => new Promise<boolean>((done) => { resolve = done }))
    const { chat, models, emit, app } = withChat({ sendRequest })
    models.draft.value = '正在提交的内容'
    const pending = chat.sendFromComposer()
    expect(chat.composerSending.value).toBe(true)

    // A user can edit or clear the composer while transport is pending. The
    // in-flight request must still prevent confirmation from overtaking it.
    models.draft.value = ''
    expect(chat.confirmDisabled.value).toBe(true)
    chat.finishEarly()
    expect(emit).not.toHaveBeenCalledWith('finish')

    resolve(false)
    expect(await pending).toBe(false)
    expect(chat.composerSending.value).toBe(false)
    expect(models.draft.value).toBe('')
    expect(chat.confirmDisabled.value).toBe(false)
    chat.finishEarly()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish')
    app.unmount()
  })

  it('waits for acknowledgement, prevents double sends and keeps edits made while waiting', async () => {
    let resolve!: (accepted: boolean) => void
    const sendRequest = vi.fn(() => new Promise<boolean>((done) => { resolve = done }))
    const { chat, models, app } = withChat({ sendRequest })
    models.draft.value = 'first request'
    models.attachments.value = [image()]
    models.annotations.value = [annotation()]
    const pending = chat.sendFromComposer()
    expect(chat.composerSending.value).toBe(true)
    expect(models.draft.value).toBe('first request')
    expect(await chat.sendFromComposer()).toBe(false)
    expect(sendRequest).toHaveBeenCalledTimes(1)
    models.draft.value = 'next request'
    models.attachments.value.push(image('next.png'))
    models.annotations.value.push(annotation({ selector: '#next' }))
    resolve(true)
    expect(await pending).toBe(true)
    expect(chat.composerSending.value).toBe(false)
    expect(models.draft.value).toBe('next request')
    expect(models.attachments.value).toHaveLength(2)
    expect(models.annotations.value).toHaveLength(2)
    app.unmount()
  })

  it('clears only the acknowledged snapshot', async () => {
    const { chat, models, app } = withChat({ sendRequest: vi.fn().mockResolvedValue(true) })
    models.draft.value = 'request'
    models.attachments.value = [image()]
    models.annotations.value = [annotation()]
    expect(await chat.sendFromComposer()).toBe(true)
    expect(models.draft.value).toBe('')
    expect(models.attachments.value).toEqual([])
    expect(models.annotations.value).toEqual([])
    app.unmount()
  })

  it('keeps a queued target and draft when the draft cannot be sent before editing', async () => {
    const { chat, models, app } = withChat({ sendRequest: vi.fn().mockResolvedValue(false) })
    feed({ busy: true, turns: [waiter('target', 0, { text: 'queued' })] })
    models.draft.value = 'keep draft'
    await chat.editQueuedItem(0)
    expect(models.draft.value).toBe('keep draft')
    expect(chat.queued.value.map((item) => item.id)).toEqual(['target'])
    app.unmount()
  })

  it('never edits a different queue item when the original was consumed during acknowledgement', async () => {
    let resolve!: (accepted: boolean) => void
    const { chat, models, emit, app } = withChat({ sendRequest: () => new Promise<boolean>((done) => { resolve = done }) })
    feed({ busy: true, turns: [waiter('target', 0, { text: 'target' }), waiter('other', 1, { text: 'other' })] })
    models.draft.value = 'new request'
    const editing = chat.editQueuedItem(0)
    feed({ busy: true, turns: [endedTurn('target', 'ok'), waiter('other', 0, { text: 'other' })] })
    resolve(true)
    await editing
    expect(chat.queued.value.map((item) => item.id ?? item.text)).toEqual(['other', 'new request'])
    expect(models.draft.value).toBe('')
    expect(emit).not.toHaveBeenCalledWith('queue-remove', 'other', 0)
    app.unmount()
  })
})

describe('confirm-flow dialog before clarify submit', () => {
  it('does not emit, discard, or play the ceremony until the dialog is confirmed', () => {
    const play = vi.fn(async () => {})
    let chat!: ReturnType<typeof useClarifyChat>
    const emit = vi.fn()
    const props = reactive({
      runId: 'run-1',
      nodeId: 'node-1',
      iteration: 0,
      turns: [],
      done: false,
      active: true,
    })
    const models = {
      draft: ref(''),
      attachments: ref<ClarifyImage[]>([]),
      annotations: ref<ReactAnnotation[]>([]),
    }
    const ceremony = {
      phase: ref('idle'),
      reduceMotion: ref(false),
      playToken: ref(0),
      playing: ref(false),
      play,
      reset: vi.fn(),
    } as ConfirmFlowCeremony
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
    app.provide(confirmFlowCeremonyKey, ceremony)
    app.mount(document.createElement('div'))

    chat.finishEarly()
    expect(chat.confirmFlowDialogOpen.value).toBe(true)
    expect(emit).not.toHaveBeenCalled()
    expect(play).not.toHaveBeenCalled()
    expect(chat.validating.value).toBe(false)

    chat.finishEarly()
    expect(emit).not.toHaveBeenCalled()
    chat.dismissConfirmFlow()
    expect(chat.confirmFlowDialogOpen.value).toBe(false)
    expect(emit).not.toHaveBeenCalled()
    expect(play).not.toHaveBeenCalled()

    chat.finishEarly()
    running('t1', { text: 'busy' })
    chat.acceptConfirmFlow()
    expect(emit).not.toHaveBeenCalled()
    expect(play).not.toHaveBeenCalled()
    expect(chat.confirmFlowDialogOpen.value).toBe(false)

    ended('t1', { text: 'busy' }, { text: 'done' })
    chat.finishEarly()
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledExactlyOnceWith('finish')
    // #850: the hand-off shows inline in the transcript, no full-desk ceremony.
    expect(play).not.toHaveBeenCalled()
    expect(chat.validating.value).toBe(true)
    app.unmount()
  })

  it('does not send a composer draft while validating or host handoff is pending', async () => {
    const { chat, models, emit, props, app } = withChat()
    models.draft.value = '校验中写下的草稿'
    chat.validating.value = true
    expect(await chat.sendFromComposer()).toBe(false)
    expect(emit).not.toHaveBeenCalled()
    expect(models.draft.value).toBe('校验中写下的草稿')
    chat.validating.value = false
    props.handoff = { phase: 'pending' }
    await nextTick()
    expect(await chat.sendFromComposer()).toBe(false)
    expect(emit).not.toHaveBeenCalled()
    expect(models.draft.value).toBe('校验中写下的草稿')
    props.handoff = null
    await nextTick()
    expect(await chat.sendFromComposer()).toBe(true)
    expect(emit).toHaveBeenCalledWith('send', '校验中写下的草稿', [], [], expect.any(String))
    app.unmount()
  })

  it('does not open the dialog when sending', async () => {
    const { chat, models, emit, app } = withChat()
    models.draft.value = '先发送'
    await chat.sendFromComposer()
    expect(emit).toHaveBeenCalledWith('send', '先发送', [], [], expect.any(String))
    expect(chat.confirmFlowDialogOpen.value).toBe(false)
    app.unmount()
  })
})

describe('accepted message reconciliation', () => {
  let mounted: ReturnType<typeof withChat> | undefined
  const setup = (over: Record<string, unknown> = {}) => (mounted = withChat(over))

  afterEach(() => {
    mounted?.app.unmount()
    mounted = undefined
    sessionStorage.clear()
  })

  it('acknowledges only the sender ID whose reply completed across identical viewer messages', async () => {
    const { chat, emit } = setup()
    chat.sendMessage('continue')
    const [ownId] = sentIds(emit)
    putClarifyOutbox('run-1', 'node-1', ownId!, { text: 'continue', images: [], annotations: [] })
    const remote = { text: 'continue', clientMessageId: 'remote-send' }
    const own = { text: 'continue', clientMessageId: ownId }
    feed({ rows: turnRows('remote', remote, [{ text: 'remote reply' }]), turns: [endedTurn('remote', 'ok', { clientMessageId: 'remote-send' })] })
    await nextTick()
    expect(takeClarifyOutbox('run-1', 'node-1', ownId, { own: true })?.text).toBe('continue')
    putClarifyOutbox('run-1', 'node-1', ownId!, { text: 'continue', images: [], annotations: [] })
    feed({
      rows: [...turnRows('remote', remote, [{ text: 'remote reply' }]), ...turnRows('own', own, [{ text: 'own reply' }])],
      turns: [endedTurn('remote', 'ok', { clientMessageId: 'remote-send' }), endedTurn('own', 'ok', { clientMessageId: ownId })],
    })
    await nextTick()
    expect(takeClarifyOutbox('run-1', 'node-1', ownId, { own: true })).toBeNull()
    expect(chat.displayTurns.value.filter(turn => turn.role === 'human')).toHaveLength(2)
    expect(chat.displayTurns.value.filter(turn => turn.role === 'agent').map(turn => turn.text).sort())
      .toEqual(['own reply', 'remote reply'])
  })

  it('restores only the refused send while a different turn is still generating', () => {
    const { chat, emit, models } = setup()
    running('active', { text: 'same text', clientMessageId: 'other-viewer' }, { text: 'active reply' })
    chat.sendMessage('same text')
    const [ownId] = sentIds(emit)
    chat.applyRequestRejected({ clientMessageId: ownId, message: 'queue full' })
    expect(models.draft.value).toBe('same text')
    expect(chat.queued.value).toEqual([])
    expect(chat.displayTurns.value.map(turn => turn.text)).toEqual(['same text', 'active reply'])
    expect(chat.displayTurns.value.at(-1)?.streaming).toBe(true)
    expect(chat.thinking.value).toBe(true)
    // Another viewer's refusal cannot refill our composer or finish the active turn.
    models.draft.value = ''
    chat.applyRequestRejected({ clientMessageId: 'foreign-rejected', message: 'not ours' })
    expect(models.draft.value).toBe('')
    expect(chat.displayTurns.value.at(-1)?.streaming).toBe(true)
  })

  it('discards a failed concurrent HTTP send by its ID without removing a later waiter', () => {
    const { chat, emit } = setup()
    running('active', { text: 'active' }, { text: 'active reply' })
    chat.sendMessage('same text')
    chat.sendMessage('same text')
    const ids = sentIds(emit)
    chat.discardLastQueued(ids[0])
    expect(chat.queued.value).toHaveLength(1)
    expect(chat.queued.value[0]?.clientMessageId).toBe(ids[1])
    expect(chat.displayTurns.value.filter(turn => turn.role === 'human').map(turn => turn.text)).toEqual(['active'])
    expect(chat.displayTurns.value.find(turn => turn.text === 'active reply')?.streaming).toBe(true)
  })

  it.each([false, true])('correlates an identical remote send while the local HTTP response returns %s', async (accepted) => {
    let acknowledge!: (ok: boolean) => void
    const sendRequest = vi.fn((_text, _images, _annotations, _clientMessageId) =>
      new Promise<boolean>((resolve) => { acknowledge = resolve }))
    const { chat, models } = setup({ sendRequest })
    models.draft.value = 'continue'
    const sending = chat.sendFromComposer()
    const ownId = sendRequest.mock.calls[0]![3] as string
    expect(ownId).toEqual(expect.any(String))
    // A second viewer sent an identical payload while our request was pending.
    const remote = { text: 'continue', clientMessageId: 'remote-send-id' }
    if (accepted) {
      const own = { text: 'continue', clientMessageId: ownId }
      feed({
        busy: true,
        rows: [...turnRows('remote', remote, [{ text: 'remote reply' }]), ...turnRows('local', own, [{ text: 'local reply', streaming: true }])],
        turns: [endedTurn('remote', 'ok', { clientMessageId: 'remote-send-id' }), runningTurn('local', own, { clientMessageId: ownId })],
      })
    } else {
      running('remote', remote, { text: 'remote reply' })
    }
    acknowledge(false)
    expect(await sending).toBe(accepted)
    expect(models.draft.value).toBe(accepted ? '' : 'continue')
    expect(chat.displayTurns.value.filter(turn => turn.role === 'human')).toHaveLength(accepted ? 2 : 1)
    expect(chat.displayTurns.value.at(-1)?.text).toBe(accepted ? 'local reply' : 'remote reply')
  })

  it.each(['streaming', 'finished'] as const)(
    'does not duplicate or reopen a %s turn when the HTTP acknowledgement arrives last',
    async (stage) => {
      let acknowledge!: (ok: boolean) => void
      const sendRequest = vi.fn((_text, _images, _annotations, _clientMessageId) =>
        new Promise<boolean>((resolve) => { acknowledge = resolve }))
      const { chat, models } = setup({ sendRequest })
      models.draft.value = 'acknowledged request'
      const sending = chat.sendFromComposer()
      const human = { text: 'acknowledged request', clientMessageId: sendRequest.mock.calls[0]![3] as string }
      if (stage === 'streaming') running('accepted-1', human, { text: 'answer already arrived' })
      else ended('accepted-1', human, { text: 'answer already arrived' })
      await nextTick()
      acknowledge(true)
      expect(await sending).toBe(true)
      await nextTick()

      expect(chat.displayTurns.value.filter((turn) => turn.role === 'human')).toHaveLength(1)
      expect(chat.displayTurns.value.filter((turn) => turn.role === 'agent')).toHaveLength(1)
      expect(chat.displayTurns.value.at(-1)?.text).toBe('answer already arrived')
      expect(chat.displayTurns.value.at(-1)?.streaming ?? false).toBe(stage === 'streaming')
      expect(chat.sessionBusy.value).toBe(stage === 'streaming')
      expect(models.draft.value).toBe('')
    },
  )

  it('accepts a send confirmed by the server dialogue when its HTTP transport reports failure', async () => {
    let acknowledge!: (ok: boolean) => void
    const sendRequest = vi.fn((_text, _images, _annotations, _clientMessageId) =>
      new Promise<boolean>((resolve) => { acknowledge = resolve }))
    const { chat, models } = setup({ sendRequest })
    models.draft.value = 'accepted despite HTTP failure'
    models.attachments.value = [image('accepted.png')]
    models.annotations.value = [annotation({ selector: '#accepted' })]
    const sending = chat.sendFromComposer()
    running('accepted', { text: 'accepted despite HTTP failure', clientMessageId: sendRequest.mock.calls[0]![3] as string },
      { text: 'confirmed server answer' })
    acknowledge(false)
    expect(await sending).toBe(true)
    expect(models.draft.value).toBe('')
    expect(models.attachments.value).toEqual([])
    expect(models.annotations.value).toEqual([])
    expect(chat.displayTurns.value.filter((turn) => turn.role === 'human')).toHaveLength(1)
    expect(chat.displayTurns.value.filter((turn) => turn.role === 'agent')).toHaveLength(1)
    expect(chat.displayTurns.value.at(-1)?.text).toBe('confirmed server answer')
    expect(chat.displayTurns.value.at(-1)?.streaming).toBe(true)
  })

  it('leaves shared composer models intact when an HTTP acknowledgement arrives after unmount', async () => {
    let acknowledge!: (ok: boolean) => void
    const { app, chat, models } = setup({
      sendRequest: () => new Promise<boolean>((resolve) => { acknowledge = resolve }),
    })
    models.draft.value = 'draft retained by the host'
    const retainedImage = image('retained.png')
    models.attachments.value = [retainedImage]
    const sending = chat.sendFromComposer()
    app.unmount()
    mounted = undefined
    acknowledge(true)
    expect(await sending).toBe(false)
    expect(models.draft.value).toBe('draft retained by the host')
    expect(models.attachments.value).toEqual([retainedImage])
  })

  it.each(['text', 'image', 'annotation'] as const)(
    'keeps distinct %s sends with identical text queued separately behind a running turn',
    async (kind) => {
      const { chat, models, emit } = setup()
      running('active', { text: 'currently running' })
      const body = kind === 'text' ? '继续' : ''
      for (let i = 1; i <= 2; i++) {
        models.draft.value = body
        models.attachments.value = kind === 'image' ? [image(`shot-${i}.png`)] : []
        models.annotations.value = kind === 'annotation' ? [annotation({ selector: `#item-${i}` })] : []
        expect(await chat.sendFromComposer()).toBe(true)
      }
      expect(emit.mock.calls.filter(([event]) => event === 'send')).toHaveLength(2)
      expect(chat.displayTurns.value.filter((turn) => turn.role === 'human')).toHaveLength(1)
      expect(chat.displayTurns.value.filter((turn) => turn.role === 'agent')).toHaveLength(1)
      expect(chat.queued.value).toHaveLength(2)
      expect(new Set(chat.queued.value.map((item) => item.clientMessageId)).size).toBe(2)
      if (kind === 'image') {
        expect(chat.queued.value.map((item) => item.images[0]?.name)).toEqual(['shot-1.png', 'shot-2.png'])
      }
      if (kind === 'annotation') {
        expect(chat.queued.value.map((item) => item.annotations[0]?.selector)).toEqual(['#item-1', '#item-2'])
      }
    },
  )

  it.each(['cancel', 'edit', 'refuse', 'reject'] as const)(
    'removes the accepted waiting row on %s without disturbing the active reply',
    async (action) => {
      const { chat, models, emit } = setup()
      const active = { text: 'active request' }
      running('active', active, { text: 'active reply' })
      models.draft.value = 'waiting request'
      models.attachments.value = [image('waiting.png')]
      models.annotations.value = [annotation({ selector: '#waiting' })]
      await chat.sendFromComposer()
      const [cmid] = sentIds(emit)
      running('active', active, { text: 'active reply' }, [waiter('waiting', 0, {
        text: 'waiting request', clientMessageId: cmid, images: [image('waiting.png')], annotations: [annotation({ selector: '#waiting' })],
      })])
      if (action === 'cancel') chat.cancelQueuedItem(0)
      if (action === 'edit') await chat.editQueuedItem(0)
      if (action === 'refuse') chat.restoreUnsentQueued({ text: 'waiting request' }, 'visit ended')
      if (action === 'reject') chat.discardLastQueued()
      running('active', active, { text: 'active reply' })
      await nextTick()

      expect(chat.queued.value).toHaveLength(0)
      expect(chat.displayTurns.value.map((turn) => turn.text)).toEqual(['active request', 'active reply'])
      expect(chat.displayTurns.value[chat.liveAgentIdx.value]?.text).toBe('active reply')
      expect(chat.displayTurns.value[chat.liveAgentIdx.value]?.streaming).toBe(true)
      if (action === 'cancel' || action === 'edit') expect(emit).toHaveBeenCalledWith('queue-remove', 'waiting', 0)
      if (action === 'edit' || action === 'refuse') {
        expect(models.draft.value).toBe('waiting request')
        expect(models.attachments.value[0]?.name).toBe('waiting.png')
        expect(models.annotations.value[0]?.selector).toBe('#waiting')
      }
    },
  )

  it.each(['cancel', 'edit'] as const)('identifies the selected same-text waiter on %s by its own queue item', async (action) => {
    const { chat, models, emit } = setup()
    const active = { text: 'active request' }
    running('active', active)
    for (const name of ['first.png', 'second.png']) {
      models.draft.value = 'same text'
      models.attachments.value = [image(name)]
      await chat.sendFromComposer()
    }
    const ids = sentIds(emit)
    running('active', active, {}, [
      waiter('first', 0, { text: 'same text', clientMessageId: ids[0], images: [image('first.png')] }),
      waiter('second', 1, { text: 'same text', clientMessageId: ids[1], images: [image('second.png')] }),
    ])
    if (action === 'cancel') chat.cancelQueuedItem(0)
    else await chat.editQueuedItem(0)
    expect(chat.queued.value.map((item) => item.id)).toEqual(['second'])
    expect(chat.queued.value[0]?.images[0]?.name).toBe('second.png')
    expect(chat.displayTurns.value.some((turn) => turn.text === 'same text')).toBe(false)
    if (action === 'edit') expect(models.attachments.value[0]?.name).toBe('first.png')
  })

  it.each(['runId', 'nodeId', 'iteration'] as const)(
    'isolates a pending send acknowledgement when the session %s changes',
    async (identity) => {
      const acknowledgements: ((ok: boolean) => void)[] = []
      const { chat, models, props } = setup({
        sendRequest: () => new Promise<boolean>((resolve) => { acknowledgements.push(resolve) }),
      })
      models.draft.value = 'old request'
      const oldSending = chat.sendFromComposer()
      feed({
        iteration: 5,
        busy: true,
        rows: turnRows('old-turn', { text: 'old request' }, [{ text: 'old session reply', streaming: true }]),
        turns: [runningTurn('old-turn', { text: 'old request' })],
      })
      if (identity === 'iteration') props.iteration += 1
      else props[identity] = `${props[identity]}-next`
      await nextTick()
      expect(chat.displayTurns.value).toHaveLength(0)
      expect(chat.queued.value).toHaveLength(0)
      expect(chat.sessionBusy.value).toBe(false)

      models.draft.value = 'new request'
      models.attachments.value = [image('new-session.png')]
      const newSending = chat.sendFromComposer()
      expect(chat.composerSending.value).toBe(true)
      acknowledgements[0]!(true)
      expect(await oldSending).toBe(false)
      expect(chat.composerSending.value).toBe(true)
      expect(models.draft.value).toBe('new request')
      expect(models.attachments.value[0]?.name).toBe('new-session.png')
      expect(chat.displayTurns.value).toHaveLength(0)

      acknowledgements[1]!(true)
      expect(await newSending).toBe(true)
      expect(chat.composerSending.value).toBe(false)
      expect(chat.displayTurns.value.filter((turn) => turn.role === 'human').map((turn) => turn.text))
        .toEqual(['new request'])
      expect(chat.displayTurns.value[0]?.images?.[0]?.name).toBe('new-session.png')
    },
  )

  it('ignores an old acknowledgement after leaving and returning to the same conversation', async () => {
    let acknowledge!: (ok: boolean) => void
    const { chat, models, props } = setup({
      sendRequest: () => new Promise<boolean>((resolve) => { acknowledge = resolve }),
    })
    models.draft.value = 'same draft text'
    const oldSending = chat.sendFromComposer()
    props.nodeId = 'other-node'
    await nextTick()
    props.nodeId = 'node-1'
    await nextTick()
    // A new edit can happen to have the same text as the request from the old visit.
    models.draft.value = 'same draft text'
    acknowledge(true)
    expect(await oldSending).toBe(false)
    expect(models.draft.value).toBe('same draft text')
    expect(chat.displayTurns.value).toHaveLength(0)
    expect(chat.sessionBusy.value).toBe(false)
  })

  it('restores an image-only refused send from its original attachment after server queue binding', async () => {
    const { chat, models, emit } = setup()
    const active = { text: 'active request' }
    running('active', active, { text: 'active reply' })
    const originalImage = image('waiting.png')
    const storedImage = image('waiting.png', { data: 'blob:waiting-asset' })
    models.attachments.value = [originalImage]
    expect(await chat.sendFromComposer()).toBe(true)
    const [cmid] = sentIds(emit)
    running('active', active, { text: 'active reply' }, [waiter('waiting-image', 0, { text: '', clientMessageId: cmid, images: [storedImage] })])
    chat.restoreUnsentQueued({ text: '', images: [storedImage] }, 'visit ended')
    running('active', active, { text: 'active reply' })
    await nextTick()
    expect(chat.queued.value).toHaveLength(0)
    expect(chat.displayTurns.value.map((turn) => turn.text)).toEqual(['active request', 'active reply'])
    expect(models.draft.value).toBe('')
    expect(models.attachments.value).toEqual([originalImage])
    expect(chat.displayTurns.value[chat.liveAgentIdx.value]?.streaming).toBe(true)
  })

  it('withdraws a server-confirmed waiter after remote cancellation without removing the interrupted active turn', async () => {
    const { chat, models, emit } = setup()
    const active = { text: 'active request' }
    running('active', active, { text: 'active partial reply' })
    models.draft.value = 'remotely cancelled waiter'
    await chat.sendFromComposer()
    const [cmid] = sentIds(emit)
    running('active', active, { text: 'active partial reply' }, [waiter('waiting', 0, { text: 'remotely cancelled waiter', clientMessageId: cmid })])
    // Another viewer stops review: server clears its waiting FIFO and ends A.
    feed({
      rows: turnRows('active', active, [{ text: 'active partial reply', interrupted: true }]),
      turns: [endedTurn('active', 'interrupted'), endedTurn('waiting', 'dropped', { clientMessageId: cmid })],
    })
    await nextTick()
    expect(chat.queued.value).toHaveLength(0)
    expect(chat.displayTurns.value.map((turn) => turn.text)).toEqual(['active request', 'active partial reply'])
    expect(chat.sessionBusy.value).toBe(false)
  })

  it('retries an accepted send that failed without duplicating its user or replacing the draft', async () => {
    const { chat, models, emit } = setup()
    models.draft.value = 'request that fails'
    await chat.sendFromComposer()
    const human = { text: 'request that fails', clientMessageId: sentIds(emit)[0] }
    ended('failed', human, { text: '(澄清回复失败:provider unavailable)', error: 'provider unavailable' }, 'failed')
    await nextTick()
    models.draft.value = 'next request draft'
    expect(chat.failRetryDisabled.value).toBe(false)
    chat.retryLastFailed()
    expect(emit).toHaveBeenCalledWith('retry-last')
    running('retry', human, { text: 'retry succeeded' })
    ended('retry', human, { text: 'retry succeeded' })
    expect(chat.displayTurns.value.filter((turn) => turn.role === 'human')).toHaveLength(1)
    expect(chat.displayTurns.value.at(-1)?.text).toBe('retry succeeded')
    expect(models.draft.value).toBe('next request draft')
  })

})

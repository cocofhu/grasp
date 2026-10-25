// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyTurn, ReactQuestion } from '@/lib/shared/types'
import { useClarifyChat } from './useClarifyChat'

const question: ReactQuestion = {
  id: 'next',
  prompt: '接下来选择哪一项？',
  allowMultiple: false,
  options: [{ id: 'yes', label: '继续' }],
}

function mountChat() {
  let chat!: ReturnType<typeof useClarifyChat>
  const props = reactive({
    runId: 'run', nodeId: 'clarify', iteration: 0,
    active: true, done: false, turns: [] as ClarifyTurn[],
  })
  const app = createApp(defineComponent({
    setup() {
      chat = useClarifyChat(props, vi.fn(), {
        draft: ref(''), attachments: ref([]), annotations: ref([]),
      })
      return () => null
    },
  }))
  app.use(createI18n({
    legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } },
  }))
  app.mount(document.createElement('div'))
  return { app, chat, props }
}

describe('clarify delayed queue snapshots', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 1 })
  })
  afterEach(() => vi.unstubAllGlobals())

  it('unlocks the next question after a delayed enqueue snapshot repeats the active item', async () => {
    const { app, chat, props } = mountChat()
    try {
      chat.sendMessage('我的选择\n第一题：继续')
      chat.applyReviewFrame({ event: 'queue_state', waiting: 1, items: [{ id: 'reply', text: '我的选择\n第一题：继续' }], busy: false })
      chat.applyReviewFrame({ event: 'queue_state', waiting: 0, items: [], busy: true })
      chat.applyReviewFrame({ event: 'turn_begin', item: { id: 'reply', text: '我的选择\n第一题：继续' } })
      chat.applyAcpEvents([{ kind: 'message', text: '已收到你的选择' }])
      // Enqueue can publish its captured queue after the pump already started
      // that item. A later turn_done must not retain it as a waiting reply.
      chat.applyReviewFrame({ event: 'queue_state', waiting: 1, items: [{ id: 'reply', text: '我的选择\n第一题：继续' }], busy: true })
      props.turns = [
        { role: 'human', text: '我的选择\n第一题：继续', at: '2026-10-25T00:00:00Z' },
        { role: 'agent', text: '已收到你的选择，请确认下一项。', at: '2026-10-25T00:00:01Z', questions: [question] },
      ]
      await nextTick()
      chat.applyReviewFrame({ event: 'turn_done' })
      expect(chat.queued.value).toHaveLength(0)
      expect(chat.thinking.value).toBe(false)
      expect(chat.liveAgentIdx.value).toBe(-1)
      expect(chat.activeQuestions.value).toEqual([question])
    } finally { app.unmount() }
  })

  it.each(['另一条回复', '继续'])('keeps a distinct waiting item with text %s when the active turn finishes', (waitingText) => {
    const { app, chat } = mountChat()
    try {
      chat.applyReviewFrame({ event: 'turn_begin', item: { id: 'active', text: '继续' } })
      chat.applyReviewFrame({
        event: 'queue_state', waiting: 2, busy: true,
        items: [{ id: 'active', text: '继续' }, { id: 'waiting', text: waitingText }],
      })
      chat.applyReviewFrame({ event: 'turn_done' })
      expect(chat.queued.value.map((item) => item.id)).toEqual(['waiting'])
      expect(chat.queued.value[0]?.text).toBe(waitingText)
      expect(chat.thinking.value).toBe(true)
      expect(chat.liveAgentIdx.value).toBe(-1)
    } finally { app.unmount() }
  })

  it('ignores a delayed waiting copy of the active item restored after refresh', () => {
    const { app, chat } = mountChat()
    try {
      chat.applyReviewFrame({
        event: 'queue_state', waiting: 0, items: [], busy: true,
        activeItem: { id: 'resumed', text: '继续' },
      })
      chat.applyReviewFrame({
        event: 'queue_state', waiting: 1, busy: true,
        items: [{ id: 'resumed', text: '继续' }],
      })
      expect(chat.queued.value).toHaveLength(0)
      chat.applyReviewFrame({ event: 'turn_done' })
      expect(chat.thinking.value).toBe(false)
      expect(chat.liveAgentIdx.value).toBe(-1)
    } finally { app.unmount() }
  })

  it.each(['turn_done', 'error'])('removes a completed item defensively on %s', (event) => {
    const { app, chat } = mountChat()
    try {
      chat.applyReviewFrame({ event: 'turn_begin', item: { id: 'active', text: '继续' } })
      chat.queued.value.push(
        { id: 'active', text: '继续', images: [], annotations: [] },
        { id: 'waiting', text: '继续', images: [], annotations: [] },
      )
      chat.applyReviewFrame({ event, message: event === 'error' ? 'failed' : undefined })
      expect(chat.queued.value.map((item) => item.id)).toEqual(['waiting'])
      expect(chat.thinking.value).toBe(true)
    } finally { app.unmount() }
  })

  it.each([false, true])('ignores a completed item in a delayed snapshot with activeItem=%s', (withActiveItem) => {
    const { app, chat } = mountChat()
    try {
      const item = { id: 'active', text: '继续' }
      chat.applyReviewFrame({ event: 'turn_begin', item })
      chat.applyReviewFrame({ event: 'turn_done' })
      chat.applyReviewFrame({
        event: 'queue_state', waiting: withActiveItem ? 0 : 1, busy: true,
        items: withActiveItem ? [] : [item],
        activeItem: withActiveItem ? item : undefined,
      })
      expect(chat.queued.value).toHaveLength(0)
      expect(chat.thinking.value).toBe(false)
      expect(chat.liveAgentIdx.value).toBe(-1)
    } finally { app.unmount() }
  })

  it('keeps successive question rounds interactive when settled live replies differ from persisted replies', async () => {
    const { app, chat, props } = mountChat()
    const ask = (round: number): ClarifyTurn => ({
      role: 'agent', text: `第 ${round} 轮问题`, at: `${round}`,
      questions: [{ ...question, id: `question-${round}` }],
    })
    try {
      props.turns = [ask(1)]
      await nextTick()
      for (let round = 1; round <= 2; round++) {
        const text = `我的选择:\n- 第 ${round} 轮问题 → 继续`
        const item = { id: `reply-${round}`, text }
        chat.sendMessage(text)
        chat.applyReviewFrame({ event: 'queue_state', waiting: 0, items: [], busy: true })
        chat.applyReviewFrame({ event: 'turn_begin', item })
        chat.applyAcpEvents([{ kind: 'message', text: '已收到选择，继续确认下一项。' }])
        props.turns = [
          ...props.turns,
          { role: 'human', text, at: `${round}` },
          ask(round + 1),
        ]
        await nextTick()
        chat.applyReviewFrame({ event: 'turn_done' })
        expect(chat.thinking.value).toBe(false)
        expect(chat.activeQuestions.value[0]?.id).toBe(`question-${round + 1}`)
        expect(chat.displayTurns.value.filter((turn) => turn.role === 'human')).toHaveLength(round)
      }
    } finally { app.unmount() }
  })

  it('keeps the newer live reply when pruning an earlier persisted live pair', async () => {
    const { app, chat, props } = mountChat()
    try {
      chat.applyReviewFrame({ event: 'turn_begin', item: { id: 'first', text: '第一项' } })
      chat.applyAcpEvents([{ kind: 'message', text: '第一项的流式结果' }])
      chat.applyReviewFrame({ event: 'turn_done' })
      chat.applyReviewFrame({ event: 'turn_begin', item: { id: 'second', text: '第二项' } })
      chat.applyAcpEvents([{ kind: 'message', text: '第二项的结果，以及尚未持久化的更多内容' }])
      props.turns = [
        { role: 'human', text: '第一项', at: '1' },
        { role: 'agent', text: '第一项的最终结果', at: '2' },
        { role: 'human', text: '第二项', at: '3' },
        { role: 'agent', text: '第二项的结果', at: '4' },
      ]
      await nextTick()
      expect(chat.liveTurns.value.map((turn) => turn.text)).toEqual([
        '第二项', '第二项的结果，以及尚未持久化的更多内容',
      ])
      expect(chat.liveAgentIdx.value).toBe(1)
      expect(chat.liveTurns.value[1]?.streaming).toBe(true)
      expect(chat.displayTurns.value.at(-1)?.text).toBe('第二项的结果，以及尚未持久化的更多内容')
    } finally { app.unmount() }
  })
})

// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyTurn, ReactQuestion } from '@/lib/shared/types'
import { useClarifyChat } from './useClarifyChat'
import { clearDialogues } from '@/lib/chat/dialogueStore'
import { endedTurn, feedDialogue, runningTurn, type RowInput } from '@/test/dialogueKit'

afterEach(() => clearDialogues())

const question: ReactQuestion = {
  id: 'next',
  prompt: '接下来选择哪一项？',
  allowMultiple: false,
  options: [{ id: 'yes', label: '继续' }],
}

function mountChat() {
  let chat!: ReturnType<typeof useClarifyChat>
  const emit = vi.fn()
  const props = reactive({
    runId: 'run', nodeId: 'clarify', iteration: 0,
    active: true, done: false, turns: [] as ClarifyTurn[],
  })
  const app = createApp(defineComponent({
    setup() {
      chat = useClarifyChat(props, emit, {
        draft: ref(''), attachments: ref([]), annotations: ref([]),
      })
      return () => null
    },
  }))
  app.use(createI18n({
    legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } },
  }))
  app.mount(document.createElement('div'))
  return { app, chat, props, emit }
}

describe('clarify question rounds from server dialogue', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 1 })
  })
  afterEach(() => vi.unstubAllGlobals())

  it('keeps successive question rounds interactive as each answered turn ends', async () => {
    const { app, chat, emit } = mountChat()
    const ask = (round: number, turnId?: string): RowInput => ({
      role: 'agent', text: `第 ${round} 轮问题`, at: `${round}`, turnId,
      questions: [{ ...question, id: `question-${round}` }],
    })
    try {
      let rows: RowInput[] = [ask(1)]
      feedDialogue('run', 'clarify', { rows })
      await nextTick()
      for (let round = 1; round <= 2; round++) {
        const text = `我的选择:\n- 第 ${round} 轮问题 → 继续`
        const turnId = `reply-${round}`
        chat.sendMessage(text)
        const clientMessageId = emit.mock.calls.at(-1)?.[4] as string
        const human: RowInput = { role: 'human', text, at: `${round}`, turnId, clientMessageId }
        feedDialogue('run', 'clarify', {
          busy: true,
          rows: [...rows, human, { role: 'agent', text: '已收到选择，继续确认下一项。', at: `${round}`, turnId, streaming: true }],
          turns: [runningTurn(turnId, { text, clientMessageId })],
        })
        await nextTick()
        expect(chat.thinking.value).toBe(true)
        rows = [...rows, human, ask(round + 1, turnId)]
        feedDialogue('run', 'clarify', { rows, turns: [endedTurn(turnId, 'ok')] })
        await nextTick()
        expect(chat.thinking.value).toBe(false)
        expect(chat.queued.value).toHaveLength(0)
        expect(chat.activeQuestions.value[0]?.id).toBe(`question-${round + 1}`)
        expect(chat.displayTurns.value.filter((turn) => turn.role === 'human')).toHaveLength(round)
      }
    } finally { app.unmount() }
  })
})

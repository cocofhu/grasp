import '../src/styles/global.css'
import { createApp, defineComponent, h, nextTick, onMounted, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'
import type { ClarifyTurn } from '../src/lib/shared/types'
import type { DialogueOutcome } from '../src/lib/shared/types'
import { initClarifyTab } from '../src/lib/inbox/clarifyDraftStash'
import { clearDialogues, dialogueKey, ingestDialogue } from '../src/lib/chat/dialogueStore'
import type { DialogueTurn } from '../src/lib/chat/dialogueState'
import { dialogueFrame, endedTurn, queuedTurn, runningTurn, type RowInput } from '../src/test/dialogueKit'

initLocale()
setTheme('light')
void initClarifyTab()

/**
 * Clarify (non-reviewMode) harness — Demo Cancel keep-queue + refresh resume.
 * A small fake server owns the dialogue: every change is published as a full
 * dialogue state, as the platform does over the run socket.
 */
const RUN_ID = 'run-clarify-e2e'
const NODE_ID = 'clarify'
const JIA = '澄清意见甲（队列首条）'
const YI = '澄清意见乙（仍在队列）'

type Turn = { turnId: string; text: string; clientMessageId?: string; thought?: string; reply?: string }

const App = defineComponent({
  name: 'ClarifySessionUxHarness',
  setup() {
    const turns = ref<ClarifyTurn[]>([])
    const hostSkipRefresh = ref(0)
    const hostDidRefresh = ref(0)
    const chatRef = ref<{
      isSessionBusy?: () => boolean
      restoreUnsentQueued?: (sent: { text: string }, notice: string) => void
    } | null>(null)
    /** Messages the chat emitted for sending (host would POST them). */
    const sent: { text: string; clientMessageId: string }[] = []
    /** Run open for input (false = run cancelled / ended, true = resumed). */
    const runActive = ref(true)
    let choiceRound = 0

    let history: RowInput[] = []
    let live: Turn | null = null
    let waiting: Turn[] = []
    let ended: DialogueTurn[] = []
    const at = () => new Date().toISOString()

    function humanRow(t: Turn): RowInput {
      return { role: 'human', text: t.text, at: at(), turnId: t.turnId, clientMessageId: t.clientMessageId }
    }
    function item(t: Turn) {
      return { text: t.text, clientMessageId: t.clientMessageId }
    }
    function publish() {
      const rows = [...history]
      const ts: DialogueTurn[] = [...ended]
      if (live) {
        rows.push(humanRow(live), {
          role: 'agent', text: live.reply ?? '', thought: live.thought, at: at(), turnId: live.turnId, streaming: true,
        })
        ts.push(runningTurn(live.turnId, item(live), { clientMessageId: live.clientMessageId }))
      }
      waiting.forEach((w, i) => ts.push(queuedTurn(w.turnId, i, item(w), { clientMessageId: w.clientMessageId })))
      ingestDialogue(dialogueFrame(RUN_ID, NODE_ID, { busy: !!live, rows, turns: ts }))
    }
    /** End the running turn; its reply rows are sealed with the outcome. */
    function endLive(outcome: DialogueOutcome, reply?: RowInput, error?: string) {
      if (!live) return
      history = [...history, humanRow(live), reply ?? {
        // Like the server: an interrupted turn with no output says so.
        role: 'agent', text: live.reply || (outcome === 'interrupted' ? '(已中断)' : ''), thought: live.thought, at: at(),
        turnId: live.turnId, outcome, interrupted: outcome === 'interrupted', ...(error ? { error } : {}),
      }]
      ended = [endedTurn(live.turnId, outcome, { clientMessageId: live.clientMessageId, ...(error ? { error } : {}) })]
      live = null
      publish()
    }
    function sentTurn(i: number, turnId: string, fallback: string): Turn {
      const s = sent[i]
      return s ? { turnId, text: s.text, clientMessageId: s.clientMessageId } : { turnId, text: fallback }
    }

    function choiceQuestion(round: number, turnId?: string): RowInput {
      return {
        role: 'agent',
        text: `第 ${round} 轮：请选择图标显示位置。`,
        at: at(),
        turnId,
        questions: [{
          id: `icon-scope-${round}`,
          prompt: `第 ${round} 轮图标显示范围`,
          options: [
            { id: 'solo', label: '仅在 Solo 选择器显示', recommended: true },
            { id: 'all', label: '所有 Agent 列表均显示' },
          ],
        }],
      }
    }

    /** The server runs the answer, then ends the turn with the next question. */
    async function completeChoiceRound(text: string, clientMessageId: string) {
      const turnId = `choice-reply-${choiceRound}`
      live = { turnId, text, clientMessageId, reply: '已收到选择，继续确认下一项。' }
      publish()
      await nextTick()
      endLive('ok', choiceQuestion(++choiceRound, turnId))
    }

    /** Host got 409 clarify_visit_ended for the last send (run ended). */
    function simVisitEnded() {
      const last = sent[sent.length - 1]
      chatRef.value?.restoreUnsentQueued?.({ text: last?.text ?? '' }, i18n.global.t('pages.clarify.visitEndedResend'))
    }
    /** Run cancelled while the reply was in flight: status push first, then the 409. */
    async function simCancelRace() {
      runActive.value = false
      await nextTick()
      simVisitEnded()
    }

    /** Host narrow-update gate mirror (RunDetail/GatesInbox busy skip). */
    function maybeHostRefresh(reason: string) {
      const busy = !!chatRef.value?.isSessionBusy?.()
      if (busy) {
        hostSkipRefresh.value += 1
        return
      }
      hostDidRefresh.value += 1
      void reason
    }

    let turnSeq = 0
    /** Drives the fake server from a spec: start a turn, stream into it, end it. */
    const server = {
      start(text: string) {
        live = { turnId: `spec-${++turnSeq}`, text }
        publish()
      },
      stream(reply: string, thought?: string) {
        if (!live) return
        live.reply = reply
        live.thought = thought
        publish()
      },
      end(outcome: DialogueOutcome, error?: string) {
        endLive(outcome, undefined, error)
      },
    }

    onMounted(async () => {
      await nextTick()
      const w = window as unknown as { __clarifyChat: typeof chatRef.value; __server: typeof server }
      w.__clarifyChat = chatRef.value
      w.__server = server
    })

    /** The server starts 甲 (乙 still waits) and streams its reply. */
    async function simTurnBeginStream() {
      live = sentTurn(0, 't-jia', JIA)
      waiting = [sentTurn(1, 't-yi', YI)]
      publish()
      await nextTick()
      // Host would see react WS here — must skip full refresh while busy.
      maybeHostRefresh('react')
      live.thought = '思考增量…'
      publish()
      await nextTick()
      live.reply = '流式产出正文（非整轮一次性）。'
      publish()
    }

    /** Two sends: 甲 runs with nothing streamed yet, 乙 waits. */
    async function simPumpOrderDual() {
      live = sentTurn(0, 't-jia', JIA)
      waiting = [sentTurn(1, 't-yi', YI)]
      publish()
      await nextTick()
      maybeHostRefresh('react')
    }

    /** Mid-stream the host's transcript catches up; the chat keeps one human row. */
    async function simTranscriptCatchUp() {
      live = { turnId: 't-jia', text: JIA, reply: '流式产出正文（非整轮一次性）。' }
      publish()
      await nextTick()
      turns.value = [{ role: 'human', text: JIA, at: at() }]
      await nextTick()
      maybeHostRefresh('react-mid-stream')
    }

    /** Page reload while the next queued turn runs: the loaded state resumes it. */
    async function simRefreshResume() {
      clearDialogues({ key: dialogueKey(RUN_ID, NODE_ID) })
      live = waiting.shift() ?? { turnId: 't-yi', text: YI }
      live.reply = '刷新后续上的流式正文。'
      publish()
    }

    /** Hard load: the running turn is known first, its recovered output a moment later. */
    async function simHardRefreshAcpRace() {
      live = { turnId: 't-race', text: '硬刷新竞态提问' }
      publish()
      await new Promise((r) => setTimeout(r, 30))
      live.thought = '竞态恢复的思考…'
      live.reply = '竞态恢复的正文续流。'
      publish()
      await nextTick()
      live.reply = '竞态恢复的正文续流。继续追加。'
      publish()
    }

    /** Full four-phase path ending in restrained completion footnote. */
    async function simFourPhaseComplete() {
      live = { turnId: 't-four', text: '四阶段提问' }
      publish()
      await nextTick()
      live.thought = '四阶段思考内容'
      publish()
      await nextTick()
      live.reply = '四阶段正文输出。'
      publish()
      await nextTick()
      endLive('ok')
    }

    return () =>
      h('div', { class: 'min-h-screen p-4 max-w-3xl mx-auto', 'data-testid': 'clarify-ux-root' }, [
        h('div', { class: 'flex gap-2 mb-3 flex-wrap' }, [
          h('button', {
            type: 'button',
            'data-testid': 'sim-choice-rounds',
            class: 'px-3 py-1.5 rounded border text-sm',
            onClick: () => {
              choiceRound = 1
              history = [choiceQuestion(choiceRound)]
              publish()
            },
          }, '模拟连续选择'),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-turn-stream',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => void simTurnBeginStream(),
            },
            '模拟 turn_begin+流式',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-pump-order-dual',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => void simPumpOrderDual(),
            },
            '模拟真实泵帧序',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-transcript-catchup',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => void simTranscriptCatchUp(),
            },
            '模拟 transcript 追上',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-refresh-resume',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => void simRefreshResume(),
            },
            '模拟刷新续传',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-hard-refresh-acp-race',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => void simHardRefreshAcpRace(),
            },
            '模拟硬刷新ACP竞态',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-four-phase-complete',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => void simFourPhaseComplete(),
            },
            '模拟四阶段完成',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-visit-ended',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => simVisitEnded(),
            },
            '模拟运行已结束(409)',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-cancel-race',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => void simCancelRace(),
            },
            '模拟取消时发送',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-resume',
              class: 'px-3 py-1.5 rounded border text-sm',
              onClick: () => {
                runActive.value = true
              },
            },
            '模拟续跑',
          ),
          h(
            'span',
            {
              'data-testid': 'host-skip-refresh',
              class: 'text-xs text-txt3 self-center',
            },
            `hostSkip=${hostSkipRefresh.value}`,
          ),
        ]),
        h(ClarifyChat, {
          ref: chatRef,
          runId: 'run-clarify-e2e',
          nodeId: 'clarify',
          iteration: 1,
          turns: turns.value,
          done: false,
          active: runActive.value,
          reviewMode: false,
          annotateEnabled: true,
          hideFinish: true,
          sendLabel: '发送澄清回复',
          onSend: (text: string, _images: unknown, _annotations: unknown, clientMessageId: string) => {
            sent.push({ text, clientMessageId })
            if (choiceRound > 0) void completeChoiceRound(text, clientMessageId)
          },
          // Stop interrupts the running turn; the queue keeps waiting.
          onCancel: () => endLive('interrupted'),
        }),
      ])
  },
})

createApp(App).use(i18n).mount('#app')

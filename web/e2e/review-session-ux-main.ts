import '../src/styles/global.css'
import { createApp, defineComponent, h, nextTick, onMounted, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'
import { ingestDialogue } from '../src/lib/chat/dialogueStore'
import { dialogueFrame, endedTurn, queuedTurn, runningTurn, type RowInput } from '../src/test/dialogueKit'

initLocale()
setTheme('light')

const App = defineComponent({
  name: 'ReviewSessionUxHarness',
  setup() {
    const surface = ref<'visual' | 'research'>('visual')
    const chatRef = ref<unknown>(null)
    /** Sends on the current surface (host would POST them). */
    let sent: { text: string; clientMessageId: string }[] = []
    let running = false

    onMounted(async () => {
      await nextTick()
      ;(window as unknown as { __reviewChat: typeof chatRef.value }).__reviewChat = chatRef.value
    })

    function switchSurface(next: 'visual' | 'research') {
      surface.value = next
      sent = []
      nextTick(() => {
        ;(window as unknown as { __reviewChat: typeof chatRef.value }).__reviewChat = chatRef.value
      })
    }

    function human(i: number): RowInput {
      return { role: 'human', text: sent[i]!.text, at: new Date().toISOString(), turnId: `t${i}`, clientMessageId: sent[i]!.clientMessageId }
    }
    function publish(spec: Parameters<typeof dialogueFrame>[2]) {
      ingestDialogue(dialogueFrame('run-e2e', surface.value, spec))
    }
    /** The server runs the first send (the second waits) and streams its reply. */
    async function simTurnBeginStream() {
      if (sent.length < 2) return
      running = true
      const turns = [
        runningTurn('t0', sent[0], { clientMessageId: sent[0]!.clientMessageId }),
        queuedTurn('t1', 0, sent[1]!, { clientMessageId: sent[1]!.clientMessageId }),
      ]
      const reply = (text: string, thought: string): RowInput =>
        ({ role: 'agent', text, thought, at: new Date().toISOString(), turnId: 't0', streaming: true })
      publish({ busy: true, rows: [human(0), reply('', '思考增量…')], turns })
      await nextTick()
      publish({ busy: true, rows: [human(0), reply('流式产出正文（非整轮一次性）。', '思考增量…')], turns })
    }
    /** Review Stop: the running turn is interrupted and the queue is cleared. */
    function onCancel() {
      if (!running) return
      running = false
      publish({
        rows: [human(0), { role: 'agent', text: '流式产出正文（非整轮一次性）。', at: new Date().toISOString(), turnId: 't0',
          interrupted: true, outcome: 'interrupted' }],
        turns: [
          endedTurn('t0', 'interrupted', { clientMessageId: sent[0]!.clientMessageId }),
          endedTurn('t1', 'dropped', { clientMessageId: sent[1]!.clientMessageId }),
        ],
      })
    }

    return () =>
      h('div', { class: 'min-h-screen p-4 max-w-3xl mx-auto', 'data-testid': 'review-ux-root' }, [
        h('div', { class: 'flex gap-2 mb-3' }, [
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'surface-visual',
              class:
                surface.value === 'visual'
                  ? 'px-3 py-1.5 rounded bg-accent text-white text-sm'
                  : 'px-3 py-1.5 rounded border text-sm',
              onClick: () => switchSurface('visual'),
            },
            '视觉复审',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'surface-research',
              class:
                surface.value === 'research'
                  ? 'px-3 py-1.5 rounded bg-accent text-white text-sm'
                  : 'px-3 py-1.5 rounded border text-sm',
              onClick: () => switchSurface('research'),
            },
            '调研复审',
          ),
          h(
            'button',
            {
              type: 'button',
              'data-testid': 'sim-turn-stream',
              class: 'px-3 py-1.5 rounded border text-sm ml-auto',
              onClick: () => void simTurnBeginStream(),
            },
            '模拟 turn_begin+流式',
          ),
        ]),
        h(ClarifyChat, {
          ref: chatRef,
          runId: 'run-e2e',
          nodeId: surface.value,
          iteration: 1,
          turns: [],
          done: false,
          active: true,
          reviewMode: true,
          onSend: (text: string, _images: unknown, _annotations: unknown, clientMessageId: string) => {
            sent.push({ text, clientMessageId })
          },
          onCancel,
        }),
      ])
  },
})

createApp(App).use(i18n).mount('#app')

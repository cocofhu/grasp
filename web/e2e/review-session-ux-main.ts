import '../src/styles/global.css'
import { createApp, defineComponent, h, nextTick, onMounted, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'

initLocale()
setTheme('light')

const App = defineComponent({
  name: 'ReviewSessionUxHarness',
  setup() {
    const surface = ref<'visual' | 'research'>('visual')
    const chatRef = ref<{
      applyReviewFrame?: (f: Record<string, unknown>) => void
      applyAcpEvents?: (e: { kind: string; text: string }[]) => void
    } | null>(null)

    onMounted(async () => {
      await nextTick()
      ;(window as unknown as { __reviewChat: typeof chatRef.value }).__reviewChat = chatRef.value
    })

    function switchSurface(next: 'visual' | 'research') {
      surface.value = next
      nextTick(() => {
        ;(window as unknown as { __reviewChat: typeof chatRef.value }).__reviewChat = chatRef.value
      })
    }

    async function simTurnBeginStream() {
      const c = chatRef.value
      if (!c?.applyReviewFrame) return
      const text =
        surface.value === 'visual' ? '视觉意见甲（队列首条）' : '调研意见甲（队列首条）'
      c.applyReviewFrame({
        event: 'turn_begin',
        item: { text },
        nodeId: surface.value,
      })
      await nextTick()
      c.applyAcpEvents?.([{ kind: 'thought', text: '思考增量…' }])
      await nextTick()
      c.applyAcpEvents?.([{ kind: 'message', text: '流式产出正文（非整轮一次性）。' }])
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
        }),
      ])
  },
})

createApp(App).use(i18n).mount('#app')

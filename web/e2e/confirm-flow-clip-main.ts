import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ReviewComposer from '../src/components/run/ReviewComposer.vue'

const params = new URLSearchParams(window.location.search)
const theme = params.get('theme') === 'light' ? 'light' : 'dark'
const hostH = Number(params.get('h') || '320')
const withError = params.get('error') === '1'
const cold = params.get('cold') === '1'
const tallInput = params.get('tall') === '1'

const Fixture = defineComponent({
  name: 'ConfirmFlowClipFixture',
  setup() {
    const draft = ref(
      tallInput
        ? Array.from({ length: 8 }, (_, i) => `多行输入第 ${i + 1} 行，拉高输入区以复现占高。`).join('\n')
        : '',
    )
    const turns = Array.from({ length: 12 }, (_, i) => ({
      role: (i % 2 === 0 ? 'agent' : 'human') as 'agent' | 'human',
      text: `消息 ${i + 1}：用于填满滚动区，确认底栏不被顶出。`,
      at: '2026-09-29T00:00:00Z',
    }))

    return () =>
      h(
        'div',
        {
          'data-testid': 'clip-host',
          // Mirrors GatesInbox card + ReviewShell sidebar: fixed height + overflow hidden.
          class: 'mx-auto flex flex-col overflow-hidden rounded-lg border border-line bg-surface',
          style: {
            width: '420px',
            height: `${hostH}px`,
          },
        },
        [
          h(ReviewComposer, {
            mode: 'clarify',
            runId: 'run-clip',
            nodeId: 'approve_1',
            iteration: 1,
            turns,
            done: false,
            active: !cold,
            canPass: true,
            coldSession: cold,
            pageControl: 'offline',
            confirmError: withError ? '产物契约不满足，请检查后重试' : null,
            draft: draft.value,
            'onUpdate:draft': (v: string) => {
              draft.value = v
            },
          }),
        ],
      )
  },
})

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme(theme)
  createApp({ render: () => h(Fixture) })
    .use(i18n)
    .mount('#app')
}

void boot()

import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'
import type { ClarifyTurn } from '../src/lib/shared/types'

async function bootstrap() {
  const params = new URLSearchParams(location.search)
  const english = params.get('lang') === 'en'
  await initLocale()
  await setLocale(english ? 'en' : 'zh-CN')
  setTheme(params.get('theme') === 'dark' ? 'dark' : 'light')
  const prompt = params.get('prompt') === 'short'
    ? (english ? 'Which area should we improve first?' : '这次优先完善哪个部分？')
    : (english
        ? 'Describe the expected result, actual behavior and reproduction steps for the selected area, including the affected environment and any evidence that helps us verify the fix. '
        : '请确认本次需要优化的功能区域，并补充期望效果、实际表现和复现步骤；如果涉及不同的运行环境，请说明环境差异以及能帮助我们验证修复的相关信息。')
      + ' https://example.test/workspace/' + 'long-path-without-spaces/'.repeat(8)
  const turns: ClarifyTurn[] = [{
    role: 'agent', text: english ? 'Please confirm the details.' : '请确认以下细节。',
    at: '2026-10-19T08:00:00Z',
    questions: [{
      id: 'scope', prompt, allowMultiple: params.get('multiple') === '1',
      options: [
        { id: 'chat', label: english ? 'Conversation layout' : '聊天布局', recommended: true },
        { id: 'settings', label: english ? 'Settings layout' : '设置布局' },
      ],
    }],
  }]
  const App = defineComponent({
    setup() {
      const submitted = ref('')
      return () => h('main', { class: 'app-shell-canvas flex h-screen min-w-0 flex-col' }, [
        h(ClarifyChat, {
          runId: 'run-question-layout', nodeId: 'clarify', iteration: 1,
          turns, done: false, reviewMode: true,
          onSend: (text: string) => { submitted.value = text },
          sendRequest: async (text: string) => { submitted.value = text; return true },
        }),
        h('output', { 'data-testid': 'question-layout-submission', class: 'sr-only' }, submitted.value),
      ])
    },
  })
  createApp(App).use(i18n).mount('#app')
}

void bootstrap()

import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import SoloWorkflowPanel from '../src/components/solo/SoloWorkflowPanel.vue'
import ChatComposer from '../src/components/chat/ChatComposer.vue'
import { useChatComposerDraft } from '../src/lib/chat/useChatComposerDraft'

async function bootstrap() {
  await initLocale(); await setLocale('zh-CN')
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await router.push('/')
  const Root = defineComponent({ setup() {
    const draft = useChatComposerDraft()
    return () => h('main', { class: 'mx-auto flex min-h-dvh w-full max-w-3xl flex-col gap-5 p-4' }, [
      h(SoloWorkflowPanel, { sessionId: 'solo-1' }),
      h('div', { class: 'mt-auto' }, h(ChatComposer, {
        draft, picks: [], placeholder: '继续 Solo 对话',
        scope: { sessionId: 'solo-1' }, testPrefix: 'resident-solo', pickTestId: 'resident-pick',
      })),
    ])
  } })
  createApp(Root).use(router).use(i18n).mount('#app')
}
void bootstrap()

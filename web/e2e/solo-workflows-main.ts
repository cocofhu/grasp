import '../src/styles/global.css'
import { computed, createApp, defineComponent, h, ref } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { useWorkflowFeature } from '../src/lib/composables/useWorkflowFeature'
import SoloWorkflowPanel from '../src/components/solo/SoloWorkflowPanel.vue'
import ChatComposer from '../src/components/chat/ChatComposer.vue'
import { useChatComposerDraft } from '../src/lib/chat/useChatComposerDraft'

async function bootstrap() {
  await initLocale(); await setLocale('zh-CN')
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await router.push('/')
  const Root = defineComponent({ setup() {
    const panel = ref<InstanceType<typeof SoloWorkflowPanel> | null>(null)
    const { enabled } = useWorkflowFeature()
    const draft = useChatComposerDraft()
    const commands = computed(() => enabled.value ? [{ id: 'workflow', label: '选择工作流', description: '选择并配置后手动启动' }] : [])
    return () => h('main', { class: 'mx-auto flex min-h-dvh w-full max-w-3xl flex-col gap-5 p-4' }, [
      h(SoloWorkflowPanel, { ref: panel, sessionId: 'solo-1' }),
      h('div', { class: 'mt-auto' }, h(ChatComposer, {
        draft, commands: commands.value, picks: [], placeholder: '继续 Solo 对话',
        scope: { sessionId: 'solo-1' }, testPrefix: 'resident-solo', pickTestId: 'resident-pick',
        onCommand: id => { if (id === 'workflow') panel.value?.openPicker() },
      })),
    ])
  } })
  createApp(Root).use(router).use(i18n).mount('#app')
}
void bootstrap()

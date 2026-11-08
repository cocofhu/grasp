import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { useWorkflowFeature } from '../src/lib/composables/useWorkflowFeature'
import SoloWorkflowPanel from '../src/components/solo/SoloWorkflowPanel.vue'

async function bootstrap() {
  await initLocale(); await setLocale('zh-CN')
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await router.push('/')
  const Root = defineComponent({ setup() {
    const panel = ref<InstanceType<typeof SoloWorkflowPanel> | null>(null)
    const { enabled } = useWorkflowFeature()
    return () => h('main', { class: 'mx-auto max-w-3xl space-y-5 p-4' }, [
      h('textarea', { 'data-testid': 'resident-solo-draft', class: 'input', placeholder: '继续 Solo 对话' }),
      enabled.value ? h('button', { 'data-testid': 'workflow-picker-open', class: 'btn', onClick: () => panel.value?.openPicker() }, '/workflow') : null,
      h(SoloWorkflowPanel, { ref: panel, sessionId: 'solo-1' }),
    ])
  } })
  createApp(Root).use(router).use(i18n).mount('#app')
}
void bootstrap()

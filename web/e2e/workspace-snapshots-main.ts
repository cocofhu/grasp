import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { useAuth } from '../src/lib/composables/useAuth'
import messages from '../src/locales/zh-CN/workspaceSnapshots.json'
import AppDrawer from '../src/components/ui/AppDrawer.vue'
import SoloWorkspaceSnapshots from '../src/components/solo/SoloWorkspaceSnapshots.vue'

async function bootstrap() {
  await initLocale(); await setLocale('zh-CN'); i18n.global.mergeLocaleMessage('zh-CN', messages)
  useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
  const runId = new URLSearchParams(location.search).has('workflow') ? 'run-1' : undefined
  const Root = defineComponent({ setup() {
    const open = ref(false), changes = ref(0)
    return () => h('main', { class: 'mx-auto max-w-3xl space-y-5 p-5' }, [
      h('h1', { class: 'text-xl font-semibold text-txt' }, 'Solo · 历史对话'),
      h('textarea', { class: 'input w-full', 'data-testid': 'resident-draft', placeholder: '继续对话' }),
      h('button', { class: 'btn', 'data-testid': 'snapshots-open', onClick: () => { open.value = true } }, '工作区与快照'),
      changes.value ? h('p', { 'data-testid': 'binding-changed', class: 'text-sm text-txt2' }, '工作区已切换') : null,
      h(AppDrawer, { open: open.value, title: '工作区与快照', width: 620, onClose: () => { open.value = false } }, { default: () => open.value ? h(SoloWorkspaceSnapshots, { sessionId: 'solo-1', sandboxId: 7, runId, nodes: [{ id: 'implementation', label: '实现' }], onBindingChanged: () => { changes.value++ } }) : null }),
    ])
  } })
  createApp(Root).use(i18n).mount('#app')
}
void bootstrap()

import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import messages from '../src/locales/zh-CN/workspaceRelease.json'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import SoloEnvironmentRelease, { type EnvironmentReleaseDraft } from '../src/components/solo/SoloEnvironmentRelease.vue'
import AppDrawer from '../src/components/ui/AppDrawer.vue'

async function bootstrap() {
  await initLocale(); await setLocale('zh-CN'); i18n.global.mergeLocaleMessage('zh-CN', messages)
  const Root = defineComponent({ setup() {
    const open = ref(false)
    const draft = ref<EnvironmentReleaseDraft | null>(null)
    const released = ref(false)
    return () => h('main', { class: 'mx-auto max-w-3xl space-y-5 p-5' }, [
      h('h1', { class: 'text-xl font-semibold text-txt' }, 'Solo · 保留的工作区'),
      h('p', { class: 'text-sm text-txt2' }, '会话已关闭，聊天和文件仍保留在原环境。'),
      h('textarea', { 'data-testid': 'resident-draft', class: 'input w-full', placeholder: '继续对话' }),
      h('button', { class: 'btn', 'data-testid': 'release-open', onClick: () => { open.value = true } }, '释放环境'),
      released.value ? h('p', { 'data-testid': 'released-status', class: 'text-sm text-txt2' }, '原环境已释放，聊天继续保留') : null,
      h(AppDrawer, { open: open.value, title: '释放环境', width: 480, onClose: () => { open.value = false } }, { default: () => open.value ? h(SoloEnvironmentRelease, { sessionId: 'solo-1', draft: draft.value, 'onUpdate:draft': value => { draft.value = value }, onChanged: () => { released.value = true } }) : null }),
    ])
  } })
  createApp(Root).use(i18n).mount('#app')
}
void bootstrap()

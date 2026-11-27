import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import WorkspaceChangesEntry from '../src/components/changes/WorkspaceChangesEntry.vue'

initLocale()
setTheme(new URLSearchParams(location.search).get('theme') === 'dark' ? 'dark' : 'light')
const root = defineComponent({
  setup() {
    const revision = ref(0)
    window.addEventListener('workspace-fixture-update', () => { revision.value++ })
    const labels = { implement: '实现', verify: '验证' }
    return () => h('main', { class: 'mx-auto max-w-4xl px-4 py-5' }, [
      h('header', { class: 'flex items-center justify-between border-b border-line pb-4' }, [
        h('h1', { class: 'text-sm font-semibold text-txt' }, '部署配置更新'),
        h(WorkspaceChangesEntry, { target: { kind: 'run', id: 'run-review' }, revision: String(revision.value), workspaceLabels: labels }),
      ]),
      h('section', { class: 'mx-auto mt-16 max-w-lg' }, [
        h(WorkspaceChangesEntry, { target: { kind: 'solo', id: 'solo-review', turnId: 'turn-a' } }),
        h('textarea', { class: 'w-full rounded-lg border border-line bg-surface p-3 text-sm text-txt', placeholder: '输入消息' }),
      ]),
    ])
  },
})
createApp(root).use(i18n).mount('#app')

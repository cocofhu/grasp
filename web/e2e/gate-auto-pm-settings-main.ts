import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import PmSettingsPanel from '../src/components/pm/PmSettingsPanel.vue'

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('light')

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { render: () => h('div') } }],
  })
  await router.push('/')

  createApp({
    render: () =>
      h('div', { class: 'mx-auto max-w-2xl', 'data-testid': 'gate-auto-settings-root' }, [
        h('h1', { class: 'mb-4 text-lg font-semibold' }, 'PM Leader 设置 · 门禁自动唤起'),
        h(PmSettingsPanel),
      ]),
  })
    .use(createPinia())
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

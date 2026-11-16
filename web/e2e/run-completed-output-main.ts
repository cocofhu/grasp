/**
 * Production RunDetailView harness for the completed-run deep link:
 * /run-completed-output.html?node=end&tab=output[&empty=1]
 */
import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import RunDetailView from '../src/views/RunDetailView.vue'

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const params = new URLSearchParams(window.location.search)
  const node = params.get('node') || ''
  const tab = params.get('tab') || ''
  const runId = params.get('run') || 'run-completed-e2e'
  const query: Record<string, string> = {}
  if (node) query.node = node
  if (tab) query.tab = tab

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/runs/:id', component: RunDetailView },
      { path: '/runs', component: { render: () => h('div') } },
      { path: '/workflows/:id/edit', component: { render: () => h('div') } },
      { path: '/login', component: { render: () => h('div', { 'data-testid': 'login-stub' }, 'login') } },
    ],
  })
  await router.push({ path: `/runs/${runId}`, query })

  createApp({ render: () => h(RouterView) })
    .use(createPinia())
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

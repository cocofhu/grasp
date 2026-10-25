import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import RunLiveView from '../src/views/RunLiveView.vue'

/** Real RunLiveView on /runs/:id/live (API and run WS mocked by the spec). */
async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  const stub = { render: () => h('div', { 'data-testid': 'left-live-page' }) }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/runs/:id/live', component: RunLiveView },
      { path: '/runs/:id', component: stub },
      { path: '/runs/:runId/nodes/:nodeId', component: stub },
      { path: '/:rest(.*)*', component: stub },
    ],
  })
  const q = new URLSearchParams(location.search)
  await router.push(`/runs/${q.get('run') || 'run-live-e2e'}/live?node=${q.get('node') || 'impl'}`)
  ;(window as unknown as { __router: typeof router }).__router = router
  createApp({ render: () => h('div', { class: 'h-screen' }, h(RouterView)) })
    .use(createPinia())
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

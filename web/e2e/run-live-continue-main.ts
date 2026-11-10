import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import RunLiveView from '../src/views/RunLiveView.vue'
import RunView from '../src/views/RunView.vue'

/** Real live and default Run routes (API and run WS mocked by the spec). */
async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  const stub = { render: () => h('div', { 'data-testid': 'left-live-page' }) }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/runs/:id/live', component: RunLiveView },
      { path: '/runs/:id', component: RunView },
      { path: '/runs/:runId/nodes/:nodeId', component: stub },
      { path: '/:rest(.*)*', component: stub },
    ],
  })
  const q = new URLSearchParams(location.search)
  if (q.has('observeSockets')) {
    // The switch regression replays an already queued socket callback after
    // navigation. Wrap Playwright's socket only after its init script ran.
    const observed: WebSocket[] = []
    ;(window as unknown as { __switchSockets: WebSocket[] }).__switchSockets = observed
    window.WebSocket = new Proxy(window.WebSocket, {
      construct(target, args) {
        const socket = Reflect.construct(target, args) as WebSocket
        if (String(args[0]).includes('/api/runs/')) observed.push(socket)
        return socket
      },
    })
  }
  const runId = q.get('run') || 'run-live-e2e'
  await router.push(q.has('history') ? `/runs/${runId}` : `/runs/${runId}/live?node=${q.get('node') || 'impl'}`)
  ;(window as unknown as { __router: typeof router }).__router = router
  createApp({ render: () => h('div', { class: 'h-screen' }, h(RouterView)) })
    .use(createPinia())
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

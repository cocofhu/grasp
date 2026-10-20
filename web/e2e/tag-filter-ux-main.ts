import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createWebHashHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import RunListView from '../src/views/RunListView.vue'
import GatesInboxView from '../src/views/GatesInboxView.vue'

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const router = createRouter({
    history: createWebHashHistory(),
    routes: [
      { path: '/runs', component: RunListView },
      { path: '/gates', component: GatesInboxView },
      {
        path: '/runs/:id',
        component: { render: () => h('div', { 'data-testid': 'run-detail-page' }, 'run-detail') },
      },
      { path: '/', redirect: '/runs' },
    ],
  })

  const params = new URLSearchParams(window.location.search)
  const page = params.get('page') === 'gates' ? '/gates' : '/runs'
  const startQuery: Record<string, string> = {}
  for (const key of ['tag', 'status', 'wf', 'sort', 'order']) {
    const v = params.get(key)
    if (v != null && v !== '') startQuery[key] = v
  }
  await router.push({ path: page, query: startQuery })

  createApp({ render: () => h(RouterView) })
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView, useRoute } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { useAuth } from '../src/lib/composables/useAuth'
import { useWorkspaceContext } from '../src/lib/composables/useWorkspaceContext'
import BoardView from '../src/views/BoardView.vue'
import DashboardView from '../src/views/DashboardView.vue'

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const params = new URLSearchParams(window.location.search)
  const startParam = params.get('start') || 'dashboard'
  useAuth().setUser({ username: 'board-preview', isAdmin: true, expiresAt: '2099-01-01T00:00:00Z' })
  const { ensureLoaded } = useWorkspaceContext()
  await ensureLoaded()

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', component: DashboardView },
      { path: '/workflows', component: { render: () => h('div', { 'data-testid': 'workflows-page' }, 'workflows') } },
      {
        path: '/board',
        component: { render: () => h(BoardView, { embedded: true }) },
      },
      {
        path: '/runs',
        component: {
          setup() {
            const route = useRoute()
            return () => {
              const status = typeof route.query.status === 'string' ? route.query.status : ''
              return h(
                'div',
                {
                  'data-testid': 'runs-page',
                  'data-status': status,
                },
                [
                  h('span', { 'data-testid': 'runs-status-query' }, status || '(none)'),
                ],
              )
            }
          },
        },
      },
      {
        path: '/runs/:id',
        component: {
          render: () => h('div', { 'data-testid': 'run-detail-page' }, 'run-detail'),
        },
      },
    ],
  })

  let start = '/dashboard'
  if (startParam === 'board') start = '/board'

  await router.push(start)

  createApp({ render: () => h(RouterView) })
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

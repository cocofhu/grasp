import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView, useRoute } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { PROJECT_CONTEXT_STORAGE_KEY } from '../src/lib/composables/useProjectContext'
import BoardView from '../src/views/BoardView.vue'
import DashboardView from '../src/views/DashboardView.vue'

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const params = new URLSearchParams(window.location.search)
  const startParam = params.get('start') || 'dashboard'
  const projectId = params.get('projectId') || 'proj-1'

  if (params.get('memory') === '1') {
    localStorage.setItem(PROJECT_CONTEXT_STORAGE_KEY, projectId)
  } else if (params.get('memory') === '0') {
    localStorage.removeItem(PROJECT_CONTEXT_STORAGE_KEY)
  }

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', component: DashboardView },
      { path: '/projects', component: { render: () => h('div', { 'data-testid': 'projects-page' }, 'projects') } },
      {
        path: '/projects/:id',
        component: {
          setup() {
            return () => h(BoardView, { projectId, embedded: true })
          },
        },
      },
      {
        path: '/runs',
        component: {
          setup() {
            const route = useRoute()
            return () => {
              const status = typeof route.query.status === 'string' ? route.query.status : ''
              const projectId =
                typeof route.query.projectId === 'string' ? route.query.projectId : ''
              return h(
                'div',
                {
                  'data-testid': 'runs-page',
                  'data-status': status,
                  'data-project-id': projectId,
                },
                [
                  h('span', { 'data-testid': 'runs-status-query' }, status || '(none)'),
                  h(
                    'span',
                    { 'data-testid': 'runs-project-id-query' },
                    projectId || '(none)',
                  ),
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
  if (startParam === 'project-board') start = `/projects/${projectId}`

  await router.push(start)

  createApp({ render: () => h(RouterView) })
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

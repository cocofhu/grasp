import '../src/styles/global.css'
import { createApp, h, defineComponent } from 'vue'
import { createMemoryHistory, createRouter, RouterView, useRoute } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { useAuth } from '../src/lib/composables/useAuth'
import ToastHost from '../src/components/ui/ToastHost.vue'
import DashboardView from '../src/views/DashboardView.vue'
import { takeHomeApproveHandoff } from '../src/lib/run/homeApproveHandoff'

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  useAuth().setUser({ username: 'home-preview', isAdmin: true, expiresAt: '2099-01-01T00:00:00Z' })

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', component: DashboardView },
      {
        path: '/gates',
        component: defineComponent({
          setup() {
            const route = useRoute()
            const seed = takeHomeApproveHandoff()
            return () =>
              h('div', { 'data-testid': 'gates-inbox-page' }, [
                h('span', { 'data-testid': 'gates-query-run' }, String(route.query.run || '')),
                h('span', { 'data-testid': 'gates-query-node' }, String(route.query.node || '')),
                h('span', { 'data-testid': 'gates-handoff-text' }, seed?.text || ''),
                h('span', { 'data-testid': 'gates-item-status' }, 'waiting_human'),
              ])
          },
        }),
      },
      {
        path: '/runs/:id',
        component: {
          render: () => h('div', { 'data-testid': 'run-detail-page' }, 'run detail'),
        },
      },
      {
        path: '/workflows/:id/edit',
        component: defineComponent({
          setup() {
            const route = useRoute()
            return () =>
              h('div', { 'data-testid': 'workflow-editor-page' }, String(route.params.id || ''))
          },
        }),
      },
    ],
  })

  await router.push('/dashboard')

  const Root = defineComponent({
    setup() {
      return () => h('div', [h(RouterView), h(ToastHost)])
    },
  })

  createApp(Root).use(i18n).use(router).mount('#app')
}

void bootstrap()

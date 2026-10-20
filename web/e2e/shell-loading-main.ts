import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { createMemoryHistory, createRouter, useRouter } from 'vue-router'
import { createPinia } from 'pinia'
import App from '../src/App.vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { installRoutePendingGuards } from '../src/lib/shared/routePending'
import { installAuthGuard } from '../src/lib/shared/authGuard'
import AppInlineError from '../src/components/ui/AppInlineError.vue'
import { shellNavPaths } from '../src/data/shellNavPaths'

installIdleScrollbar()

const params = new URLSearchParams(window.location.search)
const runsFail = params.get('scene') === 'fail-runs'

window.fetch = async (input: RequestInfo | URL) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  if (url.includes('/auth/me')) {
    return new Response(
      JSON.stringify({ username: 'e2e', expires_at: '2099-01-01T00:00:00Z', is_admin: true }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  if (url.includes('/health') || url.includes('/live')) {
    return new Response(JSON.stringify({ status: 'ok' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (url.includes('/gates')) {
    return new Response(JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  return new Response(JSON.stringify({}), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const DashboardPage = defineComponent({
    name: 'ShellLoadingDashboard',
    setup() {
      const router = useRouter()
      return () =>
        h('div', { 'data-testid': 'shell-main-dashboard' }, [
          '工作台内容',
          h(
            'button',
            { 'data-testid': 'go-runs', type: 'button', onClick: () => router.push('/runs') },
            '去运行',
          ),
        ])
    },
  })

  const RunsPage = defineComponent({
    name: 'ShellLoadingRuns',
    setup() {
      const failed = ref(runsFail)
      function retry() {
        failed.value = false
      }
      return () => {
        if (failed.value) {
          return h('div', { 'data-testid': 'shell-main-runs' }, [
            h(AppInlineError, { message: '加载失败', onRetry: retry }),
          ])
        }
        return h('div', { 'data-testid': 'shell-main-runs' }, '运行列表已恢复')
      }
    },
  })

  const DummyPage = defineComponent({
    setup() {
      return () => h('div', { 'data-testid': 'shell-main-dummy' }, 'ok')
    },
  })

  const LoginPage = defineComponent({
    setup() {
      return () => h('div', { 'data-testid': 'login-page' }, '登录')
    },
  })

  const extraRoutes = shellNavPaths()
    .filter((to) => to !== '/dashboard' && to !== '/runs')
    .map((to) => ({
      path: to,
      component: DummyPage,
    }))

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: LoginPage, meta: { titleKey: 'route.login', public: true, bare: true } },
      { path: '/', redirect: '/dashboard' },
      { path: '/dashboard', name: 'dashboard', component: DashboardPage, meta: { titleKey: 'route.dashboard' } },
      { path: '/runs', name: 'runs', component: RunsPage, meta: { titleKey: 'route.runs' } },
      ...extraRoutes,
    ],
  })

  installRoutePendingGuards(router)
  installAuthGuard(router)

  const start = params.get('start') === 'runs' ? '/runs' : '/dashboard'
  await router.push(start)

  createApp(App).use(createPinia()).use(i18n).use(router).mount('#app')
}

void bootstrap()

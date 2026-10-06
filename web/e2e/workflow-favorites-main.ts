import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, useRouter } from 'vue-router'
import { createPinia } from 'pinia'
import App from '../src/App.vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { installRoutePendingGuards } from '../src/lib/shared/routePending'
import { installAuthGuard } from '../src/lib/shared/authGuard'
import { shellNavPaths } from '../src/data/shellNavPaths'
import { useWorkflowFavorites, WORKFLOW_FAVORITES_MAX } from '../src/lib/run/useWorkflowFavorites'
import type { Workflow } from '../src/lib/shared/types'

installIdleScrollbar()

const params = new URLSearchParams(window.location.search)
const scene = params.get('scene') || 'with-items'
const username = params.get('user') || 'dev.li'

const WF_CATALOG: Record<string, Workflow> = {
  'wf-night': {
    id: 'wf-night',
    projectId: 'p-checkout',
    name: '夜间回归',
    description: '',
    status: 'draft',
    version: 1,
    updatedAt: '',
    needsRepo: false,
    nodes: [
      {
        id: 'in-1',
        type: 'input',
        label: '输入',
        position: { x: 0, y: 0 },
        config: {
          variables: [{ name: 'branch', desc: '分支', ask: true, type: 'string', value: 'main' }],
        },
      },
    ],
    edges: [],
  },
  'wf-hotfix': {
    id: 'wf-hotfix',
    projectId: 'p-checkout',
    name: '热修回滚',
    description: '',
    status: 'published',
    version: 2,
    updatedAt: '',
    needsRepo: false,
    nodes: [{ id: 'in-1', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: { variables: [] } }],
    edges: [],
  },
  'wf-bill': {
    id: 'wf-bill',
    projectId: 'p-billing',
    name: '发布预检',
    description: '',
    status: 'published',
    version: 3,
    updatedAt: '',
    needsRepo: false,
    nodes: [
      {
        id: 'in-1',
        type: 'input',
        label: '输入',
        position: { x: 0, y: 0 },
        config: {
          variables: [{ name: 'env', desc: '环境', ask: true, type: 'string', value: 'prod' }],
        },
      },
    ],
    edges: [],
  },
  'wf-pre': {
    id: 'wf-pre',
    projectId: 'p-checkout',
    name: '发布预检',
    description: '',
    status: 'published',
    version: 3,
    updatedAt: '',
    needsRepo: false,
    nodes: [],
    edges: [],
  },
  'wf-extra': {
    id: 'wf-extra',
    projectId: 'p-checkout',
    name: '金丝雀发布',
    description: '',
    status: 'published',
    version: 1,
    updatedAt: '',
    needsRepo: false,
    nodes: [],
    edges: [],
  },
}

for (let i = 0; i < WORKFLOW_FAVORITES_MAX; i++) {
  const id = `wf-full-${i}`
  WF_CATALOG[id] = {
    id,
    projectId: 'p-checkout',
    name: `满额工作流 ${i}`,
    description: '',
    status: 'published',
    version: 1,
    updatedAt: '',
    needsRepo: false,
    nodes: [],
    edges: [],
  }
}

function seedFavorites() {
  const key = `grasp.workflowFavorites.${username}`
  const seedKey = `workflow-favorites.seeded.${scene}.${username}`
  if (sessionStorage.getItem(seedKey) === '1') return
  localStorage.removeItem(key)
  sessionStorage.setItem(seedKey, '1')
  if (scene === 'empty') {
    localStorage.setItem(key, JSON.stringify([]))
    return
  }
  if (scene === 'full') {
    const list = Array.from({ length: WORKFLOW_FAVORITES_MAX }, (_, i) => ({
      workflowId: `wf-full-${i}`,
      favoritedAt: i + 1,
    }))
    localStorage.setItem(key, JSON.stringify(list))
    return
  }
  if (scene === 'gone') {
    localStorage.setItem(
      key,
      JSON.stringify([
        { workflowId: 'wf-night', favoritedAt: 2 },
        { workflowId: 'wf-missing', favoritedAt: 3 },
      ]),
    )
    return
  }
  localStorage.setItem(
    key,
    JSON.stringify([
      { workflowId: 'wf-night', favoritedAt: 5 },
      { workflowId: 'wf-hotfix', favoritedAt: 4 },
      { workflowId: 'wf-bill', favoritedAt: 1 },
    ]),
  )
}

seedFavorites()

window.fetch = async (input: RequestInfo | URL) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  if (url.includes('/auth/me')) {
    return new Response(
      JSON.stringify({ username, expires_at: '2099-01-01T00:00:00Z', is_admin: true }),
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
  if (url.includes('/projects') && !url.includes('/workflows')) {
    return new Response(
      JSON.stringify([
        { id: 'p-checkout', name: 'checkout-service' },
        { id: 'p-billing', name: 'billing-api' },
      ]),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  const wfMatch = url.match(/\/workflows\/([^/?#]+)/)
  if (wfMatch && !url.includes('/runs')) {
    const id = decodeURIComponent(wfMatch[1])
    const wf = WF_CATALOG[id]
    if (!wf) {
      return new Response(JSON.stringify({ error: 'not found' }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return new Response(JSON.stringify(wf), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (url.includes('/workflows/') && url.includes('/runs') && (input as Request).method === 'POST') {
    return new Response(JSON.stringify({ id: 'run-e2e-1' }), {
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
    name: 'FavDashboard',
    setup() {
      const router = useRouter()
      const fav = useWorkflowFavorites()
      return () =>
        h('div', { 'data-testid': 'shell-main-dashboard' }, [
          h('p', '概览 · 快捷区从任意页面可用'),
          h(
            'button',
            {
              'data-testid': 'demo-favorite-extra',
              type: 'button',
              onClick: () => fav.toggleFavorite('wf-extra', { name: '金丝雀发布' }),
            },
            '收藏金丝雀发布',
          ),
          h(
            'button',
            {
              'data-testid': 'demo-favorite-night',
              type: 'button',
              onClick: () => fav.toggleFavorite('wf-night', { name: '夜间回归' }),
            },
            '切换收藏夜间回归',
          ),
          h(
            'button',
            {
              'data-testid': 'go-runs',
              type: 'button',
              onClick: () => router.push('/runs'),
            },
            '去运行',
          ),
        ])
    },
  })

  const RunsPage = defineComponent({
    name: 'FavRuns',
    setup() {
      return () => h('div', { 'data-testid': 'shell-main-runs' }, '运行列表')
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

  await router.push('/dashboard')

  createApp(App).use(createPinia()).use(i18n).use(router).mount('#app')
}

void bootstrap()

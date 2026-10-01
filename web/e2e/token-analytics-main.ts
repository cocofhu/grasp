import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import App from '../src/App.vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { installRoutePendingGuards } from '../src/lib/shared/routePending'
import { installAuthGuard } from '../src/lib/shared/authGuard'
import TokenAnalyticsView from '../src/views/TokenAnalyticsView.vue'
import { shellNavPaths } from '../src/data/shellNavPaths'

installIdleScrollbar()

const MOCK_STATS = {
  window: '30d',
  bucketWidth: 'day',
  timezone: 'UTC',
  empty: false,
  kpi: {
    total: 9000,
    deltaPct: 10,
    inputTokens: 5000,
    outputTokens: 3000,
    cacheReadTokens: 800,
    cacheWriteTokens: 200,
    workflowTotal: 7000,
    pmTotal: 2000,
    projectCount: 1,
    runCount: 3,
    modelCount: 1,
    studioTotal: 0,
    failedTotal: 300,
    eventCount: 12,
    cacheHitRate: 0.133,
    avgPerRun: 3000,
    cost: 4.56,
    costDeltaPct: 8,
  },
  currency: 'USD',
  sources: [
    { key: 'workflow', name: 'workflow', total: 7000 },
    { key: 'pm', name: 'pm', total: 2000 },
  ],
  statuses: [
    { key: 'ok', name: 'ok', total: 8700 },
    { key: 'failed', name: 'failed', total: 300 },
  ],
  phases: [
    { key: 'production', name: 'production', total: 6000 },
    { key: 'interactive', name: 'interactive', total: 1000 },
    { key: 'chat', name: 'chat', total: 2000 },
  ],
  weekHour: Array.from({ length: 7 }, (_, d) => Array.from({ length: 24 }, (_, h) => (d < 5 && h >= 9 && h <= 19 ? (h * 37 + d * 11) % 400 : 0))),
  tree: [
    {
      key: 'p1',
      name: 'Demo',
      kind: 'project',
      value: 7000,
      children: [
        { key: 'w1', name: 'wf', kind: 'workflow', value: 5000, children: [{ key: 'agent', name: 'agent', kind: 'nodeType', value: 5000 }] },
        { key: '_pm', name: 'PM', kind: 'pm', value: 2000 },
      ],
    },
    { key: 'p2', name: 'Docs', kind: 'project', value: 2000, children: [{ key: 'w2', name: 'review', kind: 'workflow', value: 2000 }] },
  ],
  unpricedModels: ['m2'],
  trend: [
    {
      bucket: '2026-07-01',
      total: 9000,
      workflowTotal: 7000,
      pmTotal: 2000,
      inputTokens: 5000,
      outputTokens: 3000,
      cacheReadTokens: 800,
      cacheWriteTokens: 200,
    },
  ],
  prevTrend: [],
  composition: {
    total: 9000,
    inputTokens: 5000,
    outputTokens: 3000,
    cacheReadTokens: 800,
    cacheWriteTokens: 200,
  },
  projects: [
    { projectId: 'p1', name: 'Demo', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200, cost: 3.2, runCount: 2 },
    { projectId: 'p2', name: 'Docs', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0 },
  ],
  modelRanking: [
    { modelKey: 'm1', name: 'Model', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200, cost: 4.56 },
    { modelKey: 'm2', name: 'Model Mini', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0 },
  ],
  nodeTypes: [{ name: 'agent', total: 9000 }],
  workflows: [
    { workflowId: 'w1', name: 'wf', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200, kind: 'workflow' },
    { workflowId: 'w2', name: 'review', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0, kind: 'workflow' },
  ],
  heatmap: { rows: ['Model'], cols: ['Demo'], grid: [[9000]] },
  topRuns: [
    {
      runId: 'r1',
      title: 'Run',
      projectId: 'p1',
      projectName: 'Demo',
      workflowName: 'wf',
      modelKey: 'm1',
      modelName: 'Model',
      total: 9000,
    },
  ],
  projectTrends: [],
  modelTrends: [],
  filterOptions: {
    projects: [{ key: 'p1', name: 'Demo' }],
    models: [{ key: 'm1', name: 'Model' }],
  },
}

const MOCK_EVENTS = {
  total: 1,
  page: 1,
  pageSize: 20,
  currency: 'USD',
  items: [
    {
      id: 1,
      at: '2026-07-01T10:00:00Z',
      source: 'workflow',
      phase: 'production',
      status: 'ok',
      projectId: 'p1',
      projectName: 'Demo',
      runId: 'r1',
      runTitle: 'Run',
      nodeType: 'agent',
      modelKey: 'm1',
      total: 9000,
      inputTokens: 5000,
      outputTokens: 3000,
      cacheReadTokens: 800,
      cacheWriteTokens: 200,
      cost: 0,
      priced: false,
    },
  ],
}

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
  if (url.includes('/stats/platform-status')) {
    return new Response(
      JSON.stringify({ running: 0, waitingHuman: 0, failed: 0, completed: 0 }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  if (url.includes('/stats/dashboard')) {
    return new Response(
      JSON.stringify({ running: 0, waitingHuman: 0, failed: 0, completed: 0 }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  if (url.includes('/stats/token/events')) {
    return new Response(JSON.stringify(MOCK_EVENTS), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }
  if (url.includes('/stats/token/pricing')) {
    return new Response(JSON.stringify({ currency: 'USD', models: { m1: { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 } } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (url.includes('/stats/token')) {
    const parsed = new URL(url, 'http://localhost')
    const w = parsed.searchParams.get('window') || '30d'
    const bucketWidth = w === '24h' ? 'hour' : w === 'all' ? 'week' : 'day'
    return new Response(JSON.stringify({ ...MOCK_STATS, window: w, bucketWidth }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  if (url.includes('/gates')) {
    return new Response(
      JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }
  if (url.includes('/projects')) {
    return new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }
  return new Response(JSON.stringify({}), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const DashboardPage = defineComponent({
    name: 'TokenAnalyticsDashboard',
    setup() {
      return () => h('div', { 'data-testid': 'shell-main-dashboard' }, '工作台内容')
    },
  })

  const DummyPage = defineComponent({
    setup() {
      return () => h('div', { 'data-testid': 'shell-main-dummy' }, 'ok')
    },
  })

  const ProjectBoardPage = defineComponent({
    props: { id: { type: String, required: true } },
    setup(props) {
      return () => h('div', { 'data-testid': 'project-board-page' }, `board:${props.id}`)
    },
  })

  const RunDetailPage = defineComponent({
    props: { id: { type: String, required: true } },
    setup(props) {
      return () => h('div', { 'data-testid': 'run-detail-page' }, `run:${props.id}`)
    },
  })

  const LoginPage = defineComponent({
    setup() {
      return () => h('div', { 'data-testid': 'login-page' }, '登录')
    },
  })

  const known = new Set(['/dashboard', '/stats', '/login'])
  const extraRoutes = shellNavPaths()
    .filter((to) => !known.has(to))
    .map((to) => ({
      path: to,
      component: DummyPage,
    }))

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/login',
        name: 'login',
        component: LoginPage,
        meta: { titleKey: 'route.login', public: true, bare: true },
      },
      { path: '/', redirect: '/dashboard' },
      {
        path: '/dashboard',
        name: 'dashboard',
        component: DashboardPage,
        meta: { titleKey: 'route.dashboard' },
      },
      {
        path: '/stats',
        name: 'stats',
        component: TokenAnalyticsView,
        meta: { titleKey: 'route.stats' },
      },
      {
        path: '/projects/:id',
        name: 'project-detail',
        component: ProjectBoardPage,
        props: true,
        meta: { titleKey: 'route.projectDetail' },
      },
      {
        path: '/runs/:id',
        name: 'run-detail',
        component: RunDetailPage,
        props: true,
        meta: { titleKey: 'route.runDetail' },
      },
      ...extraRoutes,
    ],
  })

  installRoutePendingGuards(router)
  installAuthGuard(router)

  await router.push('/dashboard')

  createApp(App).use(createPinia()).use(i18n).use(router).mount('#app')
}

void bootstrap()

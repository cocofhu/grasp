import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { createWebHashHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import AppTopbar from '../src/components/shell/AppTopbar.vue'

installIdleScrollbar()

const params = new URLSearchParams(window.location.search)
const scene = params.get('scene') || 'ok'

type PlatformPayload = {
  cumulativeTokens: number | null
  todayTokens: number | null
  runningCount: number
  queuedCount: number
  asOf: string
  timezone: string
}

const okPayload: PlatformPayload = {
  cumulativeTokens: 1240582,
  todayTokens: 4812,
  runningCount: 3,
  queuedCount: 5,
  asOf: '2026-08-12T06:07:00Z',
  timezone: 'Asia/Shanghai',
}

const nullPayload: PlatformPayload = {
  cumulativeTokens: null,
  todayTokens: null,
  runningCount: 0,
  queuedCount: 0,
  asOf: '2026-08-12T00:00:00Z',
  timezone: 'UTC',
}

let failAfterFirst = scene === 'fail'
let callCount = 0
let lastSuccess: PlatformPayload = { ...okPayload }

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const originalFetch = window.fetch.bind(window)
window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  if (url.includes('/api/auth/me')) {
    return json({ username: 'admin', isAdmin: true, expiresAt: '2099-01-01T00:00:00Z' })
  }
  if (url.includes('/api/stats/platform-status')) {
    callCount += 1
    ;(window as unknown as { __platformStatusCalls?: number }).__platformStatusCalls = callCount
    if (scene === 'null') return json(nullPayload)
    if (failAfterFirst && callCount > 1) {
      return new Response('fail', { status: 500 })
    }
    if (scene === 'fail' && callCount === 1) {
      lastSuccess = { ...okPayload }
      return json(okPayload)
    }
    lastSuccess = scene === 'null' ? nullPayload : { ...okPayload }
    return json(lastSuccess)
  }
  if (url.includes('/api/runs')) {
    return json({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false })
  }
  if (url.includes('/api/')) {
    return json({})
  }
  return originalFetch(input, init)
}

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const router = createRouter({
    history: createWebHashHistory(),
    routes: [
      {
        path: '/',
        component: {
          render: () =>
            h('main', { class: 'p-6 text-sm text-txt2', 'data-testid': 'page-body' }, [
              'StatusMetrics E2E harness · scene=',
              scene,
            ]),
        },
      },
      {
        path: '/stats',
        name: 'stats',
        component: {
          render: () =>
            h('div', { 'data-testid': 'token-analytics-page' }, 'TokenAnalyticsView stub'),
        },
      },
      {
        path: '/runs',
        name: 'runs',
        component: {
          render: () => h('div', { 'data-testid': 'run-list-page' }, 'RunListView stub'),
        },
      },
    ],
  })
  await router.push('/')

  const Root = defineComponent({
    setup() {
      return () =>
        h('div', { class: 'min-h-screen bg-base text-txt' }, [h(AppTopbar), h(RouterView)])
    },
  })

  createApp(Root).use(i18n).use(router).mount('#app')
}

void bootstrap()

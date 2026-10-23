import { mergeConfig, type Plugin, type UserConfig } from 'vite'
import baseConfig from '/root/workspace/grasp/web/vite.config.ts'

const runs = [
  {
    id: 'run-wait-a1',
    workflowId: 'wf-1',
    workflowName: '产品需求流',
    workflowVersion: 3,
    title: '澄清登录文案',
    status: 'waiting_human',
    trigger: 'manual',
    priority: 'normal',
    startedAt: '2026-10-23T18:10:00Z',
    createdAt: '2026-10-23T18:09:00Z',
    durationSec: 420,
    progress: 0.17,
    currentNodeLabel: '需求澄清',
    tags: [],
    nodeRuns: {},
    artifacts: [],
  },
  {
    id: 'run-wait-b2',
    workflowId: 'wf-1',
    workflowName: '产品需求流',
    workflowVersion: 3,
    title: '确认列表配色',
    status: 'waiting_human',
    trigger: 'manual',
    priority: 'normal',
    startedAt: '2026-10-23T17:40:00Z',
    createdAt: '2026-10-23T17:39:00Z',
    durationSec: 900,
    progress: 0.42,
    currentNodeLabel: '需求澄清',
    tags: [],
    nodeRuns: {},
    artifacts: [],
  },
  {
    id: 'run-wait-c3',
    workflowId: 'wf-1',
    workflowName: '产品需求流',
    workflowVersion: 3,
    title: '核对阶段名称',
    status: 'waiting_human',
    trigger: 'manual',
    priority: 'normal',
    startedAt: '2026-10-23T16:20:00Z',
    createdAt: '2026-10-23T16:19:00Z',
    durationSec: 1500,
    progress: 0.63,
    currentNodeLabel: '需求澄清',
    tags: [],
    nodeRuns: {},
    artifacts: [],
  },
]

function json(res: import('node:http').ServerResponse, body: unknown, status = 200) {
  res.statusCode = status
  res.setHeader('Content-Type', 'application/json; charset=utf-8')
  res.end(JSON.stringify(body))
}

function previewPlugin(): Plugin {
  return {
    name: 'waiting-human-preview',
    transformIndexHtml(html) {
      const boot = `<script>(function(){try{localStorage.setItem('grasp-theme','light');localStorage.setItem('grasp-locale','zh-CN')}catch(e){}var p=location.pathname;if(p==='/'||p==='/dashboard')location.replace('/runs')})();</script>`
      return html.replace('<head>', '<head>' + boot)
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const raw = req.url || '/'
        const path = raw.split('?')[0]
        if (!path.startsWith('/api')) return next()
        const url = new URL(raw, 'http://preview.local')
        if (path === '/api/auth/me') {
          json(res, { username: 'preview', expires_at: '2099-01-01T00:00:00Z', is_admin: true })
          return
        }
        if (path === '/api/health' || path === '/api/live') {
          json(res, { status: 'ok', ready: true })
          return
        }
        if (path === '/api/workspace') {
          json(res, {
            name: '预览工作区',
            description: '',
            variables: [],
            workflowCount: 1,
            pmLeaderEnabled: false,
          })
          return
        }
        if (path === '/api/workflows' || path.startsWith('/api/workflows/')) {
          json(res, [
            {
              id: 'wf-1',
              name: '产品需求流',
              status: 'published',
              version: 3,
              nodes: [],
              edges: [],
            },
          ])
          return
        }
        if (path === '/api/agents') {
          json(res, [{ name: 'preview-agent' }])
          return
        }
        if (path === '/api/notifications') {
          json(res, { items: [], total: 0, unread: 0 })
          return
        }
        if (path === '/api/solo/sessions') {
          json(res, { sessions: [], maxSandboxes: 5 })
          return
        }
        if (path === '/api/runs' || path === '/api/runs/') {
          const status = url.searchParams.get('status') || ''
          const allowed = status ? new Set(status.split(',').filter(Boolean)) : null
          const items = allowed ? runs.filter((r) => allowed.has(r.status)) : runs
          const page = Math.max(1, Number(url.searchParams.get('page') || '1'))
          const pageSize = Math.max(1, Number(url.searchParams.get('pageSize') || '20'))
          const start = (page - 1) * pageSize
          json(res, { items: items.slice(start, start + pageSize), total: items.length })
          return
        }
        if (req.method === 'GET') {
          json(res, { items: [], total: 0 })
          return
        }
        json(res, { ok: true })
      })
    },
  }
}

const exported: UserConfig | ((env: { command: string; mode: string }) => UserConfig) = (env) => {
  const base = typeof baseConfig === 'function' ? baseConfig(env as never) : baseConfig
  return mergeConfig(base, {
    server: {
      host: '0.0.0.0',
      port: Number(process.env.PREVIEW_PORT) || 18080,
      strictPort: true,
    },
    plugins: [previewPlugin()],
  })
}

export default exported

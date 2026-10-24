import e2eConfig from './vite.e2e.config.ts'
import type { Plugin, UserConfig } from 'vite'

/** Preview-only: open the board harness at the site root. Not part of the app. */
function previewRun(partial: { id: string; status: string; title: string; progress?: number; durationSec?: number }) {
  return {
    id: partial.id,
    workflowId: 'wf-1',
    workflowName: '研发流程',
    title: partial.title,
    status: partial.status,
    trigger: 'manual',
    startedAt: '2026-07-18T12:00:00Z',
    durationSec: partial.durationSec ?? 120,
    progress: partial.progress ?? 40,
    currentNodeLabel: '实现',
    nodeRuns: {},
    artifacts: [],
  }
}

const previewRuns: Record<string, ReturnType<typeof previewRun>[]> = {
  running: [previewRun({ id: 'run-running-1', status: 'running', title: '看板需求-运行中' })],
  waiting_human: [previewRun({ id: 'run-waiting-1', status: 'waiting_human', title: '看板需求-等待人工', progress: 70 })],
  completed: [previewRun({ id: 'run-done-1', status: 'completed', title: '看板需求-已完成', progress: 100, durationSec: 600 })],
}

const redirectBoard: Plugin = {
  name: 'preview-board-root',
  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      const url = new URL(req.url || '/', 'http://preview.local')
      if (req.method === 'GET' && url.pathname === '/api/runs') {
        const status = url.searchParams.get('status') || ''
        const items = previewRuns[status] || []
        const pageSize = Number(url.searchParams.get('pageSize') || items.length || 20)
        res.setHeader('Content-Type', 'application/json')
        res.end(JSON.stringify({
          items,
          total: status === 'completed' ? 48 : items.length,
          page: 1,
          pageSize,
          hasMore: status === 'completed',
        }))
        return
      }
      if (req.method === 'GET' && url.pathname === '/api/workspace') {
        res.setHeader('Content-Type', 'application/json')
        res.end(JSON.stringify({ name: '工作区', description: '', variables: [] }))
        return
      }
      if (url.pathname === '/' || url.pathname === '/index.html') {
        res.statusCode = 302
        res.setHeader('Location', '/board.html?start=board')
        res.end()
        return
      }
      next()
    })
  },
}

const base = e2eConfig as UserConfig
const plugins = Array.isArray(base.plugins) ? base.plugins : []

export default {
  ...base,
  plugins: [redirectBoard, ...plugins],
  server: {
    ...base.server,
    host: '0.0.0.0',
    port: Number(process.env.PREVIEW_PORT) || 18080,
    strictPort: true,
  },
}

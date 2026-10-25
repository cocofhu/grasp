import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const web = '/root/workspace/grasp/web'
const waiting = {
  id: 'run-4d5cb057',
  workflowId: 'wf-1',
  workflowName: '自拟迭代',
  title: '在流转过程中更新会有BUG',
  status: 'waiting_human',
  priority: 'normal',
  trigger: 'manual',
  startedAt: '2026-10-25T11:48:00Z',
  durationSec: 157,
  progress: 0.16666666666666666,
  currentNodeLabel: '需求澄清',
  nodeRuns: {},
  artifacts: [],
}
const byStatus = {
  running: [],
  waiting_human: [waiting],
  completed: [],
}

export default defineConfig({
  root: `${web}/e2e`,
  plugins: [
    vue(),
    {
      name: 'board-preview-mock',
      transform(code, id) {
        const path = id.split('?')[0]
        if (!path.endsWith('board-main.ts') || code.includes('vHoverInk')) return null
        return code
          .replace(
            /import \{ i18n \} from ["']\.\.\/src\/lib\/shared\/i18n["'];?/,
            (line) => `${line}\nimport { vHoverInk } from "../src/lib/shared/hoverInkDirective";`,
          )
          .replace(
            '.use(router)',
            '.use(router).directive("hover-ink", vHoverInk)',
          )
      },
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          const url = req.url || ''
          // Rewriting req.url still leaves the browser on `/`. board-main.ts
          // reads window.location.search and defaults to the dashboard without
          // start=board, so the preview root must redirect.
          if (url === '/' || url.startsWith('/?')) {
            res.statusCode = 302
            res.setHeader('Location', '/board.html?start=board')
            res.end()
            return
          }
          if (!url.startsWith('/api/')) return next()
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          if (url.startsWith('/api/workspace')) {
            res.end(JSON.stringify({ name: '工作区', description: '', variables: [] }))
            return
          }
          if (url.startsWith('/api/runs')) {
            const status = new URL(url, 'http://local').searchParams.get('status') || ''
            const items = byStatus[status] || []
            res.end(JSON.stringify({ items, total: items.length, page: 1, pageSize: 100, hasMore: false }))
            return
          }
          res.end('{}')
        })
      },
    },
  ],
  resolve: {
    alias: { '@': `${web}/src` },
  },
  server: {
    host: '0.0.0.0',
    port: 18080,
    strictPort: true,
  },
})

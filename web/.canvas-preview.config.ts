import { defineConfig, type Plugin, type UserConfig } from 'vite'
import base from './vite.e2e.config.ts'
import { CANVAS_AGENTS } from './e2e/workflow-canvas-fixtures.ts'

const wf = {
  id: 'wf-canvas',
  name: '画布验收',
  description: '',
  projectId: 'proj-1',
  status: 'draft',
  version: 1,
  updatedAt: '2026-10-06T00:00:00Z',
  nodes: [
    { id: 'input', type: 'input', label: '输入', position: { x: 0, y: 80 }, config: { variables: [] } },
    { id: 'implement', type: 'agent', label: '实现', position: { x: 320, y: 80 }, config: { agent_profile: '实现', prompt: '' } },
  ],
  edges: [{ id: 'e1', source: 'input', target: 'implement' }],
}

const previewApi: Plugin = {
  name: 'preview-canvas-root',
  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      const raw = req.url || '/'
      const q = raw.indexOf('?')
      const path = q === -1 ? raw : raw.slice(0, q)
      const search = q === -1 ? '' : raw.slice(q)
      if (path === '/' || path === '/index.html') {
        req.url = `/workflow-canvas.html${search}`
        return next()
      }
      if (!path.startsWith('/api/')) return next()
      res.setHeader('Content-Type', 'application/json; charset=utf-8')
      if (path === '/api/workflows/wf-canvas' && req.method === 'GET') {
        res.end(JSON.stringify(wf))
        return
      }
      if (path === '/api/workflows/wf-canvas' && req.method === 'PUT') {
        res.end(JSON.stringify({ ...wf, version: wf.version + 1 }))
        return
      }
      if (path === '/api/agents') {
        res.end(JSON.stringify(CANVAS_AGENTS.map((a) => ({ ...a, files: [], mcp: [], env: {} }))))
        return
      }
      if (path === '/api/projects/proj-1') {
        res.end(JSON.stringify({ id: 'proj-1', name: 'Grasp' }))
        return
      }
      if (path.startsWith('/api/workflows')) {
        res.end('[]')
        return
      }
      res.end('{}')
    })
  },
}

const cfg = base as UserConfig

export default defineConfig({
  ...cfg,
  plugins: [...(cfg.plugins ?? []), previewApi],
  server: {
    ...(typeof cfg.server === 'object' ? cfg.server : {}),
    host: '0.0.0.0',
    port: Number(process.env.PREVIEW_PORT || 18080),
    strictPort: true,
  },
})

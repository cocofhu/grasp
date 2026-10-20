import { defineConfig, type Plugin } from 'vite'
import base from './vite.e2e.config.ts'

const redirect: Plugin = {
  name: 'preview-workspace-redirect',
  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      const path = (req.url || '').split('?')[0]
      if (path === '/' || path === '/index.html') {
        res.statusCode = 302
        res.setHeader('Location', '/workspace.html')
        res.end()
        return
      }
      next()
    })
  },
}

const plugins = Array.isArray(base.plugins) ? base.plugins.filter(Boolean) : []

export default defineConfig({
  ...base,
  plugins: [redirect, ...plugins],
  server: {
    ...(base.server ?? {}),
    host: '0.0.0.0',
    port: Number(process.env.PREVIEW_PORT || 18080),
    strictPort: true,
  },
})

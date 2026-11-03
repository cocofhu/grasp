import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const web = '/root/workspace/grasp/web'
export default defineConfig({
  root: `${web}/e2e`,
  plugins: [
    vue(),
    {
      name: 'preview-live-readonly',
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          const path = req.url?.split('?')[0]
          if (path === '/' || path === '/index.html') req.url = '/_preview-live-readonly.html'
          if (path?.startsWith('/api/runs/') && path.endsWith('/live-sessions')) {
            res.setHeader('Content-Type', 'application/json')
            res.end(JSON.stringify({ enabled: true, sessions: [] }))
            return
          }
          next()
        })
      },
    },
  ],
  resolve: { alias: { '@': `${web}/src` } },
  server: { host: '0.0.0.0', port: Number(process.env.PREVIEW_PORT || 18080), strictPort: true, fs: { allow: [web] } },
})

import { defineConfig, mergeConfig } from 'vite'
import e2e from './vite.e2e.config'

const port = Number(process.env.PREVIEW_PORT || process.env.GRASP_PREVIEW_PORT || 18083)

export default mergeConfig(e2e, defineConfig({
  server: {
    host: '0.0.0.0',
    port,
    strictPort: true,
  },
  plugins: [
    {
      name: 'root-to-workspace-board',
      configureServer(server) {
        server.middlewares.use((req, _res, next) => {
          const url = req.url || '/'
          if (url === '/' || url === '/index.html' || url.startsWith('/?')) {
            req.url = '/workspace.html?view=/board'
          }
          next()
        })
      },
    },
  ],
}))

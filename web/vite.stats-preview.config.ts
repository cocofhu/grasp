import { defineConfig, mergeConfig, type Plugin } from 'vite'
import e2eConfig from './vite.e2e.config'

const previewRoot: Plugin = {
  name: 'preview-stats-root',
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      const path = req.url?.split('?')[0]
      if (path === '/' || path === '/index.html') req.url = '/token-analytics.html'
      next()
    })
  },
}

export default mergeConfig(
  e2eConfig,
  defineConfig({
    plugins: [previewRoot],
    server: {
      host: '0.0.0.0',
      port: Number(process.env.PREVIEW_PORT) || 18080,
      strictPort: true,
    },
  }),
)

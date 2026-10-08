import { mergeConfig } from 'vite'
import base from './vite.e2e.config'

/**
 * Local preview only. Not part of the app and not committed.
 * Serves the logged-in shell harness at `/`.
 */
export default mergeConfig(base, {
  server: {
    host: '0.0.0.0',
    port: Number(process.env.PREVIEW_PORT) || 18080,
    strictPort: true,
  },
  plugins: [
    {
      name: 'shell-at-root',
      configureServer(server) {
        server.middlewares.use((req, _res, next) => {
          const raw = req.url || '/'
          const q = raw.indexOf('?')
          const path = q === -1 ? raw : raw.slice(0, q)
          const search = q === -1 ? '' : raw.slice(q)
          if (path === '/' || path === '/index.html') {
            req.url = `/shell-loading.html${search}`
          }
          next()
        })
      },
    },
  ],
})

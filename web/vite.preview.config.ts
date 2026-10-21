import { defineConfig, mergeConfig, type UserConfigExport } from 'vite'
import base from './vite.config'

const htmlFallback: import('vite').Plugin = {
  name: 'workflows-preview-entry',
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      const path = (req.url || '').split('?')[0]
      if (
        path.startsWith('/api') ||
        path.startsWith('/@') ||
        path.startsWith('/src') ||
        path.startsWith('/node_modules') ||
        path.startsWith('/fonts') ||
        path.includes('.')
      ) {
        next()
        return
      }
      req.url = '/preview.html'
      next()
    })
  },
}

export default defineConfig((env) => {
  const resolved = typeof base === 'function' ? (base as (e: typeof env) => UserConfigExport)(env) : base
  return mergeConfig(resolved, { plugins: [htmlFallback] })
})

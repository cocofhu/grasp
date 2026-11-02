import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

/** Untracked preview server: ReferencePicker with a slow fake search API at /. */
export default defineConfig({
  plugins: [
    vue(),
    {
      name: 'serve-ref-count-preview-at-root',
      configureServer(server) {
        server.middlewares.use((req, _res, next) => {
          const path = req.url?.split('?')[0]
          if (path === '/' || path === '/index.html') req.url = '/ref-count-preview.html'
          next()
        })
      },
    },
  ],
  resolve: {
    alias: [
      { find: '@/lib/api/api', replacement: fileURLToPath(new URL('./ref-count-mock-api.ts', import.meta.url)) },
      { find: '@', replacement: fileURLToPath(new URL('./src', import.meta.url)) },
    ],
  },
  optimizeDeps: { entries: ['ref-count-preview.html'] },
  server: {
    host: '0.0.0.0',
    port: Number(process.env.PREVIEW_PORT) || 18080,
    strictPort: true,
  },
})

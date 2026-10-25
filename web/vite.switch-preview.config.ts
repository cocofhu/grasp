import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

/** Untracked preview server: serve the real AppSwitch page at /. */
export default defineConfig({
  plugins: [
    vue(),
    {
      name: 'serve-switch-preview-at-root',
      configureServer(server) {
        server.middlewares.use((req, _res, next) => {
          const path = req.url?.split('?')[0]
          if (path === '/' || path === '/index.html') req.url = '/switch-hover-preview.html'
          next()
        })
      },
    },
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      vue: 'vue/dist/vue.esm-bundler.js',
    },
  },
  server: {
    host: '0.0.0.0',
    port: Number(process.env.PREVIEW_PORT) || 18080,
    strictPort: true,
  },
})

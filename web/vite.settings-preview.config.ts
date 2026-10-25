import { defineConfig, mergeConfig, type UserConfig } from 'vite'
import e2eConfig from './vite.e2e.config.ts'

const base = e2eConfig as UserConfig

export default mergeConfig(
  base,
  defineConfig({
    server: {
      host: '0.0.0.0',
      port: Number(process.env.PREVIEW_PORT) || 18080,
      strictPort: true,
    },
    plugins: [
      {
        name: 'settings-directory-preview',
        configureServer(server) {
          server.middlewares.use((req, res, next) => {
            const url = req.url || ''
            if (url === '/' || url === '/index.html') {
              res.statusCode = 302
              res.setHeader('Location', '/workspace.html?view=/settings')
              res.end()
              return
            }
            next()
          })
        },
      },
    ],
  }),
)

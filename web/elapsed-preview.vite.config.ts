import baseConfig from './vite.config.ts'
import { defineConfig, mergeConfig, type UserConfig } from 'vite'

/** Dev-only preview of the finished-turn fold. Not part of the app. */
export default defineConfig(async (env) => {
  const resolved = typeof baseConfig === 'function' ? await baseConfig(env) : baseConfig
  return mergeConfig(resolved as UserConfig, {
    plugins: [
      {
        name: 'elapsed-preview-root',
        configureServer(server) {
          server.middlewares.use((req, res, next) => {
            const path = (req.url || '').split('?')[0]
            if (req.method === 'GET' && (path === '/' || path === '/index.html')) {
              res.statusCode = 200
              res.setHeader('Content-Type', 'text/html; charset=utf-8')
              res.end(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>一轮结束后的用时</title>
</head>
<body>
<div id="app"></div>
<script type="module" src="/e2e/agent-tool-timeline-main.ts"></script>
</body>
</html>`)
              return
            }
            next()
          })
        },
      },
    ],
  })
})

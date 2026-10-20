import { createServer } from 'vite'

const port = Number(process.env.PREVIEW_PORT || 18080)
const target =
  '/e2e/cli-model-select.html?view=meta&backend=cursor&shared=grok-4.7-xhigh&theme=light'

const server = await createServer({
  configFile: new URL('./vite.config.ts', import.meta.url).pathname,
  server: { host: '0.0.0.0', port, strictPort: true },
  plugins: [
    {
      name: 'preview-runtime-follow',
      configureServer(dev) {
        dev.middlewares.use((req, res, next) => {
          const path = (req.url || '').split('?')[0]
          if (path === '/' || path === '/index.html') {
            res.statusCode = 302
            res.setHeader('Location', target)
            res.end()
            return
          }
          next()
        })
      },
    },
  ],
})

await server.listen()
console.log(`preview listening on ${port}`)

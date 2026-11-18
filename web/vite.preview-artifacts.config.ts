import { mergeConfig, type Plugin } from 'vite'
import { readFileSync } from 'node:fs'
import base from './vite.e2e.config'

const media = (p: string) => readFileSync(new URL(`../media/${p}`, import.meta.url))
const bodies: Record<string, [Buffer | string, string]> = {
  report: ['# 已交付的版本\n\n报告正文。', 'text/markdown'],
  config: ['{"fast": true}', 'application/json'],
  image: [media('yume-mark.png'), 'image/png'],
  wide: [media('screenshots/artifact-review.webp'), 'image/webp'],
  small: ['<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24"><path d="M13 2 5 13h6l-2 9 10-13h-6z" fill="none" stroke="#71717a" stroke-width="2"/></svg>', 'image/svg+xml'],
  video: [media('grasp-demo.mp4'), 'video/mp4'],
  pdf: ['%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\n%%EOF', 'application/pdf'],
  zip: ['archive-data', 'application/octet-stream'],
}
const mock: Plugin = {
  name: 'artifact-preview-mock',
  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      if (req.url === '/') { res.statusCode = 302; res.setHeader('Location', '/artifact-files.html'); res.end(); return }
      const m = req.url?.match(/^\/api\/artifacts\/([^/]+)\/(raw|download)\?revision=\d+/)
      if (!m) return next()
      const row = bodies[m[1]]
      if (!row) { res.statusCode = 503; res.end('unavailable'); return }
      res.setHeader('Content-Type', row[1])
      if (m[2] === 'download') res.setHeader('Content-Disposition', `attachment; filename="${m[1]}.bin"`)
      res.end(row[0])
    })
  },
}
export default mergeConfig(base, { plugins: [mock] })

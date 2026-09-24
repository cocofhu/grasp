import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import type { IncomingMessage } from 'node:http'
import type { Duplex } from 'node:stream'
import { WebSocketServer } from 'ws'

const mockPick = {
  selector: '#demo-title',
  tagName: 'H1',
  outerHTML: '<h1 id="demo-title">Demo</h1>',
}

const mockSandbox = {
  id: 42,
  name: 'e2e-sandbox',
  profile: 'default',
  purpose: 'test',
  status: 'running',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  containerStatus: 'running',
  busy: false,
  connected: true,
  hasCodeServer: true,
  hasAcp: true,
  password: 'e2e-secret',
}

/** Detail modal (getSandbox) includes gateway endpoints; list omits them. */
const mockSandboxDetail = {
  ...mockSandbox,
  endpoints: {
    session: '10.8.2.14:30201',
    ide: '10.8.2.14:30202',
    ssh: '10.8.2.14:30222',
    cdp: '10.8.2.14:30203',
    novnc: '10.8.2.14:30204',
    '9222': '10.8.2.14:9222',
    '6080': '10.8.2.14:6080',
    '8080': '10.8.2.14:30880',
  },
}

let vncConnectCount = 0
let vncLastGoto = ''
/** Page boot calls /__e2e/opts so WS upgrades can honor delay/fail without relying on Referer. */
let e2eOpts = { connectDelayMs: 0, fail: false }

function handleMockVncUpgrade(
  req: IncomingMessage,
  socket: Duplex,
  head: Buffer,
  wss: WebSocketServer,
) {
  const { connectDelayMs, fail } = e2eOpts
  wss.handleUpgrade(req, socket, head, (ws) => {
    vncConnectCount++
    wss.emit('connection', ws, req)
    if (fail) {
      ws.send(JSON.stringify({ type: 'error', message: 'mock vnc unavailable' }))
      return
    }
    const sendReady = () => ws.send(JSON.stringify({ type: 'ready', url: 'http://mock-app:3000/' }))
    if (connectDelayMs > 0) setTimeout(sendReady, connectDelayMs)
    else sendReady()
    /** Deferred pick so on:false can cancel before forced pick masks toggle-off. */
    let pendingPick: ReturnType<typeof setTimeout> | null = null
    ws.on('message', (data, isBinary) => {
      if (isBinary) return
      const text = typeof data === 'string' ? data : data.toString()
      try {
        const msg = JSON.parse(text) as { type?: string; on?: boolean; action?: string; url?: string }
        if (msg.type === 'navigate' && msg.action === 'goto') {
          vncLastGoto = msg.url || ''
          return
        }
        if (msg.type !== 'inspect') return
        if (pendingPick != null) {
          clearTimeout(pendingPick)
          pendingPick = null
        }
        if (msg.on) {
          pendingPick = setTimeout(() => {
            pendingPick = null
            ws.send(JSON.stringify({ type: 'picked', pick: mockPick }))
          }, 300)
        }
        // on:false: cancel pending pick; do not force inspect-canceled (client already cleared)
      } catch {
        // ignore non-JSON client frames
      }
    })
  })
}

function handleTerminalUpgrade(req: IncomingMessage, socket: Duplex, head: Buffer, wss: WebSocketServer) {
  wss.handleUpgrade(req, socket, head, (ws) => {
    wss.emit('connection', ws, req)
    ws.on('message', () => {
      /* ignore resize/input in e2e */
    })
  })
}

export default defineConfig({
  root: fileURLToPath(new URL('./e2e', import.meta.url)),
  plugins: [
    vue(),
    {
      name: 'e2e-preview-mocks',
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          if (req.url?.startsWith('/__e2e/opts')) {
            try {
              const u = new URL(req.url, 'http://e2e.local')
              e2eOpts = {
                connectDelayMs: Number(u.searchParams.get('connectDelay') || 0),
                fail: u.searchParams.get('vncFail') === '1',
              }
              if (u.searchParams.get('resetCount') === '1') vncConnectCount = 0
            } catch {
              e2eOpts = { connectDelayMs: 0, fail: false }
            }
            res.setHeader('Content-Type', 'application/json')
            res.end(JSON.stringify({ ok: true, ...e2eOpts, count: vncConnectCount }))
            return
          }

          if (req.url?.match(/^\/api\/runs\/[^/]+\/nodes\/[^/]+\/previews/)) {
            res.setHeader('Content-Type', 'application/json')
            res.end(
              JSON.stringify({
                ports: [
                  { port: 3000, label: '前端', proxyUrl: 'http://mock-app:3000/' },
                  { port: 8080, label: 'API', proxyUrl: 'http://mock-api:8080/docs' },
                ],
              }),
            )
            return
          }

          const sandboxMatch = req.url?.match(/^\/api\/sandboxes\/(\d+)(?:\/(log))?\/?(\?.*)?$/)
          if (sandboxMatch && req.method === 'GET') {
            res.setHeader('Content-Type', 'application/json')
            if (sandboxMatch[2] === 'log') {
              res.end(JSON.stringify({ content: '', live: false, found: false }))
            } else {
              // getSandbox: include endpoints for detail modal; numeric port keys present for filter checks.
              res.end(JSON.stringify({ ...mockSandboxDetail, id: Number(sandboxMatch[1]) }))
            }
            return
          }
          if (req.url?.startsWith('/api/sandboxes') && req.method === 'GET') {
            res.setHeader('Content-Type', 'application/json')
            // List path intentionally omits endpoints (no gateway Get per row).
            res.end(JSON.stringify([mockSandbox]))
            return
          }

          if (req.url?.match(/^\/api\/workflows\/?(\?.*)?$/) && req.method === 'GET') {
            res.setHeader('Content-Type', 'application/json')
            res.end(
              JSON.stringify([
                {
                  id: 'wf-1',
                  name: 'Demo Workflow',
                  description: 'A demo description that may be long',
                  status: 'published',
                  version: 3,
                  updatedAt: '2026-01-01T00:00:00Z',
                  lastRunAt: '2026-01-02T00:00:00Z',
                  needsRepo: false,
                  nodes: [],
                  edges: [],
                },
                {
                  id: 'wf-2',
                  name: 'Draft Workflow',
                  description: '',
                  status: 'draft',
                  version: 1,
                  updatedAt: '2026-01-01T00:00:00Z',
                  needsRepo: true,
                  nodes: [],
                  edges: [],
                },
              ]),
            )
            return
          }

          if (req.url?.startsWith('/sandbox-bridge/') || req.url?.startsWith('/sandbox/')) {
            res.setHeader('Content-Type', 'text/html; charset=utf-8')
            res.end('<!doctype html><title>e2e stub</title><body>ok</body>')
            return
          }

          next()
        })

        const wss = new WebSocketServer({ noServer: true })
        server.middlewares.use((_req, res, next) => {
          if (_req.url === '/__e2e/vnc-connect-count') {
            res.setHeader('Content-Type', 'application/json')
            res.end(JSON.stringify({ count: vncConnectCount }))
            return
          }
          if (_req.url === '/__e2e/vnc-last-goto') {
            res.setHeader('Content-Type', 'application/json')
            res.end(JSON.stringify({ url: vncLastGoto }))
            return
          }
          next()
        })
        server.httpServer?.on('upgrade', (req, socket, head) => {
          if (req.url?.startsWith('/preview-vnc/') || req.url?.match(/^\/sandbox-vnc\/\d+\/ws/)) {
            handleMockVncUpgrade(req, socket, head, wss)
            return
          }
          if (req.url?.match(/^\/api\/sandboxes\/\d+\/terminal/) || req.url?.match(/^\/sandboxes\/\d+\/terminal/)) {
            handleTerminalUpgrade(req, socket, head, wss)
            return
          }
        })
      },
    },
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@novnc/novnc/lib/rfb.js': fileURLToPath(new URL('./e2e/mocks/novnc-rfb.ts', import.meta.url)),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 5174,
    strictPort: true,
  },
  build: {
    rollupOptions: {
      input: {
        main: fileURLToPath(new URL('./e2e/index.html', import.meta.url)),
        console: fileURLToPath(new URL('./e2e/console.html', import.meta.url)),
        'sandbox-list': fileURLToPath(new URL('./e2e/sandbox-list.html', import.meta.url)),
        'project-detail': fileURLToPath(new URL('./e2e/project-detail.html', import.meta.url)),
        'run-detail-mobile': fileURLToPath(new URL('./e2e/run-detail-mobile.html', import.meta.url)),
        'run-detail-mobile-panel': fileURLToPath(
          new URL('./e2e/run-detail-mobile-panel.html', import.meta.url),
        ),
        'run-detail-real': fileURLToPath(new URL('./e2e/run-detail-real.html', import.meta.url)),
        'run-completed-output': fileURLToPath(
          new URL('./e2e/run-completed-output.html', import.meta.url),
        ),
        'gate-mobile-fill': fileURLToPath(new URL('./e2e/gate-mobile-fill.html', import.meta.url)),
        'gate-share-link': fileURLToPath(new URL('./e2e/gate-share-link.html', import.meta.url)),
        'gate-cold-silent': fileURLToPath(new URL('./e2e/gate-cold-silent.html', import.meta.url)),
        'clarify-inbox-product': fileURLToPath(
          new URL('./e2e/clarify-inbox-product.html', import.meta.url),
        ),
        'clarify-unread-fab': fileURLToPath(
          new URL('./e2e/clarify-unread-fab.html', import.meta.url),
        ),
        board: fileURLToPath(new URL('./e2e/board.html', import.meta.url)),
        'dashboard-home-chat': fileURLToPath(
          new URL('./e2e/dashboard-home-chat.html', import.meta.url),
        ),
        'run-list-trigger': fileURLToPath(new URL('./e2e/run-list-trigger.html', import.meta.url)),
        'structured-export-harness': fileURLToPath(
          new URL('./e2e/structured-export-harness.html', import.meta.url),
        ),
        'shell-loading': fileURLToPath(new URL('./e2e/shell-loading.html', import.meta.url)),
        'notifications-center': fileURLToPath(
          new URL('./e2e/notifications-center.html', import.meta.url),
        ),
        'token-analytics': fileURLToPath(new URL('./e2e/token-analytics.html', import.meta.url)),
      },
    },
  },
})

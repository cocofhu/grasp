import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { stripNovncSecureContext } from './vite-plugins/stripNovncSecureContext'

// The app is served at root "/" everywhere. Under a approving preview sandbox it
// is reverse-proxied at /preview/<runId>/<nodeId>/<port>/, but the platform's
// PreviewProxy transparently re-anchors HTML (injects <base>, rewrites root-
// absolute asset URLs), so the app needs no base/path awareness of its own.
export default defineConfig(({ command }) => {
  // GRASP_PREVIEW_PORT (the port the agent registers via set_preview) pins the
  // serve port so the platform proxy can reach it; defaults to Vite's usual
  // 5173 (dev) / 4173 (preview).
  const port = Number(process.env.GRASP_PREVIEW_PORT) || (command === 'serve' ? 5173 : 4173)
  return {
    base: '/',
    plugins: [vue(), stripNovncSecureContext()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    test: {
      environment: 'node',
      include: ['src/**/*.test.ts'],
      // Vitest 4: happy-dom omits window.confirm/alert/prompt; stub so vi.spyOn(window, ...) works.
      setupFiles: ['./src/test/vitest-window-stubs.ts', './vitest.setup.ts'],
      coverage: {
        provider: 'v8',
        reporter: ['text', 'text-summary', 'cobertura', 'json-summary'],
        reportsDirectory: './coverage',
        // Lines 硬门禁：不达标时 vitest 非零退出（ci-web 的 npm test -- --coverage）。
        // 仅约束 lines；branches/functions 不设阈值。本分支补充用例后保持 85%。
        thresholds: {
          lines: 85,
        },
        // 收窄分母：计入可测业务代码（含全部 components），排除 views/router/e2e/构建样式配置。
        // CI/MR 的 Lines 正则与 web:coverage-gate 均依赖此口径。
        include: [
          'src/lib/**/*.{ts,vue}',
          'src/components/**/*.{ts,vue}',
          'src/data/**/*.{ts,vue}',
          'src/App.vue',
          'src/main.ts',
        ],
        exclude: [
          'src/views/**',
          'src/router/**',
          'src/**/*.test.ts',
          'e2e/**',
          '**/*.{css,scss,sass}',
          '**/tailwind.config.*',
          '**/postcss.config.*',
          '**/playwright.config.*',
          '**/vite.config.*',
        ],
        // Vitest 4 remaps coverage through chained Vue source maps; re-apply exclude after remap.
        excludeAfterRemap: true,
      },
    },
    server: {
      // host: true binds 0.0.0.0 so the platform preview proxy (which dials the
      // container's bridge IP) can reach the dev server, not just loopback.
      host: true,
      // Preview iframes load pv.<host> so the app is not same-origin with this page.
      allowedHosts: true,
      port,
      // Dev convenience: proxy API calls to the local backend so the SPA works
      // same-origin without needing VITE_API_BASE. Override the target via env if
      // your backend runs elsewhere.
      proxy: {
        // ws: true so WebSocket upgrades (live logs, agent chat tester) also proxy.
        '/api': { target: process.env.VITE_API_PROXY || 'http://localhost:8080', changeOrigin: true, ws: true },
        // Sandbox code-server IDE reverse-proxy lives at /sandbox/:id/* (outside
        // /api). Without this, the dev SPA server swallows the iframe request and
        // returns index.html instead of the in-container IDE. ws:true because
        // code-server drives the workbench over WebSockets.
        // NOTE: the trailing slash is required — key "/sandbox" is a prefix match
        // and would also capture the SPA routes under "/sandboxes/*", breaking
        // the sandbox console page. "/sandbox/" only matches the IDE proxy.
        '/sandbox/': { target: process.env.VITE_API_PROXY || 'http://localhost:8080', changeOrigin: true, ws: true },
        // Sandbox acp-bridge native UI reverse-proxy lives at /sandbox-bridge/:id/*
        // (legacy /sandbox-acp/ still proxied by the server for compatibility).
        '/sandbox-bridge/': { target: process.env.VITE_API_PROXY || 'http://localhost:8080', changeOrigin: true, ws: true },
        '/sandbox-acp/': { target: process.env.VITE_API_PROXY || 'http://localhost:8080', changeOrigin: true, ws: true },
        // App preview reverse-proxy lives at /preview/:runId/:nodeId/:port/* (outside
        // /api). Without this, Vite dev swallows iframe requests and returns SPA HTML.
        '/preview/': {
          target: process.env.VITE_API_PROXY || 'http://localhost:8080',
          changeOrigin: true,
          ws: true,
          configure: (proxy) => {
            proxy.on('proxyReq', (proxyReq, req) => {
              if (req.headers.host) proxyReq.setHeader('X-Forwarded-Host', req.headers.host)
            })
          },
        },
        '/public/gate-approvals/preview-api/': {
          target: process.env.VITE_API_PROXY || 'http://localhost:8080',
          changeOrigin: true,
          ws: true,
          configure: (proxy) => {
            proxy.on('proxyReq', (proxyReq, req) => {
              if (req.headers.host) proxyReq.setHeader('X-Forwarded-Host', req.headers.host)
            })
          },
        },
        '/preview-vnc/': { target: process.env.VITE_API_PROXY || 'http://localhost:8080', changeOrigin: true, ws: true },
        '/preview-pick.js': { target: process.env.VITE_API_PROXY || 'http://localhost:8080', changeOrigin: true },
      },
    },
    // Mirror host/port for `vite preview` (static build) so a production-build
    // preview is reachable and served under the same base.
    preview: {
      host: true,
      port,
    },
  }
})

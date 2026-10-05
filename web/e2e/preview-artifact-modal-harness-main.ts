/**
 * Browser harness: Artifact modal stage (hideAppPreview) + preview-pick bar.
 * Temporary for test-node gate screenshots / acceptance.
 */
import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import ReactArtifactStage from '../src/components/run/ReactArtifactStage.vue'
import type { Artifact } from '../src/lib/shared/types'
import { api } from '../src/lib/api/api'
import { resetStageOpenStateForTests } from '../src/lib/run/reactArtifactPreview'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

const params = new URLSearchParams(location.search)
const mode = params.get('mode') || 'stage'
const GRASP = location.origin

initLocale()
setTheme('dark')
installIdleScrollbar()
resetStageOpenStateForTests()

const research: Artifact = {
  id: 'a1',
  name: 'research.json',
  kind: 'json',
  nodeId: 'approve_1',
  runId: 'run-1',
  workflowName: '产物弹窗',
  sizeBytes: 120,
  createdAt: '2026-09-29T00:00:00Z',
  revision: 2,
  updatedAt: '2026-09-29T01:00:00Z',
  content: JSON.stringify({ title: '调研', summary: '弹窗复用产物 Tab' }, null, 2),
}

const plan: Artifact = {
  id: 'a2',
  name: 'plan.json',
  kind: 'json',
  nodeId: 'approve_1',
  runId: 'run-1',
  workflowName: '产物弹窗',
  sizeBytes: 80,
  createdAt: '2026-09-29T00:05:00Z',
  revision: 1,
  content: JSON.stringify({ title: '计划', goals: ['底部 Artifact'] }, null, 2),
}

;(api as { artifactContent: (id: string) => Promise<Artifact> }).artifactContent = async (id: string) => {
  const hit = [research, plan].find((a) => a.id === id)
  if (!hit) throw new Error('missing ' + id)
  return { ...hit }
}
;(api as { nodePreviews: () => Promise<{ ports: unknown[] }> }).nodePreviews = async () => ({
  ports: [
    {
      port: 5173,
      label: '前端',
      runId: 'run-1',
      nodeId: 'approve_1',
      proxyUrl: '/p',
      healthy: true,
    },
  ],
})
;(api as { artifactVersions: () => Promise<unknown[]> }).artifactVersions = async () => []
;(api as { getRunNodeSandbox: () => Promise<{ id: number }> }).getRunNodeSandbox = async () => ({ id: 42 })

async function mountStage() {
  const App = defineComponent({
    name: 'PreviewArtifactStageHarness',
    setup() {
      const artifacts = ref([research, plan])
      return () =>
        h(
          'div',
          {
            'data-testid': 'preview-artifact-stage-harness',
            class: 'mx-auto flex h-screen max-w-5xl flex-col bg-base',
          },
          [
            h('div', { class: 'shrink-0 border-b border-line bg-surface px-3 py-2 text-sm font-medium' }, '产物'),
            h(ReactArtifactStage, {
              class: 'min-h-0 flex-1',
              artifacts: artifacts.value,
              runId: 'run-1',
              nodeId: 'approve_1',
              node: { type: 'agent', caps: CLARIFY_CAPS },
              hideAppPreview: true,
              remoteKind: 'app',
              annotatable: false,
            }),
          ],
        )
    },
  })
  createApp(App).use(i18n).mount('#app')
}

async function mountPickBar() {
  const root = document.getElementById('app')!
  root.innerHTML =
    '<main data-testid="preview-page" style="min-height:100vh;padding:24px;background:#0b0b0c;color:#e5e7eb">' +
    '<h1>预览页</h1><p>右下角应为 Pick / Artifact / Chat</p>' +
    '<div id="chat-echo" data-testid="chat-echo">drawer-open</div>' +
    '</main>'
  root.setAttribute('data-testid', 'preview-pick-artifact-harness')
  root.setAttribute('data-mode', 'pick')

  const originalFetch = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.includes('/__grasp/embed-origin')) {
      return new Response(
        JSON.stringify({ origin: GRASP, runId: 'run-1', nodeId: 'ap1' }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }
    return originalFetch(input, init)
  }

  history.replaceState(
    null,
    '',
    location.pathname + location.search + '#__grasp_embed&run=run-1&node=ap1&ticket=tk1',
  )
  await new Promise<void>((resolve, reject) => {
    const s = document.createElement('script')
    s.src = '/preview-pick.js'
    s.onload = () => resolve()
    s.onerror = () => reject(new Error('failed to load preview-pick.js'))
    document.documentElement.appendChild(s)
  })

  const deadline = Date.now() + 8000
  let drawerIframe: HTMLIFrameElement | null = null
  while (Date.now() < deadline) {
    const host = document.querySelector('grasp-preview-pick')
    const shadow = host?.shadowRoot ?? null
    drawerIframe = (shadow?.querySelector('[data-role="drawer"] iframe') as HTMLIFrameElement | null) || null
    if (drawerIframe) break
    await new Promise((r) => setTimeout(r, 50))
  }
  if (!drawerIframe) throw new Error('preview-pick drawer iframe missing')

  // Point chat iframe at a same-origin mock that posts grasp-embed:ready (source must be contentWindow).
  await new Promise<void>((resolve, reject) => {
    const onLoad = () => resolve()
    drawerIframe.addEventListener('load', onLoad, { once: true })
    drawerIframe.src = '/embed-chat-ready-mock.html'
    setTimeout(() => reject(new Error('chat mock load timeout')), 5000)
  })
  await new Promise((r) => setTimeout(r, 50))
}

if (mode === 'pick') {
  void mountPickBar()
} else {
  void mountStage()
}

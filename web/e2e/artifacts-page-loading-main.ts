/**
 * Browser harness: ArtifactsView dual-track loading (HardLoadLayer / RefreshStrip).
 * Temporary for test-node gate; scenarios via ?scenario=
 *   pending | ready | fail | refresh
 */
import '../src/styles/global.css'
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import ArtifactsView from '../src/views/ArtifactsView.vue'
import { api } from '../src/lib/api/api'
import type { Artifact, Workflow, Project } from '../src/lib/shared/types'

const params = new URLSearchParams(location.search)
const scenario = params.get('scenario') || 'pending'
const delayMs = Number(params.get('delay') || '2500')

setTheme('light')
installIdleScrollbar()

const sampleArtifact: Artifact = {
  id: 'art-1',
  name: 'notes.md',
  kind: 'markdown',
  nodeId: 'n1',
  runId: 'run-1',
  workflowId: 'wf-1',
  workflowName: 'Demo',
  sizeBytes: 12,
  createdAt: '2026-09-13T00:00:00Z',
}

const sampleWorkflow: Workflow = {
  id: 'wf-1',
  name: 'Demo',
  description: '',
  nodes: [],
  edges: [],
  createdAt: '2026-09-13T00:00:00Z',
  updatedAt: '2026-09-13T00:00:00Z',
}

const projects: Project[] = [
  {
    id: 'proj-a',
    name: '项目 A',
    description: '',
    variables: [],
    createdAt: '2026-09-13T00:00:00Z',
    updatedAt: '2026-09-13T00:00:00Z',
  },
  {
    id: 'proj-b',
    name: '项目 B',
    description: '',
    variables: [],
    createdAt: '2026-09-13T00:00:00Z',
    updatedAt: '2026-09-13T00:00:00Z',
  },
]

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms))
}

let failOnce = scenario === 'fail'

;(api as { listProjects: typeof api.listProjects }).listProjects = async () => projects
;(api as { getRun: typeof api.getRun }).getRun = async () =>
  ({ id: 'run-1', artifacts: [] }) as Awaited<ReturnType<typeof api.getRun>>
;(api as { listWorkflows: typeof api.listWorkflows }).listWorkflows = async () => {
  if (failOnce) {
    await sleep(400)
    throw new Error('network')
  }
  if (scenario === 'pending' || scenario === 'refresh') {
    await sleep(delayMs)
  }
  return [sampleWorkflow]
}
;(api as { listArtifacts: typeof api.listArtifacts }).listArtifacts = async (opts?: {
  page?: number
  groupBy?: string
  projectId?: string
}) => {
  const isPage = opts?.groupBy === 'run' || opts?.page != null
  if (failOnce && !isPage) {
    await sleep(400)
    throw new Error('network')
  }
  if ((scenario === 'pending' || scenario === 'refresh') && !isPage) {
    await sleep(delayMs)
  }
  if (scenario === 'refresh' && isPage) {
    await sleep(200)
  }
  if (isPage) {
    return { items: [sampleArtifact], total: 1, page: 1, pageSize: 20 }
  }
  return [sampleArtifact]
}

if (scenario === 'fail') {
  setTimeout(() => {
    failOnce = false
  }, 900)
}

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/', name: 'artifacts', component: ArtifactsView }],
})

const App = defineComponent({
  name: 'ArtifactsPageLoadingHarness',
  setup() {
    const ready = ref(false)
    void (async () => {
      await initLocale()
      await setLocale('zh-CN')
      await router.push('/')
      ready.value = true
      await nextTick()
      if (scenario === 'refresh') {
        await sleep(delayMs + 800)
        const root = document.querySelector('[data-testid="project-filter"]') as HTMLElement | null
        const trigger = root?.querySelector('button') as HTMLElement | null
        trigger?.click()
        await sleep(250)
        const opts = Array.from(
          document.querySelectorAll('[data-testid="project-filter-panel"] button'),
        ) as HTMLElement[]
        const projB = opts.find((el) => el.textContent?.includes('项目 B'))
        projB?.click()
      }
    })()
    return () =>
      ready.value
        ? h(
            'div',
            {
              class: 'h-full min-h-screen p-4',
              'data-testid': 'artifacts-loading-harness-root',
            },
            [h(ArtifactsView)],
          )
        : h('div', { 'data-testid': 'artifacts-loading-harness-boot' }, 'booting')
  },
})

createApp(App).use(createPinia()).use(i18n).use(router).mount('#app')

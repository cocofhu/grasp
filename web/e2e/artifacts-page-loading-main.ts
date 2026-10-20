import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import ArtifactsView from '../src/views/ArtifactsView.vue'
import { api } from '../src/lib/api/api'
import type { Artifact } from '../src/lib/shared/types'

const params = new URLSearchParams(location.search)
const scenario = params.get('scenario') || 'ready'
const delayMs = Number(params.get('delay') || '2000')
const sample: Artifact = { id: 'art-1', name: 'notes.md', kind: 'markdown', nodeId: 'deliver', runId: 'run-1', workflowId: 'wf-1', workflowName: 'Demo', sizeBytes: 120, createdAt: '2026-10-20T09:00:00Z' }
let treeCalls = 0
let listCalls = 0
const delay = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))
api.getArtifactTree = async () => {
  treeCalls += 1
  if (scenario === 'pending') await delay(delayMs)
  if (scenario === 'fail' && treeCalls === 1) { await delay(100); throw new Error('network') }
  return [{ projectId: 'workspace', projectName: '工作区', count: 2, sessionCount: 0, workflows: [{ workflowId: 'wf-1', workflowName: 'Demo', count: 1 }, { workflowId: 'wf-2', workflowName: 'Review', count: 1 }] }]
}
api.listArtifacts = async () => {
  listCalls += 1
  if (scenario === 'refresh' && listCalls > 1) await delay(delayMs)
  return { items: [sample], total: 1, page: 1, pageSize: 20, hasMore: false }
}
api.getRun = async () => ({ id: 'run-1', artifacts: [sample] }) as Awaited<ReturnType<typeof api.getRun>>
api.artifactContent = async () => ({ ...sample, content: '# Release notes\n\nReady for review.' })
api.artifactVersions = async () => []

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('light')
  installIdleScrollbar()
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', name: 'artifacts', component: ArtifactsView }] })
  await router.push('/')
  const App = defineComponent({ setup: () => () => h('div', { class: 'h-screen p-4', 'data-testid': 'artifacts-loading-harness-root' }, [h(ArtifactsView)]) })
  createApp(App).use(i18n).use(router).mount('#app')
}
void bootstrap()

import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ReactArtifactStage from '../src/components/run/ReactArtifactStage.vue'
import ArtifactPanel from '../src/components/run/ArtifactPanel.vue'
import type { Artifact, Run } from '../src/lib/shared/types'
import { api } from '../src/lib/api/api'

const params = new URLSearchParams(location.search)
const surface = params.get('surface') || 'stage'
setTheme(params.get('theme') === 'dark' ? 'dark' : 'light')
initLocale()

const RUN_ID = 'run-87e6cd15'

const PLAN = {
  title: '实机验收计划',
  goals: [
    { id: 'g1', title: '服务端保存', status: 'done', subgoals: [{ id: 'g1.1', title: '版本号', status: 'done' }] },
    { id: 'g2', title: '实机验收', detail: '需要真实设备', status: 'pending', subgoals: [{ id: 'g2.1', title: 'vm', status: 'pending' }] },
  ],
}

interface Stored { artifact: Artifact; content: string; history: { revision: number; content: string }[] }
const store: Record<string, Stored> = {}

function put(id: string, name: string, nodeId: string, content: string) {
  store[name] = {
    artifact: {
      id,
      name,
      kind: 'json',
      nodeId,
      runId: RUN_ID,
      workflowName: 'e2e',
      sizeBytes: content.length,
      createdAt: '2026-11-01T10:00:00Z',
      updatedAt: '2026-11-01T10:00:00Z',
      revision: 3,
    },
    content,
    history: [1, 2].map((revision) => ({ revision, content })),
  }
}
put('art-plan', 'plan.json', 'plan_1', JSON.stringify(PLAN, null, 2))
put('art-test', 'test_result.json', 'test_1', JSON.stringify({
  summary: '实机项无法执行',
  cases: [{ id: 't1', name: 'save', status: 'passed' }],
  plan_coverage: [{ plan_id: 'g1.1', passed: true, cases: ['save'] }],
  passed: 1,
  failed: 0,
  skipped: 0,
}, null, 2))

const byId = (id: string) => Object.values(store).find((s) => s.artifact.id === id)!
const artifacts = ref<Artifact[]>(Object.values(store).map((s) => ({ ...s.artifact })))

const a = api as unknown as Record<string, unknown>
a.artifactContent = async (id: string) => {
  const s = byId(id)
  return { ...s.artifact, content: s.content, etag: `etag-${s.artifact.revision}` }
}
a.artifactVersions = async (id: string) =>
  byId(id).history.map((v) => ({ artifactId: id, revision: v.revision, nodeId: byId(id).artifact.nodeId, sizeBytes: v.content.length, createdAt: '2026-11-01T09:00:00Z' }))
a.artifactVersionContent = async (id: string, revision: number) => {
  const s = byId(id)
  const v = s.history.find((x) => x.revision === revision)!
  return { artifactId: id, revision, nodeId: s.artifact.nodeId, kind: 'json', sizeBytes: v.content.length, createdAt: '', content: v.content }
}
a.saveRunArtifact = async (_runId: string, name: string, content: string, ifMatch?: string) => {
  const s = store[name]!
  if (ifMatch && ifMatch !== `etag-${s.artifact.revision}`) {
    throw Object.assign(new Error('产物已被更新'), { status: 409, code: 'artifact_conflict', revision: s.artifact.revision })
  }
  const norm = JSON.stringify(JSON.parse(content), null, 2)
  const changed = norm !== s.content
  if (changed) {
    s.history.push({ revision: s.artifact.revision, content: s.content })
    s.artifact = { ...s.artifact, revision: (s.artifact.revision || 1) + 1, updatedAt: new Date().toISOString() }
    s.content = norm
    // Same as the artifact_edit WebSocket refresh.
    setTimeout(() => {
      artifacts.value = Object.values(store).map((x) => ({ ...x.artifact }))
    }, 50)
  }
  return { ...s.artifact, content: s.content, etag: `etag-${s.artifact.revision}`, revision: s.artifact.revision, changed }
}
a.nodePreviews = async () => ({ ports: [] })
a.artifactDownloadUrl = (id: string) => `http://127.0.0.1:9/api/artifacts/${id}/download`

const run = { id: RUN_ID, status: 'waiting_human', nodes: [], artifactEdit: { editable: true, reason: '' } } as unknown as Run

const App = defineComponent({
  setup() {
    return () =>
      h('div', { class: 'mx-auto flex h-[760px] max-w-5xl flex-col border border-line bg-surface', 'data-testid': 'artifact-edit-harness' }, [
        surface === 'detail'
          ? h(ArtifactPanel, { artifacts: artifacts.value, run, editable: true })
          : h(ReactArtifactStage, {
              artifacts: artifacts.value,
              runId: RUN_ID,
              run,
              nodeId: 'test_1',
              remoteKind: 'off',
              editable: true,
            }),
      ])
  },
})

createApp(App).use(i18n).mount('#app')

import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import ArtifactPreview from '../src/components/run/ArtifactPreview.vue'
import type { Artifact, Run } from '../src/lib/shared/types'
import { api } from '../src/lib/api/api'
import { PAGE_CAPS } from '../src/test/capsFixtures'

const params = new URLSearchParams(location.search)
const scenario = params.get('scenario') || 'multi'

initLocale()
setTheme('light')
installIdleScrollbar()

const livePage: Artifact = {
  id: 'art-page-live',
  name: 'page.html',
  kind: 'html',
  nodeId: 'visual_1',
  runId: 'run-version-e2e',
  sizeBytes: 64,
  createdAt: '2026-08-21T12:00:00Z',
  content: '<!doctype html><html><body style="font-family:sans-serif;padding:24px"><h1 id="mark-latest">最新稿 v2</h1><p>产物页版本切换验收</p></body></html>',
}

const planJson: Artifact = {
  id: 'art-plan',
  name: 'plan.json',
  kind: 'json',
  nodeId: 'plan_1',
  runId: 'run-version-e2e',
  sizeBytes: 32,
  createdAt: '2026-08-21T12:00:00Z',
}

const multiRun = {
  id: 'run-version-e2e',
  nodes: [{ id: 'visual_1', type: 'agent', caps: PAGE_CAPS, label: '视觉', position: { x: 0, y: 0 }, config: {} }],
  nodeExecutions: {
    visual_1: [
      {
        nodeId: 'visual_1',
        iteration: 1,
        status: 'completed',
        outputs: {
          page: '<!doctype html><html><body style="font-family:sans-serif;padding:24px;background:#fef3c7"><h1 id="mark-v1">历史稿 v1</h1><p>只读预览</p></body></html>',
        },
      },
      {
        nodeId: 'visual_1',
        iteration: 2,
        status: 'waiting_human',
        outputs: { page: livePage.content },
      },
    ],
  },
} as unknown as Run

const singleRun = {
  id: 'run-version-e2e',
  nodes: [{ id: 'visual_1', type: 'agent', caps: PAGE_CAPS, label: '视觉', position: { x: 0, y: 0 }, config: {} }],
  nodeExecutions: {
    visual_1: [
      {
        nodeId: 'visual_1',
        iteration: 1,
        status: 'completed',
        outputs: { page: livePage.content },
      },
    ],
  },
} as unknown as Run

;(api as { artifactContent: (id: string) => Promise<Artifact & { content: string }> }).artifactContent =
  async (id: string) => {
    if (id === planJson.id) {
      return { ...planJson, content: JSON.stringify({ title: '计划', goals: [] }, null, 2) }
    }
    return { ...livePage, content: String(livePage.content) }
  }
;(api as { artifactDownloadUrl: (id: string) => string }).artifactDownloadUrl = (id: string) =>
  `http://127.0.0.1:9/api/artifacts/${id}/download`

const App = defineComponent({
  name: 'ArtifactsPageVersionHarness',
  setup() {
    const artifact = ref<Artifact | null>(
      scenario === 'json' ? planJson : livePage,
    )
    const run = ref<Run | null>(
      scenario === 'no-run' ? null : scenario === 'single' ? singleRun : multiRun,
    )

    return () =>
      h(
        'div',
        {
          class: 'mx-auto flex h-[720px] max-w-4xl flex-col border border-line bg-surface',
          'data-testid': 'artifacts-version-harness-root',
          'data-scenario': scenario,
        },
        [
          h('div', { class: 'border-b border-line px-3 py-2 text-sm text-txt2' }, [
            `scenario=${scenario} · 产物页 page.html 版本切换浏览器验收`,
          ]),
          h(ArtifactPreview, {
            artifact: artifact.value,
            scope: 'platform',
            run: run.value,
            annotatable: false,
          }),
        ],
      )
  },
})

createApp(App).use(i18n).mount('#app')

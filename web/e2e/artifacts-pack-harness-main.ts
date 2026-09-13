/**
 * Temporary browser harness: ArtifactList pack button (platform vs run scope).
 * Mirrors confirmed Demo interaction for test-node gate screenshots.
 */
import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import ArtifactList from '../src/components/run/ArtifactList.vue'
import ToastHost from '../src/components/ui/ToastHost.vue'
import type { Artifact } from '../src/lib/shared/types'
import { api } from '../src/lib/api/api'

const params = new URLSearchParams(location.search)
const scenario = params.get('scenario') || 'platform'
const failPack = params.get('fail') === '1'

initLocale()
setTheme('dark')
installIdleScrollbar()

const a1: Artifact = {
  id: 'art-1',
  name: 'plan.json',
  kind: 'json',
  nodeId: 'plan',
  runId: 'run-filled',
  workflowName: '产物页打包',
  sizeBytes: 128,
  createdAt: '2026-09-13T12:00:00Z',
}
const a2: Artifact = {
  id: 'art-2',
  name: 'page.html',
  kind: 'html',
  nodeId: 'approve',
  runId: 'run-filled',
  workflowName: '产物页打包',
  sizeBytes: 4096,
  createdAt: '2026-09-13T12:05:00Z',
}

;(api as { packRunArtifacts: (runId: string) => Promise<{ blob: Blob; filename: string }> }).packRunArtifacts =
  async (runId: string) => {
    await new Promise((r) => setTimeout(r, 400))
    if (failPack) throw new Error('mock pack failed')
    return {
      blob: new Blob([`zip-for-${runId}`], { type: 'application/zip' }),
      filename: `${runId}-artifacts.zip`,
    }
  }

const App = defineComponent({
  name: 'ArtifactsPackHarness',
  setup() {
    const activeId = ref<string | null>(a1.id)
    const scope = scenario === 'run' ? 'run' : 'platform'
    const artifacts = scenario === 'empty' ? [] : [a1, a2]
    const runSections =
      scenario === 'empty'
        ? [{ runId: 'run-empty', runTitle: '空 Run', items: [] as Artifact[] }]
        : [
            {
              runId: 'run-filled',
              runTitle: '给产物页加打包下载',
              items: [a1, a2],
            },
            {
              runId: 'run-empty',
              runTitle: '空分段',
              items: [] as Artifact[],
            },
          ]

    return () =>
      h(
        'div',
        {
          'data-testid': 'artifacts-pack-harness-root',
          class: 'mx-auto flex h-screen max-w-md flex-col border border-line bg-surface p-3',
        },
        [
          h('div', { class: 'mb-2 text-[12px] text-txt2' }, `scenario=${scenario}`),
          h(ArtifactList, {
            artifacts,
            scope,
            activeId: activeId.value,
            runSections: scope === 'platform' ? runSections : undefined,
            'onUpdate:activeId': (id: string | null) => {
              activeId.value = id
            },
            onSelect: (a: Artifact) => {
              activeId.value = a.id
            },
          }),
          h(
            'div',
            { 'data-testid': 'active-id', class: 'mt-2 text-[11px] text-txt3' },
            `activeId=${activeId.value ?? 'null'}`,
          ),
          h(ToastHost),
        ],
      )
  },
})

createApp(App).use(i18n).mount('#app')

import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/minimap/dist/style.css'
import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import WorkflowEditorView from '../src/views/WorkflowEditorView.vue'
import WorkflowCanvas from '../src/components/canvas/WorkflowCanvas.vue'
import ToastHost from '../src/components/ui/ToastHost.vue'
import { buildDefaultWorkflow } from '../src/components/canvas/composables/defaultTemplate'
import type { NodeRunStatus } from '../src/lib/shared/types'
import { CANVAS_AGENTS } from './workflow-canvas-fixtures'

const params = new URLSearchParams(window.location.search)

/** Read-only run canvas: default workflow with a running or failed status map. */
const RunCanvasFixture = defineComponent({
  setup() {
    const t = i18n.global.t as (key: string) => string
    const graph = buildDefaultWorkflow(CANVAS_AGENTS, t)
    const failed = params.get('scenario') === 'failed'
    const statusMap: Record<string, NodeRunStatus> = failed
      ? { input: 'completed', clarify: 'completed', implement: 'completed', test_review: 'failed' }
      : { input: 'completed', clarify: 'completed', implement: 'running' }
    const iterations = failed ? { implement: 2, test_review: 2 } : { implement: 2 }
    const failReasons = failed ? { test_review: '单测 3 项失败：登录接口返回 500' } : {}
    return () =>
      h('div', { class: 'h-screen w-screen bg-base' }, [
        h(WorkflowCanvas, {
          nodes: graph.nodes,
          edges: graph.edges,
          mode: 'run',
          agents: CANVAS_AGENTS,
          statusMap,
          iterations,
          failReasons,
          autoLayoutOnInit: true,
          follow: true,
          followNodeId: failed ? 'test_review' : 'implement',
        }),
      ])
  },
})

async function bootstrap() {
  await initLocale()
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme(params.get('theme') === 'dark' ? 'dark' : 'light')

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/workflows/:id/edit', component: WorkflowEditorView },
      { path: '/run', component: RunCanvasFixture },
      { path: '/:rest(.*)', component: { render: () => h('div', 'away') } },
    ],
  })
  await router.push(params.get('view') === 'run' ? '/run' : `/workflows/${params.get('wf') || 'wf-canvas'}/edit`)

  createApp({ render: () => [h(RouterView), h(ToastHost)] })
    .directive('hover-ink', vHoverInk)
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

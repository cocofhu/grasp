import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import AgentTimeline from '../src/components/run/AgentTimeline.vue'
import ReactArtifactStage from '../src/components/run/ReactArtifactStage.vue'
import type { AgentPart, Artifact } from '../src/lib/shared/types'

initLocale()
setTheme(new URLSearchParams(location.search).get('theme') === 'dark' ? 'dark' : 'light')

/**
 * A finished review turn the way cursor reports it: thought, a shell call,
 * thought, Grasp MCP writes (one failing), then the reply. The stage beside
 * it is what "查看" opens.
 */
const parts: AgentPart[] = [
  { kind: 'thought', text: '先确认依赖能装上，再把计划写进产物。' },
  { kind: 'tool', title: 'Shell', status: 'completed', summary: 'npm ci', input: '{\n  "command": "npm ci"\n}', output: 'added 812 packages in 41s', durationMs: 42_000 },
  { kind: 'tool', title: 'Read', status: 'completed', summary: 'src/App.vue', durationMs: 300 },
  { kind: 'thought', text: '依赖正常，开始整理计划。' },
  { kind: 'tool', title: 'set_plan', status: 'completed', summary: '', durationMs: 1_200 },
  { kind: 'tool', title: 'set_research', status: 'failed', output: 'set_research failed: 字段 findings 不能为空' },
  { kind: 'message', text: '计划已写入，调研结论缺少 findings，下一轮补上。' },
]

const artifacts: Artifact[] = [
  {
    id: 'a-plan',
    name: 'plan.json',
    kind: 'json',
    nodeId: 'grasp',
    runId: 'run-e2e',
    workflowName: 'wf',
    sizeBytes: 64,
    createdAt: '2026-10-01T00:00:00Z',
    revision: 1,
    content: JSON.stringify({ goals: ['装依赖', '写计划'] }, null, 2),
  },
]

const App = defineComponent({
  name: 'AgentToolTimelineHarness',
  setup() {
    return () =>
      h('div', { class: 'flex h-screen gap-3 p-3', 'data-testid': 'tool-timeline-root' }, [
        h('div', { class: 'w-[440px] shrink-0 overflow-auto rounded-lg border border-line bg-surface p-3' }, [
          h(AgentTimeline, { parts, completed: true, runId: 'run-e2e' }),
        ]),
        h('div', { class: 'min-w-0 flex-1 overflow-hidden rounded-lg border border-line' }, [
          h(ReactArtifactStage, { artifacts, runId: 'run-e2e', nodeId: 'grasp', inlineContent: true, remoteKind: 'off' }),
        ]),
      ])
  },
})

createApp(App).use(i18n).mount('#app')

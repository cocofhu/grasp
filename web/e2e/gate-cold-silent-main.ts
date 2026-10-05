import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import GateApproval from '../src/components/run/GateApproval.vue'
import type { Gate, Run } from '../src/lib/shared/types'
import { RESEARCH_CAPS } from '../src/test/capsFixtures'

const params = new URLSearchParams(window.location.search)
const session = params.get('session') === 'hot' ? 'hot' : 'cold'

const researchDoc = {
  summary: 'human_gate 冷会话静默验收',
  goals: ['冷态无 ReAct 提示', '热态就地改保留'],
}

const gate: Gate = {
  runId: 'run-gate-cold',
  nodeId: 'hg-research',
  workflowName: 'wf',
  title: '审阅调研',
  bodyMd: '请审阅',
  actions: [
    { id: 'approve', label: '通过' },
    { id: 'revise', label: '打回', requireForm: true },
  ],
  form: [{ key: 'comment', label: '评审意见' }],
  requestedAt: '2026-07-18T00:00:00Z',
  reactSessionAlive: session === 'hot',
  reactUpstreamNodeId: 'research',
}

const run = {
  id: 'run-gate-cold',
  title: 'gate cold silent',
  workflowId: 'wf-1',
  workflowName: 'wf',
  status: 'waiting_human',
  createdAt: '2026-07-18T00:00:00Z',
  nodes: [
    {
      id: 'hg-research',
      type: 'human_gate',
      label: '审阅调研',
      position: { x: 0, y: 0 },
      config: { body_template: '{{nodes.research.outputs.research}}' },
    },
    {
      id: 'research',
      type: 'agent', caps: RESEARCH_CAPS,
      label: '调研',
      position: { x: 0, y: 0 },
      config: {},
    },
  ],
  edges: [],
  nodeStates: {},
  artifacts: [
    {
      id: 'a-research',
      name: 'research.json',
      kind: 'json',
      nodeId: 'research',
      runId: 'run-gate-cold',
      workflowName: 'wf',
      sizeBytes: 64,
      createdAt: '2026-07-18T00:00:00Z',
    },
  ],
  nodeExecutions: {
    research: [
      {
        nodeId: 'research',
        iteration: 1,
        status: 'completed',
        outputs: { research_json: JSON.stringify(researchDoc) },
      },
    ],
  },
} as Run

const Fixture = defineComponent({
  name: 'GateColdSilentFixture',
  setup() {
    const resolved = ref(false)
    return () =>
      h(
        'div',
        {
          'data-testid': 'gate-cold-silent-root',
          'data-session': session,
          class: 'flex h-full min-h-0 flex-col bg-surface',
        },
        [
          h(
            'div',
            { class: 'shrink-0 border-b border-line px-3 py-2 text-sm font-semibold' },
            `GateApproval · ${session === 'hot' ? '热会话' : '冷会话'}`,
          ),
          h(
            'div',
            { class: 'min-h-0 flex-1', 'data-testid': 'gate-cold-silent-panel' },
            [
              h(GateApproval, {
                gate,
                run,
                fillPreview: true,
                onResolve: () => {
                  resolved.value = true
                },
              }),
            ],
          ),
        ],
      )
  },
})

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('dark')
  installIdleScrollbar()
  createApp(Fixture).use(i18n).mount('#app')
}

void boot()

import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import GateApproval from '../src/components/run/GateApproval.vue'
import type { Gate, Run } from '../src/lib/shared/types'

const params = new URLSearchParams(window.location.search)
const shortHtml = params.get('short') === '1'
const noUpstream = params.get('noUpstream') === '1'
const theme = params.get('theme') === 'light' ? 'light' : 'dark'

const shortPage =
  '<!doctype html><html><body style="margin:0;background:#0a0a0b;color:#eee;font:14px sans-serif"><div style="padding:12px"><b>模型构成 / 消耗排行</b><p style="color:#aaa;margin:6px 0 0">短视觉 HTML</p></div></body></html>'

const tallPage = `<!doctype html><html><body style="margin:0;background:#0a0a0b;color:#eee;font:14px sans-serif">
<div style="display:flex;height:100vh">
  <aside style="width:180px;border-right:1px solid #333;padding:12px;background:#141417">Grasp</aside>
  <main style="flex:1;padding:16px"><h1 style="margin:0 0 8px;font-size:16px">模型构成 / 消耗排行</h1>
  <p style="color:#aaa">视觉产物示意</p>
  ${Array.from({ length: 40 }, (_, i) => `<p style="margin:4px 0;color:#666">row ${i + 1}</p>`).join('')}
  </main>
</div></body></html>`

const pageHtml = shortHtml ? shortPage : tallPage

const gate: Gate = {
  runId: 'run-inbox-budget',
  nodeId: 'hg-visual',
  workflowName: 'wf',
  title: '人工评审',
  bodyMd: '请审阅视觉产物',
  actions: [
    { id: 'approve', label: '确认并流转' },
    { id: 'revise', label: '打回', requireForm: true },
  ],
  form: [{ key: 'comment', label: '评审意见' }],
  requestedAt: '2026-07-18T00:00:00Z',
}

const artifacts = noUpstream
  ? []
  : [
      {
        id: 'a-req',
        name: 'clarified_requirement.json',
        kind: 'json' as const,
        nodeId: 'react',
        runId: 'run-inbox-budget',
        workflowName: 'wf',
        sizeBytes: 64,
        createdAt: '2026-07-18T00:00:00Z',
      },
    ]

const run = {
  id: 'run-inbox-budget',
  title: 'inbox unified budget',
  workflowId: 'wf-1',
  workflowName: 'wf',
  status: 'waiting_human',
  createdAt: '2026-07-18T00:00:00Z',
  nodes: [
    {
      id: 'hg-visual',
      type: 'human_gate',
      label: '人工评审',
      position: { x: 0, y: 0 },
      config: { body_template: '{{nodes.visual.outputs.page}}' },
    },
    {
      id: 'visual',
      type: 'agent',
      label: '视觉',
      position: { x: 0, y: 0 },
      config: {},
    },
  ],
  edges: [],
  nodeStates: {},
  artifacts,
  nodeExecutions: {
    visual: [
      {
        nodeId: 'visual',
        iteration: 1,
        status: 'completed',
        outputs: { page: pageHtml },
      },
    ],
  },
} as Run

const Fixture = defineComponent({
  name: 'InboxUnifiedBudgetFixture',
  setup() {
    const resolved = ref(false)
    return () =>
      h(
        'div',
        {
          'data-testid': 'inbox-budget-root',
          class: 'flex h-full min-h-0 flex-col bg-base p-4',
        },
        [
          h(
            'div',
            {
              // Desktop inbox no longer reserves a 320px card column; the panel fills the main area.
              class: 'flex min-h-0 flex-1 flex-col',
              'data-testid': 'inbox-desktop-panel',
            },
            [
              h(
                'div',
                {
                  class: 'card flex h-full min-h-0 w-full flex-col overflow-hidden',
                  'data-testid': 'inbox-detail-card',
                },
                [
                  h(
                    'div',
                    {
                      class:
                        'flex shrink-0 items-center justify-between border-b border-line px-4 py-2.5',
                    },
                    [
                      h('span', { class: 'text-xs text-txt3' }, 'Run #inbox-budget · hg-visual'),
                    ],
                  ),
                  h(
                    'div',
                    { class: 'flex min-h-0 flex-1 flex-col overflow-hidden' },
                    [
                      h(GateApproval, {
                        gate,
                        run,
                        fillPreview: true,
                        unifiedPreviewBudget: true,
                        class: 'min-h-0 flex-1',
                        onResolve: () => {
                          resolved.value = true
                        },
                      }),
                    ],
                  ),
                ],
              ),
            ],
          ),
        ],
      )
  },
})

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme(theme)
  installIdleScrollbar()
  createApp(Fixture).use(i18n).mount('#app')
}

void boot()

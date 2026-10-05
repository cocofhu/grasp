/**
 * Production-shaped mobile Run detail fixture (post single-panel fix).
 * Mirrors RunDetailView ≤767: page tabs, mutual timeline/detail, scroll footer,
 * product default tab, and waiting_human gate/review sticky decisions.
 */
import '../src/styles/global.css'
import { createApp, computed, defineComponent, h, ref, watch, nextTick } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import StatusPill from '../src/components/ui/StatusPill.vue'
import Icon from '../src/components/ui/Icon.vue'
import ExecutionTimeline from '../src/components/run/ExecutionTimeline.vue'
import GateApproval from '../src/components/run/GateApproval.vue'
import ReviewShell from '../src/components/run/ReviewShell.vue'
import ReviewComposer from '../src/components/run/ReviewComposer.vue'
import type { Gate, Run, WFNode } from '../src/lib/shared/types'
import { CLARIFY_CAPS, PAGE_CAPS, RESEARCH_CAPS } from '../src/test/capsFixtures'

const params = new URLSearchParams(window.location.search)
const scenario = (params.get('scenario') || 'completed') as 'completed' | 'gate' | 'review'

const longPage = `<!doctype html><html><body style="margin:0;font:16px/1.5 sans-serif;padding:12px">
<h1>视觉网页产物</h1>
${'<p>长内容段落用于预览区内滚动，确保决策按钮吸底常显。</p>'.repeat(28)}
</body></html>`

const nodesCompleted: WFNode[] = [
  { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'clarify', type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', position: { x: 120, y: 0 }, config: {} },
  { id: 'research', type: 'agent', caps: RESEARCH_CAPS, label: '代码调研', position: { x: 240, y: 0 }, config: {} },
  { id: 'visual', type: 'agent', caps: PAGE_CAPS, label: '视觉网页', position: { x: 360, y: 0 }, config: {} },
  { id: 'end', type: 'output', label: '输出', position: { x: 480, y: 0 }, config: {} },
]

function mkExec(
  nodeId: string,
  startedAt: string,
  durationSec: number,
  extras: Record<string, unknown> = {},
) {
  return {
    nodeId,
    iteration: 1,
    status: 'completed' as const,
    startedAt,
    durationSec,
    outputs: {},
    ...extras,
  }
}

const completedRun: Run = {
  id: 'run-62b03a1c',
  workflowId: 'wf-pm',
  workflowName: 'PM MCP',
  workflowVersion: 8,
  status: 'completed',
  trigger: 'manual',
  startedAt: '2026-07-27T03:38:00Z',
  durationSec: 3020,
  progress: 1,
  nodeRuns: {},
  nodeExecutions: {
    start: [
      mkExec('start', '2026-07-27T03:38:00Z', 0, {
        varsSnapshot: { always_true: true, auto_clarify: true },
      }),
    ],
    clarify: [
      mkExec('clarify', '2026-07-27T03:38:10Z', 252, {
        usage: { inputTokens: 120000, outputTokens: 40000, cacheReadTokens: 8000, cacheWriteTokens: 2000 },
      }),
    ],
    research: [
      mkExec('research', '2026-07-27T03:42:00Z', 400, {
        usage: { inputTokens: 300000, outputTokens: 100000, cacheReadTokens: 10000, cacheWriteTokens: 200 },
      }),
    ],
    visual: [
      mkExec('visual', '2026-07-27T03:48:00Z', 481, {
        outputs: { page: longPage },
        usage: { inputTokens: 80000, outputTokens: 16000, cacheReadTokens: 0, cacheWriteTokens: 0 },
      }),
    ],
    end: [mkExec('end', '2026-07-27T04:28:00Z', 0, { outputs: { status: 'success' } })],
  },
  artifacts: [],
  nodes: nodesCompleted,
  edges: [],
} as Run

const gate: Gate = {
  runId: 'run-gate-panel',
  nodeId: 'hg-visual',
  workflowName: 'wf',
  title: '审阅视觉稿',
  bodyMd: '请审阅移动端单面板方案',
  actions: [
    { id: 'approve', label: '通过' },
    { id: 'revise', label: '打回', requireForm: true },
  ],
  form: [{ key: 'comment', label: '评审意见' }],
  requestedAt: '2026-07-27T04:00:00Z',
  reactSessionAlive: true,
  reactUpstreamNodeId: 'visual',
}

const gateNodes: WFNode[] = [
  { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'visual', type: 'agent', caps: PAGE_CAPS, label: '视觉网页', position: { x: 120, y: 0 }, config: {} },
  {
    id: 'hg-visual',
    type: 'human_gate',
    label: '方案确认',
    position: { x: 240, y: 0 },
    config: { body_template: '{{nodes.visual.outputs.page}}' },
  },
]

const gateRun: Run = {
  id: 'run-gate-panel',
  workflowId: 'wf-1',
  workflowName: 'PM MCP',
  workflowVersion: 8,
  status: 'waiting_human',
  trigger: 'manual',
  startedAt: '2026-07-27T03:38:00Z',
  durationSec: 1800,
  progress: 0.72,
  gate,
  nodeRuns: {
    'hg-visual': {
      nodeId: 'hg-visual',
      status: 'waiting_human',
      startedAt: '2026-07-27T04:00:00Z',
    },
  },
  nodeExecutions: {
    start: [mkExec('start', '2026-07-27T03:38:00Z', 0)],
    visual: [
      mkExec('visual', '2026-07-27T03:48:00Z', 400, {
        outputs: { page: longPage },
      }),
    ],
    'hg-visual': [
      {
        nodeId: 'hg-visual',
        iteration: 1,
        status: 'waiting_human',
        startedAt: '2026-07-27T04:00:00Z',
        outputs: {},
      },
    ],
  },
  artifacts: [
    {
      id: 'art-page',
      name: 'page.html',
      sizeBytes: 2048,
      createdAt: '2026-07-27T03:50:00Z',
    },
  ],
  nodes: gateNodes,
  edges: [],
} as Run

const reviewNodes: WFNode[] = [
  { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'visual', type: 'agent', caps: PAGE_CAPS, label: '视觉网页', position: { x: 120, y: 0 }, config: {} },
]

const reviewRun: Run = {
  id: 'run-review-panel',
  workflowId: 'wf-1',
  workflowName: 'PM MCP',
  status: 'waiting_human',
  trigger: 'manual',
  startedAt: '2026-07-27T03:38:00Z',
  durationSec: 2000,
  progress: 0.8,
  nodeRuns: {
    visual: { nodeId: 'visual', status: 'waiting_human', startedAt: '2026-07-27T03:50:00Z' },
  },
  nodeExecutions: {
    start: [mkExec('start', '2026-07-27T03:38:00Z', 0)],
    visual: [
      mkExec('visual', '2026-07-27T03:48:00Z', 400, {
        status: 'waiting_human',
        outputs: { page: longPage },
      }),
    ],
  },
  clarifyByNode: {
    visual: {
      nodeId: 'visual',
      iteration: 1,
      done: false,
      turns: [
        { role: 'agent', text: '视觉网页怎么看不了视觉网页？', at: '2026-07-27T03:55:00Z' },
        { role: 'human', text: '产物预览已嵌入，可滚动。', at: '2026-07-27T03:56:00Z' },
      ],
    },
  },
  artifacts: [],
  nodes: reviewNodes,
  edges: [],
} as Run

function forceMobileMql() {
  const mql = window.matchMedia('(max-width: 767px)')
  Object.defineProperty(mql, 'matches', { configurable: true, get: () => true })
  const orig = window.matchMedia.bind(window)
  window.matchMedia = ((query: string) => {
    if (String(query).includes('max-width: 767px')) return mql
    return orig(query)
  }) as typeof window.matchMedia
}

const Fixture = defineComponent({
  name: 'RunDetailMobilePanelFixture',
  setup() {
    forceMobileMql()

    const run = scenario === 'gate' ? gateRun : scenario === 'review' ? reviewRun : completedRun
    const nodes = run.nodes || nodesCompleted

    const mobileMainPanel = ref<'timeline' | 'detail'>(
      scenario === 'completed' ? 'timeline' : 'detail',
    )
    const selected = ref(
      scenario === 'completed' ? 'end' : scenario === 'gate' ? 'hg-visual' : 'visual',
    )
    const selectedExecIdx = ref(0)
    const timelineScrollToken = ref(0)
    const nodeTab = ref(
      scenario === 'completed' ? 'output' : scenario === 'gate' ? 'gate' : 'review',
    )
    const clarifyDraft = ref('')
    const clarifyAttachments = ref([])
    const clarifyAnnotations = ref([])

    const detailLabel = computed(() => {
      if (nodeTab.value === 'gate') return '门禁审批'
      if (nodeTab.value === 'review') return '复审'
      if (nodeTab.value === 'product') return '产物'
      return '概览'
    })

    const selNode = computed(() => nodes.find((n) => n.id === selected.value) || null)
    const selRun = computed(() => {
      const list = run.nodeExecutions?.[selected.value || ''] || []
      return list[selectedExecIdx.value] || run.nodeRuns?.[selected.value || ''] || null
    })

    function showTimeline() {
      mobileMainPanel.value = 'timeline'
      timelineScrollToken.value += 1
    }
    function showDetail() {
      mobileMainPanel.value = 'detail'
    }
    function onSelect(nodeId: string, idx: number) {
      selected.value = nodeId
      selectedExecIdx.value = idx
      if (nodeId === 'visual' && scenario === 'completed') nodeTab.value = 'product'
      else if (nodeId === 'hg-visual') nodeTab.value = 'gate'
      else if (scenario === 'review' && nodeId === 'visual') nodeTab.value = 'review'
      else nodeTab.value = 'output'
      mobileMainPanel.value = 'detail'
    }

    watch(
      () => [mobileMainPanel.value, selected.value] as const,
      async ([panel]) => {
        if (panel === 'timeline') {
          await nextTick()
          timelineScrollToken.value += 1
        }
      },
    )

    // Initial scroll for completed → last item
    if (scenario === 'completed') {
      nextTick(() => {
        timelineScrollToken.value += 1
      })
    }

    return () =>
      h(
        'div',
        {
          class: 'flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-base',
          'data-testid': 'run-detail-root',
          'data-scenario': scenario,
        },
        [
          h('header', { class: 'shrink-0 border-b border-line bg-surface px-3 py-2' }, [
            h('div', { class: 'text-[14px] font-semibold' }, `Run #${run.id.replace('run-', '')}`),
            h('div', { class: 'mt-1 flex gap-1' }, [
              h(StatusPill, { status: run.status, size: 'sm' }),
            ]),
          ]),
          h(
            'div',
            {
              class: 'flex min-h-0 min-w-0 flex-1 flex-col',
              'data-testid': 'run-detail-content',
            },
            [
              h('div', { class: 'flex shrink-0 border-b border-line bg-surface', 'data-testid': 'mobile-main-panel-tabs' }, [
                h(
                  'button',
                  {
                    type: 'button',
                    'data-testid': 'mobile-panel-timeline',
                    class:
                      mobileMainPanel.value === 'timeline'
                        ? 'flex-1 border-b-2 border-accent px-3 py-2.5 text-[12px] font-semibold text-accent'
                        : 'flex-1 border-b-2 border-transparent px-3 py-2.5 text-[12px] font-semibold text-txt3',
                    onClick: showTimeline,
                  },
                  '时间线',
                ),
                h(
                  'button',
                  {
                    type: 'button',
                    'data-testid': 'mobile-panel-detail',
                    class:
                      mobileMainPanel.value === 'detail'
                        ? 'flex-1 border-b-2 border-accent px-3 py-2.5 text-[12px] font-semibold text-accent'
                        : 'flex-1 border-b-2 border-transparent px-3 py-2.5 text-[12px] font-semibold text-txt3',
                    onClick: showDetail,
                  },
                  detailLabel.value,
                ),
              ]),
              mobileMainPanel.value === 'timeline'
                ? h(
                    'div',
                    {
                      class: 'relative min-h-0 min-w-0 flex-1',
                      'data-testid': 'run-timeline-pane',
                    },
                    [
                      h(ExecutionTimeline, {
                        run,
                        nodes,
                        selectedNodeId: selected.value,
                        selectedExecIdx: selectedExecIdx.value,
                        ensureVisibleToken: timelineScrollToken.value,
                        interactive: true,
                        onSelect,
                      }),
                    ],
                  )
                : h(
                    'div',
                    {
                      class: 'flex min-h-0 min-w-0 flex-1 flex-col bg-surface',
                      'data-testid': 'run-detail-right-panel',
                    },
                    [
                      h(
                        'div',
                        {
                          class: 'flex shrink-0 items-center gap-2 border-b border-line px-3 py-2',
                          'data-testid': 'mobile-detail-back-bar',
                        },
                        [
                          h(
                            'button',
                            {
                              type: 'button',
                              'data-testid': 'mobile-back-to-timeline',
                              class:
                                'inline-flex items-center gap-1 rounded-md border border-line bg-elevated px-2 py-1 text-[11px] font-semibold',
                              onClick: showTimeline,
                            },
                            [h(Icon, { name: 'arrow-left', size: 12 }), ' 返回时间线'],
                          ),
                          h('span', { class: 'text-[11px] text-txt3' }, selNode.value?.label || ''),
                        ],
                      ),
                      h('div', { class: 'flex shrink-0 gap-2 border-b border-line px-3 py-2', 'data-testid': 'node-tabs' }, [
                        scenario === 'gate'
                          ? h('span', { class: 'text-[12px] font-semibold text-accent', 'data-testid': 'tab-gate' }, '门禁审批')
                          : null,
                        scenario === 'review'
                          ? h('span', { class: 'text-[12px] font-semibold text-accent', 'data-testid': 'tab-review' }, '复审')
                          : null,
                        nodeTab.value === 'product'
                          ? h('span', { class: 'text-[12px] font-semibold text-accent', 'data-testid': 'tab-product' }, '产物')
                          : null,
                        nodeTab.value === 'output'
                          ? h('span', { class: 'text-[12px] font-semibold text-accent', 'data-testid': 'tab-output' }, '概览')
                          : h(
                              'button',
                              {
                                type: 'button',
                                class: 'text-[12px] text-txt3',
                                'data-testid': 'tab-output',
                                onClick: () => {
                                  nodeTab.value = 'output'
                                },
                              },
                              '概览',
                            ),
                      ].filter(Boolean)),
                      h('div', { class: 'min-h-0 flex-1', 'data-testid': 'detail-body' }, [
                        nodeTab.value === 'gate' && run.gate
                          ? h(GateApproval, {
                              gate: run.gate,
                              run,
                              fillPreview: true,
                              mobileFillRemaining: true,
                              compact: true,
                            })
                          : null,
                        nodeTab.value === 'review' && selNode.value && selRun.value
                          ? h(
                              ReviewShell,
                              { class: 'h-full min-h-0', mobile: true, sidebarWidth: 320 },
                              {
                                stage: () =>
                                  h('div', {
                                    class: 'h-full overflow-y-auto bg-elevated p-3',
                                    'data-testid': 'review-product-preview',
                                    innerHTML: longPage,
                                  }),
                                sidebar: () =>
                                  h(ReviewComposer, {
                                    mode: 'gate',
                                    runId: run.id,
                                    nodeId: 'visual',
                                    iteration: 1,
                                    draft: clarifyDraft.value,
                                    'onUpdate:draft': (v: string) => {
                                      clarifyDraft.value = v
                                    },
                                    attachments: clarifyAttachments.value,
                                    'onUpdate:attachments': (v: unknown[]) => {
                                      clarifyAttachments.value = v as never
                                    },
                                    annotations: clarifyAnnotations.value,
                                    'onUpdate:annotations': (v: unknown[]) => {
                                      clarifyAnnotations.value = v as never
                                    },
                                    canPass: true,
                                    canReject: true,
                                    rejectAllowEmpty: true,
                                  }),
                              },
                            )
                          : null,
                        nodeTab.value === 'product'
                          ? h('div', { class: 'flex h-full min-h-0 flex-col', 'data-testid': 'product-preview' }, [
                              h('div', { class: 'shrink-0 border-b border-line bg-elevated px-3 py-2 text-[11px] text-txt3' }, [
                                h('b', { class: 'text-txt' }, '视觉网页产物'),
                                ' · page.html',
                              ]),
                              h('div', {
                                class: 'min-h-0 flex-1 overflow-y-auto p-3',
                                'data-testid': 'html-preview',
                                innerHTML: longPage,
                              }),
                            ])
                          : null,
                        nodeTab.value === 'output'
                          ? h('div', { class: 'p-3', 'data-testid': 'output-overview' }, [
                              h('h4', { class: 'text-[13px] font-semibold' }, selNode.value?.label || ''),
                              h(
                                'pre',
                                { class: 'mt-2 overflow-auto bg-elevated p-2 font-mono text-[11px]' },
                                JSON.stringify(selRun.value?.outputs || { status: 'success' }, null, 2),
                              ),
                            ])
                          : null,
                      ]),
                    ],
                  ),
            ],
          ),
        ],
      )
  },
})

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  if (params.get('theme') === 'light') setTheme('light')
  createApp(Fixture).use(i18n).mount('#app')
}

void bootstrap()

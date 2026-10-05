import '../src/styles/global.css'
import { computed, createApp, defineComponent, h, ref, watch } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import ArtifactLoadingPane from '../src/components/run/ArtifactLoadingPane.vue'
import ClarifyProductStage from '../src/components/run/ClarifyProductStage.vue'
import ReviewShell from '../src/components/run/ReviewShell.vue'
import { api } from '../src/lib/api/api'
import {
  defaultClarifyProductId,
  listClarifyProductNodes,
  pickClarifyNodeRun,
  resolveClarifyProductStage,
} from '../src/lib/inbox/clarifyInboxStage'
import type { NodeRun, Run, WFNode } from '../src/lib/shared/types'
import { AUTO_CAPS, CLARIFY_CAPS, PAGE_CAPS, RESEARCH_CAPS, writesCaps } from '../src/test/capsFixtures'

type Scenario =
  | 'research'
  | 'react'
  | 'visual'
  | 'pending'
  | 'executedEmpty'
  | 'loadFailed'
  | 'loading'

const researchDoc = {
  summary: '调研结论摘要：澄清产物台应对齐 Gate 载荷。',
  findings: [{ title: '发现一', detail: 'inbox-context 需下发 nodes/artifacts。' }],
}
const planDoc = {
  title: '实施计划',
  goals: [{ title: '修复 clarify 空白', status: 'done' }],
}
const reactDoc = {
  title: '澄清需求',
  summary: '在审批箱澄清阶段展示结构化产物。',
  goals: ['预览当前节点产物'],
}
const visualPage = '<!doctype html><html><body><h1>视觉预览 Demo</h1><p>Inbox 复审主产物</p></body></html>'
const upstreamReqDoc = {
  title: '复审产物台展示上游澄清需求文档',
  summary: '对照审阅当前主产物',
  goals: ['就地查看上游澄清需求'],
  functional_requirements: [{ title: '常驻条 + 960px 只读模态', detail: '预览下方放大查看' }],
}

;(api as { artifactContent: (id: string) => Promise<{ content: string }> }).artifactContent = async (
  id: string,
) => {
  if (id === 'art-react' || id === 'art-visual-req') {
    return { content: JSON.stringify(id === 'art-visual-req' ? upstreamReqDoc : reactDoc) }
  }
  if (id === 'art-page') return { content: visualPage }
  return { content: '{}' }
}

function node(id: string, label: string, caps: WFNode['caps'] = AUTO_CAPS): WFNode {
  return { id, type: 'agent', label, position: { x: 0, y: 0 }, config: {}, caps }
}

function exec(nodeId: string, status: NodeRun['status'], outputs: Record<string, unknown> = {}): NodeRun {
  return { nodeId, iteration: 1, status, outputs }
}

function buildRun(scenario: Scenario): Run | null {
  if (scenario === 'loadFailed' || scenario === 'loading') return null

  if (scenario === 'pending') {
    // Non-PRODUCT inbox node → no tab selection; outer stage shows pending.
    return {
      id: 'run-clarify-pending',
      workflowId: 'wf',
      workflowName: 'wf',
      status: 'waiting_human',
      trigger: '',
      startedAt: '',
      durationSec: 0,
      progress: 0,
      nodes: [node('agent_1', '通用 Agent')],
      edges: [],
      artifacts: [],
      nodeExecutions: {
        agent_1: [exec('agent_1', 'pending')],
      },
      nodeRuns: {},
      clarifyByNode: {
        agent_1: { nodeId: 'agent_1', iteration: 1, turns: [], done: false },
      },
    }
  }

  if (scenario === 'executedEmpty') {
    // Non-PRODUCT inbox node already ran but has nothing StructuredProductPanel can show.
    return {
      id: 'run-clarify-empty',
      workflowId: 'wf',
      workflowName: 'wf',
      status: 'waiting_human',
      trigger: '',
      startedAt: '',
      durationSec: 0,
      progress: 0,
      nodes: [node('agent_1', '通用 Agent')],
      edges: [],
      artifacts: [],
      nodeExecutions: {
        agent_1: [exec('agent_1', 'waiting_human')],
      },
      nodeRuns: {},
      clarifyByNode: {
        agent_1: { nodeId: 'agent_1', iteration: 1, turns: [], done: false },
      },
    }
  }

  if (scenario === 'visual') {
    return {
      id: 'run-clarify-visual',
      workflowId: 'wf',
      workflowName: 'wf',
      status: 'waiting_human',
      trigger: '',
      startedAt: '',
      durationSec: 0,
      progress: 0,
      nodes: [node('visual', '视觉网页', PAGE_CAPS)],
      edges: [],
      artifacts: [
        {
          id: 'art-page',
          name: 'page.html',
          sizeBytes: 128,
          createdAt: '2026-07-23T00:00:00Z',
        },
        {
          id: 'art-visual-req',
          name: 'clarified_requirement.json',
          sizeBytes: 64,
          createdAt: '2026-07-23T00:00:00Z',
        },
      ],
      nodeExecutions: {
        visual: [exec('visual', 'waiting_human', { page: visualPage })],
      },
      nodeRuns: {},
      clarifyByNode: {
        visual: { nodeId: 'visual', iteration: 1, turns: [], done: false },
      },
    }
  }

  if (scenario === 'react') {
    return {
      id: 'run-clarify-react',
      workflowId: 'wf',
      workflowName: 'wf',
      status: 'waiting_human',
      trigger: '',
      startedAt: '',
      durationSec: 0,
      progress: 0,
      nodes: [node('react', '需求澄清', CLARIFY_CAPS)],
      edges: [],
      artifacts: [
        {
          id: 'art-react',
          name: 'clarified_requirement.json',
          sizeBytes: 64,
          createdAt: '2026-07-23T00:00:00Z',
        },
      ],
      nodeExecutions: {
        react: [
          exec('react', 'completed', {
            clarified_requirement_json: JSON.stringify(reactDoc),
          }),
        ],
      },
      nodeRuns: {},
      clarifyByNode: {
        react: { nodeId: 'react', iteration: 1, turns: [], done: false },
      },
    }
  }

  // research: multi-product slim range (research + plan)
  return {
    id: 'run-clarify-research',
    workflowId: 'wf',
    workflowName: 'wf',
    status: 'waiting_human',
    trigger: '',
    startedAt: '',
    durationSec: 0,
    progress: 0,
    nodes: [
      node('research_1', '调研结论', RESEARCH_CAPS),
      node('plan_1', '实施计划', writesCaps('plan', { review: true })),
    ],
    edges: [],
    artifacts: [
      {
        id: 'art-research',
        name: 'research.json',
        sizeBytes: 64,
        createdAt: '2026-07-23T00:00:00Z',
      },
      {
        id: 'art-plan',
        name: 'plan.json',
        sizeBytes: 64,
        createdAt: '2026-07-23T00:00:00Z',
      },
    ],
    nodeExecutions: {
      research_1: [
        exec('research_1', 'completed', {
          research_json: JSON.stringify(researchDoc),
        }),
      ],
      plan_1: [
        exec('plan_1', 'completed', {
          plan_json: JSON.stringify(planDoc),
        }),
      ],
    },
    nodeRuns: {},
    clarifyByNode: {
      research_1: { nodeId: 'research_1', iteration: 1, turns: [], done: false },
    },
  }
}

function inboxNodeId(scenario: Scenario): string {
  if (scenario === 'react') return 'react'
  if (scenario === 'visual') return 'visual'
  if (scenario === 'pending' || scenario === 'executedEmpty') return 'agent_1'
  return 'research_1'
}

const Fixture = defineComponent({
  name: 'ClarifyInboxProductFixture',
  setup() {
    const params = new URLSearchParams(location.search)
    const scenario = ref<Scenario>((params.get('scenario') as Scenario) || 'research')
    const loading = ref(scenario.value === 'loading')
    const loadError = ref(scenario.value === 'loadFailed')
    const run = ref<Run | null>(buildRun(scenario.value))
    const selectedProductId = ref<string | null>(null)
    let failOnce = scenario.value === 'loadFailed'

    const productNodes = computed(() => listClarifyProductNodes(run.value))
    watch(
      productNodes,
      (nodes) => {
        if (!nodes.length) {
          selectedProductId.value = null
          return
        }
        if (selectedProductId.value && nodes.some((n) => n.id === selectedProductId.value)) return
        selectedProductId.value = defaultClarifyProductId(inboxNodeId(scenario.value), nodes)
      },
      { immediate: true },
    )

    const selectedNode = computed(
      () => run.value?.nodes?.find((n) => n.id === selectedProductId.value) || null,
    )
    const selectedNodeRun = computed(() => {
      if (!selectedNode.value || !run.value) return null
      return pickClarifyNodeRun(run.value, selectedNode.value.id, 1)
    })
    const stageKind = computed(() =>
      resolveClarifyProductStage({
        loadError: loadError.value,
        run: run.value,
        inboxNodeId: inboxNodeId(scenario.value),
        inboxIteration: 1,
        selectedNode: selectedNode.value,
        selectedNodeRun: selectedNodeRun.value,
      }),
    )

    async function retry() {
      loading.value = true
      loadError.value = false
      run.value = null
      await new Promise((r) => setTimeout(r, 80))
      if (failOnce) {
        failOnce = false
        scenario.value = 'research'
        run.value = buildRun('research')
        loadError.value = false
      } else {
        loadError.value = true
      }
      loading.value = false
    }

    // loading scenario: hold spinner briefly then settle to research (for flash checks)
    if (scenario.value === 'loading') {
      setTimeout(() => {
        loading.value = false
        scenario.value = 'research'
        run.value = buildRun('research')
      }, 400)
    }

    return () =>
      h(
        'div',
        {
          'data-testid': 'clarify-inbox-product-root',
          class: 'flex h-full min-h-0 flex-col bg-surface',
        },
        [
          h(
            'div',
            { class: 'shrink-0 border-b border-line px-3 py-2 text-sm font-semibold' },
            `Clarify · ${scenario.value}`,
          ),
          h(
            'div',
            { class: 'min-h-0 flex-1', 'data-testid': 'clarify-inbox-product-panel' },
            loading.value
              ? [h(ArtifactLoadingPane, { messageKey: 'pages.gatesInbox.loadingRun' })]
              : [
                  h(
                    ReviewShell,
                    { class: 'min-h-0 flex-1' },
                    {
                      stage: () =>
                        h(ClarifyProductStage, {
                          productNodes: productNodes.value,
                          selectedProductId: selectedProductId.value,
                          stageKind: stageKind.value,
                          selectedNode: selectedNode.value,
                          selectedNodeRun: selectedNodeRun.value,
                          run: run.value,
                          loading: loading.value,
                          'onUpdate:selectedProductId': (id: string) => {
                            selectedProductId.value = id
                          },
                          onRetry: () => {
                            void retry()
                          },
                        }),
                      sidebar: () =>
                        h(
                          'div',
                          {
                            class: 'p-3 text-[12px] text-txt3',
                            'data-testid': 'clarify-composer-stub',
                          },
                          '澄清回复区',
                        ),
                    },
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
  const theme = new URLSearchParams(location.search).get('theme')
  setTheme(theme === 'light' ? 'light' : 'dark')
  installIdleScrollbar()
  createApp(Fixture).use(i18n).mount('#app')
}

void boot()

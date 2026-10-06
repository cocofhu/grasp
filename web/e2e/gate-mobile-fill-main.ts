import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import GateApproval from '../src/components/run/GateApproval.vue'
import type { Gate, Run } from '../src/lib/shared/types'

const pageHtml = `<!doctype html><html><body style="margin:0;font:16px/1.5 sans-serif">
${'<p>长内容段落用于预览区内滚动。</p>'.repeat(40)}
</body></html>`

const gate: Gate = {
  runId: 'run-gate-fill',
  nodeId: 'hg-visual',
  workflowName: 'wf',
  title: '审阅视觉稿',
  bodyMd: '请审阅',
  actions: [
    { id: 'approve', label: '通过' },
    { id: 'revise', label: '打回', requireForm: true },
  ],
  form: [{ key: 'comment', label: '评审意见' }],
  requestedAt: '2026-07-18T00:00:00Z',
  // Hot session so mobile-fill drawer exposes ReviewComposer reject/pass (f9).
  reactSessionAlive: true,
  reactUpstreamNodeId: 'visual',
  upstreamNodeId: 'visual',
  upstreamIteration: 1,
}

const run = {
  id: 'run-gate-fill',
  title: 'gate fill',
  workflowId: 'wf-1',
  workflowName: 'wf',
  status: 'waiting_human',
  createdAt: '2026-07-18T00:00:00Z',
  nodes: [
    {
      id: 'hg-visual',
      type: 'human_gate',
      label: '审阅视觉',
      position: { x: 0, y: 0 },
      config: { body_template: '{{nodes.visual.outputs.page}}' },
    },
  ],
  edges: [],
  nodeStates: {},
  artifacts: [
    {
      id: 'art-req',
      name: 'clarified_requirement.json',
      sizeBytes: 32,
      createdAt: '2026-07-18T00:00:00Z',
    },
  ],
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
  name: 'GateMobileFillFixture',
  setup() {
    // Force mobile breakpoint so GateApproval takes the fill-remaining branch.
    const mql = window.matchMedia('(max-width: 767px)')
    Object.defineProperty(mql, 'matches', { configurable: true, get: () => true })
    window.matchMedia = ((query: string) => {
      if (query.includes('max-width: 767px')) return mql
      return {
        matches: false,
        media: query,
        onchange: null,
        addListener() {},
        removeListener() {},
        addEventListener() {},
        removeEventListener() {},
        dispatchEvent() {
          return false
        },
      } as MediaQueryList
    }) as typeof window.matchMedia

    const resolved = ref(false)
    return () =>
      h(
        'div',
        {
          'data-testid': 'gate-mobile-fill-root',
          class: 'flex h-full min-h-0 flex-col bg-surface',
        },
        [
          h('div', { class: 'shrink-0 border-b border-line px-3 py-2 text-sm font-semibold' }, 'Run #gate-fill'),
          h(
            'div',
            { class: 'min-h-0 flex-1', 'data-testid': 'gate-mobile-fill-panel' },
            [
              h(GateApproval, {
                gate,
                run,
                fillPreview: true,
                mobileFillRemaining: true,
                compact: true,
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

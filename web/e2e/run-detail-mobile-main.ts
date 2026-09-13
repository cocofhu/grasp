import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import StatusPill from '../src/components/ui/StatusPill.vue'
import AppButton from '../src/components/ui/AppButton.vue'
import Icon from '../src/components/ui/Icon.vue'
import ExecutionStatsPanel from '../src/components/run/ExecutionStatsPanel.vue'
import ExecutionTimeline from '../src/components/run/ExecutionTimeline.vue'
import type { Run, WFNode } from '../src/lib/shared/types'

const LONG_REPOS = {
  repos: [
    { name: 'approving', path: '/root/workspace/approving', branch: 'implement/run-detail-mobile-responsive' },
    { name: 'sandbox-gateway', path: '/root/workspace/sandbox-gateway', branch: 'main' },
  ],
}

const nodes: WFNode[] = [
  { id: 'input', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: {} },
  { id: 'clarify', type: 'react', label: '需求澄清', position: { x: 120, y: 0 }, config: {} },
  { id: 'research', type: 'research', label: '技术调研', position: { x: 240, y: 0 }, config: {} },
]

const mockRun: Run = {
  id: 'run-a3f8c2',
  workflowId: 'wf-demo',
  workflowName: '非常非常非常长的工作流名称用于挤压状态芯片',
  workflowVersion: 12,
  status: 'waiting_human',
  trigger: 'manual',
  startedAt: '2026-07-16T00:00:00Z',
  durationSec: 3600,
  progress: 0.42,
  nodeRuns: {
    input: {
      nodeId: 'input',
      status: 'completed',
      startedAt: '2026-07-16T00:00:00Z',
      durationSec: 1,
      varsSnapshot: { idea: 'mobile' },
    },
    clarify: {
      nodeId: 'clarify',
      status: 'waiting_human',
      startedAt: '2026-07-16T00:01:00Z',
      durationSec: 3400,
      varsSnapshot: LONG_REPOS,
    },
    research: {
      nodeId: 'research',
      status: 'completed',
      startedAt: '2026-07-16T00:00:30Z',
      durationSec: 12,
      varsSnapshot: { topic: 'responsive' },
    },
  },
  nodeExecutions: {},
  artifacts: [],
  nodes,
  edges: [],
}

const initialView = new URLSearchParams(window.location.search).get('view') === 'timeline' ? 'timeline' : 'stats'

const RunDetailMobileFixture = defineComponent({
  name: 'RunDetailMobileFixture',
  setup() {
    const viewMode = ref<'timeline' | 'stats'>(initialView)
    const selected = ref('clarify')
    const nowMs = Date.now()
    const run = mockRun

    return () =>
      h(
        'div',
        {
          class: 'flex h-screen min-w-0 flex-col overflow-x-hidden bg-base',
          'data-testid': 'run-detail-root',
        },
        [
          h(
            'header',
            { class: 'shrink-0 overflow-x-hidden border-b border-line bg-surface px-5 py-3', 'data-testid': 'run-header' },
            [
              h('div', { class: 'flex min-w-0 flex-col gap-2 md:flex-row md:items-center md:gap-3' }, [
                h('div', { class: 'flex min-w-0 flex-1 items-center gap-2 md:gap-3', 'data-testid': 'run-header-row1' }, [
                  h('button', { class: 'flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-txt2', 'aria-label': '返回' }, [
                    h(Icon, { name: 'arrow-left', size: 18 }),
                  ]),
                  h('h1', { class: 'min-w-0 truncate text-[17px] font-semibold text-txt' }, `Run #${run.id.replace('run-', '')}`),
                  h(
                    'span',
                    {
                      class: 'chip hidden max-w-[9rem] truncate md:inline-flex',
                      title: run.workflowName,
                      'data-testid': 'workflow-chip',
                    },
                    run.workflowName,
                  ),
                  h('span', { class: 'chip shrink-0', 'data-testid': 'version-chip' }, `v${run.workflowVersion}`),
                  h('span', { 'data-testid': 'status-pill', class: 'shrink-0 inline-flex' }, [h(StatusPill, { status: run.status })]),
                ]),
                h(
                  'div',
                  {
                    class: 'flex flex-wrap items-center gap-2 pl-10 md:ml-auto md:shrink-0 md:pl-0',
                    'data-testid': 'run-header-actions',
                  },
                  [
                    h(AppButton, { variant: 'ghost', size: 'sm', icon: 'edit' }, { default: () => '编辑' }),
                    h(AppButton, { variant: 'ghost', size: 'sm', icon: 'doc' }, { default: () => '详情' }),
                    h(
                      'button',
                      {
                        class:
                          'inline-flex items-center gap-1.5 rounded-md border border-line bg-surface px-2.5 py-1.5 text-xs font-medium text-txt',
                      },
                      [h(Icon, { name: 'refresh', size: 14 }), ' 刷新'],
                    ),
                    h(AppButton, { variant: 'danger', size: 'sm', icon: 'close' }, { default: () => '取消运行' }),
                  ],
                ),
              ]),
            ],
          ),
          h('div', { class: 'flex shrink-0 items-center gap-2 border-b border-line bg-surface px-3 py-2' }, [
            h(
              'button',
              {
                class: viewMode.value === 'timeline' ? 'rounded-md bg-accent-dim px-2.5 py-1 text-accent' : 'rounded-md px-2.5 py-1 text-txt3',
                onClick: () => {
                  viewMode.value = 'timeline'
                },
              },
              '时间线',
            ),
            h(
              'button',
              {
                class: viewMode.value === 'stats' ? 'rounded-md bg-accent-dim px-2.5 py-1 text-accent' : 'rounded-md px-2.5 py-1 text-txt3',
                onClick: () => {
                  viewMode.value = 'stats'
                },
              },
              '执行统计',
            ),
          ]),
          viewMode.value === 'stats'
            ? h('div', { class: 'min-h-0 flex-1', 'data-testid': 'stats-panel' }, [
                h(ExecutionStatsPanel, {
                  run,
                  nodes,
                  wallSec: run.durationSec,
                  nowMs,
                  statsTab: 'single',
                }),
              ])
            : h('div', { class: 'min-h-0 flex-1', 'data-testid': 'timeline-panel' }, [
                h(ExecutionTimeline, {
                  run,
                  nodes,
                  selectedNodeId: selected.value,
                  selectedExecIdx: 0,
                  interactive: true,
                  nowMs,
                  onSelect: (nodeId: string) => {
                    selected.value = nodeId
                  },
                }),
              ]),
        ],
      )
  },
})

installIdleScrollbar()

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')

  const params = new URLSearchParams(window.location.search)
  if (params.get('theme') === 'light') {
    setTheme('light')
  }

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: RunDetailMobileFixture }],
  })
  await router.push('/')

  createApp({ render: () => h(RouterView) })
    .directive('hover-ink', vHoverInk)
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()

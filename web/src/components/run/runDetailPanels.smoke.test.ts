// @vitest-environment happy-dom
/**
 * Smoke-mount Demo「入口只装配」抽离的 Run* 面板壳，计入 coverage 分母。
 */
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { useClarifyDraft } from '@/lib/inbox/useClarifyDraft'
import type { Gate, NodeRun, Run, WFNode } from '@/lib/shared/types'
import RunGatePanel from './RunGatePanel.vue'
import RunLogPanel from './RunLogPanel.vue'
import RunSandboxPanel from './RunSandboxPanel.vue'
import RunClarifyPanel from './RunClarifyPanel.vue'
import RunOutputPanel from './RunOutputPanel.vue'
import RunPreviewPanel from './RunPreviewPanel.vue'
import RunProductPanel from './RunProductPanel.vue'
import RunReviewPanel from './RunReviewPanel.vue'
import ReactArtifactStage from './ReactArtifactStage.vue'

function i18n() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
}

const stubNode = {
  id: 'n1',
  type: 'agent',
  label: 'Agent',
  position: { x: 0, y: 0 },
  config: {},
} as WFNode

const stubNodeRun = {
  nodeId: 'n1',
  status: 'completed',
} as NodeRun

const stubRun = {
  id: 'run-1',
  workflowId: 'wf-1',
  workflowName: 'wf',
  status: 'running',
  trigger: 'manual',
  startedAt: '2026-08-12T00:00:00Z',
  durationSec: 10,
  progress: 50,
  nodeRuns: { n1: stubNodeRun },
  artifacts: [],
} as unknown as Run

const stubGate = {
  runId: 'run-1',
  nodeId: 'n1',
  workflowName: 'wf',
  title: 'Gate',
  bodyMd: 'ok?',
  actions: [{ id: 'approve', label: '通过' }],
  requestedAt: '2026-08-12T00:00:00Z',
} as Gate

const heavyStubs = {
  GateApproval: true,
  LiveLogPanel: true,
  ClarifyChat: true,
  ClarifyBootLoader: true,
  ReviewShell: true,
  ReactArtifactStage: true,
  ReviewComposer: true,
  AppPreviewPanel: true,
  StructuredProductPanel: true,
  NodeOutputPanel: true,
  Icon: true,
  RefreshStrip: true,
  HardLoadLayer: true,
  StatusPill: true,
}

describe('Run detail panel shells (Demo entry assembly)', () => {
  it('mounts gate/log/sandbox/clarify/output/preview/product/review shells', async () => {
    const plugins = [i18n()]
    const global = { plugins, stubs: heavyStubs }

    const gate = mount(RunGatePanel, {
      props: { gate: stubGate, run: stubRun, submitError: null },
      global,
    })
    expect(gate.exists()).toBe(true)
    ;(gate.vm as any).applyReviewFrame?.({ type: 'x' })
    gate.unmount()

    const log = mount(RunLogPanel, {
      props: { events: [], live: false, status: 'running' },
      global,
    })
    expect(log.exists()).toBe(true)
    log.unmount()

    const sandbox = mount(RunSandboxPanel, {
      props: {
        loading: false,
        sbxLog: { content: 'hello', live: true, found: true },
        selStatus: 'running',
      },
      global,
    })
    expect(sandbox.text()).toMatch(/沙箱|log|Log/i)
    sandbox.unmount()

    const clarify = mount(RunClarifyPanel, {
      props: {
        sandboxFailed: false,
        nodeLabel: '澄清',
        nodeId: 'c1',
        clarify: { nodeId: 'c1', turns: [], done: false },
        runId: 'run-1',
        run: stubRun,
        draft: '',
        attachments: [],
        inputActive: true,
        selStatus: 'waiting_human',
      },
      global: {
        plugins,
        stubs: {
          ...heavyStubs,
          ReviewShell: defineComponent({
            template: '<div data-testid="clarify-review-shell"><slot name="stage" /><slot name="sidebar" /></div>',
          }),
          ReactArtifactStage: false,
          HtmlPreview: true,
          ArtifactPreview: true,
          ClarifyChat: defineComponent({
            emits: ['send', 'finish', 'cancel'],
            template:
              '<button data-testid="clarify-send" @click="$emit(\'send\', \'hi\', [], [])">send</button>',
          }),
        },
      },
    })
    expect(clarify.find('[data-testid="react-artifact-tab-grid"]').exists()).toBe(true)
    expect(clarify.find('[data-testid="react-artifact-tab-preview"]').exists()).toBe(false)
    expect(clarify.find('[data-testid="react-artifact-card-novnc"]').exists()).toBe(true)
    const stage = clarify.findComponent(ReactArtifactStage)
    expect(stage.props('annotatable')).toBe(true)
    expect(stage.props('nodeId')).toBe('c1')
    await clarify.get('[data-testid="clarify-send"]').trigger('click')
    expect(clarify.emitted('send')?.[0]).toEqual(['hi', [], []])
    clarify.unmount()

    const output = mount(RunOutputPanel, {
      props: { node: stubNode, nodeRun: stubNodeRun, run: stubRun },
      global,
    })
    expect(output.exists()).toBe(true)
    output.unmount()

    const preview = mount(RunPreviewPanel, {
      props: { runId: 'run-1', nodeId: 'n1' },
      global,
    })
    expect(preview.exists()).toBe(true)
    preview.unmount()

    const product = mount(RunProductPanel, {
      props: { node: stubNode, nodeRun: stubNodeRun, run: stubRun },
      global,
    })
    expect(product.exists()).toBe(true)
    product.unmount()

    const review = mount(RunReviewPanel, {
      props: {
        mobile: false,
        node: stubNode,
        nodeRun: stubNodeRun,
        run: stubRun,
        clarify: { nodeId: 'n1', turns: [], done: false },
        draft: '',
        attachments: [],
        annotations: [],
        inputActive: true,
        selStatus: 'waiting_human',
      },
      global,
    })
    expect(review.exists()).toBe(true)
    await flushPromises()
    review.unmount()
  })

  it('clarify preview picks keep the element context for the agent', async () => {
    const pick = {
      selector: 'body > ul > li:nth-of-type(2) > button',
      tagName: 'BUTTON',
      text: 'Buy pro',
      outerHTML: '<button class="buy">Buy pro</button>',
      url: 'http://10.0.0.5:18080/',
    }
    const clarify = mount(RunClarifyPanel, {
      props: {
        sandboxFailed: false,
        nodeLabel: '澄清',
        nodeId: 'pick-node',
        clarify: { nodeId: 'pick-node', turns: [], done: false },
        runId: 'run-pick',
        run: stubRun,
        draft: '',
        attachments: [],
        inputActive: true,
        selStatus: 'waiting_human',
      },
      global: {
        plugins: [i18n()],
        stubs: {
          ...heavyStubs,
          ReviewShell: defineComponent({ template: '<div><slot name="stage" /></div>' }),
          ReactArtifactStage: defineComponent({
            emits: ['pick'],
            setup: (_, { emit }) => ({ fire: () => emit('pick', pick) }),
            template: '<button data-testid="stage-pick" @click="fire">pick</button>',
          }),
        },
      },
    })
    await clarify.get('[data-testid="stage-pick"]').trigger('click')
    const { annotations } = useClarifyDraft('run-pick', () => 'pick-node')
    expect(annotations.value).toEqual([
      {
        selector: pick.selector,
        url: pick.url,
        label: '/ · body > ul > li:nth-of-type(2) > button',
        tagName: 'button',
        text: 'Buy pro',
        outerHTML: pick.outerHTML,
      },
    ])
    clarify.unmount()
  })

  it('keeps clarify and review shells mounted while their sessions connect', () => {
    const plugins = [i18n()]
    const ReviewShellStub = defineComponent({
      template: '<div data-testid="run-review-shell"><slot name="stage" /><slot name="sidebar" /></div>',
    })
    const global = {
      plugins,
      stubs: {
        Icon: true,
        StatusPill: true,
        ReviewShell: ReviewShellStub,
      },
    }

    const clarify = mount(RunClarifyPanel, {
      props: {
        sandboxFailed: false,
        nodeLabel: '澄清',
        nodeId: 'c1',
        clarify: null,
        runId: 'run-1',
        run: stubRun,
        draft: '',
        attachments: [],
        inputActive: false,
        selStatus: 'pending',
      },
      global,
    })
    expect(clarify.find('[data-testid="run-review-shell"]').exists()).toBe(true)
    expect(clarify.find('[data-testid="react-connecting-stage"]').exists()).toBe(true)
    expect(clarify.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)
    expect(clarify.find('[data-testid="react-connecting-confirm"]').exists()).toBe(false)
    // plan g2.2: connecting stage only shows pipeline artifacts skeleton (no preview chrome Tab)
    expect(clarify.get('[data-testid="react-connecting-tab-pipeline"]').text()).toContain('流水线产物')
    expect(clarify.get('[data-testid="react-connecting-tab-pipeline"]').attributes('aria-selected')).toBe('true')
    expect(clarify.find('[data-testid="react-connecting-tab-preview"]').exists()).toBe(false)
    expect(clarify.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(clarify.get('[data-testid="react-connecting-pipeline-skeleton"]').exists()).toBe(true)

    const review = mount(RunReviewPanel, {
      props: {
        mobile: false,
        node: stubNode,
        nodeRun: stubNodeRun,
        run: stubRun,
        clarify: null,
        draft: '',
        attachments: [],
        annotations: [],
        inputActive: false,
        selStatus: 'starting',
      },
      global,
    })
    expect(review.find('[data-testid="run-review-shell"]').exists()).toBe(true)
    expect(review.find('[data-testid="react-connecting-stage"]').exists()).toBe(true)
    expect(review.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)
    expect((review.get('[data-testid="react-connecting-confirm"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(review.find('[data-testid="react-connecting-tab-preview"]').exists()).toBe(false)
    expect(review.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(review.get('[data-testid="react-connecting-pipeline-skeleton"]').exists()).toBe(true)
  })
})

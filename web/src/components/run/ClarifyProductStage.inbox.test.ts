// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyProductStage from './ClarifyProductStage.vue'
import { useReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import { adaptInboxContextToRun, type ClarifyInboxContext } from '@/lib/inbox/inboxContext'
import {
  listClarifyProductNodes,
  pickClarifyNodeRun,
  resolveClarifyProductStage,
} from '@/lib/inbox/clarifyInboxStage'
import { TEST_REVIEW_CAPS } from '@/test/capsFixtures'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      artifactContent: apiMocks.artifactContent,
    },
  }
})

const StructuredStub = defineComponent({
  name: 'StructuredArtifactView',
  props: { name: String, doc: Object },
  setup() {
    const channel = useReviewAnnotate()
    return { channel }
  },
  template: `
    <div data-testid="structured-view">
      <span v-if="doc && doc.title">{{ doc.title }}</span>
      <span v-else-if="doc && doc.summary">{{ doc.summary }}</span>
    </div>
  `,
})

function inboxPayload(secondOutputs: Record<string, string>): ClarifyInboxContext {
  return {
    type: 'clarify',
    status: 'waiting_human',
    nodes: [
      {
        id: 'test',
        type: 'agent',
        caps: TEST_REVIEW_CAPS,
        label: '测试',
        position: { x: 0, y: 0 },
        config: {},
      },
    ],
    artifacts: [
      {
        id: 'art-live',
        name: 'test_result.json',
        kind: 'json',
        nodeId: 'test',
        runId: 'run-inbox',
        workflowName: 'wf',
        sizeBytes: 40,
        createdAt: '2026-10-08T00:00:00Z',
      },
    ],
    nodeExecutions: {
      test: [
        {
          nodeId: 'test',
          iteration: 1,
          status: 'failed',
          outputs: {
            test_result_json: JSON.stringify({ title: '第1次收件箱结论', summary: 'iter1' }),
            review_json: JSON.stringify({ title: '第1次评审', verdict: 'request_changes' }),
          },
        },
        {
          nodeId: 'test',
          iteration: 2,
          status: 'waiting_human',
          outputs: secondOutputs,
        },
      ],
    },
    clarify: {
      nodeId: 'test',
      iteration: 2,
      turns: [{ role: 'agent', text: '请看这次', at: '2026-10-08T00:00:00Z' }],
      done: false,
      label: '测试',
    },
  }
}

function mountInbox(secondOutputs: Record<string, string>) {
  const run = adaptInboxContextToRun(inboxPayload(secondOutputs), 'run-inbox')
  const products = listClarifyProductNodes(run)
  const selected = products.find((n) => n.id === 'test') || null
  const selectedRun = pickClarifyNodeRun(run, 'test', 2)
  const stageKind = resolveClarifyProductStage({
    loadError: false,
    run,
    inboxNodeId: 'test',
    inboxIteration: 2,
    selectedNode: selected,
    selectedNodeRun: selectedRun,
  })
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const wrapper = mount(ClarifyProductStage, {
    props: {
      productNodes: products,
      selectedProductId: selected?.id ?? null,
      stageKind,
      selectedNode: selected,
      selectedNodeRun: selectedRun,
      run,
    },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        StructuredArtifactView: StructuredStub,
        HtmlPreview: true,
        AppModal: true,
      },
    },
    attachTo: document.body,
  })
  return { wrapper, run, selected }
}

describe('ClarifyProductStage inbox payload', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactContent.mockResolvedValue({
      content: JSON.stringify({ title: '活文件旧结论', summary: 'store' }),
    })
  })

  it('renders the conclusion this inbox execution wrote', async () => {
    const { wrapper, run, selected } = mountInbox({
      test_result_json: JSON.stringify({ title: '第2次收件箱结论', summary: 'iter2' }),
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-product-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-view"]').text()).toContain('第2次收件箱结论')
    expect(wrapper.find('[data-testid="structured-product-tab-review.json"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('第1次收件箱结论')
    expect(wrapper.text()).not.toContain('活文件旧结论')
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()

    const first = pickClarifyNodeRun(run, 'test', 1)
    await wrapper.setProps({ selectedNodeRun: first, selectedNode: selected })
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-view"]').text()).toContain('第1次收件箱结论')
    expect(wrapper.text()).not.toContain('活文件旧结论')
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows the empty state when this inbox execution has no snapshot', async () => {
    const { wrapper } = mountInbox({})
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="structured-product-tab-test_result.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="structured-product-tab-review.json"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('该节点尚未写入结构化产物')
    expect(wrapper.text()).not.toContain('活文件旧结论')
    expect(wrapper.text()).not.toContain('第1次收件箱结论')
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

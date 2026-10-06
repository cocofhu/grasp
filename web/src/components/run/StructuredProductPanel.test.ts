// @vitest-environment happy-dom
import { defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact, NodeRun, Run, WFNode } from '@/lib/shared/types'
import { useReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import StructuredProductPanel from './StructuredProductPanel.vue'
import { ASK_CAPS, CLARIFY_CAPS, writesCaps } from '@/test/capsFixtures'

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
      <span>{{ name }}</span>
      <span v-if="doc && doc.title">{{ doc.title }}</span>
      <ul v-if="doc && doc.goals">
        <li v-for="(g, i) in doc.goals" :key="i">{{ g }}</li>
      </ul>
      <button
        v-if="channel && channel.enabled"
        data-testid="annotate-affordance"
        type="button"
      >↗ 标注</button>
    </div>
  `,
})

const HtmlPreviewStub = defineComponent({
  name: 'HtmlPreview',
  props: { html: String, inspectable: Boolean, fillParent: Boolean },
  template:
    '<div data-testid="html-preview" :data-fill-parent="fillParent ? \'1\' : \'0\'" :data-inspectable="inspectable ? \'1\' : \'0\'">{{ html }}</div>',
})

const AppModalStub = defineComponent({
  name: 'AppModal',
  props: {
    open: Boolean,
    title: String,
    width: Number,
    closeOnBackdrop: { type: Boolean, default: true },
  },
  emits: ['close'],
  template: `
    <div
      v-if="open"
      data-testid="upstream-enlarge-modal"
      :data-width="width"
      :data-close-on-backdrop="closeOnBackdrop ? '1' : '0'"
    >
      <div data-testid="modal-backdrop" @click="closeOnBackdrop && $emit('close')" />
      <button data-testid="modal-close" type="button" @click="$emit('close')">close</button>
      <slot />
      <slot name="footer" />
    </div>
  `,
})

function artifact(overrides: Partial<Artifact> = {}): Artifact {
  return {
    id: 'a1',
    name: 'research.json',
    kind: 'json',
    nodeId: 'research',
    runId: 'run-1',
    workflowName: 'wf',
    sizeBytes: 10,
    createdAt: '2026-07-18T00:00:00Z',
    ...overrides,
  }
}

function visualNode(): WFNode {
  return { id: 'visual', type: 'agent', caps: writesCaps('page', { review: true }), label: '视觉', position: { x: 0, y: 0 }, config: {} }
}

function reactNode(): WFNode {
  return { id: 'react', type: 'agent', caps: ASK_CAPS, label: '需求澄清', position: { x: 0, y: 0 }, config: {} }
}

const REQ_DOC = {
  title: '复审产物台展示上游澄清需求文档',
  summary: '对照审阅当前主产物',
  goals: ['就地查看上游澄清需求'],
}

function runWithArtifacts(artifacts: Artifact[], extras: Partial<Run> = {}): Run {
  return {
    id: 'run-1',
    status: 'waiting_human',
    artifacts,
    ...extras,
  } as unknown as Run
}

function mountPanel(node: WFNode, nodeRun: NodeRun, run: Run, opts?: { annotatable?: boolean }) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(StructuredProductPanel, {
    props: { node, nodeRun, run, annotatable: opts?.annotatable },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        StructuredArtifactView: StructuredStub,
        HtmlPreview: HtmlPreviewStub,
        AppModal: AppModalStub,
      },
    },
    attachTo: document.body,
  })
}

describe('StructuredProductPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactContent.mockResolvedValue({
      content: JSON.stringify({ summary: '调研', findings: [{ title: 'f1' }] }),
    })
  })

  it('loads research.json from node output snapshot', async () => {
    const node: WFNode = {
      id: 'research',
      type: 'agent', caps: writesCaps('research'),
      label: '调研',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'research',
      iteration: 1,
      status: 'completed',
      outputs: { research_json: JSON.stringify({ summary: '调研', findings: [] }) },
    }
    const run = {
      id: 'run-1',
      artifacts: [
        {
          id: 'a1',
          name: 'research.json',
          kind: 'json',
          nodeId: 'research',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 10,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    } as unknown as Run
    const wrapper = mountPanel(node, nodeRun, run)
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders visual node HTML preview from execution snapshot', async () => {
    const node: WFNode = {
      id: 'visual',
      type: 'agent', caps: writesCaps('page', { review: true }),
      label: '视觉',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>demo</body></html>' },
    }
    const run = { id: 'run-1', artifacts: [] } as unknown as Run
    const wrapper = mountPanel(node, nodeRun, run)
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('review annotatable visual follows live store over stale snap', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      content: '<!doctype html><html><body>live-review</body></html>',
    })
    const node: WFNode = {
      id: 'visual',
      type: 'agent', caps: writesCaps('page', { review: true }),
      label: '视觉',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'waiting_human',
      outputs: { page: '<!doctype html><html><body>stale</body></html>' },
    }
    const run = {
      id: 'run-1',
      artifacts: [
        {
          id: 'a-page',
          name: 'page.html',
          kind: 'html',
          nodeId: 'visual',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 50,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    } as unknown as Run
    const wrapper = mountPanel(node, nodeRun, run, { annotatable: true })
    await flushPromises()
    expect(apiMocks.artifactContent).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('live-review')
    wrapper.unmount()
  })

  it('completed historical visual tab keeps frozen snap and ignores store', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      content: '<!doctype html><html><body>latest-store</body></html>',
    })
    const node: WFNode = {
      id: 'visual',
      type: 'agent', caps: writesCaps('page', { review: true }),
      label: '视觉',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-v1</body></html>' },
    }
    const run = {
      id: 'run-1',
      artifacts: [
        {
          id: 'a-page',
          name: 'page.html',
          kind: 'html',
          nodeId: 'visual',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 99,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    } as unknown as Run
    const wrapper = mountPanel(node, nodeRun, run)
    await flushPromises()
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('frozen-v1')
    wrapper.unmount()
  })

  it('completed + annotatable still freezes snap (annotatable ≠ followLive)', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      content: '<!doctype html><html><body>live-should-not-win</body></html>',
    })
    const node: WFNode = {
      id: 'visual',
      type: 'agent', caps: writesCaps('page', { review: true }),
      label: '视觉',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-history</body></html>' },
    }
    const run = {
      id: 'run-1',
      artifacts: [
        {
          id: 'a-page',
          name: 'page.html',
          kind: 'html',
          nodeId: 'visual',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 80,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    } as unknown as Run
    const wrapper = mountPanel(node, nodeRun, run, { annotatable: true })
    await flushPromises()
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('frozen-history')
    wrapper.unmount()
  })

  it('switches only eligible visual snapshots and makes historical preview read-only', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      content: '<!doctype html><html><body>live-latest</body></html>',
    })
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 4,
      status: 'waiting_human',
      outputs: { page: '<!doctype html><html><body>latest-snapshot</body></html>' },
    }
    const run = {
      id: 'run-1',
      artifacts: [
        {
          id: 'a-page',
          name: 'page.html',
          kind: 'html',
          nodeId: 'visual',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 50,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
      nodeExecutions: {
        visual: [
          {
            nodeId: 'visual',
            iteration: 1,
            status: 'completed',
            outputs: { page: '<!doctype html><html><body>frozen-v1</body></html>' },
          },
          { nodeId: 'visual', iteration: 2, status: 'failed', outputs: { page: '<html>failed</html>' } },
          { nodeId: 'visual', iteration: 3, status: 'completed', outputs: {} },
          {
            nodeId: 'visual',
            iteration: 4,
            status: 'waiting_human',
            outputs: { page: '<!doctype html><html><body>frozen-v4</body></html>' },
          },
        ],
      },
    } as unknown as Run
    const wrapper = mountPanel(visualNode(), nodeRun, run, { annotatable: true })
    await flushPromises()

    const select = wrapper.find('[data-testid="structured-product-version-select"]')
    expect(select.exists()).toBe(true)
    expect(select.findAll('option')).toHaveLength(2)
    expect(select.text()).toContain('第 4 次 · 最新')
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('live-latest')

    await select.setValue('1')
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('frozen-v1')
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-inspectable')).toBe('0')
    expect(wrapper.find('[data-testid="structured-product-historical-banner"]').text()).toContain(
      '历史版本 · 只读',
    )
    expect(wrapper.emitted('update:historicalPreview')?.slice(-1)[0]).toEqual([true])

    await select.setValue('4')
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('live-latest')
    expect(wrapper.find('[data-testid="structured-product-historical-banner"]').exists()).toBe(false)
    expect(wrapper.emitted('update:historicalPreview')?.slice(-1)[0]).toEqual([false])
    wrapper.unmount()
  })

  it('includes the current waiting-for-review snapshot and follows a newly added latest version', async () => {
    const firstVersion: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-v1</body></html>' },
    }
    const reviewVersion: NodeRun = {
      nodeId: 'visual',
      iteration: 2,
      status: 'waiting_human',
      outputs: { page: '<!doctype html><html><body>review-v2</body></html>' },
    }
    const run = runWithArtifacts([], {
      nodeExecutions: { visual: [firstVersion, reviewVersion] },
    })
    const wrapper = mountPanel(visualNode(), reviewVersion, run, { annotatable: true })
    await flushPromises()

    const select = wrapper.find('[data-testid="structured-product-version-select"]')
    expect(select.exists()).toBe(true)
    expect(select.findAll('option')).toHaveLength(2)
    expect(select.text()).toContain('第 2 次 · 最新')
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('review-v2')

    const newestVersion: NodeRun = {
      nodeId: 'visual',
      iteration: 3,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-v3</body></html>' },
    }
    await wrapper.setProps({
      nodeRun: newestVersion,
      run: runWithArtifacts([], { nodeExecutions: { visual: [firstVersion, reviewVersion, newestVersion] } }),
    })
    await flushPromises()
    expect((select.element as HTMLSelectElement).value).toBe('3')
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('frozen-v3')
    wrapper.unmount()
  })

  it('hides the visual version picker when only one snapshot is eligible', async () => {
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>only</body></html>' },
    }
    const run = {
      id: 'run-1',
      artifacts: [],
      nodeExecutions: { visual: [nodeRun] },
    } as unknown as Run
    const wrapper = mountPanel(visualNode(), nodeRun, run)
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-product-version-select"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('only')
    wrapper.unmount()
  })

  it('defaults to the latest eligible page instead of the run-detail execution selection', async () => {
    const historicalNodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>selected-execution-v1</body></html>' },
    }
    const run = {
      id: 'run-1',
      artifacts: [],
      nodeExecutions: {
        visual: [
          historicalNodeRun,
          {
            nodeId: 'visual',
            iteration: 2,
            status: 'completed',
            outputs: { page: '<!doctype html><html><body>latest-v2</body></html>' },
          },
        ],
      },
    } as unknown as Run
    const wrapper = mountPanel(visualNode(), historicalNodeRun, run)
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('latest-v2')
    expect(
      (wrapper.find('[data-testid="structured-product-version-select"]').element as HTMLSelectElement).value,
    ).toBe('2')
    wrapper.unmount()
  })

  it('shows persistent upstream bar when run has clarified_requirement.json and main is page.html', async () => {
    apiMocks.artifactContent.mockImplementation(async (id: string) => {
      if (id === 'a-req') return { content: JSON.stringify(REQ_DOC) }
      return { content: '<!doctype html><html><body>page</body></html>' }
    })
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-v1</body></html>' },
    }
    const run = runWithArtifacts([
      artifact({ id: 'a-page', name: 'page.html', kind: 'html', nodeId: 'visual' }),
      artifact({ id: 'a-req', name: 'clarified_requirement.json', kind: 'json', nodeId: 'react' }),
    ])
    const wrapper = mountPanel(visualNode(), nodeRun, run, { annotatable: true })
    await flushPromises()

    const bar = wrapper.find('[data-testid="upstream-context"]')
    expect(bar.exists()).toBe(true)
    expect(bar.attributes('data-variant')).toBe('persistent-bar')
    expect(wrapper.find('[data-testid="upstream-context-toggle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="upstream-bar-hint"]').text()).toContain('上游上下文')
    expect(wrapper.find('[data-testid="upstream-enlarge"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-product-name"]').text()).toBe('page.html')
    expect(wrapper.find('[data-testid="structured-product-header"]').text()).not.toContain(
      'clarified_requirement.json',
    )
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-fill-parent')).toBe('1')
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-inspectable')).toBe('1')
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('hides upstream bar when clarified_requirement.json is absent', async () => {
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>demo</body></html>' },
    }
    const run = runWithArtifacts([
      artifact({ id: 'a-page', name: 'page.html', kind: 'html', nodeId: 'visual' }),
    ])
    const wrapper = mountPanel(visualNode(), nodeRun, run)
    await flushPromises()
    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hides upstream bar when main product is clarified_requirement.json', async () => {
    const nodeRun: NodeRun = {
      nodeId: 'react',
      iteration: 1,
      status: 'completed',
      outputs: { clarified_requirement_json: JSON.stringify(REQ_DOC) },
    }
    const run = runWithArtifacts([
      artifact({ id: 'a-req', name: 'clarified_requirement.json', kind: 'json', nodeId: 'react' }),
    ])
    const wrapper = mountPanel(reactNode(), nodeRun, run, { annotatable: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-product-name"]').text()).toBe(
      'clarified_requirement.json',
    )
    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="annotate-affordance"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('opens 960px readonly upstream modal without ↗; main preview stays annotatable', async () => {
    apiMocks.artifactContent.mockImplementation(async (id: string) => {
      if (id === 'a-req') return { content: JSON.stringify(REQ_DOC) }
      return { content: '<html>page</html>' }
    })
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-v1</body></html>' },
    }
    const run = runWithArtifacts([
      artifact({ id: 'a-page', name: 'page.html', kind: 'html', nodeId: 'visual' }),
      artifact({ id: 'a-req', name: 'clarified_requirement.json', kind: 'json', nodeId: 'react' }),
    ])
    const wrapper = mountPanel(visualNode(), nodeRun, run, { annotatable: true })
    await flushPromises()

    await wrapper.find('[data-testid="upstream-enlarge"]').trigger('click')
    await flushPromises()

    const modal = wrapper.find('[data-testid="upstream-enlarge-modal"]')
    expect(modal.exists()).toBe(true)
    expect(modal.attributes('data-width')).toBe('960')
    expect(modal.attributes('data-close-on-backdrop')).toBe('0')
    expect(modal.text()).toContain('复审产物台展示上游澄清需求文档')
    expect(modal.text()).toContain('就地查看上游澄清需求')
    expect(wrapper.find('[data-testid="upstream-modal-readonly-footer"]').text()).toContain(
      '只读对照',
    )
    expect(modal.find('[data-testid="annotate-affordance"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-inspectable')).toBe('1')

    await wrapper.find('[data-testid="upstream-modal-mode-raw"]').trigger('click')
    await nextTick()
    expect(wrapper.find('.json-code-view').exists()).toBe(true)

    await wrapper.find('[data-testid="modal-backdrop"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="upstream-enlarge-modal"]').exists()).toBe(true)

    await wrapper.find('[data-testid="modal-close"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="upstream-enlarge-modal"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('lazy-loads requirement content; failed load shows retry without blocking preview', async () => {
    apiMocks.artifactContent.mockImplementation(async (id: string) => {
      if (id === 'a-req') throw new Error('network down')
      return { content: '<html>page</html>' }
    })
    const nodeRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-v1</body></html>' },
    }
    const run = runWithArtifacts([
      artifact({ id: 'a-page', name: 'page.html', kind: 'html', nodeId: 'visual' }),
      artifact({ id: 'a-req', name: 'clarified_requirement.json', kind: 'json', nodeId: 'react' }),
    ])
    const wrapper = mountPanel(visualNode(), nodeRun, run, { annotatable: true })
    await flushPromises()
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="html-preview"]').text()).toContain('frozen-v1')

    await wrapper.find('[data-testid="upstream-enlarge"]').trigger('click')
    await flushPromises()
    expect(apiMocks.artifactContent).toHaveBeenCalledWith('a-req')
    expect(wrapper.find('[data-testid="upstream-modal-retry"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)

    apiMocks.artifactContent.mockImplementation(async (id: string) => {
      if (id === 'a-req') return { content: JSON.stringify(REQ_DOC) }
      return { content: '<html>page</html>' }
    })
    await wrapper.find('[data-testid="upstream-modal-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="upstream-enlarge-modal"]').text()).toContain(
      '复审产物台展示上游澄清需求文档',
    )
    wrapper.unmount()
  })

  it('hides bar and closes modal when switching main product to clarified_requirement.json', async () => {
    apiMocks.artifactContent.mockImplementation(async (id: string) => {
      if (id === 'a-req') return { content: JSON.stringify(REQ_DOC) }
      return { content: '<html>page</html>' }
    })
    const visualRun: NodeRun = {
      nodeId: 'visual',
      iteration: 1,
      status: 'completed',
      outputs: { page: '<!doctype html><html><body>frozen-v1</body></html>' },
    }
    const reactRun: NodeRun = {
      nodeId: 'react',
      iteration: 1,
      status: 'completed',
      outputs: { clarified_requirement_json: JSON.stringify(REQ_DOC) },
    }
    const run = runWithArtifacts([
      artifact({ id: 'a-page', name: 'page.html', kind: 'html', nodeId: 'visual' }),
      artifact({ id: 'a-req', name: 'clarified_requirement.json', kind: 'json', nodeId: 'react' }),
    ])
    const wrapper = mountPanel(visualNode(), visualRun, run, { annotatable: true })
    await flushPromises()
    await wrapper.find('[data-testid="upstream-enlarge"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="upstream-enlarge-modal"]').exists()).toBe(true)

    await wrapper.setProps({ node: reactNode(), nodeRun: reactRun })
    await flushPromises()
    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="upstream-enlarge-modal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="structured-product-name"]').text()).toBe(
      'clarified_requirement.json',
    )

    await wrapper.setProps({ node: visualNode(), nodeRun: visualRun })
    await flushPromises()
    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="upstream-enlarge-modal"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('approve optional tabs ignore upstream same-named artifacts', async () => {
    const node: WFNode = {
      id: 'approve',
      type: 'agent', caps: CLARIFY_CAPS,
      label: 'Approve',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'approve',
      iteration: 1,
      status: 'completed',
      outputs: {
        clarified_requirement_json: JSON.stringify(REQ_DOC),
        plan_json: JSON.stringify({ goals: [{ title: 'G1', subgoals: [{ title: 'S1' }] }] }),
      },
    }
    apiMocks.artifactContent.mockResolvedValue({
      content: JSON.stringify(REQ_DOC),
    })
    const run = runWithArtifacts([
      artifact({
        id: 'a-req',
        name: 'clarified_requirement.json',
        kind: 'json',
        nodeId: 'approve',
      }),
      artifact({
        id: 'a-plan',
        name: 'plan.json',
        kind: 'json',
        nodeId: 'approve',
      }),
      // Upstream leftovers — must not open optional Approve tabs.
      artifact({ id: 'a-up-res', name: 'research.json', kind: 'json', nodeId: 'research' }),
      artifact({ id: 'a-up-page', name: 'page.html', kind: 'html', nodeId: 'visual' }),
    ])
    const wrapper = mountPanel(node, nodeRun, run)
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-product-tab-research.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="structured-product-tab-page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="structured-product-tab-clarified_requirement.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-product-tab-plan.json"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('approve optional tabs appear when this node owns the artifacts', async () => {
    const node: WFNode = {
      id: 'approve',
      type: 'agent', caps: CLARIFY_CAPS,
      label: 'Approve',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'approve',
      iteration: 1,
      status: 'completed',
      outputs: {
        clarified_requirement_json: JSON.stringify(REQ_DOC),
        plan_json: JSON.stringify({ goals: [{ title: 'G1', subgoals: [{ title: 'S1' }] }] }),
        research_json: JSON.stringify({ summary: '调研', findings: [] }),
      },
    }
    apiMocks.artifactContent.mockResolvedValue({
      content: JSON.stringify(REQ_DOC),
    })
    const run = runWithArtifacts([
      artifact({
        id: 'a-req',
        name: 'clarified_requirement.json',
        kind: 'json',
        nodeId: 'approve',
      }),
      artifact({
        id: 'a-plan',
        name: 'plan.json',
        kind: 'json',
        nodeId: 'approve',
      }),
      artifact({
        id: 'a-res',
        name: 'research.json',
        kind: 'json',
        nodeId: 'approve',
      }),
    ])
    const wrapper = mountPanel(node, nodeRun, run)
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-product-tab-research.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-product-tab-clarified_requirement.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-product-tab-plan.json"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('approve plan tab still loads after implement rewrites plan.json nodeId', async () => {
    const node: WFNode = {
      id: 'approve',
      type: 'agent', caps: CLARIFY_CAPS,
      label: 'Approve',
      position: { x: 0, y: 0 },
      config: {},
    }
    const nodeRun: NodeRun = {
      nodeId: 'approve',
      iteration: 1,
      status: 'completed',
      outputs: {},
    }
    const planDoc = { goals: [{ title: 'G1', subgoals: [{ title: 'S1' }] }] }
    apiMocks.artifactContent.mockResolvedValue({ content: JSON.stringify(planDoc) })
    const run = runWithArtifacts([
      artifact({
        id: 'a-plan',
        name: 'plan.json',
        kind: 'json',
        nodeId: 'implement',
      }),
    ])
    const wrapper = mountPanel(node, nodeRun, run)
    await flushPromises()
    await wrapper.get('[data-testid="structured-product-tab-plan.json"]').trigger('click')
    await flushPromises()
    expect(apiMocks.artifactContent).toHaveBeenCalledWith('a-plan')
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-product-name"]').text()).toBe('plan.json')
    wrapper.unmount()
  })
})

// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { OutputCard, Run } from '@/lib/shared/types'
import OutputResultCards from './OutputResultCards.vue'

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
  template: '<div data-testid="structured-view">{{ name }}</div>',
})

const HtmlPreviewStub = defineComponent({
  name: 'HtmlPreview',
  props: {
    html: String,
    enlargeable: { type: Boolean, default: true },
    fitContent: { type: Boolean, default: false },
  },
  template:
    '<div data-testid="html-preview" :data-enlargeable="enlargeable === false ? \'0\' : \'1\'" :data-fit-content="fitContent ? \'1\' : \'0\'" />',
})

const AppModalStub = defineComponent({
  name: 'AppModal',
  props: {
    open: Boolean,
    title: String,
    width: Number,
    closeOnEsc: Boolean,
  },
  emits: ['close'],
  template: `
    <div
      v-if="open"
      data-testid="output-result-enlarge-modal"
      role="dialog"
      :data-width="String(width || '')"
      :data-close-on-esc="closeOnEsc ? '1' : '0'"
    >
      <div data-testid="output-result-enlarge-title">{{ title }}</div>
      <button type="button" data-testid="output-result-enlarge-close" @click="$emit('close')">close</button>
      <slot />
    </div>
  `,
})

const cards: OutputCard[] = [
  {
    index: 1,
    template: 'research',
    title: '调研结果',
    status: 'ok',
    typeTag: '结构化产物',
    structuredArtifactName: 'research.json',
    jsonSnapshot: JSON.stringify({ summary: 'ok', findings: [] }),
  },
  {
    index: 2,
    template: 'custom',
    title: '自定义产物',
    status: 'ok',
    typeTag: '自定义产物',
    artifactName: 'page.html',
  },
  {
    index: 3,
    template: 'plan',
    title: '计划',
    status: 'ok',
    typeTag: 'Markdown',
    markdown: '## 计划正文',
  },
]

const run = {
  id: 'run-1',
  artifacts: [
    {
      id: 'a-page',
      name: 'page.html',
      kind: 'html',
      nodeId: 'output',
      runId: 'run-1',
      workflowName: 'wf',
      sizeBytes: 5,
      createdAt: '2026-07-18T00:00:00Z',
    },
    {
      id: 'a-notes',
      name: 'notes.txt',
      kind: 'text',
      nodeId: 'output',
      runId: 'run-1',
      workflowName: 'wf',
      sizeBytes: 4,
      createdAt: '2026-07-18T00:00:00Z',
    },
  ],
} as Run

function mountCards(list: OutputCard[] = cards) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(OutputResultCards, {
    props: { cards: list, run },
    global: {
      plugins: [i18n],
      stubs: {
        StructuredArtifactView: StructuredStub,
        HtmlPreview: HtmlPreviewStub,
        AppModal: AppModalStub,
        Icon: true,
      },
    },
  })
}

describe('OutputResultCards master-detail list + enlarge (g4.1)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactContent.mockResolvedValue({ content: '<html>ok</html>' })
  })

  it('multi-card: shows name list, always has current item, default first (g1.1/g1.2)', async () => {
    const wrapper = mountCards()
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-list"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="output-result-list-header"]').text()).toMatch(/产出/)
    expect(wrapper.get('[data-testid="output-result-list-header"]').text()).toContain('3')
    const toggles = wrapper.findAll('[data-testid^="output-result-card-toggle-"]')
    expect(toggles).toHaveLength(3)
    expect(toggles[0].attributes('aria-selected')).toBe('true')
    expect(toggles[1].attributes('aria-selected')).toBe('false')
    expect(wrapper.find('[data-testid="output-result-card-body-0"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="output-result-card-body-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(apiMocks.artifactContent).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="output-result-list-kind-0"]').text()).toBe('结构化')
    expect(wrapper.get('[data-testid="output-result-list-kind-1"]').text()).toBe('HTML')
    expect(wrapper.get('[data-testid="output-result-list-kind-2"]').text()).toBe('Markdown')
    wrapper.unmount()
  })

  it('clicking another row switches exclusive detail and lazy-loads HTML only for current (g1.4/g1.6)', async () => {
    const wrapper = mountCards()
    await flushPromises()
    await wrapper.get('[data-testid="output-result-card-toggle-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="output-result-card-toggle-0"]').attributes('aria-selected')).toBe(
      'false',
    )
    expect(wrapper.get('[data-testid="output-result-card-toggle-1"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.find('[data-testid="output-result-card-body-0"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="output-result-card-body-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="html-preview"]').attributes('data-enlargeable')).toBe('0')
    expect(wrapper.get('[data-testid="html-preview"]').attributes('data-fit-content')).toBe('1')
    expect(apiMocks.artifactContent).toHaveBeenCalledWith('a-page')
    expect(wrapper.find('[data-testid="html-preview-enlarge"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('clicking the current row does not collapse (g1.1)', async () => {
    const wrapper = mountCards()
    await flushPromises()
    await wrapper.get('[data-testid="output-result-card-toggle-0"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="output-result-card-toggle-0"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.find('[data-testid="output-result-card-body-0"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('single card hides list and still shows detail (g1.3)', async () => {
    const wrapper = mountCards([cards[0]])
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-list"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid^="output-result-card-toggle-"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="output-result-detail-bar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="output-result-card-body-0"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('failed row is visible without enlarge; switching back restores enlarge (g2.1/g1.5)', async () => {
    const wrapper = mountCards([
      {
        index: 1,
        template: 'research',
        title: '失败卡片',
        status: 'failed',
        typeTag: '来源失败',
        errorReason: '上游节点失败',
      },
      cards[2],
    ])
    await flushPromises()
    expect(wrapper.text()).toContain('失败卡片')
    expect(wrapper.get('[data-testid="output-result-list-kind-0"]').text()).toBe('失败')
    expect(wrapper.text()).toContain('上游节点失败')
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(false)
    await wrapper.get('[data-testid="output-result-card-toggle-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('计划正文')
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(true)
    expect(wrapper.text()).not.toMatch(/打开原始文件|下载/)
    wrapper.unmount()
  })

  it('success item opens/closes enlarge modal (width=960, Esc) without changing selection (g2.2/g2.3)', async () => {
    const wrapper = mountCards()
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-enlarge-modal"]').exists()).toBe(false)
    await wrapper.get('[data-testid="output-result-enlarge"]').trigger('click')
    await flushPromises()
    const modal = wrapper.get('[data-testid="output-result-enlarge-modal"]')
    expect(modal.attributes('data-width')).toBe('960')
    expect(modal.attributes('data-close-on-esc')).toBe('1')
    expect(wrapper.get('[data-testid="output-result-enlarge-title"]').text()).toBe('调研结果')
    expect(wrapper.get('[data-testid="output-result-enlarge-body"]').text()).toContain('research.json')
    expect(modal.text()).not.toMatch(/打开原始文件|下载/)
    await wrapper.get('[data-testid="output-result-enlarge-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-enlarge-modal"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="output-result-card-toggle-0"]').attributes('aria-selected')).toBe(
      'true',
    )
    wrapper.unmount()
  })

  it('custom non-HTML success item can enlarge (g2.1)', async () => {
    apiMocks.artifactContent.mockResolvedValue({ content: 'plain notes' })
    const wrapper = mountCards([
      {
        index: 1,
        template: 'custom',
        title: '备注',
        status: 'ok',
        typeTag: '自定义产物',
        artifactName: 'notes.txt',
      },
    ])
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-list"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('plain notes')
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(true)
    await wrapper.get('[data-testid="output-result-enlarge"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="output-result-enlarge-body"]').text()).toContain('plain notes')
    wrapper.unmount()
  })

  it('does not render open/download actions on result cards (g2.3)', async () => {
    const wrapper = mountCards([cards[0]])
    await flushPromises()
    expect(wrapper.text()).not.toMatch(/打开原始文件|下载/)
    expect(wrapper.text()).not.toContain('窗口放大查看')
    wrapper.unmount()
  })

  it('list has no independent overflow / max-height (g3.2)', async () => {
    const wrapper = mountCards()
    await flushPromises()
    const list = wrapper.get('[data-testid="output-result-list"]')
    expect(list.classes()).not.toContain('overflow-y-auto')
    expect(list.classes()).not.toContain('max-h-48')
    expect(list.classes().some((c) => c.startsWith('max-h-'))).toBe(false)
    wrapper.unmount()
  })

  it('success card without renderable body has no enlarge (g2.1/F3)', async () => {
    const wrapper = mountCards([
      {
        index: 1,
        template: 'research',
        title: '空结构化',
        status: 'ok',
        typeTag: '结构化产物',
        structuredArtifactName: 'research.json',
      },
    ])
    await flushPromises()
    expect(wrapper.text()).toContain('无效或无法解析的来源引用')
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('HTML enlarge modal uses 70vh viewport and keeps enlargeable=false (g1.6/g2.3/F4)', async () => {
    const wrapper = mountCards([cards[1]])
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-list"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(true)
    const detailPreview = wrapper.get('[data-testid="html-preview"]')
    expect(detailPreview.attributes('data-enlargeable')).toBe('0')
    expect(detailPreview.attributes('data-fit-content')).toBe('1')
    await wrapper.get('[data-testid="output-result-enlarge"]').trigger('click')
    await flushPromises()
    const viewport = wrapper.get('[data-testid="output-result-enlarge-html-viewport"]')
    expect(viewport.classes()).toContain('h-[70vh]')
    const modalPreview = viewport.get('[data-testid="html-preview"]')
    expect(modalPreview.attributes('data-enlargeable')).toBe('0')
    expect(modalPreview.attributes('data-fit-content')).toBe('0')
    expect(wrapper.get('[data-testid="output-result-enlarge-body"]').text()).not.toMatch(
      /打开原始文件|下载|窗口放大查看/,
    )
    wrapper.unmount()
  })

  it('nodes.*.outputs.page card: HtmlPreview + 自定义产物·HTML kind (g2.1/g2.2)', async () => {
    const pageCard: OutputCard = {
      index: 1,
      template: '{{nodes.visual.outputs.page}}',
      title: '网页预览 · 视觉网页',
      status: 'ok',
      typeTag: '自定义产物',
      artifactName: 'page.html',
      nodeId: 'visual',
      outputKey: 'page',
      markdown: '<!doctype html><html><body><h1>视觉网页</h1></body></html>',
    }
    const wrapper = mountCards([pageCard])
    await flushPromises()
    expect(wrapper.text()).not.toContain('结构化产物')
    expect(wrapper.get('[data-testid="output-result-detail-kind"]').text()).toBe('自定义产物 · HTML')
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('<!doctype html>')
    await wrapper.get('[data-testid="output-result-enlarge"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="output-result-enlarge-title"]').text()).toBe(
      '网页预览 · 视觉网页',
    )
    expect(wrapper.find('[data-testid="output-result-enlarge-html-viewport"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('无法预览')
    wrapper.unmount()
  })

  it('custom HTML load-fail shows 预览不可用 / 无法加载视觉网页, never Markdown source (g3.1/g3.2)', async () => {
    apiMocks.artifactContent.mockRejectedValue(new Error('fetch failed'))
    const emptyPage: OutputCard = {
      index: 1,
      template: '{{nodes.visual.outputs.page}}',
      title: '网页预览 · 视觉网页',
      status: 'ok',
      typeTag: '自定义产物',
      artifactName: 'page.html',
      nodeId: 'visual',
      outputKey: 'page',
    }
    const wrapper = mountCards([emptyPage])
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-html-load-error"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="output-result-html-load-error"]').text()).toContain('预览不可用')
    expect(wrapper.get('[data-testid="output-result-html-load-error"]').text()).toContain(
      '无法加载视觉网页',
    )
    expect(wrapper.find('[data-testid="output-result-html-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(false)
    expect(wrapper.find('.md').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/<!doctype|<html|<body/)
    wrapper.unmount()
  })

  it('custom HTML empty body (no fetch error) shows 空正文, never Markdown source (g3.1/g3.2)', async () => {
    const emptyPage: OutputCard = {
      index: 1,
      template: '{{nodes.visual.outputs.page}}',
      title: '网页预览 · 视觉网页',
      status: 'ok',
      typeTag: '自定义产物',
      artifactName: 'gone.html',
      nodeId: 'visual',
      outputKey: 'page',
    }
    const wrapper = mountCards([emptyPage])
    await flushPromises()
    expect(wrapper.find('[data-testid="output-result-html-empty"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="output-result-html-empty"]').text()).toContain('空正文')
    expect(wrapper.get('[data-testid="output-result-html-empty"]').text()).toContain(
      '没有可预览的页面内容',
    )
    expect(wrapper.find('[data-testid="output-result-html-load-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="output-result-enlarge"]').exists()).toBe(false)
    expect(wrapper.find('.md').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/<!doctype|<html|<div class=/)
    wrapper.unmount()
  })

  it('failed card uses server failTitle instead of hardcoded 来源节点失败 / 无产物 (g3.1)', async () => {
    const wrapper = mountCards([
      {
        index: 1,
        template: '{{nodes.visual.outputs.page}}',
        title: '网页预览 · 视觉网页',
        status: 'failed',
        typeTag: '来源失败',
        failTitle: '缺少可展示产出',
        errorReason: '来源已执行完成但没有可供展示的产出',
      },
    ])
    await flushPromises()
    expect(wrapper.get('[data-testid="output-result-fail-title"]').text()).toBe('缺少可展示产出')
    expect(wrapper.text()).toContain('来源已执行完成但没有可供展示的产出')
    expect(wrapper.text()).not.toContain('来源节点失败 / 无产物')
    expect(wrapper.text()).not.toContain('上游节点无输出')
    wrapper.unmount()
  })

  it('loads HTML by artifactName + nodeId; two visual cards stay independent (g3.2)', async () => {
    apiMocks.artifactContent.mockImplementation(async (id: string) => {
      if (id === 'a-va') return { content: '<html>from-art-a</html>' }
      if (id === 'a-vb') return { content: '<html>from-art-b</html>' }
      return { content: '<html>WRONG-GLOBAL</html>' }
    })
    const dualRun = {
      ...run,
      artifacts: [
        {
          id: 'a-global',
          name: 'page.html',
          kind: 'html',
          nodeId: 'visual_b',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 8,
          createdAt: '2026-07-18T00:00:00Z',
        },
        {
          id: 'a-va',
          name: 'visual_a.page.html',
          kind: 'html',
          nodeId: 'visual_a',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 8,
          createdAt: '2026-07-18T00:00:00Z',
        },
        {
          id: 'a-vb',
          name: 'visual_b.page.html',
          kind: 'html',
          nodeId: 'visual_b',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 8,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    } as Run
    const dual: OutputCard[] = [
      {
        index: 1,
        template: '{{nodes.visual_a.outputs.page}}',
        title: '网页预览 · 视觉A',
        status: 'ok',
        typeTag: '自定义产物',
        artifactName: 'visual_a.page.html',
        nodeId: 'visual_a',
        outputKey: 'page',
        markdown: '<!doctype html><html><body><h1>snap-a</h1></body></html>',
      },
      {
        index: 2,
        template: '{{nodes.visual_b.outputs.page}}',
        title: '网页预览 · 视觉B',
        status: 'ok',
        typeTag: '自定义产物',
        artifactName: 'visual_b.page.html',
        nodeId: 'visual_b',
        outputKey: 'page',
        markdown: '<!doctype html><html><body><h1>snap-b</h1></body></html>',
      },
    ]
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(OutputResultCards, {
      props: { cards: dual, run: dualRun },
      global: {
        plugins: [i18n],
        stubs: {
          StructuredArtifactView: StructuredStub,
          HtmlPreview: HtmlPreviewStub,
          AppModal: AppModalStub,
          Icon: true,
        },
      },
    })
    await flushPromises()
    const first = wrapper.getComponent({ name: 'HtmlPreview' })
    expect(first.props('html')).toContain('snap-a')
    expect(first.props('html')).not.toContain('snap-b')
    expect(first.props('html')).not.toContain('WRONG-GLOBAL')
    await wrapper.get('[data-testid="output-result-card-toggle-1"]').trigger('click')
    await flushPromises()
    const second = wrapper.getComponent({ name: 'HtmlPreview' })
    expect(second.props('html')).toContain('snap-b')
    expect(second.props('html')).not.toContain('snap-a')
    expect(apiMocks.artifactContent).toHaveBeenCalledWith('a-va')
    expect(apiMocks.artifactContent).toHaveBeenCalledWith('a-vb')
    expect(apiMocks.artifactContent).not.toHaveBeenCalledWith('a-global')
    wrapper.unmount()
  })

  it('custom HTML uses markdown fallback when artifact missing (no source leak)', async () => {
    const pageOnlyMd: OutputCard = {
      index: 1,
      template: '{{nodes.visual.outputs.page}}',
      title: '网页预览 · 视觉网页',
      status: 'ok',
      typeTag: '自定义产物',
      artifactName: 'page.html',
      markdown: '<!doctype html><html><body><p>from-markdown</p></body></html>',
    }
    const wrapper = mountCards([pageOnlyMd])
    await flushPromises()
    // No matching artifact in run → cache empty, markdown feeds HtmlPreview
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="output-result-html-unavailable"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('<!doctype html>')
    wrapper.unmount()
  })

  const fullHtmlMarkdown = `<!doctype html>
<html>
<body>
<div class="scenes" id="scenes">
  <button class="scene-btn on" type="button">1. Inbox 待澄清</button>
</div>
</body>
</html>`

  it('.htm artifactName is treated as visual HTML (g4.1)', async () => {
    const htmCard: OutputCard = {
      index: 1,
      template: 'artifact("report.htm")',
      title: 'HTM',
      status: 'ok',
      typeTag: '自定义产物',
      artifactName: 'report.htm',
      markdown: '<!doctype html><html><body><p>htm</p></body></html>',
    }
    const wrapper = mountCards([htmCard])
    await flushPromises()
    expect(wrapper.get('[data-testid="output-result-detail-kind"]').text()).toBe('自定义产物 · HTML')
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('<!doctype html>')
    wrapper.unmount()
  })

  it('outputKey=page without html filename still uses HtmlPreview (g4.1)', async () => {
    const pageKey: OutputCard = {
      index: 1,
      template: '{{nodes.visual.outputs.page}}',
      title: '网页预览 · 视觉网页',
      status: 'ok',
      typeTag: '自定义产物',
      artifactName: 'visual-page',
      outputKey: 'page',
      markdown: fullHtmlMarkdown,
    }
    const wrapper = mountCards([pageKey])
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="output-result-detail-kind"]').text()).toBe('自定义产物 · HTML')
    expect(wrapper.text()).not.toContain('scene-btn')
    wrapper.unmount()
  })

  it('body sniff: full HTML markdown without page key / html name uses HtmlPreview (g4.1)', async () => {
    const sniffed: OutputCard = {
      index: 1,
      template: 'custom',
      title: '完整文档',
      status: 'ok',
      typeTag: '自定义产物',
      artifactName: 'unnamed',
      markdown: fullHtmlMarkdown,
    }
    const wrapper = mountCards([sniffed])
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.find('.md').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('<div class="scenes"')
    wrapper.unmount()
  })

  it('real structured JSON is not misclassified as visual HTML (g4.1)', async () => {
    const wrapper = mountCards([cards[0]])
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="output-result-detail-kind"]').text()).toBe('结构化产物')
    wrapper.unmount()
  })

  it('div-only fragment without page/.html name stays off HtmlPreview (g4.1)', async () => {
    const frag: OutputCard = {
      index: 1,
      template: 'md',
      title: '片段',
      status: 'ok',
      typeTag: 'Markdown',
      markdown: '<div class="scenes">not a document</div>',
    }
    const wrapper = mountCards([frag])
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(wrapper.find('.md').exists()).toBe(true)
    wrapper.unmount()
  })
})

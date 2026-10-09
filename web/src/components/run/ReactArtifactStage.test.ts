// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact } from '@/lib/shared/types'
import { resetStageOpenStateForTests } from '@/lib/run/reactArtifactPreview'
import ReactArtifactStage from './ReactArtifactStage.vue'
import { api } from '@/lib/api/api'
import { stageLinksFor } from '@/lib/run/stageLinks'
import type { AgentCapabilities } from '@/lib/api/apiTypes'
import { ASK_CAPS, CLARIFY_CAPS, IMPLEMENT_CAPS, PREVIEW_REVIEW_CAPS, writesCaps } from '@/test/capsFixtures'

function gn(id: string, label: string, caps: AgentCapabilities) {
  return { id, type: 'agent' as const, label, position: { x: 0, y: 0 }, config: {}, caps }
}

const CLARIFY_NODE = { type: 'agent' as const, caps: CLARIFY_CAPS }
const ASK_NODE = { type: 'agent' as const, caps: ASK_CAPS }
const PAGE_NODE = { type: 'agent' as const, caps: writesCaps('page') }
const RESEARCH_NODE = { type: 'agent' as const, caps: writesCaps('research') }
const IMPLEMENT_NODE = { type: 'agent' as const, caps: IMPLEMENT_CAPS }
const PREVIEW_NODE = { type: 'agent' as const, caps: PREVIEW_REVIEW_CAPS }

const { mockAddClarifyAnnotation } = vi.hoisted(() => ({
  mockAddClarifyAnnotation: vi.fn(() => 'added'),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    artifactContent: vi.fn(async () => ({
      id: 'thumb',
      name: 'thumb.html',
      kind: 'html',
      nodeId: 'react',
      runId: 'run-1',
      workflowName: 'wf',
      sizeBytes: 18,
      createdAt: '2026-08-01T00:00:00Z',
      content: '<html>thumb</html>',
    })),
    getRunNodeSandbox: vi.fn(async () => ({ id: 42 })),
    nodePreviews: vi.fn(async () => ({ ports: [] })),
    nodeSandboxLog: vi.fn(async () => ({ content: 'stdout line', live: true, found: true })),
    sandboxIdeUrl: (id: number) => `about:blank#ide-${id}`,
    sandboxTerminalWsUrl: (id: number) => `ws://test.local/sandboxes/${id}/terminal`,
    artifactVersions: vi.fn(async () => []),
    artifactVersionContent: vi.fn(async () => ({
      artifactId: 'live',
      revision: 1,
      nodeId: 'visual_1',
      sizeBytes: 8,
      createdAt: '2026-08-01T00:00:00Z',
      content: '<p>old</p>',
    })),
  },
}))

vi.mock('@/lib/inbox/useClarifyDraft', () => ({
  addClarifyAnnotation: mockAddClarifyAnnotation,
}))

vi.mock('@xterm/xterm', () => {
  class Terminal {
    cols = 80
    rows = 24
    loadAddon() {}
    open() {}
    write() {}
    dispose() {}
    onData() {
      return { dispose() {} }
    }
  }
  return { Terminal }
})

vi.mock('@xterm/addon-fit', () => {
  class FitAddon {
    fit() {}
  }
  return { FitAddon }
})

vi.mock('@xterm/xterm/css/xterm.css', () => ({}))

function i18n() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
}

function art(partial: Partial<Artifact> & Pick<Artifact, 'id' | 'name'>): Artifact {
  return {
    kind: 'json',
    nodeId: 'react',
    runId: 'run-1',
    workflowName: 'wf',
    sizeBytes: 12,
    createdAt: '2026-08-01T00:00:00Z',
    revision: 1,
    ...partial,
  }
}

const stubs = {
  Icon: true,
  HtmlPreview: true,
  NovncPreviewPanel: defineComponent({
    props: { sandboxId: Number, inspectable: Boolean },
    template: '<div data-testid="novnc-stub" :data-inspectable="inspectable ? \'1\' : \'0\'" />',
  }),
  ArtifactPreview: defineComponent({
    props: { artifact: Object, annotatable: Boolean },
    template: '<div data-testid="artifact-preview">{{ artifact?.name }}|{{ annotatable ? \'on\' : \'off\' }}{{ artifact?.content ? \'|\' + artifact.content : \'\' }}</div>',
  }),
  AppPreviewPanel: defineComponent({
    props: { runId: String, nodeId: String },
    emits: ['pick', 'stagedPick'],
    template:
      '<div data-testid="app-preview-stub">' +
      '<button type="button" data-testid="app-preview-pick" @click="$emit(\'pick\', { selector: \'#hero\', tagName: \'DIV\', outerHTML: \'<div id=hero></div>\', url: \'http://app/\' })">pick</button>' +
      '</div>',
  }),
  PublicAppPreviewPanel: defineComponent({
    template: '<div data-testid="public-app-preview-stub" />',
  }),
}

describe('ReactArtifactStage', () => {
  beforeEach(() => {
    // Stage specs use runId values starting with "run-"; keep unrelated helper keys intact for parallel files.
    resetStageOpenStateForTests(':run-')
    vi.mocked(api.artifactContent).mockImplementation(async () =>
      art({ id: 'thumb', name: 'thumb.html', kind: 'html', content: '<html>thumb</html>' }),
    )
    vi.mocked(api.artifactVersions).mockResolvedValue([])
    vi.mocked(api.artifactVersionContent).mockResolvedValue({
      artifactId: 'live',
      revision: 1,
      nodeId: 'visual_1',
      sizeBytes: 8,
      createdAt: '2026-08-01T00:00:00Z',
      content: '<p>old</p>',
    })
    vi.mocked(api.nodePreviews).mockResolvedValue({ ports: [] })
    vi.mocked(api.getRunNodeSandbox).mockResolvedValue({ id: 42 } as never)
    vi.mocked(api.nodeSandboxLog).mockResolvedValue({ content: 'stdout line', live: true, found: true })
  })
  afterEach(() => {
    resetStageOpenStateForTests(':run-')
    document.body.innerHTML = ''
  })

  function mockTwoVersions(v1Content = '<p>old</p>') {
    vi.mocked(api.artifactVersions).mockResolvedValue([
      {
        artifactId: 'live',
        revision: 1,
        nodeId: 'visual_1',
        sizeBytes: v1Content.length,
        createdAt: '2026-08-01T00:00:00Z',
      },
    ])
    vi.mocked(api.artifactVersionContent).mockResolvedValue({
      artifactId: 'live',
      revision: 1,
      nodeId: 'visual_1',
      sizeBytes: v1Content.length,
      createdAt: '2026-08-01T00:00:00Z',
      content: v1Content,
    })
  }

  it('defaults to workflow artifacts grid, then opens a named preview tab on card click (g2.1)', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json' })],
        runId: 'run-1',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="react-artifact-tab-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-preview-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-grid"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview"]').exists()).toBe(false)
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-grid"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="artifact-preview"]').text()).toBe('research.json|off')
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('false')
    wrapper.unmount()
  })

  it('lets the chat open its artifacts and preview through stage links', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json' })],
        runId: 'run-links',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    const links = stageLinksFor('run-links')!
    expect(links.hasArtifact('research.json')).toBe(true)
    expect(links.hasArtifact('missing.json')).toBe(false)
    expect(links.canOpenPreview()).toBe(false)
    links.openPreview()
    links.openArtifact('research.json')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
    expect(stageLinksFor('run-links')).toBeNull()
  })

  it('keeps workflow artifact cards compact instead of stretching the row (g1.1 / g1.2 / g2.2)', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json' })],
        runId: 'run-compact',
        nodeId: 'clarify',
        annotatable: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()

    const scroller = wrapper.get('[data-testid="react-artifact-grid"]')
    expect(scroller.classes()).toEqual(expect.arrayContaining(['overflow-y-auto', 'p-4']))
    expect(scroller.classes()).not.toContain('overflow-hidden')

    const layout = scroller.get('.grid')
    const layoutClass = layout.attributes('class') || ''
    expect(layoutClass).not.toContain('auto-rows-fr')
    expect(layoutClass).not.toContain('h-full')
    expect(layoutClass).toContain('gap-3')
    expect(layoutClass).toContain('minmax(176px,1fr)')

    for (const id of ['react-artifact-card-browser', 'react-artifact-card-research.json']) {
      const card = wrapper.get(`[data-testid="${id}"]`)
      const cardClass = card.attributes('class') || ''
      expect(cardClass).toContain('rounded-lg')
      expect(cardClass).toContain('border')
      expect(cardClass).toContain('border-line')
      expect(cardClass).toContain('hover:border-line-strong')
      expect(cardClass).not.toContain('h-full')
      const thumb = card.element.firstElementChild as HTMLElement
      expect(thumb.className).toContain('h-[110px]')
      expect(thumb.className).not.toContain('flex-1')
    }

    wrapper.unmount()
  })

  it('adds another preview tab instead of replacing the open one', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [
          art({ id: 'a1', name: 'research.json', kind: 'json' }),
          art({ id: 'a2', name: 'plan.json', kind: 'json' }),
        ],
        runId: 'run-1',
      },
      global: { plugins: [i18n()], stubs },
    })
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-research.json"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-plan.json"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('false')
    const previews = wrapper.findAll('[data-testid="artifact-preview"]')
    expect(previews.map((n) => n.text())).toEqual(['research.json|off', 'plan.json|off'])
    wrapper.unmount()
  })

  it('enables annotate on every open preview tab', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [
          art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'clarify' }),
          art({ id: 'a2', name: 'plan.json', kind: 'json', nodeId: 'clarify' }),
        ],
        runId: 'run-1',
        nodeId: 'clarify',
        annotatable: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    await flushPromises()
    const previews = wrapper.findAll('[data-testid="artifact-preview"]')
    expect(previews.map((n) => n.text())).toEqual(['research.json|on', 'plan.json|on'])
    wrapper.unmount()
  })

  it('pins a preview tab when previewArtifact is set without dropping other open tabs', async () => {
    const first = art({ id: 'a1', name: 'page.html', kind: 'html', revision: 1, updatedAt: 't1' })
    const note = art({ id: 'a2', name: 'research.json', kind: 'json' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [first, note],
        previewArtifact: 'page.html',
        runId: 'run-1',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-research.json"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    await wrapper.setProps({
      artifacts: [{ ...first, revision: 2, updatedAt: 't2', sizeBytes: 99 }, note],
      previewArtifact: 'page.html',
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-page.html"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="react-artifact-preview-page.html"]').text()).toContain('page.html')
    wrapper.unmount()
  })

  it('keeps the tab bar when a single visual page is pinned', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })],
        previewArtifact: 'page.html',
        runId: 'run-1',
        nodeId: 'visual_bqc5',
        annotatable: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tabs"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').text()).toContain('工作流产物')
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('opens a pinned tab when the artifact arrives after previewArtifact while still on the grid', async () => {
    const note = art({ id: 'a2', name: 'research.json', kind: 'json' })
    const page = art({ id: 'a1', name: 'page.html', kind: 'html', revision: 1 })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [note],
        previewArtifact: 'page.html',
        runId: 'run-1',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="react-artifact-tab-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-tab-page.html"]').exists()).toBe(false)
    await wrapper.setProps({ artifacts: [note, page], previewArtifact: 'page.html' })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('does not steal focus when a pinned artifact arrives after the user opened another tab', async () => {
    const note = art({ id: 'a2', name: 'research.json', kind: 'json' })
    const page = art({ id: 'a1', name: 'page.html', kind: 'html', revision: 1 })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [note],
        previewArtifact: 'page.html',
        runId: 'run-1',
      },
      global: { plugins: [i18n()], stubs },
    })
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    await wrapper.setProps({ artifacts: [note, page], previewArtifact: 'page.html' })
    await flushPromises()
    // Pin must still appear on the tab bar (g2.1); focus stays on the open tab.
    expect(wrapper.find('[data-testid="react-artifact-tab-page.html"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('false')
    expect(wrapper.get('[data-testid="react-artifact-tab-unread-page.html"]').attributes('data-unread')).toBe('new')
    wrapper.unmount()
  })

  it('does not steal focus when an unrelated artifact is added under the same pin', async () => {
    const page = art({ id: 'a1', name: 'page.html', kind: 'html' })
    const note = art({ id: 'a2', name: 'research.json', kind: 'json' })
    const extra = art({ id: 'a3', name: 'plan.json', kind: 'json' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [page, note],
        previewArtifact: 'page.html',
        runId: 'run-1',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await wrapper.setProps({ artifacts: [page, note, extra], previewArtifact: 'page.html' })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('closes a preview tab and keeps the remaining one', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [
          art({ id: 'a1', name: 'research.json', kind: 'json' }),
          art({ id: 'a2', name: 'plan.json', kind: 'json' }),
        ],
        runId: 'run-1',
      },
      global: { plugins: [i18n()], stubs },
    })
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-tab-close-plan.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('opens a browser tab from the workflow card without replacing artifact tabs', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json' })],
        runId: 'run-1',
        nodeId: 'clarify',
        annotatable: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-card-browser"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('远程桌面')
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-browser"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-research.json"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-browser"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="novnc-stub"]').attributes('data-inspectable')).toBe('1')
    await wrapper.get('[data-testid="react-artifact-tab-close-browser"]').trigger('click')
    expect(wrapper.find('[data-testid="react-artifact-tab-browser"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('keeps four fixed cards on an empty grid and opens the container log once', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-fixed-empty',
        nodeId: 'clarify',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-grid-empty"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('本次运行还没有产物')
    expect(wrapper.text()).not.toContain('远程桌面')
    expect(wrapper.find('[data-testid="react-artifact-card-app"]').exists()).toBe(false)
    const titles = ['ide', 'terminal', 'browser', 'log'].map((id) =>
      wrapper.get(`[data-testid="react-artifact-card-${id}"]`).text(),
    )
    expect(titles[0]).toContain('IDE')
    expect(titles[0]).toContain('沙箱 IDE')
    expect(titles[1]).toContain('Terminal')
    expect(titles[1]).toContain('沙箱终端')
    expect(titles[2]).toContain('浏览器')
    expect(titles[2]).toContain('沙箱浏览器 · 可取点标注')
    expect(titles[3]).toContain('日志')
    expect(titles[3]).toContain('容器日志 · stdout/stderr')

    await wrapper.get('[data-testid="react-artifact-card-log"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-log"]').attributes('aria-selected')).toBe('true')
    const logPane = wrapper.get('[data-testid="react-artifact-preview-log"]')
    expect(logPane.text()).toContain('沙箱容器日志')
    expect(logPane.text()).toContain('stdout line')
    expect(logPane.text()).not.toContain('执行日志')

    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-log"]').trigger('click')
    expect(wrapper.findAll('[data-testid="react-artifact-tab-log"]').length).toBe(1)
    await wrapper.get('[data-testid="react-artifact-tab-close-log"]').trigger('click')
    expect(wrapper.find('[data-testid="react-artifact-tab-log"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('draws distinct glyphs on the four fixed cards and reuses them on tabs', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-fixed-glyphs',
        nodeId: 'clarify',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs: { ...stubs, Icon: false } },
    })
    await flushPromises()
    const ids = ['ide', 'terminal', 'browser', 'log'] as const
    const glyphOf = (id: string) =>
      wrapper.get(`[data-testid="react-artifact-card-${id}"]`).find('svg').element.innerHTML
    const glyphs = ids.map((id) => glyphOf(id))
    expect(new Set(glyphs).size).toBe(4)

    const ide = wrapper.get('[data-testid="react-artifact-card-ide"]').find('svg')
    expect(ide.html()).toContain('M8.2 8v12.4')
    expect(ide.html()).not.toContain('M14 3v5h5')
    expect(ide.attributes('width')).toBe('30')
    expect(ide.classes()).toContain('text-txt2')
    expect(ide.classes()).not.toContain('opacity-50')

    const log = wrapper.get('[data-testid="react-artifact-card-log"]').find('svg')
    expect(log.findAll('circle')).toHaveLength(3)

    for (const id of ids) {
      await wrapper.get(`[data-testid="react-artifact-card-${id}"]`).trigger('click')
      await flushPromises()
      const tab = wrapper.get(`[data-testid="react-artifact-tab-${id}"]`).find('svg')
      expect(tab.element.innerHTML).toBe(glyphOf(id))
      expect(tab.attributes('width')).toBe('13')
    }
    wrapper.unmount()
  })

  it('keeps the app preview card on the shared globe', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-fixed-glyphs-app',
        nodeId: 'preview',
        remoteKind: 'app',
      },
      global: { plugins: [i18n()], stubs: { ...stubs, Icon: false } },
    })
    await flushPromises()
    const app = wrapper.get('[data-testid="react-artifact-card-app"]').find('svg')
    expect(app.html()).toContain('r="10"')
    expect(app.attributes('width')).toBe('28')
    expect(app.classes()).toContain('opacity-50')
    const browser = wrapper.get('[data-testid="react-artifact-card-browser"]').find('svg')
    expect(browser.html()).toContain('r="8"')
    expect(browser.html()).not.toContain('r="10"')
    expect(wrapper.get('[data-testid="react-artifact-tab-novnc"]').find('svg').html()).toContain('r="10"')
    wrapper.unmount()
  })

  it('uses a no-sandbox message until a sandbox exists, and code-server copy only when the image lacks it', async () => {
    vi.mocked(api.getRunNodeSandbox).mockResolvedValue(null)
    const missing = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-fixed-no-sandbox',
        nodeId: 'clarify',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    await missing.get('[data-testid="react-artifact-card-ide"]').trigger('click')
    await flushPromises()
    const ide = missing.get('[data-testid="react-artifact-preview-ide"]')
    expect(ide.text()).toContain('当前节点没有可用沙箱，无法打开 IDE。')
    expect(ide.text()).not.toContain('code-server')
    await missing.get('[data-testid="react-artifact-card-terminal"]').trigger('click')
    await flushPromises()
    const term = missing.get('[data-testid="react-artifact-preview-terminal"]')
    expect(term.text()).toContain('当前节点没有可用沙箱，无法打开终端。')
    expect(term.text()).not.toContain('连接已断开')
    missing.unmount()

    vi.mocked(api.getRunNodeSandbox).mockResolvedValue({ id: 7, hasCodeServer: false } as never)
    const image = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-fixed-no-codeserver',
        nodeId: 'clarify',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    await image.get('[data-testid="react-artifact-card-ide"]').trigger('click')
    await flushPromises()
    expect(image.get('[data-testid="react-artifact-ide-unavailable"]').text()).toContain('code-server')
    expect(image.find('iframe[title="code-server"]').exists()).toBe(false)
    image.unmount()

    vi.mocked(api.getRunNodeSandbox).mockResolvedValue({ id: 8, hasCodeServer: true } as never)
    const ready = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-fixed-ide-ready',
        nodeId: 'clarify',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    await ready.get('[data-testid="react-artifact-card-ide"]').trigger('click')
    await flushPromises()
    expect(ready.find('iframe[title="code-server"]').exists()).toBe(true)
    expect(ready.find('[data-testid="react-artifact-ide-unavailable"]').exists()).toBe(false)
    ready.unmount()
  })

  it('inserts app preview between browser and log and keeps their panels distinct', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-fixed-app',
        nodeId: 'preview',
        remoteKind: 'app',
        annotatable: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(
      wrapper.findAll('[data-testid^="react-artifact-card-"]').map((c) => c.attributes('data-testid')),
    ).toEqual([
      'react-artifact-card-ide',
      'react-artifact-card-terminal',
      'react-artifact-card-browser',
      'react-artifact-card-app',
      'react-artifact-card-log',
    ])
    expect(wrapper.get('[data-testid="react-artifact-tab-novnc"]').text()).toContain('应用预览')
    await wrapper.get('[data-testid="react-artifact-card-browser"]').trigger('click')
    await flushPromises()
    expect(
      wrapper.get('[data-testid="react-artifact-preview-browser"]').find('[data-testid="novnc-stub"]').exists(),
    ).toBe(true)
    expect(
      wrapper.get('[data-testid="react-artifact-preview-browser"]').find('[data-testid="app-preview-stub"]').exists(),
    ).toBe(false)
    expect(
      wrapper.get('[data-testid="react-artifact-preview-novnc"]').find('[data-testid="app-preview-stub"]').exists(),
    ).toBe(true)
    expect(
      wrapper.get('[data-testid="react-artifact-preview-novnc"]').find('[data-testid="novnc-stub"]').exists(),
    ).toBe(false)
    await wrapper.get('[data-testid="react-artifact-card-browser"]').trigger('click')
    expect(wrapper.findAll('[data-testid="react-artifact-tab-browser"]').length).toBe(1)
    wrapper.unmount()
  })

  it('keeps foreign-node artifacts previewable but not annotatable', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [
          art({ id: 'own', name: 'research.json', kind: 'json', nodeId: 'research' }),
          art({ id: 'other', name: 'plan.json', kind: 'json', nodeId: 'plan' }),
        ],
        runId: 'run-1',
        nodeId: 'research',
        annotatable: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    expect(wrapper.findAll('[data-testid="react-artifact-card-readonly"]').length).toBe(1)
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    await flushPromises()
    const previews = wrapper.findAll('[data-testid="artifact-preview"]')
    expect(previews.map((n) => n.text())).toEqual(['research.json|on', 'plan.json|off'])
    wrapper.unmount()
  })

  it('opens app preview inside the remote tab without sandbox noVNC', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'note.md', kind: 'markdown' })],
        runId: 'run-1',
        nodeId: 'preview',
        annotatable: true,
        remoteKind: 'app',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="novnc-stub"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="app-preview-stub"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-novnc"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="react-artifact-tab-novnc"]').text()).toContain('应用预览')
    wrapper.unmount()
  })

  it('hides app preview tab for a clarify Agent when no ports are registered (plan g1.4 / g2.3)', async () => {
    vi.mocked(api.nodePreviews).mockResolvedValue({ ports: [] })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'approve_1' })],
        runId: 'run-approve-empty',
        nodeId: 'approve_1',
        node: CLARIFY_NODE,
        // Even if a parent still passes app, stage must stay off until registration.
        remoteKind: 'app',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(api.nodePreviews).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="app-preview-stub"]').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/尚未注册预览端口/)
    wrapper.unmount()
  })

  it('honors hideAppPreview even when clarify ports are registered (artifact embed modal)', async () => {
    vi.mocked(api.nodePreviews).mockClear()
    vi.mocked(api.nodePreviews).mockResolvedValue({
      ports: [
        {
          port: 5173,
          label: '前端',
          runId: 'run-approve-hide',
          nodeId: 'approve_1',
          proxyUrl: '/p',
          healthy: true,
        },
      ],
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'approve_1' })],
        runId: 'run-approve-hide',
        nodeId: 'approve_1',
        node: CLARIFY_NODE,
        hideAppPreview: true,
        remoteKind: 'app',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(api.nodePreviews).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="react-artifact-card-novnc"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-ide"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="app-preview-stub"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-research.json"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows app preview tab for a clarify Agent after silent probe finds ports (plan g2.2)', async () => {
    vi.mocked(api.nodePreviews).mockResolvedValue({
      ports: [
        {
          port: 5173,
          label: '前端',
          runId: 'run-approve-ports',
          nodeId: 'approve_1',
          proxyUrl: '/p',
          healthy: true,
        },
      ],
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'approve_1' })],
        runId: 'run-approve-ports',
        nodeId: 'approve_1',
        node: CLARIFY_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="app-preview-stub"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-novnc"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('upgrades a review stage of a non-clarify node to the app tab once set_preview registers', async () => {
    vi.mocked(api.nodePreviews).mockResolvedValue({
      ports: [
        { port: 5173, label: '前端', runId: 'run-impl-review', nodeId: 'impl_1', proxyUrl: '/p', healthy: true },
      ],
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'plan.json', kind: 'json', nodeId: 'impl_1' })],
        runId: 'run-impl-review',
        nodeId: 'impl_1',
        node: IMPLEMENT_NODE,
        remoteKind: 'off',
        probeRegisteredPreview: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(api.nodePreviews).toHaveBeenCalledWith('run-impl-review', 'impl_1', expect.anything())
    expect(wrapper.find('[data-testid="app-preview-stub"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps the remote kind for a non-clarify node without the review probe', async () => {
    vi.mocked(api.nodePreviews).mockClear()
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'plan.json', kind: 'json', nodeId: 'impl_1' })],
        runId: 'run-impl-plain',
        nodeId: 'impl_1',
        node: IMPLEMENT_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(api.nodePreviews).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows public app preview for a clarify Agent from share ports without probing', async () => {
    vi.mocked(api.nodePreviews).mockClear()
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'approve_1' })],
        node: CLARIFY_NODE,
        remoteKind: 'public',
        token: 't',
        ports: [{ port: 18080, kind: 'port', mode: 'vnc', directUrl: 'http://10.0.0.5:18080/' }],
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(api.nodePreviews).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="public-app-preview-stub"]').exists()).toBe(true)
    await wrapper.setProps({ ports: [] })
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not steal focus when clarify ports arrive after userMoved (plan g2.2)', async () => {
    vi.useFakeTimers()
    vi.mocked(api.nodePreviews).mockResolvedValue({ ports: [] })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'approve_1' })],
        runId: 'run-approve-moved',
        nodeId: 'approve_1',
        node: CLARIFY_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe(
      'true',
    )

    vi.mocked(api.nodePreviews).mockResolvedValue({
      ports: [
        {
          port: 5173,
          label: '前端',
          runId: 'run-approve-moved',
          nodeId: 'approve_1',
          proxyUrl: '/p',
          healthy: true,
        },
      ],
    })
    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.get('[data-testid="react-artifact-tab-novnc"]').attributes('aria-selected')).toBe('false')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('keeps the reviewed preview Agent remote tab even when the port list is still empty (plan g2.3)', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-app-preview',
        nodeId: 'preview_1',
        node: PREVIEW_NODE,
        remoteKind: 'app',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="app-preview-stub"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('emits remote pick once without staging a local annotation', async () => {
    mockAddClarifyAnnotation.mockClear()
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-1',
        nodeId: 'preview',
        annotatable: true,
        remoteKind: 'app',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    await wrapper.get('[data-testid="app-preview-pick"]').trigger('click')
    expect(wrapper.emitted('pick')).toEqual([
      [{ selector: '#hero', tagName: 'DIV', outerHTML: '<div id=hero></div>', url: 'http://app/' }],
    ])
    expect(mockAddClarifyAnnotation).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('merges visual page snapshots onto one page.html card with a footer version chip', async () => {
    mockTwoVersions('<p>old</p>')
    const live = art({
      id: 'live',
      name: 'page.html',
      kind: 'html',
      nodeId: 'visual_1',
      content: '<p>new</p>',
      revision: 2,
    })
    const alias = art({
      id: 'alias',
      name: 'visual_1.page.html',
      kind: 'html',
      nodeId: 'visual_1',
      content: '<p>new</p>',
    })
    const json = art({ id: 'json', name: 'research.json', kind: 'json', nodeId: 'research' })
    const complete = art({ id: 'nc', name: 'node_complete.json', kind: 'json', nodeId: 'visual_1' })
    const other = art({
      id: 'other',
      name: 'visual_other.page.html',
      kind: 'html',
      nodeId: 'visual_other',
      content: '<p>other</p>',
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [live, alias, json, complete, other],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [
            gn('visual_1', '视觉', writesCaps('page')),
            gn('visual_other', '另一页', writesCaps('page')),
          ],
          nodeExecutions: {
            visual_1: [
              { nodeId: 'visual_1', iteration: 1, status: 'completed', outputs: { page: '<p>old</p>' } },
              { nodeId: 'visual_1', iteration: 2, status: 'waiting_human', outputs: { page: '<p>new</p>' } },
            ],
          },
        } as any,
        nodeId: 'visual_1',
        annotatable: true,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs: { ...stubs, Teleport: false } },
      attachTo: document.body,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-card-page.html"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-visual_1.page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-page.html#iter-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-research.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-node_complete.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-visual_other.page.html"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="react-artifact-card-page.html"]').length).toBe(1)
    expect(wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').text()).toContain('v2 · 最新')
    expect(wrapper.find('[data-testid="react-artifact-card-iteration"]').exists()).toBe(false)
    await wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="react-artifact-version-option-v1"]')?.textContent).toBe('v1')
    expect(document.querySelector('[data-testid="react-artifact-version-option-v2"]')?.textContent).toContain(
      'v2 · 最新',
    )
    ;(document.querySelector('[data-testid="react-artifact-version-option-v1"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(wrapper.findAll('[data-testid="react-artifact-card-page.html"]').length).toBe(1)
    expect(wrapper.get('[data-testid="artifact-preview"]').text()).toBe('page.html|off|<p>old</p>')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-testid="react-artifact-version-option-v2"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(wrapper.get('[data-testid="artifact-preview"]').text()).toBe('page.html|on|<p>new</p>')
    wrapper.unmount()
  })

  it('hides the version chip for a single snapshot and still shows json cards', async () => {
    const live = art({ id: 'live', name: 'page.html', kind: 'html', nodeId: 'visual_1', content: '<p>only</p>' })
    const json = art({ id: 'json', name: 'research.json', kind: 'json', nodeId: 'research' })
    const complete = art({ id: 'nc', name: 'node_complete.json', kind: 'json', nodeId: 'visual_1' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [live, json, complete],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [gn('visual_1', '视觉', writesCaps('page'))],
          nodeExecutions: {
            visual_1: [{ nodeId: 'visual_1', iteration: 1, status: 'completed', outputs: { page: '<p>only</p>' } }],
          },
        } as any,
        nodeId: 'visual_1',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-version-chip"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-research.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-node_complete.json"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the version chip when archived snapshots exist for any product', async () => {
    mockTwoVersions('<p>v1</p>')
    const live = art({
      id: 'live',
      name: 'page.html',
      kind: 'html',
      nodeId: 'visual_1',
      content: '<p>v2</p>',
      revision: 2,
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [live],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [gn('visual_1', '视觉', writesCaps('page'))],
        } as any,
        nodeId: 'visual_1',
        annotatable: true,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs: { ...stubs, Teleport: false } },
      attachTo: document.body,
    })
    await flushPromises()
    expect(wrapper.findAll('[data-testid="react-artifact-card-page.html"]').length).toBe(1)
    expect(wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').text()).toContain('v2 · 最新')
    await wrapper.get('[data-testid="react-artifact-version-chip-btn-page.html"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="react-artifact-version-option-v1"]')?.textContent).toBe('v1')
    ;(document.querySelector('[data-testid="react-artifact-version-option-v1"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(wrapper.get('[data-testid="artifact-preview"]').text()).toBe('page.html|off|<p>v1</p>')
    wrapper.unmount()
  })

  it('keeps clarify page.html and agent-named HTML on the workflow grid', async () => {
    const approvePage = art({ id: 'ap', name: 'page.html', kind: 'html', nodeId: 'approve_7gl6' })
    const demo = art({ id: 'd', name: 'brand-row-preview.html', kind: 'html', nodeId: 'approve_7gl6' })
    const complete = art({ id: 'nc', name: 'node_complete.json', kind: 'json', nodeId: 'approve_7gl6' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [approvePage, demo, complete],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [gn('approve_7gl6', 'Approve', CLARIFY_CAPS)],
        } as any,
        nodeId: 'approve_7gl6',
        node: CLARIFY_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-card-page.html"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-brand-row-preview.html"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-node_complete.json"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides a standalone visual_*.page.html card from the known-product grid', async () => {
    const alias = art({
      id: 'alias',
      name: 'visual_1.page.html',
      kind: 'html',
      nodeId: 'visual_1',
      content: '<p>new</p>',
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [alias],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [gn('visual_1', '视觉', writesCaps('page'))],
          nodeExecutions: {
            visual_1: [
              { nodeId: 'visual_1', iteration: 1, status: 'completed', outputs: { page: '<p>old</p>' } },
              { nodeId: 'visual_1', iteration: 2, status: 'waiting_human', outputs: { page: '<p>new</p>' } },
            ],
          },
        } as any,
        nodeId: 'visual_1',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-card-page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-visual_1.page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-grid-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-ide"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-log"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('本次运行还没有产物')
    wrapper.unmount()
  })

  it('defaults to page.html preview for page-writing nodes and hides duplicate visual_*.page.html (s1)', async () => {
    const live = art({ id: 'live', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const copy = art({ id: 'copy', name: 'visual_bqc5.page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [copy, live],
        runId: 'run-1',
        nodeId: 'visual_bqc5',
        node: PAGE_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="react-artifact-tab-visual_bqc5.page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-tab-grid"]').exists()).toBe(true)
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await flushPromises()
    // #356 merges/hides same-node visual_*.page.html when page.html is present
    expect(wrapper.find('[data-testid="react-artifact-card-visual_bqc5.page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-page.html"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('defaults to the newest own-node HTML for unpinned clarify and ignores upstream page.html (s2)', async () => {
    const upstream = art({
      id: 'up',
      name: 'page.html',
      kind: 'html',
      nodeId: 'visual_bqc5',
      updatedAt: '2026-08-19T20:00:00Z',
    })
    const older = art({
      id: 'old',
      name: 'a.html',
      kind: 'html',
      nodeId: 'react_ymx0',
      updatedAt: '2026-08-19T10:00:00Z',
    })
    const newer = art({
      id: 'new',
      name: 'brand-row-preview.html',
      kind: 'html',
      nodeId: 'react_ymx0',
      updatedAt: '2026-08-19T12:00:00Z',
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [upstream, older, newer],
        runId: 'run-1',
        nodeId: 'react_ymx0',
        node: ASK_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-brand-row-preview.html"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.find('[data-testid="react-artifact-tab-page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-brand-row-preview.html"]').exists()).toBe(true)
    // react/approve grid lists all visible products (incl. older own-node HTML).
    expect(wrapper.find('[data-testid="react-artifact-card-a.html"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps a clarify pin ahead of newest-HTML fallback (s2)', async () => {
    const html = art({ id: 'h', name: 'brand-row-preview.html', kind: 'html', nodeId: 'react_ymx0' })
    const md = art({ id: 'm', name: 'note.md', kind: 'markdown', nodeId: 'react_ymx0' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [html, md],
        previewArtifact: 'note.md',
        runId: 'run-1',
        nodeId: 'react_ymx0',
        node: ASK_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-note.md"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('stays on workflow grid when only JSON is on stage and still opens on click (s4 g2.1)', async () => {
    const json = art({ id: 'j', name: 'research.json', kind: 'json', nodeId: 'research' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [json],
        runId: 'run-1',
        nodeId: 'research',
        node: RESEARCH_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="react-artifact-tab-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-preview-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-tab-research.json"]').exists()).toBe(false)
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('opens page.html once when it arrives while the user is still on the default workflow grid (s5 g2.1)', async () => {
    const json = art({ id: 'j', name: 'research.json', kind: 'json', nodeId: 'visual_bqc5' })
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [json],
        runId: 'run-1',
        nodeId: 'visual_bqc5',
        node: PAGE_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    await wrapper.setProps({ artifacts: [json, page] })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('does not steal focus after the user leaves the default tab, including same-name revisions (s3)', async () => {
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5', revision: 1, updatedAt: 't1' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [page],
        runId: 'run-1',
        nodeId: 'visual_bqc5',
        node: PAGE_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.setProps({
      artifacts: [{ ...page, revision: 2, updatedAt: 't2', sizeBytes: 80 }],
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="react-artifact-preview-page.html"]').text()).toContain('page.html')
    wrapper.unmount()
  })

  it('shows only known products on the visual workflow grid and pins page.html (s6)', async () => {
    const research = art({ id: 'r', name: 'research.json', kind: 'json', nodeId: 'research' })
    const requirement = art({
      id: 'c',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'react_ymx0',
    })
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const complete = art({ id: 'n', name: 'node_complete.json', kind: 'json', nodeId: 'visual_bqc5' })
    const feedback = art({ id: 'f', name: 'feedback_index.json', kind: 'json', nodeId: 'react_ymx0' })
    const copy = art({ id: 'v', name: 'visual_bqc5.page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const demo = art({ id: 'd', name: 'brand-row-preview.html', kind: 'html', nodeId: 'react_ymx0' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [research, requirement, page, complete, feedback, copy, demo],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [
            gn('visual_bqc5', '视觉', writesCaps('page')),
            gn('react_ymx0', '澄清', ASK_CAPS),
            gn('research', '调研', writesCaps('research')),
          ],
        } as any,
        nodeId: 'visual_bqc5',
        node: PAGE_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-card-research.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-clarified_requirement.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-page.html"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-node_complete.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-feedback_index.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-visual_bqc5.page.html"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-brand-row-preview.html"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-page.html"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('keeps the clarify auto-pin on the grid so closing the tab remains reopenable', async () => {
    const requirement = art({
      id: 'c',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'react_ymx0',
    })
    const demo = art({ id: 'd', name: 'brand-row-preview.html', kind: 'html', nodeId: 'react_ymx0' })
    const complete = art({ id: 'n', name: 'node_complete.json', kind: 'json', nodeId: 'react_ymx0' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [requirement, demo, complete],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [gn('react_ymx0', '澄清', ASK_CAPS)],
        } as any,
        nodeId: 'react_ymx0',
        node: ASK_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-brand-row-preview.html"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.find('[data-testid="react-artifact-card-brand-row-preview.html"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-card-node_complete.json"]').exists()).toBe(false)
    await wrapper.get('[data-testid="react-artifact-tab-close-brand-row-preview.html"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-brand-row-preview.html"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="react-artifact-card-brand-row-preview.html"]').exists()).toBe(true)
    await wrapper.get('[data-testid="react-artifact-card-brand-row-preview.html"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-brand-row-preview.html"]').attributes('aria-selected')).toBe(
      'true',
    )
    wrapper.unmount()
  })

  it('shows friendly card/tab titles and meta with technical file name (g2)', async () => {
    const research = art({
      id: 'r',
      name: 'research.json',
      kind: 'json',
      nodeId: 'research',
      content: JSON.stringify({ title: '调研标题', summary: '调研摘要内容' }),
    })
    const clarified = art({
      id: 'c',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'clarify',
      content: JSON.stringify({ title: '澄清标题', summary: '澄清摘要' }),
    })
    const page = art({
      id: 'p',
      name: 'page.html',
      kind: 'html',
      nodeId: 'visual_bqc5',
      content: '<html><head><title>视觉 Demo</title></head><body><p>视觉摘要</p></body></html>',
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [research, clarified, page],
        runId: 'run-friendly',
        run: {
          id: 'run-friendly',
          nodes: [
            gn('visual_bqc5', '视觉', writesCaps('page')),
            gn('research', '调研', writesCaps('research')),
            gn('clarify', '澄清', ASK_CAPS),
          ],
        } as any,
        nodeId: 'visual_bqc5',
        node: PAGE_NODE,
        remoteKind: 'off',
        inlineContent: true,
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    const researchCard = wrapper.get('[data-testid="react-artifact-card-research.json"]')
    expect(researchCard.text()).toContain('调研')
    expect(researchCard.text()).toContain('research.json')
    expect(researchCard.text()).toContain('JSON')
    expect(wrapper.get('[data-testid="react-artifact-card-clarified_requirement.json"]').text()).toContain(
      '需求澄清',
    )
    expect(wrapper.get('[data-testid="react-artifact-card-page.html"]').text()).toContain('网页预览')
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').text()).toContain('调研')
    wrapper.unmount()
  })

  it('renders JSON and visual HTML title+summary thumbs and keeps icon on parse failure (g1)', async () => {
    const research = art({
      id: 'r',
      name: 'research.json',
      kind: 'json',
      nodeId: 'research',
      content: JSON.stringify({ title: '工作流产物卡片「简单预览」技术调研', summary: '上游诉求对照截图。' }),
    })
    const empty = art({
      id: 'e',
      name: 'plan.json',
      kind: 'json',
      nodeId: 'plan',
      content: JSON.stringify({ goals: [] }),
    })
    const page = art({
      id: 'p',
      name: 'page.html',
      kind: 'html',
      nodeId: 'visual_bqc5',
      content:
        '<html><head><title>Grasp · Demo</title></head><body><div class="banner"><h1>主标题</h1><p>banner 摘要</p></div></body></html>',
    })
    vi.mocked(api.artifactContent).mockImplementation(async (id: string) => {
      if (id === 'r') return research
      if (id === 'e') return empty
      if (id === 'p') return page
      return art({ id, name: id, content: '' })
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [
          { ...research, content: undefined },
          { ...empty, content: undefined },
          { ...page, content: undefined },
        ],
        runId: 'run-summary',
        run: {
          id: 'run-summary',
          nodes: [
            gn('visual_bqc5', '视觉', writesCaps('page')),
            gn('research', '调研', writesCaps('research')),
            gn('plan', '计划', writesCaps('plan')),
          ],
        } as any,
        nodeId: 'visual_bqc5',
        node: PAGE_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    const researchSummary = wrapper.get(
      '[data-testid="react-artifact-card-research.json"] [data-testid="react-artifact-card-summary"]',
    )
    expect(researchSummary.text()).toContain('工作流产物卡片「简单预览」技术调研')
    expect(researchSummary.text()).toContain('上游诉求对照截图。')
    expect(
      wrapper.find('[data-testid="react-artifact-card-plan.json"] [data-testid="react-artifact-card-summary"]').exists(),
    ).toBe(false)
    const pageSummary = wrapper.get(
      '[data-testid="react-artifact-card-page.html"] [data-testid="react-artifact-card-summary"]',
    )
    expect(pageSummary.text()).toContain('Grasp · Demo')
    expect(pageSummary.text()).toContain('banner 摘要')
    wrapper.unmount()
  })

  it('restores open tabs from sessionStorage and prefers restore over pin (g3)', async () => {
    resetStageOpenStateForTests()
    const research = art({ id: 'r', name: 'research.json', kind: 'json', nodeId: 'research' })
    const clarified = art({
      id: 'c',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'clarify',
    })
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const stageProps = {
      artifacts: [research, clarified, page],
      runId: 'run-restore',
      run: {
        id: 'run-restore',
        nodes: [
          gn('visual_bqc5', '视觉', writesCaps('page')),
          gn('research', '调研', writesCaps('research')),
          gn('clarify', '澄清', ASK_CAPS),
        ],
      } as any,
      nodeId: 'visual_bqc5',
      node: PAGE_NODE,
      remoteKind: 'off' as const,
    }
    const first = mount(ReactArtifactStage, {
      props: stageProps,
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    // visual pin opens page.html; open clarified and activate it, then close page
    await first.get('[data-testid="react-artifact-card-clarified_requirement.json"]').trigger('click')
    await flushPromises()
    if (first.find('[data-testid="react-artifact-tab-close-page.html"]').exists()) {
      await first.get('[data-testid="react-artifact-tab-close-page.html"]').trigger('click')
      await flushPromises()
    }
    expect(first.get('[data-testid="react-artifact-tab-clarified_requirement.json"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(first.find('[data-testid="react-artifact-tab-page.html"]').exists()).toBe(false)
    first.unmount()

    const second = mount(ReactArtifactStage, {
      props: stageProps,
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(second.find('[data-testid="react-artifact-tab-clarified_requirement.json"]').exists()).toBe(true)
    expect(second.get('[data-testid="react-artifact-tab-clarified_requirement.json"]').attributes('aria-selected')).toBe(
      'true',
    )
    // closed page.html must not be restored even though visual pin would otherwise open it
    expect(second.find('[data-testid="react-artifact-tab-page.html"]').exists()).toBe(false)
    expect(second.get('[data-testid="react-artifact-tab-clarified_requirement.json"]').text()).toContain(
      '需求澄清',
    )
    second.unmount()
    resetStageOpenStateForTests()
  })

  it('skips vanished artifacts when restoring open state (g3.3)', async () => {
    resetStageOpenStateForTests()
    sessionStorage.setItem(
      'appr.reactStageOpen:run-gone:visual_bqc5',
      JSON.stringify({
        openNames: ['page.html', 'gone.json', 'research.json'],
        activeTab: 'preview:gone.json',
        novncOpen: false,
      }),
    )
    const research = art({ id: 'r', name: 'research.json', kind: 'json', nodeId: 'research' })
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [research, page],
        runId: 'run-gone',
        run: {
          id: 'run-gone',
          nodes: [
            gn('visual_bqc5', '视觉', writesCaps('page')),
            gn('research', '调研', writesCaps('research')),
          ],
        } as any,
        nodeId: 'visual_bqc5',
        node: PAGE_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-gone.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-tab-research.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-tab-page.html"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
    resetStageOpenStateForTests()
  })

  it('auto-pins clarified_requirement and plan on clarify without set_artifact_preview (g1.1 / g1.2)', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-auto-pin',
        nodeId: 'approve_1',
        node: CLARIFY_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    const clarified = art({
      id: 'c1',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'approve_1',
      revision: 1,
    })
    await wrapper.setProps({ artifacts: [clarified] })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-clarified_requirement.json"]').attributes('aria-selected')).toBe(
      'true',
    )
    const plan = art({ id: 'p1', name: 'plan.json', kind: 'json', nodeId: 'approve_1', revision: 1 })
    await wrapper.setProps({ artifacts: [clarified, plan] })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-clarified_requirement.json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(true)
    // Already viewing clarified — plan pins with unread, does not steal focus (g2.1).
    expect(wrapper.get('[data-testid="react-artifact-tab-clarified_requirement.json"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.get('[data-testid="react-artifact-tab-unread-plan.json"]').attributes('data-unread')).toBe('new')
    wrapper.unmount()
  })

  it('focuses the latest auto-pinned artifact when still on empty/grid (g2.1)', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-auto-idle',
        nodeId: 'approve_1',
        node: CLARIFY_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    const clarified = art({
      id: 'c1',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'approve_1',
    })
    const plan = art({ id: 'p1', name: 'plan.json', kind: 'json', nodeId: 'approve_1' })
    await wrapper.setProps({ artifacts: [clarified, plan] })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-clarified_requirement.json"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-plan.json"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('marks updated tabs without stealing focus, and reopens closed tabs on change (g1.2 / g1.3 / g2.1)', async () => {
    const clarified = art({
      id: 'c1',
      name: 'clarified_requirement.json',
      kind: 'json',
      nodeId: 'approve_1',
      revision: 1,
      updatedAt: 't1',
    })
    const plan = art({
      id: 'p1',
      name: 'plan.json',
      kind: 'json',
      nodeId: 'approve_1',
      revision: 1,
      updatedAt: 't1',
    })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [clarified, plan],
        runId: 'run-auto-unread',
        nodeId: 'approve_1',
        node: CLARIFY_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    // Seed fingerprints with both present; open clarified manually.
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-clarified_requirement.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-clarified_requirement.json"]').attributes('aria-selected')).toBe(
      'true',
    )
    await wrapper.setProps({
      artifacts: [clarified, { ...plan, revision: 2, updatedAt: 't2', sizeBytes: 40 }],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-clarified_requirement.json"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.get('[data-testid="react-artifact-tab-unread-plan.json"]').attributes('data-unread')).toBe('updated')
    await wrapper.get('[data-testid="react-artifact-tab-plan.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-unread-plan.json"]').exists()).toBe(false)

    await wrapper.get('[data-testid="react-artifact-tab-close-plan.json"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(false)
    await wrapper.setProps({
      artifacts: [
        clarified,
        { ...plan, revision: 3, updatedAt: 't3', sizeBytes: 50 },
      ],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-unread-plan.json"]').attributes('data-unread')).toBe('updated')
    wrapper.unmount()
  })

  it('does not auto-pin internal names and keeps custom HTML reopenable from the grid (g1.1 / g2.2)', async () => {
    const demo = art({
      id: 'd1',
      name: 'brand-row-preview.html',
      kind: 'html',
      nodeId: 'react_1',
      content: '<html>demo</html>',
    })
    const complete = art({ id: 'n1', name: 'node_complete.json', kind: 'json', nodeId: 'react_1' })
    const feedback = art({ id: 'f1', name: 'feedback_index.json', kind: 'json', nodeId: 'react_1' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [demo, complete, feedback],
        runId: 'run-auto-grid',
        run: {
          id: 'run-auto-grid',
          nodes: [gn('react_1', '澄清', ASK_CAPS)],
        } as any,
        nodeId: 'react_1',
        node: ASK_NODE,
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-node_complete.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-tab-feedback_index.json"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-artifact-card-brand-row-preview.html"]').exists()).toBe(true)
    await wrapper.get('[data-testid="react-artifact-tab-close-brand-row-preview.html"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-card-brand-row-preview.html"]').exists()).toBe(true)
    await wrapper.get('[data-testid="react-artifact-card-brand-row-preview.html"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-brand-row-preview.html"]').attributes('aria-selected')).toBe(
      'true',
    )
    wrapper.unmount()
  })
})

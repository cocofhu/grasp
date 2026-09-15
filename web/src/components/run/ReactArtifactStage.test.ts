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

  it('defaults to pipeline artifacts grid, then opens a named preview tab on card click (g2.1)', async () => {
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
    expect(wrapper.get('[data-testid="react-artifact-tab-grid"]').text()).toContain('流水线产物')
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

  it('opens a noVNC tab from the pipeline card without replacing artifact tabs', async () => {
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
    expect(wrapper.find('[data-testid="react-artifact-card-novnc"]').exists()).toBe(true)
    await wrapper.get('[data-testid="react-artifact-card-research.json"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await wrapper.get('[data-testid="react-artifact-card-novnc"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="react-artifact-tab-research.json"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-artifact-tab-novnc"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="novnc-stub"]').attributes('data-inspectable')).toBe('1')
    await wrapper.get('[data-testid="react-artifact-tab-close-novnc"]').trigger('click')
    expect(wrapper.find('[data-testid="react-artifact-tab-novnc"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="react-artifact-tab-research.json"]').attributes('aria-selected')).toBe('true')
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

  it('hides app preview tab for Approve when no ports are registered (plan g1.4 / g2.3)', async () => {
    vi.mocked(api.nodePreviews).mockResolvedValue({ ports: [] })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'approve_1' })],
        runId: 'run-approve-empty',
        nodeId: 'approve_1',
        nodeType: 'approve',
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

  it('shows app preview tab for Approve after silent probe finds ports (plan g2.2)', async () => {
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
        nodeType: 'approve',
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

  it('does not steal focus when Approve ports arrive after userMoved (plan g2.2)', async () => {
    vi.useFakeTimers()
    vi.mocked(api.nodePreviews).mockResolvedValue({ ports: [] })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [art({ id: 'a1', name: 'research.json', kind: 'json', nodeId: 'approve_1' })],
        runId: 'run-approve-moved',
        nodeId: 'approve_1',
        nodeType: 'approve',
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

  it('keeps app_preview remote tab even when the port list is still empty (plan g2.3)', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-app-preview',
        nodeId: 'preview_1',
        nodeType: 'app_preview',
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
            { id: 'visual_1', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
            { id: 'visual_other', type: 'visual', label: '另一页', position: { x: 0, y: 0 }, config: {} },
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
          nodes: [{ id: 'visual_1', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} }],
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
          nodes: [{ id: 'visual_1', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} }],
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

  it('keeps approve page.html and agent-named HTML on the pipeline grid', async () => {
    const approvePage = art({ id: 'ap', name: 'page.html', kind: 'html', nodeId: 'approve_7gl6' })
    const demo = art({ id: 'd', name: 'brand-row-preview.html', kind: 'html', nodeId: 'approve_7gl6' })
    const complete = art({ id: 'nc', name: 'node_complete.json', kind: 'json', nodeId: 'approve_7gl6' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [approvePage, demo, complete],
        runId: 'run-1',
        run: {
          id: 'run-1',
          nodes: [{ id: 'approve_7gl6', type: 'approve', label: 'Approve', position: { x: 0, y: 0 }, config: {} }],
        } as any,
        nodeId: 'approve_7gl6',
        nodeType: 'approve',
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
          nodes: [{ id: 'visual_1', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} }],
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
    expect(wrapper.find('[data-testid="react-artifact-grid-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('defaults to page.html preview for visual nodes and hides duplicate visual_*.page.html (s1)', async () => {
    const live = art({ id: 'live', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const copy = art({ id: 'copy', name: 'visual_bqc5.page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [copy, live],
        runId: 'run-1',
        nodeId: 'visual_bqc5',
        nodeType: 'visual',
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

  it('defaults to the newest own-node HTML for unpinned react and ignores upstream page.html (s2)', async () => {
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
        nodeType: 'react',
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

  it('keeps a react pin ahead of newest-HTML fallback (s2)', async () => {
    const html = art({ id: 'h', name: 'brand-row-preview.html', kind: 'html', nodeId: 'react_ymx0' })
    const md = art({ id: 'm', name: 'note.md', kind: 'markdown', nodeId: 'react_ymx0' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [html, md],
        previewArtifact: 'note.md',
        runId: 'run-1',
        nodeId: 'react_ymx0',
        nodeType: 'react',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="react-artifact-tab-note.md"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('stays on pipeline grid when only JSON is on stage and still opens on click (s4 g2.1)', async () => {
    const json = art({ id: 'j', name: 'research.json', kind: 'json', nodeId: 'research' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [json],
        runId: 'run-1',
        nodeId: 'research',
        nodeType: 'research',
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

  it('opens page.html once when it arrives while the user is still on the default pipeline grid (s5 g2.1)', async () => {
    const json = art({ id: 'j', name: 'research.json', kind: 'json', nodeId: 'visual_bqc5' })
    const page = art({ id: 'p', name: 'page.html', kind: 'html', nodeId: 'visual_bqc5' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [json],
        runId: 'run-1',
        nodeId: 'visual_bqc5',
        nodeType: 'visual',
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
        nodeType: 'visual',
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

  it('shows only known products on the visual pipeline grid and pins page.html (s6)', async () => {
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
            { id: 'visual_bqc5', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
            { id: 'react_ymx0', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} },
            { id: 'research', type: 'research', label: '调研', position: { x: 0, y: 0 }, config: {} },
          ],
        } as any,
        nodeId: 'visual_bqc5',
        nodeType: 'visual',
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

  it('keeps the react auto-pin on the grid so closing the tab remains reopenable', async () => {
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
          nodes: [{ id: 'react_ymx0', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} }],
        } as any,
        nodeId: 'react_ymx0',
        nodeType: 'react',
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
    const proposals = art({ id: 'pr', name: 'proposals.json', kind: 'json', nodeId: 'proposal' })
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [research, clarified, page, proposals],
        runId: 'run-friendly',
        run: {
          id: 'run-friendly',
          nodes: [
            { id: 'visual_bqc5', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
            { id: 'research', type: 'research', label: '调研', position: { x: 0, y: 0 }, config: {} },
            { id: 'clarify', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} },
            { id: 'proposal', type: 'proposal', label: '方案', position: { x: 0, y: 0 }, config: {} },
          ],
        } as any,
        nodeId: 'visual_bqc5',
        nodeType: 'visual',
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
    expect(wrapper.get('[data-testid="react-artifact-card-proposals.json"]').text()).toContain('候选方案')
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
      content: JSON.stringify({ title: '流水线产物卡片「简单预览」技术调研', summary: '上游诉求对照截图。' }),
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
            { id: 'visual_bqc5', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
            { id: 'research', type: 'research', label: '调研', position: { x: 0, y: 0 }, config: {} },
            { id: 'plan', type: 'plan', label: '计划', position: { x: 0, y: 0 }, config: {} },
          ],
        } as any,
        nodeId: 'visual_bqc5',
        nodeType: 'visual',
        remoteKind: 'off',
      },
      global: { plugins: [i18n()], stubs },
    })
    await flushPromises()
    const researchSummary = wrapper.get(
      '[data-testid="react-artifact-card-research.json"] [data-testid="react-artifact-card-summary"]',
    )
    expect(researchSummary.text()).toContain('流水线产物卡片「简单预览」技术调研')
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
          { id: 'visual_bqc5', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
          { id: 'research', type: 'research', label: '调研', position: { x: 0, y: 0 }, config: {} },
          { id: 'clarify', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} },
        ],
      } as any,
      nodeId: 'visual_bqc5',
      nodeType: 'visual',
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
            { id: 'visual_bqc5', type: 'visual', label: '视觉', position: { x: 0, y: 0 }, config: {} },
            { id: 'research', type: 'research', label: '调研', position: { x: 0, y: 0 }, config: {} },
          ],
        } as any,
        nodeId: 'visual_bqc5',
        nodeType: 'visual',
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

  it('auto-pins clarified_requirement and plan on approve without set_artifact_preview (g1.1 / g1.2)', async () => {
    const wrapper = mount(ReactArtifactStage, {
      props: {
        artifacts: [],
        runId: 'run-auto-pin',
        nodeId: 'approve_1',
        nodeType: 'approve',
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
        nodeType: 'approve',
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
        nodeType: 'approve',
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
          nodes: [{ id: 'react_1', type: 'react', label: '澄清', position: { x: 0, y: 0 }, config: {} }],
        } as any,
        nodeId: 'react_1',
        nodeType: 'react',
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

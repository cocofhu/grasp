// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  artifactVersions: vi.fn(),
  artifactDownloadUrl: vi.fn((id: string) => `http://test/api/artifacts/${id}/download`),
  deleteArtifact: vi.fn(),
}))
const copyMocks = vi.hoisted(() => ({ copyToClipboard: vi.fn() }))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      artifactContent: apiMocks.artifactContent,
      artifactVersions: apiMocks.artifactVersions,
      artifactDownloadUrl: apiMocks.artifactDownloadUrl,
      deleteArtifact: apiMocks.deleteArtifact,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn() }),
}))
vi.mock('@/lib/shared/copyToClipboard', () => ({
  copyToClipboard: copyMocks.copyToClipboard,
}))

import type { Artifact } from '@/lib/shared/types'
import ArtifactPreview from './ArtifactPreview.vue'

const clarifiedDoc = {
  title: '需求标题',
  summary: '概述',
  goals: ['g'],
  in_scope: ['in'],
  out_of_scope: ['out'],
  functional_requirements: [{ title: 'f1', detail: 'd', acceptance_criteria: ['a'] }],
  assumptions: ['a'],
  dependencies: ['d'],
  constraints: ['c'],
}

const artA = {
  id: 'art-a',
  name: 'clarified_requirement.json',
  kind: 'json' as const,
  nodeId: 'react',
  runId: 'run-1',
  workflowName: 'wf',
  sizeBytes: 100,
  createdAt: '2026-08-10T12:00:00Z',
}

const artB = {
  ...artA,
  id: 'art-b',
  name: 'plan.json',
  nodeId: 'plan',
}

function mountPreview(
  artifact: Artifact | null,
  extra: Record<string, unknown> = {},
) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ArtifactPreview, {
    props: { artifact, ...extra },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        HtmlPreview: {
          props: ['html', 'inspectable'],
          template: `<div data-testid="html-preview-stub" :data-inspectable="inspectable ? '1' : '0'">
            <div data-testid="html-preview-toolbar-stub"><slot name="toolbar-actions" /></div>
            <div v-if="$slots['canvas-overlay']" data-testid="html-preview-canvas-overlay"><slot name="canvas-overlay" /></div>
          </div>`,
        },
        StructuredArtifactView: {
          props: ['name', 'doc'],
          template: '<div data-testid="structured-stub">{{ doc?.title }}</div>',
        },
        AppModal: {
          props: ['open', 'title'],
          template: '<div v-if="open" data-testid="zoom-modal"><slot /><slot name="footer" /></div>',
        },
        AppButton: {
          emits: ['click'],
          template: '<button v-bind="$attrs" @click="$emit(\'click\')"><slot /></button>',
        },
      },
    },
  })
}

describe('ArtifactPreview structured mode + capability trim', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactVersions.mockResolvedValue([])
    apiMocks.artifactContent.mockImplementation(async (id: string) => ({
      ...(id === 'art-b' ? artB : artA),
      content: JSON.stringify(
        id === 'art-b' ? { title: '计划 B', goals: [] } : clarifiedDoc,
      ),
    }))
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('defaults to structured UI with mode toggle (g1.2)', async () => {
    const wrapper = mountPreview(artA)
    await flushPromises()
    expect(wrapper.find('[data-testid="structured-stub"]').text()).toContain('需求标题')
    expect(wrapper.find('[data-testid="artifact-preview-mode-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-raw-json"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the friendly product title while retaining the technical filename', async () => {
    const wrapper = mountPreview(artA)
    await flushPromises()
    const header = wrapper.get('[data-testid="artifact-preview-identity-bar"]')
    expect(header.text()).toContain('需求澄清')
    const meta = header.get('[data-testid="artifact-preview-file-meta"]')
    expect(meta.text()).toBe('clarified_requirement.json')
    expect(meta.find('.uppercase').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides the name row but keeps the toolbar when the host tab already names the artifact', async () => {
    const wrapper = mountPreview(artA, { hideFileRow: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-file-row"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('clarified_requirement.json')
    expect(wrapper.find('[data-testid="artifact-preview-toolbar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-mode-toggle"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('switches to raw JSON and back without losing selection (g1.2)', async () => {
    const wrapper = mountPreview(artA)
    await flushPromises()
    await wrapper.find('[data-testid="artifact-preview-mode-raw"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="artifact-preview-raw-json"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-stub"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-raw-json"]').text()).toContain('需求标题')
    await wrapper.find('[data-testid="artifact-preview-mode-structured"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="structured-stub"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('resets to structured when artifact changes while in raw (g1.3)', async () => {
    const wrapper = mountPreview(artA)
    await flushPromises()
    await wrapper.find('[data-testid="artifact-preview-mode-raw"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="artifact-preview-raw-json"]').exists()).toBe(true)
    await wrapper.setProps({ artifact: artB })
    await flushPromises()
    await nextTick()
    expect(wrapper.find('[data-testid="structured-stub"]').text()).toContain('计划 B')
    expect(wrapper.find('[data-testid="artifact-preview-raw-json"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides delete/copy/zoom/export when trim props set (g1.1 / g1.4)', async () => {
    const wrapper = mountPreview(artA, {
      hideDelete: true,
      hideCopy: true,
      hideZoom: true,
      hideExport: true,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-delete"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-copy"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-zoom"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-download-png"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-download-pdf"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-download-raw"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-mode-toggle"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps delete/copy visible by default for Artifacts detail', async () => {
    const wrapper = mountPreview(artA)
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-delete"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-copy"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-zoom"]').exists()).toBe(true)
    wrapper.unmount()
  })
})

describe('ArtifactPreview HTML toolbar actions', () => {
  const htmlArtifact = {
    ...artA,
    id: 'art-html-toolbar',
    name: 'page.html',
    kind: 'html' as const,
    content: '<!doctype html><html><body>hello</body></html>',
  }

  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactVersions.mockResolvedValue([])
    copyMocks.copyToClipboard.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('moves working copy, download and delete actions into the HTML toolbar without a metadata header', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const wrapper = mountPreview(htmlArtifact)
    await flushPromises()
    const toolbar = wrapper.get('[data-testid="html-preview-toolbar-stub"]')
    expect(wrapper.text()).not.toContain('page.html')
    expect(wrapper.text()).not.toContain('网页预览')
    expect(wrapper.text()).not.toMatch(/\bhtml\b/)
    expect(wrapper.findAll('[data-testid="artifact-preview-copy"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="artifact-preview-download-raw"]')).toHaveLength(1)
    expect(toolbar.find('[data-testid="artifact-preview-delete"]').exists()).toBe(true)

    await toolbar.get('[data-testid="artifact-preview-copy"]').trigger('click')
    await flushPromises()
    expect(copyMocks.copyToClipboard).toHaveBeenCalledWith(htmlArtifact.content)
    await toolbar.get('[data-testid="artifact-preview-download-raw"]').trigger('click')
    expect(open).toHaveBeenCalledWith('http://test/api/artifacts/art-html-toolbar/download', '_blank')

    await toolbar.get('[data-testid="artifact-preview-delete"]').trigger('click')
    expect(wrapper.find('[data-testid="zoom-modal"]').exists()).toBe(true)
    expect(apiMocks.deleteArtifact).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps raw download in the HTML toolbar when copy and delete are hidden', async () => {
    const wrapper = mountPreview(htmlArtifact, { hideCopy: true, hideDelete: true })
    await flushPromises()
    const toolbar = wrapper.get('[data-testid="html-preview-toolbar-stub"]')
    expect(wrapper.find('[data-testid="artifact-preview-copy"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-delete"]').exists()).toBe(false)
    expect(toolbar.find('[data-testid="artifact-preview-download-raw"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps the same HTML preview and toolbar through loading, failure, and retry', async () => {
    let rejectLoad!: (reason: Error) => void
    apiMocks.artifactContent.mockImplementationOnce(() => new Promise((_resolve, reject) => {
      rejectLoad = reject
    }))
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const { content: _content, ...unloaded } = htmlArtifact
    const wrapper = mountPreview(unloaded)
    await flushPromises()
    const preview = wrapper.get('[data-testid="html-preview-stub"]').element
    const toolbar = wrapper.get('[data-testid="html-preview-toolbar-stub"]')
    expect(wrapper.find('[data-testid="artifact-preview-identity-bar"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview-canvas-overlay"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('page.html')
    expect(wrapper.text()).not.toContain('网页预览')
    expect(toolbar.find('[data-testid="artifact-preview-copy"]').exists()).toBe(true)
    await toolbar.get('[data-testid="artifact-preview-download-raw"]').trigger('click')
    expect(open).toHaveBeenCalledWith('http://test/api/artifacts/art-html-toolbar/download', '_blank')

    rejectLoad(new Error('offline'))
    await flushPromises()
    const error = wrapper.get('[data-testid="html-preview-canvas-overlay"]').get('[data-testid="artifact-preview-load-error"]')
    expect(wrapper.get('[data-testid="html-preview-stub"]').element).toBe(preview)
    expect(toolbar.find('[data-testid="artifact-preview-copy"]').exists()).toBe(true)
    expect(toolbar.find('[data-testid="artifact-preview-download-raw"]').exists()).toBe(true)
    apiMocks.artifactContent.mockResolvedValueOnce(htmlArtifact)
    await error.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="html-preview-stub"]').element).toBe(preview)
    expect(wrapper.find('[data-testid="html-preview-canvas-overlay"]').exists()).toBe(false)
    expect(toolbar.find('[data-testid="artifact-preview-copy"]').exists()).toBe(true)
    expect(toolbar.find('[data-testid="artifact-preview-download-raw"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="artifact-preview-copy"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="artifact-preview-download-raw"]')).toHaveLength(1)
    wrapper.unmount()
  })

  it('retains a working raw download for an empty HTML file', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const wrapper = mountPreview({ ...htmlArtifact, content: '' })
    await flushPromises()
    const toolbar = wrapper.get('[data-testid="html-preview-toolbar-stub"]')
    expect(wrapper.get('[data-testid="html-preview-canvas-overlay"]').text()).toContain('该产物内容为空')
    expect(wrapper.find('[data-testid="artifact-preview-identity-bar"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('page.html')
    await toolbar.get('[data-testid="artifact-preview-download-raw"]').trigger('click')
    expect(open).toHaveBeenCalledWith('http://test/api/artifacts/art-html-toolbar/download', '_blank')
    wrapper.unmount()
  })
})

describe('ArtifactPreview annotate', () => {
  it('turns on HTML inspect when annotatable and a review channel is provided', async () => {
    const { defineComponent, h } = await import('vue')
    const { provideReviewAnnotate } = await import('@/lib/inbox/reviewAnnotate')
    const annotate = vi.fn()
    const htmlArt = {
      ...artA,
      id: 'art-html',
      name: 'page.html',
      kind: 'html' as const,
      content: '<!doctype html><html><body>hi</body></html>',
    }
    const Host = defineComponent({
      setup() {
        provideReviewAnnotate({
          get enabled() {
            return true
          },
          annotate,
        })
        return () =>
          h(ArtifactPreview, {
            artifact: htmlArt,
            annotatable: true,
            hideDelete: true,
          })
      },
    })
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(Host, {
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          HtmlPreview: {
            props: ['html', 'inspectable'],
            template: '<div data-testid="html-preview-stub" :data-inspectable="inspectable ? \'1\' : \'0\'"><slot name="toolbar-actions" /></div>',
          },
          AppModal: true,
          AppButton: true,
          SelectionAddToChat: true,
        },
      },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="html-preview-stub"]').attributes('data-inspectable')).toBe('1')
    wrapper.unmount()
  })

  it('keeps HTML inspect off without annotatable', async () => {
    const htmlArt = {
      ...artA,
      id: 'art-html-2',
      name: 'page.html',
      kind: 'html' as const,
      content: '<!doctype html><html><body>hi</body></html>',
    }
    const wrapper = mountPreview(htmlArt)
    await flushPromises()
    expect(wrapper.get('[data-testid="html-preview-stub"]').attributes('data-inspectable')).toBe('0')
    wrapper.unmount()
  })

  it('turns on markdown/json quote annotate when annotatable', async () => {
    const { defineComponent, h } = await import('vue')
    const { provideReviewAnnotate } = await import('@/lib/inbox/reviewAnnotate')
    const mdArt = {
      ...artA,
      id: 'art-md',
      name: 'copy.md',
      kind: 'markdown' as const,
      content: '# hello\n\nworld',
    }
    const jsonArt = {
      ...artA,
      id: 'art-json',
      name: 'notes.json',
      kind: 'json' as const,
      content: '{"title":"notes"}',
    }
    const Host = defineComponent({
      props: { artifact: { type: Object, required: true } },
      setup(p) {
        provideReviewAnnotate({
          get enabled() {
            return true
          },
          annotate: vi.fn(),
        })
        return () =>
          h(ArtifactPreview, {
            artifact: p.artifact as typeof mdArt,
            annotatable: true,
            hideDelete: true,
          })
      },
    })
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const stubs = {
      Icon: true,
      HtmlPreview: true,
      StructuredArtifactView: true,
      SelectionAddToChat: {
        props: ['enabled'],
        template: '<div data-testid="quote-annotate-stub" :data-enabled="enabled ? \'1\' : \'0\'" />',
      },
    }
    const md = mount(Host, {
      props: { artifact: mdArt },
      global: { plugins: [i18n], stubs },
    })
    await flushPromises()
    expect(md.get('[data-testid="quote-annotate-stub"]').attributes('data-enabled')).toBe('1')
    md.unmount()

    const json = mount(Host, {
      props: { artifact: jsonArt },
      global: { plugins: [i18n], stubs },
    })
    await flushPromises()
    expect(json.get('[data-testid="quote-annotate-stub"]').attributes('data-enabled')).toBe('1')
    json.unmount()
  })
})

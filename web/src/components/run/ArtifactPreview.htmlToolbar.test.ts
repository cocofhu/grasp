// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact } from '@/lib/shared/types'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  artifactVersions: vi.fn(),
  artifactVersionContent: vi.fn(),
  artifactDownloadUrl: vi.fn((id: string) => `http://test/api/artifacts/${id}/download`),
  deleteArtifact: vi.fn(),
}))

const copyMocks = vi.hoisted(() => ({
  copyToClipboard: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      artifactContent: apiMocks.artifactContent,
      artifactVersions: apiMocks.artifactVersions,
      artifactVersionContent: apiMocks.artifactVersionContent,
      artifactDownloadUrl: apiMocks.artifactDownloadUrl,
      deleteArtifact: apiMocks.deleteArtifact,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn() }),
}))

vi.mock('@/lib/shared/copyToClipboard', () => ({
  copyToClipboard: (...args: unknown[]) => copyMocks.copyToClipboard(...args),
}))

import ArtifactPreview from './ArtifactPreview.vue'

function art(partial: Partial<Artifact> & Pick<Artifact, 'id' | 'name'>): Artifact {
  return {
    kind: 'html',
    nodeId: 'visual_1',
    runId: 'run-1',
    sizeBytes: 32,
    createdAt: '2026-08-10T12:00:00Z',
    revision: 1,
    content: '<!doctype html><html><body><p>页面内容</p></body></html>',
    ...partial,
  } as Artifact
}

function mountPreview(artifact: Artifact | null, extra: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ArtifactPreview, {
    props: { artifact, ...extra },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        Icon: { template: '<span />' },
        AppModal: {
          props: ['open', 'title'],
          template: '<div v-if="open" data-testid="zoom-modal"><slot /><slot name="footer" /></div>',
        },
        AppButton: {
          emits: ['click'],
          template: '<button v-bind="$attrs" @click="$emit(\'click\')"><slot /></button>',
        },
        StructuredArtifactView: {
          props: ['name', 'doc'],
          template: '<div data-testid="structured-stub">{{ doc?.title }}</div>',
        },
      },
    },
  })
}

function toolbarOrder(wrapper: ReturnType<typeof mountPreview>) {
  const toolbar = wrapper.get('[data-testid="html-preview-toolbar"]')
  const end = toolbar.get('[data-testid="html-preview-toolbar-end"]')
  const order = [...end.element.children].map((el) => el.getAttribute('data-testid'))
  return { toolbar, end, order }
}

describe('ArtifactPreview HTML toolbar actions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactVersions.mockResolvedValue([])
    apiMocks.artifactContent.mockResolvedValue({ content: '<p>fetched</p>' })
    copyMocks.copyToClipboard.mockResolvedValue(true)
    vi.stubGlobal('open', vi.fn())
  })

  afterEach(() => {
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
  })

  it('hides identity text and keeps copy/download on the preview toolbar', async () => {
    const wrapper = mountPreview(art({ id: 'page', name: 'page.html' }), {
      hideDelete: true,
      hideVersionChip: true,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="artifact-preview-identity-bar"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('网页预览')
    expect(wrapper.text()).not.toContain('page.html')
    expect(wrapper.find('.chip').exists()).toBe(false)

    const { toolbar, order } = toolbarOrder(wrapper)
    expect(toolbar.text()).toContain('桌面')
    expect(toolbar.text()).toContain('移动端')
    expect(toolbar.text()).toContain('自适应宽度')
    expect(toolbar.text()).toContain('窗口放大查看')
    expect(order).toEqual([
      'html-preview-enlarge',
      'artifact-preview-copy',
      'artifact-preview-download-raw',
    ])

    const copy = toolbar.get('[data-testid="artifact-preview-copy"]')
    const download = toolbar.get('[data-testid="artifact-preview-download-raw"]')
    const host = wrapper.get('[data-testid="html-preview-pin-host"]')
    expect(host.element.contains(copy.element)).toBe(false)
    expect(
      host.element.compareDocumentPosition(copy.element) & Node.DOCUMENT_POSITION_PRECEDING,
    ).toBeTruthy()
    expect(wrapper.findAll('[data-testid="artifact-preview-copy"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="artifact-preview-download-raw"]')).toHaveLength(1)
    expect(copy.attributes('aria-label')).toBe('复制')
    expect(download.attributes('title')).toBe('下载原始 JSON')

    await copy.trigger('click')
    expect(copyMocks.copyToClipboard).toHaveBeenCalledWith(
      '<!doctype html><html><body><p>页面内容</p></body></html>',
    )
    await download.trigger('click')
    expect(window.open).toHaveBeenCalledWith('http://test/api/artifacts/page/download', '_blank')
    wrapper.unmount()
  })

  it('hides the filename for other html artifacts and keeps the toolbar', async () => {
    const wrapper = mountPreview(
      art({
        id: 'report',
        name: 'report.html',
        content: '<p>report body</p>',
      }),
      { hideDelete: true, hideVersionChip: true },
    )
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-identity-bar"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('report.html')
    expect(wrapper.text()).not.toContain('html')
    expect(toolbarOrder(wrapper).order).toEqual([
      'html-preview-enlarge',
      'artifact-preview-copy',
      'artifact-preview-download-raw',
    ])
    wrapper.unmount()
  })

  it('drops copy when hideCopy is set and still downloads from the toolbar', async () => {
    const wrapper = mountPreview(art({ id: 'page', name: 'page.html' }), {
      hideDelete: true,
      hideVersionChip: true,
      hideCopy: true,
    })
    await flushPromises()
    const { toolbar, order } = toolbarOrder(wrapper)
    expect(toolbar.find('[data-testid="artifact-preview-copy"]').exists()).toBe(false)
    expect(order).toEqual(['html-preview-enlarge', 'artifact-preview-download-raw'])
    wrapper.unmount()
  })

  it('keeps version, readonly, and delete on the identity bar without identity text', async () => {
    apiMocks.artifactVersions.mockResolvedValue([
      { artifactId: 'page', revision: 1, nodeId: 'visual_1', sizeBytes: 8, createdAt: 't1' },
    ])
    apiMocks.artifactVersionContent.mockResolvedValue({
      artifactId: 'page',
      revision: 1,
      nodeId: 'visual_1',
      sizeBytes: 8,
      createdAt: 't1',
      content: '<p>old</p>',
    })
    const wrapper = mountPreview(
      art({ id: 'page', name: 'page.html', revision: 2, content: '<p>new</p>' }),
    )
    await flushPromises()

    const bar = wrapper.get('[data-testid="artifact-preview-identity-bar"]')
    expect(bar.text()).not.toContain('网页预览')
    expect(bar.text()).not.toContain('page.html')
    expect(bar.text()).not.toContain('html')
    expect(bar.find('[data-testid="artifact-preview-version-chip"]').exists()).toBe(true)
    expect(bar.find('[data-testid="artifact-preview-delete"]').exists()).toBe(true)
    expect(bar.find('[data-testid="artifact-preview-copy"]').exists()).toBe(false)
    expect(bar.find('[data-testid="artifact-preview-download-raw"]').exists()).toBe(false)
    expect(toolbarOrder(wrapper).order).toEqual([
      'html-preview-enlarge',
      'artifact-preview-copy',
      'artifact-preview-download-raw',
    ])

    await bar.get('[data-testid="artifact-preview-version-chip-btn"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-testid="artifact-preview-version-option-v1"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(bar.find('[data-testid="artifact-preview-historical-readonly"]').exists()).toBe(true)
    expect(bar.find('[data-testid="artifact-preview-delete"]').exists()).toBe(false)
    expect(bar.text()).not.toContain('page.html')
    expect(wrapper.get('[data-testid="html-preview-toolbar"]').find('[data-testid="artifact-preview-copy"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps copy and download on the toolbar while HTML content is loading or failed', async () => {
    apiMocks.artifactContent.mockReturnValue(new Promise(() => {}))
    const pending = mountPreview(
      art({ id: 'page', name: 'page.html', content: undefined }),
      { hideDelete: true, hideVersionChip: true },
    )
    await flushPromises()
    expect(pending.find('[data-testid="artifact-preview-identity-bar"]').exists()).toBe(false)
    expect(pending.text()).not.toContain('网页预览')
    expect(pending.text()).not.toContain('page.html')
    expect(toolbarOrder(pending).order).toContain('artifact-preview-copy')
    expect(pending.get('[data-testid="html-preview-canvas-overlay"]').find('[data-testid="hard-load-layer"]').exists()).toBe(true)
    pending.unmount()

    apiMocks.artifactContent.mockRejectedValue(new Error('boom'))
    const failed = mountPreview(
      art({ id: 'page-err', name: 'page.html', content: undefined }),
      { hideDelete: true, hideVersionChip: true },
    )
    await flushPromises()
    expect(failed.find('[data-testid="artifact-preview-identity-bar"]').exists()).toBe(false)
    expect(failed.text()).not.toContain('page.html')
    const overlay = failed.get('[data-testid="html-preview-canvas-overlay"]')
    expect(overlay.get('[data-testid="artifact-preview-load-error"]').exists()).toBe(true)
    expect(toolbarOrder(failed).order).toEqual([
      'html-preview-enlarge',
      'artifact-preview-copy',
      'artifact-preview-download-raw',
    ])
    failed.unmount()
  })

  it('shows copy busy state and ignores a second click', async () => {
    let resolveCopy!: (ok: boolean) => void
    copyMocks.copyToClipboard.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCopy = resolve
        }),
    )
    const wrapper = mountPreview(art({ id: 'page', name: 'page.html' }), {
      hideDelete: true,
      hideVersionChip: true,
    })
    await flushPromises()
    const btn = wrapper.get('[data-testid="artifact-preview-copy"]')
    await btn.trigger('click')
    await btn.trigger('click')
    expect(copyMocks.copyToClipboard).toHaveBeenCalledTimes(1)
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('aria-busy')).toBe('true')
    expect(btn.attributes('aria-label')).toBe('复制中…')
    resolveCopy(true)
    await flushPromises()
    expect(btn.attributes('aria-label')).toBe('复制')
    wrapper.unmount()
  })
})

describe('ArtifactPreview non-HTML identity bar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactVersions.mockResolvedValue([])
    apiMocks.artifactContent.mockResolvedValue({ content: '{"a":1}' })
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('keeps kind, name, copy, and download on the JSON header', async () => {
    const research = {
      title: '调研标题',
      summary: '概述',
      questions: [{ question: 'Q1', answer: 'A1' }],
    }
    const wrapper = mountPreview(
      art({
        id: 'research',
        name: 'research.json',
        kind: 'json',
        content: JSON.stringify(research),
      }),
    )
    await flushPromises()

    const bar = wrapper.get('[data-testid="artifact-preview-identity-bar"]')
    expect(bar.text()).toContain('json')
    expect(bar.text()).toContain('调研')
    expect(bar.text()).toContain('research.json')
    expect(bar.get('[data-testid="artifact-preview-copy"]').exists()).toBe(true)
    expect(bar.get('[data-testid="artifact-preview-download-raw"]').exists()).toBe(true)
    expect(bar.get('[data-testid="artifact-preview-zoom"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview-toolbar"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview-toolbar-end"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

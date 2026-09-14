// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ArtifactPreviewProps } from './useArtifactPreview'
import type { Artifact } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  artifactVersions: vi.fn(async () => []),
  artifactVersionContent: vi.fn(),
  artifactDownloadUrl: vi.fn((id: string) => `/api/artifacts/${id}/download`),
  deleteArtifact: vi.fn(),
  publicArtifactContent: vi.fn(),
  copy: vi.fn(),
  exportStructured: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastWarn: vi.fn(),
  annotate: vi.fn(),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/lib/api/api', () => ({
  api: {
    artifactContent: mocks.artifactContent,
    artifactVersions: mocks.artifactVersions,
    artifactVersionContent: mocks.artifactVersionContent,
    artifactDownloadUrl: mocks.artifactDownloadUrl,
    deleteArtifact: mocks.deleteArtifact,
  },
}))
vi.mock('@/lib/inbox/gateShareLink', () => ({
  publicGateApi: { artifactContent: mocks.publicArtifactContent },
}))
vi.mock('@/lib/shared/copyToClipboard', () => ({ copyToClipboard: mocks.copy }))
vi.mock('@/lib/run/exportStructuredArtifact', () => ({
  exportStructuredArtifact: mocks.exportStructured,
}))
vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warn: mocks.toastWarn,
  }),
}))
vi.mock('@/lib/inbox/reviewAnnotate', () => ({
  useReviewAnnotate: () => ({ enabled: true, annotate: mocks.annotate }),
}))

import { useArtifactPreview } from './useArtifactPreview'

const artifact = (over: Partial<Artifact> = {}): Artifact =>
  ({
    id: 'a1',
    name: 'notes.md',
    kind: 'markdown',
    nodeId: 'n1',
    sizeBytes: 5,
    createdAt: '2026-01-01T00:00:00Z',
    ...over,
  }) as Artifact

function mountPreview(over: Partial<ArtifactPreviewProps> = {}) {
  let preview!: ReturnType<typeof useArtifactPreview>
  const emit = vi.fn()
  const props = reactive<ArtifactPreviewProps>({
    artifact: artifact({ content: '# hello' }),
    annotatable: true,
    ...over,
  })
  const Comp = defineComponent({
    setup() {
      preview = useArtifactPreview(props, emit as never)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.mount(document.createElement('div'))
  return { preview, props, emit, app }
}

describe('useArtifactPreview coverage', () => {
  beforeEach(() => {
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()
    mocks.artifactContent.mockResolvedValue({ content: 'loaded' })
    mocks.artifactVersions.mockResolvedValue([])
    mocks.artifactVersionContent.mockResolvedValue({
      artifactId: 'page',
      revision: 1,
      nodeId: 'n1',
      sizeBytes: 8,
      createdAt: 't1',
      content: '<h1>old</h1>',
    })
    mocks.publicArtifactContent.mockResolvedValue({ content: 'cHVibGlj' })
    mocks.artifactDownloadUrl.mockImplementation((id: string) => `/api/artifacts/${id}/download`)
    mocks.deleteArtifact.mockResolvedValue({ status: 'ok' })
    mocks.copy.mockResolvedValue(true)
    mocks.exportStructured.mockResolvedValue({ filename: 'report.png', incomplete: false })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async () => new Response(new Blob(['image']), { status: 200 })),
    )
    vi.stubGlobal('atob', (value: string) => Buffer.from(value, 'base64').toString('binary'))
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads inline and remote content, caches it, and reports genuine failures', async () => {
    const { preview, props, app } = mountPreview()
    await nextTick()
    expect(preview.activeContent.value).toBe('# hello')
    expect(preview.previewBranch.value.kind).toBe('markdown')
    await preview.loadContent(props.artifact!)
    expect(mocks.artifactContent).not.toHaveBeenCalled()

    props.artifact = artifact({ id: 'remote', content: undefined, updatedAt: 'later' })
    await flushPromises()
    expect(mocks.artifactContent).toHaveBeenCalledWith('remote', expect.objectContaining({ signal: expect.anything() }))
    expect(preview.activeContent.value).toBe('loaded')

    mocks.artifactContent.mockRejectedValueOnce(new Error('offline'))
    props.artifact = artifact({ id: 'failed', content: undefined })
    await flushPromises()
    expect(preview.loadErr.value).toContain('loadFailed')
    expect(preview.contentCache.value.failed).toBe('')
    expect(preview.loading.value).toBe(false)

    props.artifact = null
    await nextTick()
    expect(preview.previewBranch.value.kind).toBe('empty')
    expect(preview.zoom.value).toBe(false)
    expect(preview.loadErr.value).toBe('')
    app.unmount()
  })

  it('uses the public artifact endpoint and ignores abort-style failures', async () => {
    const { preview, app } = mountPreview({
      artifact: artifact({ id: 'public', name: 'public.txt', content: undefined }),
      shareToken: 'share-token',
    })
    await flushPromises()
    expect(mocks.publicArtifactContent).toHaveBeenCalledWith(
      'share-token',
      'public.txt',
      expect.any(AbortSignal),
    )
    expect(preview.activeContent.value).toBe('cHVibGlj')

    const aborted = artifact({ id: 'aborted', content: undefined })
    mocks.publicArtifactContent.mockRejectedValueOnce(new DOMException('aborted', 'AbortError'))
    await preview.loadContent(aborted, { force: true })
    expect(preview.contentCache.value.aborted).toBeUndefined()
    app.unmount()
  })

  it('copies content with success/failure feedback and a re-entry guard', async () => {
    const { preview, app } = mountPreview()
    await preview.copyContent()
    expect(mocks.copy).toHaveBeenCalledWith('# hello')
    expect(mocks.toastSuccess).toHaveBeenCalled()
    mocks.copy.mockResolvedValueOnce(false)
    await preview.copyContent()
    expect(mocks.toastError).toHaveBeenCalled()
    preview.copying.value = true
    mocks.copy.mockClear()
    await preview.copyContent()
    expect(mocks.copy).not.toHaveBeenCalled()
    app.unmount()
  })

  it('loads private and public images, retries, and handles decode/download errors', async () => {
    const { preview, props, app } = mountPreview({
      artifact: artifact({ id: 'img', name: 'screen.png', kind: 'image', content: undefined }),
    })
    await flushPromises()
    expect(fetch).toHaveBeenCalledWith('/api/artifacts/img/download', { credentials: 'include' })
    expect(preview.imageSrc.value).toBe('blob:preview')
    expect(preview.imageDownloadLoading.value).toBe(false)

    ;(fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(new Response('', { status: 500 }))
    await preview.loadImageDownload(props.artifact!)
    expect(preview.imageDownloadError.value).toBe(true)
    preview.retryImageDownload()
    await flushPromises()
    expect(fetch).toHaveBeenCalled()
    await preview.loadImageDownload(props.artifact!)
    expect(preview.imageSrc.value).toBe('blob:preview')
    preview.handleImageLoadError()
    expect(preview.imageSrc.value).toBeNull()
    expect(preview.imageDownloadError.value).toBe(true)
    preview.resetImageDownloadState()
    expect(preview.imageDownloadError.value).toBe(false)
    app.unmount()

    const pub = mountPreview({
      artifact: artifact({ id: 'pub-img', name: 'public.png', kind: 'image', content: undefined }),
      shareToken: 'token',
    })
    await flushPromises()
    expect(mocks.publicArtifactContent).toHaveBeenCalledWith('token', 'public.png')
    expect(pub.preview.imageSrc.value).toBe('blob:preview')
    pub.app.unmount()
  })

  it('annotates allowed content and blocks annotations for images or disabled state', async () => {
    const { preview, props, app } = mountPreview()
    expect(preview.canAnnotate.value).toBe(true)
    expect(preview.quoteAnnotate.value).toBe(true)
    expect(preview.blockZoomGesture.value).toBe(true)
    preview.onHtmlPick({ selector: '#button', tagName: 'BUTTON' })
    preview.onQuoteAdd({ quote: 'hello' })
    expect(mocks.annotate).toHaveBeenNthCalledWith(1, { selector: '#button', label: '#button' })
    expect(mocks.annotate).toHaveBeenNthCalledWith(2, { quote: 'hello' })
    preview.onHtmlPick({ selector: '', tagName: 'DIV' })
    expect(mocks.annotate).toHaveBeenLastCalledWith({ selector: '', label: 'DIV' })

    props.annotatable = false
    await nextTick()
    preview.stageAnnotation({ quote: 'ignored' })
    expect(mocks.annotate).toHaveBeenCalledTimes(3)
    props.artifact = artifact({ kind: 'image', name: 'x.png', content: '' })
    props.annotatable = true
    await nextTick()
    expect(preview.quoteAnnotate.value).toBe(false)
    app.unmount()
  })

  it('drives version choice and historical labels from the versions API', async () => {
    mocks.artifactVersions.mockResolvedValueOnce([
      { artifactId: 'page', revision: 1, nodeId: 'n1', sizeBytes: 8, createdAt: 't1' },
    ])
    mocks.artifactVersionContent.mockResolvedValueOnce({
      artifactId: 'page',
      revision: 1,
      nodeId: 'n1',
      sizeBytes: 8,
      createdAt: 't1',
      content: '<h1>old</h1>',
    })
    const { preview, app } = mountPreview({
      artifact: artifact({
        id: 'page',
        name: 'page.html',
        kind: 'html',
        nodeId: 'n1',
        content: '<h1>new</h1>',
        revision: 2,
      }),
    })
    await flushPromises()
    expect(preview.showVersionChip.value).toBe(true)
    expect(preview.selectedChoice.value?.latest).toBe(true)
    expect(preview.currentChipLabel.value).toContain('versionChipLatest')
    const old = preview.versionChoices.value[0]!
    await preview.selectVersion({ ...old, available: false })
    expect(preview.selectedChoice.value?.latest).toBe(true)
    await preview.selectVersion(old)
    await flushPromises()
    expect(preview.viewingHistorical.value).toBe(true)
    expect(preview.versionChipLabel(old)).toContain('versionChip')
    expect(preview.showDelete.value).toBe(false)
    app.unmount()
  })

  it('downloads live and historical artifacts through their distinct paths', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const { preview, props, app } = mountPreview()
    preview.download()
    expect(open).toHaveBeenCalledWith('/api/artifacts/a1/download', '_blank')
    props.artifact = null
    await nextTick()
    preview.download()
    expect(open).toHaveBeenCalledTimes(1)
    app.unmount()

    const historical = mountPreview({
      artifact: artifact({
        id: 'historical-page:n1:1',
        name: 'page.html#iter-1',
        kind: 'html',
        content: '<h1>old</h1>',
      }),
    })
    const click = vi.fn()
    const originalCreateElement = document.createElement.bind(document)
    const create = vi.spyOn(document, 'createElement').mockImplementation((tagName: string) =>
      tagName === 'a'
        ? ({ click, set href(_v: string) {}, set download(_v: string) {} } as unknown as HTMLAnchorElement)
        : originalCreateElement(tagName),
    )
    historical.preview.download()
    expect(click).toHaveBeenCalled()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview')
    create.mockRestore()
    historical.app.unmount()
  })

  it('exports structured artifacts from inline/zoom roots and reports all outcomes', async () => {
    const structured = artifact({
      id: 'json',
      name: 'plan.json',
      kind: 'json',
      content: JSON.stringify({ title: 'Plan', goals: [] }),
    })
    const { preview, app } = mountPreview({ artifact: structured })
    await nextTick()
    expect(preview.isStructuredPreview.value).toBe(true)
    expect(preview.showStructuredUi.value).toBe(true)
    expect(preview.exportDisabled.value).toBe(false)
    await preview.runStructuredExport('png')
    expect(mocks.toastError).toHaveBeenCalled()

    const inline = document.createElement('div')
    preview.structuredExportRootInline.value = inline
    await preview.runStructuredExport('png')
    expect(mocks.exportStructured).toHaveBeenCalledWith(inline, 'plan.json', 'png')
    expect(mocks.toastSuccess).toHaveBeenCalled()
    mocks.exportStructured.mockResolvedValueOnce({ filename: 'plan.pdf', incomplete: true })
    preview.zoom.value = true
    const zoom = document.createElement('div')
    preview.structuredExportRootZoom.value = zoom
    await preview.runStructuredExport('pdf')
    expect(mocks.exportStructured).toHaveBeenCalledWith(zoom, 'plan.json', 'pdf')
    expect(mocks.toastWarn).toHaveBeenCalled()
    mocks.exportStructured.mockRejectedValueOnce(new Error('render failed'))
    await preview.runStructuredExport('png')
    expect(mocks.toastError).toHaveBeenCalled()
    preview.downloadPng()
    await flushPromises()
    preview.downloadPdf()
    await flushPromises()

    preview.structuredMode.value = 'raw'
    expect(preview.showRawJson.value).toBe(true)
    expect(preview.structuredDoc.value).toBeTruthy()
    preview.exporting.value = true
    mocks.exportStructured.mockClear()
    await preview.runStructuredExport('png')
    expect(mocks.exportStructured).not.toHaveBeenCalled()
    app.unmount()
  })

  it('does not reload content when the artifact fingerprint is unchanged (g2.2)', async () => {
    const { preview, props, app } = mountPreview({
      artifact: artifact({ id: 'plan', name: 'plan.json', kind: 'json', content: '{"title":"A"}', updatedAt: 't1', revision: 1 }),
    })
    await flushPromises()
    mocks.artifactContent.mockClear()
    const loads = preview.contentLoadGen
    props.artifact = artifact({
      id: 'plan',
      name: 'plan.json',
      kind: 'json',
      content: '{"title":"A"}',
      updatedAt: 't1',
      revision: 1,
    })
    await flushPromises()
    expect(mocks.artifactContent).not.toHaveBeenCalled()
    expect(preview.contentLoadGen).toBe(loads)
    expect(preview.showStructuredUi.value).toBe(true)
    app.unmount()
  })

  it('opens, closes, maps, and confirms deletion with guarded and error paths', async () => {
    const { preview, emit, props, app } = mountPreview()
    expect(preview.mapDeleteError({ status: 409 })).toContain('RunNotEnded')
    expect(preview.mapDeleteError({ status: 404 })).toContain('NotFound')
    expect(preview.mapDeleteError(new Error('denied'))).toBe('denied')
    expect(preview.mapDeleteError(null)).toContain('Generic')
    preview.openDeleteConfirm()
    expect(preview.showDeleteConfirm.value).toBe(true)
    preview.closeDeleteConfirm()
    expect(preview.showDeleteConfirm.value).toBe(false)
    preview.openDeleteConfirm()
    await preview.confirmDelete()
    expect(mocks.deleteArtifact).toHaveBeenCalledWith('a1')
    expect(emit).toHaveBeenCalledWith('deleted', 'a1')
    expect(preview.showDeleteConfirm.value).toBe(false)

    mocks.deleteArtifact.mockRejectedValueOnce(Object.assign(new Error('conflict'), { status: 409 }))
    await preview.confirmDelete()
    expect(preview.deleteError.value).toContain('RunNotEnded')
    preview.deleting.value = true
    preview.closeDeleteConfirm()
    mocks.deleteArtifact.mockClear()
    await preview.confirmDelete()
    expect(mocks.deleteArtifact).not.toHaveBeenCalled()
    preview.deleting.value = false
    props.artifact = null
    await nextTick()
    await preview.confirmDelete()
    expect(mocks.deleteArtifact).not.toHaveBeenCalled()
    app.unmount()
  })
})

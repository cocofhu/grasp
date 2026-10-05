// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  artifactDownloadUrl: vi.fn((id: string) => `http://test/api/artifacts/${id}/download`),
  deleteArtifact: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      artifactContent: apiMocks.artifactContent,
      artifactDownloadUrl: apiMocks.artifactDownloadUrl,
      deleteArtifact: apiMocks.deleteArtifact,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

import ArtifactPreview from './ArtifactPreview.vue'

const imageArtifact = {
  id: 'art-img-1',
  name: 'screenshot-ui-test.png',
  kind: 'image' as const,
  nodeId: 'test',
  runId: 'run-1',
  workflowName: '测试工作流',
  sizeBytes: 1024,
  createdAt: '2026-07-21T09:30:00Z',
}

function mountPreview() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ArtifactPreview, {
    props: { artifact: imageArtifact },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        HtmlPreview: true,
        StructuredArtifactView: true,
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

describe('ArtifactPreview image preview UI', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: true,
        blob: async () => new Blob(['png-bytes'], { type: 'image/png' }),
      })),
    )
    apiMocks.artifactContent.mockResolvedValue({
      ...imageArtifact,
      content: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ',
    })
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock-image')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders image preview instead of base64 markdown text', async () => {
    const wrapper = mountPreview()
    await flushPromises()
    expect(wrapper.text()).not.toContain('iVBORw0KGgo')
    expect(wrapper.find('[data-testid="artifact-preview-image-wrap"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-image-wrap"] img').attributes('src')).toBe(
      'blob:mock-image',
    )
    wrapper.unmount()
  })

  it('applies p-3 padding on inline image-wrap success canvas', async () => {
    const wrapper = mountPreview()
    await flushPromises()
    const wrap = wrapper.find('[data-testid="artifact-preview-image-wrap"]')
    expect(wrap.exists()).toBe(true)
    expect(wrap.classes()).toContain('p-3')
    // loading/error branches must not be the padded canvas
    expect(wrapper.find('[data-testid="artifact-preview-image-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-image-error"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows loading until download completes', async () => {
    let resolveFetch: (value: unknown) => void = () => {}
    vi.stubGlobal(
      'fetch',
      vi.fn(
        () =>
          new Promise((resolve) => {
            resolveFetch = resolve
          }),
      ),
    )
    const wrapper = mountPreview()
    expect(wrapper.find('[data-testid="artifact-preview-image-loading"]').exists()).toBe(true)
    resolveFetch({
      ok: true,
      blob: async () => new Blob(['png-bytes'], { type: 'image/png' }),
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-image-loading"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows download error with retry and recovers on retry', async () => {
    const fetchMock = vi.fn(async () => ({ ok: false, blob: async () => new Blob([]) }))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountPreview()
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-image-error"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('图片加载失败')
    fetchMock.mockResolvedValueOnce({
      ok: true,
      blob: async () => new Blob(['png-bytes'], { type: 'image/png' }),
    })
    await wrapper.find('[data-testid="artifact-preview-image-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-image-wrap"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows generic content error even when download succeeds', async () => {
    apiMocks.artifactContent.mockRejectedValueOnce(new Error('content unavailable'))
    const wrapper = mountPreview()
    await flushPromises()
    expect(wrapper.text()).toContain('产物加载失败')
    expect(wrapper.text()).not.toContain('content unavailable')
    expect(wrapper.find('[data-testid="artifact-preview-image-wrap"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows download error with retry when img decode fails', async () => {
    const wrapper = mountPreview()
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-image-wrap"] img').exists()).toBe(true)
    await wrapper.find('[data-testid="artifact-preview-image-wrap"] img').trigger('error')
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-image-error"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('图片加载失败')
    wrapper.unmount()
  })

  it('shows image in zoom modal with same blob src', async () => {
    const wrapper = mountPreview()
    await flushPromises()
    await wrapper.find('button[title="放大查看"]').trigger('click')
    await flushPromises()
    const zoom = wrapper.find('[data-testid="artifact-preview-zoom-image"]')
    expect(zoom.exists()).toBe(true)
    expect(zoom.find('img').attributes('src')).toBe('blob:mock-image')
    wrapper.unmount()
  })

  it('applies p-3 padding on zoom success image container', async () => {
    const wrapper = mountPreview()
    await flushPromises()
    await wrapper.find('button[title="放大查看"]').trigger('click')
    await flushPromises()
    const zoom = wrapper.find('[data-testid="artifact-preview-zoom-image"]')
    expect(zoom.exists()).toBe(true)
    const successContainer = zoom.find('img').element.parentElement
    expect(successContainer).toBeTruthy()
    expect(successContainer!.classList.contains('p-3')).toBe(true)
    wrapper.unmount()
  })
})

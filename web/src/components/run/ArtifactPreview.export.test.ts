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

const toastMocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warn: vi.fn(),
}))

const exportMocks = vi.hoisted(() => ({
  exportStructuredArtifact: vi.fn(),
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
  useToast: () => toastMocks,
}))

vi.mock('@/lib/run/exportStructuredArtifact', async () => {
  const actual = await vi.importActual<typeof import('@/lib/run/exportStructuredArtifact')>(
    '@/lib/run/exportStructuredArtifact',
  )
  return {
    ...actual,
    exportStructuredArtifact: exportMocks.exportStructuredArtifact,
  }
})

import ArtifactPreview from './ArtifactPreview.vue'

const researchDoc = {
  title: '调研标题',
  summary: '概述',
  questions: [{ question: 'Q1', answer: 'A1' }],
}

const structuredArtifact = {
  id: 'art-structured-1',
  name: 'clarified_requirement.json',
  kind: 'json' as const,
  nodeId: 'react',
  runId: 'run-1',
  workflowName: '测试工作流',
  sizeBytes: 2048,
  createdAt: '2026-07-26T01:00:00Z',
}

const plainJsonArtifact = {
  ...structuredArtifact,
  id: 'art-json-1',
  name: 'notes.json',
}

function mountPreview(artifact: typeof structuredArtifact) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ArtifactPreview, {
    props: { artifact },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        HtmlPreview: true,
        StructuredArtifactView: {
          template: '<div data-testid="structured-stub">structured</div>',
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

describe('ArtifactPreview structured export UI', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactContent.mockResolvedValue({
      ...structuredArtifact,
      content: JSON.stringify(researchDoc),
    })
    exportMocks.exportStructuredArtifact.mockResolvedValue({
      filename: 'clarified_requirement.png',
      incomplete: false,
    })
    vi.spyOn(window, 'open').mockImplementation(() => null)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows download image/PDF text buttons for structured preview', async () => {
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    const png = wrapper.find('[data-testid="artifact-preview-download-png"]')
    const pdf = wrapper.find('[data-testid="artifact-preview-download-pdf"]')
    expect(png.exists()).toBe(true)
    expect(pdf.exists()).toBe(true)
    expect(png.text()).toContain('下载图片')
    expect(pdf.text()).toContain('下载 PDF')
    expect(wrapper.find('[data-testid="structured-artifact-export-root"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not show style export buttons for ordinary JSON', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      ...plainJsonArtifact,
      content: '{"a":1}',
    })
    const wrapper = mountPreview(plainJsonArtifact)
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-download-png"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-download-pdf"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-download-raw"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('marks raw download tooltip as original JSON', async () => {
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-download-raw"]').attributes('title')).toBe(
      '下载原始 JSON',
    )
    wrapper.unmount()
  })

  it('triggers PNG export with artifact name and success toast', async () => {
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    await wrapper.find('[data-testid="artifact-preview-download-png"]').trigger('click')
    await flushPromises()
    expect(exportMocks.exportStructuredArtifact).toHaveBeenCalledTimes(1)
    const [el, name, format] = exportMocks.exportStructuredArtifact.mock.calls[0]
    expect(name).toBe('clarified_requirement.json')
    expect(format).toBe('png')
    expect(el).toBeTruthy()
    expect(toastMocks.success).toHaveBeenCalledWith('已生成 clarified_requirement.png')
    wrapper.unmount()
  })

  it('triggers PDF export and warns when incomplete', async () => {
    exportMocks.exportStructuredArtifact.mockResolvedValue({
      filename: 'clarified_requirement.pdf',
      incomplete: true,
    })
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    await wrapper.find('[data-testid="artifact-preview-download-pdf"]').trigger('click')
    await flushPromises()
    expect(exportMocks.exportStructuredArtifact.mock.calls[0][2]).toBe('pdf')
    expect(toastMocks.warn).toHaveBeenCalledWith(
      '已生成 clarified_requirement.pdf，但部分截图可能未加载完整',
    )
    wrapper.unmount()
  })

  it('toasts error when export throws and keeps raw download available', async () => {
    exportMocks.exportStructuredArtifact.mockRejectedValue(new Error('capture failed'))
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    await wrapper.find('[data-testid="artifact-preview-download-png"]').trigger('click')
    await flushPromises()
    expect(toastMocks.error).toHaveBeenCalledWith('导出失败，请重试')
    await wrapper.find('[data-testid="artifact-preview-download-raw"]').trigger('click')
    expect(window.open).toHaveBeenCalledWith(
      'http://test/api/artifacts/art-structured-1/download',
      '_blank',
    )
    wrapper.unmount()
  })

  it('keeps inline ancestor p-4 and export root without resident padding (g3.3)', async () => {
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    const exportRoot = wrapper.find('[data-testid="structured-artifact-export-root"]')
    expect(exportRoot.exists()).toBe(true)
    expect(exportRoot.classes()).toContain('structured-artifact-export-root')
    expect(exportRoot.classes()).not.toContain('p-3')
    expect(exportRoot.classes()).not.toContain('p-4')
    expect(exportRoot.classes()).not.toContain('p-5')
    const rootEl = exportRoot.element as HTMLElement
    expect(rootEl.style.padding).toBe('')
    expect(rootEl.getAttribute('style') ?? '').not.toMatch(/padding/i)

    const scrollParent = exportRoot.element.parentElement
    expect(scrollParent).toBeTruthy()
    expect(scrollParent!.classList.contains('p-4')).toBe(true)
    expect(scrollParent!.classList.contains('scroll-area')).toBe(true)
    wrapper.unmount()
  })

  it('keeps zoom export root without resident padding (g3.3)', async () => {
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    const enlarge = wrapper.findAll('button').find((b) => b.attributes('title') === '放大查看')
    expect(enlarge).toBeTruthy()
    await enlarge!.trigger('click')
    await flushPromises()
    const zoomRoot = wrapper.find('[data-testid="structured-artifact-export-root-zoom"]')
    expect(zoomRoot.exists()).toBe(true)
    expect(zoomRoot.classes()).toContain('structured-artifact-export-root')
    expect(zoomRoot.classes()).not.toContain('p-3')
    expect(zoomRoot.classes()).not.toContain('p-4')
    expect(zoomRoot.classes()).not.toContain('p-5')
    const zoomEl = zoomRoot.element as HTMLElement
    expect(zoomEl.style.padding).toBe('')
    wrapper.unmount()
  })

  it('shows zoom footer export buttons for structured preview', async () => {
    const wrapper = mountPreview(structuredArtifact)
    await flushPromises()
    // open zoom via enlarge (first toolbar button without testid is expand)
    const buttons = wrapper.findAll('button')
    const enlarge = buttons.find((b) => b.attributes('title') === '放大查看')
    expect(enlarge).toBeTruthy()
    await enlarge!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="zoom-modal"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-zoom-download-png"]').text()).toContain(
      '下载图片',
    )
    expect(wrapper.find('[data-testid="artifact-preview-zoom-download-pdf"]').text()).toContain(
      '下载 PDF',
    )
    expect(wrapper.find('[data-testid="artifact-preview-zoom-download-raw"]').attributes('title')).toBe(
      '下载原始 JSON',
    )
    wrapper.unmount()
  })
})

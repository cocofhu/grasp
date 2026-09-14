// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact } from '@/lib/shared/types'
import ArtifactPreview from './ArtifactPreview.vue'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  artifactVersions: vi.fn(),
  artifactVersionContent: vi.fn(),
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

function art(partial: Partial<Artifact> & Pick<Artifact, 'id' | 'name'>): Artifact {
  return {
    kind: 'html',
    nodeId: 'visual_1',
    runId: 'run-1',
    sizeBytes: 10,
    createdAt: '2026-08-10T12:00:00Z',
    revision: 1,
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
    props: { artifact, scope: 'platform', ...extra },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        Teleport: false,
        Icon: true,
        HtmlPreview: {
          props: ['html', 'inspectable'],
          template:
            '<div data-testid="html-preview-stub" :data-inspectable="inspectable ? \'1\' : \'0\'">{{ html }}</div>',
        },
        StructuredArtifactView: true,
        AppModal: {
          props: ['open', 'title'],
          template: '<div v-if="open" data-testid="zoom-modal"><slot /><slot name="footer" /></div>',
        },
        AppButton: {
          emits: ['click'],
          template: '<button v-bind="$attrs" @click="$emit(\'click\')"><slot /></button>',
        },
        SelectionAddToChat: true,
        RefreshStrip: true,
        HardLoadLayer: true,
      },
    },
  })
}

describe('ArtifactPreview version switch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.artifactContent.mockImplementation(async (id: string) => ({
      id,
      name: 'page.html',
      kind: 'html',
      content: '<p>new</p>',
    }))
    apiMocks.artifactVersions.mockResolvedValue([])
    apiMocks.artifactVersionContent.mockResolvedValue({
      artifactId: 'live',
      revision: 1,
      nodeId: 'visual_1',
      sizeBytes: 8,
      createdAt: 't1',
      content: '<p>old</p>',
    })
  })

  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('shows version chip for ≥2 revisions and switches historical content', async () => {
    apiMocks.artifactVersions.mockResolvedValue([
      { artifactId: 'live', revision: 1, nodeId: 'visual_1', sizeBytes: 8, createdAt: 't1' },
    ])
    const live = art({ id: 'live', name: 'page.html', content: '<p>new</p>', revision: 2 })
    const wrapper = mountPreview(live)
    await flushPromises()

    expect(wrapper.get('[data-testid="artifact-preview-version-chip-btn"]').text()).toContain('v2 · 最新')
    expect(wrapper.get('[data-testid="html-preview-stub"]').text()).toBe('<p>new</p>')

    await wrapper.get('[data-testid="artifact-preview-version-chip-btn"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="artifact-preview-version-option-v1"]')?.textContent).toBe('v1')
    expect(document.querySelector('[data-testid="artifact-preview-version-option-v2"]')?.textContent).toContain(
      'v2 · 最新',
    )

    ;(document.querySelector('[data-testid="artifact-preview-version-option-v1"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(apiMocks.artifactVersionContent).toHaveBeenCalledWith('live', 1, undefined)
    expect(wrapper.get('[data-testid="html-preview-stub"]').text()).toBe('<p>old</p>')
    expect(wrapper.find('[data-testid="artifact-preview-historical-readonly"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="artifact-preview-delete"]').exists()).toBe(false)

    await wrapper.get('[data-testid="artifact-preview-version-chip-btn"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-testid="artifact-preview-version-option-v2"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(wrapper.get('[data-testid="html-preview-stub"]').text()).toBe('<p>new</p>')
    expect(wrapper.find('[data-testid="artifact-preview-historical-readonly"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="artifact-preview-delete"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hides chip for a single revision', async () => {
    const live = art({ id: 'live', name: 'page.html', content: '<p>only</p>', revision: 1 })
    const wrapper = mountPreview(live)
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-version-chip"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="html-preview-stub"]').text()).toBe('<p>only</p>')
    wrapper.unmount()
  })

  it('shows the version chip for plan.json when archived revisions exist', async () => {
    apiMocks.artifactVersions.mockResolvedValue([
      { artifactId: 'j1', revision: 1, nodeId: 'plan', sizeBytes: 8, createdAt: 't1' },
    ])
    apiMocks.artifactVersionContent.mockResolvedValue({
      artifactId: 'j1',
      revision: 1,
      nodeId: 'plan',
      sizeBytes: 8,
      createdAt: 't1',
      content: JSON.stringify({ title: '旧计划', goals: [] }),
    })
    const json = art({
      id: 'j1',
      name: 'plan.json',
      kind: 'json',
      revision: 2,
      content: JSON.stringify({ title: '计划', goals: [] }),
    })
    const wrapper = mountPreview(json)
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-version-chip"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hides the chip when version list fetch fails', async () => {
    apiMocks.artifactVersions.mockRejectedValue(new Error('offline'))
    const live = art({ id: 'live', name: 'page.html', content: '<p>fallback</p>', revision: 3 })
    const wrapper = mountPreview(live)
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview-version-chip"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="html-preview-stub"]').text()).toBe('<p>fallback</p>')
    wrapper.unmount()
  })
})

// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import GateProductEditor from './GateProductEditor.vue'

const apiMocks = vi.hoisted(() => ({
  saveGateArtifact: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      saveGateArtifact: apiMocks.saveGateArtifact,
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
    inspectable: Boolean,
    enlargeable: { type: Boolean, default: true },
    fillParent: Boolean,
    mode: String,
    modalTitle: String,
  },
  emits: ['pick'],
  template:
    '<div data-testid="html-preview" :data-inspectable="inspectable ? \'1\' : \'0\'" :data-enlargeable="enlargeable === false ? \'0\' : \'1\'" :data-fill-parent="fillParent ? \'1\' : \'0\'" :data-mode="mode || \'\'" :data-modal-title="modalTitle || \'\'" />',
})

const researchContent = JSON.stringify({ summary: '调研摘要', findings: [{ title: '发现 1' }] }, null, 2)

function mountEditor(
  canEdit = true,
  overrides: Record<string, unknown> = {},
) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(GateProductEditor, {
    props: {
      runId: 'run-1',
      gateNodeId: 'gate-1',
      products: [{ name: 'research.json', kind: 'json' as const }],
      savedContent: { 'research.json': researchContent },
      savedMeta: { 'research.json': { etag: 'W/"1"' } },
      canEdit,
      ...overrides,
    },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        StructuredArtifactView: StructuredStub,
        HtmlPreview: HtmlPreviewStub,
      },
    },
  })
}

const sampleHtml = '<!doctype html><html><body><p>hi</p></body></html>'

function mountHtmlEditor(overrides: Record<string, unknown> = {}) {
  return mountEditor(true, {
    products: [{ name: 'page.html', kind: 'html' as const }],
    savedContent: { 'page.html': sampleHtml },
    savedMeta: { 'page.html': { etag: 'W/"h1"' } },
    ...overrides,
  })
}

describe('GateProductEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.saveGateArtifact.mockResolvedValue({
      id: 'a1',
      name: 'research.json',
      kind: 'json',
      sizeBytes: 50,
      updatedAt: '2026-07-18T01:00:00Z',
      etag: 'W/"2"',
      content: researchContent,
    })
  })

  it('renders preview mode with structured view', async () => {
    const wrapper = mountEditor()
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('enters edit mode and saves draft', async () => {
    const wrapper = mountEditor()
    await flushPromises()
    const editBtn = wrapper.findAll('button').find((b) => b.text() === '编辑')
    expect(editBtn).toBeTruthy()
    await editBtn!.trigger('click')
    await flushPromises()
    const jsonBtn = wrapper.findAll('button').find((b) => b.text().includes('原始 JSON'))
    if (jsonBtn) await jsonBtn.trigger('click')
    await flushPromises()
    const ta = wrapper.find('[data-testid="gate-artifact-textarea"]')
    if (ta.exists()) {
      await ta.setValue('{"summary":"改动了","findings":[{"title":"发现 1"}]}')
      await wrapper.find('[data-testid="gate-artifact-save"]').trigger('click')
      await flushPromises()
      expect(apiMocks.saveGateArtifact).toHaveBeenCalled()
      expect(wrapper.emitted('saved')).toBeTruthy()
    }
    wrapper.unmount()
  })

  it('hides edit controls when canEdit is false', async () => {
    const wrapper = mountEditor(false)
    await flushPromises()
    const editBtn = wrapper.findAll('button').find((b) => b.text() === '编辑')
    expect(editBtn).toBeTruthy()
    expect((editBtn!.element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('mounts HtmlPreview only when saved HTML is non-empty', async () => {
    const wrapper = mountHtmlEditor()
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    wrapper.unmount()

    const empty = mountHtmlEditor({ savedContent: { 'page.html': '  \n' } })
    await flushPromises()
    expect(empty.find('[data-testid="gate-preview-empty"]').exists()).toBe(true)
    expect(empty.find('[data-testid="html-preview"]').exists()).toBe(false)
    empty.unmount()
  })

  it('desktop fillParent preview path forwards enlargeable + modalTitle (main product enlarge)', async () => {
    const wrapper = mountHtmlEditor({ fillParent: true, enlargeable: true })
    await flushPromises()
    const preview = wrapper.find('[data-testid="html-preview"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('data-fill-parent')).toBe('1')
    expect(preview.attributes('data-mode')).toBe('inline')
    expect(preview.attributes('data-enlargeable')).toBe('1')
    expect(preview.attributes('data-modal-title')).toBe('page.html')

    // Edit branch must hide HtmlPreview (and thus the main enlarge entry).
    await wrapper.find('[data-testid="gate-mode-edit"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-artifact-textarea"]').exists()).toBe(true)

    await wrapper.find('[data-testid="gate-mode-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-enlargeable')).toBe('1')
    wrapper.unmount()
  })

  it('forwards enlargeable=false for mobile strategy', async () => {
    const wrapper = mountHtmlEditor({ fillParent: true, enlargeable: false })
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-enlargeable')).toBe('0')
    wrapper.unmount()
  })

  it('locks edit tab while contentLoading and shows loading panel', async () => {
    const wrapper = mountHtmlEditor({ contentLoading: true })
    await flushPromises()
    const editBtn = wrapper.find('[data-testid="gate-mode-edit"]')
    expect((editBtn.element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('[data-testid="gate-preview-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    await editBtn.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-artifact-textarea"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows error panel with retry emit', async () => {
    const wrapper = mountHtmlEditor({
      loadError: 'page.html: boom',
      savedContent: { 'page.html': '' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-preview-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    await wrapper.find('[data-testid="gate-preview-retry"]').trigger('click')
    expect(wrapper.emitted('retry-load')).toBeTruthy()
    wrapper.unmount()
  })

  it('round-trips edit↔preview with non-empty HTML still mounted', async () => {
    const wrapper = mountHtmlEditor()
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    await wrapper.find('[data-testid="gate-mode-edit"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-artifact-textarea"]').exists()).toBe(true)
    await wrapper.find('[data-testid="gate-mode-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps edit draft/mode when artifacts array reference changes without identity change', async () => {
    const art = {
      id: 'a-html',
      name: 'page.html',
      kind: 'html' as const,
      nodeId: 'visual',
      runId: 'run-1',
      workflowName: 'w',
      sizeBytes: sampleHtml.length,
      createdAt: '2026-07-18T00:00:00Z',
      updatedAt: '2026-07-18T00:00:00Z',
      etag: 'W/"h1"',
    }
    const wrapper = mountHtmlEditor({ artifacts: [art] })
    await flushPromises()
    await wrapper.find('[data-testid="gate-mode-edit"]').trigger('click')
    await flushPromises()
    const ta = wrapper.find('[data-testid="gate-artifact-textarea"]')
    expect(ta.exists()).toBe(true)
    await ta.setValue('<!doctype html><html><body><p>drafting</p></body></html>')
    await flushPromises()

    // softRefresh-style: new array / bumped updatedAt, same id/etag/size
    await wrapper.setProps({
      artifacts: [
        {
          ...art,
          updatedAt: '2026-07-18T01:00:00Z',
        },
      ],
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-artifact-textarea"]').exists()).toBe(true)
    expect(
      (wrapper.find('[data-testid="gate-artifact-textarea"]').element as HTMLTextAreaElement).value,
    ).toContain('drafting')
    wrapper.unmount()
  })

  it('shows readonly badge and disables edit/save for image products', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        blob: async () => new Blob(['img'], { type: 'image/png' }),
      }),
    )
    const wrapper = mount(GateProductEditor, {
      props: {
        runId: 'run-1',
        gateNodeId: 'gate-1',
        products: [
          { name: 'research.json', kind: 'json' as const, readonly: false },
          { name: 'shot.png', kind: 'image' as const, readonly: true },
        ],
        savedContent: { 'research.json': researchContent, 'shot.png': '' },
        savedMeta: {},
        artifacts: [
          {
            id: 'img1',
            name: 'shot.png',
            kind: 'image',
            nodeId: 'test',
            runId: 'run-1',
            workflowName: 'w',
            sizeBytes: 3,
            createdAt: '',
          },
        ],
        canEdit: true,
        excludedNames: ['extra.md'],
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          StructuredArtifactView: StructuredStub,
          HtmlPreview: HtmlPreviewStub,
        },
      },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-readonly-badge"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-excluded-produces"]').text()).toContain('extra.md')

    const imgTab = wrapper.findAll('[role="tab"]').find((b) => b.text().includes('shot.png'))
    expect(imgTab).toBeTruthy()
    await imgTab!.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-readonly-image"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-artifact-save"]').exists()).toBe(false)
    const editBtn = wrapper.find('[data-testid="gate-mode-edit"]')
    expect((editBtn.element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('[data-testid="gate-artifact-textarea"]').exists()).toBe(false)

    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  it('allows HTML source edit for page.html', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(GateProductEditor, {
      props: {
        runId: 'run-1',
        gateNodeId: 'gate-1',
        products: [{ name: 'page.html', kind: 'html' as const, readonly: false }],
        savedContent: { 'page.html': '<html><body>v1</body></html>' },
        savedMeta: { 'page.html': { etag: 'W/"1"' } },
        canEdit: true,
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          StructuredArtifactView: StructuredStub,
          HtmlPreview: HtmlPreviewStub,
        },
      },
    })
    await flushPromises()
    await wrapper.find('[data-testid="gate-mode-edit"]').trigger('click')
    await flushPromises()
    const ta = wrapper.find('[data-testid="gate-artifact-textarea"]')
    expect(ta.exists()).toBe(true)
    await ta.setValue('<html><body>v2</body></html>')
    apiMocks.saveGateArtifact.mockResolvedValue({
      id: 'a1',
      name: 'page.html',
      kind: 'html',
      sizeBytes: 30,
      updatedAt: '2026-07-18T01:00:00Z',
      etag: 'W/"2"',
      content: '<html><body>v2</body></html>',
    })
    await wrapper.find('[data-testid="gate-artifact-save"]').trigger('click')
    await flushPromises()
    expect(apiMocks.saveGateArtifact).toHaveBeenCalledWith(
      'run-1',
      'gate-1',
      'page.html',
      '<html><body>v2</body></html>',
      'W/"1"',
    )
    wrapper.unmount()
  })

  it('forwards HtmlPreview pick when inspectable', async () => {
    const wrapper = mountHtmlEditor({ inspectable: true })
    await flushPromises()
    const preview = wrapper.find('[data-testid="html-preview"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('data-inspectable')).toBe('1')

    const htmlComp = wrapper.findComponent({ name: 'HtmlPreview' })
    await htmlComp.vm.$emit('pick', {
      selector: '#cta',
      tagName: 'button',
      imageDataUrl: 'data:image/png;base64,aaa',
    })
    await flushPromises()
    expect(wrapper.emitted('pick')?.[0]?.[0]).toEqual({
      selector: '#cta',
      tagName: 'button',
      imageDataUrl: 'data:image/png;base64,aaa',
    })
    wrapper.unmount()
  })

  it('does not enable inspect by default', async () => {
    const wrapper = mountHtmlEditor()
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-inspectable')).toBe('0')
    wrapper.unmount()
  })

  it('keeps research.json default form mode and allows save', async () => {
    const wrapper = mountEditor()
    await flushPromises()
    await wrapper.find('[data-testid="gate-mode-edit"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-struct-form"]').classes()).toContain('bg-overlay')
    expect(wrapper.find('[data-testid="gate-struct-form-pane"]').exists()).toBe(true)

    const title = wrapper.find('[data-testid="gate-form-title"]')
    await title.setValue('调研标题')
    await flushPromises()
    await wrapper.find('[data-testid="gate-artifact-save"]').trigger('click')
    await flushPromises()
    expect(apiMocks.saveGateArtifact).toHaveBeenCalled()
    wrapper.unmount()
  })
})

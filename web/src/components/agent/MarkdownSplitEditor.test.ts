// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import MarkdownSplitEditor from './MarkdownSplitEditor.vue'

vi.mock('@/lib/shared/markdown', () => ({
  renderMarkdown: (body: string) => `<p>${body}</p>`,
}))

const layoutSpy = vi.fn()

const CodeEditorStub = {
  name: 'CodeEditor',
  props: ['modelValue', 'language', 'minimap', 'readonly'],
  emits: ['update:modelValue', 'scroll', 'ready'],
  template: '<div data-testid="code-editor-stub" />',
  methods: {
    getEditor() {
      return { layout: layoutSpy }
    },
  },
}

const SPLIT_KEY = 'agent-md-split-ratio'
const VIEW_MODE_KEY = 'agent-md-view-mode'

const md = `---
description: demo
alwaysApply: false
---

# Hello
`

function mountEditor(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...pages } },
  })
  return mount(MarkdownSplitEditor, {
    props: {
      modelValue: md,
      filePath: 'rules/demo.md',
      variant: 'split',
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, CodeEditor: CodeEditorStub },
    },
    attachTo: document.body,
  })
}

describe('MarkdownSplitEditor view modes', () => {
  beforeEach(() => {
    localStorage.clear()
    layoutSpy.mockClear()
  })

  afterEach(() => {
    localStorage.clear()
  })

  const has = (w: ReturnType<typeof mountEditor>, id: string) => w.find(`[data-testid="${id}"]`).exists()

  it('defaults to single-column editing without minimap', async () => {
    const wrapper = mountEditor()
    await flushPromises()
    expect(has(wrapper, 'md-preview-panel')).toBe(false)
    expect(has(wrapper, 'md-preview-sash')).toBe(false)
    expect(wrapper.get('[data-testid="md-view-edit"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.findComponent({ name: 'CodeEditor' }).props('minimap')).toBe(false)
    expect(localStorage.getItem(VIEW_MODE_KEY)).toBeNull()
    wrapper.unmount()
  })

  it('switches between split, preview and edit and persists the choice', async () => {
    const wrapper = mountEditor()
    await flushPromises()
    await wrapper.get('[data-testid="md-view-split"]').trigger('click')
    expect(has(wrapper, 'md-preview-panel')).toBe(true)
    expect(has(wrapper, 'md-preview-sash')).toBe(true)
    expect(localStorage.getItem(VIEW_MODE_KEY)).toBe('split')
    expect(layoutSpy).toHaveBeenCalled()

    await wrapper.get('[data-testid="md-view-preview"]').trigger('click')
    expect(has(wrapper, 'md-preview-panel')).toBe(true)
    expect(has(wrapper, 'md-preview-sash')).toBe(false)
    expect((wrapper.find('[data-testid="code-editor-stub"]').element.parentElement as HTMLElement).style.display).toBe('none')
    expect(localStorage.getItem(VIEW_MODE_KEY)).toBe('preview')

    await wrapper.get('[data-testid="md-view-edit"]').trigger('click')
    expect(has(wrapper, 'md-preview-panel')).toBe(false)
    expect(localStorage.getItem(VIEW_MODE_KEY)).toBe('edit')
    wrapper.unmount()
  })

  it('loads the stored mode and falls back to edit when corrupted', async () => {
    localStorage.setItem(VIEW_MODE_KEY, 'preview')
    const stored = mountEditor()
    await flushPromises()
    expect(has(stored, 'md-preview-panel')).toBe(true)
    stored.unmount()

    localStorage.setItem(VIEW_MODE_KEY, 'sideways')
    const fallback = mountEditor()
    await flushPromises()
    expect(has(fallback, 'md-preview-panel')).toBe(false)
    fallback.unmount()
  })

  it('falls back to edit when localStorage throws', async () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const wrapper = mountEditor()
    await flushPromises()
    expect(has(wrapper, 'md-preview-panel')).toBe(false)
    await wrapper.get('[data-testid="md-view-preview"]').trigger('click')
    expect(has(wrapper, 'md-preview-panel')).toBe(true)
    get.mockRestore()
    set.mockRestore()
    wrapper.unmount()
  })

  it('keeps the mode when filePath changes', async () => {
    const wrapper = mountEditor()
    await flushPromises()
    await wrapper.get('[data-testid="md-view-split"]').trigger('click')
    await wrapper.setProps({ filePath: 'rules/other.md' })
    await flushPromises()
    expect(has(wrapper, 'md-preview-sash')).toBe(true)
    wrapper.unmount()
  })

  it('hides the in-pane toggle when the host owns the toolbar', async () => {
    const wrapper = mountEditor({ externalToolbar: true })
    await flushPromises()
    expect(has(wrapper, 'md-view-toggle')).toBe(false)
    ;(wrapper.vm as any).setViewMode('split')
    await flushPromises()
    expect(has(wrapper, 'md-preview-panel')).toBe(true)
    wrapper.unmount()
  })

  it('restores prior split ratio after leaving split without rewriting it', async () => {
    localStorage.setItem(SPLIT_KEY, '0.4')
    localStorage.setItem(VIEW_MODE_KEY, 'split')
    const wrapper = mountEditor()
    await flushPromises()

    const editCol = () => wrapper.findAll('.flex.h-full.min-h-0.min-w-0.flex-col')[0]
    expect(editCol().attributes('style')).toContain('40%')

    await wrapper.get('[data-testid="md-view-edit"]').trigger('click')
    await flushPromises()
    expect(localStorage.getItem(SPLIT_KEY)).toBe('0.4')
    const editStyle = editCol().attributes('style') || ''
    expect(
      /flex:\s*1\s+1\s+auto/.test(editStyle)
      || (editStyle.includes('flex-grow: 1') && editStyle.includes('flex-basis: auto')),
    ).toBe(true)

    await wrapper.get('[data-testid="md-view-split"]').trigger('click')
    await flushPromises()
    expect(editCol().attributes('style')).toContain('40%')
    expect(localStorage.getItem(SPLIT_KEY)).toBe('0.4')
    wrapper.unmount()
  })

  it('dragging sash to ratio extremes does not auto-collapse', async () => {
    localStorage.setItem(VIEW_MODE_KEY, 'split')
    const wrapper = mountEditor()
    await flushPromises()

    const splitBody = wrapper.find('div.flex.min-h-0.flex-1')
    const el = splitBody.element as HTMLElement
    vi.spyOn(el, 'getBoundingClientRect').mockReturnValue({
      left: 0,
      width: 1000,
      top: 0,
      right: 1000,
      bottom: 400,
      height: 400,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    })

    await wrapper.get('[data-testid="md-preview-sash"]').trigger('mousedown', { clientX: 500 })
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 100 })) // → 0.10 → clamp 0.28
    document.dispatchEvent(new MouseEvent('mouseup'))
    await flushPromises()
    expect(wrapper.find('[data-testid="md-preview-panel"]').exists()).toBe(true)
    expect(localStorage.getItem(SPLIT_KEY)).toBe('0.28')

    await wrapper.get('[data-testid="md-preview-sash"]').trigger('mousedown', { clientX: 280 })
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 900 })) // → 0.90 → clamp 0.72
    document.dispatchEvent(new MouseEvent('mouseup'))
    await flushPromises()
    expect(wrapper.find('[data-testid="md-preview-panel"]').exists()).toBe(true)
    expect(localStorage.getItem(SPLIT_KEY)).toBe('0.72')
    wrapper.unmount()
  })

  it('stack variant has no desktop view toggle or sash', async () => {
    const wrapper = mountEditor({ variant: 'stack' })
    await flushPromises()
    expect(wrapper.find('[data-testid="md-view-toggle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="md-preview-sash"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('编辑')
    expect(wrapper.text()).toContain('预览')
    const tabs = wrapper.findAll('[role="tab"]')
    const edit = tabs.find((tab) => tab.text().includes('编辑'))
    const preview = tabs.find((tab) => tab.text().includes('预览'))
    expect(edit?.classes()).toContain('text-txt')
    expect(edit?.classes()).not.toContain('hover:text-txt2')
    expect(preview?.classes()).toEqual(expect.arrayContaining(['text-txt3', 'hover:text-txt2']))
    expect(preview?.classes()).not.toContain('hover:text-txt')
    wrapper.unmount()
  })
})

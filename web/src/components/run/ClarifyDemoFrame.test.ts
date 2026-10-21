// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import ClarifyDemoFrame from './ClarifyDemoFrame.vue'

const HtmlPreviewStub = defineComponent({
  name: 'HtmlPreview',
  props: { html: String, mode: String, enlargeable: Boolean, modalTitle: String },
  template: '<div data-testid="html-preview" :data-mode="mode" :data-enlargeable="enlargeable ? \'1\' : \'0\'">{{ html }}</div>',
})

function mountFrame(props: { label: string; html: string; highlighted?: boolean; selected?: boolean }) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyDemoFrame, {
    props,
    global: {
      plugins: [i18n],
      stubs: { HtmlPreview: HtmlPreviewStub },
    },
  })
}

describe('ClarifyDemoFrame', () => {
  it('uses card-radius (12px) and a card enlarge control, not a nested preview modal', () => {
    const wrapper = mountFrame({
      label: '方案 A',
      html: '<!doctype html><html><body>demo</body></html>',
    })
    const root = wrapper.get('[data-testid="clarify-demo-frame"]')
    expect(root.classes()).toContain('rounded-lg')
    expect(root.classes()).not.toContain('rounded-none')
    const enlarge = wrapper.get('[data-testid="clarify-demo-enlarge"]')
    expect(enlarge.text()).not.toContain('放大')
    expect(enlarge.text()).not.toContain('Enlarge')
    expect(enlarge.attributes('aria-label')).toBe('放大')
    expect(enlarge.attributes('title')).toBe('放大')
    expect(enlarge.get('[data-icon="expand"]').attributes('width')).toBe('11')
    const preview = wrapper.get('[data-testid="html-preview"]')
    expect(preview.attributes('data-mode')).toBe('demo')
    expect(preview.attributes('data-enlargeable')).toBe('0')
    wrapper.unmount()
  })

  it('hides the card enlarge control inside the page window', () => {
    const wrapper = mount(ClarifyDemoFrame, {
      props: { label: '方案 A', html: '<p>a</p>', enlargeable: false, expanded: true },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'zh-CN',
            messages: { 'zh-CN': { ...common, ...pages } },
          }),
        ],
        stubs: { HtmlPreview: HtmlPreviewStub },
      },
    })
    expect(wrapper.find('[data-testid="clarify-demo-enlarge"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="clarify-demo-frame"]').classes()).toContain('clarify-demo-expanded')
    wrapper.unmount()
  })

  it('renders label and html preview', () => {
    const wrapper = mountFrame({
      label: '方案 A',
      html: '<!doctype html><html><body>demo</body></html>',
    })
    expect(wrapper.text()).toContain('方案 A')
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="html-preview"]').text()).toContain('demo')
    wrapper.unmount()
  })

  it('keeps the selected mark and enlarge outside the truncated name', () => {
    const label = '产物抽屉顶部加一条只读工作流，资源抽屉不放，名字长到顶栏放不下'
    const wrapper = mountFrame({
      label,
      html: '<p>a</p>',
      highlighted: true,
      selected: true,
    })
    const name = wrapper.get('[data-testid="clarify-demo-label"]')
    expect(name.text()).toBe(label)
    expect(name.classes()).toEqual(expect.arrayContaining(['truncate', 'min-w-0', 'flex-1']))
    expect(name.text()).not.toContain('已选')
    const selected = wrapper.get('[data-testid="clarify-demo-selected"]')
    expect(selected.text()).toContain('已选')
    expect(selected.classes()).toEqual(expect.arrayContaining(['shrink-0', 'whitespace-nowrap']))
    expect(name.element.contains(selected.element)).toBe(false)
    const enlarge = wrapper.get('[data-testid="clarify-demo-enlarge"]')
    expect(enlarge.classes()).toContain('shrink-0')
    expect(enlarge.text()).not.toContain('放大')
    expect(enlarge.attributes('aria-label')).toBe('放大')
    wrapper.unmount()
  })

  it('shows selected marker and highlighted border', () => {
    const wrapper = mountFrame({
      label: '方案 B',
      html: '<html></html>',
      highlighted: true,
      selected: true,
    })
    expect(wrapper.text()).toMatch(/已选|✓/)
    expect(wrapper.get('[data-testid="clarify-demo-frame"]').classes()).toContain('border-accent')
    wrapper.unmount()
  })

  it('sets the inline preview body to 168px and leaves the HtmlPreview demo height at 140px', () => {
    const here = dirname(fileURLToPath(import.meta.url))
    const frameSrc = readFileSync(join(here, 'ClarifyDemoFrame.vue'), 'utf8')
    const previewSrc = readFileSync(join(here, '../ui/HtmlPreview.vue'), 'utf8')
    expect(frameSrc).toContain('height: 168px')
    expect(frameSrc).not.toContain('70vh')
    expect(frameSrc).toContain('height: 100%')
    expect(frameSrc).toContain('height: auto')
    expect(previewSrc).toContain('h-[140px]')
    expect(previewSrc).not.toContain('h-[168px]')
  })
})

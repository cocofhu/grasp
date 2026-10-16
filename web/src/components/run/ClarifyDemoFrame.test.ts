// @vitest-environment happy-dom
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
      stubs: { Icon: true, HtmlPreview: HtmlPreviewStub },
    },
  })
}

describe('ClarifyDemoFrame', () => {
  it('uses card-radius (12px) demo frame without a nested enlarge control', () => {
    const wrapper = mountFrame({
      label: '方案 A',
      html: '<!doctype html><html><body>demo</body></html>',
    })
    const root = wrapper.get('[data-testid="clarify-demo-frame"]')
    expect(root.classes()).toContain('rounded-lg')
    expect(root.classes()).not.toContain('rounded-none')
    expect(wrapper.find('button').exists()).toBe(false)
    const preview = wrapper.get('[data-testid="html-preview"]')
    expect(preview.attributes('data-mode')).toBe('demo')
    expect(preview.attributes('data-enlargeable')).toBe('0')
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
})

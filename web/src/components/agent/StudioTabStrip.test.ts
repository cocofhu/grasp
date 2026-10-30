// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import StudioTabStrip from './StudioTabStrip.vue'

const tabs = [
  { k: 'files', l: '工作目录', n: 2 },
  { k: 'mcp', l: 'MCP', n: 0 },
  { k: 'meta', l: '元信息' },
]

describe('StudioTabStrip', () => {
  it('renders tabs with count badges, marks the selection, and emits clicks', async () => {
    const wrapper = mount(StudioTabStrip, { props: { tabs, modelValue: 'mcp', testidPrefix: 'sub-' } })
    expect(wrapper.get('[role="tablist"]').exists()).toBe(true)
    const buttons = wrapper.findAll('[role="tab"]')
    expect(buttons.map((b) => b.text())).toEqual(['工作目录2', 'MCP0', '元信息'])
    expect(buttons.map((b) => b.attributes('aria-selected'))).toEqual(['false', 'true', 'false'])
    expect(wrapper.get('[data-testid="sub-meta"]').attributes('data-studio-tab')).toBe('meta')
    expect(buttons[1]!.classes()).toContain('font-medium')
    expect(buttons[0]!.classes()).toContain('h-10')
    await wrapper.setProps({ dense: true })
    expect(wrapper.get('[data-studio-tab="files"]').classes()).toContain('h-8')

    await buttons[2]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['meta']])
  })

  it('places the underline under the selected label and uses touch-sized targets', async () => {
    const wrapper = mount(StudioTabStrip, { props: { tabs, modelValue: 'files', touch: true }, attachTo: document.body })
    const files = wrapper.get('[data-studio-tab="files"]').element as HTMLElement
    Object.defineProperty(files, 'offsetLeft', { value: 4, configurable: true })
    Object.defineProperty(files, 'offsetWidth', { value: 90, configurable: true })
    await wrapper.setProps({ modelValue: 'files', tabs: [...tabs] })
    await flushPromises()
    const indicator = wrapper.get('[data-testid="studio-tab-indicator"]').element as HTMLElement
    expect(indicator.style.transform).toBe('translateX(14px)')
    expect(indicator.style.width).toBe('70px')
    expect(wrapper.get('[data-studio-tab="files"]').classes()).toContain('min-h-11')

    await wrapper.setProps({ modelValue: 'mcp' })
    await flushPromises()
    expect(indicator.style.display).toBe('none')
    wrapper.unmount()
  })
})

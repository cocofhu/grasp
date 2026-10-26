// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import PrioritySegmented from './PrioritySegmented.vue'

describe('PrioritySegmented', () => {
  it('emits update when selecting another tier', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common } },
    })
    const wrapper = mount(PrioritySegmented, {
      props: { modelValue: 'normal' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('高')
    expect(wrapper.text()).toContain('普通')
    expect(wrapper.text()).toContain('低')
    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['high'])
    wrapper.unmount()
  })

  it('does not emit when disabled', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common } },
    })
    const wrapper = mount(PrioritySegmented, {
      props: { modelValue: 'normal', disabled: true },
      global: { plugins: [i18n] },
    })
    await wrapper.findAll('button')[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('renders a sliding indicator for the active tier (g2.2)', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common } },
    })
    const wrapper = mount(PrioritySegmented, {
      props: { modelValue: 'normal' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.find('[data-testid="priority-seg-indicator"]').exists()).toBe(true)
    const active = wrapper.find('[data-seg-active="true"]')
    expect(active.text()).toContain('普通')
    expect(active.classes()).toContain('text-txt')
    expect(active.classes().join(' ')).not.toMatch(/accent/)
    expect(wrapper.find('[data-testid="priority-seg-indicator"]').classes().join(' ')).not.toMatch(/accent/)
    wrapper.unmount()
  })
})

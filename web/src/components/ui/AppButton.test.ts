// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import AppButton from './AppButton.vue'

function mountBtn(props: Record<string, unknown> = {}, slot = '保存') {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AppButton, {
    props,
    slots: { default: slot },
    global: {
      plugins: [i18n],
      stubs: { Icon: true },
      directives: { 'hover-ink': vHoverInk },
    },
  })
}

describe('AppButton', () => {
  it('renders slot text', () => {
    const wrapper = mountBtn({}, '提交')
    expect(wrapper.text()).toBe('提交')
    wrapper.unmount()
  })

  it('applies primary variant classes', () => {
    const wrapper = mountBtn({ variant: 'primary' })
    expect(wrapper.classes().join(' ')).toContain('bg-accent')
    wrapper.unmount()
  })

  it('loading keeps original label and size, disables pointer and tab submit', () => {
    const idle = mountBtn({ variant: 'primary' }, '保存')
    const loading = mountBtn({ variant: 'primary', loading: true }, '保存')
    expect(loading.text()).toContain('保存')
    expect(loading.text()).not.toContain('提交中')
    expect(loading.attributes('disabled')).toBeDefined()
    expect(loading.attributes('aria-busy')).toBe('true')
    expect(loading.findComponent({ name: 'AppSpinner' }).exists()).toBe(true)
    const idleClass = idle.classes().filter((c) => c.startsWith('px-') || c.startsWith('py-') || c.startsWith('text-')).join(' ')
    const loadingClass = loading.classes().filter((c) => c.startsWith('px-') || c.startsWith('py-') || c.startsWith('text-')).join(' ')
    expect(loadingClass).toBe(idleClass)
    expect(loading.classes()).toContain('rounded-md')
    idle.unmount()
    loading.unmount()
  })

  it('size sm uses h-6 (24px) height token, not padding-driven height', () => {
    const wrapper = mountBtn({ size: 'sm' })
    const cls = wrapper.classes().join(' ')
    expect(cls).toMatch(/\bh-6\b/)
    expect(cls).toMatch(/\bpx-2.5\b/)
    expect(cls).toMatch(/\btext-xs\b/)
    expect(cls).not.toMatch(/\bpy-/)
    wrapper.unmount()
  })

  it('size md uses h-9 (36px) height token, not padding-driven height', () => {
    const wrapper = mountBtn({ size: 'md' })
    const cls = wrapper.classes().join(' ')
    expect(cls).toMatch(/\bh-9\b/)
    expect(cls).toMatch(/\bpx-3.5\b/)
    expect(cls).toMatch(/\btext-sm\b/)
    expect(cls).not.toMatch(/\bpy-/)
    wrapper.unmount()
  })

  it('adds pressable class when idle and skips it when loading/disabled (g2.1)', () => {
    const idle = mountBtn({ variant: 'primary' })
    expect(idle.classes()).toContain('ui-pressable')
    expect(idle.classes().join(' ')).toMatch(/focus-visible:ring-2/)
    idle.unmount()
    const loading = mountBtn({ variant: 'primary', loading: true })
    expect(loading.classes()).not.toContain('ui-pressable')
    loading.unmount()
    const disabled = mountBtn({ disabled: true })
    expect(disabled.classes()).not.toContain('ui-pressable')
    disabled.unmount()
  })

  it('uses hover-ink host instead of hover:bg-* fill (plan g2.2)', () => {
    const idle = mountBtn({ variant: 'primary' })
    expect(idle.classes()).toContain('hover-ink-host')
    expect(idle.classes().join(' ')).not.toMatch(/hover:bg-/)
    expect(idle.element.style.getPropertyValue('--hover-ink-color')).toContain('--c-accent-2')
    expect(idle.find('.hover-ink').exists()).toBe(true)
    idle.unmount()

    const ghost = mountBtn({ variant: 'ghost' })
    expect(ghost.element.style.getPropertyValue('--hover-ink-color')).toContain('--c-elevated')
    ghost.unmount()

    const danger = mountBtn({ variant: 'danger' })
    expect(danger.element.style.getPropertyValue('--hover-ink-color')).toContain('--c-err')
    danger.unmount()
  })

  it('does not enable hover-ink when disabled or loading (plan g1.3 / g2.3)', () => {
    const loading = mountBtn({ loading: true })
    expect(loading.attributes('aria-busy')).toBe('true')
    expect(loading.attributes('disabled')).toBeDefined()
    loading.unmount()
    const disabled = mountBtn({ disabled: true })
    expect(disabled.attributes('disabled')).toBeDefined()
    disabled.unmount()
  })
})

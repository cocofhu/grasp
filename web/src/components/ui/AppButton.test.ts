// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
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

  it('subtle skips hover-ink fill (review v2 / plan g2.2)', () => {
    const subtle = mountBtn({ variant: 'subtle' }, 'Subtle')
    // Directive still mounts host, but binding.enabled is false so no fill color / expand.
    expect(subtle.classes().join(' ')).not.toMatch(/hover:bg-/)
    expect(subtle.element.style.getPropertyValue('--hover-ink-color')).toBe('')
    subtle.unmount()
  })

  it('keeps bare text slot above ink stacking (plan g1.2 / review v1)', () => {
    const idle = mountBtn({ variant: 'primary' }, 'Primary')
    const face = idle.find('span.relative')
    expect(face.exists()).toBe(true)
    expect(face.text()).toBe('Primary')
    expect(face.classes()).toContain('relative')
    expect(face.classes()).toContain('z-[1]')
    const ink = idle.find('.hover-ink')
    expect(ink.exists()).toBe(true)
    idle.unmount()
  })

  it('flows slot inline icons in one centered row so they cannot wrap (plan g1.2)', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(AppButton, {
      props: { variant: 'primary', icon: 'plus' },
      slots: {
        default: () => [
          h('span', { 'data-slot-label': '' }, '新建工作流'),
          h('svg', { 'data-slot-icon': '' }),
        ],
      },
      global: {
        plugins: [i18n],
        stubs: { Icon: true },
        directives: { 'hover-ink': vHoverInk },
      },
    })
    const face = wrapper.find('span.relative')
    expect(face.classes()).toContain('relative')
    expect(face.classes()).toContain('z-[1]')
    expect(face.classes()).toContain('inline-flex')
    expect(face.classes()).toContain('items-center')
    expect(face.classes()).toContain('gap-1.5')
    expect(face.find('[data-slot-label]').exists()).toBe(true)
    expect(face.find('[data-slot-icon]').exists()).toBe(true)
    wrapper.unmount()
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

  it('does not shift content after left-edge hover-ink (plan g2.2)', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const { nextTick } = await import('vue')
    const { vi } = await import('vitest')
    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'),
      'utf8',
    )
    expect(css).toMatch(/\.hover-ink-host\s*\{[^}]*overflow:\s*clip/s)
    // plan g1.1: transform centering, not negative-margin layout box
    expect(css).not.toMatch(/margin:\s*calc\(\s*var\(--ink-d/)
    expect(css).toMatch(/translate\(-50%,\s*-50%\)/)

    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches: query.includes('hover: hover') && query.includes('pointer: fine'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )

    const wrapper = mountBtn({ variant: 'primary' }, '保存')
    await nextTick()
    const btn = wrapper.element as HTMLElement
    Object.defineProperty(btn, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({
        left: 20,
        top: 20,
        width: 96,
        height: 36,
        right: 116,
        bottom: 56,
        x: 20,
        y: 20,
        toJSON: () => ({}),
      }),
    })
    const face = wrapper.find('span.relative').element as HTMLElement
    Object.defineProperty(face, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({
        left: 36,
        top: 28,
        width: 32,
        height: 20,
        right: 68,
        bottom: 48,
        x: 36,
        y: 28,
        toJSON: () => ({}),
      }),
    })
    const before = face.getBoundingClientRect().left
    btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 22, clientY: 38, pointerType: 'mouse', bubbles: true }),
    )
    expect(btn.classList.contains('is-hover-ink')).toBe(true)
    expect(btn.scrollLeft).toBe(0)
    expect(face.getBoundingClientRect().left).toBe(before)

    wrapper.unmount()
    vi.unstubAllGlobals()
  })
})

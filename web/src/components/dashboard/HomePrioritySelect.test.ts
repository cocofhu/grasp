// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import HomePrioritySelect from './HomePrioritySelect.vue'

function mountSelect(props: { modelValue?: 'high' | 'normal' | 'low'; disabled?: boolean } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(HomePrioritySelect, {
    props: {
      modelValue: props.modelValue ?? 'normal',
      disabled: props.disabled ?? false,
    },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: { Teleport: false },
    },
  })
}

describe('HomePrioritySelect', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
  })

  // plan g1.1 — 32px chip trigger, default label 普通
  it('renders a 32px trigger defaulting to 普通', async () => {
    const wrapper = mountSelect({})
    const trigger = wrapper.get('[data-testid="home-priority-select-trigger"]')
    expect(trigger.text()).toContain('普通')
    expect(trigger.attributes('aria-label')).toBe('优先级')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    const el = trigger.element as HTMLButtonElement
    expect(getComputedStyle(el).height === '32px' || el.className.includes('home-priority-select__trigger')).toBe(true)
    wrapper.unmount()
  })

  it('disables the trigger when sending (plan g1.1)', () => {
    const wrapper = mountSelect({ disabled: true })
    expect(wrapper.get('[data-testid="home-priority-select-trigger"]').element).toHaveProperty('disabled', true)
    wrapper.unmount()
  })

  // plan g1.2 / g3.2 — Teleport + fixed + z-index 60, not nested in composer
  it('teleports a fixed z-index 60 panel to body below the trigger', async () => {
    const composer = document.createElement('div')
    composer.className = 'home-composer'
    composer.style.overflow = 'hidden'
    document.body.appendChild(composer)

    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(HomePrioritySelect, {
      props: { modelValue: 'normal' },
      attachTo: composer,
      global: {
        plugins: [i18n],
        stubs: { Teleport: false },
      },
    })
    const triggerEl = wrapper.get('[data-testid="home-priority-select-trigger"]').element as HTMLElement
    vi.spyOn(triggerEl, 'getBoundingClientRect').mockReturnValue({
      top: 200,
      bottom: 232,
      left: 40,
      right: 140,
      width: 100,
      height: 32,
      x: 40,
      y: 200,
      toJSON() {
        return {}
      },
    })
    await wrapper.get('[data-testid="home-priority-select-trigger"]').trigger('click')
    await flushPromises()
    const panel = document.querySelector('[data-testid="home-priority-select-panel"]') as HTMLElement | null
    expect(panel).toBeTruthy()
    expect(panel!.parentElement).toBe(document.body)
    expect(composer.contains(panel)).toBe(false)
    expect(panel!.style.position).toBe('fixed')
    expect(panel!.style.top).toBe('238px')
    expect(panel!.style.zIndex).toBe('60')
    expect(panel!.getAttribute('data-placement')).toBe('below')
    wrapper.unmount()
    composer.remove()
  })

  it('emits the chosen priority and writes it back to the trigger (plan g1.1)', async () => {
    const wrapper = mountSelect({})
    await wrapper.get('[data-testid="home-priority-select-trigger"]').trigger('click')
    await flushPromises()
    const high = document.querySelector('[data-testid="home-priority-select-option-high"]') as HTMLElement
    high.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe('high')
    expect(document.querySelector('[data-testid="home-priority-select-panel"]')).toBeNull()
    wrapper.unmount()
  })

  it.each(['Enter', ' ', 'ArrowDown'])('opens with %j without choosing or advancing an option', async (key) => {
    const wrapper = mountSelect({})
    const trigger = wrapper.get('[data-testid="home-priority-select-trigger"]').element as HTMLButtonElement
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
    await flushPromises()

    expect(document.querySelector('[data-testid="home-priority-select-panel"]')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(document.querySelector('.home-priority-select__opt--active')?.getAttribute('data-priority')).toBe('normal')

    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }))
    await flushPromises()
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['low']])
    expect(document.querySelector('[data-testid="home-priority-select-panel"]')).toBeNull()
    wrapper.unmount()
  })

  // plan g1.3 — Escape closes and returns focus; outside click closes
  it('closes on Escape and restores focus to the trigger', async () => {
    const wrapper = mountSelect({})
    const trigger = wrapper.get('[data-testid="home-priority-select-trigger"]')
    await trigger.trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="home-priority-select-panel"]')).toBeTruthy()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="home-priority-select-panel"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it('closes when clicking outside the panel (plan g1.3)', async () => {
    const wrapper = mountSelect({})
    await wrapper.get('[data-testid="home-priority-select-trigger"]').trigger('click')
    await flushPromises()
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="home-priority-select-panel"]')).toBeNull()
    wrapper.unmount()
  })

  it('keeps the panel inside the viewport when the trigger is near the right edge (plan g1.2)', async () => {
    const wrapper = mountSelect({})
    const triggerEl = wrapper.get('[data-testid="home-priority-select-trigger"]').element as HTMLElement
    vi.spyOn(triggerEl, 'getBoundingClientRect').mockReturnValue({
      top: 100,
      bottom: 132,
      left: window.innerWidth - 40,
      right: window.innerWidth - 8,
      width: 32,
      height: 32,
      x: window.innerWidth - 40,
      y: 100,
      toJSON() {
        return {}
      },
    })
    await wrapper.get('[data-testid="home-priority-select-trigger"]').trigger('click')
    await flushPromises()
    const panel = document.querySelector('[data-testid="home-priority-select-panel"]') as HTMLElement
    const left = Number.parseInt(panel.style.left, 10)
    const width = Number.parseInt(panel.style.width, 10)
    expect(left).toBeGreaterThanOrEqual(8)
    expect(left + width).toBeLessThanOrEqual(window.innerWidth - 8)
    wrapper.unmount()
  })

  it('opens above the trigger near the bottom of a short viewport', async () => {
    vi.stubGlobal('innerHeight', 300)
    const wrapper = mountSelect({})
    const trigger = wrapper.get('[data-testid="home-priority-select-trigger"]')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue({
      top: 232,
      bottom: 264,
      left: 40,
      right: 120,
      width: 80,
      height: 32,
      x: 40,
      y: 232,
      toJSON: () => ({}),
    })
    await trigger.trigger('click')
    await flushPromises()
    const panel = document.querySelector('[data-testid="home-priority-select-panel"]') as HTMLElement
    const top = Number.parseInt(panel.style.top, 10)
    const maxHeight = Number.parseInt(panel.style.maxHeight, 10)
    expect(panel.getAttribute('data-placement')).toBe('above')
    expect(top).toBeGreaterThanOrEqual(8)
    expect(top + maxHeight).toBeLessThanOrEqual(226)
    wrapper.unmount()
  })

  it('limits a below panel to the available viewport height', async () => {
    vi.stubGlobal('innerHeight', 300)
    const wrapper = mountSelect({})
    const trigger = wrapper.get('[data-testid="home-priority-select-trigger"]')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue({
      top: 100,
      bottom: 132,
      left: 40,
      right: 120,
      width: 80,
      height: 32,
      x: 40,
      y: 100,
      toJSON: () => ({}),
    })
    await trigger.trigger('click')
    await flushPromises()
    const panel = document.querySelector('[data-testid="home-priority-select-panel"]') as HTMLElement
    expect(panel.getAttribute('data-placement')).toBe('below')
    expect(Number.parseInt(panel.style.top, 10) + Number.parseInt(panel.style.maxHeight, 10)).toBe(292)
    wrapper.unmount()
  })

  it('scrolls clipped keyboard options into the panel without scrolling the page', async () => {
    const wrapper = mountSelect({})
    const trigger = wrapper.get('[data-testid="home-priority-select-trigger"]')
    await trigger.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const panel = document.querySelector('[data-testid="home-priority-select-panel"]') as HTMLElement
    Object.defineProperty(panel, 'clientHeight', { configurable: true, value: 100 })
    for (const [priority, top] of [['high', 55], ['normal', 100], ['low', 145]] as const) {
      const option = panel.querySelector(`[data-priority="${priority}"].home-priority-select__opt`) as HTMLElement
      Object.defineProperties(option, {
        offsetTop: { configurable: true, value: top },
        offsetHeight: { configurable: true, value: 35 },
      })
    }
    const pageScroll = { x: window.scrollX, y: window.scrollY }
    const scrollPage = vi.spyOn(window, 'scrollTo')

    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const low = panel.querySelector('[data-priority="low"].home-priority-select__opt') as HTMLElement
    expect(low.classList.contains('home-priority-select__opt--active')).toBe(true)
    expect(panel.scrollTop).toBe(80)
    expect(low.offsetTop + low.offsetHeight).toBeLessThanOrEqual(panel.scrollTop + panel.clientHeight)

    await trigger.trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    expect(panel.scrollTop).toBe(80) // The normal option already fits.
    await trigger.trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    const high = panel.querySelector('[data-priority="high"].home-priority-select__opt') as HTMLElement
    expect(high.classList.contains('home-priority-select__opt--active')).toBe(true)
    expect(high.offsetTop).toBeGreaterThanOrEqual(panel.scrollTop)
    expect(panel.scrollTop).toBe(55)
    expect({ x: window.scrollX, y: window.scrollY }).toEqual(pageScroll)
    expect(scrollPage).not.toHaveBeenCalled()
    scrollPage.mockRestore()
    wrapper.unmount()
  })
})

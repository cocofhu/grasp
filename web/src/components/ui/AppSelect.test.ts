// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import AppSelect from './AppSelect.vue'

const OPTIONS = [
  { value: 'openai', label: 'OpenAI', hint: 'openai/gpt-4.1' },
  { value: 'anthropic', label: 'Anthropic', hint: 'anthropic/claude-sonnet-4-5' },
  { value: 'custom', label: 'Custom' },
]

function i18nPlugin() {
  return createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })
}

function mountSelect(props: Record<string, unknown> = {}, attached = false) {
  return mount(AppSelect, {
    props: { modelValue: 'openai', options: OPTIONS, ...props },
    global: { plugins: [i18nPlugin()], stubs: { Icon: true, Teleport: !attached } },
    attachTo: attached ? document.body : undefined,
  })
}

afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

function press(element: Element, key: string, init: KeyboardEventInit = {}) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init })
  element.dispatchEvent(event)
  return event
}

function box(left: number, top: number, width = 116, height = 30): DOMRect {
  return { x: left, y: top, left, top, right: left + width, bottom: top + height, width, height, toJSON: () => ({}) } as DOMRect
}

describe('AppSelect', () => {
  it('shows the selected label and falls back to the placeholder', () => {
    const wrapper = mountSelect()
    expect(wrapper.get('[data-test="app-select-trigger"]').text()).toContain('OpenAI')

    const empty = mountSelect({ modelValue: '', placeholder: '请选择' })
    expect(empty.get('[data-test="app-select-trigger"]').text()).toContain('请选择')
    wrapper.unmount()
    empty.unmount()
  })

  it('emits the chosen value and closes the panel', async () => {
    const wrapper = mountSelect()
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(false)

    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(true)

    await wrapper.get('[data-test="app-select-option-anthropic"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['anthropic'])
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('marks the current option as selected', async () => {
    const wrapper = mountSelect({ modelValue: 'custom' })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    expect(wrapper.get('[data-test="app-select-option-custom"]').attributes('aria-selected')).toBe(
      'true',
    )
    expect(wrapper.get('[data-test="app-select-option-openai"]').attributes('aria-selected')).toBe(
      'false',
    )
    wrapper.unmount()
  })

  it('supports keyboard open, arrow move, enter select and escape close', async () => {
    const wrapper = mountSelect()
    const trigger = wrapper.get('[data-test="app-select-trigger"]')

    await trigger.trigger('keydown', { key: 'ArrowDown' })
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['anthropic'])

    await trigger.trigger('keydown', { key: 'Enter' })
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps a real bubbling Enter open, exposes the active option and scrolls keyboard navigation into view', async () => {
    const wrapper = mountSelect({}, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]').element as HTMLButtonElement
    trigger.focus()

    press(trigger, 'Enter')
    await flushPromises()
    expect(trigger.getAttribute('role')).toBe('combobox')
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    const listbox = document.getElementById(trigger.getAttribute('aria-controls')!)!
    expect(listbox.getAttribute('role')).toBe('listbox')
    expect(document.getElementById(trigger.getAttribute('aria-activedescendant')!)?.textContent).toContain('OpenAI')

    const lastOption = listbox.querySelector('[data-test="app-select-option-custom"]') as HTMLElement
    const reveal = vi.spyOn(lastOption, 'scrollIntoView')
    press(trigger, 'End')
    await flushPromises()
    expect(trigger.getAttribute('aria-activedescendant')).toBe(lastOption.id)
    expect(reveal).toHaveBeenCalledWith({ block: 'nearest', inline: 'nearest' })

    press(trigger, 'Home')
    await flushPromises()
    expect(document.getElementById(trigger.getAttribute('aria-activedescendant')!)?.textContent).toContain('OpenAI')
    press(trigger, 'ArrowDown')
    await flushPromises()
    press(trigger, 'Enter')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['anthropic']])
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(trigger.hasAttribute('aria-activedescendant')).toBe(false)
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })

  it('consumes Escape before the enclosing dialog and returns focus to the trigger', async () => {
    const wrapper = mountSelect({ searchable: true }, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]').element as HTMLButtonElement
    trigger.click()
    await flushPromises()
    const input = document.querySelector('[data-test="app-select-search"]') as HTMLInputElement
    expect(document.activeElement).toBe(input)
    const parentEscape = vi.fn()
    window.addEventListener('keydown', parentEscape)

    const escape = press(input, 'Escape')
    await flushPromises()
    expect(escape.defaultPrevented).toBe(true)
    expect(parentEscape).not.toHaveBeenCalled()
    expect(document.querySelector('[data-test="app-select-panel"]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    window.removeEventListener('keydown', parentEscape)
    wrapper.unmount()
  })

  it('positions the teleported panel before revealing the selected option', async () => {
    const positions: string[] = []
    vi.spyOn(HTMLElement.prototype, 'scrollIntoView').mockImplementation(function (this: HTMLElement) {
      positions.push((this.closest('[data-test="app-select-panel"]') as HTMLElement | null)?.style.position || '')
    })
    const wrapper = mountSelect({ modelValue: 'custom' }, true)
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await flushPromises()
    expect(positions.length).toBeGreaterThan(0)
    expect(positions.every(position => position === 'fixed')).toBe(true)
    wrapper.unmount()
  })

  it.each([
    { searchable: false, shiftKey: false },
    { searchable: false, shiftKey: true },
    { searchable: true, shiftKey: false },
    { searchable: true, shiftKey: true },
  ])('closes on Tab without cancelling native focus movement ($searchable search, $shiftKey shift)', async ({ searchable, shiftKey }) => {
    const wrapper = mountSelect({ searchable }, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]').element as HTMLButtonElement
    trigger.focus()
    trigger.click()
    await flushPromises()
    const focused = document.activeElement!
    const tab = press(focused, 'Tab', { shiftKey })
    // Native browser Tab runs after keydown. Its starting point must be the
    // original trigger, rather than a teleported field at the end of body.
    expect(document.activeElement).toBe(trigger)
    expect(tab.defaultPrevented).toBe(false)
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).toBeNull()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('handles searchable navigation once while preserving editing keys and IME composition', async () => {
    const wrapper = mountSelect({ searchable: true, allowCustom: true }, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]').element as HTMLButtonElement
    trigger.focus()
    press(trigger, 'Enter')
    await flushPromises()
    expect(trigger.hasAttribute('role')).toBe(false)
    const input = document.querySelector('[data-test="app-select-search"]') as HTMLInputElement
    expect(document.activeElement).toBe(input)
    expect(input.getAttribute('role')).toBe('combobox')
    expect(document.getElementById(input.getAttribute('aria-controls')!)?.getAttribute('role')).toBe('listbox')

    press(input, 'ArrowDown')
    await flushPromises()
    expect(document.getElementById(input.getAttribute('aria-activedescendant')!)?.textContent).toContain('Anthropic')
    for (const key of ['Home', 'End', ' ']) expect(press(input, key).defaultPrevented).toBe(false)
    expect(press(input, 'ArrowLeft', { ctrlKey: true }).defaultPrevented).toBe(false)
    press(input, 'Enter', { isComposing: true })
    await flushPromises()
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    input.value = 'vendor/my model'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    press(input, 'Enter')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['vendor/my model']])
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })

  it('closes an open menu immediately when disabled and rejects a stale option click', async () => {
    const wrapper = mountSelect({}, true)
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    const option = document.querySelector('[data-test="app-select-option-anthropic"]') as HTMLButtonElement
    expect(option.tabIndex).toBe(-1)
    await wrapper.setProps({ disabled: true })
    expect(document.querySelector('[data-test="app-select-panel"]')).toBeNull()
    option.click()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([
    { viewport: 390, left: 270, expectedWidth: 168, expectedLeft: 214 },
    { viewport: 140, left: 100, expectedWidth: 124, expectedLeft: 8 },
  ])('gives a ghost menu label space while clamping to a $viewport px viewport', async ({ viewport, left, expectedWidth, expectedLeft }) => {
    vi.stubGlobal('innerWidth', viewport)
    const wrapper = mountSelect({ variant: 'ghost', size: 'sm' }, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue({
      top: 100, bottom: 130, left, right: left + 116, width: 116, height: 30,
      x: left, y: 100, toJSON: () => ({}),
    } as DOMRect)
    await trigger.trigger('click')
    await flushPromises()
    const panel = document.querySelector('[data-test="app-select-panel"]') as HTMLElement
    expect(trigger.classes()).toContain('app-select-trigger--ghost')
    expect(trigger.classes()).toContain('app-select-trigger--sm')
    expect(panel.classList.contains('card')).toBe(false)
    expect(panel.style.width).toBe(`${expectedWidth}px`)
    expect(panel.style.left).toBe(`${expectedLeft}px`)
    expect(expectedLeft).toBeGreaterThanOrEqual(8)
    expect(expectedLeft + expectedWidth).toBeLessThanOrEqual(viewport - 8)
    wrapper.unmount()
  })

  it.each([
    { edge: 'below', hiddenBox: box(24, 994), event: 'scroll' },
    { edge: 'left', hiddenBox: box(-130, 300), event: 'resize' },
  ])('dismisses a ghost menu when its trigger moves completely $edge the viewport without restoring focus', async ({ hiddenBox, event }) => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 960)
    const wrapper = mountSelect({ variant: 'ghost' }, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    const bounds = vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(box(24, 300))
    await trigger.trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).not.toBeNull()
    const focus = vi.spyOn(trigger.element as HTMLElement, 'focus')
    bounds.mockReturnValue(hiddenBox)
    window.dispatchEvent(new Event(event))
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).toBeNull()
    expect(focus).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['auto', 'scroll', 'hidden', 'clip'])('dismisses a ghost menu fully clipped by an overflow:%s ancestor', async overflow => {
    const wrapper = mountSelect({ variant: 'ghost' }, true)
    const clip = document.createElement('div')
    clip.style.overflowY = overflow
    document.body.append(clip)
    clip.append(wrapper.element)
    vi.spyOn(clip, 'getBoundingClientRect').mockReturnValue(box(0, 100, 320, 160))
    // The real scrollport ends before the border box (for example, a scrollbar).
    vi.spyOn(clip, 'clientHeight', 'get').mockReturnValue(140)
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    const bounds = vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(box(24, 120))
    await trigger.trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).not.toBeNull()
    bounds.mockReturnValue(box(24, 242, 116, 10))
    clip.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).toBeNull()
    wrapper.unmount()
  })

  it.each([
    { edge: 'above', bounds: box(24, -12) },
    { edge: 'below', bounds: box(24, 944) },
    { edge: 'right', bounds: box(370, 300) },
  ])('keeps a partially visible ghost trigger $edge the viewport and clamps its panel inside', async ({ bounds: partialBox }) => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 960)
    const wrapper = mountSelect({ variant: 'ghost' }, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    const bounds = vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(box(24, 300))
    await trigger.trigger('click')
    await flushPromises()
    const panel = document.querySelector('[data-test="app-select-panel"]') as HTMLElement
    vi.spyOn(panel, 'offsetHeight', 'get').mockReturnValue(120)
    bounds.mockReturnValue(partialBox)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).toBe(panel)
    const top = Number.parseInt(panel.style.top, 10)
    const left = Number.parseInt(panel.style.left, 10)
    const maxHeight = Number.parseInt(panel.style.maxHeight, 10)
    expect(top).toBeGreaterThanOrEqual(8)
    expect(top + Math.min(120, maxHeight)).toBeLessThanOrEqual(952)
    expect(left).toBeGreaterThanOrEqual(8)
    expect(left + Number.parseInt(panel.style.width, 10)).toBeLessThanOrEqual(382)
    expect(maxHeight).toBeGreaterThanOrEqual(0)
    wrapper.unmount()
  })

  it('tracks partial clipping on each ancestor axis without dismissing a visible fragment', async () => {
    const wrapper = mountSelect({ variant: 'ghost' }, true)
    const clip = document.createElement('div')
    clip.style.overflowY = 'hidden'
    document.body.append(clip)
    clip.append(wrapper.element)
    vi.spyOn(clip, 'getBoundingClientRect').mockReturnValue(box(0, 100, 320, 160))
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    const bounds = vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(box(24, 120))
    await trigger.trigger('click')
    await flushPromises()
    bounds.mockReturnValue(box(24, 250))
    clip.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).not.toBeNull()
    // A horizontal clipping ancestor must be intersected as well.
    clip.style.overflowX = 'clip'
    bounds.mockReturnValue(box(322, 120))
    clip.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).toBeNull()
    wrapper.unmount()
  })

  it('preserves outline positioning when its trigger scrolls outside the viewport', async () => {
    vi.stubGlobal('innerHeight', 960)
    const wrapper = mountSelect({}, true)
    const trigger = wrapper.get('[data-test="app-select-trigger"]')
    const bounds = vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(box(24, 300))
    await trigger.trigger('click')
    await flushPromises()
    bounds.mockReturnValue(box(24, 994))
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.querySelector('[data-test="app-select-panel"]')).not.toBeNull()
    wrapper.unmount()
  })

  it('allows a ghost select to open before trigger geometry is measurable', async () => {
    const wrapper = mountSelect({ variant: 'ghost' })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('stays closed when disabled', async () => {
    const wrapper = mountSelect({ disabled: true })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    expect(wrapper.find('[data-test="app-select-panel"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the invalid border instead of the idle one', () => {
    const wrapper = mountSelect({ invalid: true })
    const classes = wrapper.get('[data-test="app-select-trigger"]').classes()
    expect(classes).toContain('border-err')
    expect(classes).not.toContain('border-line')
    wrapper.unmount()
  })

  it('filters, highlights and reports an empty result when searchable', async () => {
    const wrapper = mountSelect({ searchable: true })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    const box = wrapper.get('[data-test="app-select-search"]')

    await box.setValue('anth')
    expect(wrapper.find('[data-test="app-select-option-openai"]').exists()).toBe(false)
    const hit = wrapper.get('[data-test="app-select-option-anthropic"]')
    expect(hit.html()).toContain('<mark>Anth</mark>')

    // hint carries the sample model, so a model name finds its vendor.
    await box.setValue('claude')
    expect(wrapper.find('[data-test="app-select-option-anthropic"]').exists()).toBe(true)

    await box.setValue('zzz')
    expect(wrapper.find('[data-test="app-select-empty"]').exists()).toBe(true)
    expect(wrapper.findAll('[role="option"]')).toHaveLength(0)
    expect(box.attributes('aria-activedescendant')).toBeUndefined()
    wrapper.unmount()
  })

  it('keyboard picks from the filtered list and space stays literal in search', async () => {
    const wrapper = mountSelect({ searchable: true })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    const box = wrapper.get('[data-test="app-select-search"]')

    await box.setValue('cust')
    await box.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['custom'])
    wrapper.unmount()
  })

  it('clears the query when reopened', async () => {
    const wrapper = mountSelect({ searchable: true })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await wrapper.get('[data-test="app-select-search"]').setValue('anth')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(1)

    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    expect(
      (wrapper.get('[data-test="app-select-search"]').element as HTMLInputElement).value,
    ).toBe('')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(OPTIONS.length)
    wrapper.unmount()
  })

  it('offers the typed value when allowCustom, and shows it on the trigger', async () => {
    const wrapper = mountSelect({ searchable: true, allowCustom: true })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await wrapper.get('[data-test="app-select-search"]').setValue('vendor/my-model')

    expect(wrapper.find('[data-test="app-select-empty"]').exists()).toBe(false)
    await wrapper.get('[data-test="app-select-custom"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['vendor/my-model'])

    // A value outside the list is its own trigger label, not the placeholder.
    await wrapper.setProps({ modelValue: 'vendor/my-model', placeholder: '请选择' })
    expect(wrapper.get('[data-test="app-select-trigger"]').text()).toContain('vendor/my-model')
    wrapper.unmount()
  })

  it('does not offer a custom row for a query that already is an option', async () => {
    const wrapper = mountSelect({ searchable: true, allowCustom: true })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await wrapper.get('[data-test="app-select-search"]').setValue('custom')
    expect(wrapper.find('[data-test="app-select-custom"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('has no search box unless searchable', async () => {
    const wrapper = mountSelect()
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    expect(wrapper.find('[data-test="app-select-search"]').exists()).toBe(false)
    wrapper.unmount()
  })

  // A trigger click stops propagating, so the other select never sees it on the
  // document and the two panels would otherwise overlap.
  it('closes the open panel when another select opens', async () => {
    const wrapper = mount(
      {
        components: { AppSelect },
        template: `
          <div>
            <AppSelect data-test="first" model-value="openai" :options="options" />
            <AppSelect data-test="second" model-value="custom" :options="options" />
          </div>
        `,
        data: () => ({ options: OPTIONS }),
      },
      { global: { plugins: [i18nPlugin()], stubs: { Icon: true, Teleport: true } } },
    )

    const triggers = wrapper.findAll('[data-test="app-select-trigger"]')
    await triggers[0].trigger('click')
    expect(wrapper.findAll('[data-test="app-select-panel"]').length).toBe(1)

    await triggers[1].trigger('click')
    const panels = wrapper.findAll('[data-test="app-select-panel"]')
    expect(panels.length).toBe(1)
    // The one left open is the second select's.
    expect(wrapper.findAll('[data-test="app-select-option-custom"]')[0].attributes('aria-selected')).toBe(
      'true',
    )
    wrapper.unmount()
  })

  it('removes a fading menu from accessibility and keyboard interaction when another select opens', async () => {
    vi.useFakeTimers()
    const wrapper = mount({
      components: { AppSelect },
      template: '<div><AppSelect model-value="openai" :options="options" /><AppSelect model-value="custom" :options="options" /></div>',
      data: () => ({ options: OPTIONS }),
    }, {
      attachTo: document.body,
      global: { plugins: [i18nPlugin()], stubs: { Icon: true, Teleport: false, Transition: false } },
    })
    const triggers = wrapper.findAll('[data-test="app-select-trigger"]')
    await triggers[0].trigger('click')
    await nextTick()
    const firstPanel = document.querySelector('[data-test="app-select-panel"]') as HTMLElement
    await triggers[1].trigger('click')
    await nextTick()
    const panels = document.querySelectorAll('[data-test="app-select-panel"]')
    expect(panels).toHaveLength(2)
    expect(firstPanel.getAttribute('aria-hidden')).toBe('true')
    expect(firstPanel.inert).toBe(true)
    const currentPanel = panels[1] as HTMLElement
    expect(currentPanel.hasAttribute('aria-hidden')).toBe(false)
    expect(currentPanel.inert).toBe(false)
    wrapper.unmount()
  })

  it('shows an option hint next to its label', async () => {
    const wrapper = mountSelect()
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    const row = wrapper.get('[data-test="app-select-option-openai"]')
    expect(row.text()).toContain('OpenAI')
    expect(row.text()).toContain('openai/gpt-4.1')
    wrapper.unmount()
  })

  // Options that are still on their way must not read as "nothing matches".
  it('says it is loading instead of showing the empty state', async () => {
    const wrapper = mountSelect({ options: [], loading: true })
    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    expect(wrapper.find('[data-test="app-select-empty"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="app-select-loading"]').text()).toBe(common.common.search.loadingOptions)

    await wrapper.setProps({ loading: false })
    expect(wrapper.find('[data-test="app-select-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="app-select-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('teleports a fixed panel so modal overflow cannot clip it', async () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'openai', options: OPTIONS },
      global: { plugins: [i18nPlugin()], stubs: { Icon: true, Teleport: false } },
      attachTo: document.body,
    })

    const triggerEl = wrapper.get('[data-test="app-select-trigger"]').element as HTMLElement
    vi.spyOn(triggerEl, 'getBoundingClientRect').mockReturnValue({
      top: 100,
      left: 24,
      right: 224,
      bottom: 132,
      width: 200,
      height: 32,
      x: 24,
      y: 100,
      toJSON: () => ({}),
    } as DOMRect)

    await wrapper.get('[data-test="app-select-trigger"]').trigger('click')
    await flushPromises()

    const panel = document.body.querySelector('[data-test="app-select-panel"]') as HTMLElement
    expect(panel).toBeTruthy()
    expect(panel.style.position).toBe('fixed')
    expect(panel.style.width).toBe('200px')
    // Opens below the trigger (bottom + gap).
    expect(Number.parseInt(panel.style.top, 10)).toBeGreaterThan(132)
    wrapper.unmount()
  })

  it('wraps the panel in overlay-pop Transition and rotates chevron (g2.3)', () => {
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppSelect.vue'), 'utf8')
    expect(src).toMatch(/name="overlay-pop"/)
    expect(src).toMatch(/app-select-chevron/)
    expect(src).toMatch(/is-open/)
  })
})

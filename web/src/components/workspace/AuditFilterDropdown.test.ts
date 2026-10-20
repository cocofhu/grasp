// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import AuditFilterDropdown from './AuditFilterDropdown.vue'

const OPTIONS = [
  { value: '', label: '全部' },
  { value: 'alpha', label: 'Alpha readable name', sub: 'resource/first', short: 'Alpha', dot: 'mcp' },
  { value: 'beta', label: 'Beta readable name', sub: 'resource/second', short: 'Beta', dot: 'run' },
]
const mounted: { unmount: () => void }[] = []
function plugins() {
  return [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })]
}
function render(props: Record<string, unknown> = {}) {
  const wrapper = mount(AuditFilterDropdown, {
    props: { modelValue: '', options: OPTIONS, testId: 'audit-dd', ...props },
    global: { plugins: plugins() }, attachTo: document.body,
  })
  mounted.push(wrapper)
  const trigger = wrapper.get('.audit-dd-trig').element as HTMLButtonElement
  const panel = document.body.lastElementChild as HTMLElement
  return { wrapper, trigger, panel }
}
function press(element: Element, key: string, init: KeyboardEventInit = {}) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init })
  element.dispatchEvent(event)
  return event
}
function box(left: number, top: number, width = 116, height = 34): DOMRect {
  return { x: left, y: top, left, top, right: left + width, bottom: top + height, width, height, toJSON: () => ({}) } as DOMRect
}
afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('AuditFilterDropdown', () => {
  it('keeps short trigger text, grouped descriptions and dots while filtering and selecting', async () => {
    const { wrapper, trigger, panel } = render({ modelValue: 'alpha', searchable: true, labelKey: '资源', groupBy: (o: typeof OPTIONS[number]) => o.dot || '全部' })
    expect(trigger.textContent).toContain('Alpha')
    expect(trigger.textContent).not.toContain('readable name')
    expect(trigger.title).toBe('Alpha readable name')
    trigger.click()
    await flushPromises()
    expect(panel.querySelectorAll('.audit-dd-group')).toHaveLength(3)
    expect(panel.querySelector('.audit-dd-opt.on .dot')?.classList.contains('mcp')).toBe(true)
    expect(panel.querySelector('.audit-dd-opt.on .s')?.textContent).toBe('resource/first')
    const input = panel.querySelector('input')!
    input.value = 'second'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(panel.querySelectorAll('.audit-dd-opt')).toHaveLength(1)
    expect(panel.querySelector('.audit-dd-opt')?.textContent).toContain('Beta readable name')
    const option = panel.querySelector('.audit-dd-opt') as HTMLButtonElement
    option.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
    expect(panel.inert).toBe(true)
    expect(document.activeElement).toBe(trigger)
  })

  it('keeps bubbling Enter open and links arrow/Home/End navigation to real options', async () => {
    const { wrapper, trigger, panel } = render()
    trigger.focus()
    press(trigger, 'Enter')
    await flushPromises()
    expect(trigger.getAttribute('role')).toBe('combobox')
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(document.getElementById(trigger.getAttribute('aria-controls')!)?.getAttribute('role')).toBe('listbox')
    const options = panel.querySelectorAll<HTMLElement>('[role="option"]')
    const reveal = vi.spyOn(options[2], 'scrollIntoView')
    press(trigger, 'End')
    await flushPromises()
    expect(trigger.getAttribute('aria-activedescendant')).toBe(options[2].id)
    expect(reveal).toHaveBeenCalledWith({ block: 'nearest', inline: 'nearest' })
    press(trigger, 'Home')
    await flushPromises()
    expect(trigger.getAttribute('aria-activedescendant')).toBe(options[0].id)
    press(trigger, 'ArrowDown')
    await flushPromises()
    expect(trigger.getAttribute('aria-activedescendant')).toBe(options[1].id)
    press(trigger, 'Enter')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['alpha']])
    expect(trigger.hasAttribute('aria-activedescendant')).toBe(false)
    expect(document.activeElement).toBe(trigger)
  })

  it('preserves search editing and IME keys, then selects the filtered result', async () => {
    const { wrapper, trigger, panel } = render({ searchable: true, labelKey: '资源' })
    press(trigger, 'Enter')
    await flushPromises()
    const input = panel.querySelector('input')!
    expect(document.activeElement).toBe(input)
    for (const key of ['Home', 'End', ' ']) expect(press(input, key).defaultPrevented).toBe(false)
    press(input, 'Enter', { isComposing: true })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    input.value = 'beta'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(document.getElementById(input.getAttribute('aria-activedescendant')!)?.textContent).toContain('Beta')
    press(input, 'Enter')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
    expect(document.activeElement).toBe(trigger)
  })

  it('consumes Escape before a parent dialog and restores trigger focus', async () => {
    const { trigger, panel } = render({ searchable: true })
    trigger.click()
    await flushPromises()
    const parentEscape = vi.fn()
    window.addEventListener('keydown', parentEscape)
    const event = press(panel.querySelector('input')!, 'Escape')
    await flushPromises()
    expect(event.defaultPrevented).toBe(true)
    expect(parentEscape).not.toHaveBeenCalled()
    expect(panel.getAttribute('aria-hidden')).toBe('true')
    expect(document.activeElement).toBe(trigger)
    window.removeEventListener('keydown', parentEscape)
  })

  it.each([false, true])('allows native Tab movement from the original trigger (shift=%s)', async shiftKey => {
    const { trigger, panel } = render({ searchable: true })
    trigger.click()
    await flushPromises()
    const event = press(panel.querySelector('input')!, 'Tab', { shiftKey })
    expect(event.defaultPrevented).toBe(false)
    expect(document.activeElement).toBe(trigger)
    await flushPromises()
    expect(panel.inert).toBe(true)
  })

  it('rejects disabled opening and stale option clicks after disabling an open menu', async () => {
    const { wrapper, trigger, panel } = render({ disabled: true })
    trigger.click()
    await flushPromises()
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    await wrapper.setProps({ disabled: false })
    trigger.click()
    await flushPromises()
    const option = panel.querySelector('.audit-dd-opt') as HTMLButtonElement
    expect(option.tabIndex).toBe(-1)
    await wrapper.setProps({ disabled: true })
    expect(panel.inert).toBe(true)
    option.click()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('hands over to another dropdown and excludes the hidden list from accessibility', async () => {
    const wrapper = mount({
      components: { AuditFilterDropdown }, data: () => ({ options: OPTIONS }),
      template: '<div><AuditFilterDropdown model-value="" :options="options" /><AuditFilterDropdown model-value="beta" :options="options" /></div>',
    }, { attachTo: document.body, global: { plugins: plugins() } })
    mounted.push(wrapper)
    const triggers = wrapper.findAll('.audit-dd-trig')
    await triggers[0].trigger('click')
    await flushPromises()
    await triggers[1].trigger('click')
    await flushPromises()
    const panels = [...document.querySelectorAll<HTMLElement>('.audit-dd-panel')]
    expect(panels.map(panel => panel.getAttribute('aria-hidden'))).toEqual(['true', 'false'])
    expect(panels[0].inert).toBe(true)
    expect(triggers[1].attributes('aria-controls')).not.toBe(panels[0].querySelector('[role="listbox"]')?.id)
    document.body.click()
    await flushPromises()
    expect(panels[1].inert).toBe(true)
  })

  it.each([
    { viewport: 390, viewportHeight: 960, bounds: box(340, 900, 40), expectedWidth: 260, expectedLeft: 120 },
    { viewport: 140, viewportHeight: 190, bounds: box(90, 140, 40), expectedWidth: 124, expectedLeft: 8 },
  ])('flips above a bottom anchor and stays inside a $viewport px viewport', async ({ viewport, viewportHeight, bounds, expectedWidth, expectedLeft }) => {
    vi.stubGlobal('innerWidth', viewport)
    vi.stubGlobal('innerHeight', viewportHeight)
    const { trigger, panel } = render({ right: true })
    vi.spyOn(trigger, 'getBoundingClientRect').mockReturnValue(bounds)
    vi.spyOn(panel, 'offsetHeight', 'get').mockReturnValue(120)
    trigger.click()
    await flushPromises()
    expect(panel.dataset.placement).toBe('above')
    expect(panel.style.width).toBe(`${expectedWidth}px`)
    expect(panel.style.left).toBe(`${expectedLeft}px`)
    const maxHeight = Number.parseInt(panel.style.maxHeight, 10)
    expect(Number.parseInt(panel.style.top, 10)).toBeGreaterThanOrEqual(8)
    expect(Number.parseInt(panel.style.top, 10) + Math.min(maxHeight, 120)).toBeLessThanOrEqual(viewportHeight - 8)
    expect(maxHeight).toBeGreaterThanOrEqual(0)
  })

  it('sizes a block menu to its trigger, follows partial visibility and dismisses offscreen without focusing', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 844)
    const { trigger, panel } = render({ block: true, width: 180 })
    const bounds = vi.spyOn(trigger, 'getBoundingClientRect').mockReturnValue(box(24, 300, 342, 44))
    trigger.click()
    await flushPromises()
    expect(panel.style.width).toBe('342px')
    bounds.mockReturnValue(box(24, -12, 342, 44))
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(panel.inert).toBe(false)
    expect(Number.parseInt(panel.style.top, 10)).toBeGreaterThanOrEqual(8)
    const focus = vi.spyOn(trigger, 'focus')
    bounds.mockReturnValue(box(24, 900, 342, 44))
    window.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(panel.inert).toBe(true)
    expect(focus).not.toHaveBeenCalled()
  })

  it.each(['auto', 'scroll', 'hidden', 'clip'])('closes when an overflow:%s ancestor fully clips the anchor', async overflow => {
    const { wrapper, trigger, panel } = render()
    const clip = document.createElement('div')
    clip.style.overflowY = overflow
    document.body.append(clip)
    clip.append(wrapper.element)
    vi.spyOn(clip, 'getBoundingClientRect').mockReturnValue(box(0, 100, 320, 160))
    vi.spyOn(clip, 'clientHeight', 'get').mockReturnValue(140)
    const bounds = vi.spyOn(trigger, 'getBoundingClientRect').mockReturnValue(box(24, 120))
    trigger.click()
    await flushPromises()
    bounds.mockReturnValue(box(24, 230))
    clip.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(panel.inert).toBe(false)
    bounds.mockReturnValue(box(24, 242, 116, 10))
    clip.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(panel.inert).toBe(true)
  })

  it('closes when a mobile filter editor hides the anchor', async () => {
    const { wrapper, trigger, panel } = render()
    trigger.click()
    await flushPromises()
    wrapper.element.style.display = 'none'
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(panel.inert).toBe(true)
  })
})

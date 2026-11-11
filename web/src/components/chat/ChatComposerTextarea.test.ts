// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ChatComposerTextarea from './ChatComposerTextarea.vue'

let observed: ((entries: { contentRect: { width: number } }[]) => void) | undefined
const disconnect = vi.fn()

class FakeResizeObserver {
  constructor(callback: typeof observed) { observed = callback }
  observe() {}
  disconnect = disconnect
}

function layout(el: HTMLTextAreaElement, width: number, scrollHeight: number) {
  Object.defineProperty(el, 'clientWidth', { configurable: true, value: width })
  Object.defineProperty(el, 'scrollHeight', { configurable: true, value: scrollHeight })
}

describe('ChatComposerTextarea', () => {
  beforeEach(() => vi.stubGlobal('ResizeObserver', FakeResizeObserver))
  afterEach(() => {
    vi.unstubAllGlobals()
    observed = undefined
    disconnect.mockReset()
  })

  it('remeasures when the width changes so a narrow first layout does not stick', async () => {
    const wrapper = mount(ChatComposerTextarea, { props: { modelValue: 'hello' } })
    const el = wrapper.find('textarea').element
    layout(el, 40, 400)
    observed?.([{ contentRect: { width: 40 } }])
    expect(el.style.height).toBe('220px')

    layout(el, 320, 60)
    observed?.([{ contentRect: { width: 320 } }])
    expect(el.style.height).toBe('60px')

    layout(el, 320, 500)
    observed?.([{ contentRect: { width: 320 } }])
    expect(el.style.height).toBe('60px')

    wrapper.unmount()
    expect(disconnect).toHaveBeenCalled()
  })

  it('keeps the natural height when empty or hidden', async () => {
    const wrapper = mount(ChatComposerTextarea, { props: { modelValue: 'hello' } })
    const el = wrapper.find('textarea').element
    layout(el, 320, 120)
    observed?.([{ contentRect: { width: 320 } }])
    expect(el.style.height).toBe('120px')

    await wrapper.setProps({ modelValue: '' })
    await nextTick()
    expect(el.style.height).toBe('')

    await wrapper.setProps({ modelValue: 'again' })
    layout(el, 0, 900)
    observed?.([{ contentRect: { width: 0 } }])
    expect(el.style.height).toBe('')
  })

  it('caps the height relative to the viewport on short screens', () => {
    const wrapper = mount(ChatComposerTextarea, { props: { modelValue: '' } })
    expect(wrapper.find('textarea').classes()).toContain('max-h-[min(220px,30dvh)]')
  })
})

// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SlidingTabs from './SlidingTabs.vue'

class ControlledResizeObserver {
  static instances: ControlledResizeObserver[] = []
  targets = new Set<Element>()
  observe = vi.fn((target: Element) => this.targets.add(target))
  unobserve = vi.fn((target: Element) => this.targets.delete(target))
  disconnect = vi.fn(() => this.targets.clear())

  constructor(private callback: ResizeObserverCallback) {
    ControlledResizeObserver.instances.push(this)
  }

  notify(target: Element) {
    this.callback([{ target } as ResizeObserverEntry], this as unknown as ResizeObserver)
  }
}

let bounds: Record<string, DOMRect>
const wrappers: { unmount: () => void }[] = []

function rect(x: number, y: number, width: number, height = 32) {
  return new DOMRect(x, y, width, height)
}

function mountTabs(selectionAttribute = 'aria-selected', activeSelector?: string) {
  const selected = ref<string | null>('a')
  const options = ref([
    { id: 'a', label: '全部' },
    { id: 'b', label: '失败/取消' },
  ])
  const count = ref(0)
  const Host = defineComponent({
    setup: () => () => h(SlidingTabs, {
      value: selected.value,
      as: 'nav',
      role: 'tablist',
      'aria-label': '状态',
      'data-box': 'track',
      indicatorTestid: 'highlight',
      ...(activeSelector ? { activeSelector } : {}),
    }, {
      default: () => options.value.map(option => h('button', {
        key: option.id,
        type: 'button',
        'data-box': option.id,
        [selectionAttribute]: selected.value === option.id ? 'true' : 'false',
        onClick: () => { selected.value = option.id },
      }, `${option.label}${selected.value === option.id ? ` ${count.value}` : ''}`)),
    }),
  })
  const wrapper = mount(Host, { attachTo: document.body })
  wrappers.push(wrapper)
  return { wrapper, selected, options, count }
}

beforeEach(() => {
  bounds = {
    track: rect(100, 200, 240, 40),
    a: rect(104, 204, 72),
    b: rect(180, 204, 116),
  }
  ControlledResizeObserver.instances = []
  vi.stubGlobal('ResizeObserver', ControlledResizeObserver)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    return bounds[this.dataset.box ?? ''] ?? rect(0, 0, 0, 0)
  })
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('SlidingTabs', () => {
  it.each(['aria-selected', 'aria-pressed', 'aria-checked', 'data-active'])(
    'positions one background on the initial option selected with %s',
    async attribute => {
      const { wrapper } = mountTabs(attribute)
      await nextTick()

      const highlight = wrapper.get<HTMLElement>('[data-testid="highlight"]')
      expect(highlight.element.style.transform).toBe('translate3d(4px, 4px, 0)')
      expect(highlight.element.style.width).toBe('72px')
      expect(highlight.element.style.height).toBe('32px')
      expect(highlight.attributes('aria-hidden')).toBe('true')
      expect(wrapper.findAll('[data-testid="highlight"]')).toHaveLength(1)
      expect(wrapper.get('nav').attributes('role')).toBe('tablist')
      expect(wrapper.get('nav').attributes('aria-label')).toBe('状态')
    },
  )

  it('moves the same background to the clicked option using its updated count width', async () => {
    const { wrapper, count } = mountTabs()
    await nextTick()
    const originalHighlight = wrapper.get<HTMLElement>('[data-testid="highlight"]').element

    // Selecting another status moves the count badge too, changing both widths.
    bounds.a = rect(104, 204, 52)
    bounds.b = rect(160, 204, 132)
    count.value = 128
    await wrapper.get('[data-box="b"]').trigger('click')
    await nextTick()

    const highlight = wrapper.get<HTMLElement>('[data-testid="highlight"]')
    expect(highlight.element).toBe(originalHighlight)
    expect(highlight.element.style.transform).toBe('translate3d(60px, 4px, 0)')
    expect(highlight.element.style.width).toBe('132px')
    expect(wrapper.get('[data-box="a"]').attributes('aria-selected')).toBe('false')
    expect(wrapper.get('[data-box="b"]').text()).toContain('128')
    expect(wrapper.findAll('[data-testid="highlight"]')).toHaveLength(1)

    bounds.b = rect(160, 204, 148)
    count.value = 9999
    await nextTick()
    await nextTick()
    expect(highlight.element.style.width).toBe('148px')
  })

  it('hides the background for a custom filter with no selected option and restores it on selection', async () => {
    const { wrapper, selected } = mountTabs()
    await nextTick()
    const highlight = wrapper.get<HTMLElement>('[data-testid="highlight"]')
    expect(highlight.isVisible()).toBe(true)

    selected.value = null
    await nextTick()
    await nextTick()
    expect(highlight.isVisible()).toBe(false)

    await wrapper.get('[data-box="b"]').trigger('click')
    await nextTick()
    expect(highlight.isVisible()).toBe(true)
    expect(highlight.element.style.transform).toBe('translate3d(80px, 4px, 0)')
  })

  it('remeasures when a translated preceding option changes width while the track size stays fixed', async () => {
    const { wrapper, selected, options } = mountTabs()
    selected.value = 'b'
    await nextTick()
    await nextTick()
    const observer = ControlledResizeObserver.instances[0]!
    const firstOption = wrapper.get('[data-box="a"]').element
    const highlight = wrapper.get<HTMLElement>('[data-testid="highlight"]')
    expect(observer.targets.has(firstOption)).toBe(true)

    options.value[0]!.label = 'All executions'
    bounds.a = rect(104, 204, 112)
    bounds.b = rect(220, 204, 116)
    await nextTick()
    observer.notify(firstOption)
    await nextTick()
    expect(highlight.element.style.transform).toBe('translate3d(120px, 4px, 0)')

    // A font/layout resize can occur without any Vue prop or slot update.
    bounds.a = rect(104, 204, 96)
    bounds.b = rect(204, 204, 116)
    observer.notify(firstOption)
    await nextTick()
    expect(highlight.element.style.transform).toBe('translate3d(104px, 4px, 0)')
  })

  it('tracks added and removed options, including removal of the selected option', async () => {
    const { wrapper, selected, options } = mountTabs()
    await nextTick()
    const observer = ControlledResizeObserver.instances[0]!
    const removedOption = wrapper.get('[data-box="a"]').element

    bounds.c = rect(300, 204, 96)
    options.value.push({ id: 'c', label: '已完成' })
    selected.value = 'c'
    await nextTick()
    await nextTick()
    const addedOption = wrapper.get('[data-box="c"]').element
    expect(observer.targets.has(addedOption)).toBe(true)
    expect(wrapper.get<HTMLElement>('[data-testid="highlight"]').element.style.transform)
      .toBe('translate3d(200px, 4px, 0)')

    options.value = options.value.filter(option => option.id !== 'a')
    await nextTick()
    expect(observer.unobserve).toHaveBeenCalledWith(removedOption)
    expect(observer.targets.has(removedOption)).toBe(false)

    options.value = options.value.filter(option => option.id !== 'c')
    await nextTick()
    await nextTick()
    expect(observer.targets.has(addedOption)).toBe(false)
    expect(wrapper.get('[data-testid="highlight"]').isVisible()).toBe(false)
  })

  it('accounts for a bordered, scrolled track and options wrapping onto another row', async () => {
    const { wrapper, selected } = mountTabs()
    await nextTick()
    const track = wrapper.get<HTMLElement>('nav').element
    Object.defineProperties(track, {
      clientLeft: { configurable: true, value: 2 },
      clientTop: { configurable: true, value: 1 },
    })
    track.scrollLeft = 20
    track.scrollTop = 10
    bounds.b = rect(106, 240, 116)
    selected.value = 'b'
    await nextTick()
    await nextTick()

    expect(wrapper.get<HTMLElement>('[data-testid="highlight"]').element.style.transform)
      .toBe('translate3d(24px, 49px, 0)')
  })

  it('keeps layout-sized positioning inside an ancestor scaled to 0.8 during entrance', async () => {
    const { wrapper, selected } = mountTabs()
    await nextTick()
    const track = wrapper.get<HTMLElement>('nav').element
    Object.defineProperties(track, {
      offsetWidth: { configurable: true, value: 300 },
      offsetHeight: { configurable: true, value: 50 },
    })
    // Layout is 300 × 50, but a scaled dialog paints it at 240 × 40.
    bounds.track = rect(100, 200, 240, 40)
    bounds.b = rect(164, 208, 96, 24)
    selected.value = 'b'
    await nextTick()
    await nextTick()

    const highlight = wrapper.get<HTMLElement>('[data-testid="highlight"]')
    expect(highlight.element.style.transform).toBe('translate3d(80px, 10px, 0)')
    expect(highlight.element.style.width).toBe('120px')
    expect(highlight.element.style.height).toBe('30px')

    // Finishing the parent entrance should not resize or move the background.
    bounds.track = rect(100, 200, 300, 50)
    bounds.b = rect(180, 210, 120, 30)
    ControlledResizeObserver.instances[0]!.notify(track)
    await nextTick()
    expect(highlight.element.style.transform).toBe('translate3d(80px, 10px, 0)')
    expect(highlight.element.style.width).toBe('120px')
    expect(highlight.element.style.height).toBe('30px')
  })

  it('waits until an initially hidden track becomes measurable', async () => {
    bounds.a = rect(0, 0, 0, 0)
    const { wrapper } = mountTabs()
    await nextTick()
    expect(wrapper.get('[data-testid="highlight"]').isVisible()).toBe(false)

    bounds.a = rect(104, 204, 72)
    ControlledResizeObserver.instances[0]!.notify(wrapper.get('nav').element)
    await nextTick()
    expect(wrapper.get('[data-testid="highlight"]').isVisible()).toBe(true)
    expect(wrapper.get<HTMLElement>('[data-testid="highlight"]').element.style.width).toBe('72px')
  })

  it('supports a custom selection attribute without ResizeObserver', async () => {
    vi.stubGlobal('ResizeObserver', undefined)
    const { wrapper } = mountTabs('data-current', '[data-current="true"]')
    await nextTick()
    await wrapper.get('[data-box="b"]').trigger('click')
    await nextTick()
    expect(wrapper.get<HTMLElement>('[data-testid="highlight"]').element.style.transform)
      .toBe('translate3d(80px, 4px, 0)')
  })

  it('disconnects resize observation when removed', async () => {
    const { wrapper } = mountTabs()
    await nextTick()
    const observer = ControlledResizeObserver.instances[0]!
    expect(observer.targets.size).toBe(3)

    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(observer.disconnect).toHaveBeenCalledOnce()
    expect(observer.targets.size).toBe(0)
  })
})

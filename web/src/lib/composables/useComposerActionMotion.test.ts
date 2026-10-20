// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useComposerActionMotion } from './useComposerActionMotion'

const NATURAL_WIDTH = 120
const mounted = new Set<VueWrapper>()

function mediaPreference() {
  const events = new EventTarget()
  return {
    matches: false,
    addEventListener: vi.fn((type: string, listener: EventListener) => events.addEventListener(type, listener)),
    removeEventListener: vi.fn((type: string, listener: EventListener) => events.removeEventListener(type, listener)),
    change(matches: boolean) {
      this.matches = matches
      events.dispatchEvent(new Event('change'))
    },
  }
}

function animationFrame(keyframes: Keyframe[]) {
  const first = keyframes[0]!
  const animation: {
    keyframes: Keyframe[]
    width: number
    opacity: string
    active: boolean
    onfinish: (() => void) | null
    cancel: () => void
  } = {
    keyframes,
    width: Number.parseFloat(String(first.width)),
    opacity: String(first.opacity),
    active: true,
    onfinish: null,
    cancel: vi.fn(),
  }
  vi.mocked(animation.cancel).mockImplementation(() => { animation.active = false })
  return animation
}

function mountMotion() {
  let motion!: ReturnType<typeof useComposerActionMotion>
  const wrapper = mount(defineComponent({
    setup() {
      motion = useComposerActionMotion()
      return () => h('span', [h('button', 'Confirm')])
    },
  }), { attachTo: document.body })
  mounted.add(wrapper)

  const node = wrapper.element as HTMLElement
  const animations: ReturnType<typeof animationFrame>[] = []
  const current = () => animations.findLast((animation) => animation.active)
  const nativeStyle = window.getComputedStyle.bind(window)
  vi.stubGlobal('getComputedStyle', (element: Element, pseudo?: string | null) => {
    const style = nativeStyle(element, pseudo)
    const frame = element === node ? current() : undefined
    if (!frame) return style
    return new Proxy(style, {
      get(target, key) {
        if (key === 'width') return `${frame.width}px`
        if (key === 'opacity') return frame.opacity
        return Reflect.get(target, key, target)
      },
    })
  })
  vi.spyOn(node, 'getBoundingClientRect').mockImplementation(() => {
    const width = current()?.width ?? (node.style.width ? Number.parseFloat(node.style.width) : NATURAL_WIDTH)
    return { x: 0, y: 0, left: 0, right: width, top: 0, bottom: 36, width, height: 36, toJSON: () => ({}) }
  })
  node.animate = vi.fn((keyframes: Keyframe[] | PropertyIndexedKeyframes | null) => {
    if (!Array.isArray(keyframes)) throw new Error('Expected a keyframe sequence')
    const animation = animationFrame(keyframes)
    animations.push(animation)
    return animation as unknown as Animation
  })
  return {
    motion,
    node,
    animations,
    unmount() {
      wrapper.unmount()
      mounted.delete(wrapper)
    },
  }
}

describe('useComposerActionMotion lifecycle', () => {
  let preference: ReturnType<typeof mediaPreference>

  beforeEach(() => {
    preference = mediaPreference()
    vi.stubGlobal('matchMedia', vi.fn(() => preference))
  })

  afterEach(() => {
    for (const wrapper of mounted) wrapper.unmount()
    mounted.clear()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('reverses from the rendered frame and ignores a superseded completion', () => {
    const { motion, node, animations, unmount } = mountMotion()
    const left = vi.fn()
    const entered = vi.fn()
    motion.leave(node, left)
    const outgoing = animations[0]!
    const staleFinish = outgoing.onfinish!

    // Model a browser-composited frame partway through the leave animation.
    outgoing.width = 54
    outgoing.opacity = '0.45'
    expect(node.getBoundingClientRect().width).toBe(54)
    motion.cancel(node)
    expect(outgoing.cancel).toHaveBeenCalledTimes(1)
    expect(outgoing.onfinish).toBeNull()
    expect(left).not.toHaveBeenCalled()

    motion.enter(node, entered)
    const incoming = animations[1]!
    expect(Number.parseFloat(String(incoming.keyframes[0]!.width))).toBe(54)
    expect(Number(incoming.keyframes[0]!.opacity)).toBe(0.45)
    expect(node.getBoundingClientRect().width).toBe(54)
    staleFinish()
    expect(left).not.toHaveBeenCalled()
    expect(entered).not.toHaveBeenCalled()
    expect(incoming.cancel).not.toHaveBeenCalled()

    const finish = incoming.onfinish!
    finish()
    finish()
    staleFinish()
    expect(entered).toHaveBeenCalledTimes(1)
    expect(left).not.toHaveBeenCalled()
    expect(incoming.cancel).toHaveBeenCalledTimes(1)
    expect(node.getBoundingClientRect().width).toBe(NATURAL_WIDTH)
    unmount()
  })

  it('settles active motion when reduced motion is enabled and can animate again afterward', () => {
    const { motion, node, animations, unmount } = mountMotion()
    const done = vi.fn()
    motion.leave(node, done)
    const active = animations[0]!
    const queuedFinish = active.onfinish!
    active.width = 72
    active.opacity = '0.6'

    preference.change(true)
    expect(done).toHaveBeenCalledTimes(1)
    expect(active.cancel).toHaveBeenCalledTimes(1)
    expect(active.onfinish).toBeNull()
    expect(node.getBoundingClientRect().width).toBe(NATURAL_WIDTH)
    queuedFinish()
    expect(done).toHaveBeenCalledTimes(1)

    const reducedDone = vi.fn()
    motion.enter(node, reducedDone)
    expect(reducedDone).toHaveBeenCalledTimes(1)
    expect(node.animate).toHaveBeenCalledTimes(1)

    preference.change(false)
    const restoredDone = vi.fn()
    motion.enter(node, restoredDone)
    expect(node.animate).toHaveBeenCalledTimes(2)
    expect(restoredDone).not.toHaveBeenCalled()
    animations[1]!.onfinish!()
    expect(restoredDone).toHaveBeenCalledTimes(1)
    unmount()
  })

  it('cancels active animations and removes the preference listener on unmount', () => {
    const { motion, node, animations, unmount } = mountMotion()
    const done = vi.fn()
    motion.enter(node, done)
    const active = animations[0]!
    const queuedFinish = active.onfinish!
    const listener = preference.addEventListener.mock.calls[0]![1]

    unmount()
    expect(preference.removeEventListener).toHaveBeenCalledExactlyOnceWith('change', listener)
    expect(active.cancel).toHaveBeenCalledTimes(1)
    expect(active.onfinish).toBeNull()
    expect(done).not.toHaveBeenCalled()
    queuedFinish()
    preference.change(true)
    expect(done).not.toHaveBeenCalled()
    expect(active.cancel).toHaveBeenCalledTimes(1)
  })
})

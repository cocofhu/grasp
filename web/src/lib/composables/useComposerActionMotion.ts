import { onBeforeUnmount, onMounted } from 'vue'

/** Animate an optional composer action without reflowing its label mid-transition. */
export function useComposerActionMotion() {
  const running = new Map<HTMLElement, { animation: Animation; done: () => void }>()
  let preference: MediaQueryList | undefined

  function reset(node: HTMLElement) {
    for (const name of ['width', 'opacity', 'overflow']) node.style.removeProperty(name)
    const inner = node.firstElementChild as HTMLElement | null
    inner?.style.removeProperty('width')
    inner?.style.removeProperty('max-width')
  }

  function finish(node: HTMLElement) {
    const motion = running.get(node)
    if (!motion) return
    running.delete(node)
    motion.animation.onfinish = null
    motion.animation.cancel()
    reset(node)
    motion.done()
  }

  function cancel(el: Element) {
    const node = el as HTMLElement
    const motion = running.get(node)
    if (!motion) return
    // Preserve the current frame so a rapid reversal starts where it stopped.
    const frame = getComputedStyle(node)
    node.style.width = frame.width
    node.style.opacity = frame.opacity
    running.delete(node)
    motion.animation.onfinish = null
    motion.animation.cancel()
  }

  function animate(el: Element, showing: boolean, done: () => void) {
    const node = el as HTMLElement
    const fromWidth = node.style.width ? node.getBoundingClientRect().width : showing ? 0 : node.getBoundingClientRect().width
    const fromOpacity = node.style.opacity || (showing ? '0' : getComputedStyle(node).opacity)
    reset(node)
    const naturalWidth = node.getBoundingClientRect().width
    if (preference?.matches || typeof node.animate !== 'function') {
      done()
      return
    }
    const inner = node.firstElementChild as HTMLElement | null
    if (inner) {
      inner.style.width = `${naturalWidth}px`
      inner.style.maxWidth = 'none'
    }
    node.style.overflow = 'hidden'
    node.style.width = `${fromWidth}px`
    const animation = node.animate([
      { width: `${fromWidth}px`, opacity: fromOpacity },
      { width: `${showing ? naturalWidth : 0}px`, opacity: showing ? '1' : '0' },
    ], { duration: 280, easing: 'cubic-bezier(.22, 0, .18, 1)', fill: 'both' })
    running.set(node, { animation, done })
    animation.onfinish = () => {
      if (running.get(node)?.animation === animation) finish(node)
    }
  }

  function preferenceChanged() {
    if (preference?.matches) for (const node of [...running.keys()]) finish(node)
  }
  onMounted(() => {
    if (typeof window.matchMedia !== 'function') return
    preference = window.matchMedia('(prefers-reduced-motion: reduce)')
    preference.addEventListener('change', preferenceChanged)
  })
  onBeforeUnmount(() => {
    preference?.removeEventListener('change', preferenceChanged)
    for (const [node, motion] of running) {
      motion.animation.onfinish = null
      motion.animation.cancel()
      reset(node)
    }
    running.clear()
  })

  return {
    enter: (el: Element, done: () => void) => animate(el, true, done),
    leave: (el: Element, done: () => void) => animate(el, false, done),
    cancel,
  }
}

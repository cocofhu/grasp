import { nextTick, onBeforeUnmount, onMounted, readonly, ref, watch, type Ref } from 'vue'

export type IconMotionState = 'idle' | 'once' | 'busy'

export interface IconMotionOptions {
  duration: () => number
  busy?: () => boolean
  disabled?: () => boolean
  interactive?: () => boolean
  motionKey?: () => unknown
  /** Completed check marks deliberately ignore event and mount triggers. */
  eventMotion?: () => boolean
}

/** Explicit hosts also work for status rows that are intentionally not controls. */
export const iconMotionHostSelector = [
  '[data-icon-motion-host]', '.grasp-status-host', 'button', 'a[href]', 'summary',
  'input', 'select', 'textarea', '[role="button"]', '[role="tab"]',
  '[role="menuitem"]', '[role="option"]', '[tabindex]:not([tabindex="-1"])',
].join(',')

/**
 * The history title keeps a link and a sibling toggle. The nearer link would
 * otherwise own the clock, so this one heading is the hover host. Every other
 * page still uses the nearest control.
 */
const historyTitleHostSelector = '[data-icon-motion-span="history-title"]'

const disabledSelector = '[disabled], [aria-disabled="true"], [inert]'
const busySelector = '[aria-busy="true"], [data-busy="true"]'
const draggingSelector = '.vue-flow__node.dragging, [data-icon-dragging="true"]'

/**
 * Finite SVG feedback shared by glyphs and outer brand containers.
 * The closest control owns feedback; metadata has no automatic entry animation.
 */
export function useIconMotion(
  element: Ref<Element | null>,
  options: IconMotionOptions,
) {
  const state = ref<IconMotionState>('idle')
  const reduced = ref(false)
  let mounted = false
  let host: Element | null = null
  let hovered = false
  let focused = false
  let timer: ReturnType<typeof setTimeout> | undefined
  let observer: MutationObserver | undefined
  let media: MediaQueryList | undefined
  let generation = 0

  const disabled = () => options.disabled?.() === true || !!element.value?.closest(disabledSelector)
  const busy = () => options.busy?.() === true
  const blocked = () => disabled() || reduced.value || busy()
    || !!element.value?.closest(`${busySelector}, ${draggingSelector}`)
  const interactive = () => options.interactive?.() !== false
  const closestHost = (target: EventTarget | null) => {
    if (!(target instanceof Element)) return null
    return target.closest(historyTitleHostSelector) ?? target.closest(iconMotionHostSelector)
  }

  function clearTimer() {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
  }

  function reset() {
    generation++
    clearTimer()
    state.value = options.disabled?.() !== true && !reduced.value && busy() ? 'busy' : 'idle'
  }

  async function replay() {
    if (!mounted || blocked()) return
    const version = ++generation
    clearTimer()
    state.value = 'idle'
    // A rendered idle frame removes the previous CSS animation before replay.
    await nextTick()
    if (!mounted || version !== generation || blocked()) return
    // Resolve removal before re-adding the same animation during a new event.
    element.value?.getBoundingClientRect()
    state.value = 'once'
    timer = setTimeout(() => {
      timer = undefined
      if (version === generation) reset()
    }, Math.max(0, options.duration()) + 32)
  }

  function pointerOver(event: Event) {
    if (!interactive() || closestHost(event.target) !== host || hovered) return
    hovered = true
    if (!focused) void replay()
  }

  function pointerOut(event: Event) {
    if (!hovered || closestHost((event as PointerEvent).relatedTarget) === host) return
    hovered = false
    if (!focused) reset()
  }

  function focusIn(event: Event) {
    if (!interactive() || closestHost(event.target) !== host) return
    const target = event.target
    if (!(target instanceof Element) || !target.matches(':focus-visible')) return
    focused = true
    if (!hovered) void replay()
  }

  function focusOut(event: Event) {
    if (closestHost((event as FocusEvent).relatedTarget) === host) return
    focused = false
    if (!hovered) reset()
  }

  function detach() {
    host?.removeEventListener('pointerover', pointerOver)
    host?.removeEventListener('pointerout', pointerOut)
    host?.removeEventListener('focusin', focusIn)
    host?.removeEventListener('focusout', focusOut)
    observer?.disconnect()
    observer = undefined
    host = null
    hovered = false
    focused = false
  }

  function bind() {
    detach()
    if (!mounted) return
    host = closestHost(element.value)
    host?.addEventListener('pointerover', pointerOver)
    host?.addEventListener('pointerout', pointerOut)
    host?.addEventListener('focusin', focusIn)
    host?.addEventListener('focusout', focusOut)
    if (element.value && typeof MutationObserver !== 'undefined') {
      observer = new MutationObserver(() => { if (blocked()) reset() })
      // Disabled and busy semantics may be supplied by a fieldset or wrapper.
      // Observe only this icon's ancestor chain, never unrelated subtrees.
      for (let ancestor: Element | null = element.value; ancestor; ancestor = ancestor.parentElement) {
        observer.observe(ancestor, {
          attributes: true,
        attributeFilter: ['disabled', 'aria-disabled', 'aria-busy', 'data-busy', 'inert', 'class', 'data-icon-dragging'],
        })
      }
    }
    reset()
  }

  function mediaChange() {
    reduced.value = media?.matches ?? false
    reset()
  }

  onMounted(() => {
    mounted = true
    media = window.matchMedia?.('(prefers-reduced-motion: reduce)')
    reduced.value = media?.matches ?? false
    media?.addEventListener('change', mediaChange)
    bind()
  })

  watch(element, () => {
    if (closestHost(element.value) !== host) bind()
  }, { flush: 'post' })
  watch(() => [options.busy?.(), options.disabled?.(), options.interactive?.()], reset)
  watch(() => options.motionKey?.(), (value, previous) => {
    if (value !== previous && value !== undefined && options.eventMotion?.() !== false) void replay()
  })

  onBeforeUnmount(() => {
    mounted = false
    detach()
    clearTimer()
    generation++
    media?.removeEventListener('change', mediaChange)
  })

  return { state: readonly(state), replay, reset }
}

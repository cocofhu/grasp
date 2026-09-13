import type { Directive, DirectiveBinding } from 'vue'
import { canHoverInk, farthestCornerDiameter, HOVER_INK_DURATION_MS } from './hoverInkGeometry'

const INK_CLASS = 'hover-ink'
const HOST_CLASS = 'hover-ink-host'
const HOVER_CLASS = 'is-hover-ink'
const LEAVE_CLASS = 'is-leave-ink'

type HoverInkPhase = 'idle' | 'hover' | 'leave'

type HoverInkState = {
  cleanup: () => void
  binding: DirectiveBinding
  phase: HoverInkPhase
  collapseTimer?: ReturnType<typeof setTimeout>
}

const stateMap = new WeakMap<HTMLElement, HoverInkState>()

function ensureInk(el: HTMLElement): HTMLElement {
  let ink = el.querySelector(`:scope > .${INK_CLASS}`) as HTMLElement | null
  if (!ink) {
    ink = document.createElement('span')
    ink.className = INK_CLASS
    ink.setAttribute('aria-hidden', 'true')
    el.insertBefore(ink, el.firstChild)
  }
  return ink
}

/** Block when disabled, aria-busy/loading, or binding says off. */
export function isHoverInkBlocked(el: HTMLElement, binding?: DirectiveBinding): boolean {
  if (binding?.value === false) return true
  if (binding?.value && typeof binding.value === 'object' && 'enabled' in binding.value) {
    if ((binding.value as { enabled?: boolean }).enabled === false) return true
  }
  if ((el as HTMLButtonElement).disabled || el.hasAttribute('disabled')) return true
  if (el.getAttribute('aria-busy') === 'true') return true
  if (el.getAttribute('aria-disabled') === 'true') return true
  return false
}

function clearCollapseTimer(el: HTMLElement) {
  const st = stateMap.get(el)
  if (st?.collapseTimer != null) {
    clearTimeout(st.collapseTimer)
    st.collapseTimer = undefined
  }
}

/** Re-apply imperative host/ink DOM after Vue's patchClass overwrites className. */
function restoreHostState(el: HTMLElement) {
  el.classList.add(HOST_CLASS)
  ensureInk(el)
  const st = stateMap.get(el)
  if (!st) return
  el.classList.toggle(HOVER_CLASS, st.phase === 'hover')
  el.classList.toggle(LEAVE_CLASS, st.phase === 'leave')
}

function placeInk(el: HTMLElement, clientX: number, clientY: number) {
  clearCollapseTimer(el)
  const rect = el.getBoundingClientRect()
  const x = clientX - rect.left
  const y = clientY - rect.top
  const d = farthestCornerDiameter(rect.width, rect.height, x, y)
  el.style.setProperty('--ink-x', `${x}px`)
  el.style.setProperty('--ink-y', `${y}px`)
  el.style.setProperty('--ink-d', `${d}px`)
}

function expand(el: HTMLElement) {
  clearCollapseTimer(el)
  const st = stateMap.get(el)
  if (st) st.phase = 'hover'
  restoreHostState(el)
}

/** Hide paint, then collapse --ink-d so the layout box cannot stick (plan g1.2). */
function retract(el: HTMLElement) {
  const st = stateMap.get(el)
  if (st) st.phase = 'leave'
  restoreHostState(el)
  clearCollapseTimer(el)
  const collapse = () => {
    // Only collapse if still left (not re-entered).
    if (st?.phase === 'hover') return
    el.style.setProperty('--ink-d', '0px')
    if (st) st.collapseTimer = undefined
  }
  if (st) {
    st.collapseTimer = setTimeout(collapse, HOVER_INK_DURATION_MS)
  } else {
    collapse()
  }
}

function bindHoverInk(el: HTMLElement, binding: DirectiveBinding) {
  const existing = stateMap.get(el)
  if (existing) {
    existing.binding = binding
    restoreHostState(el)
    return
  }

  const onEnter = (ev: PointerEvent) => {
    if (!canHoverInk()) return
    const st = stateMap.get(el)
    if (!st || isHoverInkBlocked(el, st.binding)) return
    // Touch must not leave sticky ink (plan g1.3).
    if (ev.pointerType === 'touch') return
    placeInk(el, ev.clientX, ev.clientY)
    expand(el)
  }
  const onLeave = () => {
    retract(el)
  }

  // Window blur / tab hide: treat as leave so ink does not stick (edge_cases / review v4).
  const onBlurOrHide = () => {
    retract(el)
  }
  const onVisibility = () => {
    if (document.visibilityState === 'hidden') retract(el)
  }

  el.addEventListener('pointerenter', onEnter)
  el.addEventListener('pointerleave', onLeave)
  el.addEventListener('pointercancel', onLeave)
  window.addEventListener('blur', onBlurOrHide)
  document.addEventListener('visibilitychange', onVisibility)

  stateMap.set(el, {
    binding,
    phase: 'idle',
    cleanup: () => {
      el.removeEventListener('pointerenter', onEnter)
      el.removeEventListener('pointerleave', onLeave)
      el.removeEventListener('pointercancel', onLeave)
      window.removeEventListener('blur', onBlurOrHide)
      document.removeEventListener('visibilitychange', onVisibility)
      clearCollapseTimer(el)
      el.classList.remove(HOVER_CLASS)
      el.classList.add(LEAVE_CLASS)
      el.style.setProperty('--ink-d', '0px')
      stateMap.delete(el)
    },
  })
  restoreHostState(el)
}

/** Vue directive: landing-point solid circle hover cover (plan g1). */
export const vHoverInk: Directive = {
  mounted(el, binding) {
    bindHoverInk(el as HTMLElement, binding)
  },
  updated(el, binding) {
    const host = el as HTMLElement
    const st = stateMap.get(host)
    if (st) {
      st.binding = binding
      restoreHostState(host)
    } else {
      bindHoverInk(host, binding)
    }
  },
  unmounted(el) {
    const host = el as HTMLElement
    stateMap.get(host)?.cleanup()
    host.classList.remove(HOST_CLASS, HOVER_CLASS, LEAVE_CLASS)
  },
}

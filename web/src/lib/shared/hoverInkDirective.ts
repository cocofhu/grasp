import type { Directive, DirectiveBinding } from 'vue'
import { canHoverInk, farthestCornerDiameter } from './hoverInkGeometry'

const INK_CLASS = 'hover-ink'
const HOST_CLASS = 'hover-ink-host'
const HOVER_CLASS = 'is-hover-ink'
const LEAVE_CLASS = 'is-leave-ink'

type HoverInkState = {
  cleanup: () => void
  binding: DirectiveBinding
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

function placeInk(el: HTMLElement, clientX: number, clientY: number) {
  const rect = el.getBoundingClientRect()
  const x = clientX - rect.left
  const y = clientY - rect.top
  const d = farthestCornerDiameter(rect.width, rect.height, x, y)
  el.style.setProperty('--ink-x', `${x}px`)
  el.style.setProperty('--ink-y', `${y}px`)
  el.style.setProperty('--ink-d', `${d}px`)
}

function expand(el: HTMLElement) {
  el.classList.remove(LEAVE_CLASS)
  el.classList.add(HOVER_CLASS)
}

function retract(el: HTMLElement) {
  el.classList.remove(HOVER_CLASS)
  el.classList.add(LEAVE_CLASS)
}

function bindHoverInk(el: HTMLElement, binding: DirectiveBinding) {
  const existing = stateMap.get(el)
  if (existing) {
    existing.binding = binding
    return
  }

  el.classList.add(HOST_CLASS)
  ensureInk(el)

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

  el.addEventListener('pointerenter', onEnter)
  el.addEventListener('pointerleave', onLeave)
  el.addEventListener('pointercancel', onLeave)

  stateMap.set(el, {
    binding,
    cleanup: () => {
      el.removeEventListener('pointerenter', onEnter)
      el.removeEventListener('pointerleave', onLeave)
      el.removeEventListener('pointercancel', onLeave)
      retract(el)
      stateMap.delete(el)
    },
  })
}

/** Vue directive: landing-point solid circle hover cover (plan g1). */
export const vHoverInk: Directive = {
  mounted(el, binding) {
    bindHoverInk(el as HTMLElement, binding)
  },
  updated(el, binding) {
    const st = stateMap.get(el as HTMLElement)
    if (st) st.binding = binding
    else bindHoverInk(el as HTMLElement, binding)
  },
  unmounted(el) {
    const host = el as HTMLElement
    stateMap.get(host)?.cleanup()
    host.classList.remove(HOST_CLASS, HOVER_CLASS, LEAVE_CLASS)
  },
}

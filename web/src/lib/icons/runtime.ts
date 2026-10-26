/** Native-DOM adapter for the same icon geometry and motion used by Icon.vue. */
import definitions from './definitions.json'
import motionCSS from '../../styles/icon-motion.css?inline'

type Definition = { body: string; duration: number; viewBox?: string; filled?: boolean; strokeWidth?: number }
const glyphs = definitions as Record<string, Definition>
const isKnownIcon = (name: string) => Object.hasOwn(glyphs, name)
const HOST = 'button,a[href],summary,[role="button"],[role="tab"],[data-icon-host]'
const ICON = '.app-icon[data-icon]'
const escapeAttribute = (value: string) => value.replace(/[&"<>]/g, (c) => ({ '&': '&amp;', '"': '&quot;', '<': '&lt;', '>': '&gt;' })[c]!)

export type RuntimeIconOptions = { className?: string; busy?: boolean; enter?: boolean }

/** All names and paths come from the checked-in catalog; caller content never becomes SVG markup. */
export function runtimeIcon(name: string, size = 16, options: RuntimeIconOptions = {}): string {
  const key = isKnownIcon(name) ? name : 'help'
  const glyph = glyphs[key]
  return `<svg class="app-icon ${escapeAttribute(options.className || '')}" data-icon="${key}"${options.busy ? ' data-icon-busy="true" data-icon-motion="busy"' : ''}${options.enter && !options.busy && key !== 'check-draw' ? ' data-icon-motion="once"' : ''} width="${Number(size) || 16}" height="${Number(size) || 16}" viewBox="${glyph.viewBox || '0 0 24 24'}" fill="${glyph.filled ? 'currentColor' : 'none'}" stroke="${glyph.strokeWidth === 0 ? 'none' : 'currentColor'}" stroke-width="${glyph.strokeWidth ?? 1.7}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${glyph.body}</svg>`
}

/** Progressive enhancement for native HTML, including cloned template contents. */
export function hydrateRuntimeIcons(root: ParentNode): void {
  root.querySelectorAll<HTMLElement>('[data-runtime-icon]').forEach((node) => {
    const size = Number(node.dataset.iconSize || 16)
    const holder = node.ownerDocument.createElement('span')
    holder.innerHTML = runtimeIcon(node.dataset.runtimeIcon || '', size, { className: node.className })
    node.replaceWith(holder.firstElementChild!)
  })
}

/** One delegated controller per DOM/shadow root. Pointer and keyboard ownership share one play. */
export function installRuntimeIconMotion(root: Document | ShadowRoot | HTMLElement): () => void {
  const doc = root.ownerDocument || root as Document
  const win = doc.defaultView!
  const style = doc.createElement('style')
  style.dataset.graspIconMotion = ''
  style.textContent = '.app-icon{vertical-align:middle;flex:none;overflow:visible}.app-icon[data-icon-busy="true"]{transform-origin:center}' + motionCSS
  if (root.nodeType === 9) (root as Document).head.appendChild(style)
  else root.appendChild(style)
  const reduced = win.matchMedia?.('(prefers-reduced-motion: reduce)')
  const owners = new Map<Element, { pointer: boolean; focus: boolean }>()
  const running = new Map<SVGElement, number>()
  const disabled = (owner: Element) => !!owner.closest(':disabled,[aria-disabled="true"],[aria-busy="true"],[data-icon-disabled],[data-icon-dragging]')
  const hostFor = (target: EventTarget | null): Element | null => target instanceof win.Element ? target.closest(HOST) : null
  const stop = (icon: SVGElement) => {
    if (icon.dataset.iconMotion === 'busy') return
    win.clearTimeout(running.get(icon))
    running.delete(icon)
    icon.removeAttribute('data-icon-motion')
  }
  const iconsOf = (owner: Element) => Array.from(owner.querySelectorAll<SVGElement>(ICON)).filter((icon) => icon.closest(HOST) === owner)
  const stopOwner = (owner: Element) => iconsOf(owner).forEach(stop)
  const enter = (owner: Element, kind: 'pointer' | 'focus') => {
    const state = owners.get(owner) || { pointer: false, focus: false }
    const active = state.pointer || state.focus
    state[kind] = true
    owners.set(owner, state)
    if (active || disabled(owner) || reduced?.matches) return
    for (const icon of iconsOf(owner)) {
      if (icon.dataset.iconBusy === 'true' || !isKnownIcon(icon.dataset.icon || '')) continue
      icon.dataset.iconMotion = 'once'
      running.set(icon, win.setTimeout(() => stop(icon), glyphs[icon.dataset.icon!].duration + 40))
    }
  }
  const leave = (owner: Element, kind: 'pointer' | 'focus') => {
    const state = owners.get(owner)
    if (!state) return
    state[kind] = false
    if (!state.pointer && !state.focus) {
      owners.delete(owner)
      stopOwner(owner)
    }
  }
  const pointerover = (event: Event) => {
    if ((event as PointerEvent).pointerType === 'touch') return
    const owner = hostFor(event.target)
    if (owner && hostFor((event as PointerEvent).relatedTarget) !== owner) enter(owner, 'pointer')
  }
  const pointerout = (event: Event) => {
    const owner = hostFor(event.target)
    if (owner && hostFor((event as PointerEvent).relatedTarget) !== owner) leave(owner, 'pointer')
  }
  const focusin = (event: Event) => {
    const owner = hostFor(event.target)
    if (owner?.matches(':focus-visible')) enter(owner, 'focus')
  }
  const focusout = (event: Event) => {
    const owner = hostFor(event.target)
    if (owner && hostFor((event as FocusEvent).relatedTarget) !== owner) leave(owner, 'focus')
  }
  const cancelDragging = (event: Event) => {
    const owner = hostFor(event.target)
    if (owner?.matches('[data-icon-host]')) stopOwner(owner)
  }
  const cancelMotion = () => running.forEach((_timer, icon) => stop(icon))
  const observer = new win.MutationObserver(() => {
    owners.forEach((_state, owner) => {
      if (!owner.isConnected || disabled(owner)) stopOwner(owner)
      if (!owner.isConnected) owners.delete(owner)
    })
  })
  observer.observe(root, { subtree: true, childList: true, attributes: true, attributeFilter: ['disabled', 'aria-disabled', 'aria-busy', 'data-icon-dragging'] })
  root.addEventListener('pointerover', pointerover)
  root.addEventListener('pointerout', pointerout)
  root.addEventListener('focusin', focusin)
  root.addEventListener('focusout', focusout)
  root.addEventListener('pointerdown', cancelDragging)
  reduced?.addEventListener?.('change', cancelMotion)
  return () => {
    cancelMotion()
    observer.disconnect()
    owners.clear()
    root.removeEventListener('pointerover', pointerover)
    root.removeEventListener('pointerout', pointerout)
    root.removeEventListener('focusin', focusin)
    root.removeEventListener('focusout', focusout)
    root.removeEventListener('pointerdown', cancelDragging)
    reduced?.removeEventListener?.('change', cancelMotion)
    style.remove()
  }
}

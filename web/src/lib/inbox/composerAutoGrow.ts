/**
 * Composer textarea height follows visible line count at the current width:
 * empty draft → placeholder wrap; with draft → soft + hard wrap.
 * Cap is only an overflow ceiling (wide empty must not sit at max).
 */

export const CLARIFY_AUTO_GROW_MIN = 40
export const CLARIFY_AUTO_GROW_MAX = 128

export const PARAGRAPH_AUTO_GROW_MIN = 72
/** Height-constrained hosts (mobile gate drawer) start at two lines instead of three. */
export const PARAGRAPH_AUTO_GROW_MIN_COMPACT = 40
export const PARAGRAPH_AUTO_GROW_MAX = 320

/** Measure how tall `text` is when wrapped to the textarea's current content box. */
export function measureWrappedTextHeight(el: HTMLTextAreaElement, text: string): number {
  const width = el.clientWidth
  if (width <= 0) return 0
  const cs = getComputedStyle(el)
  const sizer = document.createElement('div')
  sizer.setAttribute('data-composer-sizer', '')
  sizer.style.position = 'absolute'
  sizer.style.left = '-9999px'
  sizer.style.top = '0'
  sizer.style.visibility = 'hidden'
  sizer.style.pointerEvents = 'none'
  sizer.style.whiteSpace = 'pre-wrap'
  sizer.style.overflowWrap = 'anywhere'
  sizer.style.wordBreak = 'break-word'
  sizer.style.width = `${width}px`
  sizer.style.font = cs.font
  sizer.style.fontSize = cs.fontSize
  sizer.style.fontFamily = cs.fontFamily
  sizer.style.fontWeight = cs.fontWeight
  sizer.style.letterSpacing = cs.letterSpacing
  sizer.style.lineHeight = cs.lineHeight
  sizer.style.padding = cs.padding
  sizer.style.border = cs.border
  sizer.style.boxSizing = cs.boxSizing as string
  sizer.textContent = text.length ? text : '\u00a0'
  document.body.appendChild(sizer)
  const h = sizer.offsetHeight
  sizer.remove()
  return h
}

type ComposerAutoGrowOpts = {
  min: number
  max: number
  /** Placeholder / hint used when the draft is empty. */
  emptyHint?: string
}

/**
 * Set textarea height from visible lines. Returns whether content exceeds max
 * (caller should enable inner scroll).
 */
export function applyComposerAutoGrow(el: HTMLTextAreaElement, opts: ComposerAutoGrowOpts): boolean {
  const draft = el.value
  el.style.height = 'auto'
  const wrapSource = draft || opts.emptyHint || el.placeholder || ''
  const wrapH = measureWrappedTextHeight(el, wrapSource)
  // Unlaid-out / tests (clientWidth 0): wrapH is 0 → fall back to native scrollHeight.
  const contentH = draft
    ? Math.max(el.scrollHeight, wrapH)
    : wrapH > 0
      ? wrapH
      : el.scrollHeight
  const h = Math.min(Math.max(contentH, opts.min), opts.max)
  el.style.height = `${h}px`
  return contentH > opts.max
}

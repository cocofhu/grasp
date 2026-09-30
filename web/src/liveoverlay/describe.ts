/** Element description the agent uses to find the picked element in source. */
export type LiveElement = {
  selector: string
  tagName: string
  id?: string
  classes?: string[]
  text?: string
  outerHTML?: string
  styles?: Record<string, string>
}

const MAX_TEXT = 120
const MAX_HTML = 1024
const STYLE_KEYS = [
  'color',
  'background-color',
  'font-family',
  'font-size',
  'font-weight',
  'line-height',
  'letter-spacing',
  'padding',
  'margin',
  'border-radius',
  'border',
  'box-shadow',
  'display',
  'gap',
  'grid-template-columns',
  'flex-direction',
]

function cssEscape(s: string): string {
  const esc = (globalThis as { CSS?: { escape?: (v: string) => string } }).CSS?.escape
  return esc ? esc(s) : s.replace(/[^a-zA-Z0-9_-]/g, (c) => `\\${c}`)
}

/** A CSS path from the nearest id (or body) down to el, using nth-of-type. */
export function selectorPath(el: Element): string {
  const parts: string[] = []
  let cur: Element | null = el
  while (cur && cur.nodeType === 1 && cur !== document.documentElement) {
    const tag = cur.tagName.toLowerCase()
    if (cur.id && !/^\d/.test(cur.id)) {
      parts.unshift(`${tag}#${cssEscape(cur.id)}`)
      break
    }
    if (tag === 'body') {
      parts.unshift('body')
      break
    }
    const parent: Element | null = cur.parentElement
    let part = tag
    if (parent) {
      const same = Array.from(parent.children).filter((c) => c.tagName === cur!.tagName)
      if (same.length > 1) part += `:nth-of-type(${same.indexOf(cur) + 1})`
    }
    parts.unshift(part)
    cur = parent
  }
  return parts.join(' > ')
}

export function visibleText(el: Element): string {
  const raw = (el as HTMLElement).innerText ?? el.textContent ?? ''
  const t = raw.replace(/\s+/g, ' ').trim()
  return t.length > MAX_TEXT ? t.slice(0, MAX_TEXT) : t
}

function clip(s: string, max: number): string {
  return s.length > max ? s.slice(0, max) : s
}

export function describeElement(el: Element): LiveElement {
  const out: LiveElement = { selector: selectorPath(el), tagName: el.tagName.toLowerCase() }
  if (el.id) out.id = el.id
  const classes = Array.from(el.classList).filter((c) => !c.startsWith('__hp-')).slice(0, 12)
  if (classes.length) out.classes = classes
  const text = visibleText(el)
  if (text) out.text = text
  const html = el.outerHTML.replace(/\s+/g, ' ').replace(/ class="__hp-[^"]*"/g, '')
  if (html) out.outerHTML = clip(html, MAX_HTML)
  try {
    const cs = getComputedStyle(el)
    const styles: Record<string, string> = {}
    for (const k of STYLE_KEYS) {
      const v = cs.getPropertyValue(k)
      if (v && v !== 'normal' && v !== 'none' && v !== '0px') styles[k] = clip(v.trim(), 120)
    }
    if (Object.keys(styles).length) out.styles = styles
  } catch {
    // Detached or exotic element: no styles.
  }
  return out
}

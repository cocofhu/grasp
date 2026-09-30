/** Finds Live preview wrappers the agent wrote into source (rendered by HMR). */

export type ParamOption = { value: string; label?: string }

export type LiveParam = {
  id: string
  kind: 'range' | 'steps' | 'toggle'
  label?: string
  min?: number
  max?: number
  step?: number
  unit?: string
  default?: string | number
  options?: ParamOption[]
}

export type VariantEl = { n: number; el: HTMLElement; label: string; params: LiveParam[] }

export type Wrapper = { sid: string; el: HTMLElement; original: HTMLElement | null; variants: VariantEl[] }

const PARAM_ID = /^[a-z][a-z0-9-]{0,31}$/

export function parseParams(raw: string | null): LiveParam[] {
  if (!raw) return []
  let v: unknown
  try {
    v = JSON.parse(raw)
  } catch {
    return []
  }
  if (!Array.isArray(v)) return []
  const out: LiveParam[] = []
  for (const it of v.slice(0, 6)) {
    if (!it || typeof it !== 'object') continue
    const p = it as Record<string, unknown>
    const id = typeof p.id === 'string' ? p.id : ''
    const kind = p.kind
    if (!PARAM_ID.test(id) || (kind !== 'range' && kind !== 'steps' && kind !== 'toggle')) continue
    const param: LiveParam = { id, kind }
    if (typeof p.label === 'string') param.label = p.label.slice(0, 24)
    for (const k of ['min', 'max', 'step'] as const) if (typeof p[k] === 'number') param[k] = p[k] as number
    if (typeof p.unit === 'string' && /^[a-z%]{0,4}$/.test(p.unit)) param.unit = p.unit
    if (typeof p.default === 'string' || typeof p.default === 'number') param.default = p.default
    if (Array.isArray(p.options)) {
      param.options = p.options
        .filter((o): o is Record<string, unknown> => !!o && typeof o === 'object' && typeof (o as Record<string, unknown>).value === 'string')
        .slice(0, 6)
        .map((o) => ({ value: String(o.value), label: typeof o.label === 'string' ? o.label.slice(0, 16) : undefined }))
    }
    if (kind === 'range' && (param.min === undefined || param.max === undefined)) continue
    if (kind === 'steps' && !param.options?.length) continue
    out.push(param)
  }
  return out
}

/** Direct variant children of a wrapper (display:contents keeps them as children). */
export function readWrapper(el: HTMLElement): Wrapper | null {
  const sid = el.getAttribute('data-grasp-live') || ''
  if (!/^[A-Za-z0-9_-]{6,64}$/.test(sid)) return null
  let original: HTMLElement | null = null
  const variants: VariantEl[] = []
  for (const child of Array.from(el.children)) {
    const raw = child.getAttribute('data-grasp-variant')
    if (raw === null || !/^\d+$/.test(raw)) continue
    const n = Number(raw)
    const h = child as HTMLElement
    if (n === 0) {
      original = h
      continue
    }
    variants.push({ n, el: h, label: (child.getAttribute('data-grasp-variant-label') || '').slice(0, 16), params: parseParams(child.getAttribute('data-grasp-params')) })
  }
  variants.sort((a, b) => a.n - b.n)
  return { sid, el, original, variants }
}

export function scanWrappers(root: ParentNode = document): Wrapper[] {
  const out: Wrapper[] = []
  root.querySelectorAll<HTMLElement>('[data-grasp-live]').forEach((el) => {
    const w = readWrapper(el)
    if (w) out.push(w)
  })
  return out
}

/** Value applied to a param: CSS var for range, data attribute otherwise. */
export function applyParam(el: HTMLElement, p: LiveParam, value: string | number) {
  if (p.kind === 'range') el.style.setProperty(`--gp-${p.id}`, `${value}${p.unit || ''}`)
  else el.setAttribute(`data-gp-${p.id}`, String(value))
}

export function paramDefault(p: LiveParam): string | number {
  if (p.default !== undefined) return p.default
  if (p.kind === 'range') return p.min ?? 0
  if (p.kind === 'toggle') return 'off'
  return p.options?.[0]?.value ?? ''
}

/** Show variant n (0 = original) in place, hiding the others. */
export function showVariant(w: Wrapper, n: number) {
  const previouslyVisible = w.variants.filter((v) => !v.el.hidden)
  const originalVisible = !!w.original && !w.original.hidden
  if (w.original) setVariantVisible(w.original, n === 0)
  for (const v of w.variants) setVariantVisible(v.el, v.n === n)
  const selected = w.variants.find((v) => v.n === n)
  // Fade only a candidate-to-candidate switch. Compare and original peek stay immediate.
  if (selected && !originalVisible && previouslyVisible.length === 1 && previouslyVisible[0].n !== n) {
    fadeCandidate(selected.el)
  }
}

const displays = new WeakMap<HTMLElement, { value: string; priority: string }>()
const transitions = new WeakMap<HTMLElement, Animation>()

function cancelTransition(el: HTMLElement) {
  transitions.get(el)?.cancel()
  transitions.delete(el)
}

function fadeCandidate(el: HTMLElement) {
  if (typeof el.animate !== 'function' || (typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches)) return
  // Opacity preserves the page layout, transforms and the synchronous selected candidate.
  const animation = el.animate([{ opacity: 0 }, { opacity: getComputedStyle(el).opacity || '1' }], {
    duration: 160,
    easing: 'ease-out',
  })
  transitions.set(el, animation)
  animation.onfinish = () => {
    if (transitions.get(el) === animation) transitions.delete(el)
  }
}

/** Author display rules, including inline !important, must not reveal a hidden candidate. */
export function setVariantVisible(el: HTMLElement, visible: boolean) {
  cancelTransition(el)
  const value = el.style.getPropertyValue('display')
  const priority = el.style.getPropertyPriority('display')
  const forced = value === 'none' && priority === 'important'
  if (!visible) {
    if (!displays.has(el) || !forced) displays.set(el, { value, priority })
    el.hidden = true
    el.style.setProperty('display', 'none', 'important')
    return
  }
  const previous = displays.get(el)
  // An HMR patch may have supplied a newer inline display while this root was hidden.
  if (previous && forced) {
    if (previous.value) el.style.setProperty('display', previous.value, previous.priority)
    else el.style.removeProperty('display')
  }
  displays.delete(el)
  el.hidden = false
}

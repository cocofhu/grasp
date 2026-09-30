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
  if (w.original) w.original.hidden = n !== 0
  for (const v of w.variants) v.el.hidden = v.n !== n
}

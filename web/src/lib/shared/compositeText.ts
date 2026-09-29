import type { ClarifyImage, CompositeText } from '@/lib/shared/types'
import { blobContentUrl } from '../api/api'
import { i18n } from './i18n'

/** Prefer explicit url (public gate opaque image route), then blob:{id}, then data. */
export function imgSrc(im: ClarifyImage): string {
  const url = (im.url || '').trim()
  if (url) return url
  const ref = (im.ref || '').trim()
  if (ref.startsWith('blob:')) {
    return blobContentUrl(ref)
  }
  if (im.data) {
    return `data:${im.mimeType || 'image/png'};base64,${im.data}`
  }
  return ''
}

/** Prefer live/restored `url` (Agent tester), then imgSrc. */
export function chatImageSrc(im: ClarifyImage & { url?: string }): string {
  const url = (im.url || '').trim()
  if (url) return url
  return imgSrc(im)
}

export function isCompositeText(v: unknown): v is CompositeText {
  return !!v && typeof v === 'object' && ('text' in v || 'images' in v)
}

/** True when text or images provide non-empty content. */
export function isCompositeFilled(v: unknown): boolean {
  if (v == null) return false
  if (typeof v === 'string') return v.trim().length > 0
  if (isCompositeText(v)) {
    return (v.text?.trim().length ?? 0) > 0 || (v.images?.length ?? 0) > 0
  }
  return String(v).trim().length > 0
}

/** Submit shape: object when images present, plain string when text-only. */
export function normalizeCompositeSubmit(text: string, images: ClarifyImage[]): string | CompositeText {
  const t = text.trim()
  if (images.length > 0) {
    return { text: t, images }
  }
  return t
}

export function compositeDisplayText(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'string') return v
  if (isCompositeText(v)) return v.text || ''
  return String(v)
}

export function compositeImages(v: unknown): ClarifyImage[] {
  if (isCompositeText(v)) return v.images || []
  return []
}

/** Standalone image-count label (matches backend run title). */
export function formatImageCountFull(n: number): string {
  return i18n.global.t('common.format.imageCountFull', { n })
}

/** Compact chip suffix when text is also shown (e.g. "需求 · 2图"). */
export function formatImageCountChip(n: number): string {
  return i18n.global.t('common.format.imageCountChip', { n })
}

/** Badge / tooltip label. */
function formatImageCountBadge(n: number): string {
  return i18n.global.t('common.format.imageCountBadge', { n })
}

export interface FormatVarOptions {
  /** Show 是/否 instead of true/false. */
  localeBool?: boolean
}

/** Human-readable plain-text fallback (tooltips, titles). Omits image count when text exists. */
export function formatVarValue(v: unknown, opts?: FormatVarOptions): string {
  if (v == null || v === '') return '—'
  if (typeof v === 'boolean') {
    if (opts?.localeBool) return v ? '是' : '否'
    return v ? 'true' : 'false'
  }
  if (typeof v === 'string') return v
  if (Array.isArray(v)) return v.length ? v.join(', ') : '—'
  if (isCompositeText(v)) {
    const t = (v.text || '').trim()
    const n = v.images?.length ?? 0
    if (t) return t
    if (n) return formatImageCountFull(n)
    return '—'
  }
  if (typeof v === 'object') {
    try {
      return JSON.stringify(v)
    } catch {
      return String(v)
    }
  }
  return String(v)
}

/** Compact chip text: text + optional ·N图 suffix when no badge UI is available. */
export function formatVarChip(v: unknown, maxLen = 80): string {
  if (isCompositeText(v)) {
    const t = (v.text || '').trim()
    const n = v.images?.length ?? 0
    let s = t
    if (!s && n) s = formatImageCountFull(n)
    else if (s && n) s = `${s} · ${formatImageCountChip(n)}`
    s = s.replace(/\s+/g, ' ').trim()
    return s.length > maxLen ? s.slice(0, maxLen) + '…' : s
  }
  let s = formatVarValue(v).replace(/\s+/g, ' ').trim()
  if (s.length > maxLen) s = s.slice(0, maxLen) + '…'
  return s
}

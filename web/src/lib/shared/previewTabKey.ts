import type { PreviewPort } from '@/lib/api/apiTypes'

export function isUrlPreview(p: PreviewPort): boolean {
  return p.kind === 'url'
}

export function previewTabKey(p: PreviewPort): string {
  if (isUrlPreview(p)) return `url:${(p.url || '').trim()}`
  return `port:${p.port}`
}

export function previewTabLabel(p: PreviewPort): string {
  const label = (p.label || '').trim()
  if (label) return label
  if (isUrlPreview(p)) {
    const u = (p.url || '').trim()
    try {
      const parsed = new URL(u)
      return parsed.host + parsed.pathname
    } catch {
      return u
    }
  }
  return String(p.port)
}

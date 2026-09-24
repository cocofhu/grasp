/** Host label that keeps the preview document off the approval origin. */
export const PREVIEW_HOST_LABEL = 'pv'

type PageLocation = {
  protocol: string
  hostname: string
  port: string
}

export function isPreviewDocumentHostname(hostname: string): boolean {
  return (hostname || '').toLowerCase().startsWith(`${PREVIEW_HOST_LABEL}.`)
}

function isIPv4(hostname: string): boolean {
  return /^\d{1,3}(\.\d{1,3}){3}$/.test(hostname)
}

function isLoopbackIPv4(hostname: string): boolean {
  return /^127(?:\.\d{1,3}){3}$/.test(hostname)
}

/**
 * Loopback addresses use a *.localhost name. Chromium resolves it to this
 * machine and treats it as a secure context, so partitioned cookies can be
 * stored on http. pv.<ip>.sslip.io is cross-site and not secure on http.
 */
function loopbackPreviewHostname(hostname: string): string {
  const bare = hostname.replace(/^\[|\]$/g, '')
  if (isLoopbackIPv4(bare)) return `${PREVIEW_HOST_LABEL}.${bare}.localhost`
  if (bare === '::1') return `${PREVIEW_HOST_LABEL}.v6-0-0-0-0-0-0-0-1.localhost`
  return `${PREVIEW_HOST_LABEL}.${bare}.localhost`
}

/** Same-site child host for a registrable DNS name. Loopback uses *.localhost. */
export function previewDocumentHost(hostname: string, port = ''): string {
  const bare = (hostname || '').replace(/^\[|\]$/g, '')
  let host = bare
  if (!isPreviewDocumentHostname(bare)) {
    if (isLoopbackIPv4(bare) || bare === '::1') host = loopbackPreviewHostname(bare)
    else if (isIPv4(bare)) host = `${PREVIEW_HOST_LABEL}.${bare}.sslip.io`
    else if (bare) host = `${PREVIEW_HOST_LABEL}.${bare}`
  }
  return port ? `${host}:${port}` : host
}

function pageOf(page?: PageLocation): PageLocation {
  if (page) return page
  const loc = globalThis.location
  return {
    protocol: loc?.protocol || 'http:',
    hostname: loc?.hostname || '',
    port: loc?.port || '',
  }
}

/**
 * Absolute URL of a preview path on the preview document host.
 * The approval page origin is never the document origin.
 */
export function toPreviewDocumentURL(pathOrUrl: string, page?: PageLocation): string {
  const raw = (pathOrUrl || '').trim()
  if (!raw) return ''
  const loc = pageOf(page)
  if (/^https?:\/\//i.test(raw)) {
    try {
      const u = new URL(raw)
      if (isPreviewDocumentHostname(u.hostname)) return u.href
      const host = previewDocumentHost(u.hostname, u.port)
      return `${u.protocol}//${host}${u.pathname}${u.search}${u.hash}`
    } catch {
      return raw
    }
  }
  const path = raw.startsWith('/') ? raw : `/${raw}`
  const host = previewDocumentHost(loc.hostname, loc.port)
  const protocol = loc.protocol || 'http:'
  return `${protocol}//${host}${path}`
}

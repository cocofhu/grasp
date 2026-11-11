import type { ClarifyImage } from '@/lib/shared/types'
import { i18n } from './i18n'

/** Site / platform ACP single-file limit (clarified: 50 MiB). */
export const SITE_ATTACH_MAX_BYTES = 50 * 1024 * 1024

export const SITE_ATTACH_MAX_MIB = 50

/** True when a URL itself is an image data URL or http(s) image address. */
export function isLikelyImageUrl(url: string): boolean {
  const u = (url || '').trim()
  if (!u) return false
  if (u.toLowerCase().startsWith('data:image/')) return true
  return /^https?:\/\//i.test(u) && /\.(png|jpe?g|gif|webp|bmp|svg)(?:\?|#|$)/i.test(u)
}

/**
 * Infer mime from a restored image URL (data:image/… or http(s) with ext).
 * Bare http(s) imageURLs (no ext) default to image/png so thumbs stay previewable.
 */
export function inferImageMimeFromUrl(url: string): string {
  const u = (url || '').trim()
  if (!u) return ''
  const dataMatch = /^data:(image\/[a-z0-9.+-]+)/i.exec(u)
  if (dataMatch) return dataMatch[1].toLowerCase()
  const extMatch = /\.(png|jpe?g|gif|webp|bmp|svg)(?:\?|#|$)/i.exec(u)
  if (extMatch) {
    const ext = extMatch[1].toLowerCase()
    if (ext === 'jpg' || ext === 'jpeg') return 'image/jpeg'
    if (ext === 'svg') return 'image/svg+xml'
    return `image/${ext}`
  }
  if (/^https?:\/\//i.test(u)) return 'image/png'
  return ''
}

/** True when mime, filename, or url looks like an image (preview as thumb). */
export function isImageAttachment(
  im: Pick<ClarifyImage, 'mimeType' | 'name'> | { mimeType?: string; name?: string; url?: string },
): boolean {
  const mime = (im.mimeType || '').split(';')[0]!.trim().toLowerCase()
  if (mime.startsWith('image/')) return true
  if (mime && mime !== 'application/octet-stream') return false
  const name = (im.name || '').toLowerCase()
  if (/\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(name)) return true
  return isLikelyImageUrl('url' in im ? String(im.url || '') : '')
}

type AttachmentKind = 'image' | 'audio' | 'video' | 'file'

const MEDIA_MIME: Record<string, string> = {
  mp3: 'audio/mpeg', wav: 'audio/wav', ogg: 'audio/ogg', oga: 'audio/ogg',
  m4a: 'audio/mp4', aac: 'audio/aac', flac: 'audio/flac', opus: 'audio/ogg',
  mp4: 'video/mp4', m4v: 'video/mp4', webm: 'video/webm', ogv: 'video/ogg',
  mov: 'video/quicktime', avi: 'video/x-msvideo', mkv: 'video/x-matroska',
}

/** Browser/file-manager MIME can be empty or generic, especially for dragged files. */
export function attachmentMime(im: { mimeType?: string; name?: string }): string {
  const mime = (im.mimeType || '').split(';')[0]!.trim().toLowerCase()
  if (mime && mime !== 'application/octet-stream') return mime
  const ext = (im.name || '').split('.').pop()?.toLowerCase() || ''
  return MEDIA_MIME[ext] || mime || 'application/octet-stream'
}

export function attachmentKind(im: { mimeType?: string; name?: string; url?: string }): AttachmentKind {
  const mime = attachmentMime(im)
  if (mime.startsWith('audio/')) return 'audio'
  if (mime.startsWith('video/')) return 'video'
  if (isImageAttachment(im)) return 'image'
  return 'file'
}

export function attachmentSizeLabel(im: ClarifyImage): string {
  const bytes = attachmentByteLength(im)
  if (!bytes && im.sizeBytes === undefined) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

export function attachmentDetails(im: ClarifyImage): string {
  return [i18n.global.t(`common.attachments.${attachmentKind(im)}`), attachmentSizeLabel(im)].filter(Boolean).join(' · ')
}

export function attachmentsReading(images: ClarifyImage[]): boolean {
  return images.some(im => im.readState === 'reading')
}

/** Download links accept the attachment transports, never executable URL schemes. */
export function attachmentDownloadUrl(src: string): string {
  try {
    const protocol = new URL(src, 'https://attachment.invalid').protocol
    return src.trim() && ['http:', 'https:', 'blob:', 'data:'].includes(protocol) ? src : ''
  } catch {
    return ''
  }
}

/** Display label: original name, else fallback. */
export function attachmentDisplayName(im: ClarifyImage, index = 0): string {
  const named = (im.name || '').trim()
  if (named) return named
  return `attachment-${index + 1}`
}

/** Prefer File.name; empty → distinguishable fallback. */
function fileAttachmentName(file: File, index = 0): string {
  const named = (file.name || '').trim()
  if (named) return named
  return `attachment-${index + 1}`
}

/** Decode base64 payload length (approx original bytes). */
export function attachmentByteLength(im: ClarifyImage): number {
  if (im.sizeBytes && im.sizeBytes > 0) return im.sizeBytes
  if (!im.data) return 0
  const len = im.data.length
  let pad = 0
  if (im.data.endsWith('==')) pad = 2
  else if (im.data.endsWith('=')) pad = 1
  return Math.max(0, Math.floor((len * 3) / 4) - pad)
}

function isOversizedAttachment(im: ClarifyImage, maxBytes = SITE_ATTACH_MAX_BYTES): boolean {
  return attachmentByteLength(im) > maxBytes
}

export function findOversizedAttachments(
  images: ClarifyImage[],
  maxBytes = SITE_ATTACH_MAX_BYTES,
): ClarifyImage[] {
  return images.filter((im) => isOversizedAttachment(im, maxBytes))
}

export type ReadAttachment = ClarifyImage & { data: string; mimeType: string; url: string; name: string }

/**
 * Size-gate then read each file as a data URL. `onRead` fires asynchronously per
 * accepted file (FileReader order); `url` is the full data URL, `data` the bare base64.
 */
export function readFilesAsAttachments(
  files: ArrayLike<File>,
  opts: {
    maxBytes?: number
    onStart?: (att: ReadAttachment) => void
    onRead: (att: ReadAttachment, pending: ReadAttachment) => void
    onError?: (name: string, pending: ReadAttachment) => void
  },
): { rejected: string[]; accepted: number } {
  const maxBytes = opts.maxBytes ?? SITE_ATTACH_MAX_BYTES
  const rejected: string[] = []
  let accepted = 0
  Array.from(files).forEach((f, i) => {
    const name = fileAttachmentName(f, i)
    if (f.size > maxBytes) {
      rejected.push(name)
      return
    }
    const mimeType = attachmentMime({ mimeType: f.type, name })
    const reader = new FileReader()
    const pending: ReadAttachment = { data: '', url: '', mimeType, name, sizeBytes: f.size, readState: 'reading' }
    opts.onStart?.(pending)
    let settled = false
    const fail = () => {
      if (settled) return
      settled = true
      opts.onError?.(name, pending)
    }
    reader.onload = () => {
      if (settled) return
      settled = true
      const url = String(reader.result || '')
      const comma = url.indexOf(',')
      opts.onRead({ data: comma >= 0 ? url.slice(comma + 1) : url, mimeType, url, name, sizeBytes: f.size }, pending)
    }
    reader.onerror = fail
    reader.onabort = fail
    try {
      reader.readAsDataURL(f)
    } catch {
      fail()
    }
    accepted++
  })
  return { rejected, accepted }
}

/** File items from a paste event; empty when the clipboard carries no files. */
export function filesFromClipboard(e: ClipboardEvent): File[] {
  const items = e.clipboardData?.items
  if (!items) return []
  const picked: File[] = []
  for (const it of Array.from(items)) {
    if (it.kind === 'file') {
      const f = it.getAsFile()
      if (f) picked.push(f)
    }
  }
  return picked
}

/** Human-readable reject toast: selection stage. */
export function formatSelectRejectMessage(names: string[], maxMiB = SITE_ATTACH_MAX_MIB): string {
  return `已拒绝超限文件：${names.join(', ')}。单文件上限 ${maxMiB} MiB，未进入待发送列表。`
}

/** Human-readable reject toast: send stage (blocks whole message). */
export function formatSendRejectMessage(names: string[], maxMiB = SITE_ATTACH_MAX_MIB): string {
  return `发送已阻止：存在超限文件（${names.join(', ')}）。上限 ${maxMiB} MiB，整条消息未发送。`
}

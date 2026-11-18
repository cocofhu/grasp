import type { ArtifactAttachment, ArtifactKind } from './types'

export type FileBadgeTone = 'pdf' | 'doc' | 'data' | 'audio' | 'media' | 'archive'

export interface FileBadge {
  label: string
  tone: FileBadgeTone
}

const KIND_LABEL: Record<ArtifactKind, string> = {
  markdown: 'MD', json: 'JSON', yaml: 'YAML', html: 'HTML', text: 'TXT',
  image: 'IMG', video: 'VID', audio: 'AUD', pdf: 'PDF', file: 'FILE',
}

const KIND_TONE: Record<ArtifactKind, FileBadgeTone> = {
  markdown: 'doc', text: 'doc', html: 'doc', json: 'data', yaml: 'data',
  audio: 'audio', image: 'media', video: 'media', pdf: 'pdf', file: 'archive',
}

/** Badge text is the extension when it fits the 4-character label, otherwise a per-kind fallback. */
export function fileBadge(file: Pick<ArtifactAttachment, 'name' | 'kind'>): FileBadge {
  const dot = file.name.lastIndexOf('.')
  const ext = dot > 0 ? file.name.slice(dot + 1) : ''
  const label = /^[a-z0-9]{1,4}$/i.test(ext) ? ext.toUpperCase() : KIND_LABEL[file.kind] ?? KIND_LABEL.file
  return { label, tone: KIND_TONE[file.kind] ?? 'archive' }
}

export function sizeLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1048576).toFixed(1)} MB`
}

import type { InjectionKey } from 'vue'
import { isKnownMissing, markMissing, parseBlobId } from '@/lib/shared/blobMissingCache'
import { EMBED_IMAGE_MESSAGE } from './embedChat'

/** Provided by the embed chat so ClarifyChat opens the page-level window instead of AppModal. */
export const EMBED_IMAGE_PREVIEW_KEY: InjectionKey<EmbedImagePreview> = Symbol('embedImagePreview')

export type EmbedImagePreview = {
  open: (src: string, label: string) => void
  close: () => void
}

/** Same-origin channel between the chat iframe and the picture iframe. The preview page is a different origin and cannot join. */
export function embedImageChannelName(runId: string, nodeId: string): string {
  return `grasp-embed-image:${runId}:${nodeId}`
}

export type EmbedImageFrameMessage =
  | { kind: 'ready'; runId: string; nodeId: string }
  | { kind: 'bye'; runId: string; nodeId: string }
  | { kind: 'show'; runId: string; nodeId: string; src: string; label: string; missing: boolean }
  | { kind: 'missing'; runId: string; nodeId: string; blobId: string }

export function parseEmbedImageFrameMessage(data: unknown): EmbedImageFrameMessage | null {
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  const runId = typeof m.runId === 'string' ? m.runId : ''
  const nodeId = typeof m.nodeId === 'string' ? m.nodeId : ''
  if (!runId || !nodeId) return null
  if (m.kind === 'ready' || m.kind === 'bye') return { kind: m.kind, runId, nodeId }
  if (m.kind === 'show' && typeof m.src === 'string') {
    return {
      kind: 'show',
      runId,
      nodeId,
      src: m.src,
      label: typeof m.label === 'string' ? m.label : '',
      missing: m.missing === true,
    }
  }
  if (m.kind === 'missing' && typeof m.blobId === 'string' && m.blobId) {
    return { kind: 'missing', runId, nodeId, blobId: m.blobId }
  }
  return null
}

function clipLabel(label: string): string {
  return label.replace(/[\r\n\t]/g, ' ').trim().slice(0, 180)
}

export type EmbedImagePreviewApi = EmbedImagePreview & { dispose: () => void }

/**
 * Chat iframe side. `post` notifies the preview page to open or close the shell
 * and carries only the action and display name. The picture address is queued
 * here and sent on the Grasp channel once the picture frame says it is ready.
 */
export function createEmbedImagePreview(opts: {
  runId: string
  nodeId: string
  post: (msg: { type: typeof EMBED_IMAGE_MESSAGE; action: 'open' | 'close'; label?: string }) => void
}): EmbedImagePreviewApi {
  let channel: BroadcastChannel | null = null
  try {
    if (typeof BroadcastChannel === 'function') channel = new BroadcastChannel(embedImageChannelName(opts.runId, opts.nodeId))
  } catch {
    channel = null
  }
  let ready = false
  let pending: { src: string; label: string; missing: boolean } | null = null

  function publish() {
    if (!channel || !ready || !pending) return
    const msg: EmbedImageFrameMessage = {
      kind: 'show',
      runId: opts.runId,
      nodeId: opts.nodeId,
      src: pending.src,
      label: pending.label,
      missing: pending.missing,
    }
    channel.postMessage(msg)
  }

  if (channel) {
    channel.onmessage = (ev: MessageEvent) => {
      const msg = parseEmbedImageFrameMessage(ev.data)
      if (!msg || msg.runId !== opts.runId || msg.nodeId !== opts.nodeId) return
      if (msg.kind === 'ready') {
        ready = true
        publish()
      } else if (msg.kind === 'bye') {
        ready = false
      } else if (msg.kind === 'missing') {
        markMissing(msg.blobId)
      }
    }
  }

  return {
    open(src: string, label: string) {
      const id = parseBlobId(src)
      pending = { src, label: clipLabel(label), missing: !!(id && isKnownMissing(id)) }
      opts.post({ type: EMBED_IMAGE_MESSAGE, action: 'open', label: pending.label })
      publish()
    },
    close() {
      pending = null
      opts.post({ type: EMBED_IMAGE_MESSAGE, action: 'close' })
    },
    dispose() {
      channel?.close()
      channel = null
      ready = false
    },
  }
}

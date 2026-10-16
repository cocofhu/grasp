import type { InjectionKey } from 'vue'
import { EMBED_DEMO_MESSAGE } from './embedChat'

/** Provided by the embed chat so ClarifyChat opens the page-level window instead of AppModal. */
export const EMBED_DEMO_PREVIEW_KEY: InjectionKey<EmbedDemoPreview> = Symbol('embedDemoPreview')

export type EmbedDemoPreview = {
  open: (html: string, label: string) => void
  close: () => void
}

/** Same-origin channel between the chat iframe and the demo iframe. The preview page is a different origin and cannot join. */
export function embedDemoChannelName(runId: string, nodeId: string): string {
  return `grasp-embed-demo:${runId}:${nodeId}`
}

export type EmbedDemoFrameMessage =
  | { kind: 'ready'; runId: string; nodeId: string }
  | { kind: 'bye'; runId: string; nodeId: string }
  | { kind: 'show'; runId: string; nodeId: string; html: string; label: string }

export function parseEmbedDemoFrameMessage(data: unknown): EmbedDemoFrameMessage | null {
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  const runId = typeof m.runId === 'string' ? m.runId : ''
  const nodeId = typeof m.nodeId === 'string' ? m.nodeId : ''
  if (!runId || !nodeId) return null
  if (m.kind === 'ready' || m.kind === 'bye') return { kind: m.kind, runId, nodeId }
  if (m.kind === 'show' && typeof m.html === 'string') {
    return {
      kind: 'show',
      runId,
      nodeId,
      html: m.html,
      label: typeof m.label === 'string' ? m.label : '',
    }
  }
  return null
}

function clipLabel(label: string): string {
  return label.replace(/[\r\n\t]/g, ' ').trim().slice(0, 180)
}

export type EmbedDemoPreviewApi = EmbedDemoPreview & { dispose: () => void }

/**
 * Chat iframe side. `post` notifies the preview page to open or close the shell
 * and carries only the action and display name. The demo HTML is queued here
 * and sent on the Grasp channel once the demo frame says it is ready.
 */
export function createEmbedDemoPreview(opts: {
  runId: string
  nodeId: string
  post: (msg: { type: typeof EMBED_DEMO_MESSAGE; action: 'open' | 'close'; label?: string }) => void
}): EmbedDemoPreviewApi {
  let channel: BroadcastChannel | null = null
  try {
    if (typeof BroadcastChannel === 'function') channel = new BroadcastChannel(embedDemoChannelName(opts.runId, opts.nodeId))
  } catch {
    channel = null
  }
  let ready = false
  let pending: { html: string; label: string } | null = null

  function publish() {
    if (!channel || !ready || !pending) return
    const msg: EmbedDemoFrameMessage = {
      kind: 'show',
      runId: opts.runId,
      nodeId: opts.nodeId,
      html: pending.html,
      label: pending.label,
    }
    channel.postMessage(msg)
  }

  if (channel) {
    channel.onmessage = (ev: MessageEvent) => {
      const msg = parseEmbedDemoFrameMessage(ev.data)
      if (!msg || msg.runId !== opts.runId || msg.nodeId !== opts.nodeId) return
      if (msg.kind === 'ready') {
        ready = true
        publish()
      } else if (msg.kind === 'bye') {
        ready = false
      }
    }
  }

  return {
    open(html: string, label: string) {
      pending = { html, label: clipLabel(label) }
      opts.post({ type: EMBED_DEMO_MESSAGE, action: 'open', label: pending.label })
      publish()
    },
    close() {
      pending = null
      opts.post({ type: EMBED_DEMO_MESSAGE, action: 'close' })
    },
    dispose() {
      channel?.close()
      channel = null
      ready = false
    },
  }
}

/** Authenticated drawer ↔ preview handshake; the caller validates source/origin. */
export const EMBED_LIVE_CONTEXT_REQUEST = 'grasp-embed:live-context-request'
export const EMBED_LIVE_CONTEXT_RESULT = 'grasp-embed:live-context-result'
export type EmbedLiveContext = { url: string }

/** Works on direct HTTP previews as well as secure contexts. */
export function liveRequestId(): string {
  const bytes = new Uint8Array(16)
  if (globalThis.crypto?.getRandomValues) globalThis.crypto.getRandomValues(bytes)
  else for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256)
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

export function createEmbedLiveContext(post: (message: Record<string, unknown>) => void) {
  const pending = new Map<string, { resolve: (context: EmbedLiveContext) => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout> }>()
  return {
    request(): Promise<EmbedLiveContext> {
      const nonce = liveRequestId()
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(nonce)
          reject(new Error('Preview Live controls did not respond'))
        }, 3000)
        pending.set(nonce, { resolve, reject, timer })
        post({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce })
      })
    },
    onResult(data: unknown): boolean {
      if (!data || typeof data !== 'object') return false
      const message = data as Record<string, unknown>
      if (message.type !== EMBED_LIVE_CONTEXT_RESULT) return false
      const request = typeof message.nonce === 'string' ? pending.get(message.nonce) : undefined
      if (!request) return true
      clearTimeout(request.timer)
      pending.delete(message.nonce as string)
      if (message.ok === true && typeof message.url === 'string' && message.url.length <= 2048 && /^https?:\/\//.test(message.url)) request.resolve({ url: message.url })
      else request.reject(new Error('Preview Live controls are unavailable'))
      return true
    },
    dispose() {
      for (const request of pending.values()) {
        clearTimeout(request.timer)
        request.reject(new Error('Preview disconnected'))
      }
      pending.clear()
    },
  }
}

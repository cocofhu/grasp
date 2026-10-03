/**
 * Coalesce rapid stream text updates into at most one markdown render per
 * animation frame so token deltas cannot flood marked+DOMPurify.
 * The output defaults to an HTML string; block renderers (renderMarkdownBlocks)
 * pass `empty: []` and get the per-block HTML list instead.
 */

export type StreamMarkdownPreviewOptions<T = string> = {
  render: (src: string) => T
  /** Value published for empty text (defaults to ''). */
  empty?: T
  /** Inject for tests; defaults to requestAnimationFrame. */
  schedule?: (cb: () => void) => number
  cancel?: (id: number) => void
}

export type StreamMarkdownPreview<T = string> = {
  /** Absolute replace (resume snapshot / clear). */
  setText: (text: string) => void
  /** Append a delta and schedule a coalesced render. */
  append: (delta: string) => void
  /** Current raw stream text. */
  getText: () => string
  /** Last rendered output (may lag text by up to one frame). */
  getHtml: () => T
  /** Force flush (tests / end-of-stream). */
  flush: () => void
  /** Cancel pending frame and clear. */
  reset: () => void
  /** Subscribe to output updates; returns unsubscribe. */
  subscribe: (listener: (html: T) => void) => () => void
}

export function createStreamMarkdownPreview<T = string>(
  opts: StreamMarkdownPreviewOptions<T>,
): StreamMarkdownPreview<T> {
  const schedule = opts.schedule ?? ((cb) => requestAnimationFrame(cb))
  const cancel = opts.cancel ?? ((id) => cancelAnimationFrame(id))
  const empty = (opts.empty ?? '') as T
  let text = ''
  let html: T = empty
  let pending = false
  let handle = 0
  const listeners = new Set<(html: T) => void>()

  function notify() {
    for (const l of listeners) l(html)
  }

  function flush() {
    if (pending) {
      cancel(handle)
      pending = false
      handle = 0
    }
    html = text ? opts.render(text) : empty
    notify()
  }

  function scheduleFlush() {
    if (pending) return
    pending = true
    handle = schedule(() => flush())
  }

  return {
    setText(next: string) {
      text = next
      scheduleFlush()
    },
    append(delta: string) {
      if (!delta) return
      text += delta
      scheduleFlush()
    },
    getText: () => text,
    getHtml: () => html,
    flush,
    reset() {
      text = ''
      html = empty
      if (pending) {
        cancel(handle)
        pending = false
        handle = 0
      }
      notify()
    },
    subscribe(listener) {
      listeners.add(listener)
      return () => {
        listeners.delete(listener)
      }
    },
  }
}

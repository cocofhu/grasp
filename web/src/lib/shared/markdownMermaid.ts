/**
 * Draws `pre.md-mermaid` blocks from markdown.ts as SVG wherever the HTML
 * lands (v-html in chat, artifact preview, node output). One document-level
 * observer covers every surface, and a `<html>` class change redraws the
 * diagrams in the new theme. The source code stays visible until an SVG is
 * ready and remains the fallback when Mermaid cannot parse it.
 */
import { withMermaidSerial } from '@/components/run/mermaidRenderQueue'
import { mermaidThemeName, themeVars } from '@/components/run/mermaidTheme'

const SELECTOR = 'pre.md-mermaid'
/** SVGs kept per theme+source so a block re-rendered by Vue swaps back in before paint. */
const CACHE_MAX = 32
/** Smallest fit-to-width scale; 16px Mermaid labels stay at the 12px code size. */
const MIN_SCALE = 0.75

const svgCache = new Map<string, string>()
const inflight = new Map<string, Promise<string | null>>()
const failedSources = new Set<string>()
let contentObserver: MutationObserver | null = null
let themeObserver: MutationObserver | null = null
let scanQueued = false
let renderSeq = 0

function themeKey(): string {
  return `${mermaidThemeName()}\n${JSON.stringify(themeVars())}`
}

function sourceOf(pre: Element): string {
  return (pre.querySelector(':scope > code')?.textContent ?? '').trim()
}

function cacheSvg(key: string, svg: string) {
  svgCache.delete(key)
  svgCache.set(key, svg)
  if (svgCache.size > CACHE_MAX) {
    const oldest = svgCache.keys().next().value
    if (oldest !== undefined) svgCache.delete(oldest)
  }
}

function applySvg(pre: HTMLElement, key: string, svg: string) {
  let box = pre.querySelector<HTMLElement>(':scope > .md-mermaid-svg')
  if (!box) {
    box = document.createElement('span')
    box.className = 'md-mermaid-svg'
    pre.appendChild(box)
  }
  box.innerHTML = svg
  const el = box.querySelector('svg')
  const width = Number(el?.getAttribute('viewBox')?.split(/[\s,]+/)[2])
  // Past MIN_SCALE a wide diagram scrolls inside the card instead of shrinking.
  if (el && width > 0) el.style.minWidth = `${Math.round(width * MIN_SCALE)}px`
  pre.dataset.mermaid = 'done'
  pre.dataset.mermaidKey = key
}

class MermaidParseError extends Error {}

function renderSvg(source: string, key: string): Promise<string | null> {
  const pending = inflight.get(key)
  if (pending) return pending
  const task = withMermaidSerial(async (mermaid) => {
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      suppressErrorRendering: true,
      theme: mermaidThemeName(),
      themeVariables: themeVars(),
    })
    try {
      await mermaid.parse(source)
    } catch {
      throw new MermaidParseError()
    }
    let lastErr: unknown
    for (let attempt = 0; attempt < 2; attempt++) {
      renderSeq += 1
      const id = `md-mmd-${Date.now()}-${renderSeq}`
      try {
        return (await mermaid.render(id, source)).svg
      } catch (err) {
        lastErr = err
      } finally {
        document.getElementById(`d${id}`)?.remove()
      }
    }
    throw lastErr
  })
    .then((svg) => {
      cacheSvg(key, svg)
      return svg
    })
    .catch((err) => {
      if (err instanceof MermaidParseError) failedSources.add(source)
      return null
    })
    .finally(() => inflight.delete(key))
  inflight.set(key, task)
  return task
}

function hydrate(pre: HTMLElement, theme: string) {
  const source = sourceOf(pre)
  if (!source) return
  const key = `${theme}\n${source}`
  if (pre.dataset.mermaidKey === key) return
  if (failedSources.has(source)) {
    // A theme redraw keeps the SVG already shown; only a never-drawn block falls back.
    if (pre.dataset.mermaid !== 'done') pre.dataset.mermaid = 'failed'
    return
  }
  const cached = svgCache.get(key)
  if (cached !== undefined) {
    applySvg(pre, key, cached)
    return
  }
  void renderSvg(source, key).then((svg) => {
    if (!pre.isConnected || sourceOf(pre) !== source) return
    if (svg !== null) {
      // The theme may have flipped while this one rendered; the next scan redraws it.
      if (pre.dataset.mermaidKey !== key) applySvg(pre, key, svg)
      if (key !== `${themeKey()}\n${source}`) scheduleScan()
    } else if (pre.dataset.mermaid !== 'done') {
      pre.dataset.mermaid = 'failed'
    }
  })
}

function scan() {
  scanQueued = false
  const pres = document.querySelectorAll<HTMLElement>(SELECTOR)
  if (!pres.length) return
  const theme = themeKey()
  for (const pre of Array.from(pres)) hydrate(pre, theme)
}

/** Microtask, so a cached SVG replaces a re-rendered block before the frame paints. */
function scheduleScan() {
  if (scanQueued) return
  scanQueued = true
  queueMicrotask(scan)
}

function addsMermaid(records: MutationRecord[]): boolean {
  for (const record of records) {
    for (const node of Array.from(record.addedNodes)) {
      if (node.nodeType !== 1) continue
      const el = node as Element
      if (el.matches(SELECTOR) || el.querySelector(SELECTOR)) return true
    }
  }
  return false
}

/** Called by the markdown renderer the first time it emits a Mermaid block. */
export function ensureMarkdownMermaid(): void {
  if (contentObserver || typeof document === 'undefined' || typeof MutationObserver === 'undefined') return
  contentObserver = new MutationObserver((records) => {
    if (addsMermaid(records)) scheduleScan()
  })
  contentObserver.observe(document.documentElement, { childList: true, subtree: true })
  themeObserver = new MutationObserver(scheduleScan)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  scheduleScan()
}

/** Test hook: stop observing and forget cached SVGs and parse failures. */
export function resetMarkdownMermaidForTest(): void {
  contentObserver?.disconnect()
  themeObserver?.disconnect()
  contentObserver = null
  themeObserver = null
  scanQueued = false
  svgCache.clear()
  inflight.clear()
  failedSources.clear()
}

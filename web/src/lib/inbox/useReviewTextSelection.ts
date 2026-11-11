import { onBeforeUnmount, onMounted, ref, unref, watch, type Ref } from 'vue'

interface ReviewTextSelectionCached {
  quote: string
  jsonPath?: string
  label?: string
}

interface UseReviewTextSelectionOptions {
  /** When false, selection never shows the toolbar. */
  enabled: Ref<boolean> | boolean
  /** Stage root; selections outside this element are ignored. */
  root: Ref<HTMLElement | null | undefined>
  /** Called when the user selects across two different data-json-path fields. */
  onCrossField?: () => void
}

function findFieldEl(node: Node | null, root: HTMLElement): HTMLElement | null {
  let el: Node | null = node
  if (el && el.nodeType === Node.TEXT_NODE) el = el.parentElement
  while (el && el instanceof HTMLElement && el !== root) {
    if (el.dataset?.jsonPath) return el
    el = el.parentElement
  }
  return null
}

/** Skip composer / form controls so reply drafts never become quote chips. */
function isIgnoredSelectionHost(node: Node | null, root: HTMLElement): boolean {
  let el: Node | null = node
  if (el && el.nodeType === Node.TEXT_NODE) el = el.parentElement
  while (el && el instanceof HTMLElement && el !== root) {
    const tag = el.tagName
    if (tag === 'TEXTAREA' || tag === 'INPUT' || tag === 'SELECT') return true
    if (el.isContentEditable) return true
    if (el.dataset?.reviewComposer != null) return true
    el = el.parentElement
  }
  return false
}

function analyzeSelection(root: HTMLElement | null | undefined, enabled: boolean) {
  if (!enabled || !root) return null
  const sel = window.getSelection()
  if (!sel || sel.isCollapsed || !sel.rangeCount) return null
  const text = String(sel.toString() || '')
    .replace(/\s+/g, ' ')
    .trim()
  if (!text) return null

  const range = sel.getRangeAt(0)
  const ancestor = range.commonAncestorContainer
  if (!root.contains(ancestor)) return null
  if (
    isIgnoredSelectionHost(range.startContainer, root) ||
    isIgnoredSelectionHost(range.endContainer, root)
  ) {
    return null
  }

  const startField = findFieldEl(range.startContainer, root)
  const endField = findFieldEl(range.endContainer, root)

  if (startField && endField && startField !== endField) {
    return { cross: true as const, range }
  }

  const field = startField || endField
  return {
    cross: false as const,
    quote: text,
    jsonPath: field?.dataset.jsonPath || undefined,
    label: field?.dataset.label || field?.dataset.jsonPath || undefined,
    range,
  }
}

/**
 * Shared selection → floating "Add to Chat" state for review annotatable stages.
 * Caches the last valid selection so toolbar mousedown/click does not lose it.
 */
export function useReviewTextSelection(opts: UseReviewTextSelectionOptions) {
  const visible = ref(false)
  const style = ref<{ left: string; top: string }>({ left: '0px', top: '0px' })
  const cached = ref<ReviewTextSelectionCached | null>(null)
  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let crossFieldToastAt = 0

  function isEnabled() {
    return !!unref(opts.enabled)
  }

  function hide() {
    visible.value = false
  }

  function positionToolbar(range: Range) {
    const rect = range.getBoundingClientRect()
    // Approximate toolbar size before mount measurement.
    const tw = 168
    const th = 34
    // happy-dom / collapsed layout may report a zero box — still show the
    // toolbar near a fallback origin so add-to-chat remains reachable.
    const hasBox = !!(rect && (rect.width || rect.height || rect.top || rect.left))
    let left = hasBox ? rect.left + rect.width / 2 - tw / 2 : 8
    let top = hasBox ? rect.top - th - 8 : 8
    if (top < 8) top = hasBox ? rect.bottom + 8 : 8
    if (left < 8) left = 8
    if (left + tw > window.innerWidth - 8) left = Math.max(8, window.innerWidth - tw - 8)
    style.value = { left: `${left}px`, top: `${top}px` }
    visible.value = true
  }

  let hideTimer: ReturnType<typeof setTimeout> | null = null

  function refresh() {
    const info = analyzeSelection(opts.root.value, isEnabled())
    if (!info) {
      // Keep cache briefly so toolbar click can still read it after Selection
      // collapses; then hide if still empty.
      if (!visible.value) return
      if (hideTimer) clearTimeout(hideTimer)
      hideTimer = setTimeout(() => {
        hideTimer = null
        const again = analyzeSelection(opts.root.value, isEnabled())
        if (!again || again.cross) hide()
      }, 120)
      return
    }
    if (hideTimer) {
      clearTimeout(hideTimer)
      hideTimer = null
    }
    if (info.cross) {
      cached.value = null
      hide()
      const now = Date.now()
      if (now - crossFieldToastAt > 800) {
        crossFieldToastAt = now
        opts.onCrossField?.()
      }
      return
    }
    cached.value = {
      quote: info.quote,
      jsonPath: info.jsonPath,
      label: info.label,
    }
    positionToolbar(info.range)
  }

  function onSelectionChange() {
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(refresh, 10)
  }

  function onMouseDown(e: MouseEvent) {
    const root = opts.root.value
    // Clicks inside the floating toolbar must not clear selection prematurely.
    const t = e.target
    if (t instanceof Element && t.closest('[data-selection-add-to-chat]')) return
    // After outside mousedown, selection may collapse — hide if empty.
    setTimeout(() => {
      const info = analyzeSelection(root, isEnabled())
      if (!info || info.cross) {
        if (!info) hide()
      }
    }, 0)
  }

  function onScroll() {
    if (!visible.value) return
    const info = analyzeSelection(opts.root.value, isEnabled())
    if (info && !info.cross) positionToolbar(info.range)
    else if (!info) hide()
  }

  function clearSelection() {
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
    cached.value = null
    hide()
  }

  /** Read cached (or live) selection for the add action. */
  function takeSelection(): ReviewTextSelectionCached | null {
    if (cached.value?.quote) return { ...cached.value }
    const info = analyzeSelection(opts.root.value, isEnabled())
    if (!info || info.cross || !info.quote) return null
    return { quote: info.quote, jsonPath: info.jsonPath, label: info.label }
  }

  /** Preserve Selection while pressing the floating button. */
  function preserveOnMouseDown(e: MouseEvent) {
    e.preventDefault()
  }

  onMounted(() => {
    document.addEventListener('selectionchange', onSelectionChange)
    document.addEventListener('mousedown', onMouseDown)
    window.addEventListener('scroll', onScroll, true)
  })

  onBeforeUnmount(() => {
    document.removeEventListener('selectionchange', onSelectionChange)
    document.removeEventListener('mousedown', onMouseDown)
    window.removeEventListener('scroll', onScroll, true)
    if (debounceTimer) clearTimeout(debounceTimer)
    if (hideTimer) clearTimeout(hideTimer)
  })

  watch(
    () => unref(opts.enabled),
    (on) => {
      if (!on) clearSelection()
    },
  )

  return {
    visible,
    style,
    cached,
    clearSelection,
    takeSelection,
    preserveOnMouseDown,
    refresh,
  }
}

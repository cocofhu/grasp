/**
 * Copy text to the system clipboard.
 *
 * - Secure context (HTTPS / localhost): prefer Clipboard API; on failure, fall back.
 * - Non-secure context (e.g. http://intranet-IP): skip the API and use legacy
 *   textarea + execCommand('copy') in the same user-gesture stack.
 *
 * Returns true on success, false when both paths fail. Never throws to the UI.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  const value = String(text ?? '')

  if (typeof window !== 'undefined' && window.isSecureContext) {
    const writeText = navigator.clipboard?.writeText
    if (typeof writeText === 'function') {
      try {
        await writeText.call(navigator.clipboard, value)
        return true
      } catch {
        // fall through to legacy
      }
    }
  }

  return legacyCopy(value)
}

function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined') return false
  const focused = document.activeElement
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.cssText = 'position:fixed;left:-9999px;top:0;opacity:0;'
    document.body.appendChild(ta)
    try {
      ta.focus()
      ta.select()
      ta.setSelectionRange(0, text.length)
      return !!document.execCommand('copy')
    } finally {
      const restoreFocus = document.activeElement === ta
      ta.remove()
      if (restoreFocus && focused instanceof HTMLElement && focused.isConnected) {
        focused.focus({ preventScroll: true })
      }
    }
  } catch {
    return false
  }
}

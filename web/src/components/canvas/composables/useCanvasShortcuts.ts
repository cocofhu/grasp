import { getCurrentScope, onScopeDispose } from 'vue'

export type CanvasAction =
  | 'undo'
  | 'redo'
  | 'copy'
  | 'paste'
  | 'duplicate'
  | 'delete'
  | 'selectAll'
  | 'save'
  | 'fitView'
  | 'autoLayout'
  | 'quickAdd'
  | 'help'
  | 'escape'
  | 'rename'
  | 'navLeft'
  | 'navRight'
  | 'navUp'
  | 'navDown'

export interface ShortcutDef {
  action: CanvasAction
  /** i18n key under canvas.shortcuts.actions */
  labelKey: string
  keys: string[][]
}

/** Shortcut table, also rendered by ShortcutHelp. "Mod" is Cmd on macOS, Ctrl elsewhere. */
export const SHORTCUTS: ShortcutDef[] = [
  { action: 'undo', labelKey: 'undo', keys: [['Mod', 'Z']] },
  { action: 'redo', labelKey: 'redo', keys: [['Mod', 'Shift', 'Z'], ['Mod', 'Y']] },
  { action: 'copy', labelKey: 'copy', keys: [['Mod', 'C']] },
  { action: 'paste', labelKey: 'paste', keys: [['Mod', 'V']] },
  { action: 'duplicate', labelKey: 'duplicate', keys: [['Mod', 'D']] },
  { action: 'delete', labelKey: 'delete', keys: [['Delete'], ['Backspace']] },
  { action: 'selectAll', labelKey: 'selectAll', keys: [['Mod', 'A']] },
  { action: 'save', labelKey: 'save', keys: [['Mod', 'S']] },
  { action: 'fitView', labelKey: 'fitView', keys: [['Shift', '1']] },
  { action: 'autoLayout', labelKey: 'autoLayout', keys: [['Shift', 'L']] },
  { action: 'quickAdd', labelKey: 'quickAdd', keys: [['/'], ['Mod', 'K']] },
  { action: 'rename', labelKey: 'rename', keys: [['F2']] },
  { action: 'navRight', labelKey: 'navigate', keys: [['←'], ['→'], ['↑'], ['↓']] },
  { action: 'help', labelKey: 'help', keys: [['?']] },
  { action: 'escape', labelKey: 'escape', keys: [['Esc']] },
]

export function isMac(): boolean {
  return typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || '')
}

/** True when focus is in a text field, where plain keys must type instead of acting. */
export function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el || typeof el.tagName !== 'string') return false
  if (el.isContentEditable) return true
  const tag = el.tagName.toLowerCase()
  if (tag === 'textarea' || tag === 'select') return true
  if (tag !== 'input') return false
  const type = ((el as HTMLInputElement).type || 'text').toLowerCase()
  return !['checkbox', 'radio', 'button', 'submit', 'range', 'color'].includes(type)
}

type KeyLike = Pick<KeyboardEvent, 'key' | 'code' | 'ctrlKey' | 'metaKey' | 'shiftKey' | 'altKey'> & {
  target?: EventTarget | null
}

/** Maps a keydown to a canvas action, or null. Text fields only see Mod+S and Esc. */
export function resolveShortcut(e: KeyLike, mac = isMac()): CanvasAction | null {
  const mod = mac ? e.metaKey : e.ctrlKey
  const key = e.key.length === 1 ? e.key.toLowerCase() : e.key
  const typing = isTypingTarget(e.target ?? null)
  if (mod && !e.altKey && key === 's') return 'save'
  if (key === 'Escape') return 'escape'
  if (typing || e.altKey) return null
  if (mod) {
    if (key === 'z') return e.shiftKey ? 'redo' : 'undo'
    if (key === 'y') return 'redo'
    if (key === 'c') return 'copy'
    if (key === 'v') return 'paste'
    if (key === 'd') return 'duplicate'
    if (key === 'a') return 'selectAll'
    if (key === 'k') return 'quickAdd'
    return null
  }
  if (e.shiftKey && (e.code === 'Digit1' || key === '!' || key === '1')) return 'fitView'
  if (e.shiftKey && key === 'l') return 'autoLayout'
  if (key === '?') return 'help'
  if (key === '/') return 'quickAdd'
  if (key === 'Delete' || key === 'Backspace') return 'delete'
  if (key === 'F2') return 'rename'
  if (key === 'ArrowLeft') return 'navLeft'
  if (key === 'ArrowRight') return 'navRight'
  if (key === 'ArrowUp') return 'navUp'
  if (key === 'ArrowDown') return 'navDown'
  return null
}

export type ShortcutHandlers = Partial<Record<CanvasAction, (e: KeyboardEvent) => boolean | void>>

/**
 * Listens on window while `active()` holds. A handler that returns false lets the
 * key through (e.g. Esc with nothing to close); otherwise the default is prevented.
 */
export function useCanvasShortcuts(handlers: ShortcutHandlers, active: () => boolean = () => true) {
  function onKeydown(e: KeyboardEvent) {
    if (e.defaultPrevented || !active()) return
    const action = resolveShortcut(e)
    if (!action) return
    const fn = handlers[action]
    if (!fn) return
    if (fn(e) === false) return
    e.preventDefault()
    e.stopPropagation()
  }
  if (typeof window !== 'undefined') window.addEventListener('keydown', onKeydown)
  const stop = () => {
    if (typeof window !== 'undefined') window.removeEventListener('keydown', onKeydown)
  }
  if (getCurrentScope()) onScopeDispose(stop)
  return { stop, onKeydown }
}

export function formatKey(k: string, mac = isMac()): string {
  if (k === 'Mod') return mac ? '⌘' : 'Ctrl'
  if (k === 'Shift') return mac ? '⇧' : 'Shift'
  return k
}

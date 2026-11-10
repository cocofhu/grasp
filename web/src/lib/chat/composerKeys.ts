/**
 * IME composition keys must never act as a send. Safari reports the Enter
 * that commits a candidate after compositionend, with keyCode 229.
 */
export function isComposingKey(event: KeyboardEvent, composing = false): boolean {
  return composing || event.isComposing || event.keyCode === 229
}

/** A bare Enter outside composition: the chat composer's send key. */
export function isPlainEnter(event: KeyboardEvent, composing = false): boolean {
  return event.key === 'Enter' && !event.shiftKey && !event.ctrlKey && !event.altKey && !event.metaKey && !isComposingKey(event, composing)
}

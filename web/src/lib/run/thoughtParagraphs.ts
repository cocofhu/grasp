/**
 * Split thought text into paragraphs. Consecutive blank lines collapse so
 * the UI can use a fixed 8px gap instead of drawing empty pre-wrap rows.
 */
export function thoughtParagraphs(text: string): string[] {
  return String(text || '')
    .replace(/\r\n/g, '\n')
    .split(/\n\s*\n/)
    .map((p) => p.replace(/[ \t]*\n[ \t]*/g, ' ').trim())
    .filter(Boolean)
}

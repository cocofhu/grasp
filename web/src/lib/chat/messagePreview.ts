import { renderChatMarkdown } from '@/lib/shared/markdown'

/** Match the last visible message block, keeping all paragraphs inside it. */
export function lastMessageText(parts: readonly { kind: string; text?: string }[] | undefined, fallback = ''): string {
  const blocks = parts || []
  for (let index = blocks.length - 1; index >= 0; index--) {
    const part = blocks[index]!
    if (part.kind === 'message' && part.text?.trim()) return part.text
  }
  return fallback
}

/** Readable navigation text; the inert fragment is never inserted into the page. */
export function messagePreviewText(markdown: string): string {
  const template = document.createElement('template')
  template.innerHTML = renderChatMarkdown(markdown)
  for (const image of template.content.querySelectorAll('img')) {
    image.replaceWith(document.createTextNode(image.getAttribute('alt') || ''))
  }
  // textContent alone joins adjacent paragraphs and table cells without spaces.
  for (const block of template.content.querySelectorAll('br, p, div, h1, h2, h3, h4, h5, h6, li, pre, blockquote, tr, th, td, hr')) {
    block.after(document.createTextNode(' '))
  }
  return (template.content.textContent || '').replace(/\s+/g, ' ').trim()
}

const MIRRORED = [
  'fontFamily', 'fontSize', 'fontStyle', 'fontVariant', 'fontWeight', 'fontStretch', 'lineHeight', 'letterSpacing',
  'wordSpacing', 'textIndent', 'textTransform', 'tabSize', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft',
  'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth', 'borderStyle', 'wordBreak', 'overflowWrap',
] as const

/**
 * Where the character at `index` sits inside a textarea, relative to the
 * textarea's border box and after its scroll. Measured with a hidden mirror
 * that wraps the text the same way.
 */
export function textareaCaretRect(el: HTMLTextAreaElement, index: number): { left: number; top: number; height: number } {
  const style = getComputedStyle(el)
  const mirror = document.createElement('div')
  for (const key of MIRRORED) mirror.style[key] = style[key]
  Object.assign(mirror.style, {
    position: 'absolute', top: '0', left: '-9999px', visibility: 'hidden', overflow: 'hidden',
    whiteSpace: 'pre-wrap', boxSizing: 'content-box', height: 'auto',
    width: `${el.clientWidth - (parseFloat(style.paddingLeft) || 0) - (parseFloat(style.paddingRight) || 0)}px`,
  })
  mirror.textContent = el.value.slice(0, index)
  const mark = document.createElement('span')
  mark.textContent = el.value.slice(index) || '.'
  mirror.appendChild(mark)
  document.body.appendChild(mirror)
  const lineHeight = parseFloat(style.lineHeight) || (parseFloat(style.fontSize) || 16) * 1.4
  const rect = {
    left: mark.offsetLeft - el.scrollLeft,
    top: mark.offsetTop - el.scrollTop,
    height: lineHeight,
  }
  mirror.remove()
  return rect
}

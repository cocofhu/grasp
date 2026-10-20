/** Insert / find / highlight helpers aligned with the approved page.html Demo. */

import { escapeHtml } from '@/lib/shared/highlightJson'

export type DraftInsertCmd =
  | 'h1'
  | 'h2'
  | 'h3'
  | 'bold'
  | 'italic'
  | 'code'
  | 'link'
  | 'ul'
  | 'ol'
  | 'fence'
  | 'table'

export type DraftInsertInput = {
  value: string
  selectionStart: number
  selectionEnd: number
  cmd: DraftInsertCmd
}

export type DraftInsertResult = {
  value: string
  selectionStart: number
  selectionEnd: number
}

const TABLE_TEMPLATE = '| 列 A | 列 B |\n| --- | --- |\n| 单元格 | 单元格 |'

/** Demo find-bar scroll uses ~19px per line (12px / 1.6). */
export const DRAFT_SRC_LINE_PX = 19

export function insertDraftMarkdown(input: DraftInsertInput): DraftInsertResult {
  const start = Math.max(0, input.selectionStart)
  const end = Math.max(start, input.selectionEnd)
  const val = input.value
  const sel = val.slice(start, end)
  const before = val.slice(0, start)
  const after = val.slice(end)
  let ins = ''
  let cursor = start
  let markEnd = end

  function wrap(left: string, right: string, placeholder: string) {
    const inner = sel || placeholder
    ins = left + inner + right
    cursor = start + left.length
    markEnd = cursor + inner.length
  }

  const nl = before && !/\n$/.test(before) ? '\n' : ''

  switch (input.cmd) {
    case 'h1':
      ins = nl + '# ' + (sel || '标题')
      cursor = start + ins.length
      markEnd = cursor
      break
    case 'h2':
      ins = nl + '## ' + (sel || '标题')
      cursor = start + ins.length
      markEnd = cursor
      break
    case 'h3':
      ins = nl + '### ' + (sel || '标题')
      cursor = start + ins.length
      markEnd = cursor
      break
    case 'bold':
      wrap('**', '**', '粗体')
      break
    case 'italic':
      wrap('*', '*', '斜体')
      break
    case 'code':
      wrap('`', '`', 'code')
      break
    case 'link': {
      const text = sel || '链接文本'
      ins = '[' + text + '](https://)'
      cursor = start + text.length + 3
      markEnd = cursor + 8
      break
    }
    case 'ul':
      ins = nl + '- ' + (sel || '列表项')
      cursor = start + ins.length
      markEnd = cursor
      break
    case 'ol':
      ins = nl + '1. ' + (sel || '列表项')
      cursor = start + ins.length
      markEnd = cursor
      break
    case 'fence': {
      const inner = sel || 'code'
      ins = '```\n' + inner + '\n```'
      cursor = start + 4
      markEnd = cursor + inner.length
      break
    }
    case 'table':
      ins = TABLE_TEMPLATE
      cursor = start
      markEnd = start + ins.length
      break
    default:
      return { value: val, selectionStart: start, selectionEnd: end }
  }

  return {
    value: before + ins + after,
    selectionStart: cursor,
    selectionEnd: markEnd,
  }
}

export type DraftFindInput = {
  value: string
  query: string
  from: number
}

export type DraftFindResult = {
  index: number
  end: number
  scrollTop: number
}

/** Wrap-around search; scrollTop matches Demo `(lines - 4) * 19`. */
export function findNextDraftMatch(input: DraftFindInput): DraftFindResult | null {
  const q = input.query
  if (!q) return null
  const t = input.value
  const from = input.from || 0
  let i = t.indexOf(q, from)
  if (i < 0) i = t.indexOf(q, 0)
  if (i < 0) return null
  const lines = t.slice(0, i).split('\n').length
  return {
    index: i,
    end: i + q.length,
    scrollTop: Math.max(0, (lines - 4) * DRAFT_SRC_LINE_PX),
  }
}

export function highlightDraftSource(text: string): string {
  let s = escapeHtml(text)
  s = s.replace(/```[\s\S]*?```/g, (m) => `<span class="rd-tok-code">${m}</span>`)
  s = s.replace(/`[^`\n]+`/g, (m) => `<span class="rd-tok-code">${m}</span>`)
  s = s.replace(/^#{1,3} .+$/gm, (m) => `<span class="rd-tok-head">${m}</span>`)
  s = s.replace(/\*\*[^*\n]+\*\*/g, (m) => `<span class="rd-tok-bold">${m}</span>`)
  s = s.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, (_, a, b) => `${a}<span class="rd-tok-em">*${b}*</span>`)
  s = s.replace(/\[[^\]]+\]\([^)]+\)/g, (m) => `<span class="rd-tok-link">${m}</span>`)
  return s || ' '
}

export function clampDraftSplitRatio(ratio: number): number {
  return Math.max(0.28, Math.min(0.72, ratio))
}

export function syncScrollTop(fromEl: HTMLElement, toEl: HTMLElement) {
  const max = fromEl.scrollHeight - fromEl.clientHeight
  const omax = toEl.scrollHeight - toEl.clientHeight
  if (max > 0 && omax > 0) {
    toEl.scrollTop = (fromEl.scrollTop / max) * omax
  }
}

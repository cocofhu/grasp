import { describe, expect, it } from 'vitest'
import {
  clampDraftSplitRatio,
  findNextDraftMatch,
  highlightDraftSource,
  insertDraftMarkdown,
} from './requirementDraftMarkdown'

describe('insertDraftMarkdown (Demo insert)', () => {
  it('wraps selection with bold and reselects inner text', () => {
    const r = insertDraftMarkdown({
      value: 'ab',
      selectionStart: 0,
      selectionEnd: 2,
      cmd: 'bold',
    })
    expect(r.value).toBe('**ab**')
    expect(r.selectionStart).toBe(2)
    expect(r.selectionEnd).toBe(4)
  })

  it('inserts bold placeholder when there is no selection', () => {
    const r = insertDraftMarkdown({
      value: 'x',
      selectionStart: 1,
      selectionEnd: 1,
      cmd: 'bold',
    })
    expect(r.value).toBe('x**粗体**')
    expect(r.selectionStart).toBe(3)
    expect(r.selectionEnd).toBe(5)
  })

  it('wraps italic and inline code placeholders', () => {
    expect(
      insertDraftMarkdown({ value: '', selectionStart: 0, selectionEnd: 0, cmd: 'italic' }).value,
    ).toBe('*斜体*')
    expect(
      insertDraftMarkdown({ value: '', selectionStart: 0, selectionEnd: 0, cmd: 'code' }).value,
    ).toBe('`code`')
  })

  it('prefixes H1/H2/H3 with a newline when not at line start', () => {
    const h1 = insertDraftMarkdown({
      value: 'hi',
      selectionStart: 2,
      selectionEnd: 2,
      cmd: 'h1',
    })
    expect(h1.value).toBe('hi\n# 标题')
    const h2 = insertDraftMarkdown({
      value: '',
      selectionStart: 0,
      selectionEnd: 0,
      cmd: 'h2',
    })
    expect(h2.value).toBe('## 标题')
    const h3 = insertDraftMarkdown({
      value: 'a\n',
      selectionStart: 2,
      selectionEnd: 2,
      cmd: 'h3',
    })
    expect(h3.value).toBe('a\n### 标题')
  })

  it('builds a link and selects the URL placeholder', () => {
    const r = insertDraftMarkdown({
      value: '',
      selectionStart: 0,
      selectionEnd: 0,
      cmd: 'link',
    })
    expect(r.value).toBe('[链接文本](https://)')
    expect(r.value.slice(r.selectionStart, r.selectionEnd)).toBe('https://')
  })

  it('uses selection as link text', () => {
    const r = insertDraftMarkdown({
      value: 'docs',
      selectionStart: 0,
      selectionEnd: 4,
      cmd: 'link',
    })
    expect(r.value).toBe('[docs](https://)')
    expect(r.value.slice(r.selectionStart, r.selectionEnd)).toBe('https://')
  })

  it('inserts ul/ol with optional leading newline', () => {
    expect(
      insertDraftMarkdown({ value: 'a', selectionStart: 1, selectionEnd: 1, cmd: 'ul' }).value,
    ).toBe('a\n- 列表项')
    expect(
      insertDraftMarkdown({ value: '', selectionStart: 0, selectionEnd: 0, cmd: 'ol' }).value,
    ).toBe('1. 列表项')
  })

  it('inserts a fenced code block and selects the body', () => {
    const r = insertDraftMarkdown({
      value: '',
      selectionStart: 0,
      selectionEnd: 0,
      cmd: 'fence',
    })
    expect(r.value).toBe('```\ncode\n```')
    expect(r.value.slice(r.selectionStart, r.selectionEnd)).toBe('code')
  })

  it('inserts a minimal GFM table template', () => {
    const r = insertDraftMarkdown({
      value: '',
      selectionStart: 0,
      selectionEnd: 0,
      cmd: 'table',
    })
    expect(r.value).toBe('| 列 A | 列 B |\n| --- | --- |\n| 单元格 | 单元格 |')
    expect(r.selectionStart).toBe(0)
    expect(r.selectionEnd).toBe(r.value.length)
  })
})

describe('findNextDraftMatch', () => {
  it('finds from the caret and wraps around', () => {
    const first = findNextDraftMatch({ value: 'foo bar foo', query: 'foo', from: 1 })
    expect(first?.index).toBe(8)
    const wrap = findNextDraftMatch({ value: 'foo bar foo', query: 'foo', from: 11 })
    expect(wrap?.index).toBe(0)
  })

  it('returns null when there is no match or empty query', () => {
    expect(findNextDraftMatch({ value: 'abc', query: 'z', from: 0 })).toBeNull()
    expect(findNextDraftMatch({ value: 'abc', query: '', from: 0 })).toBeNull()
  })

  it('computes Demo-style scrollTop from line index', () => {
    const value = 'a\nb\nc\nd\ne\nfindme'
    const r = findNextDraftMatch({ value, query: 'findme', from: 0 })
    expect(r?.index).toBe(value.indexOf('findme'))
    expect(r?.scrollTop).toBe((6 - 4) * 19)
  })
})

describe('highlightDraftSource', () => {
  it('marks headings, emphasis, code and links', () => {
    const html = highlightDraftSource('# 标题\n**粗** *斜* `code` [a](https://x)')
    expect(html).toContain('rd-tok-head')
    expect(html).toContain('rd-tok-bold')
    expect(html).toContain('rd-tok-em')
    expect(html).toContain('rd-tok-code')
    expect(html).toContain('rd-tok-link')
  })

  it('escapes raw HTML so scripts stay inert', () => {
    const html = highlightDraftSource('<script>alert(1)</script>')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })
})

describe('clampDraftSplitRatio', () => {
  it('clamps to 0.28–0.72', () => {
    expect(clampDraftSplitRatio(0.1)).toBe(0.28)
    expect(clampDraftSplitRatio(0.9)).toBe(0.72)
    expect(clampDraftSplitRatio(0.5)).toBe(0.5)
  })
})

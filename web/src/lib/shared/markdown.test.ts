import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('dompurify', () => ({
  default: {
    sanitize: (html: string) => html,
  },
}))

import {
  clearMarkdownCache,
  getMarkdownParseCount,
  markdownCacheSize,
  renderMarkdown,
  renderMarkdownBlocks,
  resetMarkdownParseCount,
} from './markdown'

afterEach(() => {
  clearMarkdownCache()
  resetMarkdownParseCount()
})

describe('renderMarkdown cache', () => {
  it('parses once for identical source and returns the same HTML', () => {
    const src = '# Hello\n\n**world**'
    const a = renderMarkdown(src)
    expect(getMarkdownParseCount()).toBe(1)
    expect(a).toContain('<strong>world</strong>')

    const b = renderMarkdown(src)
    expect(getMarkdownParseCount()).toBe(1)
    expect(b).toBe(a)
    expect(markdownCacheSize()).toBe(1)
  })

  it('parses again when source text changes', () => {
    renderMarkdown('one')
    renderMarkdown('two')
    expect(getMarkdownParseCount()).toBe(2)
    expect(markdownCacheSize()).toBe(2)
  })

  it('evicts oldest entries beyond LRU capacity', () => {
    for (let i = 0; i < 70; i++) {
      renderMarkdown(`doc-${i}`)
    }
    expect(markdownCacheSize()).toBe(64)
    // Oldest should be gone → re-parse
    const before = getMarkdownParseCount()
    renderMarkdown('doc-0')
    expect(getMarkdownParseCount()).toBe(before + 1)
  })
})

describe('renderMarkdownBlocks', () => {
  it('renders one HTML string per top-level block', () => {
    const cache = new Map<string, string>()
    const blocks = renderMarkdownBlocks('# Title\n\npara **one**\n\n- a\n- b\n\n```ts\nconst x = 1\n```', cache)
    expect(blocks).toHaveLength(4)
    expect(blocks[0]).toContain('<h1')
    expect(blocks[1]).toContain('<strong>one</strong>')
    expect(blocks[2]).toContain('<li>a</li>')
    expect(blocks[3]).toContain('language-ts')
  })

  it('re-parses only the block still being written', () => {
    const cache = new Map<string, string>()
    const first = renderMarkdownBlocks('# Title\n\npara', cache)
    resetMarkdownParseCount()
    const next = renderMarkdownBlocks('# Title\n\npara grows', cache)
    expect(getMarkdownParseCount()).toBe(1)
    expect(next[0]).toBe(first[0])
    expect(next[1]).toContain('para grows')
  })

  it('resolves reference links across blocks and drops empty ones', () => {
    const cache = new Map<string, string>()
    const blocks = renderMarkdownBlocks('see [docs][d]\n\n[d]: https://example.com', cache)
    expect(blocks).toHaveLength(1)
    expect(blocks[0]).toContain('href="https://example.com"')
  })

  it('prunes stale entries once the cache outgrows the live set', () => {
    const cache = new Map<string, string>()
    for (let i = 0; i < 80; i++) renderMarkdownBlocks(`p${i}`, cache)
    expect(cache.size).toBeLessThanOrEqual(65)
    expect(renderMarkdownBlocks('', cache)).toEqual([])
  })
})

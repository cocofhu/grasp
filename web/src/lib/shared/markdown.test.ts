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
  renderChatMarkdown,
  renderChatMarkdownBlocks,
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

describe('renderChatMarkdown', () => {
  const src = '说明如下：<script>关键步骤</script>\n\n**加粗**\n\n```js\nconst x = 1\n```'

  it('escapes raw HTML but still renders emphasis and code', () => {
    const html = renderChatMarkdown(src)
    expect(html).toContain('关键步骤')
    expect(html).toContain('&lt;script&gt;')
    expect(html).not.toContain('<script')
    expect(html).toContain('<strong>加粗</strong>')
    expect(html).toContain('const x = 1')
    expect(html).toContain('language-js')
  })

  it('does not change artifact markdown, which still receives raw HTML', () => {
    const plain = renderMarkdown('hi <script>x</script>')
    expect(plain).toContain('<script>')
    const chat = renderChatMarkdown('hi <script>x</script>')
    expect(chat).not.toContain('<script')
    expect(chat).toContain('x')
  })

  it('treats ATX headings with or without a space after the hashes', () => {
    expect(renderChatMarkdown('## 上游交接')).toMatch(/<h2[^>]*>上游交接<\/h2>/)
    expect(renderChatMarkdown('##上游交接')).toMatch(/<h2[^>]*>上游交接<\/h2>/)
  })

  it('keeps fenced code characters that look like tags', () => {
    const html = renderChatMarkdown('```html\n<script>alert(1)</script>\n```')
    expect(html).toContain('language-html')
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
    expect(html).not.toContain('<script')
  })

  it('renders the same blocks while a reply streams', () => {
    const cache = new Map<string, string>()
    const blocks = renderChatMarkdownBlocks(src, cache)
    expect(blocks.join('')).toContain('关键步骤')
    expect(blocks.join('')).toContain('<strong>加粗</strong>')
    expect(blocks.join('')).toContain('const x = 1')
    expect(blocks.join('')).not.toContain('<script')
  })
})

const SCREENSHOT_URL = 'http://127.0.0.1:18080/solo/s-running'
const SCREENSHOT_SENTENCE =
  `${SCREENSHOT_URL}（中文）。这是模拟会话，不是真实后端。交付时不要把会话环境入口加回更多菜单、错误卡或空态。`

function anchors(html: string): { href: string; text: string }[] {
  return [...html.matchAll(/<a href="([^"]*)">([^<]*)<\/a>/g)].map((match) => ({
    href: match[1].replace(/&amp;/g, '&'),
    text: match[2].replace(/&amp;/g, '&'),
  }))
}

describe('bare URL boundary', () => {
  const renders = [renderChatMarkdown, renderMarkdown]

  it('keeps the screenshot sentence link on the URL for every renderer', () => {
    const htmls = [
      renderChatMarkdown(SCREENSHOT_SENTENCE),
      renderMarkdown(SCREENSHOT_SENTENCE),
      renderChatMarkdownBlocks(SCREENSHOT_SENTENCE, new Map()).join(''),
      renderMarkdownBlocks(SCREENSHOT_SENTENCE, new Map()).join(''),
    ]
    for (const html of htmls) {
      expect(anchors(html)).toEqual([{ href: SCREENSHOT_URL, text: SCREENSHOT_URL }])
      expect(html).toContain(`${SCREENSHOT_URL}</a>（中文）。这是模拟会话`)
      expect(html).not.toContain('s-running（')
    }
  })

  it('leaves a newline before the URL outside the anchor', () => {
    const src = `桌面浏览器停在\n${SCREENSHOT_SENTENCE}`
    for (const render of renders) {
      const html = render(src)
      expect(html).toContain('停在<br>')
      expect(anchors(html)).toEqual([{ href: SCREENSHOT_URL, text: SCREENSHOT_URL }])
      expect(html).toContain(`${SCREENSHOT_URL}</a>（中文）`)
    }
  })

  it('stops before fullwidth punctuation', () => {
    for (const punct of ['。', '，', '、', '；', '：', '（', '）', '！', '？']) {
      for (const render of renders) {
        const html = render(`https://example.com/a${punct}尾`)
        expect(anchors(html)).toEqual([{ href: 'https://example.com/a', text: 'https://example.com/a' }])
        expect(html).toContain(`</a>${punct}尾`)
      }
    }
  })

  it('keeps a query string and leaves the following Chinese comma outside', () => {
    const url = 'https://example.com/a?q=1&x=2'
    for (const render of renders) {
      const html = render(`${url}，然后继续`)
      expect(anchors(html)).toEqual([{ href: url, text: url }])
      expect(html).toContain('</a>，然后继续')
    }
  })

  it('keeps balanced parentheses, percent-encoding, and IPv6 inside the link', () => {
    const wiki = 'https://en.wikipedia.org/wiki/URL_(disambiguation)'
    const encoded = 'https://example.com/%E4%B8%AD'
    const ipv6 = 'http://[::1]:8080/solo'
    for (const render of renders) {
      expect(anchors(render(`${wiki}（说明）`))).toEqual([{ href: wiki, text: wiki }])
      expect(anchors(render(`${encoded}文`))).toEqual([{ href: encoded, text: encoded }])
      const ipv6Html = render(`${ipv6}（中文）`)
      const [link] = anchors(ipv6Html)
      expect(link.text).toBe(ipv6)
      expect(decodeURI(link.href)).toBe(ipv6)
      expect(ipv6Html).toContain('</a>（中文）')
    }
  })

  it('drops trailing ASCII punctuation but not a balanced closing paren', () => {
    for (const punct of ['.', ',', '!', '?', ')']) {
      for (const render of renders) {
        const html = render(`https://example.com/path${punct}`)
        expect(anchors(html)).toEqual([
          { href: 'https://example.com/path', text: 'https://example.com/path' },
        ])
        expect(html).toContain(`</a>${punct}`)
      }
    }
    const wiki = 'https://en.wikipedia.org/wiki/URL_(disambiguation)'
    expect(anchors(renderChatMarkdown(`${wiki}.`))).toEqual([{ href: wiki, text: wiki }])
    expect(renderChatMarkdown(`${wiki}.`)).toContain('</a>.')
  })

  it('links each bare URL on its own', () => {
    const first = 'http://a.example/one'
    const second = 'https://b.example/two'
    for (const render of renders) {
      expect(anchors(render(`${first} ${second}`))).toEqual([
        { href: first, text: first },
        { href: second, text: second },
      ])
      expect(anchors(render(`${first}和${second}`))).toEqual([
        { href: first, text: first },
        { href: second, text: second },
      ])
    }
  })

  it('leaves explicit links, code, and spaced Chinese alone', () => {
    for (const render of renders) {
      expect(anchors(render('[中文说明](https://example.com)'))).toEqual([
        { href: 'https://example.com', text: '中文说明' },
      ])
      expect(anchors(render('http://example.com/a 然后'))).toEqual([
        { href: 'http://example.com/a', text: 'http://example.com/a' },
      ])
      const inline = render('`http://127.0.0.1:18080/solo/s-running（中文）`')
      const fenced = render('```\nhttp://127.0.0.1:18080/solo/s-running（中文）。\n```')
      expect(inline).not.toContain('<a ')
      expect(inline).toContain('s-running（中文）')
      expect(fenced).not.toContain('<a ')
      expect(fenced).toContain('s-running（中文）')
    }
  })

  it('still links www. and ftp without adopting host:port or a bare scheme', () => {
    for (const render of renders) {
      expect(anchors(render('www.example.com/docs（说明）'))).toEqual([
        { href: 'http://www.example.com/docs', text: 'www.example.com/docs' },
      ])
      expect(anchors(render('ftp://files.example.com/a（中文）'))).toEqual([
        { href: 'ftp://files.example.com/a', text: 'ftp://files.example.com/a' },
      ])
      expect(anchors(render('写给 user@example.com。'))).toEqual([
        { href: 'mailto:user@example.com', text: 'user@example.com' },
      ])
      expect(anchors(render('<https://example.com/a>'))).toEqual([
        { href: 'https://example.com/a', text: 'https://example.com/a' },
      ])
      for (const src of ['http://', 'http://（中文）', 'http://.', '127.0.0.1:8080', '0.0.0.0:18080', 'javascript:alert(1)']) {
        expect(render(src)).not.toContain('<a ')
      }
    }
  })
})

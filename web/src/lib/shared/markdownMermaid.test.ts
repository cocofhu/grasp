// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushMermaidQueue } from '@/components/run/mermaidRenderQueue'
import { clearMarkdownCache, renderChatMarkdown, renderMarkdown } from './markdown'
import { resetMarkdownMermaidForTest } from './markdownMermaid'

const mermaidInitialize = vi.fn()
const mermaidParse = vi.fn()
const mermaidRender = vi.fn()

// happy-dom's parser drops the first element's tag inside DOMPurify; the browser e2e covers the real sanitizer.
vi.mock('dompurify', () => ({
  default: {
    sanitize: (html: string) => html,
  },
}))

vi.mock('mermaid', () => ({
  default: {
    initialize: (...args: unknown[]) => mermaidInitialize(...args),
    parse: (...args: unknown[]) => mermaidParse(...args),
    render: (...args: unknown[]) => mermaidRender(...args),
  },
}))

const SOURCE = 'flowchart LR\n  A[凭证页] --> B[CodexLoginManager]'
const FENCE = `\`\`\`mermaid\n${SOURCE}\n\`\`\``

/** SVG tagged with the theme Mermaid was initialized with, using the render id like Mermaid does. */
function themedSvg(id: string) {
  const theme = mermaidInitialize.mock.calls.at(-1)?.[0]?.theme
  return `<svg id="${id}" data-theme="${theme}"><marker id="${id}_arrow"></marker><path marker-end="url(#${id}_arrow)"></path></svg>`
}

async function settle() {
  for (let i = 0; i < 4; i++) {
    await Promise.resolve()
    await flushMermaidQueue()
  }
}

function mountHtml(html: string) {
  const host = document.createElement('div')
  host.className = 'md'
  host.innerHTML = html
  document.body.appendChild(host)
  return host
}

beforeEach(() => {
  document.documentElement.className = ''
  mermaidParse.mockResolvedValue(true)
  mermaidRender.mockImplementation(async (id: string) => ({ svg: themedSvg(id) }))
})

afterEach(() => {
  resetMarkdownMermaidForTest()
  clearMarkdownCache()
  document.body.innerHTML = ''
  document.documentElement.className = ''
  vi.clearAllMocks()
})

describe('markdown mermaid hydration', () => {
  it('draws a fenced diagram from chat markdown and hides the source', async () => {
    const host = mountHtml(renderChatMarkdown(`架构如下\n\n${FENCE}`))
    await settle()

    const pre = host.querySelector<HTMLElement>('pre.md-mermaid')!
    expect(pre.dataset.mermaid).toBe('done')
    expect(pre.querySelector('.md-mermaid-svg svg')?.getAttribute('data-theme')).toBe('dark')
    expect(pre.querySelector('code')?.textContent).toBe(SOURCE)
    expect(mermaidParse).toHaveBeenCalledWith(SOURCE)
    expect(mermaidInitialize).toHaveBeenLastCalledWith(
      expect.objectContaining({ securityLevel: 'strict', suppressErrorRendering: true, theme: 'dark' }),
    )
  })

  it('redraws in the light theme and back when <html> switches', async () => {
    const host = mountHtml(renderMarkdown(FENCE))
    await settle()
    const svgTheme = () => host.querySelector('.md-mermaid-svg svg')?.getAttribute('data-theme')
    expect(svgTheme()).toBe('dark')

    document.documentElement.classList.add('light')
    await settle()
    expect(svgTheme()).toBe('base')
    expect(mermaidInitialize).toHaveBeenLastCalledWith(expect.objectContaining({ theme: 'base' }))

    document.documentElement.classList.remove('light')
    await settle()
    expect(svgTheme()).toBe('dark')
    // The dark SVG came back from the cache.
    expect(mermaidRender).toHaveBeenCalledTimes(2)
  })

  it('ignores <html> class changes that keep the theme', async () => {
    mountHtml(renderMarkdown(FENCE))
    await settle()
    document.documentElement.classList.add('route-pending')
    await settle()
    expect(mermaidRender).toHaveBeenCalledTimes(1)
  })

  it('keeps the source visible when Mermaid cannot parse it', async () => {
    mermaidParse.mockRejectedValue(new Error('Parse error'))
    const host = mountHtml(renderMarkdown('```mermaid\nflowchart LR\n  A -->\n```'))
    await settle()

    const pre = host.querySelector<HTMLElement>('pre.md-mermaid')!
    expect(pre.dataset.mermaid).toBe('failed')
    expect(pre.querySelector('.md-mermaid-svg')).toBeNull()
    expect(mermaidRender).not.toHaveBeenCalled()

    document.documentElement.classList.add('light')
    await settle()
    expect(mermaidParse).toHaveBeenCalledTimes(1)
  })

  it('retries a transient render failure once', async () => {
    mermaidRender.mockRejectedValueOnce(new Error('busy'))
    const host = mountHtml(renderMarkdown(FENCE))
    await settle()
    expect(host.querySelector<HTMLElement>('pre.md-mermaid')!.dataset.mermaid).toBe('done')
    expect(mermaidRender).toHaveBeenCalledTimes(2)
  })

  it('reuses the SVG when Vue replaces the block with the same diagram', async () => {
    const host = mountHtml(renderChatMarkdown(FENCE))
    await settle()
    host.innerHTML = renderChatMarkdown(`${FENCE}\n\n后文`)
    await settle()

    expect(host.querySelector<HTMLElement>('pre.md-mermaid')!.dataset.mermaid).toBe('done')
    expect(mermaidRender).toHaveBeenCalledTimes(1)
  })

  it('renders two identical blocks once, each with its own ids', async () => {
    const host = mountHtml(renderMarkdown(`${FENCE}\n\n${FENCE}`))
    await settle()
    const pres = host.querySelectorAll<HTMLElement>('pre.md-mermaid')
    expect(pres).toHaveLength(2)
    expect([...pres].every((p) => p.dataset.mermaid === 'done')).toBe(true)
    expect(mermaidRender).toHaveBeenCalledTimes(1)

    const renderId = mermaidRender.mock.calls[0][0] as string
    const ids = [...host.querySelectorAll('.md-mermaid-svg svg')].map((svg) => svg.id)
    expect(new Set(ids).size).toBe(2)
    expect(ids).not.toContain(renderId)
    for (const pre of pres) {
      const svg = pre.querySelector('svg')!
      expect(svg.querySelector('marker')!.id).toBe(`${svg.id}_arrow`)
      expect(svg.querySelector('path')!.getAttribute('marker-end')).toBe(`url(#${svg.id}_arrow)`)
    }
  })

  it('does not retry a diagram that failed to render on later scans', async () => {
    mermaidRender.mockRejectedValue(new Error('broken'))
    const host = mountHtml(renderMarkdown(FENCE))
    await settle()
    expect(host.querySelector<HTMLElement>('pre.md-mermaid')!.dataset.mermaid).toBe('failed')
    expect(mermaidRender).toHaveBeenCalledTimes(2)

    mountHtml(renderChatMarkdown(FENCE))
    await settle()
    expect(mermaidRender).toHaveBeenCalledTimes(2)
  })

  it('never shows a render that finished after the theme switched', async () => {
    let releaseDark!: () => void
    mermaidRender.mockImplementationOnce(
      (id: string) => new Promise((resolve) => {
        const svg = themedSvg(id)
        releaseDark = () => resolve({ svg })
      }),
    )
    const host = mountHtml(renderMarkdown(FENCE))
    await vi.waitFor(() => expect(mermaidRender).toHaveBeenCalledTimes(1))
    const themes: string[] = []
    new MutationObserver(() => {
      const theme = host.querySelector('.md-mermaid-svg svg')?.getAttribute('data-theme')
      if (theme) themes.push(theme)
    }).observe(host, { childList: true, subtree: true })

    document.documentElement.classList.add('light')
    releaseDark()
    await settle()

    expect(host.querySelector('.md-mermaid-svg svg')?.getAttribute('data-theme')).toBe('base')
    expect(themes).not.toContain('dark')
  })

  it('does not draw into a block that left the page', async () => {
    let release!: () => void
    mermaidRender.mockImplementationOnce(
      () => new Promise((resolve) => (release = () => resolve({ svg: '<svg></svg>' }))),
    )
    const host = mountHtml(renderMarkdown(FENCE))
    await Promise.resolve()
    await vi.waitFor(() => expect(mermaidRender).toHaveBeenCalled())
    const pre = host.querySelector<HTMLElement>('pre.md-mermaid')!
    host.remove()
    release()
    await settle()
    expect(pre.querySelector('.md-mermaid-svg')).toBeNull()
  })

  it('leaves unclosed streaming fences alone', async () => {
    const host = mountHtml(renderChatMarkdown(`\`\`\`mermaid\n${SOURCE}`))
    await settle()
    expect(host.querySelector('pre.md-mermaid')).toBeNull()
    expect(mermaidParse).not.toHaveBeenCalled()
  })
})

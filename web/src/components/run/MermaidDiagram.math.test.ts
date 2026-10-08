// @vitest-environment happy-dom
import { createRequire } from 'node:module'
import { readFileSync, readdirSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import katex from 'katex'
import mermaid from 'mermaid'
import MermaidDiagram from './MermaidDiagram.vue'
import { flushMermaidQueue } from './mermaidRenderQueue'

const require = createRequire(import.meta.url)

/**
 * Chrome exposes MathMLElement, which is what mermaid.core checks before it
 * dynamically imports katex. happy-dom does not, so define it here and the
 * component still initializes mermaid the same way production does.
 */
const view = window as Window & { MathMLElement?: typeof HTMLElement }
if (typeof view.MathMLElement === 'undefined') {
  view.MathMLElement = class MathMLElement extends HTMLElement {}
}

function createI18nPlugin() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: {
      'zh-CN': {
        pages: { plan: { diagramFallback: '图渲染失败,显示源码' } },
      },
    },
  })
}

function mountDiagram(source: string) {
  return mount(MermaidDiagram, {
    props: {
      diagram: { format: 'mermaid', source },
      jsonPath: 'architecture.diagrams[0]',
    },
    global: { plugins: [createI18nPlugin()] },
  })
}

function versionAtLeast(version: string, min: string) {
  const parts = version.split('.').map((n) => Number(n))
  const floor = min.split('.').map((n) => Number(n))
  for (let i = 0; i < 3; i++) {
    const a = parts[i] ?? 0
    const b = floor[i] ?? 0
    if (a !== b) return a > b
  }
  return true
}

/**
 * Mermaid draws into a temporary node, then DOMPurify serializes that node.
 * happy-dom drops an SVG that contains a style element, so the string handed
 * back to the host is empty even though the drawn document is an SVG.
 * Read that node while it still holds the diagram.
 */
function trackDrawnSvg() {
  const proto = Element.prototype
  const desc = Object.getOwnPropertyDescriptor(proto, 'innerHTML')
  if (!desc?.get || !desc.set) {
    throw new Error('happy-dom innerHTML descriptor is missing')
  }
  let svg = ''
  Object.defineProperty(proto, 'innerHTML', {
    configurable: true,
    enumerable: desc.enumerable ?? true,
    get() {
      const value = String(desc.get!.call(this))
      const id = (this as HTMLElement).id || ''
      if (id.startsWith('d') && value.includes('<svg')) svg = value
      return value
    },
    set(value: string) {
      desc.set!.call(this, value)
    },
  })
  return {
    svg: () => svg,
    restore() {
      Object.defineProperty(proto, 'innerHTML', desc)
    },
  }
}

describe('MermaidDiagram math label smoke', () => {
  afterEach(async () => {
    vi.restoreAllMocks()
    document.documentElement.classList.remove('light')
    document.body.innerHTML = ''
    await flushMermaidQueue()
  })

  it('loads katex from the overridden package through mermaid.core', () => {
    const mermaidEntry = require.resolve('mermaid')
    expect(mermaidEntry.replaceAll('\\', '/')).toMatch(/\/dist\/mermaid\.core\.mjs$/)
    expect(readFileSync(mermaidEntry, 'utf8')).not.toContain('0.16.47')

    const mermaidDir = dirname(require.resolve('mermaid/package.json'))
    const coreChunks = join(mermaidDir, 'dist/chunks/mermaid.core')
    const coreSource = readdirSync(coreChunks)
      .filter((name) => name.endsWith('.mjs'))
      .map((name) => readFileSync(join(coreChunks, name), 'utf8'))
      .join('\n')
    expect(coreSource).toContain('await import("katex")')
    expect(coreSource).not.toContain('0.16.47')
    expect(existsSync(join(mermaidDir, 'node_modules/katex'))).toBe(false)

    const katexFromMermaid = createRequire(require.resolve('mermaid/package.json')).resolve('katex/package.json')
    const resolved = JSON.parse(readFileSync(katexFromMermaid, 'utf8')) as { version: string }
    expect(resolved.version).toBe(katex.version)
    expect(versionAtLeast(katex.version, '0.18.2')).toBe(true)
    expect(katex.version.startsWith('0.18.')).toBe(true)

    const mermaidPkg = JSON.parse(readFileSync(require.resolve('mermaid/package.json'), 'utf8')) as { version: string }
    expect(mermaidPkg.version.startsWith('11.17.')).toBe(true)
  })

  it('renders an SVG when a label contains a math fragment', async () => {
    const drawn = trackDrawnSvg()
    const renderToString = vi.spyOn(katex, 'renderToString')
    const initialize = vi.spyOn(mermaid, 'initialize')
    const wrapper = mountDiagram('flowchart LR\n  A["$$E=mc^2$$"] --> B')
    try {
      await flushPromises()
      await flushMermaidQueue()
      await flushPromises()

      expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
      const svg = drawn.svg()
      expect(svg).toContain('<svg')
      expect(svg).toContain('<math')
      expect(svg).toContain('katex')
      expect(renderToString).toHaveBeenCalled()
      expect(renderToString.mock.calls[0]?.[0]).toBe('E=mc^2')
      const produced = String(renderToString.mock.results[0]?.value ?? '')
      expect(produced).toContain('<math')
      expect(svg).toContain('<mi')
      const options = renderToString.mock.calls[0]?.[1] as {
        throwOnError?: boolean
        displayMode?: boolean
        output?: string
      }
      expect(options.throwOnError).toBe(true)
      expect(options.displayMode).toBe(true)
      expect(options.output).toBeTruthy()
      expect(versionAtLeast(katex.version, '0.18.2')).toBe(true)
      const cfg = initialize.mock.calls.at(-1)?.[0] as { securityLevel?: string }
      expect(cfg.securityLevel).toBe('strict')
    } finally {
      drawn.restore()
      wrapper.unmount()
    }
  }, 20000)

  it('still falls back to source when the diagram is illegal', async () => {
    const wrapper = mountDiagram('flowchart LR\n  A-->[')
    await flushPromises()
    await flushMermaidQueue()

    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="plan-diagram-fallback-hint"]').text()).toBe('图渲染失败,显示源码')
    expect(wrapper.find('[data-testid="plan-diagram-fallback-source"]').text()).toContain('A-->[')
    expect(wrapper.find('svg').exists()).toBe(false)
    wrapper.unmount()
  }, 20000)
})

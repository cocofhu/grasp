// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * plan g3.1 — JSON 代码板主题令牌契约：
 * :root = VS Code Dark 现状调色板；html.light = VS Code Light+。
 * 三处消费点不再硬编码旧 JSON hex。
 */
const dir = dirname(fileURLToPath(import.meta.url))
const css = readFileSync(join(dir, 'global.css'), 'utf8')
const vueDir = join(dir, '../components')

function block(source: string, selector: string): string {
  const start = source.indexOf(selector)
  expect(start).toBeGreaterThanOrEqual(0)
  const open = source.indexOf('{', start)
  let depth = 0
  for (let i = open; i < source.length; i++) {
    if (source[i] === '{') depth++
    else if (source[i] === '}') {
      depth--
      if (depth === 0) return source.slice(open, i + 1)
    }
  }
  throw new Error(`unclosed ${selector}`)
}

function token(blockCss: string, name: string): string {
  const m = blockCss.match(new RegExp(`${name}:\\s*([^;]+);`))
  expect(m, `${name} in block`).toBeTruthy()
  return m![1].trim()
}

const OLD_JSON_HEX = ['#1e1e1e', '#9cdcfe', '#ce9178', '#b5cea8', '#569cd6', '#d4d4d4', '#7dd3c7']

describe('global.css JSON code tokens (g3.1 / g1.1)', () => {
  const root = block(css, ':root')
  const light = block(css, 'html.light')

  it('dark :root matches VS Code Dark palette', () => {
    expect(token(root, '--json-bg')).toBe('#1e1e1e')
    expect(token(root, '--json-fg')).toBe('#d4d4d4')
    expect(token(root, '--json-key')).toBe('#9cdcfe')
    expect(token(root, '--json-str')).toBe('#ce9178')
    expect(token(root, '--json-num')).toBe('#b5cea8')
    expect(token(root, '--json-kw')).toBe('#569cd6')
    expect(token(root, '--json-punc')).toBe('#d4d4d4')
  })

  it('html.light matches VS Code Light+ corresponding colors', () => {
    expect(token(light, '--json-bg')).toMatch(/^#(fff|ffffff|fefefe)$/i)
    expect(token(light, '--json-key')).toBe('#0451a5')
    expect(token(light, '--json-str')).toBe('#a31515')
    expect(token(light, '--json-num')).toBe('#098658')
    expect(token(light, '--json-kw')).toBe('#0000ff')
    expect(token(light, '--json-punc')).toBe('#393a34')
    expect(token(light, '--json-fg')).toBe('#393a34')
  })

  it('binds .json-code-view and tok-* inside code board / payload only (g1.2)', () => {
    expect(css).toMatch(/\.json-code-view\s*\{[^}]*background:\s*var\(--json-bg\)/s)
    expect(css).toMatch(/\.json-code-view\s*\{[^}]*border:\s*1px solid rgb\(var\(--c-line\)\)/s)
    expect(css).toMatch(/\.json-code-view pre\s*\{[^}]*font-size:\s*12\.5px/s)
    expect(css).toMatch(/\.json-code-view \.tok-key,\s*\n\.payload \.tok-key/)
    expect(css).toMatch(/color:\s*var\(--json-key\)/)
    expect(css).toMatch(/color:\s*var\(--json-str\)/)
    expect(css).toMatch(/color:\s*var\(--json-num\)/)
    expect(css).toMatch(/color:\s*var\(--json-kw\)/)
    expect(css).toMatch(/color:\s*var\(--json-punc\)/)
  })
})

describe('Vue consumers no longer hardcode JSON palette (g3.1 / g2)', () => {
  const files = [
    join(vueDir, 'run/ArtifactPreview.vue'),
    join(vueDir, 'run/UpstreamRequirementContext.vue'),
    join(vueDir, 'workspace/WorkspaceAuditPanel.vue'),
  ]

  it.each(files)('%s does not contain old JSON hex', (file) => {
    const src = readFileSync(file, 'utf8')
    for (const hex of OLD_JSON_HEX) {
      expect(src.toLowerCase()).not.toContain(hex)
    }
  })

  it('keeps .json-code-view class on ArtifactPreview and UpstreamRequirementContext', () => {
    const preview = readFileSync(join(vueDir, 'run/ArtifactPreview.vue'), 'utf8')
    const upstream = readFileSync(join(vueDir, 'run/UpstreamRequirementContext.vue'), 'utf8')
    expect(preview).toContain('class="json-code-view')
    expect(upstream).toContain('class="json-code-view')
    expect(preview).toContain('fallback-tag')
    expect(upstream).toContain('fallback-tag')
  })
})

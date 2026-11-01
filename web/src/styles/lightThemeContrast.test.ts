// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const css = readFileSync(join(dir, 'global.css'), 'utf8')
const sidebar = readFileSync(join(dir, '../components/shell/AppSidebar.vue'), 'utf8')

const block = (selector: string) => css.match(new RegExp(`(?:^|\\n)${selector}\\s*\\{[^}]*\\}`))?.[0] ?? ''

describe('light theme contrast tokens', () => {
  it('darkens light txt2/txt3 and leaves the dark tokens alone', () => {
    const light = block('html\\.light')
    expect(light).toContain('--c-txt2: 52 52 60;')
    expect(light).toContain('--c-txt3: 82 82 91;')
    const dark = block(':root')
    expect(dark).toContain('--c-txt2: 161 161 170;')
    expect(dark).toContain('--c-txt3: 110 110 120;')
  })

  it('whitens the light sidebar rail and keeps the dark rail on base', () => {
    expect(sidebar).toContain('--sidebar-rail-bg: light-dark(#fcfcfc, rgb(var(--c-base)));')
    expect(sidebar).not.toContain('#f6f6f6')
  })
})

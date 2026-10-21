import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const globalCss = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), 'global.css'),
  'utf8',
)

describe('global.css dark theme scrollbar (g2)', () => {
  it('forces dark color-scheme on html:not(.light) to avoid whitish native thumbs (g2.1/g2.2)', () => {
    expect(globalCss).toMatch(/html:not\(\.light\)\s*\{[^}]*color-scheme:\s*dark/)
  })

  it('uses dark base surface for visible scrollbar track in dark theme (g2.2)', () => {
    expect(globalCss).toContain('html:not(.light) *:hover::-webkit-scrollbar-track')
    expect(globalCss).toContain('html:not(.light) *.is-scrolling::-webkit-scrollbar-track')
    expect(globalCss).toMatch(
      /html:not\(\.light\)[\s\S]*scrollbar-color:\s*rgb\(var\(--c-line\)\)\s*rgb\(var\(--c-base\)\)/,
    )
  })

  it('keeps idle-invisible transparent thumb contract (g2.3)', () => {
    expect(globalCss).toContain('scrollbar-color: transparent transparent')
    expect(globalCss).toMatch(/::-webkit-scrollbar-thumb\s*\{[^}]*background:\s*transparent/)
  })

  // Chromium >= 121 drops ::-webkit-scrollbar styling once the standard properties
  // apply, which brought back the wide native bar with arrows.
  it('applies scrollbar-width / scrollbar-color only where ::-webkit-scrollbar is unsupported', () => {
    const outside = globalCss.replace(/@supports not selector\(::-webkit-scrollbar\)\s*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/g, '')
    expect(globalCss).toContain('@supports not selector(::-webkit-scrollbar)')
    expect(outside).not.toMatch(/scrollbar-width:\s*thin/)
    expect(outside).not.toMatch(/scrollbar-color:/)
  })

  it('hides WebKit scrollbar arrow buttons and keeps the thin 4px bar', () => {
    expect(globalCss).toMatch(/::-webkit-scrollbar-button\s*\{[^}]*display:\s*none/)
    expect(globalCss).toMatch(/::-webkit-scrollbar\s*\{[^}]*width:\s*4px/)
  })
})

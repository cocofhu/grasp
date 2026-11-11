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
    const outside = globalCss
      .replace(/@supports not selector\(::-webkit-scrollbar\)\s*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/g, '')
      .replace(/@supports selector\(::-webkit-scrollbar\)\s*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/g, '')
    expect(globalCss).toContain('@supports not selector(::-webkit-scrollbar)')
    expect(outside).not.toMatch(/scrollbar-width:\s*thin/)
    expect(outside).not.toMatch(/scrollbar-color:/)
  })

  it('pins the board root to initial scrollbar values when the webkit pseudo exists', () => {
    const block = globalCss.match(/@supports selector\(::-webkit-scrollbar\)\s*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/)
    expect(block?.[0]).toMatch(/\.board-workbench\s*\{[^}]*scrollbar-width:\s*initial/)
    expect(block?.[0]).toMatch(/scrollbar-color:\s*initial/)
    expect(block?.[0]).not.toMatch(/scrollbar-width:\s*thin/)
    expect(block?.[0]).not.toMatch(/\.run-board-column-body/)
    expect(block?.[0]).not.toMatch(/\.settings-main-scroll/)
  })

  it('hides WebKit scrollbar arrow buttons and keeps the thin 4px bar', () => {
    expect(globalCss).toMatch(/::-webkit-scrollbar-button\s*\{[^}]*display:\s*none/)
    expect(globalCss).toMatch(/::-webkit-scrollbar\s*\{[^}]*width:\s*4px/)
  })
})

describe('scrollbar exceptions stay put', () => {
  const read = (rel: string) => readFileSync(join(dirname(fileURLToPath(import.meta.url)), rel), 'utf8')

  it('keeps the settings page bar at 8px', () => {
    const shell = read('../components/shell/AppShell.vue')
    expect(shell).toMatch(/\.settings-main-scroll::-webkit-scrollbar\s*\{\s*width:\s*8px/)
    expect(shell).toMatch(/scrollbar-color:\s*rgb\(var\(--c-txt3\)\s*\/\s*\.65\)\s*transparent/)
  })

  it('keeps the workflow rail and conversation rail scrollbars hidden', () => {
    const home = read('../views/DashboardView.vue')
    expect(home).toMatch(/\.home-workflow-rail\s*\{[^}]*scrollbar-width:\s*none/)
    const chat = read('../components/chat/ChatMessageRail.vue')
    expect(chat).toMatch(/\.chat-message-rail-track\s*\{[^}]*scrollbar-width:\s*none/)
  })

  it('keeps the Monaco scrollbar size', () => {
    const editor = read('../components/ui/CodeEditor.vue')
    expect(editor).toMatch(/verticalScrollbarSize:\s*10/)
    expect(editor).toMatch(/horizontalScrollbarSize:\s*10/)
  })
})

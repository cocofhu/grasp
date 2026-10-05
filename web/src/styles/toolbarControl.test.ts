// @vitest-environment node
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('toolbar-control shared sizing (g2.1 g2.2 g4.1 g4.2 g4.4)', () => {
  const css = readFileSync(resolve(__dirname, 'global.css'), 'utf8')
  const runList = readFileSync(resolve(__dirname, '../views/RunListView.vue'), 'utf8')
  const artifacts = readFileSync(resolve(__dirname, '../views/ArtifactsView.vue'), 'utf8')

  it('defines a single size+radius rule without theme colors', () => {
    expect(css).toMatch(/\.toolbar-control\s*\{[^}]*@apply[^}]*min-h-\[44px\]/)
    expect(css).toMatch(/\.toolbar-control\s*\{[^}]*md:min-h-\[34px\]/)
    expect(css).toMatch(/\.toolbar-control\s*\{[^}]*rounded-md/)
    const block = css.match(/\.toolbar-control\s*\{[^}]+\}/)?.[0] ?? ''
    expect(block).not.toMatch(/border-line|bg-surface|text-txt|hover:/)
  })

  it('caps count chips so they cannot grow the control (g3.4 g4.1)', () => {
    const block = css.match(/\.toolbar-control\s+\.chip\s*\{[^}]+\}/)?.[0] ?? ''
    expect(block).toMatch(/h-5/)
    expect(block).toMatch(/py-0\b/)
    expect(block).toMatch(/leading-none/)
    expect(block).toMatch(/shrink-0/)
  })

  it('RunListView toolbar still hosts the four shared filters without local size classes (g4.1)', () => {
    expect(runList).toContain('<TagFilter')
    expect(runList).toContain('<ProjectFilter')
    expect(runList).toContain('<StatusFilter')
    expect(runList).toContain('<WorkflowFilter')
    expect(runList).toContain('flex w-full flex-col gap-2 md:w-auto md:flex-row md:items-center')
  })

  it('ArtifactsView toolbar still hosts ProjectFilter without local size overrides (g4.2)', () => {
    expect(artifacts).toContain('<ProjectFilter')
    expect(artifacts).toContain('md:flex-row md:items-start md:justify-between')
  })
})

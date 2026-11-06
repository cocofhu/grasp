// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

describe('mobile form zoom contract', () => {
  const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'global.css'), 'utf8')

  it('keeps text metrics stable and raises editable mobile controls to 16px', () => {
    expect(css).toMatch(/-webkit-text-size-adjust:\s*100%/)
    expect(css).toMatch(/text-size-adjust:\s*100%/)
    expect(css).toMatch(/@media\s*\(max-width:\s*767px\)[\s\S]*?font-size:\s*max\(16px,\s*1em\)\s*!important/)
  })

  it('does not disable user zoom through the viewport contract', () => {
    const index = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../index.html'), 'utf8')
    expect(index).not.toMatch(/user-scalable\s*=\s*no/i)
    expect(index).not.toMatch(/maximum-scale\s*=\s*1(?:[.\"]|\s|,)/i)
  })
})

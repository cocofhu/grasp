// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'HomeModeSwitch.vue'), 'utf8')

describe('HomeModeSwitch hover', () => {
  it('deepens idle options one step and leaves the selected accent alone', () => {
    expect(src).toContain('.home-mode__opt:hover:not(.home-mode__opt--on)')
    expect(src).toMatch(/\.home-mode__opt:hover:not\(\.home-mode__opt--on\)\s*\{[^}]*--c-txt2/)
    expect(src).not.toMatch(/\.home-mode__opt:hover[^{]*\{[^}]*--c-txt\)/)
    expect(src).toMatch(/\.home-mode__opt--on\s*\{[^}]*--c-accent-2/)
  })
})

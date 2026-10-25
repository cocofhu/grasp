import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(join(here, 'StageWorkflow.vue'), 'utf8')
const style = source.slice(source.indexOf('<style'))

function block(selector: string): string {
  const found = style.match(new RegExp(`${selector}\\s*\\{[^}]*\\}`))
  expect(found, selector).not.toBeNull()
  return found![0]
}

describe('StageWorkflow row geometry', () => {
  it('centers a single-line row with symmetric padding and keeps the optical offsets', () => {
    const node = block('\\.stage-workflow-node')
    expect(node).toContain('min-height: 46px')
    expect(node).toContain('align-items: flex-start')
    expect(node).not.toContain('align-items: center')
    expect(node).toContain('padding: 12px 10px')
    expect(node).not.toContain('padding: 8px 10px')

    const label = block('\\.stage-workflow-label')
    expect(label).toContain('padding-top: 1px')

    const status = block('\\.stage-workflow-status')
    expect(status).toContain('padding-top: 3px')
    expect(status).toContain('line-height: 16px')
  })

  it('shifts only the connector ends so the line still meets the lowered marks', () => {
    const line = block('\\.stage-workflow-step:not\\(:last-child\\)::after')
    expect(line).toContain('left: 20px')
    expect(line).toContain('top: 33px')
    expect(line).toContain('bottom: -13px')
    expect(line).toContain('z-index: 0')
    expect(line).toContain('width: 2px')
    expect(line).toContain('background: var(--wf-line)')
    expect(line).not.toContain('top: 29px')
    expect(line).not.toContain('bottom: -9px')

    const done = block('\\.stage-workflow-step\\.is-done:not\\(:last-child\\)::after')
    expect(done).toContain('background: color-mix(in srgb, var(--wf-ok) 72%, transparent)')
  })

  it('keeps the current-row fill, transparent hover, and entrance animation', () => {
    expect(style).toMatch(/\.stage-workflow-node\.is-current\s*\{\s*background:\s*color-mix\(in srgb, var\(--wf-ink\) 6%, transparent\);/)
    expect(style).toMatch(/\.stage-workflow-node:hover\s*\{\s*background:\s*transparent;/)
    expect(style).toMatch(/\.stage-workflow-node\.is-current:hover\s*\{\s*background:\s*color-mix\(in srgb, var\(--wf-ink\) 6%, transparent\);/)
    expect(style).toContain('animation: stage-workflow-step-in 220ms cubic-bezier(.22, 1, .36, 1) both')
    expect(style).toContain('@keyframes stage-workflow-step-in')
  })
})

import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const vue = readFileSync(join(dir, 'AppSidebarNav.vue'), 'utf8')
const style = vue.slice(vue.indexOf('<style scoped>'))
const motion = readFileSync(join(dir, '../../styles/icon-motion.css'), 'utf8')
const breath = JSON.parse(readFileSync(join(dir, '../../lib/icons/definitions.json'), 'utf8'))['breath-dot'] as {
  duration: number
  body: string
}

function rule(selector: string): string {
  const found = style.match(new RegExp(`${selector}\\s*\\{[^}]*\\}`, 's'))
  expect(found, selector).not.toBeNull()
  return found![0]
}

describe('sidebar breath ripple', () => {
  it('overrides only the nav-row busy breath loop to a 1.6s single ripple', () => {
    const core = rule('\\.nav-row-type-icon\\[data-icon="breath-dot"\\]\\[data-icon-motion="busy"\\] :deep\\(\\.grasp-heart-core\\)')
    const ring = rule('\\.nav-row-type-icon\\[data-icon="breath-dot"\\]\\[data-icon-motion="busy"\\] :deep\\(\\.grasp-heart-ring\\)')
    expect(core).toContain('animation: navRowBreathCore 1.6s ease-in-out infinite')
    expect(ring).toContain('animation: navRowBreathRing 1.6s ease-out infinite')
    expect(ring).toContain('vector-effect: non-scaling-stroke')

    const coreFrames = style.match(/@keyframes navRowBreathCore \{[\s\S]*?\n\}/)?.[0] ?? ''
    expect(coreFrames).toContain('7% { transform: scale(0.86); }')
    expect(coreFrames).toContain('0%, 16%, 100% { transform: scale(1); }')

    const ringFrames = style.match(/@keyframes navRowBreathRing \{[\s\S]*?\n\}/)?.[0] ?? ''
    expect(ringFrames).toContain('0% { transform: scale(0.75); opacity: 0; }')
    expect(ringFrames).toContain('12% { opacity: 0.62; }')
    expect(ringFrames).toContain('46%, 100% { transform: scale(3.2); opacity: 0; }')
    expect(style).not.toMatch(/\.nav-row-type-icon\[data-icon="arc-spinner"\]/)
    expect(style).not.toMatch(/\.nav-row-type-icon\[data-icon="user-hold"\]/)
  })

  it('plays one extra ring only on breath-dot hover or focus-visible', () => {
    const ping = rule('\\.nav-row-type-icon\\[data-icon="breath-dot"\\] :deep\\(\\.nav-breath-ping\\)')
    expect(ping).toContain('opacity: 0')
    expect(ping).toContain('vector-effect: non-scaling-stroke')

    const shot = rule('\\.nav-item:hover > \\.nav-row-type-icon\\[data-icon="breath-dot"\\] :deep\\(\\.nav-breath-ping\\),\\s*\\.nav-item:focus-visible > \\.nav-row-type-icon\\[data-icon="breath-dot"\\] :deep\\(\\.nav-breath-ping\\)')
    expect(shot).toContain('animation: navRowBreathPing 700ms ease-out 1')
    expect(shot).not.toContain('infinite')

    const hot = style.match(/@keyframes navRowBreathPing \{[\s\S]*?\n\}/)?.[0] ?? ''
    expect(hot).toContain('20% { opacity: 0.95; }')
    expect(hot).toContain('100% { transform: scale(3.2); opacity: 0; }')

    expect(vue.match(/:breath-ping="mark\.name === 'breath-dot'"/g)).toHaveLength(2)
    expect(style).not.toMatch(/\[data-icon="arc-spinner"\][\s\S]{0,180}nav-breath-ping/)
    expect(style).not.toMatch(/\[data-icon="user-hold"\][\s\S]{0,180}nav-breath-ping/)

    expect(style).toMatch(
      /@media \(prefers-reduced-motion: reduce\) \{\s*\.nav-row-type-icon\[data-icon="breath-dot"\]\[data-icon-motion="busy"\] :deep\(\.grasp-heart-core\)[\s\S]*?:deep\(\.grasp-heart-ring\)[\s\S]*?:deep\(\.nav-breath-ping\)[\s\S]*?:hover > \.nav-row-type-icon\[data-icon="breath-dot"\][\s\S]*?:focus-visible > \.nav-row-type-icon\[data-icon="breath-dot"\][\s\S]*?animation: none !important;/,
    )
  })

  it('leaves the shared busy motion and breath geometry unchanged', () => {
    expect(motion).toContain('animation: iconBusyCore 2100ms ease-in-out infinite')
    expect(motion).toContain('animation: iconBusyRing 2100ms ease-out infinite')
    expect(motion).toContain('scale(2.1)')
    expect(motion).not.toContain('1.6s')
    expect(motion).not.toContain('navRowBreath')
    expect(breath.duration).toBe(1150)
    expect(breath.body).toContain('r="3.4"')
    expect(breath.body).not.toContain('nav-breath-ping')
  })
})

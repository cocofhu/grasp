import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { IDENTITY_COLORS, IDENTITY_ICONS, identityIcon, searchIdentityIcons } from './identityIcons'

const srcRoot = join(dirname(fileURLToPath(import.meta.url)), '../..')

const catalogIds = [
  'folder', 'dollar', 'book', 'graduation', 'pencil', 'pen', 'code', 'play',
  'music', 'popcorn', 'palette', 'stethoscope', 'asterisk', 'lotus', 'briefcase',
  'chart', 'kettlebell', 'dumbbell', 'calendar', 'scales', 'compass', 'plane',
  'globe', 'wrench', 'paw', 'flask', 'brain', 'heart', 'plant',
]

describe('identity icon catalog', () => {
  it('matches the server catalog and has no emoji entries', () => {
    expect(IDENTITY_ICONS.map((icon) => icon.id)).toEqual(catalogIds)
    expect(new Set(catalogIds).size).toBe(catalogIds.length)
    expect(IDENTITY_COLORS.map((color) => color.id)).toEqual([
      'black', 'red', 'orange', 'yellow', 'green', 'blue', 'purple', 'pink',
    ])
    expect(JSON.stringify(IDENTITY_ICONS)).not.toMatch(/emoji/i)
    expect(IDENTITY_ICONS.every((icon) => icon.zh && icon.aliases.length && !icon.body.includes('<svg'))).toBe(true)
  })

  it('searches Chinese names and English aliases', () => {
    expect(searchIdentityIcons('烧瓶').map((icon) => icon.id)).toEqual(['flask'])
    expect(searchIdentityIcons('FLASK').map((icon) => icon.id)).toEqual(['flask'])
    expect(searchIdentityIcons('plane').map((icon) => icon.id)).toEqual(['plane'])
    expect(searchIdentityIcons('没有这枚')).toEqual([])
    expect(searchIdentityIcons('').length).toBe(IDENTITY_ICONS.length)
    expect(identityIcon('flask')?.zh).toBe('烧瓶')
    expect(identityIcon('not-real')).toBeUndefined()
  })

  it('leaves letter avatars on surfaces outside the Solo selector', () => {
    const untouched = [
      'components/solo/SoloSessionPane.vue',
      'components/canvas/inspector/AgentPicker.vue',
      'components/canvas/nodes/AgentNode.vue',
      'components/canvas/NodePalette.vue',
      'components/canvas/chrome/QuickAdd.vue',
      'components/canvas/NodeInspector.vue',
      'components/agent/AgentSidebar.vue',
      'components/agent/CreateAgentTeamWizard.vue',
    ]
    for (const rel of untouched) {
      const source = readFileSync(join(srcRoot, rel), 'utf8')
      expect(source, rel).not.toContain('identityIcons')
      expect(source, rel).not.toContain('AgentIconPicker')
      expect(source, rel).not.toContain('AgentIdentityGlyph')
    }
    expect(readFileSync(join(srcRoot, 'components/solo/SoloSessionPane.vue'), 'utf8')).toContain('agentInitial')
    expect(readFileSync(join(srcRoot, 'components/canvas/nodes/AgentNode.vue'), 'utf8')).toContain('agentInitial')
  })
})

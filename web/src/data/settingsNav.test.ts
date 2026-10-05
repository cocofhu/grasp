import { describe, expect, it } from 'vitest'
import {
  isSettingsChrome,
  settingsItemActive,
  settingsNavItems,
} from './settingsNav'
import { shellNavPaths } from './shellNavPaths'

describe('settingsNav (plan g2.1 / g2.2 / g2.3)', () => {
  it('lists subnav in the confirmed order', () => {
    expect(settingsNavItems.map((i) => i.labelKey)).toEqual([
      'nav.projects',
      'nav.notifications',
      'nav.stats',
      'nav.artifacts',
      'nav.agents',
      'nav.sandboxes',
      'nav.general',
      'nav.integrations',
    ])
    expect(settingsNavItems.map((i) => i.to)).toEqual([
      '/projects',
      '/notifications',
      '/stats',
      '/artifacts',
      '/agents',
      '/sandboxes',
      '/settings',
      '/settings',
    ])
  })

  it('treats settings-state routes as chrome and skips full pages (plan g3.1)', () => {
    expect(isSettingsChrome('/settings')).toBe(true)
    expect(isSettingsChrome('/settings/other')).toBe(true)
    expect(isSettingsChrome('/projects')).toBe(true)
    expect(isSettingsChrome('/projects/abc')).toBe(true)
    expect(isSettingsChrome('/notifications')).toBe(true)
    expect(isSettingsChrome('/agents')).toBe(true)
    expect(isSettingsChrome('/dashboard')).toBe(false)
    expect(isSettingsChrome('/gates')).toBe(false)
    expect(isSettingsChrome('/runs')).toBe(false)
    expect(isSettingsChrome('/runs/rid', true)).toBe(false)
    expect(isSettingsChrome('/sandboxes/sid/console', true)).toBe(false)
  })

  it('highlights prefix routes and exact general vs integrations (plan g2.2 / g2.3)', () => {
    const projects = settingsNavItems[0]
    const general = settingsNavItems.find((i) => i.labelKey === 'nav.general')!
    const integ = settingsNavItems.find((i) => i.labelKey === 'nav.integrations')!
    expect(settingsItemActive(projects, '/projects/p1', {})).toBe(true)
    expect(settingsItemActive(projects, '/runs', {})).toBe(false)
    expect(settingsItemActive(general, '/settings', {})).toBe(true)
    expect(settingsItemActive(general, '/settings/other', {})).toBe(false)
    expect(settingsItemActive(general, '/settings', { integrations: '1' })).toBe(false)
    expect(settingsItemActive(integ, '/settings', { integrations: '1' })).toBe(true)
    expect(settingsItemActive(integ, '/settings', {})).toBe(false)
  })

  it('shellNavPaths includes both workspace and settings destinations', () => {
    const paths = shellNavPaths()
    expect(paths).toContain('/dashboard')
    expect(paths).toContain('/settings')
    expect(paths).toContain('/projects')
    expect(paths).not.toContain('/settings/platform-rules')
  })
})

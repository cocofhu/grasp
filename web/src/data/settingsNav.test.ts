import { describe, expect, it } from 'vitest'
import { isSettingsChrome, settingsItemActive, settingsNavItems } from './settingsNav'
import { shellNavPaths } from './shellNavPaths'

describe('workspace settings navigation', () => {
  it('keeps every workspace capability reachable in three clear groups', () => {
    const paths = settingsNavItems.map((item) => item.to)
    expect(paths).toEqual(expect.arrayContaining(['/board', '/workflows', '/agents', '/settings/credentials', '/settings/audit', '/settings']))
    expect(settingsNavItems.map((item) => item.labelKey)).not.toContain('nav.workspaceInfo')
    expect(settingsNavItems.filter((item) => item.groupKey).map((item) => item.groupKey)).toEqual(['nav.groupWorkspaceTools', 'nav.groupExecution', 'nav.groupPlatform'])
    expect(new Set(paths.filter((path) => path !== '/settings')).size).toBe(paths.filter((path) => path !== '/settings').length)
    expect(paths).not.toContain('/stats')
    expect(paths).not.toContain('/assistant')
    for (const retired of ['/settings/notification-policy', '/settings/channels', '/settings/schedules']) expect(paths).not.toContain(retired)
    expect(settingsNavItems.map((item) => (item.query?.integrations ? `${item.to}?integrations=1` : item.to))).toEqual([
      '/board',
      '/runs',
      '/workflows',
      '/artifacts',
      '/notifications',
      '/settings/audit',
      '/agents',
      '/sandboxes',
      '/settings/projects',
      '/settings/credentials',
      '/settings',
      '/settings?integrations=1',
    ])
    const platformStart = settingsNavItems.findIndex((item) => item.groupKey === 'nav.groupPlatform')
    expect(settingsNavItems.slice(platformStart).map((item) => item.labelKey)).toEqual(['nav.general', 'nav.integrations'])
    expect(settingsNavItems.find((item) => item.to === '/sandboxes')?.groupKey).toBeUndefined()
  })
  it('lists run history under workspace tools right after the board', () => {
    const runs = settingsNavItems.find((item) => item.to === '/runs')!
    expect(runs).toMatchObject({ icon: 'runs', labelKey: 'nav.runs' })
    expect(runs.groupKey).toBeUndefined()
    expect(runs.exact).toBeUndefined()
    const index = settingsNavItems.indexOf(runs)
    expect(settingsNavItems[index - 1].to).toBe('/board')
    expect(settingsItemActive(runs, '/runs', {})).toBe(true)
    expect(settingsItemActive(runs, '/runs/abc', {})).toBe(true)
    expect(settingsItemActive(runs, '/runsx', {})).toBe(false)
    expect(settingsItemActive(runs, '/board', {})).toBe(false)
  })
  it('keeps operational tools and full pages in workspace chrome', () => {
    for (const path of ['/dashboard', '/board', '/workflows', '/runs', '/gates', '/stats']) expect(isSettingsChrome(path)).toBe(false)
    for (const path of ['/settings', '/settings/credentials', '/agents', '/artifacts', '/notifications']) expect(isSettingsChrome(path)).toBe(true)
    expect(isSettingsChrome('/sandboxes/1/console', true)).toBe(false)
  })
  it('separates exact general settings, integrations and messages', () => {
    const find = (key: string) => settingsNavItems.find((item) => item.labelKey === `nav.${key}`)!
    expect(settingsItemActive(find('general'), '/settings', {})).toBe(true)
    expect(settingsItemActive(find('general'), '/settings/credentials', {})).toBe(false)
    expect(settingsItemActive(find('general'), '/settings', { integrations: '1' })).toBe(false)
    expect(settingsItemActive(find('integrations'), '/settings', { integrations: '1' })).toBe(true)
    expect(settingsItemActive(find('notifications'), '/notifications', {})).toBe(true)
    expect(shellNavPaths()).toEqual(expect.arrayContaining(['/dashboard', '/workflows', '/settings/credentials', '/stats']))
  })
})

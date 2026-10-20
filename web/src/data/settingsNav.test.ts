import { describe, expect, it } from 'vitest'
import { isSettingsChrome, settingsItemActive, settingsNavItems } from './settingsNav'
import { shellNavPaths } from './shellNavPaths'

describe('workspace settings navigation', () => {
  it('keeps every former project capability reachable in three clear groups', () => {
    const paths = settingsNavItems.map((item) => item.to)
    expect(paths).toEqual(expect.arrayContaining(['/board', '/workflows', '/requirements', '/assistant', '/settings/notification-policy', '/settings/assistant', '/settings/external-mcp', '/settings/schedules', '/agents', '/settings/shared-agent', '/settings/credentials', '/settings/variables', '/settings/audit', '/settings/workspace']))
    expect(settingsNavItems.filter((item) => item.groupKey).map((item) => item.groupKey)).toEqual(['nav.groupWorkspaceTools', 'nav.groupExecution', 'nav.groupPlatform'])
    expect(paths.some((path) => path.startsWith('/projects'))).toBe(false)
    expect(new Set(paths.filter((path) => path !== '/settings')).size).toBe(paths.filter((path) => path !== '/settings').length)
  })
  it('keeps operational tools and full pages in workspace chrome', () => {
    for (const path of ['/dashboard', '/board', '/requirements', '/assistant', '/workflows', '/runs', '/gates']) expect(isSettingsChrome(path)).toBe(false)
    for (const path of ['/settings', '/settings/credentials', '/agents', '/artifacts', '/stats', '/notifications']) expect(isSettingsChrome(path)).toBe(true)
    expect(isSettingsChrome('/sandboxes/1/console', true)).toBe(false)
  })
  it('separates exact general settings, integrations, notification policy and messages', () => {
    const find = (key: string) => settingsNavItems.find((item) => item.labelKey === `nav.${key}`)!
    expect(settingsItemActive(find('general'), '/settings', {})).toBe(true)
    expect(settingsItemActive(find('general'), '/settings/credentials', {})).toBe(false)
    expect(settingsItemActive(find('general'), '/settings', { integrations: '1' })).toBe(false)
    expect(settingsItemActive(find('integrations'), '/settings', { integrations: '1' })).toBe(true)
    expect(settingsItemActive(find('notificationPolicy'), '/notifications', {})).toBe(false)
    expect(settingsItemActive(find('notifications'), '/notifications', {})).toBe(true)
    expect(shellNavPaths()).toEqual(expect.arrayContaining(['/dashboard', '/workflows', '/settings/credentials']))
  })
})

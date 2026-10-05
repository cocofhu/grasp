import type { SidebarNavItem } from './sidebarNav'

export type SettingsNavItem = SidebarNavItem & {
  exact?: boolean
  query?: Record<string, string>
  /** When set, a group label is rendered before this item. */
  groupKey?: string
}

/**
 * Settings-chrome subnav order (plan g1.2):
 * Projects, Notifications, Stats, Artifacts, Agents, Sandboxes, General, Integrations.
 * Global `/agents` Studio coexists with project detail `?tab=agents` (restored after #546).
 */
export const settingsNavItems: SettingsNavItem[] = [
  { to: '/projects', icon: 'folder', labelKey: 'nav.projects' },
  { to: '/notifications', icon: 'bell', labelKey: 'nav.notifications' },
  { to: '/stats', icon: 'chart', labelKey: 'nav.stats' },
  { to: '/artifacts', icon: 'artifact', labelKey: 'nav.artifacts' },
  { to: '/agents', icon: 'robot', labelKey: 'nav.agents' },
  { to: '/sandboxes', icon: 'terminal', labelKey: 'nav.sandboxes' },
  { to: '/settings', icon: 'settings', labelKey: 'nav.general', exact: true, groupKey: 'nav.groupPlatform' },
  {
    to: '/settings',
    icon: 'connector',
    labelKey: 'nav.integrations',
    exact: true,
    query: { integrations: '1' },
  },
]

export const SETTINGS_CHROME_PREFIXES = [
  '/projects',
  '/notifications',
  '/stats',
  '/artifacts',
  '/agents',
  '/sandboxes',
  '/settings',
] as const

/** Settings subnav on matching routes; full-page views keep workspace chrome (plan g3.1). */
export function isSettingsChrome(path: string, full?: boolean): boolean {
  if (full) return false
  return SETTINGS_CHROME_PREFIXES.some((p) => path === p || path.startsWith(`${p}/`))
}

export function isIntegrationsQuery(raw: unknown): boolean {
  if (Array.isArray(raw)) return raw.some((v) => isIntegrationsQuery(v))
  return raw === '1' || raw === 'true'
}

export function settingsItemActive(
  item: SettingsNavItem,
  path: string,
  query: Record<string, unknown> | { integrations?: unknown },
): boolean {
  const integrationsOn = isIntegrationsQuery((query as { integrations?: unknown }).integrations)
  if (item.query?.integrations) {
    return path === '/settings' && integrationsOn
  }
  if (item.exact) {
    if (item.to === '/settings') return path === '/settings' && !integrationsOn
    return path === item.to
  }
  return path === item.to || path.startsWith(`${item.to}/`)
}

export function settingsItemKey(item: SettingsNavItem, index: number): string {
  if (item.query?.integrations) return 'integrations'
  if (item.exact && item.to === '/settings') return 'general'
  return `${item.to}-${index}`
}

import type { SidebarNavItem } from './sidebarNav'

export type SettingsNavItem = SidebarNavItem & { exact?: boolean; query?: Record<string, string>; groupKey?: string }

export const settingsNavItems: SettingsNavItem[] = [
  { to: '/board', icon: 'board', labelKey: 'nav.board', groupKey: 'nav.groupWorkspaceTools' },
  { to: '/requirements', icon: 'doc', labelKey: 'nav.requirements' },
  { to: '/workflows', icon: 'workflow', labelKey: 'nav.workflows' },
  { to: '/assistant', icon: 'chat', labelKey: 'nav.assistant' },
  { to: '/artifacts', icon: 'artifact', labelKey: 'nav.artifacts' },
  { to: '/notifications', icon: 'bell', labelKey: 'nav.notifications' },
  { to: '/settings/schedules', icon: 'clock', labelKey: 'nav.schedules' },
  { to: '/settings/audit', icon: 'history', labelKey: 'nav.audit' },
  { to: '/agents', icon: 'robot', labelKey: 'nav.agents', groupKey: 'nav.groupExecution' },
  { to: '/sandboxes', icon: 'terminal', labelKey: 'nav.sandboxes' },
  { to: '/settings/projects', icon: 'git', labelKey: 'nav.projects' },
  { to: '/settings/credentials', icon: 'key', labelKey: 'nav.credentials' },
  { to: '/settings/variables', icon: 'variable', labelKey: 'nav.variables' },
  { to: '/settings/assistant', icon: 'chat', labelKey: 'nav.assistantSettings' },
  { to: '/settings/notification-policy', icon: 'bell-settings', labelKey: 'nav.notificationPolicy' },
  { to: '/settings/external-mcp', icon: 'connector', labelKey: 'nav.externalMcp' },
  { to: '/settings', icon: 'settings', labelKey: 'nav.general', exact: true, groupKey: 'nav.groupPlatform' },
  { to: '/settings', icon: 'integration', labelKey: 'nav.integrations', exact: true, query: { integrations: '1' } },
  { to: '/settings/workspace', icon: 'folder', labelKey: 'nav.workspaceInfo' },
]

export const SETTINGS_CHROME_PREFIXES = [
  '/notifications',
  '/artifacts',
  '/agents',
  '/sandboxes',
  '/settings',
] as const

/** Default chrome for direct visits; settings navigation also retains context on shared tools. */
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

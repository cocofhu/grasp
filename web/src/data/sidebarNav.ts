export type SidebarNavItem = { to: string; icon: string; labelKey: string }
export type SidebarNavGroup = { titleKey?: string; items: SidebarNavItem[] }

/** Workspace primary nav: Home / Workflows / Runs / Usage / Settings. Pending items live under the nav, not as a tab. */
export const sidebarNavGroups: SidebarNavGroup[] = [
  {
    items: [
      { to: '/dashboard', icon: 'dashboard', labelKey: 'nav.dashboard' },
      { to: '/workflows', icon: 'workflow', labelKey: 'nav.workflows' },
      { to: '/runs', icon: 'runs', labelKey: 'nav.runs' },
      { to: '/stats', icon: 'chart', labelKey: 'nav.stats' },
      { to: '/settings', icon: 'settings', labelKey: 'nav.settings' },
    ],
  },
]

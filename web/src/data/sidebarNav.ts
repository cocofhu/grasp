import { isNodeConversationPath } from '@/lib/run/nodeConversation'

export type SidebarNavItem = { to: string; icon: string; labelKey: string }
export type SidebarNavGroup = { titleKey?: string; items: SidebarNavItem[] }

/** Workspace primary nav: Home / Board / Workflows / Usage / Settings. Pending items live under the nav, not as a tab. */
export const sidebarNavGroups: SidebarNavGroup[] = [
  {
    items: [
      { to: '/dashboard', icon: 'dashboard', labelKey: 'nav.dashboard' },
      { to: '/board', icon: 'board', labelKey: 'nav.board' },
      { to: '/workflows', icon: 'workflow', labelKey: 'nav.workflows' },
      { to: '/stats', icon: 'chart', labelKey: 'nav.stats' },
      { to: '/settings', icon: 'settings', labelKey: 'nav.settings' },
    ],
  },
]

/** Kanban plus the run list/detail/live belong to the Board workspace section. */
export function isBoardSectionPath(path: string): boolean {
  if (isNodeConversationPath(path)) return false
  return path === '/board' || path === '/runs' || path.startsWith('/runs/')
}

/**
 * Solo and a run's node conversation start from Home.
 * `/runs/{runId}/nodes/{nodeId}` must not be treated as the board.
 */
export function isHomeSectionPath(path: string): boolean {
  return (
    path === '/dashboard' ||
    isNodeConversationPath(path) ||
    path.startsWith('/solo') ||
    path === '/requirements' ||
    path === '/assistant'
  )
}

import { isNodeConversationPath } from '@/lib/run/nodeConversation'

export type SidebarNavItem = { to: string; icon: string; labelKey: string }
export type SidebarNavGroup = { titleKey?: string; items: SidebarNavItem[] }

/** Solo is the main entry. Workflow definitions are managed in Settings. */
export const sidebarNavGroups: SidebarNavGroup[] = [
  {
    items: [
      { to: '/dashboard', icon: 'dashboard', labelKey: 'nav.dashboard' },
      { to: '/board', icon: 'board', labelKey: 'nav.board' },
      { to: '/runs', icon: 'runs', labelKey: 'nav.allRuns' },
      { to: '/stats', icon: 'chart', labelKey: 'nav.stats' },
      { to: '/settings', icon: 'settings', labelKey: 'nav.settings' },
    ],
  },
]

/** Only the kanban page. Run list, detail, and live belong to All runs. */
export function isBoardSectionPath(path: string): boolean {
  return navPathname(path) === '/board'
}

/**
 * Run list, detail, and live. Node conversations stay on Home.
 * Search and hash are ignored so a filtered run list stays in this section.
 */
export function isRunsSectionPath(path: string): boolean {
  const pathname = navPathname(path)
  if (isNodeConversationPath(pathname)) return false
  return pathname === '/runs' || pathname.startsWith('/runs/')
}

function navPathname(path: string): string {
  const cut = path.search(/[?#]/)
  return cut === -1 ? path : path.slice(0, cut)
}

/**
 * Solo and a run's node conversation start from Home.
 * `/runs/{runId}/nodes/{nodeId}` stays on Home, not All runs or the board.
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

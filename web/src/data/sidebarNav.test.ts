import { describe, expect, it } from 'vitest'
import { sidebarNavGroups } from './sidebarNav'

describe('sidebarNav (plan g1.1)', () => {
  it('workspace primary is exactly home / workflows / runs / usage / settings', () => {
    expect(sidebarNavGroups).toHaveLength(1)
    expect(sidebarNavGroups[0].titleKey).toBeUndefined()
    const items = sidebarNavGroups[0].items
    expect(items.map((i) => i.to)).toEqual([
      '/dashboard',
      '/workflows',
      '/runs',
      '/stats',
      '/settings',
    ])
    expect(items.find((i) => i.to === '/workflows')).toEqual({
      to: '/workflows',
      icon: 'workflow',
      labelKey: 'nav.workflows',
    })
    expect(items.find((i) => i.to === '/stats')).toEqual({
      to: '/stats',
      icon: 'chart',
      labelKey: 'nav.stats',
    })
  })

  it('does not expose collapsed pages or config group as workspace primary', () => {
    const allTos = sidebarNavGroups.flatMap((g) => g.items.map((i) => i.to))
    expect(allTos).not.toContain('/notifications')
    expect(allTos).not.toContain('/artifacts')
    expect(allTos).not.toContain('/agents')
    expect(allTos).not.toContain('/sandboxes')
    expect(allTos).not.toContain('/board')
    expect(allTos).not.toContain('/integrations')
    expect(allTos).not.toContain('/triggers')
    expect(sidebarNavGroups.some((g) => g.titleKey === 'nav.groupConfig')).toBe(false)
  })

  it('keeps the runs label key and does not put gates in the workspace nav', () => {
    const items = sidebarNavGroups[0].items
    expect(items.find((i) => i.to === '/runs')?.labelKey).toBe('nav.runs')
    expect(items.find((i) => i.to === '/gates')).toBeUndefined()
  })
})

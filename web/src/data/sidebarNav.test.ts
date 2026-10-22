import { describe, expect, it } from 'vitest'
import enNav from '@/locales/en/nav.json'
import zhNav from '@/locales/zh-CN/nav.json'
import { isBoardSectionPath, isHomeSectionPath, sidebarNavGroups } from './sidebarNav'

describe('sidebarNav (plan g1.1)', () => {
  it('workspace primary is exactly home / board / workflows / usage / settings', () => {
    expect(sidebarNavGroups).toHaveLength(1)
    expect(sidebarNavGroups[0].titleKey).toBeUndefined()
    const items = sidebarNavGroups[0].items
    expect(items.map((i) => i.to)).toEqual([
      '/dashboard',
      '/board',
      '/workflows',
      '/stats',
      '/settings',
    ])
    expect(items.find((i) => i.to === '/board')).toEqual({
      to: '/board',
      icon: 'board',
      labelKey: 'nav.board',
    })
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
    expect(zhNav.nav.board).toBe('看板')
    expect(enNav.nav.board).toBe('Board')
  })

  it('does not expose collapsed pages or config group as workspace primary', () => {
    const allTos = sidebarNavGroups.flatMap((g) => g.items.map((i) => i.to))
    expect(allTos).not.toContain('/notifications')
    expect(allTos).not.toContain('/artifacts')
    expect(allTos).not.toContain('/agents')
    expect(allTos).not.toContain('/sandboxes')
    expect(allTos).toContain('/board')
    expect(allTos).not.toContain('/runs')
    expect(allTos).not.toContain('/integrations')
    expect(allTos).not.toContain('/triggers')
    expect(sidebarNavGroups.some((g) => g.titleKey === 'nav.groupConfig')).toBe(false)
  })

  it('keeps the board label key and does not put gates or runs in the workspace nav', () => {
    const items = sidebarNavGroups[0].items
    expect(items.find((i) => i.to === '/board')?.labelKey).toBe('nav.board')
    expect(items.find((i) => i.to === '/runs')).toBeUndefined()
    expect(items.find((i) => i.to === '/gates')).toBeUndefined()
  })

  it('treats the kanban and run list/detail as the board section', () => {
    expect(isBoardSectionPath('/board')).toBe(true)
    expect(isBoardSectionPath('/runs')).toBe(true)
    expect(isBoardSectionPath('/runs/run-1')).toBe(true)
    expect(isBoardSectionPath('/runs/run-1/live')).toBe(true)
    expect(isBoardSectionPath('/dashboard')).toBe(false)
    expect(isBoardSectionPath('/workflows')).toBe(false)
    expect(isBoardSectionPath('/gates')).toBe(false)
  })

  it('treats Solo chat as the home section', () => {
    expect(isHomeSectionPath('/dashboard')).toBe(true)
    expect(isHomeSectionPath('/solo')).toBe(true)
    expect(isHomeSectionPath('/solo/abc')).toBe(true)
    expect(isHomeSectionPath('/gates/run-1')).toBe(true)
    expect(isHomeSectionPath('/board')).toBe(false)
  })
})

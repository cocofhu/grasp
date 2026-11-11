import { describe, expect, it } from 'vitest'
import enNav from '@/locales/en/nav.json'
import zhNav from '@/locales/zh-CN/nav.json'
import { isBoardSectionPath, isHomeSectionPath, isRunsSectionPath, sidebarNavGroups } from './sidebarNav'

describe('sidebarNav (plan g1.1)', () => {
  it('workspace primary is Solo home / board / all runs / usage / settings', () => {
    expect(sidebarNavGroups).toHaveLength(1)
    expect(sidebarNavGroups[0].titleKey).toBeUndefined()
    const items = sidebarNavGroups[0].items
    expect(items.map((i) => i.to)).toEqual([
      '/dashboard',
      '/board',
      '/runs',
      '/stats',
      '/settings',
    ])
    expect(items.find((i) => i.to === '/board')).toEqual({
      to: '/board',
      icon: 'board',
      labelKey: 'nav.board',
    })
    expect(items.find((i) => i.to === '/runs')).toEqual({
      to: '/runs',
      icon: 'runs',
      labelKey: 'nav.allRuns',
    })
    expect(items.find((i) => i.to === '/workflows')).toBeUndefined()
    expect(items.find((i) => i.to === '/stats')).toEqual({
      to: '/stats',
      icon: 'chart',
      labelKey: 'nav.stats',
    })
    expect(zhNav.nav.board).toBe('看板')
    expect(enNav.nav.board).toBe('Board')
    expect(zhNav.nav.allRuns).toBe('全部运行')
    expect(enNav.nav.allRuns).toBe('All runs')
    expect(items.find((i) => i.to === '/runs')?.labelKey).not.toBe('nav.runs')
    expect(items.find((i) => i.to === '/runs')?.labelKey).not.toBe('nav.viewAllRuns')
  })

  it('does not expose collapsed pages or config group as workspace primary', () => {
    const allTos = sidebarNavGroups.flatMap((g) => g.items.map((i) => i.to))
    expect(allTos).not.toContain('/notifications')
    expect(allTos).not.toContain('/artifacts')
    expect(allTos).not.toContain('/agents')
    expect(allTos).not.toContain('/sandboxes')
    expect(allTos).toContain('/board')
    expect(allTos).toContain('/runs')
    expect(allTos).not.toContain('/integrations')
    expect(allTos).not.toContain('/triggers')
    expect(sidebarNavGroups.some((g) => g.titleKey === 'nav.groupConfig')).toBe(false)
  })

  it('keeps the board label key and does not put gates in the workspace nav', () => {
    const items = sidebarNavGroups[0].items
    expect(items.find((i) => i.to === '/board')?.labelKey).toBe('nav.board')
    expect(items.find((i) => i.to === '/runs')?.labelKey).toBe('nav.allRuns')
    expect(items.find((i) => i.to === '/gates')).toBeUndefined()
  })

  it('treats only the kanban as the board section', () => {
    expect(isBoardSectionPath('/board')).toBe(true)
    expect(isBoardSectionPath('/board?lane=running')).toBe(true)
    expect(isBoardSectionPath('/runs')).toBe(false)
    expect(isBoardSectionPath('/runs/run-1')).toBe(false)
    expect(isBoardSectionPath('/runs/run-1/live')).toBe(false)
    expect(isBoardSectionPath('/runs/run-1/nodes/clarify')).toBe(false)
    expect(isBoardSectionPath('/dashboard')).toBe(false)
    expect(isBoardSectionPath('/workflows')).toBe(false)
    expect(isBoardSectionPath('/gates')).toBe(false)
  })

  it('treats the run list, detail, and live as all runs, not a node conversation', () => {
    expect(isRunsSectionPath('/runs')).toBe(true)
    expect(isRunsSectionPath('/runs?status=failed')).toBe(true)
    expect(isRunsSectionPath('/runs/run-1')).toBe(true)
    expect(isRunsSectionPath('/runs/run-1?view=canvas')).toBe(true)
    expect(isRunsSectionPath('/runs/run-1/live')).toBe(true)
    expect(isRunsSectionPath('/runs/run-1/live?node=a')).toBe(true)
    expect(isRunsSectionPath('/runs/run-1/nodes/clarify')).toBe(false)
    expect(isRunsSectionPath('/runs/run-1/nodes/clarify?share=1')).toBe(false)
    expect(isRunsSectionPath('/board')).toBe(false)
    expect(isRunsSectionPath('/dashboard')).toBe(false)
    expect(isRunsSectionPath('/solo/abc')).toBe(false)
  })

  it('treats Solo chat and node conversations as the home section', () => {
    expect(isHomeSectionPath('/dashboard')).toBe(true)
    expect(isHomeSectionPath('/solo')).toBe(true)
    expect(isHomeSectionPath('/solo/abc')).toBe(true)
    expect(isHomeSectionPath('/runs/run-1/nodes/clarify')).toBe(true)
    expect(isHomeSectionPath('/runs/run-1/nodes/clarify/')).toBe(true)
    expect(isHomeSectionPath('/runs/run-1')).toBe(false)
    expect(isHomeSectionPath('/runs/run-1/live')).toBe(false)
    expect(isHomeSectionPath('/gates')).toBe(false)
    expect(isHomeSectionPath('/board')).toBe(false)
  })
})

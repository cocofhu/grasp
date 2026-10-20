import { describe, expect, it } from 'vitest'
import type { RequirementDraft } from '@/lib/shared/types'
import {
  barStyle,
  buildGanttRows,
  computeTimelineWindow,
  draftBarRange,
  normalizeDraft,
  sortMilestones,
  withContextualParents,
} from './requirementDraftSchedule'

function d(partial: Partial<RequirementDraft> & { id: string }): RequirementDraft {
  return normalizeDraft({
    projectId: 'p',
    title: partial.title || partial.id,
    bodyMarkdown: '',
    status: 'open',
    kind: 'requirement',
    startAt: '',
    dueAt: '',
    progress: 0,
    parentId: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...partial,
  })
}

describe('requirementDraftSchedule', () => {
  it('normalizes empty kind and single-day bar range', () => {
    const row = normalizeDraft({
      id: 'rd-1',
      projectId: 'p',
      title: 'x',
      bodyMarkdown: '',
      status: 'open',
      kind: '' as any,
      startAt: '2026-08-10',
      dueAt: '',
      progress: 0,
      parentId: null,
      createdAt: '',
      updatedAt: '',
    })
    expect(row.kind).toBe('requirement')
    expect(draftBarRange(row)).toEqual({ start: '2026-08-10', end: '2026-08-10' })
  })

  it('puts unscheduled tops in unscheduled zone; group parents stay out', () => {
    const parent = d({ id: 'p1', title: '模块' })
    const child = d({ id: 'c1', title: '子', parentId: 'p1', startAt: '2026-08-01', dueAt: '2026-08-05' })
    const lone = d({ id: 'u1', title: '未排期' })
    const { unscheduled, scheduled } = buildGanttRows([parent, child, lone])
    expect(unscheduled.map((r) => r.draft.id)).toEqual(['u1'])
    expect(scheduled.some((r) => r.draft.id === 'p1' && r.rowKind === 'group')).toBe(true)
    expect(scheduled.some((r) => r.draft.id === 'c1' && r.indent)).toBe(true)
  })

  it('injects contextual parents when search only hits children', () => {
    const parent = d({ id: 'p1', title: '账户体系' })
    const child = d({ id: 'c1', title: '登录', parentId: 'p1' })
    const rows = withContextualParents([child], new Map([['p1', parent], ['c1', child]]))
    expect(rows[0]).toMatchObject({ draft: { id: 'p1' }, contextual: true })
    expect(rows[1]).toMatchObject({ draft: { id: 'c1' }, contextual: false })
  })

  it('sorts milestones by dueAt ascending', () => {
    const a = d({ id: 'm1', kind: 'milestone', dueAt: '2026-08-20', title: 'B' })
    const b = d({ id: 'm2', kind: 'milestone', dueAt: '2026-08-10', title: 'A' })
    const req = d({ id: 'r1' })
    expect(sortMilestones([a, b, req]).map((x) => x.id)).toEqual(['m2', 'm1'])
  })

  it('computes timeline window including today when empty', () => {
    const win = computeTimelineWindow([], 'week', '2026-08-12')
    expect(win.ticks.length).toBeGreaterThan(0)
    expect(win.start <= '2026-08-12').toBe(true)
    const style = barStyle('2026-08-12', '2026-08-14', win)
    expect(style.left).toMatch(/%$/)
    expect(style.width).toMatch(/%$/)
  })
})

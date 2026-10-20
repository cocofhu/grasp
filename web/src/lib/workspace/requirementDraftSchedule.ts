import type { RequirementDraft, RequirementDraftKind } from '@/lib/shared/types'

export type GanttScale = 'day' | 'week' | 'month'

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

export function normalizeDraft(d: RequirementDraft): RequirementDraft {
  return {
    ...d,
    kind: (d.kind === 'milestone' ? 'milestone' : 'requirement') as RequirementDraftKind,
    startAt: d.startAt || '',
    dueAt: d.dueAt || '',
    progress: clampProgress(d.progress ?? 0),
    parentId: d.parentId || null,
  }
}

export function clampProgress(n: number): number {
  if (!Number.isFinite(n)) return 0
  return Math.max(0, Math.min(100, Math.round(n)))
}

export function isValidDateOnly(v: string): boolean {
  if (!v) return true
  if (!DATE_RE.test(v)) return false
  const [y, m, d] = v.split('-').map(Number)
  const dt = new Date(y, m - 1, d)
  return dt.getFullYear() === y && dt.getMonth() === m - 1 && dt.getDate() === d
}

export function todayLocalISO(): string {
  const n = new Date()
  return toISODate(n)
}

export function toISODate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

export function parseISODate(v: string): Date | null {
  if (!isValidDateOnly(v) || !v) return null
  const [y, m, d] = v.split('-').map(Number)
  return new Date(y, m - 1, d)
}

export function addDays(iso: string, n: number): string {
  const d = parseISODate(iso)
  if (!d) return iso
  d.setDate(d.getDate() + n)
  return toISODate(d)
}

/** Effective bar range: single-ended dates collapse to that day. */
export function draftBarRange(d: RequirementDraft): { start: string; end: string } | null {
  if (d.kind === 'milestone') {
    if (!d.dueAt) return null
    return { start: d.dueAt, end: d.dueAt }
  }
  const s = d.startAt || ''
  const e = d.dueAt || ''
  if (!s && !e) return null
  if (s && !e) return { start: s, end: s }
  if (!s && e) return { start: e, end: e }
  return { start: s, end: e }
}

export function hasOwnSchedule(d: RequirementDraft): boolean {
  if (d.kind === 'milestone') return Boolean(d.dueAt)
  return Boolean(d.startAt || d.dueAt)
}

/**
 * When search only hits children, inject parent rows for gantt/list context.
 * Parents that did not match the query are marked contextual.
 */
export function withContextualParents(
  matched: RequirementDraft[],
  allById: Map<string, RequirementDraft>,
): { draft: RequirementDraft; contextual: boolean }[] {
  const matchedIds = new Set(matched.map((d) => d.id))
  const out: { draft: RequirementDraft; contextual: boolean }[] = []
  const seen = new Set<string>()
  for (const d of matched) {
    if (d.parentId && !matchedIds.has(d.parentId) && !seen.has(d.parentId)) {
      const parent = allById.get(d.parentId)
      if (parent) {
        out.push({ draft: parent, contextual: true })
        seen.add(parent.id)
      }
    }
    if (!seen.has(d.id)) {
      out.push({ draft: d, contextual: false })
      seen.add(d.id)
    }
  }
  return out
}

export type GanttRowKind = 'unscheduled' | 'group' | 'bar' | 'milestone'

export interface GanttRow {
  draft: RequirementDraft
  contextual: boolean
  rowKind: GanttRowKind
  indent: boolean
}

/** Build gantt rows: unscheduled tops first, then parents with children indented. */
export function buildGanttRows(
  items: RequirementDraft[],
  opts?: { contextualIds?: Set<string> },
): { unscheduled: GanttRow[]; scheduled: GanttRow[] } {
  const contextualIds = opts?.contextualIds || new Set<string>()
  const byId = new Map(items.map((d) => [d.id, d]))
  const childrenOf = new Map<string, RequirementDraft[]>()
  const tops: RequirementDraft[] = []

  for (const d of items) {
    if (d.parentId && byId.has(d.parentId)) {
      const list = childrenOf.get(d.parentId) || []
      list.push(d)
      childrenOf.set(d.parentId, list)
    } else {
      tops.push(d)
    }
  }

  const unscheduled: GanttRow[] = []
  const scheduled: GanttRow[] = []

  function pushRow(d: RequirementDraft, indent: boolean) {
    const contextual = contextualIds.has(d.id)
    const kids = childrenOf.get(d.id) || []
    const own = hasOwnSchedule(d)
    const hasScheduledChild = kids.some((k) => hasOwnSchedule(k) || (childrenOf.get(k.id) || []).length > 0)

    if (!own && !indent) {
      // Top-level: unscheduled zone only if no scheduled children either
      if (!hasScheduledChild) {
        unscheduled.push({ draft: d, contextual, rowKind: 'unscheduled', indent: false })
        return
      }
      // Group header (no bar)
      scheduled.push({ draft: d, contextual, rowKind: 'group', indent: false })
    } else if (!own && indent) {
      // Child without dates — still show under parent in scheduled section as name-only
      scheduled.push({
        draft: d,
        contextual,
        rowKind: d.kind === 'milestone' ? 'milestone' : 'group',
        indent,
      })
    } else {
      scheduled.push({
        draft: d,
        contextual,
        rowKind: d.kind === 'milestone' ? 'milestone' : 'bar',
        indent,
      })
    }

    for (const child of kids) {
      pushRow(child, true)
    }
  }

  // Unscheduled tops first (no scheduled children)
  for (const d of tops) {
    const kids = childrenOf.get(d.id) || []
    const own = hasOwnSchedule(d)
    const hasScheduledChild = kids.some((k) => hasOwnSchedule(k))
    if (!own && !hasScheduledChild) {
      unscheduled.push({
        draft: d,
        contextual: contextualIds.has(d.id),
        rowKind: 'unscheduled',
        indent: false,
      })
    }
  }

  for (const d of tops) {
    const kids = childrenOf.get(d.id) || []
    const own = hasOwnSchedule(d)
    const hasScheduledChild = kids.some((k) => hasOwnSchedule(k))
    if (!own && !hasScheduledChild) continue
    pushRow(d, false)
  }

  return { unscheduled, scheduled }
}

export interface TimelineWindow {
  start: string
  end: string
  ticks: string[]
  tickCount: number
}

export function computeTimelineWindow(
  drafts: RequirementDraft[],
  scale: GanttScale,
  today = todayLocalISO(),
): TimelineWindow {
  let min = today
  let max = today
  let any = false
  for (const d of drafts) {
    const r = draftBarRange(d)
    if (!r) continue
    any = true
    if (r.start < min) min = r.start
    if (r.end > max) max = r.end
  }
  if (!any) {
    min = today
    max = today
  }

  // Pad window
  if (scale === 'day') {
    min = addDays(min, -3)
    max = addDays(max, 10)
  } else if (scale === 'week') {
    min = addDays(min, -7)
    max = addDays(max, 21)
  } else {
    min = addDays(min, -14)
    max = addDays(max, 45)
  }

  const ticks: string[] = []
  let cur = min
  const step = scale === 'day' ? 1 : scale === 'week' ? 7 : 30
  // Cap ticks for performance
  for (let i = 0; i < 90; i++) {
    ticks.push(cur)
    const next = addDays(cur, step)
    if (next > max && i >= 6) break
    cur = next
    if (cur > max && ticks.length >= 7) break
  }
  if (ticks.length === 0) ticks.push(today)
  return { start: ticks[0], end: ticks[ticks.length - 1], ticks, tickCount: ticks.length }
}

export function positionPct(iso: string, window: TimelineWindow): number {
  const a = parseISODate(window.start)
  const b = parseISODate(window.end)
  const t = parseISODate(iso)
  if (!a || !b || !t) return 0
  const total = Math.max(1, b.getTime() - a.getTime())
  const span = scaleDaySpan(window)
  // Place at start of day; use inclusive end padding of one tick unit for bars
  const offset = t.getTime() - a.getTime()
  return Math.max(0, Math.min(100, (offset / (total + span)) * 100))
}

function scaleDaySpan(window: TimelineWindow): number {
  if (window.ticks.length < 2) return 86400000
  const a = parseISODate(window.ticks[0])
  const b = parseISODate(window.ticks[1])
  if (!a || !b) return 86400000
  return Math.max(86400000, b.getTime() - a.getTime())
}

export function barStyle(start: string, end: string, window: TimelineWindow): { left: string; width: string } {
  const left = positionPct(start, window)
  const right = positionPct(addDays(end, 1), window) // exclusive end of day
  const width = Math.max(1.2, right - left)
  return { left: `${left}%`, width: `${width}%` }
}

export function diamondStyle(due: string, window: TimelineWindow): { left: string } {
  const left = positionPct(due, window)
  return { left: `calc(${left}% - 6px)` }
}

export function tickLabel(iso: string, scale: GanttScale): string {
  const d = parseISODate(iso)
  if (!d) return iso
  const m = d.getMonth() + 1
  const day = d.getDate()
  if (scale === 'month') return `${m}月`
  if (scale === 'week') return `${m}/${day}`
  return `${day}`
}

export function parentCandidates(
  items: RequirementDraft[],
  selfId: string,
): RequirementDraft[] {
  const childParentIds = new Set(
    items.filter((d) => d.parentId).map((d) => d.parentId as string),
  )
  return items.filter((d) => {
    if (d.id === selfId) return false
    if (d.kind !== 'requirement') return false
    if (d.parentId) return false
    // self already has children → candidates still listed for others; caller blocks setting parent on self
    return true
  }).filter((d) => {
    // Prefer tops that aren't the self-as-parent case handled elsewhere
    void childParentIds
    return true
  })
}

export function draftHasChildren(items: RequirementDraft[], id: string): boolean {
  return items.some((d) => d.parentId === id)
}

export function sortMilestones(items: RequirementDraft[]): RequirementDraft[] {
  return items
    .filter((d) => d.kind === 'milestone')
    .slice()
    .sort((a, b) => {
      const da = a.dueAt || ''
      const db = b.dueAt || ''
      if (da !== db) return da < db ? -1 : 1
      return a.title.localeCompare(b.title)
    })
}

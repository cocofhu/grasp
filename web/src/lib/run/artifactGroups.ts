import type { Artifact } from '@/lib/shared/types'

export interface RunSection {
  runId: string
  runTitle?: string
  items: Artifact[]
  latestAt: number
}

function sortByCreatedAtDesc(artifacts: Artifact[]): Artifact[] {
  return [...artifacts].sort((x, y) => new Date(y.createdAt).getTime() - new Date(x.createdAt).getTime())
}

/** Short run id for section titles: run-abc123 → #abc123 (matches RunListView strip + #). */
export function runIdShort(id: string): string {
  return '#' + id.replace(/^run-/, '')
}

export function runSectionTitle(runTitle: string | undefined, runId: string): string {
  return runTitle?.trim() ? runTitle : runIdShort(runId)
}

export function groupByRun(artifacts: Artifact[]): RunSection[] {
  const list = Array.isArray(artifacts) ? artifacts : []
  const map = new Map<string, RunSection>()
  for (const a of list) {
    if (!map.has(a.runId)) {
      map.set(a.runId, { runId: a.runId, runTitle: a.runTitle, items: [], latestAt: 0 })
    }
    map.get(a.runId)!.items.push(a)
  }
  for (const sec of map.values()) {
    sec.items = sortByCreatedAtDesc(sec.items)
    sec.latestAt = Math.max(...sec.items.map((i) => new Date(i.createdAt).getTime()))
  }
  return [...map.values()].sort((a, b) => b.latestAt - a.latestAt)
}

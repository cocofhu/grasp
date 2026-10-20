import type { LocationQuery } from 'vue-router'
import type { ResourceListItem } from '@/components/ui/ResourceList.vue'
import type { ArtifactListScope } from '@/lib/api/clients/artifactsClient'
import type { ArtifactTree, ArtifactTreeWorkflow } from '@/lib/shared/types'

export const SESSION_CHILD_ID = '__session__'
export type ArtifactSelection = { kind: 'all' } | { kind: 'workflow'; workflowId: string } | { kind: 'session' }
export type ArtifactTreeLabels = { session: string; unnamedWorkflow: string; all?: string }
export function workflowLabel(wf: ArtifactTreeWorkflow, unnamed: string): string { return wf.workflowName.trim() || unnamed }
export function buildArtifactTreeNodes(tree: ArtifactTree | null, labels: ArtifactTreeLabels): ResourceListItem[] {
  if (!tree) return []
  const rows: ResourceListItem[] = [{ id: 'all', label: labels.all || labels.unnamedWorkflow, count: tree.count, icon: 'artifact' }]
  rows.push(...tree.workflows.map((wf) => ({ id: 'workflow:' + wf.workflowId, label: workflowLabel(wf, labels.unnamedWorkflow), count: wf.count, icon: 'workflow' })))
  if (tree.sessionCount) rows.push({ id: 'session', label: labels.session, count: tree.sessionCount, icon: 'robot' })
  return rows
}
function firstString(v: LocationQuery[string] | undefined): string { const s = Array.isArray(v) ? v[0] : v; return typeof s === 'string' ? s : '' }
export function selectionFromQuery(query: LocationQuery): ArtifactSelection {
  const workflowId = firstString(query.workflow)
  if (workflowId) return { kind: 'workflow', workflowId }
  return firstString(query.session) === '1' ? { kind: 'session' } : { kind: 'all' }
}
export function selectionToQuery(sel: ArtifactSelection): Record<string, string> {
  if (sel.kind === 'workflow') return { workflow: sel.workflowId }
  if (sel.kind === 'session') return { session: '1' }
  return {}
}
export function selectionTreeKey(sel: ArtifactSelection | null): string { return !sel ? '' : sel.kind === 'workflow' ? 'workflow:' + sel.workflowId : sel.kind }
export function selectionFromTreeKey(key: string): ArtifactSelection | null {
  if (key === 'all' || key === 'session') return { kind: key }
  return key.startsWith('workflow:') && key.slice(9) ? { kind: 'workflow', workflowId: key.slice(9) } : null
}
export function resolveArtifactSelection(tree: ArtifactTree | null, wanted: ArtifactSelection | null): ArtifactSelection | null {
  if (!tree?.count) return null
  if (wanted?.kind === 'workflow' && tree.workflows.some((w) => w.workflowId === wanted.workflowId)) return wanted
  if (wanted?.kind === 'session' && tree.sessionCount > 0) return wanted
  return { kind: 'all' }
}
export function selectionScope(sel: ArtifactSelection): ArtifactListScope {
  if (sel.kind === 'workflow') return { workflowId: sel.workflowId }
  if (sel.kind === 'session') return { session: true }
  return {}
}
export type SelectionInfo = { rootLabel: string; childLabel: string; count: number }
export function describeSelection(tree: ArtifactTree | null, sel: ArtifactSelection | null, labels: ArtifactTreeLabels): SelectionInfo | null {
  if (!tree || !sel) return null
  const base = { rootLabel: labels.all || labels.unnamedWorkflow }
  if (sel.kind === 'session') return { ...base, childLabel: labels.session, count: tree.sessionCount }
  if (sel.kind === 'workflow') {
    const wf = tree.workflows.find((w) => w.workflowId === sel.workflowId)
    if (wf) return { ...base, childLabel: workflowLabel(wf, labels.unnamedWorkflow), count: wf.count }
  }
  return { ...base, childLabel: '', count: tree.count }
}

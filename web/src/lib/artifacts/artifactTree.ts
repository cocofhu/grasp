import type { LocationQuery } from 'vue-router'
import {
  childKey,
  parseTreeKey,
  projectKey,
  type ProjectTreeNode,
} from '@/components/ui/projectTree'
import type { ArtifactListScope } from '@/lib/api/clients/artifactsClient'
import type { ArtifactTreeProject, ArtifactTreeWorkflow } from '@/lib/shared/types'

/** Tree child id of a project's workflow-less (Agent session) bucket. */
export const SESSION_CHILD_ID = '__session__'

export type ArtifactSelection =
  | { kind: 'project'; projectId: string }
  | { kind: 'workflow'; projectId: string; workflowId: string }
  | { kind: 'session'; projectId: string }

export type ArtifactTreeLabels = { session: string; unnamedWorkflow: string }

export function workflowLabel(wf: ArtifactTreeWorkflow, unnamed: string): string {
  return wf.workflowName.trim() || unnamed
}

export function projectLabel(p: ArtifactTreeProject): string {
  return p.projectName.trim() || p.projectId
}

export function buildArtifactTreeNodes(
  tree: ArtifactTreeProject[],
  labels: ArtifactTreeLabels,
): ProjectTreeNode[] {
  return tree.map((p) => {
    const children = p.workflows.map((wf) => ({
      id: wf.workflowId,
      label: workflowLabel(wf, labels.unnamedWorkflow),
      count: wf.count,
      icon: 'workflow',
    }))
    if (p.sessionCount > 0) {
      children.push({ id: SESSION_CHILD_ID, label: labels.session, count: p.sessionCount, icon: 'robot' })
    }
    return { id: p.projectId, label: projectLabel(p), count: p.count, children }
  })
}

function firstString(v: LocationQuery[string] | undefined): string {
  const s = Array.isArray(v) ? v[0] : v
  return typeof s === 'string' ? s : ''
}

/** Raw (unvalidated) selection requested by ?project=&workflow= / ?project=&session=1. */
export function selectionFromQuery(query: LocationQuery): ArtifactSelection | null {
  const projectId = firstString(query.project)
  if (!projectId) return null
  const workflowId = firstString(query.workflow)
  if (workflowId) return { kind: 'workflow', projectId, workflowId }
  if (firstString(query.session) === '1') return { kind: 'session', projectId }
  return { kind: 'project', projectId }
}

export function selectionToQuery(sel: ArtifactSelection): Record<string, string> {
  if (sel.kind === 'workflow') return { project: sel.projectId, workflow: sel.workflowId }
  if (sel.kind === 'session') return { project: sel.projectId, session: '1' }
  return { project: sel.projectId }
}

export function selectionTreeKey(sel: ArtifactSelection | null): string {
  if (!sel) return ''
  if (sel.kind === 'workflow') return childKey(sel.projectId, sel.workflowId)
  if (sel.kind === 'session') return childKey(sel.projectId, SESSION_CHILD_ID)
  return projectKey(sel.projectId)
}

export function selectionFromTreeKey(key: string): ArtifactSelection | null {
  const parsed = parseTreeKey(key)
  if (!parsed) return null
  const { projectId, childId } = parsed
  if (!childId) return { kind: 'project', projectId }
  if (childId === SESSION_CHILD_ID) return { kind: 'session', projectId }
  return { kind: 'workflow', projectId, workflowId: childId }
}

/**
 * Clamp a requested selection to what the tree holds: unknown children fall back
 * to their project, unknown projects (or none requested) to the first project.
 */
export function resolveArtifactSelection(
  tree: ArtifactTreeProject[],
  wanted: ArtifactSelection | null,
): ArtifactSelection | null {
  const project = (wanted && tree.find((p) => p.projectId === wanted.projectId)) || tree[0]
  if (!project) return null
  const projectId = project.projectId
  if (wanted?.projectId === projectId) {
    if (wanted.kind === 'workflow' && project.workflows.some((w) => w.workflowId === wanted.workflowId)) {
      return wanted
    }
    if (wanted.kind === 'session' && project.sessionCount > 0) return wanted
  }
  return { kind: 'project', projectId }
}

export function selectionScope(sel: ArtifactSelection): ArtifactListScope {
  if (sel.kind === 'workflow') return { projectId: sel.projectId, workflowId: sel.workflowId }
  if (sel.kind === 'session') return { projectId: sel.projectId, session: true }
  return { projectId: sel.projectId }
}

export type SelectionInfo = {
  project: ArtifactTreeProject
  projectLabel: string
  /** Second breadcrumb segment; empty when the whole project is selected. */
  childLabel: string
  count: number
}

export function describeSelection(
  tree: ArtifactTreeProject[],
  sel: ArtifactSelection | null,
  labels: ArtifactTreeLabels,
): SelectionInfo | null {
  if (!sel) return null
  const project = tree.find((p) => p.projectId === sel.projectId)
  if (!project) return null
  const base = { project, projectLabel: projectLabel(project) }
  if (sel.kind === 'session') return { ...base, childLabel: labels.session, count: project.sessionCount }
  if (sel.kind === 'workflow') {
    const wf = project.workflows.find((w) => w.workflowId === sel.workflowId)
    if (wf) return { ...base, childLabel: workflowLabel(wf, labels.unnamedWorkflow), count: wf.count }
  }
  return { ...base, childLabel: '', count: project.count }
}

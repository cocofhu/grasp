import type { WFEdge, WFNode } from '../shared/types'

/** Frozen graph snapshot used as the dirty baseline (nodes + edges). */
export type GraphBaseline = {
  nodes: WFNode[]
  edges: WFEdge[]
}

/** Frozen metadata snapshot (does not affect toolbar / run-before-save). */
export type MetaBaseline = {
  name: string
  description: string
  needsRepo: boolean
}

export type WorkflowDirtySource = {
  nodes: WFNode[]
  edges: WFEdge[]
  name: string
  description: string
  needsRepo: boolean
}

function cloneJSON<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

function stableStringify(v: unknown): string {
  return JSON.stringify(v)
}

/** Capture nodes/edges after hydrate as the graph baseline. */
export function snapshotGraph(wf: Pick<WorkflowDirtySource, 'nodes' | 'edges'>): GraphBaseline {
  return cloneJSON({
    nodes: wf.nodes ?? [],
    edges: wf.edges ?? [],
  })
}

export function snapshotMeta(
  wf: Pick<WorkflowDirtySource, 'name' | 'description' | 'needsRepo'>,
): MetaBaseline {
  return {
    name: wf.name ?? '',
    description: wf.description ?? '',
    needsRepo: !!wf.needsRepo,
  }
}

export function isGraphDirty(
  wf: Pick<WorkflowDirtySource, 'nodes' | 'edges'>,
  baseline: GraphBaseline | null,
): boolean {
  if (!baseline) return false
  return (
    stableStringify({ nodes: wf.nodes ?? [], edges: wf.edges ?? [] }) !==
    stableStringify({ nodes: baseline.nodes, edges: baseline.edges })
  )
}

export function isMetaDirty(
  wf: Pick<WorkflowDirtySource, 'name' | 'description' | 'needsRepo'>,
  baseline: MetaBaseline | null,
): boolean {
  if (!baseline) return false
  return (
    (wf.name ?? '') !== baseline.name ||
    (wf.description ?? '') !== baseline.description ||
    !!wf.needsRepo !== baseline.needsRepo
  )
}

/** saveDraft branch: noop → skip PUT; meta → PUT keep status; graph → PUT (may draft). */
export type SaveDraftBranch = 'noop' | 'meta' | 'graph'

export function saveDraftBranch(graphDirty: boolean, metaDirty: boolean): SaveDraftBranch {
  if (graphDirty) return 'graph'
  if (metaDirty) return 'meta'
  return 'noop'
}

/** beforeRunStart: new workflows always save; otherwise only when the graph is dirty. */
export function shouldSaveBeforeRun(hasId: boolean, graphDirty: boolean): boolean {
  return !hasId || graphDirty
}

/** Toolbar copy key: only graph dirty shows unsaved. */
export function toolbarSavedKey(graphDirty: boolean): 'common.saved.unsaved' | 'common.saved.saved' {
  return graphDirty ? 'common.saved.unsaved' : 'common.saved.saved'
}

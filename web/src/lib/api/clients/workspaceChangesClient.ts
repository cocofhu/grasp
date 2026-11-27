import { req } from '../httpCore'

export type WorkspaceChangesTarget = {
  kind: 'solo' | 'run'
  id: string
  nodeId?: string
  iteration?: number
  turnId?: string
}

export type WorkspaceChangedPath = {
  id: string
  path: string
  previousPath?: string
  status: string
  added: number | null
  deleted: number | null
  hasPatch: boolean
  hasBefore: boolean
  hasAfter: boolean
}

export type WorkspaceChangedRepository = {
  name: string
  directory: string
  branch: string
  baseBranch: string
  baseSha: string
  headSha: string
  unavailable?: boolean
  added: number
  deleted: number
  fileCount: number
  files: WorkspaceChangedPath[]
}

export type WorkspaceChangeSnapshot = {
  id: string
  workspace: string
  nodeId?: string
  turnId?: string
  interrupted?: boolean
  capturedAt: string
  repos: WorkspaceChangedRepository[]
}

export type WorkspaceChangesView = {
  status: 'ready' | 'unavailable' | 'pending' | 'no_repositories'
  canRefresh: boolean
  snapshot?: WorkspaceChangeSnapshot
  workspaces?: { id: string; nodeId?: string; capturedAt: string }[]
}

export type WorkspaceChangeFile = { patch?: string; before?: string; after?: string }

function base({ kind, id }: WorkspaceChangesTarget) {
  return (kind === 'solo' ? '/solo/sessions/' : '/runs/') + encodeURIComponent(id) + '/changes'
}

function suffix(target: WorkspaceChangesTarget, workspace?: string) {
  const params = new URLSearchParams()
  if (target.nodeId) params.set('nodeId', target.nodeId)
  if (target.iteration) params.set('iteration', String(target.iteration))
  if (target.turnId) {
    params.set('scope', 'turn')
    params.set('turnId', target.turnId)
  }
  if (workspace) params.set('workspace', workspace)
  return '?' + params.toString()
}

export const workspaceChangesClient = {
  list: (target: WorkspaceChangesTarget, workspace?: string, signal?: AbortSignal) =>
    req<WorkspaceChangesView>(base(target) + suffix(target, workspace), { signal }),
  refresh: (target: WorkspaceChangesTarget, workspace?: string, signal?: AbortSignal) =>
    req<WorkspaceChangesView>(base(target) + '/refresh' + suffix(target, workspace), { method: 'POST', signal }),
  file: (target: WorkspaceChangesTarget, snapshot: string, file: string, signal?: AbortSignal) =>
    req<WorkspaceChangeFile>(base(target) + '/' + encodeURIComponent(snapshot) + '/files/' + encodeURIComponent(file), { signal }),
}

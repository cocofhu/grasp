import type { Project, SessionRepo, SoloRepoChoice, SoloSession } from '@/lib/api/apiTypes'

type RepoSource = Pick<SoloSession, 'repos' | 'project' | 'projectId' | 'branch'> | null | undefined

/** Frozen repositories for display. Legacy sessions that only stored one project become a one-item list. */
export function sessionRepos(session: RepoSource): SessionRepo[] {
  if (!session) return []
  if (session.repos?.length) return session.repos
  const project = session.project
  if (!project?.repoUrl) return []
  return [
    {
      position: 0,
      projectId: session.projectId || project.id,
      name: project.name,
      repoUrl: project.repoUrl,
      repoDirName: project.repoDirName || '',
      branch: session.branch || project.defaultBranch || '',
    },
  ]
}

/** First repository name, or "name +N" when more than one is frozen. */
export function sessionRepoLabel(session: RepoSource): string {
  const repos = sessionRepos(session)
  if (!repos.length) return ''
  if (repos.length === 1) return repos[0]!.name
  return `${repos[0]!.name} +${repos.length - 1}`
}

/** Trigger text: none, one name, or the first name plus the count of the rest. */
export function repoTriggerLabel(ids: string[], nameOf: (id: string) => string, none: string): string {
  if (!ids.length) return none
  const first = nameOf(ids[0]!) || ids[0]!
  if (ids.length === 1) return first
  return `${first} +${ids.length - 1}`
}

/** Create payload. Empty branch overrides are omitted so each repository keeps its default. */
export function soloCreateChoices(ids: string[], branches: Record<string, string | undefined>): SoloRepoChoice[] {
  return ids
    .map((id) => id.trim())
    .filter(Boolean)
    .map((projectId) => {
      const branch = String(branches[projectId] || '').trim()
      return branch ? { projectId, branch } : { projectId }
    })
}

export function branchOfRepo(repo: Pick<SessionRepo, 'branch'>, remoteDefault: string): string {
  return repo.branch?.trim() || remoteDefault
}

export function selectedProjectNames(ids: string[], projects: Pick<Project, 'id' | 'name'>[]): string[] {
  return ids.map((id) => projects.find((project) => project.id === id)?.name || id)
}

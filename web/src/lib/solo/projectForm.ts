import type { Project, ProjectInput } from '@/lib/api/apiTypes'

export type ProjectDraft = {
  name: string
  description: string
  repoUrl: string
  defaultBranch: string
  repoDirName: string
}

export function emptyDraft(): ProjectDraft {
  return {
    name: '',
    description: '',
    repoUrl: '',
    defaultBranch: '',
    repoDirName: '',
  }
}

export function draftFromProject(p: Project): ProjectDraft {
  return {
    name: p.name,
    description: p.description ?? '',
    repoUrl: p.repoUrl,
    defaultBranch: p.defaultBranch ?? '',
    repoDirName: p.repoDirName ?? '',
  }
}

export function inputFromDraft(d: ProjectDraft): ProjectInput {
  return {
    name: d.name.trim(),
    description: d.description.trim(),
    repoUrl: d.repoUrl.trim(),
    defaultBranch: d.defaultBranch.trim(),
    repoDirName: d.repoDirName.trim(),
  }
}

/** Default directory name the server derives from the URL. */
export function repoNameFromUrl(url: string): string {
  const s = url.trim().replace(/\/+$/, '').replace(/\.git$/i, '')
  const tail = s.split(/[/:]/).pop() ?? ''
  return /^[A-Za-z0-9._-]+$/.test(tail) ? tail : ''
}

export function isRepoUrl(url: string): boolean {
  const s = url.trim()
  return /^(https?|ssh|git):\/\/\S+$/i.test(s) || /^[\w.-]+@[^:\s]+:\S+$/.test(s)
}

export function draftProblems(d: ProjectDraft): string[] {
  const out: string[] = []
  if (!d.name.trim()) out.push('name')
  if (!isRepoUrl(d.repoUrl)) out.push('repoUrl')
  if (/[\s|,]/.test(d.defaultBranch.trim())) out.push('branch')
  if (d.repoDirName.trim() && !/^[A-Za-z0-9._-]+$/.test(d.repoDirName.trim())) out.push('repoDir')
  return out
}

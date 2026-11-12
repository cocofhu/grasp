import type { Project, ProjectInput } from '@/lib/api/apiTypes'

export type ProjectDraft = {
  name: string
  description: string
  repoUrl: string
  defaultBranch: string
  repoDirName: string
  credentialId: string
  commitName: string
  commitEmail: string
}

export function emptyDraft(): ProjectDraft {
  return {
    name: '',
    description: '',
    repoUrl: '',
    defaultBranch: '',
    repoDirName: '',
    credentialId: '',
    commitName: '',
    commitEmail: '',
  }
}

export function draftFromProject(p: Project): ProjectDraft {
  return {
    name: p.name,
    description: p.description ?? '',
    repoUrl: p.repoUrl,
    defaultBranch: p.defaultBranch ?? '',
    repoDirName: p.repoDirName ?? '',
    credentialId: p.credentialId ?? '',
    commitName: p.commitName ?? '',
    commitEmail: p.commitEmail ?? '',
  }
}

export function inputFromDraft(d: ProjectDraft): ProjectInput {
  return {
    name: d.name.trim(),
    description: d.description.trim(),
    repoUrl: d.repoUrl.trim(),
    defaultBranch: d.defaultBranch.trim(),
    repoDirName: d.repoDirName.trim(),
    credentialId: d.credentialId.trim(),
    commitName: d.commitName.trim(),
    commitEmail: d.commitEmail.trim(),
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
  if (!validCommitName(d.commitName)) out.push('commitName')
  if (!validCommitEmail(d.commitEmail)) out.push('commitEmail')
  return out
}

/** Same rules as the server: Git rejects these characters in an author name. */
export function validCommitName(name: string): boolean {
  const s = name.trim()
  return [...s].length <= 120 && !/[<>\r\n\0]/.test(s)
}

export function validCommitEmail(email: string): boolean {
  const s = email.trim()
  return !s || /^[^\s<>@]+@[^\s<>@]+$/.test(s)
}

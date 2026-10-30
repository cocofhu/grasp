import type { MessageReference } from '@/lib/api/apiTypes'

/** Icon for a reference chip or picker row. */
export function referenceIcon(ref: Pick<MessageReference, 'kind' | 'state' | 'access' | 'error'>): string {
  if (ref.access === 'denied' || ref.error) return 'warning'
  if (ref.kind === 'solo') return 'chat'
  if (ref.kind === 'github_pr') return ref.state === 'merged' ? 'git' : 'branch'
  return ref.state === 'closed' ? 'check' : 'circle'
}

/** owner/repo of a github.com clone URL (https or ssh), or ''. */
export function githubRepoOf(url: string | undefined): string {
  const m = (url || '').trim().match(/github\.com[:/]([\w.-]+)\/([\w.-]+?)(?:\.git)?\/?$/i)
  return m ? `${m[1]}/${m[2]}` : ''
}

/** Link a history chip opens: GitHub URL, or the referenced Solo session. */
export function referenceHref(ref: Pick<MessageReference, 'kind' | 'target' | 'url'>): string {
  if (ref.kind === 'solo') return `/solo/${encodeURIComponent(ref.target)}`
  return ref.url || ''
}

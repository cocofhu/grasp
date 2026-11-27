import type { MessageReference } from '@/lib/api/apiTypes'
import { artifactsClient } from '@/lib/api/clients/artifactsClient'

/** Icon for a reference chip or picker row. */
export function referenceIcon(ref: Pick<MessageReference, 'kind' | 'state' | 'access' | 'error'>): string {
  if (ref.access === 'denied' || ref.error) return 'warning'
  if (ref.kind === 'solo') return 'chat'
  if (ref.kind === 'artifact') return 'artifact'
  if (ref.kind === 'github_discussion') return 'pending-chat'
  if (ref.kind === 'github_pr') return ref.state === 'merged' ? 'git' : 'branch'
  return ref.state === 'closed' ? 'check' : 'circle'
}

/** Icon tone class: open green, merged accent, closed grey; sessions keep the text color. */
export function referenceTone(ref: Pick<MessageReference, 'kind' | 'state' | 'access' | 'error'>): string {
  if (ref.access === 'denied' || ref.error || ref.kind === 'solo' || ref.kind === 'artifact') return ''
  if (ref.state === 'merged') return 'ref-tone--merged'
  if (ref.state === 'closed') return 'ref-tone--closed'
  return 'ref-tone--open'
}

/** owner/repo of a github.com clone URL (https or ssh), or ''. */
export function githubRepoOf(url: string | undefined): string {
  const m = (url || '').trim().match(/github\.com[:/]([\w.-]+)\/([\w.-]+?)(?:\.git)?\/?$/i)
  return m ? `${m[1]}/${m[2]}` : ''
}

/** Link a history chip opens. */
export function referenceHref(ref: Pick<MessageReference, 'kind' | 'target' | 'url' | 'anchor' | 'error'>): string {
  if (ref.kind === 'solo') return `/solo/${encodeURIComponent(ref.target)}`
  if (ref.kind === 'artifact') {
    const revision = Number(ref.anchor)
    return !ref.error && Number.isSafeInteger(revision) && revision > 0 ? artifactsClient.artifactRawUrl(ref.target, revision) : ''
  }
  return ref.url || ''
}

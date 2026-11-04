import type { Artifact, ArtifactEditReason, Run, RunArtifactEditState } from '@/lib/shared/types'
import { isReadonlyArtifactKind, inferArtifactKind } from '@/lib/inbox/gateUpstream'
import { isFeedbackArtifactName, isHistoricalStageArtifact } from './reactArtifactPreview'

const HISTORICAL_NAME = /#iter-\d+$/

/** Whether this artifact (latest version) may be opened in the editor at all. */
export function isEditableArtifact(artifact: Pick<Artifact, 'id' | 'name' | 'kind'> | null | undefined): boolean {
  if (!artifact) return false
  const name = String(artifact.name || '').trim()
  if (!name || HISTORICAL_NAME.test(name) || name.includes('/')) return false
  if (isFeedbackArtifactName(name)) return false
  if (isHistoricalStageArtifact(artifact)) return false
  if (isReadonlyArtifactKind(artifact.kind) || isReadonlyArtifactKind(inferArtifactKind(name))) return false
  return true
}

function reactSessionsBusy(run: Run): boolean {
  const sessions = run.reactSessions
  if (!sessions) return false
  return Object.values(sessions).some((s) => !!s && (s.busy === true || (s.waiting ?? 0) > 0))
}

/**
 * Client view of server ArtifactEditState. `liveBusy` comes from a mounted
 * conversation (thinking / queued) and, when known, wins over the run snapshot.
 */
export function resolveRunEditState(run: Run | null | undefined, liveBusy?: boolean | null): RunArtifactEditState {
  if (!run) return { editable: false, reason: 'run_running' }
  switch (run.status) {
    case 'failed':
      return { editable: true, reason: '' }
    case 'completed':
    case 'cancelled':
      return { editable: false, reason: 'run_ended' }
    case 'waiting_human': {
      const busy =
        liveBusy ?? (run.artifactEdit ? run.artifactEdit.reason === 'session_busy' : reactSessionsBusy(run))
      return busy ? { editable: false, reason: 'session_busy' } : { editable: true, reason: '' }
    }
    default:
      return { editable: false, reason: 'run_running' }
  }
}

/** i18n key under pages.artifactEditor for a blocked reason. */
export function editReasonKey(reason: ArtifactEditReason): string {
  switch (reason) {
    case 'run_ended':
      return 'pages.artifactEditor.reasonRunEnded'
    case 'session_busy':
      return 'pages.artifactEditor.reasonSessionBusy'
    case 'run_running':
      return 'pages.artifactEditor.reasonRunRunning'
    default:
      return ''
  }
}

function stableStringify(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(stableStringify).join(',')}]`
  if (v && typeof v === 'object') {
    const keys = Object.keys(v as Record<string, unknown>).sort()
    return `{${keys.map((k) => `${JSON.stringify(k)}:${stableStringify((v as Record<string, unknown>)[k])}`).join(',')}}`
  }
  return JSON.stringify(v ?? null)
}

function tryParse(text: string): { ok: true; value: unknown } | { ok: false } {
  try {
    return { ok: true, value: JSON.parse(text) }
  } catch {
    return { ok: false }
  }
}

/** Content equality: JSON compares by value (key order ignored), other kinds by text. */
export function sameArtifactContent(kind: string, a: string, b: string): boolean {
  if (a === b) return true
  if (kind === 'json') {
    const pa = tryParse(a)
    const pb = tryParse(b)
    if (pa.ok && pb.ok) return stableStringify(pa.value) === stableStringify(pb.value)
  }
  return a.replace(/\r\n/g, '\n').trimEnd() === b.replace(/\r\n/g, '\n').trimEnd()
}

function countLeafChanges(a: unknown, b: unknown): number {
  if (Array.isArray(a) && Array.isArray(b)) {
    let n = 0
    const len = Math.max(a.length, b.length)
    for (let i = 0; i < len; i++) n += i < a.length && i < b.length ? countLeafChanges(a[i], b[i]) : 1
    return n
  }
  if (a && b && typeof a === 'object' && typeof b === 'object' && !Array.isArray(a) && !Array.isArray(b)) {
    const ka = a as Record<string, unknown>
    const kb = b as Record<string, unknown>
    let n = 0
    for (const k of new Set([...Object.keys(ka), ...Object.keys(kb)])) {
      if (!(k in ka) || !(k in kb)) n += 1
      else n += countLeafChanges(ka[k], kb[k])
    }
    return n
  }
  return stableStringify(a) === stableStringify(b) ? 0 : 1
}

/** Rough "N changes" count: changed JSON leaves, or changed lines for text. */
export function countArtifactChanges(kind: string, base: string, draft: string): number {
  if (sameArtifactContent(kind, base, draft)) return 0
  if (kind === 'json') {
    const pa = tryParse(base)
    const pb = tryParse(draft)
    if (pa.ok && pb.ok) return Math.max(1, countLeafChanges(pa.value, pb.value))
  }
  const la = base.replace(/\r\n/g, '\n').split('\n')
  const lb = draft.replace(/\r\n/g, '\n').split('\n')
  let n = 0
  const len = Math.max(la.length, lb.length)
  for (let i = 0; i < len; i++) if (la[i] !== lb[i]) n++
  return Math.max(1, n)
}

/** 1-based line/column of a JSON.parse failure, or null when the text parses. */
export function jsonErrorLocation(text: string): { line: number; column: number; message: string } | null {
  try {
    JSON.parse(text)
    return null
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e)
    const lc = /line (\d+) column (\d+)/i.exec(message)
    if (lc) return { line: Number(lc[1]), column: Number(lc[2]), message }
    const pos = /position (\d+)/i.exec(message)
    const offset = pos ? Math.min(Number(pos[1]), text.length) : text.length
    const before = text.slice(0, offset).split('\n')
    return { line: before.length, column: (before[before.length - 1]?.length ?? 0) + 1, message }
  }
}

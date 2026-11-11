/**
 * CommentPin session state for HtmlPreview element annotations (MVP).
 * Keyed by runId + gateNodeId + gate iteration so a new feedback round starts empty.
 * Persists to sessionStorage for refresh/re-entry within the same round.
 * Does not touch PreviewIssue / Agent enqueue.
 */

export type CommentPinScreenshot = 'present' | 'MISSING'

export interface CommentPinBounds {
  left: number
  top: number
  width: number
  height: number
}

export interface CommentPin {
  id: string
  seq: number
  selector: string
  comment: string
  currentText?: string
  screenshot: CommentPinScreenshot
  /** Optional data URL when screenshot === 'present'. */
  imageDataUrl?: string
  markKind: 'click'
  bounds?: CommentPinBounds
  createdAt: string
  updatedAt: string
}

interface CommentPinRoundState {
  pins: CommentPin[]
  nextSeq: number
  /** True after a successful PUT annotation-artifact for the current pin set. */
  artifactCommitted: boolean
  /** Fingerprint of pins when last written (detect dirty after commit). */
  committedFingerprint: string
}

const ANNOTATION_HARD_SCOPE = '仅改标中区域；越界先问'

function pinKey(runId: string, gateNodeId: string, iteration: number): string {
  return `appr.commentPins:${runId}:${gateNodeId}:${iteration}`
}

function emptyState(): CommentPinRoundState {
  return { pins: [], nextSeq: 1, artifactCommitted: false, committedFingerprint: '' }
}

function fingerprint(pins: CommentPin[]): string {
  return pins
    .map((p) => `${p.id}:${p.seq}:${p.selector}:${p.comment}:${p.screenshot}`)
    .join('|')
}

function loadState(key: string): CommentPinRoundState {
  if (typeof sessionStorage === 'undefined') return emptyState()
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return emptyState()
    const parsed = JSON.parse(raw) as CommentPinRoundState
    if (!parsed || !Array.isArray(parsed.pins) || typeof parsed.nextSeq !== 'number') {
      return emptyState()
    }
    return {
      pins: parsed.pins,
      nextSeq: Math.max(1, parsed.nextSeq),
      artifactCommitted: !!parsed.artifactCommitted,
      committedFingerprint: parsed.committedFingerprint || '',
    }
  } catch {
    return emptyState()
  }
}

function saveState(key: string, state: CommentPinRoundState) {
  if (typeof sessionStorage === 'undefined') return
  try {
    sessionStorage.setItem(key, JSON.stringify(state))
  } catch {
    // quota / private mode — keep in-memory only
  }
}

const memory = new Map<string, CommentPinRoundState>()

/** Test-only: clear in-memory + sessionStorage pin state. */
export function resetCommentPinsForTests() {
  memory.clear()
  if (typeof sessionStorage === 'undefined') return
  const keys: string[] = []
  for (let i = 0; i < sessionStorage.length; i++) {
    const k = sessionStorage.key(i)
    if (k?.startsWith('appr.commentPins:')) keys.push(k)
  }
  for (const k of keys) sessionStorage.removeItem(k)
}

function ensure(runId: string, gateNodeId: string, iteration: number): CommentPinRoundState {
  const k = pinKey(runId, gateNodeId, iteration)
  let s = memory.get(k)
  if (!s) {
    s = loadState(k)
    memory.set(k, s)
  }
  return s
}

function persist(runId: string, gateNodeId: string, iteration: number, state: CommentPinRoundState) {
  const k = pinKey(runId, gateNodeId, iteration)
  memory.set(k, state)
  saveState(k, state)
}

function newId(seq: number): string {
  return `pin-${Date.now()}-${seq}-${Math.random().toString(36).slice(2, 7)}`
}

type SaveCommentPinInput = {
  selector: string
  comment: string
  currentText?: string
  imageDataUrl?: string
  bounds?: CommentPinBounds
  /** When set, update that pin instead of creating a new one. */
  editingId?: string | null
}

type SaveCommentPinResult = {
  pin: CommentPin
  /** True when this mutate cancelled a prior committed write (server must clear). */
  needsServerInvalidate: boolean
}

/** Create or update a pin. Seq is assigned on create and never recycled on delete. */
export function saveCommentPin(
  runId: string,
  gateNodeId: string,
  iteration: number,
  input: SaveCommentPinInput,
): SaveCommentPinResult | null {
  const comment = (input.comment || '').trim()
  const selector = (input.selector || '').trim()
  if (!comment || !selector) return null

  const prevRound = ensure(runId, gateNodeId, iteration)
  const wasCommitted =
    !!prevRound.artifactCommitted && prevRound.committedFingerprint === fingerprint(prevRound.pins)
  const state = { ...prevRound, pins: [...prevRound.pins] }
  const now = new Date().toISOString()
  const hasShot = !!(input.imageDataUrl && input.imageDataUrl.startsWith('data:'))
  const screenshot: CommentPinScreenshot = hasShot ? 'present' : 'MISSING'

  let pin: CommentPin
  if (input.editingId) {
    const idx = state.pins.findIndex((p) => p.id === input.editingId)
    if (idx < 0) return null
    const prev = state.pins[idx]
    pin = {
      ...prev,
      comment,
      selector,
      currentText: (input.currentText || '').trim() || prev.currentText,
      screenshot,
      imageDataUrl: hasShot ? input.imageDataUrl : undefined,
      bounds: input.bounds ?? prev.bounds,
      updatedAt: now,
    }
    state.pins[idx] = pin
  } else {
    const seq = state.nextSeq
    pin = {
      id: newId(seq),
      seq,
      selector,
      comment,
      currentText: (input.currentText || '').trim() || undefined,
      screenshot,
      imageDataUrl: hasShot ? input.imageDataUrl : undefined,
      markKind: 'click',
      bounds: input.bounds,
      createdAt: now,
      updatedAt: now,
    }
    state.pins.push(pin)
    state.nextSeq = seq + 1
  }

  // Any mutate cancels committed state (f4).
  state.artifactCommitted = false
  state.committedFingerprint = ''
  persist(runId, gateNodeId, iteration, state)
  return { pin, needsServerInvalidate: wasCommitted }
}

type DeleteCommentPinResult = {
  deleted: boolean
  /** True when this mutate cancelled a prior committed write (server must clear). */
  needsServerInvalidate: boolean
}

export function deleteCommentPin(
  runId: string,
  gateNodeId: string,
  iteration: number,
  pinId: string,
): DeleteCommentPinResult {
  const prev = ensure(runId, gateNodeId, iteration)
  const wasCommitted =
    !!prev.artifactCommitted && prev.committedFingerprint === fingerprint(prev.pins)
  const pins = prev.pins.filter((p) => p.id !== pinId)
  if (pins.length === prev.pins.length) {
    return { deleted: false, needsServerInvalidate: false }
  }
  persist(runId, gateNodeId, iteration, {
    ...prev,
    pins,
    artifactCommitted: false,
    committedFingerprint: '',
  })
  return { deleted: true, needsServerInvalidate: wasCommitted }
}

export function listCommentPins(
  runId: string,
  gateNodeId: string,
  iteration: number,
): CommentPin[] {
  return [...ensure(runId, gateNodeId, iteration).pins].sort((a, b) => a.seq - b.seq)
}

export function getCommentPinRound(
  runId: string,
  gateNodeId: string,
  iteration: number,
): CommentPinRoundState {
  const s = ensure(runId, gateNodeId, iteration)
  return {
    pins: [...s.pins].sort((a, b) => a.seq - b.seq),
    nextSeq: s.nextSeq,
    artifactCommitted: s.artifactCommitted && s.committedFingerprint === fingerprint(s.pins),
    committedFingerprint: s.committedFingerprint,
  }
}

export function markCommentPinsCommitted(
  runId: string,
  gateNodeId: string,
  iteration: number,
): void {
  const s = ensure(runId, gateNodeId, iteration)
  const fp = fingerprint(s.pins)
  persist(runId, gateNodeId, iteration, {
    ...s,
    pins: [...s.pins],
    artifactCommitted: true,
    committedFingerprint: fp,
  })
}

/** Build the JSON body for PUT .../annotation-artifact. */
export function buildAnnotationArtifactPayload(pins: CommentPin[]): {
  kind: string
  consumer: string
  route: string
  hardScope: string
  count: number
  annotations: Array<{
    seq: number
    selector: string
    comment: string
    currentText?: string
    screenshot: CommentPinScreenshot
    imageDataUrl?: string
    markKind: string
    bounds?: CommentPinBounds
  }>
} {
  const sorted = [...pins].sort((a, b) => a.seq - b.seq)
  return {
    kind: 'preview_annotations',
    consumer: 'next_node',
    route: 'artifact_only',
    hardScope: ANNOTATION_HARD_SCOPE,
    count: sorted.length,
    annotations: sorted.map((p) => ({
      seq: p.seq,
      selector: p.selector,
      comment: p.comment,
      currentText: p.currentText,
      screenshot: p.screenshot,
      imageDataUrl: p.screenshot === 'present' ? p.imageDataUrl : undefined,
      markKind: p.markKind,
      bounds: p.bounds,
    })),
  }
}

/** Human-readable draft preview (Demo-aligned). */
export function formatAnnotationArtifactPreview(pins: CommentPin[]): string {
  if (!pins.length) return '（暂无标注）'
  const lines = [
    'kind: preview_annotations',
    'consumer: next_node',
    'route: artifact_only  # 不发送 Agent',
    `hardScope: ${ANNOTATION_HARD_SCOPE}`,
    `count: ${pins.length}`,
    'annotations:',
  ]
  for (const p of [...pins].sort((a, b) => a.seq - b.seq)) {
    lines.push(`  - seq: ${p.seq}`)
    lines.push(`    selector: ${p.selector}`)
    lines.push(`    comment: ${JSON.stringify(p.comment)}`)
    lines.push(`    currentText: ${JSON.stringify(p.currentText || '')}`)
    lines.push(`    screenshot: ${p.screenshot}`)
    lines.push(`    markKind: ${p.markKind}`)
  }
  return lines.join('\n')
}

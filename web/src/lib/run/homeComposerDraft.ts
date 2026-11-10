import type { ClarifyImage } from '../shared/types'
import { MAX_REFERENCES, type ReferenceToken } from '../solo/referenceTokens'
import {
  HOME_DRAFT_ID,
  blobToBase64,
  getDraftIdb,
  isQuotaError,
  tryBase64ToBlob,
  type DraftAttachmentRecord,
  type HomeDraftRecord,
} from './draftIdb'

/** localStorage text fallback used when IndexedDB writes fail. */
export const HOME_COMPOSER_DRAFT_KEY = 'grasp.home.composerDraft'

export const HOME_COMPOSER_DRAFT_SCHEMA = '1'

export interface HomeDraftAttachment {
  name?: string
  mimeType: string
  /** base64 payload without data URL prefix */
  data: string
}

export interface HomeComposerDraft {
  schemaVersion: string
  savedAt: number
  pipelineId: string
  text: string
  attachments: HomeDraftAttachment[]
  referenceTokens?: ReferenceToken[]
}

/** ok = full save; partial = text persisted, attachments only in memory; quota_exceeded / error. */
export type SaveHomeComposerDraftResult = 'ok' | 'partial' | 'quota_exceeded' | 'error'

function isAttachment(v: unknown): v is HomeDraftAttachment {
  if (!v || typeof v !== 'object') return false
  const a = v as Record<string, unknown>
  return typeof a.mimeType === 'string' && typeof a.data === 'string'
}

function normalizeAttachments(raw: unknown): HomeDraftAttachment[] {
  if (!Array.isArray(raw)) return []
  const out: HomeDraftAttachment[] = []
  for (const item of raw) {
    if (!isAttachment(item)) continue
    const next: HomeDraftAttachment = { mimeType: item.mimeType, data: item.data }
    if (typeof item.name === 'string' && item.name) next.name = item.name
    out.push(next)
  }
  return out
}

/** Keep exact spans so restoring a draft does not depend on loaded repo choices. */
function normalizeReferenceTokens(raw: unknown, text: string): ReferenceToken[] {
  if (!Array.isArray(raw)) return []
  const out: ReferenceToken[] = []
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue
    const { ref, start, end } = item
    if (!ref || !['solo', 'github_issue', 'github_pr'].includes(ref.kind) ||
        typeof ref.target !== 'string' || !ref.target || typeof ref.title !== 'string' ||
        !Number.isInteger(start) || !Number.isInteger(end) || start < 0 || end <= start || end > text.length ||
        text[start] !== '@' || out.some(token => start < token.end && end > token.start)) continue
    out.push({ start, end, ref: {
      kind: ref.kind, target: ref.target, title: ref.title,
      ...(typeof ref.url === 'string' ? { url: ref.url } : {}),
      ...(typeof ref.state === 'string' ? { state: ref.state } : {}),
    } })
    if (out.length === MAX_REFERENCES) break
  }
  return out.sort((a, b) => a.start - b.start)
}

function parseDraft(raw: string): HomeComposerDraft | null {
  try {
    const parsed = JSON.parse(raw) as Partial<HomeComposerDraft>
    if (!parsed || typeof parsed !== 'object') return null
    if (typeof parsed.text !== 'string') return null
    if (typeof parsed.pipelineId !== 'string') return null
    if (typeof parsed.savedAt !== 'number' || !Number.isFinite(parsed.savedAt)) return null
    const attachments = normalizeAttachments(parsed.attachments)
    const referenceTokens = normalizeReferenceTokens(parsed.referenceTokens, parsed.text)
    return {
      schemaVersion:
        typeof parsed.schemaVersion === 'string' && parsed.schemaVersion
          ? parsed.schemaVersion
          : HOME_COMPOSER_DRAFT_SCHEMA,
      savedAt: parsed.savedAt,
      pipelineId: parsed.pipelineId,
      text: parsed.text,
      attachments,
      ...(referenceTokens.length ? { referenceTokens } : {}),
    }
  } catch {
    return null
  }
}

/** True when there is nothing worth persisting (empty text + no attachments). */
export function isHomeComposerDraftEmpty(text: string, attachments: ClarifyImage[]): boolean {
  return !text.trim() && attachments.length === 0
}

function toPersistableAttachments(attachments: ClarifyImage[]): HomeDraftAttachment[] {
  const out: HomeDraftAttachment[] = []
  for (const im of attachments) {
    if (!im.data) continue
    const next: HomeDraftAttachment = { mimeType: im.mimeType, data: im.data }
    if (im.name) next.name = im.name
    out.push(next)
  }
  return out
}

function toIdbAttachments(attachments: HomeDraftAttachment[]): DraftAttachmentRecord[] {
  const out: DraftAttachmentRecord[] = []
  let i = 0
  for (const a of attachments) {
    const blob = tryBase64ToBlob(a.data, a.mimeType)
    if (!blob) continue
    const rec: DraftAttachmentRecord = {
      id: `home:${HOME_DRAFT_ID}:${i}`,
      ownerKind: 'home',
      ownerId: HOME_DRAFT_ID,
      mimeType: a.mimeType,
      data: blob,
      sizeBytes: blob.size,
      sortIndex: i,
    }
    if (a.name) rec.name = a.name
    out.push(rec)
    i++
  }
  return out
}

async function fromIdbAttachments(rows: DraftAttachmentRecord[]): Promise<HomeDraftAttachment[]> {
  const out: HomeDraftAttachment[] = []
  for (const row of rows) {
    const data = await blobToBase64(row.data)
    const next: HomeDraftAttachment = { mimeType: row.mimeType, data }
    if (row.name) next.name = row.name
    out.push(next)
  }
  return out
}

function readTextFallback(): HomeComposerDraft | null {
  try {
    const raw = localStorage.getItem(HOME_COMPOSER_DRAFT_KEY)
    if (!raw) return null
    return parseDraft(raw)
  } catch {
    return null
  }
}

function writeTextFallback(draft: HomeComposerDraft): SaveHomeComposerDraftResult {
  try {
    localStorage.setItem(HOME_COMPOSER_DRAFT_KEY, JSON.stringify(draft))
    return draft.attachments.length > 0 ? 'partial' : 'ok'
  } catch (e: unknown) {
    if (isQuotaError(e)) {
      if (draft.attachments.length > 0 || draft.text) {
        try {
          const slim: HomeComposerDraft = { ...draft, attachments: [] }
          localStorage.setItem(HOME_COMPOSER_DRAFT_KEY, JSON.stringify(slim))
          return 'quota_exceeded'
        } catch (e2: unknown) {
          if (isQuotaError(e2)) return 'quota_exceeded'
          return 'error'
        }
      }
      return 'quota_exceeded'
    }
    return 'error'
  }
}

function clearTextFallback(): void {
  try {
    localStorage.removeItem(HOME_COMPOSER_DRAFT_KEY)
  } catch {
    /* ignore private mode */
  }
}

async function draftFromIdb(packed: {
  record: HomeDraftRecord
  attachments: DraftAttachmentRecord[]
}): Promise<HomeComposerDraft> {
  const referenceTokens = normalizeReferenceTokens(packed.record.referenceTokens, packed.record.text)
  return {
    schemaVersion: packed.record.schemaVersion || HOME_COMPOSER_DRAFT_SCHEMA,
    savedAt: packed.record.savedAt,
    pipelineId: packed.record.pipelineId,
    text: packed.record.text,
    attachments: await fromIdbAttachments(packed.attachments),
    ...(referenceTokens.length ? { referenceTokens } : {}),
  }
}

export async function loadHomeComposerDraft(): Promise<HomeComposerDraft | null> {
  const fallback = readTextFallback()
  try {
    const packed = await getDraftIdb().getHome()
    if (packed && fallback) {
      // Prefer newer savedAt so quota-fallback LS text wins over stale IDB (review v2 / F4).
      if (fallback.savedAt > packed.record.savedAt) {
        return fallback
      }
      clearTextFallback()
      return draftFromIdb(packed)
    }
    if (packed) {
      return draftFromIdb(packed)
    }
  } catch {
    /* fall through to text fallback */
  }
  return fallback
}

export async function clearHomeComposerDraft(): Promise<void> {
  try {
    await getDraftIdb().deleteHome()
  } catch {
    /* ignore */
  }
  clearTextFallback()
}

/**
 * Persist home composer draft to IndexedDB (attachments as Blob).
 * Empty content clears the record instead of writing a shell.
 * On IDB failure: text(+workflow) may fall back to localStorage → partial / quota_exceeded.
 */
export async function saveHomeComposerDraft(
  text: string,
  attachments: ClarifyImage[],
  pipelineId: string,
  referenceTokens: ReferenceToken[] = [],
): Promise<SaveHomeComposerDraftResult> {
  const persistable = toPersistableAttachments(attachments)
  if (isHomeComposerDraftEmpty(text, persistable as ClarifyImage[])) {
    await clearHomeComposerDraft()
    return 'ok'
  }
  const payload: HomeComposerDraft = {
    schemaVersion: HOME_COMPOSER_DRAFT_SCHEMA,
    savedAt: Date.now(),
    pipelineId: pipelineId || '',
    text,
    attachments: persistable,
    referenceTokens: normalizeReferenceTokens(referenceTokens, text),
  }
  const record: HomeDraftRecord = {
    id: HOME_DRAFT_ID,
    schemaVersion: payload.schemaVersion,
    savedAt: payload.savedAt,
    pipelineId: payload.pipelineId,
    text: payload.text,
    referenceTokens: payload.referenceTokens,
  }
  try {
    await getDraftIdb().putHome(record, toIdbAttachments(persistable))
    clearTextFallback()
    return 'ok'
  } catch (e: unknown) {
    const fallback: HomeComposerDraft = { ...payload, attachments: [] }
    const fb = writeTextFallback(fallback)
    if (isQuotaError(e)) {
      return fb === 'ok' || fb === 'partial' ? 'quota_exceeded' : fb
    }
    if (fb === 'error' || fb === 'quota_exceeded') return fb
    // IDB unavailable: text on LS; attachments stay in memory → partial when images present
    return persistable.length > 0 ? 'partial' : 'ok'
  }
}

import type { ClarifyImage } from '../shared/types'
import {
  blobToBase64,
  getDraftIdb,
  isQuotaError,
  tryBase64ToBlob,
  type DraftAttachmentRecord,
  type RunDraftRecord,
} from './draftIdb'

export interface RunDraftPayload {
  workflowId: string
  savedAt: number
  inputs: Record<string, string>
  images: Record<string, ClarifyImage[]>
}

/** ok = full save; partial = fields persisted without images; quota_exceeded / error. */
export type SaveRunDraftResult = 'ok' | 'partial' | 'quota_exceeded' | 'error'

/** localStorage text fallback used when IndexedDB writes fail. */
const FALLBACK_PREFIX = 'run-draft:'

function draftKey(workflowId: string): string {
  return `${FALLBACK_PREFIX}${workflowId}`
}

function readTextFallback(workflowId: string): RunDraftPayload | null {
  try {
    const raw = localStorage.getItem(draftKey(workflowId))
    if (!raw) return null
    return JSON.parse(raw) as RunDraftPayload
  } catch {
    return null
  }
}

function clearTextFallback(workflowId: string): void {
  try {
    localStorage.removeItem(draftKey(workflowId))
  } catch {
    /* ignore */
  }
}

function writeTextFallback(payload: RunDraftPayload): SaveRunDraftResult {
  const slim: RunDraftPayload = {
    ...payload,
    images: {},
  }
  try {
    localStorage.setItem(draftKey(payload.workflowId), JSON.stringify(slim))
    const hadImages = Object.values(payload.images).some((arr) => (arr || []).length > 0)
    return hadImages ? 'partial' : 'ok'
  } catch (e: unknown) {
    if (isQuotaError(e)) return 'quota_exceeded'
    return 'error'
  }
}

function flattenImages(
  workflowId: string,
  images: Record<string, ClarifyImage[]>,
): DraftAttachmentRecord[] {
  const out: DraftAttachmentRecord[] = []
  let sortIndex = 0
  for (const [fieldKey, list] of Object.entries(images)) {
    for (const im of list || []) {
      if (!im.data) continue
      const blob = tryBase64ToBlob(im.data, im.mimeType)
      if (!blob) continue
      const rec: DraftAttachmentRecord = {
        id: `run:${workflowId}:${fieldKey}:${sortIndex}`,
        ownerKind: 'run',
        ownerId: workflowId,
        mimeType: im.mimeType,
        data: blob,
        sizeBytes: blob.size,
        fieldKey,
        sortIndex,
      }
      if (im.name) rec.name = im.name
      out.push(rec)
      sortIndex++
    }
  }
  return out
}

async function inflateImages(rows: DraftAttachmentRecord[]): Promise<Record<string, ClarifyImage[]>> {
  const images: Record<string, ClarifyImage[]> = {}
  const sorted = rows.slice().sort((a, b) => (a.sortIndex ?? 0) - (b.sortIndex ?? 0))
  for (const row of sorted) {
    const key = row.fieldKey || '_'
    if (!images[key]) images[key] = []
    const data = await blobToBase64(row.data)
    const im: ClarifyImage = { mimeType: row.mimeType, data }
    if (row.name) im.name = row.name
    if (row.sizeBytes != null) im.sizeBytes = row.sizeBytes
    images[key].push(im)
  }
  return images
}

function isRunDraftEmpty(inputs: Record<string, string>, images: Record<string, ClarifyImage[]>): boolean {
  const hasText = Object.values(inputs).some((v) => String(v ?? '').trim())
  if (hasText) return false
  for (const list of Object.values(images)) {
    if ((list || []).some((im) => !!im.data)) return false
  }
  return true
}

export async function loadRunDraft(workflowId: string): Promise<RunDraftPayload | null> {
  const fallback = readTextFallback(workflowId)
  try {
    const packed = await getDraftIdb().getRun(workflowId)
    if (packed && fallback) {
      // Prefer newer savedAt so quota-fallback LS fields win over stale IDB (review v2 / F4).
      if ((fallback.savedAt || 0) > packed.record.savedAt) {
        return fallback
      }
      clearTextFallback(workflowId)
      return draftFromIdbRun(packed)
    }
    if (packed) {
      return draftFromIdbRun(packed)
    }
  } catch {
    /* fall through */
  }
  return fallback
}

async function draftFromIdbRun(packed: {
  record: RunDraftRecord
  attachments: DraftAttachmentRecord[]
}): Promise<RunDraftPayload> {
  let inputs: Record<string, string> = {}
  try {
    inputs = JSON.parse(packed.record.inputsJson || '{}') as Record<string, string>
  } catch {
    inputs = {}
  }
  return {
    workflowId: packed.record.workflowId,
    savedAt: packed.record.savedAt,
    inputs,
    images: await inflateImages(packed.attachments),
  }
}

export async function clearRunDraft(workflowId: string): Promise<void> {
  try {
    await getDraftIdb().deleteRun(workflowId)
  } catch {
    /* ignore */
  }
  clearTextFallback(workflowId)
}

export async function saveRunDraft(
  workflowId: string,
  inputs: Record<string, string>,
  images: Record<string, ClarifyImage[]>,
): Promise<SaveRunDraftResult> {
  if (isRunDraftEmpty(inputs, images)) {
    await clearRunDraft(workflowId)
    return 'ok'
  }
  const payload: RunDraftPayload = {
    workflowId,
    savedAt: Date.now(),
    inputs,
    images,
  }
  const record: RunDraftRecord = {
    workflowId,
    savedAt: payload.savedAt,
    inputsJson: JSON.stringify(inputs),
  }
  try {
    await getDraftIdb().putRun(record, flattenImages(workflowId, images))
    clearTextFallback(workflowId)
    return 'ok'
  } catch (e: unknown) {
    const fb = writeTextFallback(payload)
    if (isQuotaError(e)) {
      return fb === 'ok' || fb === 'partial' ? 'quota_exceeded' : fb
    }
    if (fb === 'error' || fb === 'quota_exceeded') return fb
    const hadImages = Object.values(images).some((arr) => (arr || []).some((im) => !!im.data))
    return hadImages ? 'partial' : 'ok'
  }
}

export async function mergeRunDraft(
  workflowId: string,
  seedInputs: Record<string, string>,
  seedImages: Record<string, ClarifyImage[]>,
  fieldKeys: string[],
): Promise<{ inputs: Record<string, string>; images: Record<string, ClarifyImage[]>; restored: boolean }> {
  const draft = await loadRunDraft(workflowId)
  if (!draft) {
    return { inputs: { ...seedInputs }, images: { ...seedImages }, restored: false }
  }

  const inputs = { ...seedInputs }
  const images = { ...seedImages }

  for (const key of fieldKeys) {
    if (Object.prototype.hasOwnProperty.call(draft.inputs, key)) {
      inputs[key] = draft.inputs[key]
    }
    if (Object.prototype.hasOwnProperty.call(draft.images, key)) {
      images[key] = draft.images[key] ? [...draft.images[key]] : []
    }
  }

  return { inputs, images, restored: true }
}

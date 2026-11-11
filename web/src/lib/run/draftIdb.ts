/**
 * IndexedDB persistence for home / run drafts (plan g1.1).
 * Attachments stored as Blob; injectable backend for unit tests.
 */

export const DRAFT_IDB_NAME = 'grasp-drafts'
export const DRAFT_IDB_VERSION = 1
export const HOME_DRAFT_STORE = 'homeDraft'
export const RUN_DRAFT_STORE = 'runDraft'
export const ATTACHMENTS_STORE = 'attachments'
export const HOME_DRAFT_ID = 'current'

export type DraftOwnerKind = 'home' | 'run'

export interface HomeDraftRecord {
  id: string
  schemaVersion: string
  savedAt: number
  pipelineId: string
  text: string
}

export interface RunDraftRecord {
  workflowId: string
  savedAt: number
  inputsJson: string
}

export interface DraftAttachmentRecord {
  id: string
  ownerKind: DraftOwnerKind
  ownerId: string
  name?: string
  mimeType: string
  data: Blob
  sizeBytes?: number
  fieldKey?: string
  sortIndex: number
}

export interface DraftIdbBackend {
  putHome(record: HomeDraftRecord, attachments: DraftAttachmentRecord[]): Promise<void>
  getHome(): Promise<{ record: HomeDraftRecord; attachments: DraftAttachmentRecord[] } | null>
  deleteHome(): Promise<void>
  putRun(record: RunDraftRecord, attachments: DraftAttachmentRecord[]): Promise<void>
  getRun(workflowId: string): Promise<{ record: RunDraftRecord; attachments: DraftAttachmentRecord[] } | null>
  deleteRun(workflowId: string): Promise<void>
}

let injected: DraftIdbBackend | null = null
let dbPromise: Promise<IDBDatabase | null> | null = null

/** Test-only: swap the storage backend (memory / failing stubs). */
export function __setDraftIdbBackendForTests(backend: DraftIdbBackend | null): void {
  injected = backend
  dbPromise = null
}

export function __resetDraftIdbForTests(): void {
  injected = null
  dbPromise = null
}

export function createMemoryDraftIdb(): DraftIdbBackend {
  let home: HomeDraftRecord | null = null
  let homeAtt: DraftAttachmentRecord[] = []
  const runs = new Map<string, RunDraftRecord>()
  const runAtt = new Map<string, DraftAttachmentRecord[]>()

  return {
    async putHome(record, attachments) {
      // Atomic snapshot replace (mirrors single-tx native put).
      const nextHome = { ...record }
      const nextAtt = attachments.map((a) => ({ ...a }))
      home = nextHome
      homeAtt = nextAtt
    },
    async getHome() {
      if (!home) return null
      return { record: { ...home }, attachments: homeAtt.map((a) => ({ ...a })) }
    },
    async deleteHome() {
      home = null
      homeAtt = []
    },
    async putRun(record, attachments) {
      const nextRec = { ...record }
      const nextAtt = attachments.map((a) => ({ ...a }))
      runs.set(nextRec.workflowId, nextRec)
      runAtt.set(nextRec.workflowId, nextAtt)
    },
    async getRun(workflowId) {
      const record = runs.get(workflowId)
      if (!record) return null
      return {
        record: { ...record },
        attachments: (runAtt.get(workflowId) || []).map((a) => ({ ...a })),
      }
    },
    async deleteRun(workflowId) {
      runs.delete(workflowId)
      runAtt.delete(workflowId)
    },
  }
}

function isQuotaError(e: unknown): boolean {
  const err = e as { name?: string; code?: number }
  return err?.name === 'QuotaExceededError' || err?.code === 22
}

export function base64ToBlob(b64: string, mimeType: string): Blob {
  const binary = atob(b64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return new Blob([bytes], { type: mimeType || 'application/octet-stream' })
}

/** Convert base64 attachments; skip corrupt payloads instead of failing the whole save. */
export function tryBase64ToBlob(b64: string, mimeType: string): Blob | null {
  try {
    return base64ToBlob(b64, mimeType)
  } catch {
    return null
  }
}

export async function blobToBase64(blob: Blob): Promise<string> {
  const buf = await blob.arrayBuffer()
  const bytes = new Uint8Array(buf)
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

function openNativeDb(): Promise<IDBDatabase | null> {
  if (dbPromise) return dbPromise
  dbPromise = (async () => {
    if (typeof indexedDB === 'undefined') return null
    return new Promise<IDBDatabase | null>((resolve) => {
      let req: IDBOpenDBRequest
      try {
        req = indexedDB.open(DRAFT_IDB_NAME, DRAFT_IDB_VERSION)
      } catch {
        resolve(null)
        return
      }
      req.onerror = () => resolve(null)
      req.onupgradeneeded = () => {
        const next = req.result
        if (!next.objectStoreNames.contains(HOME_DRAFT_STORE)) {
          next.createObjectStore(HOME_DRAFT_STORE, { keyPath: 'id' })
        }
        if (!next.objectStoreNames.contains(RUN_DRAFT_STORE)) {
          next.createObjectStore(RUN_DRAFT_STORE, { keyPath: 'workflowId' })
        }
        if (!next.objectStoreNames.contains(ATTACHMENTS_STORE)) {
          const store = next.createObjectStore(ATTACHMENTS_STORE, { keyPath: 'id' })
          store.createIndex('byOwner', ['ownerKind', 'ownerId'], { unique: false })
        }
      }
      req.onsuccess = () => {
        const opened = req.result
        opened.onversionchange = () => {
          try {
            opened.close()
          } catch {
            /* ignore */
          }
          dbPromise = null
        }
        resolve(opened)
      }
    })
  })()
  return dbPromise
}

function idbReq<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error || new Error('idb request failed'))
  })
}

function txDone(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error || new Error('idb tx failed'))
    tx.onabort = () => reject(tx.error || new Error('idb tx aborted'))
  })
}

/**
 * Replace owner attachments + main record in a single readwrite transaction.
 * All mutations are queued in the getAllKeys onsuccess callback so the tx cannot
 * auto-commit between delete and put (review v1 atomicity).
 */
function replaceDraftInTx(
  db: IDBDatabase,
  storeNames: string[],
  ownerKind: DraftOwnerKind,
  ownerId: string,
  putMain: (tx: IDBTransaction) => void,
  attachments: DraftAttachmentRecord[],
): Promise<void> {
  const tx = db.transaction(storeNames, 'readwrite')
  return new Promise((resolve, reject) => {
    let settled = false
    const fail = (err: unknown) => {
      if (settled) return
      settled = true
      try {
        tx.abort()
      } catch {
        /* already finished */
      }
      reject(err instanceof Error ? err : new Error(String(err)))
    }
    tx.oncomplete = () => {
      if (settled) return
      settled = true
      resolve()
    }
    tx.onerror = () => fail(tx.error || new Error('idb tx failed'))
    tx.onabort = () => {
      if (settled) return
      settled = true
      reject(tx.error || new Error('idb tx aborted'))
    }
    const attStore = tx.objectStore(ATTACHMENTS_STORE)
    const index = attStore.index('byOwner')
    const keysReq = index.getAllKeys([ownerKind, ownerId])
    keysReq.onerror = () => fail(keysReq.error || new Error('idb getAllKeys failed'))
    keysReq.onsuccess = () => {
      try {
        for (const key of keysReq.result) attStore.delete(key)
        putMain(tx)
        for (const a of attachments) attStore.put(a)
      } catch (e) {
        fail(e)
      }
    }
  })
}

async function loadAttachments(
  db: IDBDatabase,
  ownerKind: DraftOwnerKind,
  ownerId: string,
): Promise<DraftAttachmentRecord[]> {
  const tx = db.transaction(ATTACHMENTS_STORE, 'readonly')
  const index = tx.objectStore(ATTACHMENTS_STORE).index('byOwner')
  const rows = (await idbReq(index.getAll([ownerKind, ownerId]))) as DraftAttachmentRecord[]
  await txDone(tx)
  return rows.slice().sort((a, b) => (a.sortIndex ?? 0) - (b.sortIndex ?? 0))
}

/**
 * Replace home/run draft + attachments in one IDB transaction.
 * Failure rolls back — previous attachments stay intact (review v1).
 */
function createNativeBackend(): DraftIdbBackend {
  return {
    async putHome(record, attachments) {
      const db = await openNativeDb()
      if (!db) throw new Error('IndexedDB unavailable')
      await replaceDraftInTx(
        db,
        [HOME_DRAFT_STORE, ATTACHMENTS_STORE],
        'home',
        HOME_DRAFT_ID,
        (tx) => {
          tx.objectStore(HOME_DRAFT_STORE).put(record)
        },
        attachments,
      )
    },
    async getHome() {
      const db = await openNativeDb()
      if (!db) return null
      const tx = db.transaction(HOME_DRAFT_STORE, 'readonly')
      const record = (await idbReq(tx.objectStore(HOME_DRAFT_STORE).get(HOME_DRAFT_ID))) as
        | HomeDraftRecord
        | undefined
      await txDone(tx)
      if (!record) return null
      const attachments = await loadAttachments(db, 'home', HOME_DRAFT_ID)
      return { record, attachments }
    },
    async deleteHome() {
      const db = await openNativeDb()
      if (!db) return
      await replaceDraftInTx(
        db,
        [HOME_DRAFT_STORE, ATTACHMENTS_STORE],
        'home',
        HOME_DRAFT_ID,
        (tx) => {
          tx.objectStore(HOME_DRAFT_STORE).delete(HOME_DRAFT_ID)
        },
        [],
      )
    },
    async putRun(record, attachments) {
      const db = await openNativeDb()
      if (!db) throw new Error('IndexedDB unavailable')
      await replaceDraftInTx(
        db,
        [RUN_DRAFT_STORE, ATTACHMENTS_STORE],
        'run',
        record.workflowId,
        (tx) => {
          tx.objectStore(RUN_DRAFT_STORE).put(record)
        },
        attachments,
      )
    },
    async getRun(workflowId) {
      const db = await openNativeDb()
      if (!db) return null
      const tx = db.transaction(RUN_DRAFT_STORE, 'readonly')
      const record = (await idbReq(tx.objectStore(RUN_DRAFT_STORE).get(workflowId))) as
        | RunDraftRecord
        | undefined
      await txDone(tx)
      if (!record) return null
      const attachments = await loadAttachments(db, 'run', workflowId)
      return { record, attachments }
    },
    async deleteRun(workflowId) {
      const db = await openNativeDb()
      if (!db) return
      await replaceDraftInTx(
        db,
        [RUN_DRAFT_STORE, ATTACHMENTS_STORE],
        'run',
        workflowId,
        (tx) => {
          tx.objectStore(RUN_DRAFT_STORE).delete(workflowId)
        },
        [],
      )
    },
  }
}

export function getDraftIdb(): DraftIdbBackend {
  return injected || createNativeBackend()
}

export { isQuotaError }

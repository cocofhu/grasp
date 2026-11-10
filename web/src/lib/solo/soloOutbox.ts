import type { ReferenceInput, SoloCreateBody } from '@/lib/api/apiTypes'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

export type SoloTurnRequest = {
  clientMessageId: string
  text: string
  images?: ClarifyImage[]
  references?: ReferenceInput[]
  annotations?: ReactAnnotation[]
}

export type SoloCreationOutbox = {
  scope: string
  id: string
  kind: 'creation'
  request: SoloCreateBody
  createdAt: string
  continuationRunId?: string
}
export type SoloMessageOutbox = {
  scope: string
  id: string
  kind: 'message'
  request: SoloTurnRequest
  createdAt: string
}
export type SoloOutboxRecord = SoloCreationOutbox | SoloMessageOutbox

const databaseName = 'grasp-solo-outbox-v1'
const table = 'requests'
let opening: Promise<IDBDatabase> | undefined

export function soloOutboxScope(principal: string, kind: SoloOutboxRecord['kind'], sessionID = '') {
  if (!principal || (kind === 'message' && !sessionID)) throw new Error('Outbox requires an authorized owner and conversation')
  // IndexedDB already separates origins. Never store embed bearer tokens or
  // session cookies; the owner and canonical chat ID are sufficient here.
  return JSON.stringify([principal, kind, sessionID])
}

export function newSoloRequestID() {
  const bytes = new Uint8Array(16)
  globalThis.crypto.getRandomValues(bytes)
  return `local-${Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')}`
}

function database(): Promise<IDBDatabase> {
  if (opening) return opening
  opening = new Promise<IDBDatabase>((resolve, reject) => {
    if (!globalThis.indexedDB) { reject(new Error('Browser storage is unavailable')); return }
    let abandoned = false
    const request = indexedDB.open(databaseName, 1)
    request.onupgradeneeded = () => {
      const store = request.result.createObjectStore(table, { keyPath: 'key' })
      store.createIndex('scope', 'scope')
    }
    request.onerror = () => reject(request.error || new Error('Cannot open browser storage'))
    request.onblocked = () => { abandoned = true; reject(new Error('Browser storage upgrade is blocked')) }
    request.onsuccess = () => {
      const db = request.result
      if (abandoned) { db.close(); return }
      db.onversionchange = () => { db.close(); opening = undefined }
      resolve(db)
    }
  }).catch(error => { opening = undefined; throw error })
  return opening
}

async function transaction<T>(mode: IDBTransactionMode, run: (store: IDBObjectStore, complete: (result: T) => void, fail: (error: Error) => void) => void): Promise<T> {
  const db = await database()
  return new Promise<T>((resolve, reject) => {
    const tx = db.transaction(table, mode)
    let result: T
    let failure: Error | undefined
    tx.oncomplete = () => resolve(result)
    tx.onerror = () => reject(failure || tx.error || new Error('Browser storage failed'))
    tx.onabort = () => reject(failure || tx.error || new Error('Browser storage was interrupted'))
    run(tx.objectStore(table), value => { result = value }, error => { failure = error; tx.abort() })
  })
}

export async function readSoloOutbox(scope: string): Promise<SoloOutboxRecord[]> {
  return transaction('readonly', (store, complete) => {
    const request = store.index('scope').getAll(scope)
    request.onsuccess = () => complete(request.result.map(({ key: _key, ...record }) => record as SoloOutboxRecord))
  })
}

export async function saveSoloOutbox(input: SoloOutboxRecord): Promise<void> {
  // Freeze before the first await: editing the composer must not mutate a
  // saved request while IndexedDB opens. Transaction completion precedes POST.
  const record: SoloOutboxRecord = JSON.parse(JSON.stringify(input))
  const expected = record.kind === 'creation' ? record.request.clientRequestId : record.request.clientMessageId
  if (!record.id || expected !== record.id || !record.scope) throw new Error('Outbox request identity is invalid')
  await transaction<void>('readwrite', (store, complete, fail) => {
    const key = JSON.stringify([record.scope, record.id])
    const read = store.get(key)
    read.onsuccess = () => {
      if (read.result) {
        const { key: _key, ...previous } = read.result
        if (JSON.stringify(previous) !== JSON.stringify(record)) { fail(new Error('Saved request identity cannot be reused for changed content')); return }
        complete()
      } else if (record.kind === 'creation') {
        const pending = store.index('scope').get(record.scope)
        pending.onsuccess = () => {
          if (pending.result) { fail(new Error('Another creation request still needs confirmation')); return }
          store.add({ key, ...record })
          complete()
        }
      } else {
        store.add({ key, ...record })
        complete()
      }
    }
  })
}

export async function removeSoloOutbox(scope: string, id: string): Promise<void> {
  await transaction<void>('readwrite', (store, complete) => {
    store.delete(JSON.stringify([scope, id]))
    complete()
  })
}

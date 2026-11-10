import type { WorkspaceBindingRequest, WorkspaceCaptureRequest, WorkspaceRestoreRequest, WorkspaceScope } from '@/lib/api/clients/workspaceSnapshotsClient'

export type WorkspaceRequest =
  | { action: 'capture'; source: WorkspaceScope; body: WorkspaceCaptureRequest }
  | { action: 'restore'; body: WorkspaceRestoreRequest }
  | { action: 'activate' | 'return'; body: WorkspaceBindingRequest }

export interface SavedWorkspaceRequest {
  scope: string
  id: string
  request: WorkspaceRequest
  receiptId?: string
}

export function workspaceRequestScope(owner: string, sessionId: string) {
  if (!owner || !sessionId) throw new Error('Workspace requests require a signed-in owner and conversation')
  return JSON.stringify([owner, sessionId])
}

let opening: Promise<IDBDatabase> | undefined
function database(): Promise<IDBDatabase> {
  if (opening) return opening
  opening = new Promise<IDBDatabase>((resolve, reject) => {
    if (!globalThis.indexedDB) { reject(new Error('Browser storage is unavailable')); return }
    let blocked = false
    const request = indexedDB.open('grasp-workspace-requests-v1', 1)
    request.onupgradeneeded = () => { request.result.createObjectStore('requests', { keyPath: 'scope' }) }
    request.onerror = () => reject(request.error || new Error('Browser storage failed'))
    request.onblocked = () => { blocked = true; reject(new Error('Browser storage is blocked')) }
    request.onsuccess = () => {
      const db = request.result
      if (blocked) { db.close(); return }
      db.onversionchange = () => { db.close(); opening = undefined }
      resolve(db)
    }
  }).catch(error => { opening = undefined; throw error })
  return opening
}

async function transaction<T>(mode: IDBTransactionMode, run: (store: IDBObjectStore, finish: (value: T) => void, fail: (error: Error) => void) => void): Promise<T> {
  const db = await database()
  return new Promise<T>((resolve, reject) => {
    const tx = db.transaction('requests', mode)
    let value: T
    let error: Error | undefined
    tx.oncomplete = () => resolve(value)
    tx.onabort = () => reject(error || tx.error || new Error('Browser storage was interrupted'))
    tx.onerror = () => reject(error || tx.error || new Error('Browser storage failed'))
    run(tx.objectStore('requests'), result => { value = result }, cause => { error = cause; tx.abort() })
  })
}

export async function readWorkspaceRequest(scope: string): Promise<SavedWorkspaceRequest | null> {
  return transaction('readonly', (store, finish) => {
    const read = store.get(scope)
    read.onsuccess = () => finish(read.result || null)
  })
}

// A single pending operation per owner/chat is committed before POST. Editing
// selections or another tab cannot replace an uncertain operation's identity.
export async function saveWorkspaceRequest(input: SavedWorkspaceRequest): Promise<void> {
  const record: SavedWorkspaceRequest = JSON.parse(JSON.stringify(input))
  if (!record.scope || !record.id || record.id !== record.request.body.clientRequestId || record.receiptId) throw new Error('Invalid workspace request identity')
  await transaction<void>('readwrite', (store, finish, fail) => {
    const read = store.get(record.scope)
    read.onsuccess = () => {
      const previous: SavedWorkspaceRequest | undefined = read.result
      if (previous && (previous.id !== record.id || JSON.stringify(previous.request) !== JSON.stringify(record.request))) {
        fail(new Error('An earlier workspace request still needs confirmation'))
        return
      }
      if (!previous) store.add(record)
      finish()
    }
  })
}

export async function attachWorkspaceReceipt(scope: string, id: string, receiptId: string): Promise<void> {
  if (!receiptId) throw new Error('Missing workspace receipt')
  await transaction<void>('readwrite', (store, finish, fail) => {
    const read = store.get(scope)
    read.onsuccess = () => {
      const record: SavedWorkspaceRequest | undefined = read.result
      if (!record || record.id !== id || record.receiptId && record.receiptId !== receiptId) {
        fail(new Error('Workspace request receipt changed'))
        return
      }
      store.put({ ...record, receiptId })
      finish()
    }
  })
}

export async function removeWorkspaceRequest(scope: string, id: string): Promise<void> {
  await transaction<void>('readwrite', (store, finish) => {
    const read = store.get(scope)
    read.onsuccess = () => {
      if (read.result?.id === id) store.delete(scope)
      finish()
    }
  })
}

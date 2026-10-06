// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ATTACHMENTS_STORE,
  DRAFT_IDB_NAME,
  DRAFT_IDB_VERSION,
  HOME_DRAFT_ID,
  HOME_DRAFT_STORE,
  RUN_DRAFT_STORE,
  __resetDraftIdbForTests,
  __setDraftIdbBackendForTests,
  createMemoryDraftIdb,
  getDraftIdb,
} from './draftIdb'

type Row = Record<string, any>

function successfulRequest<T>(result: T) {
  const req: any = { result, error: null, onsuccess: null, onerror: null }
  queueMicrotask(() => req.onsuccess?.({ target: req }))
  return req
}

class FakeIndex {
  constructor(
    private rows: Map<IDBValidKey, Row>,
    private fail = false,
  ) {}

  getAllKeys([kind, id]: [string, string]) {
    const keys = [...this.rows.entries()]
      .filter(([, row]) => row.ownerKind === kind && row.ownerId === id)
      .map(([key]) => key)
    const req: any = { result: keys, error: this.fail ? new Error('keys failed') : null }
    queueMicrotask(() => (this.fail ? req.onerror?.() : req.onsuccess?.()))
    return req
  }

  getAll([kind, id]: [string, string]) {
    if (this.fail) {
      const req: any = { result: undefined, error: new Error('getAll failed') }
      queueMicrotask(() => req.onerror?.())
      return req
    }
    return successfulRequest(
      [...this.rows.values()].filter((row) => row.ownerKind === kind && row.ownerId === id),
    )
  }
}

class FakeStore {
  indexes = new Set<string>()

  constructor(
    private rows: Map<IDBValidKey, Row>,
    private keyPath: string,
    private failIndex = false,
  ) {}

  createIndex(name: string) {
    this.indexes.add(name)
    return new FakeIndex(this.rows)
  }

  index() {
    return new FakeIndex(this.rows, this.failIndex)
  }

  put(row: Row) {
    this.rows.set(row[this.keyPath], row)
    return successfulRequest(row[this.keyPath])
  }

  get(key: IDBValidKey) {
    return successfulRequest(this.rows.get(key))
  }

  delete(key: IDBValidKey) {
    this.rows.delete(key)
    return successfulRequest(undefined)
  }
}

class FakeTransaction {
  oncomplete: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  error: Error | null = null
  aborted = false

  constructor(
    private db: FakeDb,
    private mode: 'complete' | 'error' | 'abort' = 'complete',
  ) {
    setTimeout(() => {
      if (this.aborted) return
      if (mode === 'error') {
        this.error = new Error('transaction failed')
        this.onerror?.()
      } else if (mode === 'abort') {
        this.error = new Error('transaction aborted')
        this.onabort?.()
      } else {
        this.oncomplete?.()
      }
    }, 1)
  }

  objectStore(name: string) {
    if (this.db.throwStore === name) throw new Error('store operation failed')
    return this.db.stores.get(name)!
  }

  abort() {
    this.aborted = true
    this.onabort?.()
  }
}

class FakeDb {
  stores = new Map<string, FakeStore>()
  objectStoreNames = { contains: (name: string) => this.stores.has(name) }
  onversionchange: (() => void) | null = null
  close = vi.fn()
  txMode: 'complete' | 'error' | 'abort' = 'complete'
  throwStore = ''
  failAttachmentIndex = false

  createObjectStore(name: string, opts: { keyPath: string }) {
    const store = new FakeStore(new Map(), opts.keyPath)
    this.stores.set(name, store)
    return store
  }

  transaction(names: string | string[], _mode: IDBTransactionMode) {
    if (this.failAttachmentIndex && this.stores.has(ATTACHMENTS_STORE)) {
      const old = this.stores.get(ATTACHMENTS_STORE)!
      ;(old as any).failIndex = true
    }
    expect(Array.isArray(names) ? names.length : names).toBeTruthy()
    return new FakeTransaction(this, this.txMode) as unknown as IDBTransaction
  }
}

function installOpen(db: FakeDb, mode: 'success' | 'error' | 'throw' = 'success') {
  const open = vi.fn((_name: string, _version?: number) => {
    if (mode === 'throw') throw new Error('blocked')
    const req: any = { result: db, error: mode === 'error' ? new Error('open failed') : null }
    queueMicrotask(() => {
      if (mode === 'error') {
        req.onerror?.()
      } else {
        req.onupgradeneeded?.()
        req.onsuccess?.()
      }
    })
    return req
  })
  vi.stubGlobal('indexedDB', { open, deleteDatabase: vi.fn() })
  return open
}

function primaryOpenCount(open: ReturnType<typeof vi.fn>) {
  return open.mock.calls.filter((c) => c[0] === DRAFT_IDB_NAME).length
}

const homeRecord = {
  id: HOME_DRAFT_ID,
  schemaVersion: '1',
  savedAt: 1,
  pipelineId: 'pipe',
  text: 'saved text',
}

const attachment = (id: string, sortIndex: number) => ({
  id,
  ownerKind: 'home' as const,
  ownerId: HOME_DRAFT_ID,
  mimeType: 'text/plain',
  data: new Blob([id]),
  sortIndex,
})

describe('draftIdb native coverage', () => {
  afterEach(() => {
    __resetDraftIdbForTests()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('persists, replaces, sorts, and deletes home and run snapshots natively', async () => {
    const db = new FakeDb()
    const open = installOpen(db)
    const backend = getDraftIdb()

    await backend.putHome(homeRecord, [attachment('later', 2), attachment('first', 0)])
    const home = await backend.getHome()
    expect(home?.record.text).toBe('saved text')
    expect(home?.attachments.map((a) => a.id)).toEqual(['first', 'later'])

    await backend.putHome({ ...homeRecord, text: 'replacement' }, [attachment('only', 1)])
    expect((await backend.getHome())?.attachments.map((a) => a.id)).toEqual(['only'])
    await backend.deleteHome()
    expect(await backend.getHome()).toBeNull()

    const runAtt = {
      ...attachment('run-att', 0),
      ownerKind: 'run' as const,
      ownerId: 'wf-1',
    }
    await backend.putRun({ workflowId: 'wf-1', savedAt: 2, inputsJson: '{"a":1}' }, [runAtt])
    expect((await backend.getRun('wf-1'))?.attachments[0]?.id).toBe('run-att')
    await backend.deleteRun('wf-1')
    expect(await backend.getRun('wf-1')).toBeNull()

    expect(open).toHaveBeenCalledWith(DRAFT_IDB_NAME, DRAFT_IDB_VERSION)
    expect(db.stores.has(HOME_DRAFT_STORE)).toBe(true)
    expect(db.stores.has(RUN_DRAFT_STORE)).toBe(true)
    expect(db.stores.has(ATTACHMENTS_STORE)).toBe(true)
  })

  it('reuses the open promise and closes/reset it on version changes', async () => {
    const db = new FakeDb()
    const open = installOpen(db)
    const backend = getDraftIdb()
    expect(await backend.getHome()).toBeNull()
    expect(await backend.getRun('none')).toBeNull()
    expect(primaryOpenCount(open)).toBe(1)
    db.onversionchange?.()
    expect(db.close).toHaveBeenCalled()
    expect(await backend.getHome()).toBeNull()
    expect(primaryOpenCount(open)).toBe(2)

    db.close.mockImplementationOnce(() => {
      throw new Error('already closed')
    })
    db.onversionchange?.()
    expect(await backend.getHome()).toBeNull()
  })

  it('handles missing, throwing, and failed IndexedDB opens', async () => {
    vi.stubGlobal('indexedDB', undefined)
    expect(await getDraftIdb().getHome()).toBeNull()
    await expect(getDraftIdb().putHome(homeRecord, [])).rejects.toThrow('IndexedDB unavailable')

    __resetDraftIdbForTests()
    installOpen(new FakeDb(), 'throw')
    expect(await getDraftIdb().getRun('x')).toBeNull()

    __resetDraftIdbForTests()
    installOpen(new FakeDb(), 'error')
    await expect(
      getDraftIdb().putRun({ workflowId: 'x', savedAt: 1, inputsJson: '{}' }, []),
    ).rejects.toThrow('IndexedDB unavailable')
    await expect(getDraftIdb().deleteHome()).resolves.toBeUndefined()
    await expect(getDraftIdb().deleteRun('x')).resolves.toBeUndefined()
  })

  it('rejects key lookup, synchronous store, transaction error, and abort failures', async () => {
    const db = new FakeDb()
    installOpen(db)
    const backend = getDraftIdb()
    await backend.putHome(homeRecord, [])

    ;(db.stores.get(ATTACHMENTS_STORE) as any).failIndex = true
    await expect(backend.putHome(homeRecord, [])).rejects.toThrow('keys failed')

    ;(db.stores.get(ATTACHMENTS_STORE) as any).failIndex = false
    db.throwStore = HOME_DRAFT_STORE
    await expect(backend.putHome(homeRecord, [])).rejects.toThrow('store operation failed')
    db.throwStore = ''

    db.txMode = 'error'
    await expect(backend.putRun({ workflowId: 'x', savedAt: 1, inputsJson: '{}' }, [])).rejects.toThrow(
      'transaction failed',
    )
    db.txMode = 'abort'
    await expect(backend.deleteRun('x')).rejects.toThrow('transaction aborted')
  })

  it('keeps memory snapshots isolated and honors an injected backend', async () => {
    const mem = createMemoryDraftIdb()
    const input = { ...homeRecord }
    const att = attachment('a', 0)
    await mem.putHome(input, [att])
    input.text = 'mutated'
    att.sortIndex = 9
    const loaded = await mem.getHome()
    expect(loaded?.record.text).toBe('saved text')
    expect(loaded?.attachments[0]?.sortIndex).toBe(0)
    loaded!.record.text = 'changed output'
    expect((await mem.getHome())?.record.text).toBe('saved text')

    const custom = { ...mem, getHome: vi.fn(async () => null) }
    __setDraftIdbBackendForTests(custom)
    expect(getDraftIdb()).toBe(custom)
    await getDraftIdb().getHome()
    expect(custom.getHome).toHaveBeenCalled()
  })
})

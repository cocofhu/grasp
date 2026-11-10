// Vitest 4: vi.spyOn() requires an existing function. happy-dom's Window
// does not implement window.confirm, so provide a no-op default the tests can spy on.
if (typeof window !== 'undefined' && typeof window.confirm !== 'function') {
  window.confirm = () => true
}
import 'fake-indexeddb/auto'
import { beforeEach } from 'vitest'

// Exercise the actual outbox transactions. Every test starts without another
// test's private drafts; production never falls back to in-memory storage.
beforeEach(async () => {
  if (!(await indexedDB.databases()).some(database => database.name === 'grasp-solo-outbox-v1')) return
  await new Promise<void>((resolve, reject) => {
    const request = indexedDB.open('grasp-solo-outbox-v1')
    request.onerror = () => reject(request.error)
    request.onsuccess = () => {
      const db = request.result
      if (!db.objectStoreNames.contains('requests')) { db.close(); resolve(); return }
      const tx = db.transaction('requests', 'readwrite')
      tx.objectStore('requests').clear()
      tx.oncomplete = () => { db.close(); resolve() }
      tx.onerror = () => { db.close(); reject(tx.error) }
    }
  })
})

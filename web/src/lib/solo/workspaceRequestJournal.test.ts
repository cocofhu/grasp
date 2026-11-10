import 'fake-indexeddb/auto'
import { describe, expect, it } from 'vitest'
import { attachWorkspaceReceipt, readWorkspaceRequest, removeWorkspaceRequest, saveWorkspaceRequest, workspaceRequestScope, type SavedWorkspaceRequest } from './workspaceRequestJournal'

let serial = 0
function record(owner = 'alice', session = `chat-${++serial}`): SavedWorkspaceRequest {
  return { scope: workspaceRequestScope(owner, session), id: `request-${serial}`, request: { action: 'restore', body: { clientRequestId: `request-${serial}`, snapshotId: 'original-snapshot', expectedBinding: 'original-binding', durationMillis: 60000 } } }
}

describe('workspace request journal', () => {
  it('freezes the complete original request before storage opens', async () => {
    const input = record(), original = structuredClone(input)
    const saved = saveWorkspaceRequest(input)
    input.request.body.expectedBinding = 'later-workspace'
    await saved
    expect(await readWorkspaceRequest(original.scope)).toEqual(original)
    await expect(saveWorkspaceRequest(input)).rejects.toThrow('earlier')
    expect(await readWorkspaceRequest(original.scope)).toEqual(original)
  })

  it('atomically accepts only one pending request across concurrent drawers', async () => {
    const first = record(), second = record()
    second.scope = first.scope
    const results = await Promise.allSettled([saveWorkspaceRequest(first), saveWorkspaceRequest(second)])
    expect(results.filter(row => row.status === 'fulfilled')).toHaveLength(1)
    const saved = await readWorkspaceRequest(first.scope)
    expect([first.id, second.id]).toContain(saved?.id)
  })

  it('isolates users and chats and prevents late receipt or cleanup from replacing a new request', async () => {
    const first = record(), other = record('bob')
    await saveWorkspaceRequest(first)
    await saveWorkspaceRequest(other)
    await attachWorkspaceReceipt(first.scope, first.id, 'original-receipt')
    await expect(attachWorkspaceReceipt(first.scope, first.id, 'different-receipt')).rejects.toThrow('changed')
    await removeWorkspaceRequest(first.scope, first.id)
    const next = record(); next.scope = first.scope
    await saveWorkspaceRequest(next)
    await removeWorkspaceRequest(first.scope, first.id)
    await expect(attachWorkspaceReceipt(first.scope, first.id, 'late-receipt')).rejects.toThrow('changed')
    expect(await readWorkspaceRequest(first.scope)).toEqual(next)
    expect(await readWorkspaceRequest(other.scope)).toEqual(other)
  })

  it('rejects incomplete identity before any write', async () => {
    expect(() => workspaceRequestScope('', 'chat')).toThrow('owner')
    const input = record(); input.request.body.clientRequestId = 'different-request'
    await expect(saveWorkspaceRequest(input)).rejects.toThrow('identity')
    expect(await readWorkspaceRequest(input.scope)).toBeNull()
  })
})

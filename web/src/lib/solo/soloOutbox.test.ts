import { describe, expect, it } from 'vitest'
import { newSoloRequestID, readSoloOutbox, removeSoloOutbox, saveSoloOutbox, soloOutboxScope, type SoloCreationOutbox, type SoloMessageOutbox } from './soloOutbox'

function creation(principal = 'alice', id = newSoloRequestID()): SoloCreationOutbox {
  return { scope: soloOutboxScope(principal, 'creation'), id, kind: 'creation', request: { clientRequestId: id, agentName: 'Solo', text: 'original command' }, createdAt: '2026-11-09T00:00:00Z' }
}

describe('Solo browser outbox transactions', () => {
  it('freezes the complete request before asynchronous storage and reads it back', async () => {
    const record = creation()
    record.request.images = [{ mimeType: 'image/png', data: 'large-inline-image', name: 'image.png' }]
    record.request.repos = [{ projectId: 'project-a', branch: 'feature-a' }]
    const saved = saveSoloOutbox(record)
    record.request.text = 'edited after click'
    record.request.repos[0]!.branch = 'another branch'
    await saved
    const [restored] = await readSoloOutbox(record.scope)
    expect(restored).toMatchObject({ request: { text: 'original command', images: record.request.images, repos: [{ projectId: 'project-a', branch: 'feature-a' }] } })
    await saveSoloOutbox(restored!)
    await expect(saveSoloOutbox(record)).rejects.toThrow('changed content')
  })

  it('allows only one concurrent creation per owner while preserving other accounts', async () => {
    const a = creation(), b = creation()
    const results = await Promise.allSettled([saveSoloOutbox(a), saveSoloOutbox(b)])
    expect(results.filter(result => result.status === 'fulfilled')).toHaveLength(1)
    expect(await readSoloOutbox(a.scope)).toHaveLength(1)
    const other = creation('bob', a.id)
    await saveSoloOutbox(other)
    expect(await readSoloOutbox(other.scope)).toEqual([other])
    await removeSoloOutbox(a.scope, a.id)
    expect(await readSoloOutbox(other.scope)).toEqual([other])
  })

  it('scopes immutable message identities by account and canonical conversation', async () => {
    const id = newSoloRequestID()
    const a: SoloMessageOutbox = { scope: soloOutboxScope('alice', 'message', 'a'), id, kind: 'message', request: { clientMessageId: id, text: 'same text', references: [{ kind: 'solo', target: 'another-conversation' }] }, createdAt: '2026-11-09T00:00:00Z' }
    const b: SoloMessageOutbox = { ...a, scope: soloOutboxScope('alice', 'message', 'b') }
    await Promise.all([saveSoloOutbox(a), saveSoloOutbox(b)])
    expect(await readSoloOutbox(soloOutboxScope('bob', 'message', 'a'))).toEqual([])
    await removeSoloOutbox(a.scope, a.id)
    expect(await readSoloOutbox(a.scope)).toEqual([])
    expect(await readSoloOutbox(b.scope)).toEqual([b])
  })

  it('rejects mismatched identities before writing any request', async () => {
    const record = creation()
    record.request.clientRequestId = 'another-identity'
    await expect(saveSoloOutbox(record)).rejects.toThrow('identity')
    expect(await readSoloOutbox(record.scope)).toEqual([])
  })
})

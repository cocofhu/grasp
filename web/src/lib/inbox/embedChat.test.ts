// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  EMBED_PICK_MESSAGE,
  clearEmbedSession,
  embedChatPath,
  loadEmbedSession,
  parseEmbedPickMessage,
  parseEmbedTicketFromHash,
  redeemEmbedTicket,
  saveEmbedSession,
} from './embedChat'

beforeEach(() => sessionStorage.clear())
afterEach(() => vi.unstubAllGlobals())

describe('embedChat', () => {
  it('builds the drawer path and reads the ticket from the fragment', () => {
    expect(embedChatPath('run-1', 'n/1')).toBe('/embed/runs/run-1/nodes/n%2F1/chat')
    expect(parseEmbedTicketFromHash('#ticket=abc')).toBe('abc')
    expect(parseEmbedTicketFromHash('#t=abc')).toBe('')
    expect(parseEmbedTicketFromHash('')).toBe('')
  })

  it('keeps the drawer token per run/node and drops it once expired', () => {
    const now = Date.parse('2026-09-01T00:00:00Z')
    saveEmbedSession('r', 'n', { token: 'gse_x', expiresAt: '2026-09-01T01:00:00Z' })
    expect(loadEmbedSession('r', 'n', now)?.token).toBe('gse_x')
    expect(loadEmbedSession('r', 'other', now)).toBeNull()
    expect(loadEmbedSession('r', 'n', Date.parse('2026-09-01T02:00:00Z'))).toBeNull()
    expect(sessionStorage.length).toBe(0)

    saveEmbedSession('r', 'n', { token: 'gse_y', expiresAt: '2026-09-01T01:00:00Z' })
    clearEmbedSession('r', 'n')
    expect(loadEmbedSession('r', 'n', now)).toBeNull()
  })

  it('redeems with the embed header and rejects a session for another node', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ token: 'gse_z', runId: 'r', nodeId: 'n', expiresAt: '2026-09-01T12:00:00Z' })),
    )
    vi.stubGlobal('fetch', fetchMock)
    await expect(redeemEmbedTicket('tk', 'r', 'n')).resolves.toEqual({ token: 'gse_z', expiresAt: '2026-09-01T12:00:00Z' })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/embed-api/session')
    expect(init.credentials).toBe('omit')
    expect(init.headers['X-Grasp-Embed']).toBe('1')
    expect(JSON.parse(init.body)).toEqual({ ticket: 'tk' })

    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ token: 'gse_z', runId: 'r', nodeId: 'other', expiresAt: '2026-09-01T12:00:00Z' })),
    )
    await expect(redeemEmbedTicket('tk', 'r', 'n')).resolves.toBeNull()
    fetchMock.mockResolvedValueOnce(new Response('{}', { status: 401 }))
    await expect(redeemEmbedTicket('tk', 'r', 'n')).resolves.toBeNull()
    fetchMock.mockResolvedValueOnce(new Response('{}', { status: 502 }))
    await expect(redeemEmbedTicket('tk', 'r', 'n')).rejects.toThrow('502')
  })

  it('accepts only well-formed pick messages', () => {
    expect(parseEmbedPickMessage({ type: EMBED_PICK_MESSAGE, payload: { selector: 'a.x', tagName: 'A', outerHTML: '<a>', url: 'http://h/p', extra: 1 } })).toEqual({
      selector: 'a.x',
      tagName: 'A',
      outerHTML: '<a>',
      text: undefined,
      url: 'http://h/p',
    })
    expect(parseEmbedPickMessage({ type: EMBED_PICK_MESSAGE, payload: { selector: ' ' } })).toBeNull()
    expect(parseEmbedPickMessage({ type: 'other', payload: { selector: 'a' } })).toBeNull()
    expect(parseEmbedPickMessage('grasp-embed:pick')).toBeNull()
  })
})

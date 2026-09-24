// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  canCreateGateShare,
  formatRemainingSec,
  inboxShareKind,
  isLoopbackShareHost,
  isShareableInboxItem,
  mergePublicGatePreview,
  normalizePermissionPreset,
  parseShareTokenFromHash,
  publicGateApi,
  publicGateContentKey,
  publicPreviewVncWsUrl,
  recallShareUrl,
  rememberShareUrl,
  forgetShareUrl,
  remainingSecFromExpiresAt,
  shareApiErrorMessage,
  shareStatusLabel,
} from './gateShareLink'

const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
const t = (key: string, values?: Record<string, unknown>) => `${key}:${values?.n ?? values?.remaining ?? ''}`

describe('gateShareLink additional coverage', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    fetchMock.mockResolvedValue(response({ status: 'ok' }))
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('covers normalization, invalid inputs, status fallbacks, and error messages', () => {
    expect(normalizePermissionPreset('react_only')).toBe('react_only')
    expect(normalizePermissionPreset('unknown')).toBe('full')
    expect(isShareableInboxItem(null)).toBe(false)
    expect(isShareableInboxItem({ type: 'gate', nodeType: 'other' } as never)).toBe(false)
    expect(inboxShareKind(undefined)).toBe('human_gate')
    expect(isLoopbackShareHost('')).toBe(false)
    expect(isLoopbackShareHost('not a valid host %')).toBe(false)
    expect(parseShareTokenFromHash('t=%20token%20&x=1')).toBe('token')
    expect(canCreateGateShare()).toBe(true)
    expect(canCreateGateShare({ state: 'active', canCreate: true })).toBe(true)
    expect(canCreateGateShare({ state: 'active' })).toBe(false)
    expect(canCreateGateShare({ state: 'none' })).toBe(true)
    expect(remainingSecFromExpiresAt('not-a-date', -4)).toBe(0)
    expect(remainingSecFromExpiresAt(undefined, undefined)).toBeUndefined()
    expect(formatRemainingSec(undefined, t)).toContain('expired')
    expect(formatRemainingSec(30, t)).toContain('remainingMinutes:1')
    expect(shareStatusLabel({ state: 'revoked' }, t)).toContain('stateRevoked')
    expect(shareStatusLabel({ state: 'expired' }, t)).toContain('stateExpired')
    expect(shareApiErrorMessage('permission_denied', t)).toContain('permissionDenied')
    expect(shareApiErrorMessage('', t)).toContain('shareFailed')
    expect(shareApiErrorMessage(new Error('backend detail'), t)).toBe('backend detail')
  })

  it('tolerates unavailable session storage and ignores blank remembered URLs', () => {
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota')
    })
    rememberShareUrl('coverage-run', 'blank', undefined, ' ')
    expect(set).not.toHaveBeenCalled()
    rememberShareUrl('coverage-run', 'node', undefined, ' https://example.test/#t=x ')
    expect(recallShareUrl('coverage-run', 'node')).toBe('https://example.test/#t=x')

    forgetShareUrl('coverage-run', 'node')
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('private')
    })
    expect(recallShareUrl('coverage-run', 'missing')).toBe('')
    get.mockRestore()
    const remove = vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
      throw new Error('private')
    })
    expect(() => forgetShareUrl('coverage-run', 'missing')).not.toThrow()
    remove.mockRestore()
  })

  it('merges changed sparse hashes, explicit bodies, idle pointers, and initial previews', () => {
    const initial = { status: 'active', nonce: 'n' }
    expect(mergePublicGatePreview(null, initial)).toBe(initial)

    const prev = {
      status: 'active',
      visualHtml: 'old',
      visualHtmlHash: 'v1',
      upstream: { text: 'old' },
      upstreamHash: 'u1',
      structured: { title: 'old' },
      structuredHash: 's1',
      turns: [{ role: 'agent', text: 'old' }],
      turnsHash: 't1',
      nonce: 'keep',
      sessionBusy: true,
      activeItem: { id: 'active' },
    }
    const changed = mergePublicGatePreview(prev, {
      status: 'active',
      visualHtmlHash: 'v2',
      upstreamHash: 'u2',
      structuredHash: 's2',
      turnsHash: 't2',
      sessionBusy: false,
      activeItem: null,
    })
    expect(changed.visualHtml).toBeUndefined()
    expect(changed.upstream).toBeUndefined()
    expect(changed.structured).toBeUndefined()
    expect(changed.turns).toBeUndefined()
    expect(changed.nonce).toBe('keep')
    expect(changed.activeItem).toBeUndefined()

    const explicit = mergePublicGatePreview(prev, {
      status: 'active',
      visualHtml: '',
      upstream: null,
      structured: undefined,
      turns: [],
      sessionBusy: true,
    })
    expect(explicit).toMatchObject({ visualHtml: '', upstream: null, turns: [] })
    expect(publicGateContentKey(null)).toBe('')
    expect(publicGateContentKey({ status: 'active', visualHtml: 'body' })).toContain('body')
  })

  it('builds websocket, preview, and upstream requests and reports failures', async () => {
    expect(publicGateApi.eventsWsUrl()).toBe('ws://localhost:3000/public/gate-approvals/events')
    const signal = new AbortController().signal
    fetchMock.mockResolvedValueOnce(response({ status: 'active' }))
    await publicGateApi.preview('token', signal, {
      visualHtmlHash: ' vh ',
      upstreamHash: ' uh ',
      structuredHash: ' sh ',
      turnsHash: ' th ',
      silent: true,
      issueNonce: true,
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/public/gate-approvals/preview', {
      method: 'GET',
      credentials: 'omit',
      signal,
      headers: {
        'X-Gate-Share-Token': 'token',
        'X-Gate-Share-Requested': '1',
        'X-Gate-Known-Visual-Html-Hash': 'vh',
        'X-Gate-Known-Upstream-Hash': 'uh',
        'X-Gate-Known-Structured-Hash': 'sh',
        'X-Gate-Known-Turns-Hash': 'th',
        'X-Gate-Silent-Poll': '1',
        'X-Gate-Issue-Nonce': '1',
      },
    })

    fetchMock.mockResolvedValueOnce(response({ error: 'gone' }, 404))
    await expect(publicGateApi.preview('token')).rejects.toMatchObject({ message: 'gone', status: 404 })
    fetchMock.mockResolvedValueOnce(new Response('invalid', { status: 500 }))
    await expect(publicGateApi.preview('token')).rejects.toThrow('500')

    fetchMock.mockResolvedValueOnce(response({ status: 'ok', upstream: { text: 'x' } }))
    await publicGateApi.upstream('token', signal)
    expect(fetchMock).toHaveBeenLastCalledWith('/public/gate-approvals/upstream', expect.objectContaining({
      method: 'GET',
      signal,
      headers: { 'X-Gate-Share-Token': 'token', 'X-Gate-Share-Requested': '1' },
    }))
    fetchMock.mockResolvedValueOnce(response({ message: 'upstream denied' }, 403))
    await expect(publicGateApi.upstream('token')).rejects.toThrow('upstream denied')
  })

  it('posts decisions, replies, cancel, and queue mutations with exact bodies', async () => {
    fetchMock.mockResolvedValueOnce(response({ action: 'approve' }, 409))
    await expect(publicGateApi.decide({ token: 't', action: 'approve', nonce: 'n' })).resolves.toMatchObject({
      status: 'used',
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/public/gate-approvals/decide', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ token: 't', action: 'approve', nonce: 'n' }),
    }))
    fetchMock.mockResolvedValueOnce(response({ message: 'bad nonce' }, 400))
    await expect(publicGateApi.decide({ token: 't', action: 'approve', nonce: 'bad' })).rejects.toThrow('bad nonce')

    const calls: Array<[() => Promise<unknown>, string, unknown]> = [
      [
        () => publicGateApi.reply({ token: 't', text: 'hello', images: [{ data: 'x' }] }),
        '/public/gate-approvals/reply',
        { token: 't', text: 'hello', images: [{ data: 'x' }] },
      ],
      [() => publicGateApi.cancel('t'), '/public/gate-approvals/cancel', { token: 't' }],
      [() => publicGateApi.queueRemove('t', 'q1'), '/public/gate-approvals/queue/remove', { token: 't', itemId: 'q1' }],
      [() => publicGateApi.queueReorder('t', ['q2', 'q1']), '/public/gate-approvals/queue/reorder', { token: 't', itemIds: ['q2', 'q1'] }],
    ]
    for (const [invoke, url, body] of calls) {
      fetchMock.mockResolvedValueOnce(response({ status: 'ok' }))
      await invoke()
      expect(fetchMock).toHaveBeenLastCalledWith(url, expect.objectContaining({
        method: 'POST',
        body: JSON.stringify(body),
      }))
      fetchMock.mockResolvedValueOnce(response({ error: `${url} failed` }, 400))
      await expect(invoke()).rejects.toThrow(`${url} failed`)
    }
  })

  it('loads artifacts and preview tickets with exact URLs and error branches', async () => {
    const signal = new AbortController().signal
    await publicGateApi.artifacts('token', signal)
    expect(fetchMock).toHaveBeenLastCalledWith('/public/gate-approvals/artifacts', expect.objectContaining({
      method: 'GET',
      signal,
    }))
    fetchMock.mockResolvedValueOnce(response({ message: 'artifacts denied' }, 403))
    await expect(publicGateApi.artifacts('token')).rejects.toThrow('artifacts denied')

    fetchMock.mockResolvedValueOnce(response({ content: 'hello' }))
    await publicGateApi.artifactContent('token', 'a / b.json', signal)
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/public/gate-approvals/artifacts/a%20%2F%20b.json/content',
      expect.objectContaining({ method: 'GET', signal }),
    )
    fetchMock.mockResolvedValueOnce(response({ error: 'missing' }, 404))
    await expect(publicGateApi.artifactContent('token', 'missing')).rejects.toThrow('missing')

    fetchMock.mockResolvedValueOnce(response({ ticket: 'ticket' }))
    await publicGateApi.previewTicket('token', 8080, undefined, signal)
    expect(fetchMock).toHaveBeenLastCalledWith('/public/gate-approvals/preview-ticket', expect.objectContaining({
      method: 'POST',
      signal,
      body: JSON.stringify({ port: 8080, purpose: 'vnc' }),
    }))
    fetchMock.mockResolvedValueOnce(response({ message: 'ticket denied' }, 403))
    await expect(publicGateApi.previewTicket('token', 8080, 'api')).rejects.toThrow('ticket denied')

    fetchMock.mockResolvedValueOnce(response({ status: 'active', ticket: 'et', runId: 'r', nodeId: 'n', expiresAt: '' }))
    await expect(publicGateApi.embedTicket('token')).resolves.toMatchObject({ ticket: 'et', runId: 'r' })
    fetchMock.mockResolvedValueOnce(response({ status: 'revoked' }))
    await expect(publicGateApi.embedTicket('token')).rejects.toThrow('revoked')

    expect(publicPreviewVncWsUrl('a b')).toContain('?ticket=a%20b')
    expect(publicPreviewVncWsUrl('x', '/custom/ws?mode=vnc')).toContain('/custom/ws?mode=vnc&ticket=x')
    expect(publicPreviewVncWsUrl('x', ' ')).toContain('/public/gate-approvals/preview-vnc/ws?ticket=x')
  })
})

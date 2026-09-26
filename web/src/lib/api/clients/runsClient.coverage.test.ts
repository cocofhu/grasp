// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiState } from '../httpCore'
import { runsClient } from './runsClient'

const ok = (body: unknown = { status: 'ok' }) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })

describe('runsClient request coverage', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    fetchMock.mockImplementation(async () => ok())
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => vi.unstubAllGlobals())

  it('builds filtered and paginated run lists, including validated sorting', async () => {
    const signal = new AbortController().signal
    await runsClient.listRuns()
    await runsClient.listRuns({
      status: 'running',
      tag: 'nightly',
      wf: 'wf 1',
      projectId: 'p1',
      page: 2,
      pageSize: 25,
      sort: 'priority',
      order: 'desc',
      signal,
    })
    await runsClient.listRuns({ sort: 'invalid', order: 'asc' })

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/runs', {
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/runs?status=running&tag=nightly&wf=wf+1&projectId=p1&page=2&pageSize=25&sort=priority&order=desc',
      expect.objectContaining({ signal }),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/runs', expect.not.objectContaining({ signal }))
  })

  it('builds start-run payload variants and core run actions', async () => {
    const signal = new AbortController().signal
    await runsClient.getRun('r1')
    await runsClient.runArtifacts('r1')
    await runsClient.inboxContext('r1', 'node / 1', 3, { signal })
    await runsClient.listProjectRunTags('project / 1')
    await runsClient.startRun('wf1', { topic: 'x' }, 'manual', 'high', ['tag'], {
      signal,
      env: [{ key: 'K', value: 'V' }],
      title: '  Nightly  ',
      firstMessage: { text: ' hello ', images: [] },
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/workflows/wf1/runs', expect.objectContaining({
      method: 'POST',
      signal,
      body: JSON.stringify({
        inputs: { topic: 'x' },
        trigger: 'manual',
        priority: 'high',
        tags: ['tag'],
        env: [{ key: 'K', value: 'V' }],
        title: 'Nightly',
        firstMessage: { text: 'hello', images: [] },
      }),
    }))

    await runsClient.startRun('wf2', {}, 'manual', 'normal', [], {
      env: [],
      title: ' ',
      firstMessage: { text: ' ', images: [{ data: 'x' }] as never },
    })
    expect(JSON.parse(fetchMock.mock.calls.at(-1)![1].body)).toMatchObject({
      firstMessage: { text: '', images: [{ data: 'x' }] },
    })
    await runsClient.startRun('wf3', {}, 'manual', 'normal', [], { firstMessage: { text: ' ' } })
    expect(JSON.parse(fetchMock.mock.calls.at(-1)![1].body)).not.toHaveProperty('firstMessage')

    await runsClient.updateRunPriority('r1', 'high')
    await runsClient.cancelRun('r1')
    await runsClient.deleteRun('r1')
    await runsClient.resumeRun('r1')
    await runsClient.resumeGate('r1', 'n1', 'approve', { note: 'ok' })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/runs/r1/gates/n1/resume', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ action: 'approve', form: { note: 'ok' } }),
    }))
    expect(runsClient.runEventsWsUrl('r1')).toContain('/api/runs/r1/events')
    expect(runsClient.exportRunLogsUrl('r1')).toContain('/api/runs/r1/logs/export')
  })

  it('issues all share, artifact, react, queue, and preview requests exactly', async () => {
    await runsClient.createGateShareLink('r', 'n', '8h', 'react_only')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/runs/r/gates/n/share-link', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ ttlTier: '8h', permissionPreset: 'react_only' }),
    }))
    await runsClient.getGateShareLink('r', 'n')
    await runsClient.regenGateShareLink('r', 'n')
    await runsClient.revokeGateShareLink('r', 'n')
    await runsClient.createReviewShareLink('r', 'n')
    await runsClient.getReviewShareLink('r', 'n')
    await runsClient.regenReviewShareLink('r', 'n')
    await runsClient.revokeReviewShareLink('r', 'n')
    await runsClient.listGatePrimaryArtifacts('r', 'n')
    await runsClient.saveGateArtifact('r', 'n', 'report / final.json', '{}', 'etag-1')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/runs/r/gates/n/artifacts/report%20%2F%20final.json',
      expect.objectContaining({
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'If-Match': 'etag-1' },
        body: JSON.stringify({ content: '{}' }),
      }),
    )
    await runsClient.saveGateArtifact('r', 'n', 'x.txt', 'x')
    expect(fetchMock.mock.calls.at(-1)![1].headers).toEqual({ 'Content-Type': 'application/json' })
    const annotation = { annotations: [{ seq: 1, selector: '#x', comment: 'fix', screenshot: 'MISSING' }] } as never
    await runsClient.saveAnnotationArtifact('r', 'n', annotation)
    await runsClient.reactReply('r', 'n', 'text', [], true, [{ selector: '#x' }])
    await runsClient.reactReply('r', 'n', '', [], false, [], true)
    expect(JSON.parse(String(fetchMock.mock.calls.at(-1)![1].body))).toEqual({
      text: '',
      images: [],
      force: false,
      annotations: [],
      retryLast: true,
      abortRunning: false,
    })
    await runsClient.reactCancel('r', 'n')
    await runsClient.reactQueueRemove('r', 'n', 'q1')
    await runsClient.reactQueueReorder('r', 'n', ['q2', 'q1'])
    await runsClient.gateReactRevise('r', 'n', 'revise')
    await runsClient.gateReactCancel('r', 'n')
    await runsClient.gateReactQueueRemove('r', 'n', 'q1')
    await runsClient.gateReactQueueReorder('r', 'n', ['q1'])
    await runsClient.nodePreviews('r', 'n')
    await runsClient.listPreviewIssues('r', 'n')
    await runsClient.createPreviewIssue('r', 'n', 'broken', '#x', 8080, [])
    await runsClient.deletePreviewIssue('r', 'n', 'i1')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/runs/r/nodes/n/preview-issues/i1', expect.objectContaining({
      method: 'DELETE',
    }))
  })

  it('builds node event and sandbox requests and maps sandbox responses', async () => {
    const signal = new AbortController().signal
    await runsClient.nodeEvents('r', 'n')
    await runsClient.nodeEvents('r', 'n', { cursor: 'c 1', limit: 50, signal })
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/runs/r/nodes/n/events?cursor=c+1&limit=50',
      expect.objectContaining({ signal }),
    )
    await runsClient.nodeSandboxLog('r', 'n', { signal })
    await runsClient.nodePreviews('r', 'n', { signal })

    fetchMock.mockResolvedValueOnce(new Response('', { status: 404 }))
    await expect(runsClient.getRunNodeSandbox('r', 'n')).resolves.toBeNull()

    fetchMock.mockResolvedValueOnce(ok({ id: 'sandbox-1' }))
    await expect(runsClient.getRunNodeSandbox('r', 'n')).resolves.toEqual({ id: 'sandbox-1' })
    expect(apiState.checked).toBe(true)
    expect(apiState.online).toBe(true)

    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'sandbox unavailable' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await expect(runsClient.getRunNodeSandbox('r', 'n')).rejects.toThrow('sandbox unavailable')
    fetchMock.mockResolvedValueOnce(new Response('gateway', { status: 502 }))
    await expect(runsClient.getRunNodeSandbox('r', 'n')).rejects.toThrow('502 /runs/r/nodes/n/sandbox')
  })
})

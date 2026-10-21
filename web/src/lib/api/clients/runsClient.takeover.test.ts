// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { runsClient } from './runsClient'

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('runsClient takeover + last_activity sort', () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('passes sort=last_activity through the whitelist', async () => {
    fetchMock.mockResolvedValue(json({ items: [], total: 0 }))
    await runsClient.listRuns({ sort: 'last_activity', order: 'desc', pageSize: 8 })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/runs?pageSize=8&sort=last_activity&order=desc')
  })

  it('POSTs takeover with the iteration and returns the result', async () => {
    fetchMock.mockResolvedValue(json({ runId: 'r1', nodeId: 'impl', iteration: 2, mode: 'manual', state: 'stopping', status: 'taken_over' }))
    const res = await runsClient.takeoverNode('r1', 'impl', 2)
    expect(fetchMock).toHaveBeenCalledWith('/api/runs/r1/nodes/impl/takeover', expect.objectContaining({ method: 'POST', body: JSON.stringify({ iteration: 2 }) }))
    expect(res.state).toBe('stopping')
  })

  it('surfaces the 409 code as the error message', async () => {
    fetchMock.mockResolvedValue(json({ error: 'node_finishing', message: 'node is finishing' }, 409))
    await expect(runsClient.takeoverNode('r1', 'impl')).rejects.toThrow('node_finishing')
    expect(fetchMock.mock.calls[0][1].body).toBe('{}')
  })

  it('keeps the 409 current (run_not_running carries the re-read run status)', async () => {
    fetchMock.mockResolvedValue(json({ error: 'run_not_running', message: '运行未在执行中: cancelled', current: { runStatus: 'cancelled', nodeId: 'impl' } }, 409))
    const err = await runsClient.takeoverNode('r1', 'impl', 1).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(Error)
    expect((err as Error).message).toBe('run_not_running')
    expect(err).toMatchObject({ status: 409, current: { runStatus: 'cancelled', nodeId: 'impl' } })
  })

  it('a duplicate takeover answers 200 already_manual and resolves like success', async () => {
    fetchMock.mockResolvedValue(json({ runId: 'r1', nodeId: 'impl', iteration: 1, mode: 'manual', state: 'waiting_human', status: 'already_manual' }))
    await expect(runsClient.takeoverNode('r1', 'impl', 1)).resolves.toMatchObject({ status: 'already_manual', state: 'waiting_human' })
  })
})

// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'

afterEach(() => { vi.unstubAllGlobals(); window.localStorage.clear() })
describe('model directory save notifications', () => {
  it('refreshes this page and deduplicates broadcast plus storage delivery', async () => {
    vi.resetModules()
    let receiver: ((event: { data: string }) => void) | undefined
    const notices: string[] = []
    vi.stubGlobal('BroadcastChannel', class {
      set onmessage(value: (event: { data: string }) => void) { receiver = value }
      postMessage(value: string) { notices.push(value) }
    })
    const { modelCatalogRevision, invalidateModelCatalog } = await import('./modelCatalogEvents')
    invalidateModelCatalog()
    expect(modelCatalogRevision.value).toBe(1)
    expect(notices).toHaveLength(1)
    receiver?.({ data: 'other-tab:save-2' })
    window.dispatchEvent(new StorageEvent('storage', { key: 'grasp:model-catalog', newValue: 'other-tab:save-2' }))
    expect(modelCatalogRevision.value).toBe(2)
    expect(window.localStorage.getItem('grasp:model-catalog')).toBe(notices[0])
  })
  it('keeps successful saves usable when browser persistence and channels are restricted', async () => {
    vi.resetModules()
    vi.stubGlobal('BroadcastChannel', class { constructor() { throw new Error('restricted') } })
    const { modelCatalogRevision, invalidateModelCatalog } = await import('./modelCatalogEvents')
    expect(() => invalidateModelCatalog()).not.toThrow()
    expect(modelCatalogRevision.value).toBe(1)
  })
  it('invalidates metadata and binding writes while leaving workspace file editing alone', async () => {
    vi.resetModules()
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ status: 'ok' }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const { modelCatalogRevision } = await import('./modelCatalogEvents')
    const { req } = await import('./httpCore')
    await req('/agents/example/workspace/fs/path', { method: 'PUT', body: '{}' })
    await req('/workspace/shared-agent-config/test', { method: 'POST', body: '{}' })
    expect(modelCatalogRevision.value).toBe(0)
    await req('/agents/example', { method: 'PUT', body: '{}' })
    await req('/agents/example/rename', { method: 'POST', body: '{}' })
    await req('/workspace/model-configurations/group', { method: 'PUT', body: '{}' })
    expect(modelCatalogRevision.value).toBe(3)
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ error: 'failed save' }), { status: 500 }))
    await expect(req('/agents/example', { method: 'PUT', body: '{}' })).rejects.toThrow('failed save')
    expect(modelCatalogRevision.value).toBe(3)
  })
})

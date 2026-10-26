import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cliCatalogFailed, loadCliModels, resetCliCatalog } from './cliCatalog'

const mocks = vi.hoisted(() => ({ models: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { cliModels: mocks.models } }))

beforeEach(() => {
  mocks.models.mockReset()
  resetCliCatalog()
})

describe('official coding-tool model catalogs', () => {
  it('loads OpenCode official model IDs through the server and shares pending requests', async () => {
    const models = [{ id: 'opencode/official-model', name: 'Official model', vendor: 'opencode' }]
    mocks.models.mockResolvedValue({ models })
    const first = loadCliModels(' OpenCode ')
    const second = loadCliModels('opencode')
    expect(first).toBe(second)
    expect(await first).toEqual(models)
    expect(mocks.models).toHaveBeenCalledExactlyOnceWith('opencode')
    expect(await loadCliModels('opencode')).toEqual(models)
    expect(mocks.models).toHaveBeenCalledTimes(1)
    expect(cliCatalogFailed('OPENCODE')).toBe(false)
  })

  it('retries an unavailable official catalog and clears its failure state after recovery', async () => {
    mocks.models.mockRejectedValueOnce(new Error('Catalog unavailable'))
    expect(await loadCliModels('opencode')).toEqual([])
    expect(cliCatalogFailed('opencode')).toBe(true)
    const models = [{ id: 'opencode/restored-model', name: 'Restored model', vendor: 'opencode' }]
    mocks.models.mockResolvedValueOnce({ models })
    expect(await loadCliModels('opencode')).toEqual(models)
    expect(mocks.models).toHaveBeenCalledTimes(2)
    expect(cliCatalogFailed('opencode')).toBe(false)
  })

  it('treats an HTTP-success catalog error as unavailable and retries instead of caching an empty catalog', async () => {
    mocks.models.mockResolvedValueOnce({ models: [], error: 'Official catalog unavailable' })
    expect(await loadCliModels('opencode')).toEqual([])
    expect(cliCatalogFailed('opencode')).toBe(true)
    const models = [{ id: 'opencode/saved-model', name: 'Saved model', vendor: 'opencode' }]
    mocks.models.mockResolvedValueOnce({ models })
    expect(await loadCliModels('opencode')).toEqual(models)
    expect(mocks.models).toHaveBeenCalledTimes(2)
    expect(cliCatalogFailed('opencode')).toBe(false)
  })
})

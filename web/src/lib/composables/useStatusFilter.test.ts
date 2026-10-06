import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { RouteLocationNormalizedLoaded, Router } from 'vue-router'
import {
  parseStatusQuery,
  serializeStatusQuery,
  normalizeStatuses,
  STATUS_ORDER,
  STATUS_FILTER_STORAGE_KEY,
  readStatusFilterFromStorage,
  writeStatusFilterToStorage,
  clearStatusFilterStorage,
  initStatusFilterFromStorage,
} from './useStatusFilter'

describe('parseStatusQuery', () => {
  it('returns empty for empty input', () => {
    expect(parseStatusQuery('')).toEqual([])
  })

  it('parses single valid status', () => {
    expect(parseStatusQuery('running')).toEqual(['running'])
  })

  it('parses multiple comma-separated statuses', () => {
    expect(parseStatusQuery('failed,running')).toEqual(['failed', 'running'])
  })

  it('filters invalid values and dedupes', () => {
    expect(parseStatusQuery('running,invalid,running,failed')).toEqual(['running', 'failed'])
  })

  it('returns empty when all values invalid', () => {
    expect(parseStatusQuery('bad,unknown')).toEqual([])
  })

  it('trims whitespace around values', () => {
    expect(parseStatusQuery(' running , failed ')).toEqual(['running', 'failed'])
  })
})

describe('serializeStatusQuery', () => {
  it('returns empty for empty array', () => {
    expect(serializeStatusQuery([])).toBe('')
  })

  it('orders by STATUS_ORDER regardless of input order', () => {
    expect(serializeStatusQuery(['failed', 'running', 'queued'])).toBe('running,queued,failed')
  })

  it('serializes single value', () => {
    expect(serializeStatusQuery(['completed'])).toBe('completed')
  })

  it('ignores unknown statuses', () => {
    expect(serializeStatusQuery(['running', 'bogus'])).toBe('running')
  })
})

describe('normalizeStatuses', () => {
  it('returns empty for zero selected', () => {
    expect(normalizeStatuses([])).toEqual([])
  })

  it('returns empty when all 6 statuses selected', () => {
    expect(normalizeStatuses([...STATUS_ORDER])).toEqual([])
  })

  it('preserves partial selection', () => {
    expect(normalizeStatuses(['running', 'failed'])).toEqual(['running', 'failed'])
  })

  it('preserves single selection', () => {
    expect(normalizeStatuses(['queued'])).toEqual(['queued'])
  })
})

describe('round-trip', () => {
  it('serialize after parse yields fixed order', () => {
    const parsed = parseStatusQuery('cancelled,running,failed')
    expect(serializeStatusQuery(parsed)).toBe('running,failed,cancelled')
  })

  it('single-value URL remains valid', () => {
    expect(parseStatusQuery('running')).toEqual(['running'])
    expect(serializeStatusQuery(['running'])).toBe('running')
  })
})

describe('status filter localStorage', () => {
  let store: Record<string, string>

  beforeEach(() => {
    store = {}
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store[k] ?? null,
      setItem: (k: string, v: string) => {
        store[k] = v
      },
      removeItem: (k: string) => {
        delete store[k]
      },
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('read/write/clear use the fixed storage key', () => {
    writeStatusFilterToStorage('running,failed')
    expect(store[STATUS_FILTER_STORAGE_KEY]).toBe('running,failed')
    expect(readStatusFilterFromStorage()).toBe('running,failed')
    clearStatusFilterStorage()
    expect(store[STATUS_FILTER_STORAGE_KEY]).toBeUndefined()
    expect(readStatusFilterFromStorage()).toBe('')
  })

  it('read returns empty when localStorage throws', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked')
      },
    })
    expect(readStatusFilterFromStorage()).toBe('')
  })
})

function mockRoute(query: Record<string, string | undefined>): RouteLocationNormalizedLoaded {
  return { query } as RouteLocationNormalizedLoaded
}

function mockRouter() {
  const replace = vi.fn().mockResolvedValue(undefined)
  return { replace, router: { replace } as unknown as Router }
}

describe('initStatusFilterFromStorage', () => {
  let store: Record<string, string>

  beforeEach(() => {
    store = {}
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store[k] ?? null,
      setItem: (k: string, v: string) => {
        store[k] = v
      },
      removeItem: (k: string) => {
        delete store[k]
      },
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('syncs valid URL status to localStorage (f4)', async () => {
    const route = mockRoute({ status: 'running' })
    const { router } = mockRouter()

    const restored = await initStatusFilterFromStorage(route, router)

    expect(restored).toBe(false)
    expect(store[STATUS_FILTER_STORAGE_KEY]).toBe('running')
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('URL status takes precedence over localStorage (f4 conflict)', async () => {
    store[STATUS_FILTER_STORAGE_KEY] = 'failed'
    const route = mockRoute({ status: 'completed' })
    const { router } = mockRouter()

    const restored = await initStatusFilterFromStorage(route, router)

    expect(restored).toBe(false)
    expect(store[STATUS_FILTER_STORAGE_KEY]).toBe('completed')
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('restores from storage when URL has no status (f2)', async () => {
    store[STATUS_FILTER_STORAGE_KEY] = 'running,failed'
    const route = mockRoute({})
    const { router, replace } = mockRouter()

    const restored = await initStatusFilterFromStorage(route, router)

    expect(restored).toBe(true)
    expect(replace).toHaveBeenCalledWith({ query: { status: 'running,failed' } })
  })

  it('preserves existing ?wf= when restoring from storage', async () => {
    store[STATUS_FILTER_STORAGE_KEY] = 'running'
    const route = mockRoute({ wf: 'feature-dev' })
    const { router, replace } = mockRouter()

    await initStatusFilterFromStorage(route, router)

    expect(replace).toHaveBeenCalledWith({ query: { wf: 'feature-dev', status: 'running' } })
  })

  it('clears invalid storage and does not replace URL', async () => {
    store[STATUS_FILTER_STORAGE_KEY] = 'bogus,unknown'
    const route = mockRoute({})
    const { router, replace } = mockRouter()

    const restored = await initStatusFilterFromStorage(route, router)

    expect(restored).toBe(false)
    expect(store[STATUS_FILTER_STORAGE_KEY]).toBeUndefined()
    expect(replace).not.toHaveBeenCalled()
  })

  it('does not write localStorage for invalid-only URL status', async () => {
    const route = mockRoute({ status: 'bogus' })
    const { router } = mockRouter()

    const restored = await initStatusFilterFromStorage(route, router)

    expect(restored).toBe(false)
    expect(store[STATUS_FILTER_STORAGE_KEY]).toBeUndefined()
  })
})

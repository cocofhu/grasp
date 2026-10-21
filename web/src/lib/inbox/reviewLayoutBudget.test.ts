import { describe, expect, it } from 'vitest'
import {
  OUTER_RIGHT_MIN,
  OUTER_SASH_SNAP,
  OUTER_SASH_WIDTH,
  OUTER_SASH_WIDTH_KEY_SHARED,
  REVIEW_CANVAS_MIN,
  REVIEW_RIGHT_BUDGET,
  applyOuterSashMem,
  clampOuterRight,
  estimateOuterWorkspace,
  initOuterSashFromMemory,
  outerRightMax,
  parseOuterSashMem,
  readSharedOuterSashMem,
  reviewDefaultRightPx,
  writeSharedOuterSashMem,
} from './reviewLayoutBudget'

describe('reviewLayoutBudget constants', () => {
  it('reserves room for a desktop conversation with a separate optional product panel', () => {
    expect(REVIEW_RIGHT_BUDGET).toBe(960)
    expect(REVIEW_CANVAS_MIN).toBe(120)
    expect(OUTER_SASH_WIDTH_KEY_SHARED).toBe('run-detail-outer-sash:shared')
  })

  it('readSharedOuterSashMem reads and writes the shared key', () => {
    const store = new Map<string, string>()
    const ls = {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => {
        store.set(k, v)
      },
    }
    const orig = globalThis.localStorage
    Object.defineProperty(globalThis, 'localStorage', { value: ls, configurable: true })
    try {
      expect(readSharedOuterSashMem()).toBeNull()
      store.set(OUTER_SASH_WIDTH_KEY_SHARED, '{"width":900,"fullOpen":true}')
      expect(readSharedOuterSashMem()).toEqual({ width: 900, fullOpen: true })
      writeSharedOuterSashMem({ width: 880, fullOpen: false })
      expect(store.get(OUTER_SASH_WIDTH_KEY_SHARED)).toBe('{"width":880,"fullOpen":false}')
    } finally {
      Object.defineProperty(globalThis, 'localStorage', { value: orig, configurable: true })
    }
  })
})

describe('conversation defaults across actual container widths', () => {
  it.each([
    [1280, 960, 316],
    [1122, 960, 158], // 1440px with the app directory still visible
    [980, 856, 120],
    [780, 656, 120],
  ])('workspace %i keeps a conversation width %i and a diagram reserve %i', (workspace, expected, diagram) => {
    const right = reviewDefaultRightPx(workspace)
    expect(right).toBe(expected)
    expect(workspace - right - OUTER_SASH_WIDTH).toBe(diagram)
  })

  it('protects a readable chat on a small desktop container and still permits a full-width drag', () => {
    expect(reviewDefaultRightPx(400)).toBe(320)
    expect(clampOuterRight(396, 400)).toEqual({ width: 396, fullOpen: true })
  })
})

describe('outer sash drag clamp (canvas minWidth is 0, not REVIEW_CANVAS_MIN)', () => {
  it('uses a compact conversation floor independent of a hidden product panel', () => {
    expect(OUTER_SASH_WIDTH).toBe(4)
    expect(OUTER_SASH_SNAP).toBe(16)
    expect(OUTER_RIGHT_MIN).toBe(320)
  })

  it('left limit: rightMax = workspace-4 so canvas visible width can be 0', () => {
    const ws = 1000
    expect(outerRightMax(ws)).toBe(996)
    const full = clampOuterRight(996, ws, true)
    expect(full.width).toBe(996)
    expect(full.fullOpen).toBe(true)
    expect(ws - OUTER_SASH_WIDTH - full.width).toBe(0)
  })

  it('right limit: cannot drag the right panel below 320', () => {
    const ws = 1200
    const squeezed = clampOuterRight(100, ws, false)
    expect(squeezed.width).toBe(320)
    expect(squeezed.fullOpen).toBe(false)
  })

  it('near-full SNAP=16 adsorbs to canvas=0', () => {
    const ws = 1000
    const almost = clampOuterRight(996 - 16, ws, true)
    expect(almost.width).toBe(996)
    expect(almost.fullOpen).toBe(true)
    const notYet = clampOuterRight(996 - 17, ws, true)
    expect(notYet.fullOpen).toBe(false)
    expect(notYet.width).toBe(979)
  })

  it('default px matches review formula and is not full-open', () => {
    const ws = 1280
    const def = reviewDefaultRightPx(ws)
    expect(def).toBe(960)
    expect(def).toBeLessThan(outerRightMax(ws))
  })

  it('fullOpen flag wins over stale pixels when the window grows', () => {
    const grown = applyOuterSashMem({ width: 996, fullOpen: true }, 1400)
    expect(grown.fullOpen).toBe(true)
    expect(grown.width).toBe(outerRightMax(1400))
    const pixelsOnly = applyOuterSashMem({ width: 996, fullOpen: false }, 1400)
    expect(pixelsOnly.fullOpen).toBe(false)
    expect(pixelsOnly.width).toBe(996)
  })

  it('parseOuterSashMem reads width+fullOpen JSON', () => {
    expect(parseOuterSashMem('{"width":900,"fullOpen":true}')).toEqual({
      width: 900,
      fullOpen: true,
    })
    expect(parseOuterSashMem('not-json')).toBeNull()
    expect(applyOuterSashMem(null, 1280).width).toBe(960)
  })

  it('initOuterSashFromMemory sync-applies shared memory before mount measure', () => {
    const store = new Map<string, string>()
    const ls = {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => {
        store.set(k, v)
      },
    }
    const orig = globalThis.localStorage
    Object.defineProperty(globalThis, 'localStorage', { value: ls, configurable: true })
    try {
      store.set(OUTER_SASH_WIDTH_KEY_SHARED, '{"width":650,"fullOpen":false}')
      const ws = 1280
      const init = initOuterSashFromMemory(ws)
      expect(init.width).toBe(650)
      expect(init.fullOpen).toBe(false)
      expect(init).toEqual(applyOuterSashMem(readSharedOuterSashMem(), ws))
      store.delete(OUTER_SASH_WIDTH_KEY_SHARED)
      expect(initOuterSashFromMemory(ws).width).toBe(reviewDefaultRightPx(ws))
    } finally {
      Object.defineProperty(globalThis, 'localStorage', { value: orig, configurable: true })
    }
  })

  it('estimateOuterWorkspace subtracts shell sidebar on desktop widths', () => {
    const orig = globalThis.window
    Object.defineProperty(globalThis, 'window', {
      value: { innerWidth: 1400 },
      configurable: true,
    })
    try {
      expect(estimateOuterWorkspace()).toBe(1026)
    } finally {
      Object.defineProperty(globalThis, 'window', { value: orig, configurable: true })
    }
  })
})

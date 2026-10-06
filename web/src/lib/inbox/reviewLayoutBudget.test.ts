import { describe, expect, it } from 'vitest'
import {
  OUTER_CHAT_FLOOR,
  OUTER_RIGHT_MIN,
  OUTER_SASH_SNAP,
  OUTER_SASH_WIDTH,
  OUTER_SASH_WIDTH_KEY_SHARED,
  OUTER_STAGE_MIN,
  REVIEW_CANVAS_MIN,
  REVIEW_GAP,
  REVIEW_RIGHT_BUDGET,
  REVIEW_SIDEBAR,
  REVIEW_SHELL_WIDTH_KEY_APPROVAL,
  REVIEW_SHELL_WIDTH_KEY_CLARIFY,
  REVIEW_SHELL_WIDTH_KEY_REVIEW,
  REVIEW_STAGE_TARGET,
  applyOuterSashMem,
  clampOuterRight,
  computeDesktopReviewSplit,
  estimateOuterWorkspace,
  initOuterSashFromMemory,
  outerRightMax,
  parseOuterSashMem,
  readSharedOuterSashMem,
  reviewDefaultRightPx,
  reviewRightPanelCssWidth,
  writeSharedOuterSashMem,
} from './reviewLayoutBudget'

describe('reviewLayoutBudget constants', () => {
  it('matches approved page.html budget formula', () => {
    expect(REVIEW_STAGE_TARGET).toBe(480)
    expect(REVIEW_SIDEBAR).toBe(300)
    expect(REVIEW_GAP).toBe(1)
    expect(REVIEW_RIGHT_BUDGET).toBe(781)
    expect(REVIEW_CANVAS_MIN).toBe(200)
    expect(reviewRightPanelCssWidth()).toBe('min(781px, calc(100% - 200px))')
  })

  it('exposes distinct ReviewShell width storage keys', () => {
    expect(REVIEW_SHELL_WIDTH_KEY_REVIEW).toBe('review-shell-sidebar-width:review')
    expect(REVIEW_SHELL_WIDTH_KEY_APPROVAL).toBe('review-shell-sidebar-width:approval')
    expect(REVIEW_SHELL_WIDTH_KEY_CLARIFY).toBe('review-shell-sidebar-width:clarify')
    expect(REVIEW_SHELL_WIDTH_KEY_REVIEW).not.toBe(REVIEW_SHELL_WIDTH_KEY_APPROVAL)
  })

  it('isolates outer sash storage from inner ReviewShell keys', () => {
    expect(OUTER_SASH_WIDTH_KEY_SHARED).toBe('run-detail-outer-sash:shared')
    expect(OUTER_SASH_WIDTH_KEY_SHARED).not.toBe(REVIEW_SHELL_WIDTH_KEY_CLARIFY)
    expect(OUTER_SASH_WIDTH_KEY_SHARED).not.toBe(REVIEW_SHELL_WIDTH_KEY_REVIEW)
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

describe('computeDesktopReviewSplit vs page.html viewports', () => {
  it('wide 1280: stage ≥480, canvas visible above floor', () => {
    const s = computeDesktopReviewSplit(1280)
    expect(s.right).toBe(781)
    expect(s.stage).toBeGreaterThanOrEqual(REVIEW_STAGE_TARGET)
    expect(s.canvas).toBeGreaterThan(REVIEW_CANVAS_MIN)
    expect(s.sidebar).toBe(300)
  })

  it('mid 980: canvas at floor, stage near target (may be 1px under)', () => {
    const s = computeDesktopReviewSplit(980)
    expect(s.canvas).toBe(REVIEW_CANVAS_MIN)
    expect(s.right).toBe(780)
    // availForRight 780 < budget 781 → slight compress; still ≫ 120px strip
    expect(s.stage).toBeGreaterThanOrEqual(REVIEW_STAGE_TARGET - REVIEW_GAP)
    expect(s.stage).toBeGreaterThan(120)
  })

  it('narrow 780: canvas ≥200, stage ≫120, still horizontal split room', () => {
    const s = computeDesktopReviewSplit(780)
    expect(s.canvas).toBe(REVIEW_CANVAS_MIN)
    expect(s.right).toBe(580)
    expect(s.stage).toBe(279)
    expect(s.stage).toBeGreaterThan(120)
    expect(s.stage).toBeLessThan(REVIEW_STAGE_TARGET)
    // stage + sidebar + gap fits in right panel
    expect(s.stage + s.sidebar + REVIEW_GAP).toBe(s.right)
  })

  it('degradation order: compress canvas to floor before stage dips under 480', () => {
    // Just above floor+budget: canvas still >200, stage at target.
    const wideEnough = computeDesktopReviewSplit(REVIEW_CANVAS_MIN + REVIEW_RIGHT_BUDGET + 50)
    expect(wideEnough.canvas).toBeGreaterThan(REVIEW_CANVAS_MIN)
    expect(wideEnough.stage).toBe(REVIEW_STAGE_TARGET)

    // Exactly at floor+budget: canvas pinned, stage still target.
    const atFloor = computeDesktopReviewSplit(REVIEW_CANVAS_MIN + REVIEW_RIGHT_BUDGET)
    expect(atFloor.canvas).toBe(REVIEW_CANVAS_MIN)
    expect(atFloor.stage).toBe(REVIEW_STAGE_TARGET)

    // Below default-reserve+budget: default formula still pins canvas at 200 (not a drag floor).
    const tight = computeDesktopReviewSplit(REVIEW_CANVAS_MIN + REVIEW_RIGHT_BUDGET - 100)
    expect(tight.canvas).toBe(REVIEW_CANVAS_MIN)
    expect(tight.stage).toBeLessThan(REVIEW_STAGE_TARGET)
    expect(tight.stage).toBeGreaterThan(120)
  })
})

describe('outer sash drag clamp (canvas minWidth is 0, not REVIEW_CANVAS_MIN)', () => {
  it('rightMin is 324 (stage 160 + sash 4 + depressed chat 160), not CHAT_MIN 240', () => {
    expect(OUTER_SASH_WIDTH).toBe(4)
    expect(OUTER_SASH_SNAP).toBe(16)
    expect(OUTER_STAGE_MIN).toBe(160)
    expect(OUTER_CHAT_FLOOR).toBe(160)
    expect(OUTER_RIGHT_MIN).toBe(324)
    expect(OUTER_RIGHT_MIN).not.toBe(160 + 4 + 240)
  })

  it('left limit: rightMax = workspace-4 so canvas visible width can be 0', () => {
    const ws = 1000
    expect(outerRightMax(ws)).toBe(996)
    const full = clampOuterRight(996, ws, true)
    expect(full.width).toBe(996)
    expect(full.fullOpen).toBe(true)
    expect(ws - OUTER_SASH_WIDTH - full.width).toBe(0)
  })

  it('right limit: cannot drag the right panel below ~324', () => {
    const ws = 1200
    const squeezed = clampOuterRight(100, ws, false)
    expect(squeezed.width).toBe(324)
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
    expect(def).toBe(781)
    expect(def).toBe(computeDesktopReviewSplit(ws).right)
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
    expect(applyOuterSashMem(null, 1280).width).toBe(781)
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
      expect(estimateOuterWorkspace()).toBe(1160)
    } finally {
      Object.defineProperty(globalThis, 'window', { value: orig, configurable: true })
    }
  })
})

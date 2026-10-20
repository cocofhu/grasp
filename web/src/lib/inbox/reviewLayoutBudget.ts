/** Desktop node details prioritize the conversation; products open inside it on demand. */
export const REVIEW_RIGHT_BUDGET = 960
/** Default reserve for the workflow diagram, not a drag limit. */
export const REVIEW_CANVAS_MIN = 120

/** Outer sash (diagram/timeline vs node conversation). */
export const OUTER_SASH_WIDTH = 4
export const OUTER_SASH_SNAP = 16
/** A readable compact conversation; products use the separate panel below its breakpoint. */
export const OUTER_RIGHT_MIN = 320

/**
 * Outer sash persistence — shared across node tabs and independent of product-panel width.
 * Stores pixel width + fullOpen so a resized window does not leave a fake gap.
 * Shared across all desktop node tabs so tab switches do not jump width.
 */
export const OUTER_SASH_WIDTH_KEY_SHARED = 'run-detail-outer-sash:shared'

export function readSharedOuterSashMem(): OuterSashMem | null {
  try {
    if (typeof localStorage === 'undefined') return null
    return parseOuterSashMem(localStorage.getItem(OUTER_SASH_WIDTH_KEY_SHARED))
  } catch {
    return null
  }
}

export function writeSharedOuterSashMem(mem: OuterSashMem) {
  try {
    if (typeof localStorage === 'undefined') return
    localStorage.setItem(OUTER_SASH_WIDTH_KEY_SHARED, JSON.stringify(mem))
  } catch {
    /* quota / private mode */
  }
}

export function outerRightMax(workspace: number, sash = OUTER_SASH_WIDTH): number {
  return Math.max(0, Math.round(workspace - sash))
}

export function outerRightMin(workspace: number): number {
  const max = outerRightMax(workspace)
  return Math.min(OUTER_RIGHT_MIN, max)
}

/** Prefer a full conversation and reserve a small diagram strip until the user drags. */
export function reviewDefaultRightPx(workspace: number): number {
  const raw = Math.min(REVIEW_RIGHT_BUDGET, workspace - REVIEW_CANVAS_MIN - OUTER_SASH_WIDTH)
  return clampOuterRight(raw, workspace, false).width
}

export type OuterSashMem = { width: number; fullOpen: boolean }

export function clampOuterRight(
  px: number,
  workspace: number,
  snapFull = true,
): OuterSashMem {
  const max = outerRightMax(workspace)
  const min = outerRightMin(workspace)
  let width = Math.round(px)
  if (!Number.isFinite(width)) width = min
  width = Math.max(min, Math.min(max, width))
  let fullOpen = width >= max && max > 0
  if (snapFull && max - width <= OUTER_SASH_SNAP && max > min) {
    width = max
    fullOpen = true
  }
  return { width, fullOpen }
}

export function parseOuterSashMem(raw: string | null | undefined): OuterSashMem | null {
  if (raw == null || raw === '') return null
  try {
    const v = JSON.parse(raw) as unknown
    if (!v || typeof v !== 'object') return null
    const rec = v as { width?: unknown; fullOpen?: unknown }
    const width = Number(rec.width)
    if (!Number.isFinite(width)) return null
    return { width: Math.round(width), fullOpen: rec.fullOpen === true }
  } catch {
    return null
  }
}

/**
 * Restore remembered split. fullOpen wins over stale pixels (window grew).
 * Does not snap: restoring a near-max custom width without the flag stays un-full.
 */
export function applyOuterSashMem(mem: OuterSashMem | null, workspace: number): OuterSashMem {
  if (mem?.fullOpen) {
    const max = outerRightMax(workspace)
    return { width: max, fullOpen: true }
  }
  if (mem) return clampOuterRight(mem.width, workspace, false)
  return { width: reviewDefaultRightPx(workspace), fullOpen: false }
}

/** Rough split-root width before mount (sidebar-aware; outer sash is desktop-only). */
export function estimateOuterWorkspace(): number {
  if (typeof window === 'undefined') return 1280
  const vw = window.innerWidth || 0
  if (vw <= 0) return 1280
  // Desktop sidebar: 56px rail + 248px list + 14px padding; mobile has no outer sash.
  if (vw >= 768) return Math.max(400, vw - 318)
  return vw
}

/** Sync read localStorage and apply before first paint (re-clamp after real measure). */
export function initOuterSashFromMemory(workspace = estimateOuterWorkspace()): OuterSashMem {
  return applyOuterSashMem(readSharedOuterSashMem(), workspace)
}

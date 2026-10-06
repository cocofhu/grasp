/**
 * Desktop Run Detail「复审 / Agent交互」layout budget.
 * Default right width is aligned with the approved prototype:
 * stage target 480 + sidebar 300 + gap 1 → right ≈ 781; canvas *default* reserve 200.
 * REVIEW_CANVAS_MIN is only the default-reserve in reviewRightPanelCssWidth /
 * computeDesktopReviewSplit — not the drag canvas minWidth (drag floor is 0).
 */
export const REVIEW_STAGE_TARGET = 480
export const REVIEW_SIDEBAR = 300
export const REVIEW_GAP = 1
export const REVIEW_RIGHT_BUDGET = REVIEW_STAGE_TARGET + REVIEW_SIDEBAR + REVIEW_GAP
export const REVIEW_CANVAS_MIN = 200

/** Outer sash (canvas/timeline vs whole right panel). Visual 4px; hit expands ~12px. */
export const OUTER_SASH_WIDTH = 4
/** Near-full snap: within this many px of rightMax, adsorb to canvas=0. */
export const OUTER_SASH_SNAP = 16
/** Inner stage floor (ReviewShell STAGE_MIN) used in outer rightMin. */
export const OUTER_STAGE_MIN = 160
/**
 * Depressed chat floor for the *outer* rightMin (not inner CHAT_MIN=240).
 * ReviewShell already squeezes chat below 240 when the shell is tight.
 */
export const OUTER_CHAT_FLOOR = 160
/** rightMin ≈ stage 160 + inner sash 4 + depressed chat 160. */
export const OUTER_RIGHT_MIN = OUTER_STAGE_MIN + OUTER_SASH_WIDTH + OUTER_CHAT_FLOOR

/** localStorage keys for ReviewShell desktop sidebar width (scene-isolated). */
export const REVIEW_SHELL_WIDTH_KEY_REVIEW = 'review-shell-sidebar-width:review'
export const REVIEW_SHELL_WIDTH_KEY_APPROVAL = 'review-shell-sidebar-width:approval'
export const REVIEW_SHELL_WIDTH_KEY_CLARIFY = 'review-shell-sidebar-width:clarify'

/**
 * Outer sash persistence — isolated from review-shell-sidebar-width:*.
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

/** CSS width for the right panel default (un-customized) on outer-sash tabs. */
export function reviewRightPanelCssWidth(
  budget = REVIEW_RIGHT_BUDGET,
  canvasMin = REVIEW_CANVAS_MIN,
): string {
  return `min(${budget}px, calc(100% - ${canvasMin}px))`
}

/**
 * Ideal *default* split under width:min(budget, 100%-canvasMin).
 * Drag path must not use REVIEW_CANVAS_MIN as canvas minWidth — canvas may go to 0.
 */
export function computeDesktopReviewSplit(parentWidth: number) {
  const right = Math.min(REVIEW_RIGHT_BUDGET, parentWidth - REVIEW_CANVAS_MIN)
  const canvas = parentWidth - right
  const stage = right - REVIEW_SIDEBAR - REVIEW_GAP
  return { right, canvas, stage, sidebar: REVIEW_SIDEBAR }
}

export function outerRightMax(workspace: number, sash = OUTER_SASH_WIDTH): number {
  return Math.max(0, Math.round(workspace - sash))
}

export function outerRightMin(workspace: number): number {
  const max = outerRightMax(workspace)
  return Math.min(OUTER_RIGHT_MIN, max)
}

/** Un-customized default right px (same formula as reviewRightPanelCssWidth). */
export function reviewDefaultRightPx(workspace: number): number {
  const raw = Math.min(REVIEW_RIGHT_BUDGET, workspace - REVIEW_CANVAS_MIN)
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
  // App shell sidebar ≈240px on md+; mobile has no horizontal outer sash.
  if (vw >= 768) return Math.max(400, vw - 240)
  return vw
}

/** Sync read localStorage and apply before first paint (re-clamp after real measure). */
export function initOuterSashFromMemory(workspace = estimateOuterWorkspace()): OuterSashMem {
  return applyOuterSashMem(readSharedOuterSashMem(), workspace)
}

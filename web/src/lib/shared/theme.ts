import { nextTick, ref } from 'vue'

export type ThemeName = 'dark' | 'light'

const STORAGE_KEY = 'grasp-theme'

function readStored(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

function initial(): ThemeName {
  const saved = readStored()
  if (saved === 'dark' || saved === 'light') return saved
  return 'dark'
}

export const theme = ref<ThemeName>(initial())

/**
 * Synchronous choice for the shell toggle.
 * theme.value is what Vue paints (icon .is-active). It must stay on the
 * previous theme until the view-transition update callback, or the old
 * snapshot is already the destination icon (review v1).
 */
let committed: ThemeName = theme.value

/**
 * True from startViewTransition until its update callback finishes.
 * Clicks in that window only advance `committed`; the in-flight callback
 * paints the latest choice so two fast clicks are not both computed from
 * the still-unchanged theme.value (plan g2.2).
 */
let vtUpdatePending = false

let override: ThemeName | null = null

function apply(t: ThemeName) {
  t = override ?? t
  const root = document.documentElement
  root.classList.toggle('light', t === 'light')
  root.style.colorScheme = t
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

/** Bumps on each animated toggle so a skipped transition cannot clear the next capture. */
let themeMotionGen = 0

function clearThemeMotionClass(gen: number, className: string) {
  if (gen !== themeMotionGen) return
  document.documentElement.classList.remove(className)
}

/**
 * Paint `committed` and wait until Vue has flushed .is-active.
 * Called only from inside startViewTransition's update callback, after the
 * old snapshot (plan g1.2 / review v1).
 */
async function paintCommittedTheme() {
  let guard = 0
  while (guard++ < 8) {
    const next = committed
    theme.value = next
    apply(next)
    await nextTick()
    if (committed === next) return
  }
  theme.value = committed
  apply(committed)
  await nextTick()
}

/**
 * Shell theme button only.
 * Page colors cross-fade for --dur-overlay (200ms, ease-out-expo).
 * The visible sun/moon spin is 280ms ease (±90°, scale 0.55 → 1) on the
 * view-transition layer. Reduced motion paints immediately.
 * setTheme / embed override / public chrome stay instant.
 *
 * View Transitions: do not touch theme.value before the update callback.
 * The old snapshot must still be the previous sun/moon and the previous
 * html light class; the callback then writes the next theme and returns
 * nextTick() so the new snapshot is the destination icon.
 * theme-vt-capture stays until ready, which is after both snapshots.
 * It freezes the live icon transition so the snapshots are resting icons;
 * the spin plays on the pseudo-elements, not as a 4px pop.
 */
function applyAnimated(t: ThemeName) {
  const root = document.documentElement
  if (prefersReducedMotion()) {
    vtUpdatePending = false
    root.classList.remove('theme-vt-capture', 'theme-color-motion')
    theme.value = t
    apply(t)
    return
  }

  root.classList.remove('theme-color-motion')
  if (typeof document.startViewTransition === 'function') {
    // Callback has not run yet: it will read the latest `committed`.
    if (vtUpdatePending) return
    const gen = ++themeMotionGen
    // Freeze the live icon transition so both snapshots are resting icons.
    // The 280ms spin plays on the view-transition pseudos, not this class.
    root.classList.add('theme-vt-capture')
    vtUpdatePending = true
    try {
      const vt = document.startViewTransition(() => {
        const pending = paintCommittedTheme()
        void pending.finally(() => {
          vtUpdatePending = false
        })
        return pending
      })
      const done = () => clearThemeMotionClass(gen, 'theme-vt-capture')
      void vt.ready.then(done, done)
      void vt.finished.then(done, done)
      return
    } catch {
      vtUpdatePending = false
      clearThemeMotionClass(gen, 'theme-vt-capture')
      theme.value = t
      apply(t)
      return
    }
  }

  // No View Transitions: the icon keeps its own CSS transition (review v1).
  // Color motion is limited to shell surfaces in global.css (review v3).
  const gen = ++themeMotionGen
  root.classList.add('theme-color-motion')
  theme.value = t
  void root.offsetWidth
  apply(t)
  window.setTimeout(() => clearThemeMotionClass(gen, 'theme-color-motion'), 240)
}

export function setTheme(t: ThemeName) {
  committed = t
  theme.value = t
  localStorage.setItem(STORAGE_KEY, t)
  themeMotionGen++
  vtUpdatePending = false
  const root = document.documentElement
  root.classList.remove('theme-vt-capture', 'theme-color-motion')
  apply(t)
}

export function toggleTheme() {
  const next: ThemeName = committed === 'dark' ? 'light' : 'dark'
  committed = next
  localStorage.setItem(STORAGE_KEY, next)
  applyAnimated(next)
}

/**
 * Public external page: force light chrome (html.light + color-scheme).
 * Does not call setTheme or write grasp-theme, so internal users'
 * persisted theme is not polluted.
 */
export function applyPublicLightChrome(): void {
  const root = document.documentElement
  root.classList.add('light')
  root.style.colorScheme = 'light'
}

/**
 * Embedded pages follow the host page's theme. The override wins over the
 * persisted theme without writing grasp-theme; null goes back to it.
 */
export function setThemeOverride(t: ThemeName | null): void {
  override = t
  apply(theme.value)
}

/** Re-apply persisted/default theme after leaving a public page. */
export function reapplyThemeChrome(): void {
  apply(theme.value)
}

apply(theme.value)

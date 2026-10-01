import { ref } from 'vue'
import {
  GRASP_STORAGE_KEYS,
  LEGACY_STORAGE_KEYS,
  migrateLocalStorageKey,
} from './migrateBrandStorage'

export type ThemeName = 'dark' | 'light'

const STORAGE_KEY = GRASP_STORAGE_KEYS.theme

function initial(): ThemeName {
  const saved = migrateLocalStorageKey(LEGACY_STORAGE_KEYS.theme, STORAGE_KEY) as ThemeName | null
  if (saved === 'dark' || saved === 'light') return saved
  return 'dark'
}

export const theme = ref<ThemeName>(initial())

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
 * Shell theme button only (plan g1 / g2).
 * Page colors cross-fade for --dur-overlay (same 200ms as the language menu).
 * The icon's own rise/scale stays on the button styles. Reduced motion paints immediately.
 * setTheme / embed override / public chrome stay instant.
 */
function applyAnimated(t: ThemeName) {
  const root = document.documentElement
  if (prefersReducedMotion()) {
    root.classList.remove('theme-vt-capture', 'theme-color-motion')
    apply(t)
    return
  }

  root.classList.remove('theme-color-motion')
  if (typeof document.startViewTransition === 'function') {
    const gen = ++themeMotionGen
    // Freeze icon CSS transitions so the new snapshot is the finished icon, not the first frame.
    root.classList.add('theme-vt-capture')
    try {
      const vt = document.startViewTransition(() => apply(t))
      const done = () => clearThemeMotionClass(gen, 'theme-vt-capture')
      void vt.ready.then(done, done)
      void vt.finished.then(done, done)
      return
    } catch {
      clearThemeMotionClass(gen, 'theme-vt-capture')
      apply(t)
      return
    }
  }

  // No View Transitions: let color properties ease for the same overlay duration.
  const gen = ++themeMotionGen
  root.classList.add('theme-color-motion')
  void root.offsetWidth
  apply(t)
  window.setTimeout(() => clearThemeMotionClass(gen, 'theme-color-motion'), 240)
}

export function setTheme(t: ThemeName) {
  theme.value = t
  localStorage.setItem(STORAGE_KEY, t)
  apply(t)
}

export function toggleTheme() {
  const next: ThemeName = theme.value === 'dark' ? 'light' : 'dark'
  theme.value = next
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

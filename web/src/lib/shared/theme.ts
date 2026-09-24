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

export function setTheme(t: ThemeName) {
  theme.value = t
  localStorage.setItem(STORAGE_KEY, t)
  apply(t)
}

export function toggleTheme() {
  setTheme(theme.value === 'dark' ? 'light' : 'dark')
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

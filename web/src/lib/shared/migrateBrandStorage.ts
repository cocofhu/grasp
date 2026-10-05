/**
 * One-shot approving-* → grasp-* Web Storage migration.
 * Old keys may appear only in LEGACY_* tables / this helper.
 */

export const LEGACY_STORAGE_KEYS = {
  theme: 'approving-theme',
  locale: 'approving-locale',
  sidebarHidden: 'approving-sidebar-hidden',
  projectContext: 'approving-project-context',
  runsStatusFilter: 'approving-runs-status-filter',
  onboardingSuppressPrefix: 'approving-onboarding-suppress:',
  workflowFavoritesPrefix: 'approving.workflowFavorites.',
  homeLastWorkflowId: 'approving.home.lastPipelineId',
  homeLastPriority: 'approving.home.lastPriority',
  homeComposerDraft: 'approving.home.composerDraft',
  gateShareUrlPrefix: 'approving.gateShareUrl.',
  /** Dead notify keys — only cleaned up if present. */
  notificationsPrefsPrefix: 'approving.notifications.prefs.',
  runTerminalReadIdsPrefix: 'approving.runTerminalNotifications.readIds.',
} as const

export const GRASP_STORAGE_KEYS = {
  theme: 'grasp-theme',
  locale: 'grasp-locale',
  sidebarHidden: 'grasp-sidebar-hidden',
  projectContext: 'grasp-project-context',
  runsStatusFilter: 'grasp-runs-status-filter',
  onboardingSuppressPrefix: 'grasp-onboarding-suppress:',
  workflowFavoritesPrefix: 'grasp.workflowFavorites.',
  homeLastWorkflowId: 'grasp.home.lastPipelineId',
  homeLastPriority: 'grasp.home.lastPriority',
  homeComposerDraft: 'grasp.home.composerDraft',
  gateShareUrlPrefix: 'grasp.gateShareUrl.',
} as const

/** Legacy IDB database name (read once then drop). */
export const LEGACY_DRAFT_IDB_NAME = 'approving-drafts'

/**
 * If `newKey` is empty and `oldKey` has a value, copy to newKey and remove oldKey.
 * If both exist, keep newKey and remove oldKey.
 * Returns the value that should be used (new if present, else migrated old, else null).
 */
export function migrateLocalStorageKey(oldKey: string, newKey: string): string | null {
  if (typeof localStorage === 'undefined') return null
  try {
    const next = localStorage.getItem(newKey)
    const prev = localStorage.getItem(oldKey)
    if (next != null && next !== '') {
      if (prev != null) localStorage.removeItem(oldKey)
      return next
    }
    if (prev != null) {
      localStorage.setItem(newKey, prev)
      localStorage.removeItem(oldKey)
      return prev
    }
    return null
  } catch {
    return null
  }
}

/** Same semantics for sessionStorage (gate share URLs). */
export function migrateSessionStorageKey(oldKey: string, newKey: string): string | null {
  if (typeof sessionStorage === 'undefined') return null
  try {
    const next = sessionStorage.getItem(newKey)
    const prev = sessionStorage.getItem(oldKey)
    if (next != null && next !== '') {
      if (prev != null) sessionStorage.removeItem(oldKey)
      return next
    }
    if (prev != null) {
      sessionStorage.setItem(newKey, prev)
      sessionStorage.removeItem(oldKey)
      return prev
    }
    return null
  } catch {
    return null
  }
}

/**
 * Migrate every localStorage key with `oldPrefix` to the same suffix under `newPrefix`.
 * Deletes old keys after copy. When both exist for a suffix, keeps new.
 */
export function migrateLocalStoragePrefix(oldPrefix: string, newPrefix: string): void {
  if (typeof localStorage === 'undefined') return
  try {
    const toMigrate: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (k && k.startsWith(oldPrefix)) toMigrate.push(k)
    }
    for (const oldKey of toMigrate) {
      const suffix = oldKey.slice(oldPrefix.length)
      const newKey = newPrefix + suffix
      migrateLocalStorageKey(oldKey, newKey)
    }
  } catch {
    /* private mode */
  }
}

export function migrateSessionStoragePrefix(oldPrefix: string, newPrefix: string): void {
  if (typeof sessionStorage === 'undefined') return
  try {
    const toMigrate: string[] = []
    for (let i = 0; i < sessionStorage.length; i++) {
      const k = sessionStorage.key(i)
      if (k && k.startsWith(oldPrefix)) toMigrate.push(k)
    }
    for (const oldKey of toMigrate) {
      const suffix = oldKey.slice(oldPrefix.length)
      const newKey = newPrefix + suffix
      migrateSessionStorageKey(oldKey, newKey)
    }
  } catch {
    /* private mode */
  }
}

/** Drop leftover brand keys that have no grasp successor (dead notify prefs). */
export function removeLocalStoragePrefix(prefix: string): void {
  if (typeof localStorage === 'undefined') return
  try {
    const doomed: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (k && k.startsWith(prefix)) doomed.push(k)
    }
    for (const k of doomed) localStorage.removeItem(k)
  } catch {
    /* ignore */
  }
}

let brandMigrationRan = false

/** Idempotent: migrate all known fixed + prefix keys once per page load. */
export function runBrandStorageMigration(): void {
  if (brandMigrationRan) return
  brandMigrationRan = true
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.theme, GRASP_STORAGE_KEYS.theme)
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.locale, GRASP_STORAGE_KEYS.locale)
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.sidebarHidden, GRASP_STORAGE_KEYS.sidebarHidden)
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.projectContext, GRASP_STORAGE_KEYS.projectContext)
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.runsStatusFilter, GRASP_STORAGE_KEYS.runsStatusFilter)
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.homeLastWorkflowId, GRASP_STORAGE_KEYS.homeLastWorkflowId)
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.homeLastPriority, GRASP_STORAGE_KEYS.homeLastPriority)
  migrateLocalStorageKey(LEGACY_STORAGE_KEYS.homeComposerDraft, GRASP_STORAGE_KEYS.homeComposerDraft)
  migrateLocalStoragePrefix(
    LEGACY_STORAGE_KEYS.onboardingSuppressPrefix,
    GRASP_STORAGE_KEYS.onboardingSuppressPrefix,
  )
  migrateLocalStoragePrefix(
    LEGACY_STORAGE_KEYS.workflowFavoritesPrefix,
    GRASP_STORAGE_KEYS.workflowFavoritesPrefix,
  )
  migrateSessionStoragePrefix(
    LEGACY_STORAGE_KEYS.gateShareUrlPrefix,
    GRASP_STORAGE_KEYS.gateShareUrlPrefix,
  )
  removeLocalStoragePrefix(LEGACY_STORAGE_KEYS.notificationsPrefsPrefix)
  removeLocalStoragePrefix(LEGACY_STORAGE_KEYS.runTerminalReadIdsPrefix)
}

/** Test-only. */
export function __resetBrandStorageMigrationForTests(): void {
  brandMigrationRan = false
}

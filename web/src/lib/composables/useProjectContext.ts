import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import {
  GRASP_STORAGE_KEYS,
  LEGACY_STORAGE_KEYS,
  migrateLocalStorageKey,
} from '@/lib/shared/migrateBrandStorage'

export const PROJECT_CONTEXT_STORAGE_KEY = GRASP_STORAGE_KEYS.projectContext

migrateLocalStorageKey(LEGACY_STORAGE_KEYS.projectContext, PROJECT_CONTEXT_STORAGE_KEY)

/** Sentinel for「全部项目」— empty string in URL/storage means all. */
const PROJECT_CONTEXT_ALL = ''

export function readStoredProjectId(): string {
  try {
    const v = localStorage.getItem(PROJECT_CONTEXT_STORAGE_KEY)
    return v == null ? PROJECT_CONTEXT_ALL : v
  } catch {
    return PROJECT_CONTEXT_ALL
  }
}

export function writeStoredProjectId(id: string) {
  try {
    if (!id) localStorage.removeItem(PROJECT_CONTEXT_STORAGE_KEY)
    else localStorage.setItem(PROJECT_CONTEXT_STORAGE_KEY, id)
  } catch {
    /* ignore quota / private mode */
  }
}

/**
 * Project context for Runs / Gates / Artifacts.
 * - URL `?projectId=` is the source of truth when present
 * - On first visit without query: restore last choice from localStorage;
 *   if none, default to「全部项目」
 * - Changing selection updates URL + localStorage
 */
export function useProjectContext() {
  const route = useRoute()
  const router = useRouter()

  const selected = computed<string>({
    get: () => {
      if (typeof route.query.projectId === 'string') return route.query.projectId
      // Absent from URL → treat as「全部」for the getter; hydration happens via ensureHydrated.
      return PROJECT_CONTEXT_ALL
    },
    set: (val) => {
      writeStoredProjectId(val)
      const query = { ...route.query }
      if (val) query.projectId = val
      else delete query.projectId
      router.replace({ query })
    },
  })

  /**
   * Call once on mount of platform list pages to restore stored project into URL.
   * When deep-linking with `?run=` (inbox wait), do not write a stored project
   * over filters that may belong to a different run (plan g1.2). Match by id
   * presence of `?run=` / `?projectId=`, never by project name.
   */
  function ensureHydrated() {
    if (typeof route.query.projectId === 'string') {
      writeStoredProjectId(route.query.projectId)
      return
    }
    const deepLinkRun =
      typeof route.query.run === 'string' ? route.query.run.trim() : ''
    if (deepLinkRun) {
      // Waiting on a specific run: keep current (possibly empty) project filter
      // so a stored proj-a cannot filter out a just-submitted proj-b pipeline.
      return
    }
    const stored = readStoredProjectId()
    if (stored) {
      const query = { ...route.query, projectId: stored }
      router.replace({ query })
    }
  }

  function setProject(id: string) {
    selected.value = id
  }

  return { selected, ensureHydrated, setProject }
}

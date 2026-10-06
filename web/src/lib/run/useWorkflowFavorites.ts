import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useAuth } from '@/lib/composables/useAuth'
import { useToast } from '@/lib/composables/useToast'
import { httpStatusOf } from '@/lib/shared/listRequestSeq'
import type { Workflow } from '@/lib/shared/types'

/** Personal quick-launch favorites hard limit (clarified requirement / Demo). */
export const WORKFLOW_FAVORITES_MAX = 8

export type WorkflowFavoriteEntry = {
  workflowId: string
  /** Epoch ms when the workflow was favorited. */
  favoritedAt: number
}

/** Hydrated row for sidebar quick-launch (current names, not stale snapshots). */
export type FavoriteWorkflowDisplay = {
  workflowId: string
  favoritedAt: number
  name: string
  projectId: string
  projectName: string
  status: 'draft' | 'published'
}

export function favoritesKeyForUser(username: string): string {
  return `grasp.workflowFavorites.${username || 'anonymous'}`
}

function resolveUsername(): { name: string; settled: boolean } {
  const { user, ready } = useAuth()
  const name = user.value?.username?.trim() || ''
  if (name) return { name, settled: true }
  if (ready && ready.value === false) return { name: '', settled: false }
  return { name: 'anonymous', settled: true }
}

export function loadFavoriteEntries(username: string): WorkflowFavoriteEntry[] {
  if (typeof localStorage === 'undefined') return []
  try {
    const raw = localStorage.getItem(favoritesKeyForUser(username))
    if (!raw) return []
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    const out: WorkflowFavoriteEntry[] = []
    const seen = new Set<string>()
    for (const item of parsed) {
      if (!item || typeof item !== 'object') continue
      const workflowId = String((item as WorkflowFavoriteEntry).workflowId || '').trim()
      const favoritedAt = Number((item as WorkflowFavoriteEntry).favoritedAt)
      if (!workflowId || !Number.isFinite(favoritedAt) || seen.has(workflowId)) continue
      seen.add(workflowId)
      out.push({ workflowId, favoritedAt })
      if (out.length >= WORKFLOW_FAVORITES_MAX) break
    }
    return out
  } catch {
    return []
  }
}

function persistEntries(username: string, list: WorkflowFavoriteEntry[]) {
  if (typeof localStorage === 'undefined') return
  try {
    localStorage.setItem(favoritesKeyForUser(username), JSON.stringify(list))
  } catch {
    // Quota / private mode — ignore.
  }
}

const entries = ref<WorkflowFavoriteEntry[]>([])
const displayItems = ref<FavoriteWorkflowDisplay[]>([])
const usernameKey = ref('anonymous')
const hydrating = ref(false)
const displayError = ref<string | null>(null)

let authWatchInstalled = false
let prefsHydrated = false
let hydrateGeneration = 0

function ensureAuthWatch() {
  if (authWatchInstalled) return
  authWatchInstalled = true
  const { user, ready } = useAuth()
  watch(
    () => [ready.value, user.value?.username] as const,
    () => {
      hydrateFromStorage()
      void hydrateDisplay()
    },
  )
}

/** Load local favorites for the current (settled) username. */
export function hydrateFromStorage() {
  ensureAuthWatch()
  const { name, settled } = resolveUsername()
  if (!settled) return
  usernameKey.value = name
  entries.value = loadFavoriteEntries(name)
  prefsHydrated = true
}

function ensureHydrated() {
  if (!prefsHydrated) hydrateFromStorage()
}

/** Stored array order is the user's manual sidebar order. */
const listSorted = computed(() => entries.value.slice())

function isFavorite(workflowId: string): boolean {
  ensureHydrated()
  const id = workflowId.trim()
  if (!id) return false
  return entries.value.some((e) => e.workflowId === id)
}

function writeBack(next: WorkflowFavoriteEntry[]) {
  entries.value = next
  persistEntries(usernameKey.value, next)
}

function removeIds(ids: string[]) {
  if (!ids.length) return
  const drop = new Set(ids)
  writeBack(entries.value.filter((e) => !drop.has(e.workflowId)))
}

/**
 * Fetch ≤8 workflows by id + project names; 404/missing → silent strip & persist.
 * Network errors keep the local entry (no strip).
 */
export async function hydrateDisplay(): Promise<void> {
  ensureHydrated()
  const gen = ++hydrateGeneration
  const orderedEntries = listSorted.value
  if (!orderedEntries.length) {
    displayItems.value = []
    displayError.value = null
    return
  }

  hydrating.value = true
  displayError.value = null

  try {
    let projectNameById = new Map<string, string>()
    try {
      const projects = await api.listProjects()
      if (gen !== hydrateGeneration) return
      projectNameById = new Map(projects.map((p) => [p.id, p.name]))
    } catch {
      // Keep going with empty project names; still resolve workflows.
    }

    const missing: string[] = []
    const rows: FavoriteWorkflowDisplay[] = []

    await Promise.all(
      orderedEntries.map(async (entry) => {
        try {
          const wf = await api.getWorkflow(entry.workflowId)
          if (gen !== hydrateGeneration) return
          const projectId = (wf.projectId || '').trim()
          rows.push({
            workflowId: wf.id,
            favoritedAt: entry.favoritedAt,
            name: wf.name,
            projectId,
            projectName: projectId ? projectNameById.get(projectId) || projectId : '',
            status: wf.status === 'published' ? 'published' : 'draft',
          })
        } catch (e) {
          const status = httpStatusOf(e)
          if (status === 404) {
            missing.push(entry.workflowId)
          }
        }
      }),
    )

    if (gen !== hydrateGeneration) return

    if (missing.length) {
      removeIds(missing)
    }

    const rowsByWorkflowId = new Map(rows.map((row) => [row.workflowId, row]))
    displayItems.value = orderedEntries
      .map((entry) => rowsByWorkflowId.get(entry.workflowId))
      .filter((row): row is FavoriteWorkflowDisplay => !!row)
  } finally {
    if (gen === hydrateGeneration) {
      hydrating.value = false
    }
  }
}

/** Resolve a Workflow for launch; 404 silently strips the favorite. */
export async function getFavoriteWorkflow(workflowId: string): Promise<Workflow | null> {
  try {
    return await api.getWorkflow(workflowId)
  } catch (e) {
    if (httpStatusOf(e) === 404) {
      removeIds([workflowId])
      displayItems.value = displayItems.value.filter((d) => d.workflowId !== workflowId)
      return null
    }
    throw e
  }
}

/**
 * Shared personal workflow favorites (username-keyed localStorage).
 * Call from setup so toast/i18n are bound correctly.
 */
export function useWorkflowFavorites() {
  ensureHydrated()
  ensureAuthWatch()
  const toast = useToast()
  const { t } = useI18n()

  /**
   * Toggle favorite for a workflow.
   * @returns whether the workflow is favorited after the call (false when full reject or unfavorited).
   */
  function toggleFavorite(workflowId: string, opts?: { name?: string; silent?: boolean }): boolean {
    ensureHydrated()
    const id = workflowId.trim()
    if (!id) return false

    const existing = entries.value.find((e) => e.workflowId === id)
    if (existing) {
      writeBack(entries.value.filter((e) => e.workflowId !== id))
      displayItems.value = displayItems.value.filter((d) => d.workflowId !== id)
      if (!opts?.silent) {
        const label = opts?.name?.trim() || id
        toast.success(t('common.toast.favoriteRemoved', { name: label }))
      }
      return false
    }

    if (entries.value.length >= WORKFLOW_FAVORITES_MAX) {
      if (!opts?.silent) {
        toast.warn(t('common.toast.favoriteFull', { max: WORKFLOW_FAVORITES_MAX }))
      }
      return false
    }

    writeBack([{ workflowId: id, favoritedAt: Date.now() }, ...entries.value])
    if (!opts?.silent) {
      const label = opts?.name?.trim() || id
      toast.success(t('common.toast.favoriteAdded', { name: label }))
    }
    void hydrateDisplay()
    return true
  }

  /** Sidebar star: unfavorite only (no add, no launch). */
  function unfavorite(workflowId: string, opts?: { name?: string; silent?: boolean }): void {
    ensureHydrated()
    const id = workflowId.trim()
    if (!id || !isFavorite(id)) return
    toggleFavorite(id, opts)
  }

  /** Persist a manual move. Invalid/same-index moves are intentionally no-ops. */
  function reorderFavorites(from: number, to: number): void {
    ensureHydrated()
    if (
      !Number.isInteger(from) ||
      !Number.isInteger(to) ||
      from < 0 ||
      to < 0 ||
      from >= entries.value.length ||
      to >= entries.value.length ||
      from === to
    ) {
      return
    }

    const next = entries.value.slice()
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    writeBack(next)
    const displayByWorkflowId = new Map(displayItems.value.map((item) => [item.workflowId, item]))
    displayItems.value = next
      .map((entry) => displayByWorkflowId.get(entry.workflowId))
      .filter((item): item is FavoriteWorkflowDisplay => !!item)
  }

  return {
    entries,
    listSorted,
    displayItems,
    hydrating,
    displayError,
    isFavorite,
    toggleFavorite,
    unfavorite,
    reorderFavorites,
    hydrateDisplay,
    hydrateFromStorage,
    getFavoriteWorkflow,
    max: WORKFLOW_FAVORITES_MAX,
  }
}

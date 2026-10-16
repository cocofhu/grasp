import { computed, ref } from 'vue'
import { useAuth } from '@/lib/composables/useAuth'

/** One pending row pinned in the current user's sidebar. */
export type SidebarPinRecord = {
  itemKey: string
  /** Epoch ms. A larger value sorts earlier. */
  pinnedAt: number
}

export function sidebarPinsStorageKey(username: string): string {
  return `grasp.sidebarPendingPins.${username || 'anonymous'}`
}

export function loadSidebarPins(username: string): SidebarPinRecord[] {
  if (typeof localStorage === 'undefined') return []
  try {
    const raw = localStorage.getItem(sidebarPinsStorageKey(username))
    if (!raw) return []
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    const out: SidebarPinRecord[] = []
    const seen = new Set<string>()
    for (const item of parsed) {
      if (!item || typeof item !== 'object') continue
      const itemKey = String((item as SidebarPinRecord).itemKey || '').trim()
      const pinnedAt = Number((item as SidebarPinRecord).pinnedAt)
      if (!itemKey || !Number.isFinite(pinnedAt) || seen.has(itemKey)) continue
      seen.add(itemKey)
      out.push({ itemKey, pinnedAt })
    }
    return out
  } catch {
    return []
  }
}

export function saveSidebarPins(username: string, pins: readonly SidebarPinRecord[]) {
  if (typeof localStorage === 'undefined') return
  try {
    localStorage.setItem(sidebarPinsStorageKey(username), JSON.stringify(pins))
  } catch {
    // Quota / private mode — the in-memory order still applies for this page.
  }
}

/**
 * Pinned rows first (newest pin first). Unpinned rows keep their incoming
 * relative order, which is extras then recently updated.
 */
export function orderWithPins<T>(
  rows: readonly T[],
  keyOf: (row: T) => string,
  pins: readonly SidebarPinRecord[],
): T[] {
  const at = new Map<string, number>()
  for (const pin of pins) {
    if (!pin.itemKey || !Number.isFinite(pin.pinnedAt)) continue
    at.set(pin.itemKey, pin.pinnedAt)
  }
  const pinned: T[] = []
  const rest: T[] = []
  for (const row of rows) {
    if (at.has(keyOf(row))) pinned.push(row)
    else rest.push(row)
  }
  pinned.sort((a, b) => at.get(keyOf(b))! - at.get(keyOf(a))!)
  return [...pinned, ...rest]
}

const pinEpoch = ref(0)

function settledUsername(): string | null {
  const { user, ready } = useAuth()
  const name = user.value?.username?.trim() || ''
  if (name) return name
  if (ready.value === false) return null
  return 'anonymous'
}

/** Current user's local pending-row pins. Not written onto the run. */
export function useSidebarPins() {
  const pins = computed(() => {
    const name = settledUsername()
    // Reading pinEpoch makes a local write recompute this list.
    if (!name || pinEpoch.value < 0) return [] as SidebarPinRecord[]
    return loadSidebarPins(name)
  })

  function write(next: SidebarPinRecord[]) {
    const name = settledUsername()
    if (!name) return
    saveSidebarPins(name, next)
    pinEpoch.value++
  }

  function pin(itemKey: string) {
    const key = itemKey.trim()
    if (!key) return
    const current = pins.value.filter((row) => row.itemKey !== key)
    const max = current.reduce((highest, row) => Math.max(highest, row.pinnedAt), 0)
    const now = Date.now()
    write([...current, { itemKey: key, pinnedAt: now > max ? now : max + 1 }])
  }

  function unpin(itemKey: string) {
    const key = itemKey.trim()
    if (!key) return
    write(pins.value.filter((row) => row.itemKey !== key))
  }

  function isPinned(itemKey: string): boolean {
    return pins.value.some((row) => row.itemKey === itemKey)
  }

  return { pins, pin, unpin, isPinned }
}

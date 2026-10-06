import { nextTick, ref } from 'vue'

export const STORAGE_KEY = 'grasp-sidebar-hidden'

function readStored(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

function persist(hidden: boolean) {
  try {
    localStorage.setItem(STORAGE_KEY, hidden ? 'true' : 'false')
  } catch {
    // Private mode / quota: keep in-memory toggle; refresh falls back to expanded.
  }
}

/** Desktop-only preference. Default expanded (false). Independent of mobile drawerOpen. */
export const sidebarHidden = ref(readStored())

export function hydrateSidebarHiddenFromStorage() {
  sidebarHidden.value = readStored()
}

export function setSidebarHidden(hidden: boolean) {
  sidebarHidden.value = hidden
  persist(hidden)
}

export function hideDesktopSidebar() {
  setSidebarHidden(true)
}

export function showDesktopSidebar() {
  setSidebarHidden(false)
}

export async function focusDesktopNavControl(kind: 'hide' | 'show') {
  await nextTick()
  const testid = kind === 'hide' ? 'desktop-nav-hide' : 'floating-nav-ball'
  const el = document.querySelector(`[data-testid="${testid}"]`) as HTMLElement | null
  el?.focus()
}

export function __resetSidebarHiddenForTests() {
  sidebarHidden.value = false
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

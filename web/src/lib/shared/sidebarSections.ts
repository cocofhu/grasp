import { ref } from 'vue'

export type SidebarSection = 'running' | 'history'

export const STORAGE_KEY = 'grasp-sidebar-collapsed-sections'

function readStored(): SidebarSection[] {
  try {
    const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]')
    return Array.isArray(raw) ? raw.filter((s): s is SidebarSection => s === 'running' || s === 'history') : []
  } catch {
    return []
  }
}

function persist(sections: SidebarSection[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(sections))
  } catch {
    // Private mode / quota: keep the in-memory toggle; refresh falls back to expanded.
  }
}

/** Sidebar sections the viewer folded. Shared by the desktop sidebar and the mobile drawer. */
export const collapsedSections = ref<SidebarSection[]>(readStored())

export function isSectionCollapsed(section: SidebarSection): boolean {
  return collapsedSections.value.includes(section)
}

export function toggleSection(section: SidebarSection) {
  const next = isSectionCollapsed(section)
    ? collapsedSections.value.filter((s) => s !== section)
    : [...collapsedSections.value, section]
  collapsedSections.value = next
  persist(next)
}

export function __resetSidebarSectionsForTests() {
  collapsedSections.value = []
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

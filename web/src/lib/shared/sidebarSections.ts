import { ref } from 'vue'

export type SidebarSection = 'running' | 'auto' | 'history'

const KNOWN_SECTIONS = new Set<SidebarSection>(['running', 'auto', 'history'])

export const STORAGE_KEY = 'grasp-sidebar-collapsed-sections'

/** Drop names this build does not know, so an older preference file still opens. */
export function parseStoredSections(raw: unknown): SidebarSection[] {
  if (!Array.isArray(raw)) return []
  return raw.filter((s): s is SidebarSection => typeof s === 'string' && KNOWN_SECTIONS.has(s as SidebarSection))
}

function readStored(): SidebarSection[] {
  try {
    return parseStoredSections(JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]'))
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

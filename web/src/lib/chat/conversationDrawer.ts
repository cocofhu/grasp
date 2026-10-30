import { reactive } from 'vue'

/** Which home the shared conversation drawer is showing. Null means closed. */
export type ConversationDrawerMode = 'resources' | 'artifacts' | null

export interface ConversationDrawerEntry {
  /** Sandbox-stage hosts opt in; embed and gate approval leave this false. */
  split: boolean
  mode: ConversationDrawerMode
  /** Bumped when the conversation changes so the stage drops the previous preview. */
  resetEpoch: number
}

const drawers = reactive(new Map<string, ConversationDrawerEntry>())

export type RememberedPanelMode = Exclude<ConversationDrawerMode, null>

interface RememberedPanel { open: boolean; mode: RememberedPanelMode }
/** Chats are oldest first. Stored as an array so numeric ids keep that order. */
interface PanelMemory { last: RememberedPanel | null; chats: Map<string, RememberedPanel> }

export const PANEL_MEMORY_STORAGE_KEY = 'grasp.conversation.drawer'
/** Oldest chats drop first so the stored list stays small. */
export const PANEL_MEMORY_LIMIT = 200

function parsePanel(raw: unknown): RememberedPanel | null {
  if (!raw || typeof raw !== 'object') return null
  const { open, mode } = raw as Partial<RememberedPanel>
  if (typeof open !== 'boolean') return null
  return { open, mode: mode === 'resources' ? 'resources' : 'artifacts' }
}

/**
 * Drawer switch with the home it last showed, kept per chat plus the last one used.
 * A chat without its own entry follows the last one. Read from localStorage each
 * time so other tabs' writes are not overwritten.
 */
function loadMemory(): PanelMemory {
  const memory: PanelMemory = { last: null, chats: new Map() }
  try {
    const parsed = JSON.parse(localStorage.getItem(PANEL_MEMORY_STORAGE_KEY) || 'null') as { last?: unknown; chats?: unknown } | null
    memory.last = parsePanel(parsed?.last)
    if (Array.isArray(parsed?.chats)) {
      for (const row of parsed.chats as unknown[]) {
        if (!Array.isArray(row) || typeof row[0] !== 'string') continue
        const panel = parsePanel(row[1])
        if (panel) memory.chats.set(row[0], panel)
      }
    }
  } catch { /* private storage or bad JSON */ }
  return memory
}

function saveMemory(memory: PanelMemory) {
  try {
    localStorage.setItem(PANEL_MEMORY_STORAGE_KEY, JSON.stringify({ last: memory.last, chats: [...memory.chats] }))
  } catch { /* private storage */ }
}

function readPanel(chatKey?: string): RememberedPanel | null {
  const memory = loadMemory()
  const key = keyOf(chatKey)
  return (key && memory.chats.get(key)) || memory.last
}

/** Read the switch for a chat, falling back to the last one used. Missing means collapsed. */
export function readArtifactPanelOpen(chatKey?: string): boolean {
  return readPanel(chatKey)?.open === true
}

/** Home the drawer last showed for a chat. Missing means artifacts. */
export function readRememberedPanelMode(chatKey?: string): RememberedPanelMode {
  return readPanel(chatKey)?.mode ?? 'artifacts'
}

/** Write only when the user opens or closes the drawer. Closing keeps the last home. */
export function writeArtifactPanelOpen(open: boolean, mode?: RememberedPanelMode, chatKey?: string): void {
  const memory = loadMemory()
  const key = keyOf(chatKey)
  const panel = { open, mode: mode ?? ((key && memory.chats.get(key)) || memory.last)?.mode ?? 'artifacts' }
  memory.last = panel
  if (key) {
    memory.chats.delete(key)
    memory.chats.set(key, panel)
    for (const stale of [...memory.chats.keys()].slice(0, Math.max(0, memory.chats.size - PANEL_MEMORY_LIMIT))) memory.chats.delete(stale)
  }
  saveMemory(memory)
}

function keyOf(runId?: string | null): string {
  return String(runId || '').trim()
}

/** Shared drawer state for a run. The stage and the conversation shell are not in one inject tree. */
export function ensureConversationDrawer(runId?: string | null): ConversationDrawerEntry | null {
  const id = keyOf(runId)
  if (!id) return null
  let entry = drawers.get(id)
  if (!entry) {
    drawers.set(id, { split: false, mode: null, resetEpoch: 0 })
    entry = drawers.get(id)
  }
  return entry ?? null
}

export function peekConversationDrawer(runId?: string | null): ConversationDrawerEntry | null {
  const id = keyOf(runId)
  if (!id) return null
  return drawers.get(id) ?? null
}

export function resetConversationDrawers(): void {
  drawers.clear()
  try { localStorage.removeItem(PANEL_MEMORY_STORAGE_KEY) } catch { /* private storage */ }
}

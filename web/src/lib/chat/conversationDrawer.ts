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

/**
 * One artifact-panel switch for the current page session.
 * Not keyed by run. Memory only: a full reload starts closed.
 * null means the user has not opened or closed it yet.
 */
let artifactPanelSession: boolean | null = null

/** Read the session switch. Missing means collapsed. */
export function readArtifactPanelOpen(): boolean {
  return artifactPanelSession === true
}

/** Write only when the user opens or closes the artifact panel. */
export function writeArtifactPanelOpen(open: boolean): void {
  artifactPanelSession = open
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
  artifactPanelSession = null
}

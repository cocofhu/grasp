import { computed, reactive, toValue, type MaybeRefOrGetter } from 'vue'
import { api } from '@/lib/api/api'
import { i18n } from '@/lib/shared/i18n'
import type { Artifact, RunArtifactSaveResult } from '@/lib/shared/types'
import { sameArtifactContent } from './artifactEdit'

export interface ArtifactDraft {
  name: string
  artifactId: string
  kind: Artifact['kind']
  /** Server body the draft started from (or was last reloaded / saved to). */
  base: string
  baseEtag: string
  baseRevision: number
  content: string
  loading: boolean
  saving: boolean
  error: string
  /** Server field path (e.g. `goals[0].title`) → message. */
  fieldErrors: Record<string, string>
  /** Set when the server rejected If-Match; holds the live revision. */
  conflict: { revision: number } | null
  /** The artifact no longer exists on the server. */
  missing: boolean
}

export type ArtifactSaveOutcome =
  | { ok: true; result: RunArtifactSaveResult }
  | { ok: false; code: string }

interface RunEditorState {
  drafts: Record<string, ArtifactDraft>
  /** Conversation busy flags published by mounted chats (key: nodeId). */
  busy: Record<string, boolean>
}

const states = new Map<string, RunEditorState>()

function stateFor(runId: string): RunEditorState {
  let s = states.get(runId)
  if (!s) {
    s = reactive({ drafts: {}, busy: {} }) as RunEditorState
    states.set(runId, s)
  }
  return s
}

function isDraftDirty(d: ArtifactDraft): boolean {
  return !d.loading && !sameArtifactContent(d.kind, d.base, d.content)
}

/** Any run has unsaved artifact edits (used by the beforeunload guard). */
export function hasAnyDirtyArtifactDraft(): boolean {
  for (const s of states.values()) {
    if (Object.values(s.drafts).some(isDraftDirty)) return true
  }
  return false
}

let unloadBound = false
function bindBeforeUnload() {
  if (unloadBound || typeof window === 'undefined') return
  unloadBound = true
  window.addEventListener('beforeunload', (e) => {
    if (!hasAnyDirtyArtifactDraft()) return
    e.preventDefault()
    e.returnValue = ''
  })
}

/** Publish (or clear with null) a mounted chat's busy flag for a run. */
export function setArtifactSessionBusy(runId: string, key: string, busy: boolean | null) {
  if (!runId) return
  const s = stateFor(runId)
  if (busy === null) delete s.busy[key]
  else s.busy[key] = busy
}

/** Test hook: drop every shared draft. */
export function resetArtifactEditorsForTest() {
  states.clear()
}

/**
 * Per-run shared artifact drafts. Every caller with the same runId sees the same
 * drafts, so split panes and the chat composer agree on what is unsaved.
 */
export function useArtifactEditor(runIdSource: MaybeRefOrGetter<string | null | undefined>) {
  bindBeforeUnload()
  const runId = computed(() => String(toValue(runIdSource) || ''))
  const state = computed(() => (runId.value ? stateFor(runId.value) : null))

  const drafts = computed<Record<string, ArtifactDraft>>(() => state.value?.drafts ?? {})
  const dirtyNames = computed(() => Object.values(drafts.value).filter(isDraftDirty).map((d) => d.name))
  const hasDirty = computed(() => dirtyNames.value.length > 0)
  /** Busy flag from mounted chats; null when no chat for this run is mounted. */
  const sessionBusy = computed<boolean | null>(() => {
    const flags = Object.values(state.value?.busy ?? {})
    return flags.length ? flags.some(Boolean) : null
  })

  function draft(name: string): ArtifactDraft | undefined {
    return drafts.value[name]
  }

  function isDirty(name: string): boolean {
    const d = drafts.value[name]
    return !!d && isDraftDirty(d)
  }

  async function fetchBase(d: ArtifactDraft) {
    d.loading = true
    try {
      const full = await api.artifactContent(d.artifactId)
      d.base = String(full.content ?? '')
      d.content = d.base
      d.baseEtag = String(full.etag || '')
      d.baseRevision = Number(full.revision || d.baseRevision || 1)
      d.missing = false
    } catch (e) {
      const status = (e as { status?: number }).status
      if (status === 404) d.missing = true
      d.error = e instanceof Error ? e.message : String(e)
    } finally {
      d.loading = false
    }
  }

  /** Start editing (or rejoin an existing draft for the same name). */
  async function begin(artifact: Pick<Artifact, 'id' | 'name' | 'kind' | 'revision'>): Promise<ArtifactDraft | null> {
    const s = state.value
    if (!s) return null
    const existing = s.drafts[artifact.name]
    if (existing) return existing
    s.drafts[artifact.name] = {
      name: artifact.name,
      artifactId: artifact.id,
      kind: artifact.kind,
      base: '',
      baseEtag: '',
      baseRevision: Number(artifact.revision || 1),
      content: '',
      loading: true,
      saving: false,
      error: '',
      fieldErrors: {},
      conflict: null,
      missing: false,
    }
    const d = s.drafts[artifact.name]!
    await fetchBase(d)
    return d
  }

  function update(name: string, content: string) {
    const d = drafts.value[name]
    if (!d) return
    d.content = content
    if (d.error && !d.conflict) d.error = ''
    if (Object.keys(d.fieldErrors).length) d.fieldErrors = {}
  }

  function discard(name: string) {
    const s = state.value
    if (s) delete s.drafts[name]
  }

  function discardAll() {
    const s = state.value
    if (!s) return
    for (const k of Object.keys(s.drafts)) delete s.drafts[k]
  }

  /** "Load latest": drop local edits and restart from the server body. */
  async function reloadLatest(name: string) {
    const d = drafts.value[name]
    if (!d) return
    d.conflict = null
    d.error = ''
    d.fieldErrors = {}
    await fetchBase(d)
  }

  /** Save one draft. `force` skips If-Match (the "save anyway" path after a conflict). */
  async function save(name: string, opts: { force?: boolean } = {}): Promise<ArtifactSaveOutcome> {
    const d = drafts.value[name]
    if (!d || !runId.value) return { ok: false, code: 'not_editing' }
    if (d.saving) return { ok: false, code: 'saving' }
    d.saving = true
    d.error = ''
    d.fieldErrors = {}
    try {
      const result = await api.saveRunArtifact(runId.value, name, d.content, opts.force ? undefined : d.baseEtag || undefined)
      discard(name)
      return { ok: true, result }
    } catch (e) {
      const err = e as Error & { status?: number; code?: string; revision?: number; field?: string }
      const code = String(err.code || '')
      d.error = err.message || String(e)
      if (code === 'artifact_conflict') {
        d.conflict = { revision: Number(err.revision || 0) }
      } else if (code === 'artifact_not_found' || err.status === 404) {
        d.missing = true
      } else if (err.field) {
        d.fieldErrors = { [err.field]: d.error }
      }
      return { ok: false, code: code || String(err.status || 'error') }
    } finally {
      if (drafts.value[name]) d.saving = false
    }
  }

  /** Save every dirty draft; resolves false (and stops) on the first failure. */
  async function saveAll(): Promise<{ ok: boolean; failed?: string; results: RunArtifactSaveResult[] }> {
    const results: RunArtifactSaveResult[] = []
    for (const name of [...dirtyNames.value]) {
      const r = await save(name)
      if (!r.ok) return { ok: false, failed: name, results }
      results.push(r.result)
    }
    return { ok: true, results }
  }

  /**
   * Ask before throwing away unsaved edits. Returns true when the caller may
   * proceed; on confirm the affected drafts are discarded.
   */
  function confirmLeave(names?: string[]): boolean {
    const targets = (names ?? dirtyNames.value).filter(isDirty)
    if (!targets.length) return true
    const ok = typeof window === 'undefined' ? true : window.confirm(
      i18n.global.t('pages.artifactEditor.confirmLeave', { names: targets.join(', ') }),
    )
    if (ok) for (const n of targets) discard(n)
    return ok
  }

  function setSessionBusy(key: string, busy: boolean) {
    const s = state.value
    if (s) s.busy[key] = busy
  }

  function clearSessionBusy(key: string) {
    const s = state.value
    if (s) delete s.busy[key]
  }

  return {
    runId,
    drafts,
    dirtyNames,
    hasDirty,
    sessionBusy,
    draft,
    isDirty,
    begin,
    update,
    discard,
    discardAll,
    reloadLatest,
    save,
    saveAll,
    confirmLeave,
    setSessionBusy,
    clearSessionBusy,
  }
}

export type ArtifactEditorApi = ReturnType<typeof useArtifactEditor>

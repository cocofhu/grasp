/** Per-tab view state of each Live session: survives reloads, never shared. */

export type Mode = 'inplace' | 'compare'

export type SessionView = {
  current: number
  mode: Mode
  params: Record<string, Record<string, string | number>>
}

const KEY = '__grasp_live'
const MAX = 20

type Saved = Record<string, SessionView & { at: number }>

function load(): Saved {
  try {
    const v = JSON.parse(sessionStorage.getItem(KEY) || '{}')
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Saved) : {}
  } catch {
    return {}
  }
}

function save(all: Saved) {
  const keys = Object.keys(all).sort((a, b) => all[b].at - all[a].at)
  const trimmed: Saved = {}
  for (const k of keys.slice(0, MAX)) trimmed[k] = all[k]
  try {
    sessionStorage.setItem(KEY, JSON.stringify(trimmed))
  } catch {
    // Storage blocked: views last until reload.
  }
}

export function getView(sid: string): SessionView | null {
  const v = load()[sid]
  if (!v || typeof v.current !== 'number') return null
  return { current: v.current, mode: v.mode === 'compare' ? 'compare' : 'inplace', params: v.params && typeof v.params === 'object' ? v.params : {} }
}

export function putView(sid: string, view: SessionView) {
  const all = load()
  all[sid] = { ...view, at: Date.now() }
  save(all)
}

export function dropView(sid: string) {
  const all = load()
  if (!(sid in all)) return
  delete all[sid]
  save(all)
}

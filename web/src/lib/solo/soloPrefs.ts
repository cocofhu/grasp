import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'

export type HomeMode = 'workflow' | 'solo'

export function backendLabel(id: string | undefined): string {
  return ACP_BACKENDS.find((b) => b.id === id)?.label ?? String(id || '')
}

const MODE_KEY = 'grasp.home.mode'
const AGENT_KEY = 'grasp.home.lastSoloAgent'
const PROJECT_KEY = 'grasp.home.lastSoloProject'
const PROJECTS_KEY = 'grasp.home.lastSoloProjects'

function read(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function write(key: string, value: string) {
  try {
    if (value) localStorage.setItem(key, value)
    else localStorage.removeItem(key)
  } catch {
    /* private mode */
  }
}

function cleanIds(ids: string[]): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const item of ids) {
    const id = String(item || '').trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    out.push(id)
  }
  return out
}

/** Ordered repository ids from the last launch. Falls back to the old single id. */
export function readLastProjects(): string[] {
  try {
    const raw = localStorage.getItem(PROJECTS_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as unknown
      if (Array.isArray(parsed)) return cleanIds(parsed.filter((item): item is string => typeof item === 'string'))
    }
  } catch {
    /* private mode or bad json */
  }
  const one = read(PROJECT_KEY).trim()
  return one ? [one] : []
}

export function writeLastProjects(ids: string[]) {
  const clean = cleanIds(ids)
  try {
    if (clean.length) localStorage.setItem(PROJECTS_KEY, JSON.stringify(clean))
    else localStorage.removeItem(PROJECTS_KEY)
  } catch {
    /* private mode */
  }
  write(PROJECT_KEY, clean[0] || '')
}

export const soloPrefs = {
  homeMode: (): HomeMode => (read(MODE_KEY) === 'solo' ? 'solo' : 'workflow'),
  setHomeMode: (m: HomeMode) => write(MODE_KEY, m),
  lastAgent: () => read(AGENT_KEY),
  setLastAgent: (name: string) => write(AGENT_KEY, name),
  lastProject: () => readLastProjects()[0] || '',
  setLastProject: (id: string) => writeLastProjects(id ? [id] : []),
  lastProjects: () => readLastProjects(),
  setLastProjects: (ids: string[]) => writeLastProjects(ids),
}

/** "github.com/acme/app" style short form of a repo URL. */
export function repoShort(url: string): string {
  const s = String(url || '').trim()
  if (!s) return ''
  const scp = /^[\w.-]+@([^:]+):(.+)$/.exec(s)
  const body = scp ? `${scp[1]}/${scp[2]}` : s.replace(/^[a-z+]+:\/\//i, '').replace(/^[^@/]+@/, '')
  return body.replace(/\.git$/i, '').replace(/\/+$/, '')
}

/** Expected repo host family, used to warn on URL / access-mode mismatch. */
export function repoProtocol(url: string): 'https' | 'ssh' | '' {
  const s = String(url || '').trim()
  if (/^https?:\/\//i.test(s)) return 'https'
  if (/^ssh:\/\//i.test(s) || /^[\w.-]+@[^:]+:/.test(s)) return 'ssh'
  return ''
}

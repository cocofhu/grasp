import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'

export type HomeMode = 'workflow' | 'solo'

export function backendLabel(id: string | undefined): string {
  return ACP_BACKENDS.find((b) => b.id === id)?.label ?? String(id || '')
}

const MODE_KEY = 'grasp.home.mode'
const AGENT_KEY = 'grasp.home.lastSoloAgent'
const PROJECT_KEY = 'grasp.home.lastSoloProject'

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

export const soloPrefs = {
  homeMode: (): HomeMode => (read(MODE_KEY) === 'solo' ? 'solo' : 'workflow'),
  setHomeMode: (m: HomeMode) => write(MODE_KEY, m),
  lastAgent: () => read(AGENT_KEY),
  setLastAgent: (name: string) => write(AGENT_KEY, name),
  lastProject: () => read(PROJECT_KEY),
  setLastProject: (id: string) => write(PROJECT_KEY, id),
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

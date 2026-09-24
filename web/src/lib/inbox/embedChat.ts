import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import type { ThemeName } from '@/lib/shared/theme'

/** Must match server handlers.headerEmbedRequest. */
const EMBED_REQUEST_HEADER = 'X-Grasp-Embed'
const STORAGE_PREFIX = 'grasp.embed.'

export const EMBED_PICK_MESSAGE = 'grasp-embed:pick'
export const EMBED_READY_MESSAGE = 'grasp-embed:ready'
export const EMBED_THEME_MESSAGE = 'grasp-embed:theme'

export type EmbedTicket = {
  ticket: string
  runId: string
  nodeId: string
  expiresAt: string
}

export type EmbedSession = {
  token: string
  expiresAt: string
}

type RedeemResponse = EmbedSession & { kind?: string; runId?: string; nodeId?: string; error?: string }

/** Drawer page path; the ticket rides in the fragment so it never reaches a server log. */
export function embedChatPath(runId: string, nodeId: string): string {
  return `/embed/runs/${encodeURIComponent(runId)}/nodes/${encodeURIComponent(nodeId)}/chat`
}

/** Direct preview URL carrying a drawer ticket for the in-page pick script (preview-pick.js). */
export function directPreviewEmbedUrl(directUrl: string, t: EmbedTicket, theme: ThemeName): string {
  const base = directUrl.split('#')[0]
  const q = new URLSearchParams({ run: t.runId, node: t.nodeId, ticket: t.ticket, theme })
  return `${base}#__grasp_embed&${q.toString()}`
}

function hashParams(hash: string): URLSearchParams {
  return new URLSearchParams((hash || '').replace(/^#/, ''))
}

export function parseEmbedTicketFromHash(hash: string): string {
  return hashParams(hash).get('ticket')?.trim() || ''
}

function asTheme(v: unknown): ThemeName | null {
  return v === 'dark' || v === 'light' ? v : null
}

export function parseEmbedThemeFromHash(hash: string): ThemeName | null {
  return asTheme(hashParams(hash).get('theme'))
}

export function parseEmbedThemeMessage(data: unknown): ThemeName | null {
  if (!data || typeof data !== 'object') return null
  const m = data as { type?: unknown; theme?: unknown }
  return m.type === EMBED_THEME_MESSAGE ? asTheme(m.theme) : null
}

function storageKey(runId: string, nodeId: string): string {
  return `${STORAGE_PREFIX}${runId}.${nodeId}`
}

export function loadEmbedSession(runId: string, nodeId: string, now = Date.now()): EmbedSession | null {
  try {
    const raw = sessionStorage.getItem(storageKey(runId, nodeId))
    if (!raw) return null
    const s = JSON.parse(raw) as Partial<EmbedSession>
    if (typeof s.token !== 'string' || !s.token || typeof s.expiresAt !== 'string') return null
    if (Date.parse(s.expiresAt) <= now) {
      clearEmbedSession(runId, nodeId)
      return null
    }
    return { token: s.token, expiresAt: s.expiresAt }
  } catch {
    return null
  }
}

export function saveEmbedSession(runId: string, nodeId: string, s: EmbedSession): void {
  try {
    sessionStorage.setItem(storageKey(runId, nodeId), JSON.stringify(s))
  } catch {
    // Storage blocked (third-party iframe policy): the session lasts until reload.
  }
}

export function clearEmbedSession(runId: string, nodeId: string): void {
  try {
    sessionStorage.removeItem(storageKey(runId, nodeId))
  } catch {
    // ignore
  }
}

/** Trade a one-shot ticket for a drawer token. Resolves null when the ticket is spent or unknown. */
export async function redeemEmbedTicket(ticket: string, runId: string, nodeId: string): Promise<EmbedSession | null> {
  const res = await fetch('/embed-api/session', {
    method: 'POST',
    credentials: 'omit',
    headers: { 'Content-Type': 'application/json', [EMBED_REQUEST_HEADER]: '1' },
    body: JSON.stringify({ ticket }),
  })
  if (res.status === 401 || res.status === 403) return null
  if (!res.ok) throw Object.assign(new Error(`${res.status}`), { status: res.status })
  const body = (await res.json()) as RedeemResponse
  if (!body.token || body.runId !== runId || body.nodeId !== nodeId) return null
  return { token: body.token, expiresAt: body.expiresAt }
}

/** Validate a pick relayed by the preview page before it becomes an annotation. */
export function parseEmbedPickMessage(data: unknown): AppPreviewPickPayload | null {
  if (!data || typeof data !== 'object') return null
  const m = data as { type?: unknown; payload?: unknown }
  if (m.type !== EMBED_PICK_MESSAGE || !m.payload || typeof m.payload !== 'object') return null
  const p = m.payload as Record<string, unknown>
  if (typeof p.selector !== 'string' || !p.selector.trim()) return null
  const str = (v: unknown) => (typeof v === 'string' ? v : undefined)
  return {
    selector: p.selector,
    tagName: str(p.tagName) || '',
    outerHTML: str(p.outerHTML) || '',
    text: str(p.text),
    url: str(p.url),
  }
}

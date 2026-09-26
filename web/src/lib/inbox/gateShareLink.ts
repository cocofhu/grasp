import type { Artifact, ClarifyInboxItem, GateInboxItem, GateShareInboxStatus, InboxItem } from '@/lib/shared/types'
import {
  GRASP_STORAGE_KEYS,
  LEGACY_STORAGE_KEYS,
  migrateSessionStorageKey,
  migrateSessionStoragePrefix,
} from '@/lib/shared/migrateBrandStorage'
import type { EmbedTicket } from '@/lib/inbox/embedChat'

export const GATE_SHARE_TTL_TIERS = ['1h', '8h', '24h', '72h', '7d'] as const
export type GateShareTTLTier = (typeof GATE_SHARE_TTL_TIERS)[number]
export const DEFAULT_GATE_SHARE_TTL: GateShareTTLTier = '24h'

export const GATE_SHARE_PERMISSION_PRESETS = ['full', 'react_only'] as const
export type GateSharePermissionPreset = (typeof GATE_SHARE_PERMISSION_PRESETS)[number]
export const DEFAULT_GATE_SHARE_PERMISSION: GateSharePermissionPreset = 'full'

/** Normalize API / stored preset; empty/unknown ⇒ full (legacy compatible). */
export function normalizePermissionPreset(
  raw?: string | null,
): GateSharePermissionPreset {
  return raw === 'react_only' ? 'react_only' : 'full'
}

const GATE_SHARE_TOKEN_HEADER = 'X-Gate-Share-Token'
const GATE_SHARE_REQUEST_HEADER = 'X-Gate-Share-Requested'

const SHARE_URL_STORAGE_PREFIX = GRASP_STORAGE_KEYS.gateShareUrlPrefix

migrateSessionStoragePrefix(
  LEGACY_STORAGE_KEYS.gateShareUrlPrefix,
  SHARE_URL_STORAGE_PREFIX,
)

/** In-memory cache plus sessionStorage so refresh can copy the same active URL. */
const shareUrlMemory = new Map<string, string>()

function shareMemoryKey(runId: string, nodeId: string, iteration?: number): string {
  return `${runId}:${nodeId}:${iteration ?? 0}`
}

function storageKey(runId: string, nodeId: string, iteration?: number): string {
  return SHARE_URL_STORAGE_PREFIX + shareMemoryKey(runId, nodeId, iteration)
}

export function rememberShareUrl(runId: string, nodeId: string, iteration: number | undefined, url: string) {
  const u = url.trim()
  if (!u) return
  const memKey = shareMemoryKey(runId, nodeId, iteration)
  shareUrlMemory.set(memKey, u)
  try {
    sessionStorage.setItem(storageKey(runId, nodeId, iteration), u)
  } catch {
    /* private mode / quota */
  }
}

export function recallShareUrl(runId: string, nodeId: string, iteration?: number): string {
  const mem = shareUrlMemory.get(shareMemoryKey(runId, nodeId, iteration))
  if (mem) return mem
  try {
    const key = storageKey(runId, nodeId, iteration)
    const legacy = LEGACY_STORAGE_KEYS.gateShareUrlPrefix + shareMemoryKey(runId, nodeId, iteration)
    migrateSessionStorageKey(legacy, key)
    return sessionStorage.getItem(key) || ''
  } catch {
    return ''
  }
}

export function forgetShareUrl(runId: string, nodeId: string, iteration?: number) {
  shareUrlMemory.delete(shareMemoryKey(runId, nodeId, iteration))
  try {
    sessionStorage.removeItem(storageKey(runId, nodeId, iteration))
  } catch {
    /* ignore */
  }
}

export function isHumanGateInboxItem(item: InboxItem | null | undefined): item is GateInboxItem {
  return !!item && item.type === 'gate' && item.nodeType === 'human_gate'
}

function isReviewInboxItem(item: InboxItem | null | undefined): item is ClarifyInboxItem {
  return !!item && item.type === 'clarify' && item.kind === 'review'
}

function isAppPreviewInboxItem(item: InboxItem | null | undefined): item is ClarifyInboxItem {
  return !!item && item.type === 'clarify' && item.kind === 'app_preview'
}

/** Inbox 待澄清: kind=clarify, or legacy items with missing kind (not review/app_preview). */
function isClarifyInboxItem(item: InboxItem | null | undefined): item is ClarifyInboxItem {
  return !!item && item.type === 'clarify' && item.kind !== 'review' && item.kind !== 'app_preview'
}

/** Inbox share entry: human_gate, 待复审, app_preview, or 待澄清 (review share API). */
export function isShareableInboxItem(item: InboxItem | null | undefined): boolean {
  return (
    isHumanGateInboxItem(item) ||
    isReviewInboxItem(item) ||
    isAppPreviewInboxItem(item) ||
    isClarifyInboxItem(item)
  )
}

/** Clarify / app preview / Inbox review all mint ShareLinkKindReview links — never /gates. */
export function inboxShareKind(item: InboxItem | null | undefined): 'human_gate' | 'review' {
  return isReviewInboxItem(item) || isAppPreviewInboxItem(item) || isClarifyInboxItem(item)
    ? 'review'
    : 'human_gate'
}

const SHARE_API_ERROR_KEYS: Record<string, string> = {
  no_standard_action: 'pages.gatesInbox.share.errors.noStandardAction',
  not_human_gate: 'pages.gatesInbox.share.errors.notHumanGate',
  not_review_session: 'pages.gatesInbox.share.errors.notReviewSession',
  used_readonly: 'pages.gatesInbox.share.errors.usedReadonly',
  run_ended: 'pages.gatesInbox.share.errors.runEnded',
  gate_not_pending: 'pages.gatesInbox.share.errors.gateNotPending',
  review_not_pending: 'pages.gatesInbox.share.errors.reviewNotPending',
  review_busy: 'pages.gatesInbox.share.errors.reviewBusy',
  review_validation_failed: 'pages.gatesInbox.share.errors.reviewValidationFailed',
  invalid_ttl: 'pages.gatesInbox.share.errors.invalidTtl',
  invalid_permission_preset: 'pages.gatesInbox.share.errors.invalidPermissionPreset',
  permission_denied: 'pages.gatesInbox.share.errors.permissionDenied',
  not_active: 'pages.gatesInbox.share.errors.notActive',
  not_found: 'pages.gatesInbox.share.errors.notFound',
  share_failed: 'pages.gatesInbox.share.errors.shareFailed',
}

export function shareApiErrorMessage(
  err: unknown,
  t: (key: string, values?: Record<string, unknown>) => string,
): string {
  const msg = err instanceof Error ? err.message : String(err || '')
  const code = msg.trim()
  const key = SHARE_API_ERROR_KEYS[code]
  if (key) return t(key)
  return msg || t('pages.gatesInbox.share.errors.shareFailed')
}

/** Mask fragment token so the management panel never shows plaintext by default. */
export function maskShareUrl(url: string): string {
  const i = url.indexOf('#t=')
  if (i < 0) return url
  return `${url.slice(0, i + 3)}••••••••`
}

/**
 * Loopback hosts that must not be one-click copied for external share.
 * Only localhost / 127.0.0.1 / ::1 (with or without port); general LAN is allowed.
 */
export function isLoopbackHostname(hostname: string): boolean {
  const host = hostname.trim().toLowerCase().replace(/^\[|\]$/g, '')
  return host === 'localhost' || host === '127.0.0.1' || host === '::1'
}

/** True when a share URL (or host:port) resolves to a loopback hostname. */
export function isLoopbackShareHost(urlOrHost: string): boolean {
  const raw = urlOrHost.trim()
  if (!raw) return false
  try {
    const u = raw.includes('://') ? new URL(raw) : new URL(`http://${raw}`)
    return isLoopbackHostname(u.hostname)
  } catch {
    return false
  }
}

/** Read token from `#t=…` only — never from path/query. */
export function parseShareTokenFromHash(hash: string): string {
  const raw = (hash || '').startsWith('#') ? hash.slice(1) : hash || ''
  if (!raw) return ''
  const params = new URLSearchParams(raw)
  return (params.get('t') || '').trim()
}

export function isGateShareActive(st?: GateShareInboxStatus | null): boolean {
  return st?.state === 'active'
}

export function canCreateGateShare(st?: GateShareInboxStatus | null): boolean {
  if (!st) return true
  if (st.state === 'used') return false
  if (st.canCreate != null) return !!st.canCreate
  return st.state === 'none' || st.state === 'revoked' || st.state === 'expired'
}

/** Seconds left until expiresAt; falls back to remainingSec snapshot. */
export function remainingSecFromExpiresAt(
  expiresAt?: string,
  remainingSec?: number,
  nowMs: number = Date.now(),
): number | undefined {
  if (expiresAt) {
    const ms = Date.parse(expiresAt)
    if (!Number.isNaN(ms)) return Math.max(0, Math.floor((ms - nowMs) / 1000))
  }
  if (typeof remainingSec === 'number') return Math.max(0, Math.floor(remainingSec))
  return undefined
}

export function formatRemainingSec(
  sec: number | undefined,
  t: (key: string, values?: Record<string, unknown>) => string,
): string {
  const n = Math.max(0, Math.floor(sec ?? 0))
  if (n <= 0) return t('pages.gatesInbox.share.expired')
  const days = Math.floor(n / 86400)
  if (days >= 2) return t('pages.gatesInbox.share.remainingDays', { n: days })
  const hours = Math.floor(n / 3600)
  if (hours >= 1) return t('pages.gatesInbox.share.remainingHours', { n: hours })
  const mins = Math.max(1, Math.floor(n / 60))
  return t('pages.gatesInbox.share.remainingMinutes', { n: mins })
}

export function shareStatusLabel(
  st: GateShareInboxStatus | undefined,
  t: (key: string, values?: Record<string, unknown>) => string,
): string {
  const state = st?.state || 'none'
  if (state === 'active') {
    return t('pages.gatesInbox.share.stateActive', {
      remaining: formatRemainingSec(st?.remainingSec, t),
    })
  }
  if (state === 'used') return t('pages.gatesInbox.share.stateUsed')
  if (state === 'revoked') return t('pages.gatesInbox.share.stateRevoked')
  if (state === 'expired') return t('pages.gatesInbox.share.stateExpired')
  return t('pages.gatesInbox.share.stateNone')
}

export type PublicGatePreviewTurn = {
  role: 'agent' | 'human' | string
  text?: string
  at?: string
  interrupted?: boolean
  images?: Array<{
    data?: string
    mimeType?: string
    name?: string
    ref?: string
  }>
  annotations?: Array<{
    selector?: string
    jsonPath?: string
    label?: string
    note?: string
    quote?: string
    url?: string
    truncated?: boolean
  }>
}

export type PublicGateQueueItem = {
  id?: string
  text?: string
  images?: PublicGatePreviewTurn['images']
  annotations?: PublicGatePreviewTurn['annotations']
}

export type PublicGateActiveItem = {
  id?: string
  text?: string
  images?: PublicGatePreviewTurn['images']
  annotations?: PublicGatePreviewTurn['annotations']
}

export type PublicGatePreview = {
  status: string
  kind?: 'human_gate' | 'review' | string
  title?: string
  description?: string
  remainingSec?: number
  expiresAt?: string
  /** Link-level capability: full | react_only. Absent/empty ⇒ full. */
  permissionPreset?: 'full' | 'react_only' | string
  actions?: { approve?: string; reject?: string; confirm?: string; reply?: string; cancel?: string }
  visualHtml?: string
  /** Content hash for sparse silent poll; always present when server computed it. */
  visualHtmlHash?: string
  /** Structured-doc hash for sparse silent poll (bodies may be omitted). */
  structuredHash?: string
  /** Turns hash for sparse silent poll (bodies may be omitted). */
  turnsHash?: string
  structured?: {
    name?: string
    title?: string
    goals?: unknown
    text?: string
    description?: string
    doc?: Record<string, unknown>
  }
  nonce?: string
  turns?: PublicGatePreviewTurn[]
  upstream?: {
    name?: string
    title?: string
    summary?: string
    description?: string
    text?: string
    /** Present only on on-demand /upstream; open/poll preview never embeds doc. */
    doc?: Record<string, unknown>
  }
  /** Summary upstream hash for sparse silent poll. */
  upstreamHash?: string
  reactSessionAlive?: boolean
  sessionBusy?: boolean
  waiting?: number
  queueItems?: PublicGateQueueItem[]
  activeItem?: PublicGateActiveItem | null
  productKind?: 'visual' | 'structured' | 'app_preview' | string
  productName?: string
  /** Desensitized app_preview ports for public remote / API iframe. */
  ports?: PublicPreviewPort[]
  /** Graph node type from preview DTO; react ⇒ 待澄清. Kind stays review. */
  nodeType?: string
  /** In-flight ACP rails (message/thought) while sessionBusy — poll fallback. */
  liveEvents?: PublicGateLiveEvent[]
}

export type PublicGateLiveEvent = {
  kind: 'message' | 'thought' | string
  text?: string
}

export type PublicPreviewPort = {
  port: number
  label?: string
  kind?: 'port' | 'url' | string
  url?: string
  /** vnc = remote+pick; api = same-origin iframe (no pick). */
  mode?: 'vnc' | 'api' | 'url' | string
  /** IP-direct iframe target when the node switch is on. */
  directUrl?: string
}

export type PublicPreviewTicketResult = {
  status: string
  ticket?: string
  expiresAt?: string
  port?: number
  mode?: string
  wsPath?: string
  iframePath?: string
  error?: string
  message?: string
}

export type PublicGateUpstreamResult = {
  status: string
  upstream?: PublicGatePreview['upstream'] | null
  error?: string
  message?: string
}

export type PublicGateDecideResult = {
  status: string
  action?: string
  alreadyProcessed?: boolean
  error?: string
  message?: string
  kind?: string
  code?: string
  runningOpId?: string
}

export type PublicGateReplyResult = {
  status: string
  error?: string
  message?: string
  kind?: string
}

/** Public review-share artifact list item (metadata only; no runId). */
export type PublicGateArtifactMeta = {
  id: string
  name: string
  kind: Artifact['kind']
  nodeId: string
  sizeBytes: number
  createdAt: string
  updatedAt?: string
  revision?: number
}

export type PublicGateArtifactsResult = {
  status: string
  artifacts?: PublicGateArtifactMeta[]
  /** Sanitized graph nodes for ReactArtifactStage grid filtering. */
  nodes?: Array<{ id: string; type: string; label?: string; config?: Record<string, unknown> }>
  error?: string
  message?: string
}

export type PublicGateArtifactContentResult = {
  status?: string
  id?: string
  name?: string
  kind?: Artifact['kind']
  nodeId?: string
  sizeBytes?: number
  createdAt?: string
  updatedAt?: string
  revision?: number
  content?: string
  etag?: string
  error?: string
  message?: string
}

async function readJson<T>(res: Response): Promise<T> {
  try {
    return (await res.json()) as T
  } catch {
    throw new Error(`${res.status}`)
  }
}

function keepSparseField<K extends keyof PublicGatePreview>(
  merged: PublicGatePreview,
  prev: PublicGatePreview,
  next: PublicGatePreview,
  bodyKey: K,
  hashKey: 'visualHtmlHash' | 'upstreamHash' | 'structuredHash' | 'turnsHash',
) {
  const hashChanged = typeof next[hashKey] === 'string' && next[hashKey] !== (prev[hashKey] ?? '')
  if (Object.prototype.hasOwnProperty.call(next, bodyKey)) {
    merged[bodyKey] = next[bodyKey]
  } else if (hashChanged) {
    merged[bodyKey] = next[bodyKey]
  } else if (prev[bodyKey] != null) {
    merged[bodyKey] = prev[bodyKey]
  }
  if (typeof next[hashKey] === 'string') {
    merged[hashKey] = next[hashKey]
  } else if (prev[hashKey] != null) {
    merged[hashKey] = prev[hashKey]
  }
}

/** Merge sparse poll payloads: omitted large fields keep the previous client value. */
export function mergePublicGatePreview(
  prev: PublicGatePreview | null,
  next: PublicGatePreview,
): PublicGatePreview {
  if (!prev) return next
  const merged: PublicGatePreview = { ...prev, ...next }
  keepSparseField(merged, prev, next, 'visualHtml', 'visualHtmlHash')
  keepSparseField(merged, prev, next, 'upstream', 'upstreamHash')
  keepSparseField(merged, prev, next, 'structured', 'structuredHash')
  keepSparseField(merged, prev, next, 'turns', 'turnsHash')
  // Silent polls may omit nonce; never drop the last usable one.
  if (!next.nonce && prev.nonce) merged.nonce = prev.nonce
  // Session state is omitempty on the wire: absent means idle, never "unchanged".
  merged.sessionBusy = !!next.sessionBusy
  merged.waiting = next.waiting || 0
  merged.queueItems = next.queueItems
  if (!next.sessionBusy) {
    merged.liveEvents = undefined
    // Sparse idle payloads often omit activeItem; drop stale in-flight pointer.
    if (next.activeItem == null) merged.activeItem = undefined
  }
  return merged
}

/**
 * Content fingerprint for silent-poll short-circuit.
 * Excludes remainingSec / expiresAt clock drift and nonce rotation.
 */
export function publicGateContentKey(p: PublicGatePreview | null | undefined): string {
  if (!p) return ''
  return JSON.stringify({
    status: p.status,
    kind: p.kind || '',
    title: p.title || '',
    description: p.description || '',
    actions: p.actions || null,
    visualHtmlHash: p.visualHtmlHash || '',
    visualHtml: p.visualHtmlHash ? '' : p.visualHtml || '',
    structuredHash: p.structuredHash || '',
    structured: p.structuredHash ? null : p.structured || null,
    turnsHash: p.turnsHash || '',
    turns: p.turnsHash ? null : p.turns || null,
    upstreamHash: p.upstreamHash || '',
    reactSessionAlive: !!p.reactSessionAlive,
    sessionBusy: !!p.sessionBusy,
    waiting: p.waiting || 0,
    queueItems: p.queueItems || [],
    activeItem: p.activeItem || null,
    productKind: p.productKind || '',
    productName: p.productName || '',
    ports: (p.ports || []).map((x) => ({ port: x.port, label: x.label || '', mode: x.mode || '' })),
    liveEvents: p.liveEvents || null,
  })
}

export type PublicGatePreviewKnown = {
  visualHtmlHash?: string
  upstreamHash?: string
  structuredHash?: string
  turnsHash?: string
  silent?: boolean
  issueNonce?: boolean
}

/** Unauthenticated public gate APIs. Token never goes in path/query. */
export const publicGateApi = {
  eventsWsUrl(): string {
    return window.location.origin.replace(/^http/, 'ws') + '/public/gate-approvals/events'
  },
  preview(
    token: string,
    signal?: AbortSignal,
    known?: PublicGatePreviewKnown,
  ): Promise<PublicGatePreview> {
    const headers: Record<string, string> = {
      [GATE_SHARE_TOKEN_HEADER]: token,
      [GATE_SHARE_REQUEST_HEADER]: '1',
    }
    const vh = known?.visualHtmlHash?.trim()
    const uh = known?.upstreamHash?.trim()
    const sh = known?.structuredHash?.trim()
    const th = known?.turnsHash?.trim()
    if (vh) headers['X-Gate-Known-Visual-Html-Hash'] = vh
    if (uh) headers['X-Gate-Known-Upstream-Hash'] = uh
    if (sh) headers['X-Gate-Known-Structured-Hash'] = sh
    if (th) headers['X-Gate-Known-Turns-Hash'] = th
    if (known?.silent) headers['X-Gate-Silent-Poll'] = '1'
    if (known?.issueNonce) headers['X-Gate-Issue-Nonce'] = '1'
    return fetch('/public/gate-approvals/preview', {
      method: 'GET',
      credentials: 'omit',
      signal,
      headers,
    }).then(async (res) => {
      const body = await readJson<PublicGatePreview & { error?: string; message?: string }>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  upstream(token: string, signal?: AbortSignal): Promise<PublicGateUpstreamResult> {
    return fetch('/public/gate-approvals/upstream', {
      method: 'GET',
      credentials: 'omit',
      signal,
      headers: {
        [GATE_SHARE_TOKEN_HEADER]: token,
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
    }).then(async (res) => {
      const body = await readJson<PublicGateUpstreamResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  decide(
    payload: {
      token: string
      action: string
      comment?: string
      name?: string
      nonce: string
      abortRunning?: boolean
    },
    signal?: AbortSignal,
  ): Promise<PublicGateDecideResult> {
    return fetch('/public/gate-approvals/decide', {
      method: 'POST',
      credentials: 'omit',
      signal,
      headers: {
        'Content-Type': 'application/json',
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
      body: JSON.stringify(payload),
    }).then(async (res) => {
      const body = await readJson<PublicGateDecideResult>(res)
      if (body.status === 'sandbox_busy' || body.code === 'sandbox_busy') {
        return { ...body, status: 'sandbox_busy' }
      }
      if (!res.ok && res.status !== 409) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return { ...body, status: body.status || (res.status === 409 ? 'used' : body.status) }
    })
  },
  reply(payload: {
    token: string
    text: string
    annotations?: Array<{
      selector?: string
      jsonPath?: string
      label?: string
      note?: string
      quote?: string
    }>
    images?: Array<{ data?: string; mimeType?: string; name?: string }>
  }): Promise<PublicGateReplyResult> {
    return fetch('/public/gate-approvals/reply', {
      method: 'POST',
      credentials: 'omit',
      headers: {
        'Content-Type': 'application/json',
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
      body: JSON.stringify(payload),
    }).then(async (res) => {
      const body = await readJson<PublicGateReplyResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  cancel(token: string): Promise<PublicGateReplyResult> {
    return fetch('/public/gate-approvals/cancel', {
      method: 'POST',
      credentials: 'omit',
      headers: {
        'Content-Type': 'application/json',
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
      body: JSON.stringify({ token }),
    }).then(async (res) => {
      const body = await readJson<PublicGateReplyResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  queueRemove(token: string, itemId: string): Promise<PublicGateReplyResult> {
    return fetch('/public/gate-approvals/queue/remove', {
      method: 'POST',
      credentials: 'omit',
      headers: {
        'Content-Type': 'application/json',
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
      body: JSON.stringify({ token, itemId }),
    }).then(async (res) => {
      const body = await readJson<PublicGateReplyResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  queueReorder(token: string, itemIds: string[]): Promise<PublicGateReplyResult> {
    return fetch('/public/gate-approvals/queue/reorder', {
      method: 'POST',
      credentials: 'omit',
      headers: {
        'Content-Type': 'application/json',
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
      body: JSON.stringify({ token, itemIds }),
    }).then(async (res) => {
      const body = await readJson<PublicGateReplyResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  artifacts(token: string, signal?: AbortSignal): Promise<PublicGateArtifactsResult> {
    return fetch('/public/gate-approvals/artifacts', {
      method: 'GET',
      credentials: 'omit',
      signal,
      headers: {
        [GATE_SHARE_TOKEN_HEADER]: token,
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
    }).then(async (res) => {
      const body = await readJson<PublicGateArtifactsResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  artifactContent(
    token: string,
    name: string,
    signal?: AbortSignal,
  ): Promise<PublicGateArtifactContentResult> {
    const encoded = encodeURIComponent(name)
    return fetch(`/public/gate-approvals/artifacts/${encoded}/content`, {
      method: 'GET',
      credentials: 'omit',
      signal,
      headers: {
        [GATE_SHARE_TOKEN_HEADER]: token,
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
    }).then(async (res) => {
      const body = await readJson<PublicGateArtifactContentResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  /**
   * Exchange share token for a short-lived preview ticket (VNC or API iframe).
   * Share token stays in header; ticket may appear in WS query / iframe path.
   */
  previewTicket(
    token: string,
    port: number,
    purpose?: 'vnc' | 'api' | string,
    signal?: AbortSignal,
  ): Promise<PublicPreviewTicketResult> {
    return fetch('/public/gate-approvals/preview-ticket', {
      method: 'POST',
      credentials: 'omit',
      signal,
      headers: {
        'Content-Type': 'application/json',
        [GATE_SHARE_TOKEN_HEADER]: token,
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
      body: JSON.stringify({ port, purpose: purpose || 'vnc' }),
    }).then(async (res) => {
      const body = await readJson<PublicPreviewTicketResult>(res)
      if (!res.ok) {
        throw Object.assign(new Error(body.message || body.error || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
  /** One-shot ticket for the direct-preview chat drawer; needs reply permission. */
  embedTicket(token: string, signal?: AbortSignal): Promise<EmbedTicket> {
    return fetch('/public/gate-approvals/embed-ticket', {
      method: 'POST',
      credentials: 'omit',
      signal,
      headers: {
        'Content-Type': 'application/json',
        [GATE_SHARE_TOKEN_HEADER]: token,
        [GATE_SHARE_REQUEST_HEADER]: '1',
      },
    }).then(async (res) => {
      const body = await readJson<EmbedTicket & { status?: string; error?: string; message?: string }>(res)
      // A spent or revoked link answers 200 with a status and no ticket.
      if (!res.ok || !body.ticket) {
        throw Object.assign(new Error(body.message || body.error || body.status || `${res.status}`), {
          status: res.status,
          body,
        })
      }
      return body
    })
  },
}

/** Build a site-root ws(s) URL for the public preview-vnc channel. */
export function publicPreviewVncWsUrl(ticket: string, wsPath?: string): string {
  const path = (wsPath || '/public/gate-approvals/preview-vnc/ws').trim() || '/public/gate-approvals/preview-vnc/ws'
  const q = `ticket=${encodeURIComponent(ticket)}`
  const base = path.includes('?') ? `${path}&${q}` : `${path}?${q}`
  return window.location.origin.replace(/^http/, 'ws') + base
}

/**
 * Boot record for one node visit (runId:nodeId:iteration), fed only by real
 * state changes the platform exposes — nothing here advances on a timer:
 *
 *   node start   inbox item `requestedAt` (= StateRun.StartedAt, server clock)
 *   pulling      sandbox row status `pulling` (gateway is pulling the image)
 *   creating     sandbox row status `creating` (container + startup.sh workspace
 *                clone + ACP bridge handshake; the backend does not split these)
 *   ready        sandbox row status `running`; its `updatedAt` is stamped by
 *                RegisterRunSandbox right after ACP connect (server clock)
 *   live         the inbox item leaves `starting` (conversation row created,
 *                session parked) — observed by the client, no server timestamp
 *
 * The connecting checklist renders these states; ClarifyChat reads the same
 * record afterwards for its folded「环境已就绪」row. Times are only shown when
 * both ends are server timestamps; otherwise the step shows no time.
 */
import { reactive } from 'vue'

export type BootStepId = 'sandbox' | 'workspace' | 'agent'
export type BootStepState = 'done' | 'running' | 'pending'

export interface BootRecord {
  /** Node entered (server clock, ms). */
  nodeStartedAt: number | null
  /** Last lifecycle status seen for this boot's sandbox row. */
  sandboxStatus: string | null
  /** Sandbox registered running (server clock, ms). */
  readyAt: number | null
  /** Session went live (item left `starting`). */
  live: boolean
}

export interface BootStepView {
  id: BootStepId
  state: BootStepState
  /** Real elapsed seconds, or null when the backend does not expose both ends. */
  sec: number | null
}

export interface BootSandboxObservation {
  status?: string | null
  updatedAt?: string | null
}

const MAX_RECORDS = 50
const records = reactive(new Map<string, BootRecord>())

export function bootKey(runId: string, nodeId: string, iteration?: number | null): string {
  return `${runId}:${nodeId}:${iteration && iteration > 0 ? iteration : 1}`
}

function parseTs(iso?: string | null): number | null {
  if (!iso) return null
  const ms = Date.parse(iso)
  return Number.isFinite(ms) && ms > 0 ? ms : null
}

/** Start (or keep) the record for a node visit that is booting. */
export function beginBoot(key: string, nodeStartedAt?: string | null): void {
  const existing = records.get(key)
  if (existing) {
    if (existing.nodeStartedAt == null) existing.nodeStartedAt = parseTs(nodeStartedAt)
    return
  }
  records.set(key, { nodeStartedAt: parseTs(nodeStartedAt), sandboxStatus: null, readyAt: null, live: false })
  while (records.size > MAX_RECORDS) {
    const oldest = records.keys().next().value
    if (oldest === undefined) break
    records.delete(oldest)
  }
}

/**
 * Record one sandbox poll. A row last touched before this node started belongs
 * to an earlier visit (retired sandbox of the same run+node) and is ignored.
 */
export function observeBootSandbox(key: string, sb: BootSandboxObservation | null | undefined): void {
  const rec = records.get(key)
  if (!rec || !sb) return
  const status = String(sb.status || '').trim().toLowerCase()
  if (!status) return
  const updatedAt = parseTs(sb.updatedAt)
  if (rec.nodeStartedAt != null && updatedAt != null && updatedAt < rec.nodeStartedAt) return
  if (rec.live && rec.readyAt != null) return
  rec.sandboxStatus = status
  if (status === 'running' && rec.readyAt == null) rec.readyAt = updatedAt
}

/** The item left `starting`: session is live. */
export function markBootLive(key: string): void {
  const rec = records.get(key)
  if (rec) rec.live = true
}

export function getBootRecord(key: string | null | undefined): BootRecord | null {
  return key ? records.get(key) ?? null : null
}

export function clearBootRecords(): void {
  records.clear()
}

function readyOf(rec: BootRecord | null): boolean {
  return !!rec && (rec.live || rec.readyAt != null || rec.sandboxStatus === 'running')
}

/** Seconds from node start to sandbox registered running, both server clock. */
export function bootReadySec(rec: BootRecord | null): number | null {
  if (!rec || rec.nodeStartedAt == null || rec.readyAt == null) return null
  const ms = rec.readyAt - rec.nodeStartedAt
  return ms >= 0 ? Math.round(ms / 1000) : null
}

/**
 * Three user-facing steps from the real states:
 * - sandbox: running until the row is `running`; time = node start → ready.
 * - workspace: the clone runs inside the container before ACP is ready and is
 *   not reported on its own, so it ticks together with sandbox, without time.
 * - agent: running between ready and live, done when live; no server time.
 */
export function bootSteps(rec: BootRecord | null): BootStepView[] {
  const ready = readyOf(rec)
  const live = !!rec?.live
  return [
    { id: 'sandbox', state: ready ? 'done' : 'running', sec: ready ? bootReadySec(rec) : null },
    { id: 'workspace', state: ready ? 'done' : 'pending', sec: null },
    { id: 'agent', state: live ? 'done' : ready ? 'running' : 'pending', sec: null },
  ]
}

import type { ClarifyTurn, Run } from '../shared/types'
import { bootKey, seedBoot } from '../run/bootRecord'

/**
 * One executed node of a run, in execution order, for the continuous inbox
 * transcript (确认并流转 → the next node's chat continues below the previous
 * node's, separated by a node row).
 */
export interface ChainSegment {
  nodeId: string
  iteration: number
  label: string
  /** chat = clarify/review/takeover conversation; auto = no conversation (agent ran unattended); gate = human_gate. */
  kind: 'chat' | 'auto' | 'gate'
  status: string
  /** Execution start (ordering). */
  at: string
  /** Execution end (only on ended segments; #850 endedAt). Meta time of the fold row. */
  endedAt?: string
  turns: ClarifyTurn[]
  /** Auto segments: number of agent events (#850 eventCount); no turns. */
  eventCount?: number
  /** Gate segments: approve / reject outcome (optional; requested #850 follow-up). */
  decision?: string
  /** What this node does (interpolated node task), for the opening bubble after a hand-off. */
  task?: string
  /** The note this visit left for the next node (outputs.handoff); platform = written for a silent upstream. */
  handoff?: string
  handoffSource?: 'agent' | 'platform'
  /** Sandbox start-up of this visit: pulling / creating while booting, ready once the Agent is connected. */
  boot?: ChainBoot
}

export interface ChainBoot {
  status: string
  readyAt?: string
}

export interface RunChainCurrent {
  id: string
  name: string
  iteration?: number
  /** agent | clarify | review | takeover | gate */
  kind?: string
  mode?: 'auto' | 'manual'
  state?: string
}

export interface RunChain {
  runId: string
  status: Run['status'] | string
  current: RunChainCurrent | null
  segments: ChainSegment[]
}

/**
 * Wire shape of GET /api/runs/:id/conversation-chain (#850, feature/conversation-chain @ 3410ac20).
 * One segment per (nodeId, iteration), execution order. clarify/review/takeover
 * segments always carry turns (role/text/at, plus questions/tools/parts/forms
 * when the turn has them; images and the raw event log are omitted); agent
 * segments carry eventCount only. endedAt only once ended. name may be "".
 * task only on Agent-node agent/clarify/review/takeover segments.
 */
export interface RunChainWireSegment {
  nodeId: string
  iteration?: number
  name?: string
  kind?: string
  mode?: 'auto' | 'manual' | string
  status?: string
  startedAt?: string
  endedAt?: string
  turns?: Array<Pick<ClarifyTurn, 'role' | 'text' | 'at' | 'questions' | 'tools' | 'parts' | 'forms'>>
  eventCount?: number
  task?: string
  handoff?: string
  handoffSource?: string
  boot?: { status?: string; readyAt?: string } | null
  /**
   * kind:'gate' + mode:'manual' segments (requested #850 follow-up): the
   * gate-react reject / re-review discussion as its own segment after the
   * upstream iteration, with turns and an optional decision. Today that
   * discussion is folded into the upstream node's turns (review_session.go:691);
   * both shapes render.
   */
  decision?: string
}
export interface RunChainResponse {
  runId: string
  status: string
  currentNode?: RunChainCurrent | null
  segments: RunChainWireSegment[]
}

/** Graph metadata the chain does not carry (label fallback for name:"", node type for input/output). */
export type ChainNodeMeta = ReadonlyArray<{ id: string; label?: unknown; type?: unknown }>

const TERMINAL = new Set(['completed', 'failed', 'cancelled'])
export function isRunTerminal(status: string | undefined | null): boolean {
  return !!status && TERMINAL.has(status)
}

/** Text, or the structured detail the prior fold renders. */
function turnHasDetail(t: ClarifyTurn): boolean {
  return !!String(t.text ?? '').trim() || !!t.questions?.length || !!t.tools?.length || !!t.parts?.length || !!t.forms?.length
}

function segKind(kind: string | undefined, hasTurns: boolean): ChainSegment['kind'] {
  if (kind === 'gate') return 'gate'
  if (kind === 'clarify' || kind === 'review' || kind === 'takeover' || hasTurns) return 'chat'
  return 'auto'
}

/** Graph plumbing nodes: no work of their own, never a transcript row. */
const PLUMBING_TYPES = new Set(['input', 'output'])
/**
 * input/output nodes also get a segment (agent/auto, no task, 0 events). They
 * are hidden: they are instantaneous graph plumbing, a row would read like an
 * agent step. Known type wins; without graph metadata fall back to the wire
 * signature (auto, no task, no events, zero duration).
 */
export function isPlumbingSegment(s: RunChainWireSegment, type?: string): boolean {
  if (type) return PLUMBING_TYPES.has(type)
  return (
    (s.kind ?? 'agent') === 'agent' &&
    !s.task &&
    !s.eventCount &&
    !s.turns &&
    !!s.startedAt &&
    s.endedAt === s.startedAt
  )
}

function handoffOf(text: unknown, source: unknown): Pick<ChainSegment, 'handoff' | 'handoffSource'> {
  const handoff = typeof text === 'string' ? text.trim() : ''
  if (!handoff) return {}
  return { handoff, handoffSource: source === 'platform' ? 'platform' : 'agent' }
}

export function normalizeRunChain(res: RunChainResponse, nodes?: ChainNodeMeta): RunChain {
  const meta = new Map((nodes ?? []).map((n) => [n.id, n]))
  const label = (id: string, name?: string) => name || (meta.get(id)?.label as string | undefined) || id
  const cur = res.currentNode ?? null
  return {
    runId: res.runId,
    status: res.status,
    current: cur ? { ...cur, name: label(cur.id, cur.name) } : null,
    segments: (res.segments ?? [])
      .filter((s) => !isPlumbingSegment(s, meta.get(s.nodeId)?.type as string | undefined))
      .map((s) => {
        // A bare 确认并流转 (no text and no structured detail) carries nothing to show.
        const turns = ((s.turns ?? []) as ClarifyTurn[]).filter(turnHasDetail)
        return {
          nodeId: s.nodeId,
          iteration: s.iteration ?? 1,
          label: label(s.nodeId, s.name),
          kind: segKind(s.kind, turns.length > 0),
          status: s.status || 'completed',
          at: s.startedAt || '',
          endedAt: s.endedAt || undefined,
          turns,
          eventCount: s.eventCount,
          decision: s.decision || undefined,
          task: s.task || undefined,
          ...handoffOf(s.handoff, s.handoffSource),
          ...(s.boot?.status ? { boot: { status: s.boot.status, readyAt: s.boot.readyAt || undefined } } : {}),
        }
      }),
  }
}

/** Boot records of every visit the server reports, so the boot steps survive a reload and stay on finished nodes. */
export function seedChainBoots(chain: RunChain | null | undefined): void {
  for (const s of chain?.segments ?? []) {
    if (s.boot) seedBoot(bootKey(chain!.runId, s.nodeId, s.iteration), s.at, s.boot)
  }
}

/** The visit on screen is not in the chain yet, or its sandbox is still starting. */
export function chainBootPending(chain: RunChain | null | undefined, nodeId: string, iteration: number): boolean {
  const s = (chain?.segments ?? []).find((x) => x.nodeId === nodeId && x.iteration === iteration)
  return !s || (!!s.boot && s.boot.status !== 'ready')
}

/** Plumbing node ids (input/output) of a run graph, for live frames (node_enter) that carry no type. */
export function isPlumbingNode(nodes: ChainNodeMeta | undefined, nodeId: string): boolean {
  const t = (nodes ?? []).find((n) => n.id === nodeId)?.type
  return typeof t === 'string' && PLUMBING_TYPES.has(t)
}

/**
 * Fallback on today's backend: derive the chain from GET /runs/:id
 * (nodeRuns order by startedAt + clarifyByNode). Heavier payload, latest
 * execution per node only (loops collapse to their last pass).
 */
export function runChainFromRun(run: Run): RunChain {
  const label = (id: string) => ((run.nodes ?? []).find((n) => n.id === id)?.label as string | undefined) || id
  const type = (id: string) => (run.nodes ?? []).find((n) => n.id === id)?.type as string | undefined
  const task = (id: string) => nodeTaskFromConfig((run.nodes ?? []).find((n) => n.id === id)?.config)
  const runs = Object.values(run.nodeRuns ?? {}).filter(
    (nr) => nr && nr.status !== 'pending' && nr.status !== 'skipped' && !PLUMBING_TYPES.has(type(nr.nodeId) ?? ''),
  )
  runs.sort((a, b) => String(a.startedAt || '').localeCompare(String(b.startedAt || '')))
  let current: RunChainCurrent | null = null
  const segments: ChainSegment[] = []
  for (const nr of runs) {
    const conv = run.clarifyByNode?.[nr.nodeId]
    const turns = conv?.turns ?? []
    const kind: ChainSegment['kind'] = type(nr.nodeId) === 'human_gate' ? 'gate' : turns.length ? 'chat' : 'auto'
    segments.push({
      nodeId: nr.nodeId,
      iteration: nr.iteration ?? conv?.iteration ?? 1,
      label: label(nr.nodeId),
      kind,
      status: nr.status,
      at: nr.startedAt || '',
      turns,
      task: task(nr.nodeId),
      ...handoffOf(nr.outputs?.handoff, nr.outputs?.handoff_source),
    })
    if (nr.status === 'running' || nr.status === 'waiting_human') {
      current = {
        id: nr.nodeId,
        name: label(nr.nodeId),
        iteration: nr.iteration,
        mode: kind === 'auto' && nr.status === 'running' ? 'auto' : 'manual',
        state: nr.status,
      }
    }
  }
  return { runId: run.id, status: run.status, current, segments }
}

/**
 * Segments shown above the active node's conversation: everything the run
 * executed before (nodeId, iteration). Unknown active → every other node.
 */
export function priorSegments(chain: RunChain | null | undefined, nodeId: string, iteration = 1): ChainSegment[] {
  if (!chain) return []
  const idx = chain.segments.findIndex((s) => s.nodeId === nodeId && s.iteration === iteration)
  const list = idx >= 0 ? chain.segments.slice(0, idx) : chain.segments.filter((s) => s.nodeId !== nodeId)
  return list.filter((s) => s.status !== 'running' && s.status !== 'waiting_human')
}

const TASK_MAX = 280
/** Opening-bubble text: trimmed, clipped; raw config.prompt keeps {{vars}} un-interpolated (fallback only). */
export function clipTask(text: string | undefined | null): string {
  const s = String(text ?? '').trim()
  if (!s) return ''
  const r = [...s]
  return r.length > TASK_MAX ? r.slice(0, TASK_MAX).join('').trimEnd() + '…' : s
}
export function nodeTaskFromConfig(config: Record<string, any> | undefined | null): string | undefined {
  return clipTask(typeof config?.prompt === 'string' ? config.prompt : '') || undefined
}

/** The active node's own segment (for its task). */
export function activeSegment(chain: RunChain | null | undefined, nodeId: string, iteration = 1): ChainSegment | null {
  return chain?.segments.find((s) => s.nodeId === nodeId && s.iteration === iteration) ?? null
}

export function segmentKey(runId: string, s: Pick<ChainSegment, 'nodeId' | 'iteration'>): string {
  return `${runId}:${s.nodeId}:${s.iteration}`
}

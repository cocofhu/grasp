import type { Artifact, ClarifyTurn, Gate, NodeRun, Run, WFEdge, WFNode } from '../shared/types'

export type GateInboxContext = {
  type: 'gate'
  nodes: WFNode[]
  /** Run-graph transitions. Absent on older inbox-context payloads. */
  edges?: WFEdge[]
  artifacts: Artifact[]
  nodeExecutions: Record<string, NodeRun[]>
  /** Every executed node's latest visit (status only), for the workflow outline. */
  nodeRuns?: Record<string, NodeRun>
  /** Authoritative busy/queue for Inbox gate hard-refresh resume. */
  reactSessions?: Run['reactSessions']
  /** Gate DTO subset (reactUpstream / sessionAlive) for hot-revise seed. */
  gate?: Gate
}

export type ClarifyInboxContext = {
  type: 'clarify'
  status: Run['status']
  /** Full graph nodes (same shape as gate inbox-context). */
  nodes?: WFNode[]
  /** Run-graph transitions. Absent on older inbox-context payloads. */
  edges?: WFEdge[]
  /** Run-level artifact metadata (content loaded on demand). */
  artifacts?: Artifact[]
  /** Slim executions for current node ∪ parseable upstream refs. */
  nodeExecutions?: Record<string, NodeRun[]>
  /** Every executed node's latest visit (status only), for the workflow outline. */
  nodeRuns?: Record<string, NodeRun>
  /** Authoritative busy/queue for refresh-resume (parity with Run detail). */
  reactSessions?: Run['reactSessions']
  clarify: {
    nodeId: string
    iteration?: number
    turns: ClarifyTurn[]
    done: boolean
    label: string
    previewArtifact?: string
    /** Approve sandbox still booting: empty transcript, replies not accepted yet. */
    starting?: boolean
    takeover?: boolean
    /** Taken-over node's task (interpolated prompt), when the backend sends it. */
    task?: string
    /** Starting only: the launcher's opening message, waiting for the agent. */
    firstMessage?: string
  }
}

export type InboxContextResponse = GateInboxContext | ClarifyInboxContext

/** Item is no longer pending. HTTP 200 so the browser does not log a failed fetch. */
export type InboxLeftPending = {
  pending: false
}

/** Maps inbox-context API response to the Run subset GateApproval/ClarifyChat expect. */
export function adaptInboxContextToRun(ctx: InboxContextResponse, runId: string): Run {
  const base: Run = {
    id: runId,
    workflowId: '',
    workflowName: '',
    status: 'waiting_human',
    trigger: '',
    startedAt: '',
    durationSec: 0,
    progress: 0,
    nodeRuns: {},
    artifacts: [],
  }

  if (ctx.type === 'gate') {
    return {
      ...base,
      nodes: ctx.nodes,
      edges: ctx.edges,
      artifacts: ctx.artifacts,
      nodeRuns: ctx.nodeRuns ?? {},
      nodeExecutions: ctx.nodeExecutions,
      reactSessions: ctx.reactSessions,
      gate: ctx.gate,
    }
  }

  return {
    ...base,
    status: ctx.status,
    nodes: ctx.nodes,
    edges: ctx.edges,
    artifacts: ctx.artifacts ?? [],
    nodeRuns: ctx.nodeRuns ?? {},
    nodeExecutions: ctx.nodeExecutions,
    reactSessions: ctx.reactSessions,
    clarifyByNode: {
      [ctx.clarify.nodeId]: {
        nodeId: ctx.clarify.nodeId,
        iteration: ctx.clarify.iteration,
        turns: ctx.clarify.turns ?? [],
        done: ctx.clarify.done,
        previewArtifact: ctx.clarify.previewArtifact,
        ...(ctx.clarify.takeover ? { takeover: true } : {}),
        ...(ctx.clarify.task ? { task: ctx.clarify.task } : {}),
        ...(ctx.clarify.firstMessage ? { firstMessage: ctx.clarify.firstMessage } : {}),
      },
    },
  }
}

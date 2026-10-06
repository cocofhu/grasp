import { computed, type Ref } from 'vue'
import { MarkerType } from '@vue-flow/core'
import type { NodeRunStatus, NodeType, WFEdge, WFNode } from '@/lib/shared/types'
import { flowFingerprint, pruneFlowCache, reuseFlowElement, type FlowNodeCacheEntry } from '@/lib/run/workflowCanvasFlow'
import { isKnownNodeType } from '@/lib/workflow/graphValidation'
import { nodeCapabilities, type AgentCapsLookup } from '@/lib/workflow/nodeOutlets'
import type { CanvasEdgeData, CanvasMode, CanvasNodeData, EdgeRunState } from './canvasContext'
import { capabilityFlags, findAgent, nodeOutlets, normHandle, type CanvasAgent, type Outlet, type Translate } from './outlets'
import { checkConnection } from './useConnectionRules'
import { NODE_ICONS } from './paletteItems'

export interface FlowNodeObj {
  id: string
  type: string
  position: { x: number; y: number }
  draggable: boolean
  connectable: boolean
  selectable: boolean
  focusable: boolean
  selected: boolean
  ariaLabel: string
  data: CanvasNodeData
}

export interface FlowEdgeObj {
  id: string
  source: string
  target: string
  sourceHandle?: string
  type: string
  selected: boolean
  selectable: boolean
  focusable: boolean
  ariaLabel: string
  markerEnd: { type: MarkerType; color: string; width: number; height: number }
  data: CanvasEdgeData
}

export interface FlowInputs {
  nodes: () => WFNode[]
  edges: () => WFEdge[]
  mode: () => CanvasMode
  agents: () => CanvasAgent[]
  lookup: () => AgentCapsLookup
  statusMap: () => Record<string, NodeRunStatus> | undefined
  iterations: () => Record<string, number> | undefined
  failReasons: () => Record<string, string> | undefined
  activePath: () => string[] | undefined
  issues: () => Map<string, string[]> | undefined
  selectedNodes: () => string[]
  selectedEdges: () => string[]
  renamingId: () => string | null
  connecting: Ref<{ source: string; sourceHandle: string } | null>
  /** Display-only positions (read-only canvases laid out on open); model positions win when absent. */
  positions?: () => Map<string, { x: number; y: number }> | null
  typeText: (type: NodeType) => { label: string; desc: string }
  t: Translate
}

function componentFor(type: string): string {
  if (type === 'agent') return 'agent'
  if (type === 'human_gate') return 'collab'
  return 'control'
}

function controlSummary(n: WFNode, t: Translate): string | undefined {
  const c = (n.config || {}) as Record<string, any>
  const len = (v: unknown) => (Array.isArray(v) ? v.length : 0)
  switch (n.type) {
    case 'input':
      return t('canvas.node.summary.variables', { n: len(c.variables) })
    case 'output':
      return len(c.results) ? t('canvas.node.summary.sources', { n: len(c.results) }) : t('canvas.node.summary.noSources')
    case 'set_var':
      return t('canvas.node.summary.assignments', { n: len(c.assignments) })
    case 'branch':
      return t('canvas.node.summary.cases', { n: len(c.cases) })
    case 'human_gate':
      return String(c.title || '') || t('canvas.node.summary.actions', { n: len(c.actions) })
    default:
      return undefined
  }
}

const TONE_VAR: Record<CanvasEdgeData['tone'], string> = {
  default: 'var(--flow-edge)',
  ok: 'rgb(var(--c-ok))',
  err: 'rgb(var(--c-err))',
}

/** Builds the Vue Flow node / edge arrays, reusing objects whose inputs did not change. */
export function useFlowElements(inp: FlowInputs) {
  const nodeCache = new Map<string, FlowNodeCacheEntry<FlowNodeObj>>()
  const edgeCache = new Map<string, FlowNodeCacheEntry<FlowEdgeObj>>()

  /** Outlets per node, plus stale handles still used by edges so those edges stay visible. */
  const outletsById = computed(() => {
    const lookup = inp.lookup()
    const m = new Map<string, Outlet[]>()
    for (const n of inp.nodes()) m.set(n.id, nodeOutlets(n, lookup, inp.t))
    for (const e of inp.edges()) {
      const outs = m.get(e.source)
      if (!outs) continue
      const h = normHandle(e.sourceHandle)
      if (outs.some((o) => o.id === h)) continue
      const src = inp.nodes().find((n) => n.id === e.source)
      if (src?.type === 'output') continue
      outs.push({ id: h, label: h ? inp.t('canvas.outlet.stale', { handle: h }) : inp.t('nodes.outlets.default'), tone: 'err', stale: true })
    }
    return m
  })

  const flowNodes = computed<FlowNodeObj[]>(() => {
    const mode = inp.mode()
    const run = mode === 'run'
    const readonly = mode !== 'edit'
    const status = inp.statusMap() || {}
    const iters = inp.iterations() || {}
    const fails = inp.failReasons() || {}
    const issues = inp.issues()
    const selected = new Set(inp.selectedNodes())
    const renaming = inp.renamingId()
    const conn = inp.connecting.value
    const graph = { nodes: inp.nodes(), edges: inp.edges() }
    const agents = inp.agents()
    const lookup = inp.lookup()
    const positions = inp.positions?.()
    const out = graph.nodes.map((n) => {
      const known = isKnownNodeType(n.type)
      const text = known ? inp.typeText(n.type) : { label: String(n.type), desc: '' }
      const cfg = (n.config || {}) as Record<string, any>
      const isAgent = n.type === 'agent'
      const agentName = isAgent ? String(cfg.agent_profile ?? '').trim() : ''
      const caps = isAgent ? nodeCapabilities(n, lookup) : undefined
      let connect: CanvasNodeData['connect'] = null
      if (conn && !readonly && n.type !== 'input') {
        const res = checkConnection(graph, { source: conn.source, sourceHandle: conn.sourceHandle, target: n.id })
        connect = res.ok ? { valid: true } : { valid: false, reason: inp.t(res.reason) }
      } else if (conn && !readonly && n.type === 'input' && n.id !== conn.source) {
        connect = { valid: false, reason: inp.t('canvas.rules.inputNoIncoming') }
      }
      const data: CanvasNodeData = {
        nodeType: n.type,
        title: n.label || text.label,
        typeLabel: text.label,
        icon: known ? NODE_ICONS[n.type] : 'alert',
        subtitle: isAgent ? undefined : controlSummary(n, inp.t),
        agentName: agentName || undefined,
        agentMissing: isAgent && !!agentName && agents.length > 0 && !findAgent(agents, agentName) && !n.caps,
        flags: isAgent ? capabilityFlags(caps) : undefined,
        goal: isAgent ? String(cfg.prompt ?? '').trim() : undefined,
        outlets: outletsById.value.get(n.id) || [],
        hasTarget: n.type !== 'input',
        mode,
        status: run ? status[n.id] || 'pending' : undefined,
        iteration: run ? iters[n.id] : undefined,
        failReason: run ? fails[n.id] : undefined,
        issues: readonly ? undefined : issues?.get(n.id),
        connect: n.id === conn?.source ? null : connect,
        renaming: renaming === n.id,
      }
      const position = positions?.get(n.id) ?? { x: n.position?.x ?? 0, y: n.position?.y ?? 0 }
      const isSel = selected.has(n.id)
      const fp = flowFingerprint({ data, position, readonly })
      return reuseFlowElement(nodeCache, n.id, fp, isSel, () => ({
        id: n.id,
        type: componentFor(n.type),
        position,
        draggable: !readonly,
        connectable: !readonly,
        selectable: true,
        focusable: true,
        selected: isSel,
        ariaLabel: inp.t('canvas.aria.node', { type: text.label, label: data.title }),
        data,
      }))
    })
    pruneFlowCache(nodeCache, graph.nodes.map((n) => n.id))
    return out
  })

  const edgeRun = computed(() => {
    if (inp.mode() !== 'run') return null
    const status = inp.statusMap() || {}
    const active = new Set(inp.activePath() || [])
    const started = Object.keys(status).length > 0
    return (e: WFEdge): EdgeRunState | undefined => {
      if (!started) return undefined
      if (active.has(e.id)) return 'active'
      const s = status[e.source]
      const tgt = status[e.target]
      if (s === 'completed' && tgt && tgt !== 'pending' && tgt !== 'skipped') return 'traversed'
      return 'dim'
    }
  })

  const flowEdges = computed<FlowEdgeObj[]>(() => {
    const readonly = inp.mode() !== 'edit'
    const selected = new Set(inp.selectedEdges())
    const label = new Map(inp.nodes().map((n) => [n.id, n.label || n.id]))
    const runOf = edgeRun.value
    const conn = readonly ? null : inp.connecting.value
    const ids = new Set(inp.nodes().map((n) => n.id))
    const out: FlowEdgeObj[] = []
    for (const e of inp.edges()) {
      if (!ids.has(e.source) || !ids.has(e.target)) continue
      const handle = normHandle(e.sourceHandle)
      const kind = e.kind || 'success'
      const tone: CanvasEdgeData['tone'] =
        handle === 'pass' ? 'ok' : handle === 'fail' || kind === 'failure' ? 'err' : 'default'
      const when = String(e.when ?? '').trim()
      const note = String(e.label ?? '').trim()
      const kindLabel = kind !== 'success' ? inp.t(`common.edgeKinds.${kind}.label`) : ''
      const data: CanvasEdgeData = {
        tone,
        dashed: handle === 'fail' || kind !== 'success',
        label: note || when || kindLabel,
        hasCondition: !!when,
        run: runOf ? runOf(e) : undefined,
        editable: !readonly,
        sourceLabel: label.get(e.source) || e.source,
        targetLabel: label.get(e.target) || e.target,
        replacing: !!conn && conn.source === e.source && conn.sourceHandle === handle && !when && kind === 'success',
      }
      const color = data.run === 'traversed' || data.run === 'active' ? 'var(--flow-edge-active)' : TONE_VAR[tone]
      const fp = flowFingerprint({ s: e.source, t: e.target, h: handle, data, color })
      const isSel = selected.has(e.id)
      out.push(
        reuseFlowElement(edgeCache, e.id, fp, isSel, () => ({
          id: e.id,
          source: e.source,
          target: e.target,
          ...(handle ? { sourceHandle: handle } : {}),
          type: 'flow',
          selected: isSel,
          selectable: !readonly,
          focusable: !readonly,
          ariaLabel: inp.t('canvas.aria.edge', { source: data.sourceLabel, target: data.targetLabel }),
          markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
          data,
        })),
      )
    }
    pruneFlowCache(edgeCache, inp.edges().map((e) => e.id))
    return out
  })

  return { flowNodes, flowEdges, outletsById }
}

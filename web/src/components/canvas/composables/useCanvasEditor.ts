import { computed, inject, ref, type InjectionKey, type Ref } from 'vue'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { useCanvasHistory } from './useCanvasHistory'
import { useCanvasClipboard } from './useCanvasClipboard'
import { checkAddNode, checkConnection, type ConnectionAttempt, type GraphLike } from './useConnectionRules'
import { animatePositions, computeAutoLayout, estimateNodeSize, type Point, type Size } from './useAutoLayout'
import { createNode, insertOnEdge, makeEdge, neighbors, removeElements, snap, type NodeSpec } from './graphOps'
import { collectGraphIssues, issuesByNode } from './useGraphIssues'
import { agentLookup, nodeOutlets, normHandle, type CanvasAgent, type Translate } from './outlets'

export type AddLink =
  | { kind: 'outlet'; source: string; sourceHandle?: string | null }
  | { kind: 'edge'; edgeId: string }
  | { kind: 'after'; nodeId: string }

export interface CanvasEditorOptions {
  graph: GraphLike
  /** Project Agents; null while loading. */
  agents: Ref<CanvasAgent[] | null>
  t: Translate
  /** Default label for a new node of a type. */
  typeLabel: (type: WFNode['type']) => string
  /** Show a message; `action` (e.g. Undo) is offered next to it when given. */
  notify?: (message: string, action?: NotifyAction) => void
}

export interface NotifyAction {
  label: string
  run: () => void
}

/** Shared editing state and commands for one workflow canvas (edit mode). */
export function useCanvasEditor(opts: CanvasEditorOptions) {
  const { graph, t } = opts
  const history = useCanvasHistory(graph)
  const clipboard = useCanvasClipboard(() => graph)

  const selectedNodeIds = ref<string[]>([])
  const selectedEdgeIds = ref<string[]>([])
  const renamingId = ref<string | null>(null)
  /** Bumped to ask the inspector to focus the goal field. */
  const focusGoalTick = ref(0)
  const inspectorDismissed = ref(false)
  /** Set by the canvas: where a node added without a position should go. */
  let placeHint: () => Point = () => ({ x: 0, y: 0 })
  /** Set by the canvas: measured node sizes for auto layout. */
  let measure: (n: WFNode) => Size = estimateNodeSize

  const agentList = computed(() => opts.agents.value ?? [])
  const lookup = computed(() => agentLookup(agentList.value))
  const issues = computed(() => collectGraphIssues(graph, opts.agents.value, t))
  const nodeIssues = computed(() => issuesByNode(issues.value))
  const inspectorNodeId = computed(() =>
    !inspectorDismissed.value && selectedNodeIds.value.length === 1 && !selectedEdgeIds.value.length
      ? selectedNodeIds.value[0]!
      : null,
  )
  const inspectorNode = computed(() => graph.nodes.find((n) => n.id === inspectorNodeId.value) ?? null)

  function notify(key: string) {
    opts.notify?.(t(key))
  }

  function setSelection(nodeIds: string[], edgeIds: string[] = []) {
    const nodes = new Set(graph.nodes.map((n) => n.id))
    const edges = new Set(graph.edges.map((e) => e.id))
    selectedNodeIds.value = nodeIds.filter((id) => nodes.has(id))
    selectedEdgeIds.value = edgeIds.filter((id) => edges.has(id))
    inspectorDismissed.value = false
    if (renamingId.value && !selectedNodeIds.value.includes(renamingId.value)) renamingId.value = null
  }

  function selectNode(id: string, additive = false) {
    if (additive) {
      const cur = selectedNodeIds.value
      setSelection(cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id], selectedEdgeIds.value)
    } else setSelection([id])
  }

  function clearSelection() {
    setSelection([])
    renamingId.value = null
  }

  function selectAll() {
    setSelection(graph.nodes.map((n) => n.id), graph.edges.map((e) => e.id))
    inspectorDismissed.value = true
  }

  function openInspector(id: string, focusGoal = false) {
    setSelection([id])
    if (focusGoal) focusGoalTick.value++
  }

  function closeInspector() {
    clearSelection()
  }

  function firstOutlet(node: WFNode): string | null {
    const outs = nodeOutlets(node, lookup.value, t)
    return outs.length ? outs[0]!.id : null
  }

  /** First outlet of `node` that has no unconditional edge yet. */
  function freeOutlet(node: WFNode): string | null {
    for (const o of nodeOutlets(node, lookup.value, t)) {
      const taken = graph.edges.some(
        (e) => e.source === node.id && normHandle(e.sourceHandle) === o.id && !String(e.when ?? '').trim(),
      )
      if (!taken) return o.id
    }
    return null
  }

  function addNode(spec: NodeSpec, at?: Point, link?: AddLink): WFNode | null {
    if (spec.type === 'agent' && !spec.agentProfile) return null
    const allowed = checkAddNode(graph, spec.type)
    if (!allowed.ok) {
      notify(allowed.reason)
      return null
    }
    const label = spec.label || spec.agentProfile || opts.typeLabel(spec.type)
    const node = createNode({ ...spec, label }, at ?? placeHint(), graph.nodes.map((n) => n.id))
    let replaced: WFEdge | null = null
    if (link?.kind === 'edge') {
      const out = firstOutlet(node)
      if (!insertOnEdge(graph, link.edgeId, node, out ?? '')) graph.nodes.push(node)
    } else {
      graph.nodes.push(node)
      let from: ConnectionAttempt | null = null
      if (link?.kind === 'outlet') from = { source: link.source, sourceHandle: link.sourceHandle, target: node.id }
      if (link?.kind === 'after') {
        const src = graph.nodes.find((n) => n.id === link.nodeId)
        const h = src ? freeOutlet(src) : null
        if (src && h !== null) from = { source: src.id, sourceHandle: h, target: node.id }
      }
      const res = from ? checkConnection(graph, from) : null
      if (from && res?.ok) replaced = pushEdge(from, res.replaces)
    }
    history.commit()
    setSelection([node.id])
    if (replaced) offerUndoReplace(replaced)
    return node
  }

  /** Adds the edge, first removing the one it replaces; returns the removed edge. */
  function pushEdge(c: ConnectionAttempt, replaces?: string): WFEdge | null {
    let removed: WFEdge | null = null
    if (replaces) {
      const i = graph.edges.findIndex((e) => e.id === replaces)
      if (i >= 0) removed = graph.edges.splice(i, 1)[0]!
    }
    graph.edges.push(makeEdge(graph, c))
    return removed
  }

  function offerUndoReplace(old: WFEdge) {
    const target = graph.nodes.find((n) => n.id === old.target)
    const at = history.size.value
    opts.notify?.(t('canvas.toast.edgeReplaced', { label: target?.label || old.target }), {
      label: t('canvas.toast.undo'),
      run: () => {
        if (history.size.value === at && !history.canRedo.value) undo()
      },
    })
  }

  function connect(c: ConnectionAttempt): boolean {
    const res = checkConnection(graph, c)
    if (!res.ok) {
      notify(res.reason)
      return false
    }
    const replaced = pushEdge(c, res.replaces)
    history.commit()
    if (replaced) offerUndoReplace(replaced)
    return true
  }

  function offerUndoDelete(nodeCount: number) {
    if (!nodeCount) return
    const at = history.size.value
    opts.notify?.(t('canvas.toast.deleted', { n: nodeCount }), {
      label: t('canvas.toast.undo'),
      run: () => {
        if (history.size.value === at && !history.canRedo.value) undo()
      },
    })
  }

  function removeNodes(ids: string[]) {
    const count = graph.nodes.filter((n) => ids.includes(n.id)).length
    const changed = removeElements(graph, ids)
    if (changed) history.commit()
    setSelection(selectedNodeIds.value.filter((id) => !ids.includes(id)), selectedEdgeIds.value)
    if (changed) offerUndoDelete(count)
  }

  function removeEdge(id: string) {
    if (removeElements(graph, [], [id])) history.commit()
    setSelection(selectedNodeIds.value, selectedEdgeIds.value.filter((x) => x !== id))
  }

  function removeSelection(): boolean {
    if (!selectedNodeIds.value.length && !selectedEdgeIds.value.length) return false
    const ids = new Set(selectedNodeIds.value)
    const count = graph.nodes.filter((n) => ids.has(n.id)).length
    const changed = removeElements(graph, selectedNodeIds.value, selectedEdgeIds.value)
    clearSelection()
    if (changed) {
      history.commit()
      offerUndoDelete(count)
    }
    return changed
  }

  /** Palette item waiting to be placed with a click on the canvas; null when not placing. */
  const placing = ref<NodeSpec | null>(null)

  /** Enters placement mode for `spec`; the same spec again cancels it. */
  function togglePlacing(spec: NodeSpec) {
    const cur = placing.value
    placing.value = cur && cur.type === spec.type && (cur.agentProfile ?? '') === (spec.agentProfile ?? '') ? null : { ...spec }
  }

  function cancelPlacing(): boolean {
    if (!placing.value) return false
    placing.value = null
    return true
  }

  function updateEdge(id: string, patch: Partial<Pick<WFEdge, 'when' | 'kind' | 'label'>>) {
    const e = graph.edges.find((x) => x.id === id)
    if (!e) return
    for (const [k, v] of Object.entries(patch) as [keyof WFEdge, unknown][]) {
      if (v === '' || v === undefined || (k === 'kind' && v === 'success')) delete e[k]
      else (e as unknown as Record<string, unknown>)[k] = v
    }
  }

  function moveNodes(moves: { id: string; x: number; y: number }[]) {
    let changed = false
    for (const m of moves) {
      const n = graph.nodes.find((x) => x.id === m.id)
      if (!n) continue
      const p = { x: snap(m.x), y: snap(m.y) }
      if (n.position?.x !== p.x || n.position?.y !== p.y) {
        n.position = p
        changed = true
      }
    }
    if (changed) history.commit()
  }

  function renameNode(id: string, label: string) {
    const n = graph.nodes.find((x) => x.id === id)
    const v = label.trim()
    renamingId.value = null
    if (!n || !v || n.label === v) return
    n.label = v
    history.commit()
  }

  function startRename(id?: string) {
    const target = id ?? (selectedNodeIds.value.length === 1 ? selectedNodeIds.value[0] : null)
    if (target) renamingId.value = target
  }

  function copy(): boolean {
    return clipboard.copy(selectedNodeIds.value)
  }

  function paste(at?: Point): boolean {
    const ids = clipboard.paste(at)
    if (!ids.length) return false
    history.commit()
    setSelection(ids)
    inspectorDismissed.value = ids.length === 1 ? false : true
    return true
  }

  function duplicate(ids = selectedNodeIds.value): boolean {
    const added = clipboard.duplicate(ids)
    if (!added.length) return false
    history.commit()
    setSelection(added)
    return true
  }

  function afterHistoryJump() {
    setSelection(selectedNodeIds.value, selectedEdgeIds.value)
  }

  function undo() {
    const ok = history.undo()
    if (ok) afterHistoryJump()
    return ok
  }

  function redo() {
    const ok = history.redo()
    if (ok) afterHistoryJump()
    return ok
  }

  function replaceGraph(nodes: WFNode[], edges: WFEdge[]) {
    graph.nodes.splice(0, graph.nodes.length, ...nodes)
    graph.edges.splice(0, graph.edges.length, ...edges)
    clearSelection()
    history.commit()
  }

  let layoutRun = 0
  async function autoLayout(animate = true): Promise<void> {
    if (!graph.nodes.length) return
    const run = ++layoutRun
    const target = await computeAutoLayout(graph.nodes, graph.edges, measure)
    if (run !== layoutRun) return
    const from = new Map(graph.nodes.map((n) => [n.id, { x: n.position?.x ?? 0, y: n.position?.y ?? 0 }]))
    const apply = (frame: Map<string, Point>) => {
      for (const n of graph.nodes) {
        const p = frame.get(n.id)
        if (p) n.position = { x: p.x, y: p.y }
      }
    }
    history.begin()
    try {
      await animatePositions(from, target, apply, animate ? undefined : 0)
    } finally {
      history.end()
    }
  }

  /** Arrow-key navigation: right / left follow edges, up / down move among siblings. */
  function navigate(dir: 'left' | 'right' | 'up' | 'down'): string | null {
    const cur = selectedNodeIds.value[selectedNodeIds.value.length - 1]
    if (!cur) {
      const first = graph.nodes.find((n) => n.type === 'input') ?? graph.nodes[0]
      if (first) setSelection([first.id])
      return first?.id ?? null
    }
    const { prev, next } = neighbors(graph, cur)
    const byY = (ids: string[]) =>
      [...new Set(ids)]
        .map((id) => graph.nodes.find((n) => n.id === id))
        .filter((n): n is WFNode => !!n)
        .sort((a, b) => (a.position?.y ?? 0) - (b.position?.y ?? 0))
    let pick: WFNode | undefined
    if (dir === 'right') pick = byY(next)[0]
    else if (dir === 'left') pick = byY(prev)[0]
    else {
      const parents = prev.length ? prev : []
      const siblings = byY(parents.flatMap((p) => neighbors(graph, p).next))
      const pool = siblings.length > 1 ? siblings : byY([...prev, ...next, cur])
      const i = pool.findIndex((n) => n.id === cur)
      pick = dir === 'up' ? pool[i - 1] : pool[i + 1]
    }
    if (!pick) return null
    setSelection([pick.id])
    return pick.id
  }

  return {
    graph,
    agents: agentList,
    agentsLoaded: computed(() => opts.agents.value !== null),
    lookup,
    history,
    issues,
    nodeIssues,
    selectedNodeIds,
    selectedEdgeIds,
    inspectorNodeId,
    inspectorNode,
    renamingId,
    focusGoalTick,
    setSelection,
    selectNode,
    clearSelection,
    selectAll,
    openInspector,
    closeInspector,
    addNode,
    connect,
    checkConnection: (c: ConnectionAttempt) => checkConnection(graph, c),
    removeNodes,
    removeEdge,
    removeSelection,
    placing,
    togglePlacing,
    cancelPlacing,
    updateEdge,
    moveNodes,
    renameNode,
    startRename,
    copy,
    paste,
    duplicate,
    canPaste: clipboard.hasContent,
    undo,
    redo,
    replaceGraph,
    autoLayout,
    navigate,
    setPlaceHint: (fn: () => Point) => {
      placeHint = fn
    },
    setMeasure: (fn: (n: WFNode) => Size) => {
      measure = fn
    },
  }
}

export type CanvasEditor = ReturnType<typeof useCanvasEditor>

export const CANVAS_EDITOR: InjectionKey<CanvasEditor> = Symbol('canvas-editor')

export function useInjectedEditor(): CanvasEditor | null {
  return inject(CANVAS_EDITOR, null)
}

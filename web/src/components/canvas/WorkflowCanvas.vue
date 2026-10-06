<script setup lang="ts">
import { computed, markRaw, nextTick, onBeforeUnmount, onMounted, provide, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueFlow, useVueFlow, type Connection, type Edge, type EdgeChange, type NodeChange } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { MiniMap } from '@vue-flow/minimap'
import './canvas.css'
import AgentNode from './nodes/AgentNode.vue'
import ControlNode from './nodes/ControlNode.vue'
import CollabNode from './nodes/CollabNode.vue'
import FlowEdge from './edges/FlowEdge.vue'
import EdgeLabelEditor from './edges/EdgeLabelEditor.vue'
import CanvasToolbar from './chrome/CanvasToolbar.vue'
import QuickAdd from './chrome/QuickAdd.vue'
import ShortcutHelp from './chrome/ShortcutHelp.vue'
import EmptyCanvas from './chrome/EmptyCanvas.vue'
import Icon from '../ui/Icon.vue'
import { useNodeDefs } from '@/lib/run/useNodeDefs'
import { theme } from '@/lib/shared/theme'
import type { NodeRunStatus, NodeType, WFEdge, WFNode } from '@/lib/shared/types'
import { CANVAS_CTX, type CanvasContext, type CanvasMode, type NodeMenuAction } from './composables/canvasContext'
import type { AddLink, CanvasEditor } from './composables/useCanvasEditor'
import { agentLookup, normHandle, type CanvasAgent } from './composables/outlets'
import { useFlowElements } from './composables/useFlowElements'
import { useCanvasShortcuts, isMac } from './composables/useCanvasShortcuts'
import { buildPaletteItems, decodePaletteDrag, paletteKey, PALETTE_MIME, type PaletteItem } from './composables/paletteItems'
import {
  AGENT_NODE_WIDTH,
  computeAutoLayout,
  estimateNodeSize,
  needsInitialLayout,
  prefersReducedMotion,
  type Point,
  type Size,
} from './composables/useAutoLayout'
import { buildDefaultWorkflow } from './composables/defaultTemplate'
import { createNode, type NodeSpec } from './composables/graphOps'
import { findFreeSpot } from './composables/placement'
import { alignmentGuides, type AlignmentGuides } from './composables/alignmentGuides'

const props = withDefaults(
  defineProps<{
    nodes: WFNode[]
    edges: WFEdge[]
    mode?: CanvasMode
    /** Required in edit mode. */
    editor?: CanvasEditor | null
    /** Run mode: project agents used to resolve outlets when nodes carry no caps snapshot. */
    agents?: CanvasAgent[]
    statusMap?: Record<string, NodeRunStatus>
    iterations?: Record<string, number>
    failReasons?: Record<string, string>
    activePath?: string[]
    selectedNode?: string | null
    /** Run mode follow toggle; undefined hides it. */
    follow?: boolean
    followNodeId?: string | null
    /** Lay the graph out once when it is opened without usable positions. */
    autoLayoutOnInit?: boolean
  }>(),
  {
    mode: 'edit',
    editor: null,
    agents: () => [],
    statusMap: undefined,
    iterations: undefined,
    failReasons: undefined,
    activePath: undefined,
    selectedNode: null,
    follow: undefined,
    followNodeId: null,
    autoLayoutOnInit: false,
  },
)

const emit = defineEmits<{
  (e: 'select-node', id: string): void
  (e: 'pane-click'): void
  (e: 'reply', id: string): void
  (e: 'save'): void
  (e: 'update:follow', value: boolean): void
}>()

const { t } = useI18n()
const { NODE_DEFS } = useNodeDefs()
const tr = (key: string, named?: Record<string, unknown>) => (named ? t(key, named) : t(key))
const typeText = (type: NodeType) => ({
  label: NODE_DEFS.value[type]?.label ?? String(type),
  desc: NODE_DEFS.value[type]?.desc ?? '',
})

const nodeTypes = { agent: markRaw(AgentNode), control: markRaw(ControlNode), collab: markRaw(CollabNode) } as any
const edgeTypes = { flow: markRaw(FlowEdge) } as any

const flowId = `wf-canvas-${Math.random().toString(36).slice(2, 8)}`
const {
  fitView,
  zoomIn,
  zoomOut,
  setCenter,
  screenToFlowCoordinate,
  viewport,
  findNode,
  dimensions,
} = useVueFlow(flowId)

const editing = computed(() => props.mode === 'edit' && !!props.editor)
const host = ref<HTMLElement | null>(null)
const hostSize = ref({ width: 0, height: 0 })
const connecting = ref<{ source: string; sourceHandle: string } | null>(null)
const hoveredEdge = ref<string | null>(null)
const showMinimap = ref(false)
const showHelp = ref(false)
const panning = ref(false)

type QuickAddState = { x: number; y: number; flow: Point; link?: AddLink; title: string; centered?: boolean }
const quickAdd = shallowRef<QuickAddState | null>(null)
const edgeEditor = shallowRef<{ id: string; x: number; y: number } | null>(null)
let lastPointer: { x: number; y: number } | null = null

const runLookup = computed(() => agentLookup(props.agents))
const displayLayout = shallowRef<Map<string, Point> | null>(null)
const { flowNodes, flowEdges } = useFlowElements({
  nodes: () => props.nodes,
  edges: () => props.edges,
  mode: () => props.mode,
  agents: () => props.editor?.agents.value ?? props.agents,
  lookup: () => props.editor?.lookup.value ?? runLookup.value,
  statusMap: () => props.statusMap,
  iterations: () => props.iterations,
  failReasons: () => props.failReasons,
  activePath: () => props.activePath,
  issues: () => props.editor?.nodeIssues.value,
  selectedNodes: () =>
    props.editor ? props.editor.selectedNodeIds.value : props.selectedNode ? [props.selectedNode] : [],
  selectedEdges: () => props.editor?.selectedEdgeIds.value ?? [],
  renamingId: () => props.editor?.renamingId.value ?? null,
  connecting,
  typeText,
  t: tr,
  positions: () => displayLayout.value,
})


const paletteItems = computed<PaletteItem[]>(() =>
  props.editor ? buildPaletteItems(props.editor.agents.value, typeText, tr) : [],
)

// ── Theme-dependent SVG colours (attributes cannot read CSS variables) ──
const colors = ref({ dot: '#1f1f26', node: '#36363e', mask: 'rgba(10,10,11,0.7)', ok: '', err: '', accent: '' })
function readColors() {
  if (typeof window === 'undefined') return
  const cs = getComputedStyle(document.documentElement)
  const rgb = (name: string, a?: number) => {
    const v = cs.getPropertyValue(name).trim()
    if (!v) return ''
    return a === undefined ? `rgb(${v})` : `rgb(${v} / ${a})`
  }
  colors.value = {
    dot: cs.getPropertyValue('--flow-dot').trim() || colors.value.dot,
    node: rgb('--c-line-strong') || colors.value.node,
    mask: rgb('--c-base', 0.7) || colors.value.mask,
    ok: rgb('--c-ok'),
    err: rgb('--c-err'),
    accent: rgb('--c-accent'),
  }
}
watch(theme, () => void nextTick(() => requestAnimationFrame(readColors)))

function minimapColor(n: { id: string }): string {
  const s = props.statusMap?.[n.id]
  if (s === 'completed') return colors.value.ok || colors.value.node
  if (s === 'failed') return colors.value.err || colors.value.node
  if (s === 'running' || s === 'waiting_human') return colors.value.accent || colors.value.node
  return colors.value.node
}

// ── Geometry helpers ──
function hostPoint(clientX: number, clientY: number) {
  const r = host.value?.getBoundingClientRect()
  return { x: clientX - (r?.left ?? 0), y: clientY - (r?.top ?? 0) }
}

function updateHostSize() {
  const r = host.value?.getBoundingClientRect()
  if (r) hostSize.value = { width: r.width, height: r.height }
}

function measure(n: WFNode) {
  const d = findNode(n.id)?.dimensions
  return d && d.width && d.height ? { width: d.width, height: d.height } : estimateNodeSize(n)
}

/** Size used when the new node's type is unknown (the largest card). */
const DEFAULT_PLACE_SIZE: Size = { width: AGENT_NODE_WIDTH, height: 108 }

function specSize(spec: NodeSpec): Size {
  return estimateNodeSize(createNode(spec, { x: 0, y: 0 }, []))
}

/** Nearest spot to `p` where a `size` card does not overlap an existing node. */
function freeSpot(p: Point, size: Size = DEFAULT_PLACE_SIZE): Point {
  const others = props.nodes.map((n) => ({ x: n.position?.x ?? 0, y: n.position?.y ?? 0, ...measure(n) }))
  return findFreeSpot(p, size, others)
}

function viewCenter(): Point {
  const r = host.value?.getBoundingClientRect()
  if (!r) return { x: 0, y: 0 }
  const c = screenToFlowCoordinate({ x: r.left + r.width / 2, y: r.top + r.height / 2 })
  return freeSpot({ x: c.x - 110, y: c.y - 40 })
}

function pointerFlow(): Point | undefined {
  if (!lastPointer) return undefined
  return screenToFlowCoordinate(lastPointer)
}

function nodeAnchor(spec: NodeSpec, p: Point): Point {
  return { x: p.x - specSize(spec).width / 2, y: p.y - 28 }
}

// ── Editor wiring ──
watch(
  () => props.editor,
  (ed) => {
    if (!ed) return
    ed.setPlaceHint(viewCenter)
    ed.setMeasure(measure)
  },
  { immediate: true },
)

function onNodesChange(changes: NodeChange[]) {
  const ed = props.editor
  if (!ed || props.mode !== 'edit') return
  let ids: string[] | null = null
  for (const c of changes) {
    if (c.type !== 'select') continue
    ids ??= [...ed.selectedNodeIds.value]
    if (c.selected && !ids.includes(c.id)) ids.push(c.id)
    if (!c.selected) ids = ids.filter((x) => x !== c.id)
  }
  if (ids && !sameSet(ids, ed.selectedNodeIds.value)) ed.setSelection(ids, ed.selectedEdgeIds.value)
}

function onEdgesChange(changes: EdgeChange[]) {
  const ed = props.editor
  if (!ed || props.mode !== 'edit') return
  let ids: string[] | null = null
  for (const c of changes) {
    if (c.type !== 'select') continue
    ids ??= [...ed.selectedEdgeIds.value]
    if (c.selected && !ids.includes(c.id)) ids.push(c.id)
    if (!c.selected) ids = ids.filter((x) => x !== c.id)
  }
  if (ids && !sameSet(ids, ed.selectedEdgeIds.value)) ed.setSelection(ed.selectedNodeIds.value, ids)
}

function sameSet(a: string[], b: string[]) {
  return a.length === b.length && a.every((x) => b.includes(x))
}

function onNodeClick({ node }: { node: { id: string } }) {
  if (props.mode === 'run') emit('select-node', node.id)
}

function onNodeDoubleClick({ node }: { node: { id: string } }) {
  if (editing.value) props.editor!.openInspector(node.id, true)
}

const guides = ref<AlignmentGuides | null>(null)

function onNodeDrag(e: { node: { id: string; position: Point }; nodes: { id: string }[] }) {
  if (!editing.value) return
  const dragged = new Set(e.nodes.map((n) => n.id))
  const own = props.nodes.find((n) => n.id === e.node.id)
  if (!own) return
  const others = props.nodes
    .filter((n) => !dragged.has(n.id))
    .map((n) => ({ id: n.id, x: n.position?.x ?? 0, y: n.position?.y ?? 0, ...measure(n) }))
  guides.value = alignmentGuides({ id: own.id, ...e.node.position, ...measure(own) }, others)
}

function onNodeDragStop(e: { nodes: { id: string; position: Point }[] }) {
  guides.value = null
  props.editor?.moveNodes(e.nodes.map((n) => ({ id: n.id, x: n.position.x, y: n.position.y })))
}

/** The mouseup that ends a connection drag also reaches the pane as a click; it must not close what that drag opened. */
let connectEndedAt = 0

function onPaneClick() {
  if (performance.now() - connectEndedAt < 50) return
  closeOverlays()
  props.editor?.clearSelection()
  emit('pane-click')
}

let connected = false
function onConnectStart(e: { nodeId?: string; handleId?: string | null; handleType?: string }) {
  if (!editing.value || e.handleType !== 'source' || !e.nodeId) return
  connected = false
  connecting.value = { source: e.nodeId, sourceHandle: normHandle(e.handleId) }
}

function onConnect(c: Connection) {
  connected = true
  props.editor?.connect({ source: c.source, sourceHandle: normHandle(c.sourceHandle), target: c.target })
}

function onConnectEnd(ev?: MouseEvent | TouchEvent) {
  const from = connecting.value
  connecting.value = null
  if (from) connectEndedAt = performance.now()
  if (!from || connected || !ev) return
  const pt = 'changedTouches' in ev ? ev.changedTouches[0] : (ev as MouseEvent)
  if (!pt) return
  const el = document.elementFromPoint(pt.clientX, pt.clientY) as HTMLElement | null
  if (el?.closest('.vue-flow__node, .vue-flow__handle')) return
  const src = props.nodes.find((n) => n.id === from.source)
  openQuickAdd(
    pt.clientX,
    pt.clientY,
    { kind: 'outlet', source: from.source, sourceHandle: from.sourceHandle },
    t('canvas.quickAdd.titleConnect', { label: src?.label || from.source }),
  )
}

function isValidConnection(c: Connection | Edge): boolean {
  // Vue Flow also runs this on every edge passed in through props; those are the model and always render.
  if ('id' in c && props.edges.some((e) => e.id === c.id)) return true
  if (!props.editor) return false
  return props.editor.checkConnection({ source: c.source, sourceHandle: normHandle(c.sourceHandle), target: c.target }).ok
}

// ── Adding nodes ──
function openQuickAdd(clientX: number, clientY: number, link: AddLink | undefined, title: string) {
  if (!editing.value) return
  updateHostSize()
  edgeEditor.value = null
  const p = hostPoint(clientX, clientY)
  quickAdd.value = { x: p.x, y: p.y, flow: screenToFlowCoordinate({ x: clientX, y: clientY }), link, title }
}

function openCommandPalette() {
  if (!editing.value) return
  updateHostSize()
  edgeEditor.value = null
  const ed = props.editor!
  const sel = ed.selectedNodeIds.value.length === 1 ? props.nodes.find((n) => n.id === ed.selectedNodeIds.value[0]) : null
  let flow: Point
  let link: AddLink | undefined
  if (sel) {
    const size = measure(sel)
    flow = freeSpot({ x: (sel.position?.x ?? 0) + size.width + 96, y: sel.position?.y ?? 0 })
    link = { kind: 'after', nodeId: sel.id }
  } else {
    const p = pointerFlow()
    flow = p ? { x: p.x - 110, y: p.y - 28 } : viewCenter()
  }
  quickAdd.value = { x: 0, y: 0, flow, link, title: t('canvas.quickAdd.titleCommand'), centered: true }
}

function onQuickPick(item: PaletteItem) {
  const q = quickAdd.value
  quickAdd.value = null
  if (!q || !props.editor) return
  const at = q.centered ? q.flow : q.link?.kind === 'outlet' ? { x: q.flow.x + 16, y: q.flow.y - 28 } : nodeAnchor(item.spec, q.flow)
  props.editor.addNode(item.spec, q.link?.kind === 'edge' ? at : freeSpot(at, specSize(item.spec)), q.link)
}

function onDragOver(ev: DragEvent) {
  if (!editing.value || !ev.dataTransfer?.types.includes(PALETTE_MIME)) return
  ev.preventDefault()
  ev.dataTransfer.dropEffect = 'copy'
}

function onDrop(ev: DragEvent) {
  if (!editing.value) return
  const spec = decodePaletteDrag(ev.dataTransfer?.getData(PALETTE_MIME))
  if (!spec) return
  ev.preventDefault()
  const p = screenToFlowCoordinate({ x: ev.clientX, y: ev.clientY })
  props.editor!.cancelPlacing()
  props.editor!.addNode(spec, freeSpot(nodeAnchor(spec, p), specSize(spec)))
}

// ── Click-to-place (palette click) ──
const placing = computed(() => (editing.value ? props.editor!.placing.value : null))
const ghostPointer = ref<{ x: number; y: number } | null>(null)
let placeDownAt: { x: number; y: number } | null = null

const placingItem = computed(() => {
  const spec = placing.value
  if (!spec) return null
  const key = paletteKey(spec)
  return paletteItems.value.find((i) => i.key === key) ?? null
})

const ghost = computed(() => {
  const spec = placing.value
  const p = ghostPointer.value
  if (!spec || !p) return null
  const size = specSize(spec)
  const flow = freeSpot(nodeAnchor(spec, screenToFlowCoordinate(p)), size)
  const { x, y, zoom } = viewport.value
  return { left: flow.x * zoom + x, top: flow.y * zoom + y, width: size.width * zoom, height: size.height * zoom }
})

watch(placing, (spec) => {
  if (!spec) ghostPointer.value = null
  else if (lastPointer) ghostPointer.value = { ...lastPointer }
})

function onPlacePointerDown(ev: PointerEvent) {
  placeDownAt = placing.value ? { x: ev.clientX, y: ev.clientY } : null
}

function onPlaceClick(ev: MouseEvent) {
  const spec = placing.value
  if (!spec || ev.button !== 0) return
  const el = ev.target as HTMLElement
  if (!el.closest('.vue-flow') || el.closest('.cchrome, .vue-flow__minimap')) return
  if (placeDownAt && Math.hypot(ev.clientX - placeDownAt.x, ev.clientY - placeDownAt.y) > 5) return
  ev.preventDefault()
  ev.stopPropagation()
  const p = screenToFlowCoordinate({ x: ev.clientX, y: ev.clientY })
  const ed = props.editor!
  if (!ev.shiftKey) ed.cancelPlacing()
  ed.addNode(spec, freeSpot(nodeAnchor(spec, p), specSize(spec)))
}

function onPlaceContextMenu(ev: MouseEvent) {
  if (!placing.value) return
  ev.preventDefault()
  props.editor!.cancelPlacing()
}

function onHostDblClick(ev: MouseEvent) {
  const el = ev.target as HTMLElement
  if (!editing.value || el.closest('.vue-flow__node, .vue-flow__edge, .cedge-mid, .cchrome, .vue-flow__minimap')) return
  if (!el.closest('.vue-flow')) return
  openQuickAdd(ev.clientX, ev.clientY, undefined, t('canvas.quickAdd.titleCommand'))
}

function onPointerMove(ev: PointerEvent) {
  lastPointer = { x: ev.clientX, y: ev.clientY }
  if (placing.value) ghostPointer.value = lastPointer
}

function onPointerLeave() {
  lastPointer = null
  ghostPointer.value = null
}

// ── Context for nodes / edges ──
function onNodeMenu(id: string, action: NodeMenuAction) {
  const ed = props.editor
  if (!ed) return
  if (action === 'edit') ed.openInspector(id, true)
  else if (action === 'rename') {
    ed.setSelection([id])
    ed.startRename(id)
  } else if (action === 'duplicate') ed.duplicate([id])
  else if (action === 'delete') ed.removeNodes([id])
}

function midpointClient(ev: MouseEvent) {
  const el = (ev.currentTarget as HTMLElement | null)?.closest('.cedge-mid') as HTMLElement | null
  const r = el?.getBoundingClientRect()
  return r ? { x: r.left + r.width / 2, y: r.top + r.height / 2 } : { x: ev.clientX, y: ev.clientY }
}

const ctx: CanvasContext = {
  mode: computed(() => props.mode),
  hoveredEdge,
  setEdgeHover: (id) => {
    hoveredEdge.value = id
  },
  onNodeMenu,
  onRename: (id, label) => {
    const ed = props.editor
    if (!ed) return
    if (label === null) ed.renamingId.value = null
    else ed.renameNode(id, label)
  },
  onReply: (id) => emit('reply', id),
  onEdgeInsert: (id, ev) => {
    const c = midpointClient(ev)
    openQuickAdd(c.x, c.y + 12, { kind: 'edge', edgeId: id }, t('canvas.quickAdd.titleInsert'))
  },
  onEdgeDelete: (id) => {
    if (edgeEditor.value?.id === id) edgeEditor.value = null
    props.editor?.removeEdge(id)
  },
  onEdgeEdit: (id, ev) => {
    if (!editing.value) return
    updateHostSize()
    quickAdd.value = null
    const c = midpointClient(ev)
    const p = hostPoint(c.x, c.y)
    edgeEditor.value = { id, x: p.x, y: p.y }
  },
}
provide(CANVAS_CTX, ctx)

const editingEdge = computed(() => (edgeEditor.value ? props.edges.find((e) => e.id === edgeEditor.value!.id) ?? null : null))

function closeEdgeEditor() {
  edgeEditor.value = null
  props.editor?.history.flush()
}

function closeOverlays(): boolean {
  if (showHelp.value) {
    showHelp.value = false
    return true
  }
  if (quickAdd.value) {
    quickAdd.value = null
    return true
  }
  if (edgeEditor.value) {
    closeEdgeEditor()
    return true
  }
  return false
}

// ── Layout / viewport ──
const reduced = prefersReducedMotion()

/** Lowest zoom the opening view may use; wider graphs overflow instead of shrinking past legibility. */
const INITIAL_MIN_ZOOM = 0.7

async function fit(duration = reduced ? 0 : 240, minZoom?: number) {
  await nextTick()
  await fitView({ padding: 0.2, maxZoom: 1, minZoom, duration })
}

async function layout(animate = true) {
  if (!props.editor) return
  await props.editor.autoLayout(animate && !reduced)
  await fit()
}

function waitFrame() {
  return new Promise<void>((r) => (typeof requestAnimationFrame === 'function' ? requestAnimationFrame(() => r()) : r()))
}

async function startFromTemplate() {
  const ed = props.editor
  if (!ed) return
  const g = buildDefaultWorkflow(ed.agents.value, tr)
  ed.history.begin()
  try {
    ed.replaceGraph(g.nodes, g.edges)
    await nextTick()
    await waitFrame()
    await ed.autoLayout(false)
  } finally {
    ed.history.end()
  }
  await fit(0)
}

function startBlank() {
  const ed = props.editor
  if (!ed) return
  ed.history.begin()
  try {
    ed.addNode({ type: 'input' }, { x: 0, y: 0 })
    ed.addNode({ type: 'output' }, { x: 320, y: 0 })
  } finally {
    ed.history.end()
  }
  ed.clearSelection()
  void fit(0)
}

let initialized = false
async function onNodesInitialized() {
  if (initialized) return
  initialized = true
  if (props.autoLayoutOnInit && needsInitialLayout(props.nodes)) {
    if (props.editor) {
      await props.editor.autoLayout(false)
      props.editor.history.reset()
    } else {
      displayLayout.value = await computeAutoLayout(props.nodes, props.edges, measure)
      await nextTick()
    }
  }
  await fit(0, INITIAL_MIN_ZOOM)
  if (props.follow && props.followNodeId) centerOn(props.followNodeId, 0)
}

// ── Follow (run mode) ──
function onMoveStart() {
  if (props.follow) emit('update:follow', false)
}

function centerOn(id: string, duration = reduced ? 0 : 400) {
  const n = findNode(id)
  if (!n) return
  const w = n.dimensions?.width || 220
  const h = n.dimensions?.height || 80
  void setCenter(n.computedPosition.x + w / 2, n.computedPosition.y + h / 2, {
    zoom: Math.max(viewport.value.zoom, 0.8),
    duration,
  })
}

watch(
  () => [props.follow, props.followNodeId] as const,
  ([on, id]) => {
    if (on && id) void nextTick(() => centerOn(id))
  },
)

// ── Keyboard ──
function focusNode(id: string) {
  const el = host.value?.querySelector<HTMLElement>(`.vue-flow__node[data-id="${CSS.escape(id)}"]`)
  el?.focus({ preventScroll: true })
  const n = findNode(id)
  if (!n || !host.value) return
  const z = viewport.value.zoom
  const x = n.computedPosition.x * z + viewport.value.x
  const y = n.computedPosition.y * z + viewport.value.y
  const w = (n.dimensions?.width || 220) * z
  const h = (n.dimensions?.height || 80) * z
  if (x < 0 || y < 0 || x + w > dimensions.value.width || y + h > dimensions.value.height) centerOn(id)
}

function focusedNodeId(): string | null {
  const el = document.activeElement as HTMLElement | null
  if (!el || !host.value?.contains(el)) return null
  return el.closest<HTMLElement>('.vue-flow__node')?.dataset.id ?? null
}

function nav(dir: 'left' | 'right' | 'up' | 'down') {
  const focused = focusedNodeId()
  if (!editing.value) {
    if (focused && props.mode === 'run') emit('select-node', focused)
    return false
  }
  const ed = props.editor!
  if (focused && !ed.selectedNodeIds.value.includes(focused)) ed.setSelection([focused])
  const id = ed.navigate(dir)
  if (id) focusNode(id)
}

function onHostKeydown(ev: KeyboardEvent) {
  if (ev.key !== 'Enter' && ev.key !== ' ') return
  const id = (ev.target as HTMLElement).closest<HTMLElement>('.vue-flow__node')?.dataset.id
  if (!id || (ev.target as HTMLElement).closest('input, textarea, button')) return
  ev.preventDefault()
  if (props.mode === 'run') emit('select-node', id)
  else props.editor?.openInspector(id, ev.key === 'Enter')
}

function shortcutsActive(): boolean {
  const el = host.value
  if (!el || !el.isConnected || el.offsetParent === null) return false
  const modal = document.querySelector('[aria-modal="true"]')
  return !modal || el.contains(modal)
}

const ed = () => props.editor!
useCanvasShortcuts(
  {
    undo: () => (editing.value ? void ed().undo() : false),
    redo: () => (editing.value ? void ed().redo() : false),
    copy: () => (editing.value ? ed().copy() : false),
    paste: () => (editing.value && ed().canPaste() ? ed().paste(pointerFlow()) : false),
    duplicate: () => (editing.value ? void ed().duplicate() : false),
    delete: () => (editing.value ? ed().removeSelection() : false),
    selectAll: () => (editing.value ? void ed().selectAll() : false),
    save: () => (editing.value ? void emit('save') : false),
    fitView: () => void fit(),
    autoLayout: () => (editing.value ? void layout() : false),
    quickAdd: () => (editing.value ? void openCommandPalette() : false),
    help: () => {
      showHelp.value = true
    },
    rename: () => (editing.value ? void ed().startRename() : false),
    escape: () => {
      if (props.editor?.cancelPlacing()) return
      if (closeOverlays()) return
      if (props.editor?.renamingId.value) {
        props.editor.renamingId.value = null
        return
      }
      if (props.editor && (props.editor.selectedNodeIds.value.length || props.editor.selectedEdgeIds.value.length)) {
        props.editor.clearSelection()
        return
      }
      return false
    },
    navLeft: () => nav('left'),
    navRight: () => nav('right'),
    navUp: () => nav('up'),
    navDown: () => nav('down'),
  },
  shortcutsActive,
)

// Space-drag pan cursor feedback.
function onSpace(ev: KeyboardEvent) {
  if (ev.code !== 'Space' || (ev.target as HTMLElement)?.closest?.('input, textarea, select, [contenteditable="true"]')) return
  panning.value = ev.type === 'keydown'
}

let ro: ResizeObserver | null = null
onMounted(() => {
  readColors()
  updateHostSize()
  window.addEventListener('keydown', onSpace)
  window.addEventListener('keyup', onSpace)
  if (typeof ResizeObserver !== 'undefined' && host.value) {
    ro = new ResizeObserver(updateHostSize)
    ro.observe(host.value)
  }
})
onBeforeUnmount(() => {
  props.editor?.cancelPlacing()
  window.removeEventListener('keydown', onSpace)
  window.removeEventListener('keyup', onSpace)
  ro?.disconnect()
})

const showEmpty = computed(() => editing.value && props.nodes.length === 0 && !placing.value)

const selectedCount = computed(() => (editing.value ? props.editor!.selectedNodeIds.value.length : 0))

/**
 * Edit mode: dragging empty canvas draws a selection box; Space + drag, the middle /
 * right button or a two-finger scroll pans; pinch or Mod + scroll zooms.
 */
const flowInteraction = computed(() =>
  editing.value
    ? {
        selectionKeyCode: true,
        panOnDrag: [1, 2],
        panOnScroll: true,
        zoomActivationKeyCode: isMac() ? 'Meta' : 'Control',
      }
    : { selectionKeyCode: null, panOnDrag: true, panOnScroll: false },
)

defineExpose({ fit, layout, centerOn, openCommandPalette })
</script>

<template>
  <div
    ref="host"
    class="canvas-host relative h-full w-full"
    :class="{ 'is-panning': panning, 'is-connecting': !!connecting, 'is-placing': !!placing }"
    :aria-label="t('canvas.aria.canvas')"
    role="application"
    data-testid="workflow-canvas"
    @drop="onDrop"
    @dragover="onDragOver"
    @dblclick="onHostDblClick"
    @pointermove="onPointerMove"
    @pointerleave="onPointerLeave"
    @pointerdown.capture="onPlacePointerDown"
    @click.capture="onPlaceClick"
    @contextmenu.capture="onPlaceContextMenu"
    @keydown="onHostKeydown"
  >
    <VueFlow
      :id="flowId"
      :nodes="flowNodes"
      :edges="flowEdges"
      :node-types="nodeTypes"
      :edge-types="edgeTypes"
      :nodes-draggable="editing"
      :nodes-connectable="editing"
      :elements-selectable="true"
      :nodes-focusable="true"
      :edges-focusable="editing"
      :disable-keyboard-a11y="true"
      v-bind="flowInteraction"
      :multi-selection-key-code="'Shift'"
      :delete-key-code="null"
      :pan-activation-key-code="'Space'"
      :zoom-on-scroll="true"
      :zoom-on-pinch="true"
      :zoom-on-double-click="false"
      :snap-to-grid="true"
      :snap-grid="[8, 8]"
      :min-zoom="0.25"
      :max-zoom="2"
      :connection-radius="28"
      :is-valid-connection="isValidConnection"
      :connection-line-options="{ type: 'smoothstep' as any, style: { stroke: 'var(--flow-edge-active)', strokeWidth: 1.75 } }"
      @nodes-change="onNodesChange"
      @edges-change="onEdgesChange"
      @node-click="onNodeClick"
      @node-double-click="onNodeDoubleClick"
      @node-drag="onNodeDrag"
      @node-drag-stop="onNodeDragStop"
      @pane-click="onPaneClick"
      @connect-start="onConnectStart"
      @connect="onConnect"
      @connect-end="onConnectEnd"
      @move-start="onMoveStart"
      @nodes-initialized="onNodesInitialized"
    >
      <Background variant="dots" :gap="16" :size="1.2" :pattern-color="colors.dot" />
      <MiniMap
        v-if="showMinimap"
        pannable
        zoomable
        :node-color="minimapColor"
        :mask-color="colors.mask"
        class="cchrome !bottom-14 !left-3 !right-auto"
        data-testid="canvas-minimap"
      />
    </VueFlow>

    <div v-if="guides" class="pointer-events-none absolute inset-0 z-[4] overflow-hidden" aria-hidden="true" data-testid="canvas-guides">
      <div
        v-for="x in guides.vertical"
        :key="`v${x}`"
        class="canvas-guide canvas-guide-v"
        :style="{ left: `${x * viewport.zoom + viewport.x}px` }"
      />
      <div
        v-for="y in guides.horizontal"
        :key="`h${y}`"
        class="canvas-guide canvas-guide-h"
        :style="{ top: `${y * viewport.zoom + viewport.y}px` }"
      />
    </div>

    <EmptyCanvas v-if="showEmpty" @template="startFromTemplate" @blank="startBlank" />

    <div
      v-if="ghost && placingItem"
      class="canvas-ghost pointer-events-none absolute z-[5]"
      :style="{ left: `${ghost.left}px`, top: `${ghost.top}px`, width: `${ghost.width}px`, height: `${ghost.height}px` }"
      aria-hidden="true"
      data-testid="canvas-place-ghost"
    >
      <span class="truncate">{{ placingItem.label }}</span>
    </div>

    <div class="pointer-events-none absolute left-1/2 top-3 z-20 -translate-x-1/2">
      <div
        v-if="placing"
        class="cchrome pointer-events-auto inline-flex items-center gap-2 px-3 py-1.5 text-[12px] text-txt2"
        role="status"
        data-testid="canvas-place-hint"
      >
        <Icon name="plus" :size="13" class="text-accent-2" />
        <span>{{ t('canvas.placement.hint', { label: placingItem?.label ?? '' }) }}</span>
        <button type="button" class="text-txt3 underline hover:text-txt" data-testid="canvas-place-cancel" @click.stop="editor?.cancelPlacing()">
          {{ t('common.buttons.cancel') }}
        </button>
      </div>
      <div
        v-else-if="selectedCount > 0"
        class="cchrome pointer-events-auto inline-flex items-center gap-1 py-1 pl-3 pr-1 text-[12px] text-txt2"
        role="toolbar"
        :aria-label="t('canvas.selection.count', { n: selectedCount })"
        data-testid="canvas-selection-bar"
      >
        <span>{{ t('canvas.selection.count', { n: selectedCount }) }}</span>
        <span class="text-txt3" aria-hidden="true">·</span>
        <button
          type="button"
          class="inline-flex h-7 items-center gap-1 rounded-md px-2 text-err hover:bg-err/10"
          data-testid="canvas-selection-delete"
          @click.stop="editor?.removeSelection()"
        >
          <Icon name="trash" :size="13" />{{ t('canvas.selection.delete') }}
        </button>
      </div>
    </div>

    <CanvasToolbar
      :zoom="viewport.zoom"
      :editable="editing"
      :minimap="showMinimap"
      :can-undo="editor?.history.canUndo.value"
      :can-redo="editor?.history.canRedo.value"
      @zoom-in="zoomIn({ duration: reduced ? 0 : 160 })"
      @zoom-out="zoomOut({ duration: reduced ? 0 : 160 })"
      @fit="fit()"
      @layout="layout()"
      @toggle-minimap="showMinimap = !showMinimap"
      @undo="editor?.undo()"
      @redo="editor?.redo()"
      @help="showHelp = true"
    />

    <button
      v-if="mode === 'run' && follow !== undefined"
      type="button"
      class="cchrome absolute right-3 top-3 z-10 inline-flex h-8 items-center gap-1.5 px-2.5 text-[12px]"
      :class="follow ? 'text-accent-2' : 'text-txt2 hover:text-txt'"
      :aria-pressed="follow"
      :title="follow ? t('canvas.toolbar.followOn') : t('canvas.toolbar.followOff')"
      data-testid="canvas-follow"
      @click="emit('update:follow', !follow)"
    >
      <Icon name="crosshair" :size="14" />{{ t('canvas.toolbar.follow') }}
    </button>

    <QuickAdd
      v-if="quickAdd"
      :items="paletteItems"
      :title="quickAdd.title"
      :x="quickAdd.x"
      :y="quickAdd.y"
      :centered="quickAdd.centered"
      :bounds="hostSize"
      @pick="onQuickPick"
      @close="quickAdd = null"
    />

    <EdgeLabelEditor
      v-if="edgeEditor && editingEdge"
      :key="edgeEditor.id"
      :edge="editingEdge"
      :x="edgeEditor.x"
      :y="edgeEditor.y"
      :bounds="hostSize"
      @update="(p) => editor?.updateEdge(edgeEditor!.id, p)"
      @delete="ctx.onEdgeDelete(edgeEditor!.id)"
      @close="closeEdgeEditor"
    />

    <ShortcutHelp v-if="showHelp" @close="showHelp = false" />

    <slot />
  </div>
</template>

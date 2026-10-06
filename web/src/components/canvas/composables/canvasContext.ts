import { inject, type InjectionKey, type Ref } from 'vue'
import type { NodeRunStatus, NodeType } from '@/lib/shared/types'
import type { CapabilityFlags, Outlet } from './outlets'

/** edit: editable; run: read-only with run status; view: read-only snapshot (version preview). */
export type CanvasMode = 'edit' | 'run' | 'view'
export type NodeMenuAction = 'edit' | 'rename' | 'duplicate' | 'delete'
export type EdgeRunState = 'traversed' | 'active' | 'dim'

/** Port state while a connection is being dragged. */
export interface PortConnectState {
  valid: boolean
  reason?: string
}

/** Flow node `data` for every canvas node. Kept flat so it can be fingerprinted. */
export interface CanvasNodeData {
  nodeType: NodeType
  title: string
  typeLabel: string
  icon: string
  subtitle?: string
  agentName?: string
  agentMissing?: boolean
  flags?: CapabilityFlags
  goal?: string
  outlets: Outlet[]
  hasTarget: boolean
  mode: CanvasMode
  status?: NodeRunStatus
  iteration?: number
  failReason?: string
  issues?: string[]
  connect?: PortConnectState | null
  renaming?: boolean
}

export interface CanvasEdgeData {
  tone: 'default' | 'ok' | 'err'
  dashed: boolean
  label: string
  hasCondition: boolean
  run?: EdgeRunState
  editable: boolean
  sourceLabel: string
  targetLabel: string
  /** A connection being dragged from this edge's outlet would replace it. */
  replacing?: boolean
}

export interface CanvasContext {
  mode: Ref<CanvasMode>
  hoveredEdge: Ref<string | null>
  setEdgeHover: (id: string | null) => void
  onNodeMenu: (id: string, action: NodeMenuAction) => void
  onRename: (id: string, label: string | null) => void
  onReply: (id: string) => void
  onEdgeInsert: (id: string, ev: MouseEvent) => void
  onEdgeDelete: (id: string) => void
  onEdgeEdit: (id: string, ev: MouseEvent) => void
}

export const CANVAS_CTX: InjectionKey<CanvasContext> = Symbol('canvas-ctx')

export function useCanvasContext(): CanvasContext | null {
  return inject(CANVAS_CTX, null)
}

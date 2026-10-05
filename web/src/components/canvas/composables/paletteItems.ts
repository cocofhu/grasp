import type { NodeType } from '@/lib/shared/types'
import type { NodeSpec } from './graphOps'
import { capabilitySummary, type CanvasAgent, type Translate } from './outlets'

export type PaletteGroup = 'agent' | 'control' | 'collab'

export interface PaletteItem {
  key: string
  group: PaletteGroup
  spec: NodeSpec
  label: string
  desc: string
  icon?: string
  /** Agent items render an avatar instead of an icon. */
  agent?: string
}

export const CONTROL_TYPES: NodeType[] = ['input', 'output', 'set_var', 'branch']
export const COLLAB_TYPES: NodeType[] = ['human_gate', 'proposal_select']

export const NODE_ICONS: Record<NodeType, string> = {
  input: 'input',
  output: 'output',
  set_var: 'variable',
  branch: 'branch',
  agent: 'robot',
  human_gate: 'gate',
  proposal_select: 'check',
}

export const PALETTE_MIME = 'application/grasp-node'

export function buildPaletteItems(
  agents: CanvasAgent[],
  typeText: (type: NodeType) => { label: string; desc: string },
  t: Translate,
): PaletteItem[] {
  const items: PaletteItem[] = agents.map((a) => ({
    key: `agent:${a.name}`,
    group: 'agent',
    spec: { type: 'agent', agentProfile: a.name },
    label: a.name,
    desc: capabilitySummary(a.capabilities, t),
    agent: a.name,
  }))
  items.push({
    key: 'agent:',
    group: 'agent',
    spec: { type: 'agent' },
    label: t('canvas.palette.blankAgent'),
    desc: t('canvas.palette.blankAgentDesc'),
    icon: NODE_ICONS.agent,
  })
  for (const [group, types] of [
    ['control', CONTROL_TYPES],
    ['collab', COLLAB_TYPES],
  ] as const) {
    for (const type of types) {
      const { label, desc } = typeText(type)
      items.push({ key: `type:${type}`, group, spec: { type }, label, desc, icon: NODE_ICONS[type] })
    }
  }
  return items
}

export function filterPaletteItems(items: PaletteItem[], q: string): PaletteItem[] {
  const s = q.trim().toLowerCase()
  if (!s) return items
  return items.filter(
    (i) =>
      i.label.toLowerCase().includes(s) ||
      i.desc.toLowerCase().includes(s) ||
      i.spec.type.toLowerCase().includes(s),
  )
}

export function encodePaletteDrag(spec: NodeSpec): string {
  return JSON.stringify({ type: spec.type, agentProfile: spec.agentProfile })
}

export function decodePaletteDrag(raw: string | undefined | null): NodeSpec | null {
  if (!raw) return null
  try {
    const v = JSON.parse(raw) as NodeSpec
    return v && typeof v.type === 'string' ? { type: v.type, agentProfile: v.agentProfile || undefined } : null
  } catch {
    return null
  }
}

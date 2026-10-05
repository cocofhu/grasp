import type { AgentCapabilities } from '@/lib/api/apiTypes'
import type { WFNode } from '@/lib/shared/types'
import { summarizeCapabilities } from '@/lib/workflow/agentCapabilities'
import { nodeOutlets as rawNodeOutlets, type AgentCapsLookup } from '@/lib/workflow/nodeOutlets'

export type { AgentCapabilities }

/** The subset of an Agent the canvas needs. */
export interface CanvasAgent {
  name: string
  projectId?: string
  capabilities?: AgentCapabilities | null
}

export type OutletTone = 'default' | 'ok' | 'err'

/** One source handle on the right side of a node. id '' is the plain outlet. */
export interface Outlet {
  id: string
  label: string
  tone: OutletTone
  /** An edge still leaves through a handle the node no longer has. */
  stale?: boolean
}

export type Translate = (key: string, named?: Record<string, unknown>) => string

export function agentLookup(agents: CanvasAgent[]): AgentCapsLookup {
  return new Map(agents.map((a) => [a.name, a]))
}

export function findAgent(agents: CanvasAgent[], name: unknown): CanvasAgent | undefined {
  const n = String(name ?? '').trim()
  return n ? agents.find((a) => a.name === n) : undefined
}

/** Display outlets: labels translated, pass / fail toned. Plain outlets carry no label. */
export function nodeOutlets(node: WFNode, agents: AgentCapsLookup, t: Translate): Outlet[] {
  return rawNodeOutlets(node, agents).map((o) => ({
    id: o.id,
    label: o.kind === 'default' ? '' : o.label || (o.labelKey ? t(o.labelKey) : o.id),
    tone: o.kind === 'pass' ? 'ok' : o.kind === 'fail' ? 'err' : 'default',
  }))
}

/** Normalizes a Vue Flow handle id (null / undefined / '') to the stored sourceHandle form. */
export function normHandle(h: string | null | undefined): string {
  return h ? String(h) : ''
}

export interface CapabilityFlags {
  ask: boolean
  preview: boolean
  review: boolean
  gate: boolean
}

export function capabilityFlags(caps: AgentCapabilities | null | undefined): CapabilityFlags {
  const s = summarizeCapabilities(caps)
  return {
    ask: !!s && (s.interaction === 'clarify' || s.asksHuman),
    preview: !!s?.preview,
    review: !!s?.review,
    gate: !!s?.gated,
  }
}

/** One-line capability summary, e.g. "多轮澄清 · 应用预览 · 写 需求规格、计划". */
export function capabilitySummary(caps: AgentCapabilities | null | undefined, t: Translate): string {
  const s = summarizeCapabilities(caps)
  if (!s) return t('canvas.caps.undeclared')
  const parts = [t(`nodes.capabilities.interaction.${s.interaction}`)]
  if (s.review) parts.push(t('canvas.caps.review'))
  if (s.preview) parts.push(t('canvas.caps.preview'))
  if (s.writes.length) parts.push(t('canvas.caps.writes', { list: s.writes.map((w) => schemaLabel(w.name, t)).join(t('canvas.caps.listSep')) }))
  return parts.join(' · ')
}

export function schemaLabel(name: string, t: Translate): string {
  const key = `nodes.schemas.${name}.label`
  const v = t(key)
  return v === key ? name : v
}

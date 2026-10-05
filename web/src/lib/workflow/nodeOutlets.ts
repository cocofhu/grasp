// Named node outlets. An edge's sourceHandle must equal one of the source
// node's outlet ids; "" is the plain outlet (edges without a handle).

import type { AgentCapabilities } from '@/lib/api/apiTypes'
import type { WFNode } from '@/lib/shared/types'
import { isGated } from './agentCapabilities'

export const PASS_OUTLET = 'pass'
export const FAIL_OUTLET = 'fail'
export const ELSE_OUTLET = 'else'
export const DEFAULT_OUTLET = ''

export type OutletKind = 'default' | 'pass' | 'fail' | 'case' | 'else' | 'action'

export interface NodeOutlet {
  /** Edge.sourceHandle value; "" for the plain outlet. */
  id: string
  kind: OutletKind
  /** Literal label (branch `when`, human_gate action label); empty for default / pass / fail / else. */
  label: string
  /** i18n key for kinds without a literal label (default / pass / fail / else). */
  labelKey?: string
}

/** Agents by name; only `capabilities` is read. */
export type AgentCapsLookup =
  | Record<string, { capabilities?: AgentCapabilities | null } | undefined>
  | Map<string, { capabilities?: AgentCapabilities | null } | undefined>

const DEFAULT: NodeOutlet = { id: DEFAULT_OUTLET, kind: 'default', label: '', labelKey: 'nodes.outlets.default' }

/** Outlets of an agent node whose Agent has caps: pass / fail when it writes a verdict schema. */
export function agentOutlets(caps: AgentCapabilities | null | undefined): NodeOutlet[] {
  if (!isGated(caps)) return [DEFAULT]
  return [
    { id: PASS_OUTLET, kind: 'pass', label: '', labelKey: 'nodes.outlets.pass' },
    { id: FAIL_OUTLET, kind: 'fail', label: '', labelKey: 'nodes.outlets.fail' },
  ]
}

function lookupCaps(agents: AgentCapsLookup | undefined, name: string): AgentCapabilities | undefined {
  if (!agents || !name) return undefined
  const a = agents instanceof Map ? agents.get(name) : agents[name]
  return a?.capabilities ?? undefined
}

/** Capabilities governing an agent node: the run snapshot (`caps`) wins over the live Agent. */
export function nodeCapabilities(
  node: Pick<WFNode, 'type' | 'config' | 'caps'>,
  agents?: AgentCapsLookup,
): AgentCapabilities | undefined {
  if (node.type !== 'agent') return undefined
  return node.caps ?? lookupCaps(agents, String(node.config?.agent_profile ?? '').trim())
}

/** Every outlet of a node, in display order. */
export function nodeOutlets(
  node: Pick<WFNode, 'type' | 'config' | 'caps'>,
  agents?: AgentCapsLookup,
): NodeOutlet[] {
  switch (node.type) {
    case 'output':
      return []
    case 'agent':
      return agentOutlets(nodeCapabilities(node, agents))
    case 'branch': {
      const cases = Array.isArray(node.config?.cases) ? node.config.cases : []
      const out: NodeOutlet[] = []
      for (const c of cases) {
        const id = String(c?.id ?? '').trim()
        if (!id || id === ELSE_OUTLET) continue
        out.push({ id, kind: 'case', label: String(c?.when ?? '').trim() || id })
      }
      out.push({ id: ELSE_OUTLET, kind: 'else', label: '', labelKey: 'nodes.outlets.else' })
      return out
    }
    case 'human_gate': {
      const actions = Array.isArray(node.config?.actions) ? node.config.actions : []
      const out: NodeOutlet[] = []
      for (const a of actions) {
        const id = String(a?.id ?? '').trim()
        if (!id) continue
        out.push({ id, kind: 'action', label: String(a?.label ?? '').trim() || id })
      }
      return out
    }
    default:
      return [DEFAULT]
  }
}

/** True when `handle` names an outlet of the node ("" / undefined = plain outlet). */
export function isNodeOutlet(
  node: Pick<WFNode, 'type' | 'config' | 'caps'>,
  handle: string | undefined,
  agents?: AgentCapsLookup,
): boolean {
  const id = handle ?? DEFAULT_OUTLET
  return nodeOutlets(node, agents).some((o) => o.id === id)
}

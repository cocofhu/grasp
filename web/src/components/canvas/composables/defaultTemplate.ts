import type { WFEdge, WFNode } from '@/lib/shared/types'
import { isClarify, isGated, writesSchema } from '@/lib/workflow/agentCapabilities'
import { FAIL_OUTLET, PASS_OUTLET } from '@/lib/workflow/nodeOutlets'
import { defaultConfig } from './graphOps'
import type { CanvasAgent, Translate } from './outlets'

type Role = 'clarify' | 'implement' | 'test_review'

const ROLE_MATCH: Record<Role, (a: CanvasAgent) => boolean> = {
  clarify: (a) => isClarify(a.capabilities),
  implement: (a) => writesSchema(a.capabilities, 'implementation_result'),
  test_review: (a) => isGated(a.capabilities),
}

/** Picks a distinct project Agent per role: template id first, then matching capabilities. */
export function pickTemplateAgents(agents: (CanvasAgent & { templateId?: string })[]): Record<Role, string> {
  const used = new Set<string>()
  const out = { clarify: '', implement: '', test_review: '' } as Record<Role, string>
  for (const role of Object.keys(ROLE_MATCH) as Role[]) {
    const byTemplate = agents.find((a) => !used.has(a.name) && a.templateId === role)
    const byCaps = agents.find((a) => !used.has(a.name) && ROLE_MATCH[role](a))
    const pick = byTemplate ?? byCaps
    if (pick) {
      used.add(pick.name)
      out[role] = pick.name
    }
  }
  return out
}

/**
 * The default workflow: input → 需求澄清 → 实现 → 测试评审, pass → output, fail → 实现.
 * Positions are left at the origin; the caller lays the graph out.
 */
export function buildDefaultWorkflow(
  agents: (CanvasAgent & { templateId?: string })[],
  t: Translate,
): { nodes: WFNode[]; edges: WFEdge[] } {
  const picked = pickTemplateAgents(agents)
  const at = { x: 0, y: 0 }
  const agentNode = (id: string, role: Role): WFNode => ({
    id,
    type: 'agent',
    label: t(`canvas.template.labels.${role}`),
    position: { ...at },
    config: { agent_profile: picked[role], prompt: t(`canvas.template.goals.${role}`) },
  })
  const nodes: WFNode[] = [
    { id: 'input', type: 'input', label: t('canvas.template.labels.input'), position: { ...at }, config: defaultConfig('input') },
    agentNode('clarify', 'clarify'),
    agentNode('implement', 'implement'),
    agentNode('test_review', 'test_review'),
    { id: 'output', type: 'output', label: t('canvas.template.labels.output'), position: { ...at }, config: defaultConfig('output') },
  ]
  const edges: WFEdge[] = [
    { id: 'e_input_clarify', source: 'input', target: 'clarify' },
    { id: 'e_clarify_implement', source: 'clarify', target: 'implement' },
    { id: 'e_implement_test', source: 'implement', target: 'test_review' },
    { id: 'e_test_pass', source: 'test_review', sourceHandle: PASS_OUTLET, target: 'output' },
    { id: 'e_test_fail', source: 'test_review', sourceHandle: FAIL_OUTLET, target: 'implement' },
  ]
  return { nodes, edges }
}

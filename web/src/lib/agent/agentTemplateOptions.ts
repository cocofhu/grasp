/**
 * Template catalog helpers for Agent create wizard (plan g1 / g2).
 * Order: blank → test → preflight → remaining roles.
 */
import type { AgentTemplateOption } from '@/components/agent/AgentTemplateSelect.vue'

export type TeamTemplateRow = {
  id: string
  embedName: string
  roleLabelZh: string
  summary: string
}

/** Unique-deliver subtitle for known packs (mirrors clarify mock). */
const DELIVER_BY_ID: Record<string, string> = {
  test: 'TestAgent · set_test_result',
  preflight: 'PreflightAgent · set_preflight',
  clarify: 'ClarifyAgent · set_clarified_requirement',
  research: 'ResearchAgent · set_research',
  plan: 'PlanAgent · set_plan',
  proposal: 'ProposalAgent · set_proposals',
  implement: 'ImplementAgent · set_implementation_result',
  review: 'ReviewAgent · set_review',
  visual: 'VisualAgent · write_artifact',
  preview: 'PreviewAgent · set_preview',
}

const PINNED_ORDER = ['test', 'preflight'] as const

export function blankTemplateOption(label: string, subtitle: string): AgentTemplateOption {
  return { id: 'blank', name: label, subtitle }
}

export function buildTemplateOptions(
  rows: TeamTemplateRow[],
  blankLabel: string,
  blankSubtitle: string,
): AgentTemplateOption[] {
  const byId = new Map(rows.map((r) => [r.id, r]))
  const out: AgentTemplateOption[] = [blankTemplateOption(blankLabel, blankSubtitle)]
  const seen = new Set<string>()
  for (const id of PINNED_ORDER) {
    const r = byId.get(id)
    if (!r) continue
    out.push({
      id: r.id,
      name: r.roleLabelZh,
      subtitle: DELIVER_BY_ID[r.id] || `${r.embedName}${r.summary ? ` · ${r.summary}` : ''}`,
    })
    seen.add(id)
  }
  for (const r of rows) {
    if (seen.has(r.id)) continue
    out.push({
      id: r.id,
      name: r.roleLabelZh,
      subtitle: DELIVER_BY_ID[r.id] || `${r.embedName}${r.summary ? ` · ${r.summary}` : ''}`,
    })
  }
  return out
}

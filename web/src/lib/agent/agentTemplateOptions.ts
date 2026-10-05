/**
 * Template catalog helpers for the Agent create wizard: blank first, then the
 * built-in templates in server (workflow) order.
 */
import type { AgentTemplate } from '@/lib/api/apiTypes'
import type { AgentTemplateOption } from '@/components/agent/AgentTemplateSelect.vue'

export function blankTemplateOption(label: string, subtitle: string): AgentTemplateOption {
  return { id: 'blank', name: label, subtitle }
}

export function buildTemplateOptions(
  rows: AgentTemplate[],
  blankLabel: string,
  blankSubtitle: string,
): AgentTemplateOption[] {
  const out: AgentTemplateOption[] = [blankTemplateOption(blankLabel, blankSubtitle)]
  for (const r of rows) {
    if (!r.id || r.id === 'blank') continue
    out.push({
      id: r.id,
      name: r.roleLabelZh || r.embedName,
      subtitle: r.embedName,
      description: r.summary || '',
      capabilities: r.capabilities,
    })
  }
  return out
}

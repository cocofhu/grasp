/**
 * Template catalog helpers for the Agent create wizard: blank first, then the
 * built-in templates in server (workflow) order.
 */
import type { AgentTemplate } from '@/lib/api/apiTypes'
import type { AgentTemplateOption } from '@/components/agent/AgentTemplateSelect.vue'

export function blankTemplateOption(label: string, subtitle: string): AgentTemplateOption {
  return { id: 'blank', name: label, subtitle }
}

type Translate = (key: string) => string
type TranslateExists = (key: string) => boolean

/** Built-in templates show localized copy; unknown ids fall back to the server's label and summary. */
export function buildTemplateOptions(
  rows: AgentTemplate[],
  t: Translate,
  te: TranslateExists,
): AgentTemplateOption[] {
  const out: AgentTemplateOption[] = [
    blankTemplateOption(t('pages.agentStudio.wizard.basics.templateBlank'), t('pages.agentStudio.wizard.basics.templateBlankSub')),
  ]
  for (const r of rows) {
    if (!r.id || r.id === 'blank') continue
    const key = `pages.onboarding.team.templates.${r.id}`
    const known = te(`${key}.title`)
    out.push({
      id: r.id,
      name: known ? t(`${key}.title`) : r.roleLabelZh || r.embedName,
      description: known ? t(`${key}.desc`) : r.summary || '',
      capabilities: r.capabilities,
    })
  }
  return out
}

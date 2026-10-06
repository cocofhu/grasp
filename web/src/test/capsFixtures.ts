// Capability snapshots of the three built-in templates, for tests.
import type { AgentCapabilities } from '@/lib/api/apiTypes'

export const CLARIFY_CAPS: AgentCapabilities = {
  interaction: 'clarify',
  tools: ['ask_question', 'set_artifact_preview', 'set_preview'],
  reads: ['*'],
  writes: [
    { schema: 'clarified_requirement', required: true },
    { schema: 'plan', required: true },
    { schema: 'research' },
    { schema: 'root_cause' },
    { schema: 'page' },
  ],
}

export const IMPLEMENT_CAPS: AgentCapabilities = {
  interaction: 'auto',
  review: true,
  tools: ['set_preview', 'update_plan_status'],
  reads: ['*'],
  writes: [{ schema: 'implementation_result', required: true }],
}

export const TEST_REVIEW_CAPS: AgentCapabilities = {
  interaction: 'auto',
  review: true,
  tools: ['set_preview'],
  reads: ['*'],
  writes: [
    { schema: 'test_result', required: true },
    { schema: 'review', required: true },
  ],
}

/** Plain auto Agent without review or products. */
export const AUTO_CAPS: AgentCapabilities = { interaction: 'auto', reads: ['*'] }

/** Auto Agent writing one required schema; `extra` overrides any field. */
export function writesCaps(schema: string, extra: Partial<AgentCapabilities> = {}): AgentCapabilities {
  return { interaction: 'auto', reads: ['*'], writes: [{ schema, required: true }], ...extra }
}

/** Clarify Agent writing only the requirement spec. */
export const ASK_CAPS: AgentCapabilities = {
  interaction: 'clarify',
  tools: ['ask_question'],
  reads: ['*'],
  writes: [{ schema: 'clarified_requirement', required: true }],
}

/** Reviewed auto Agent that registers an app preview. */
export const PREVIEW_REVIEW_CAPS: AgentCapabilities = {
  interaction: 'auto',
  review: true,
  tools: ['set_preview'],
  reads: ['*'],
}

/** Reviewed auto Agent writing the research report. */
export const RESEARCH_CAPS: AgentCapabilities = writesCaps('research', { review: true })

/** Reviewed auto Agent writing a page.html product. */
export const PAGE_CAPS: AgentCapabilities = writesCaps('page', { review: true, tools: ['set_artifact_preview'] })

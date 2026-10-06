/** Morphological rules for PM progress citations (mirrors server pm_citations.go). */

const RUN_ID_RE = /^run-[0-9a-f]{8}$/i
const WORKFLOW_ID_RE = /^wf-[0-9a-f]{8}$/i
const ARTIFACT_ID_RE = /^art-[0-9a-f]{8}$/i
const ARTIFACT_NAME_RE = /^[a-z0-9][a-z0-9._-]*\.[a-z0-9]{1,16}$/i
const GATE_TARGET_RE = /^run-[0-9a-f]{8}(?::[a-z0-9][a-z0-9_.-]*)?$/i
const PLAN_ID_RE = /^g\d+(?:\.\d+)?$/i
const PLAN_SCOPED_RE = /^run-[0-9a-f]{8}:g\d+(?:\.\d+)?$/i

export function isValidRunIdShape(id: string): boolean {
  return RUN_ID_RE.test(id.trim())
}

/** True when the route id cannot be a real Run (e.g. bare "trigger"). */
export function isClearlyInvalidRunRouteId(id: string): boolean {
  const t = id.trim()
  if (!t) return true
  // Production IDs are run-+8hex; anything without run- prefix is never a Run.
  if (!/^run-/i.test(t)) return true
  return false
}

export function isValidPmCitationShape(type: string, targetId: string): boolean {
  const typ = type.trim().toLowerCase()
  const target = targetId.trim()
  if (!target) return false
  switch (typ) {
    case 'run':
      return RUN_ID_RE.test(target)
    case 'workflow':
      return WORKFLOW_ID_RE.test(target)
    case 'artifact':
      return ARTIFACT_ID_RE.test(target) || ARTIFACT_NAME_RE.test(target)
    case 'gate':
      return GATE_TARGET_RE.test(target)
    case 'plan':
      return PLAN_ID_RE.test(target) || PLAN_SCOPED_RE.test(target)
    default:
      return false
  }
}

export function shortRunId(runId: string): string {
  return runId.replace(/^run-/i, '')
}

/** True when the model-supplied summary is just the bare extract key (type:raw). */
export function isBareExtractSnippet(type: string, snippet: string | undefined, targetId: string): boolean {
  if (!snippet) return false
  const s = snippet.trim().toLowerCase()
  const typ = type.trim().toLowerCase()
  const tid = targetId.trim().toLowerCase()
  return s === `${typ}:${tid}` || s === tid
}

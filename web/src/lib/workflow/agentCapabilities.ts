// Agent capabilities (agent.json `capabilities`) and the product schemas they
// reference. Schemas come from the server-generated node manifest; validation
// mirrors server/internal/models/capabilities.go.

import manifest from '@/data/nodeManifest.generated.json'
import type { AgentCapabilities, GrantableTool, ProductWrite } from '@/lib/api/apiTypes'

export type ProductSchema = {
  name: string
  label: string
  artifactName: string
  setTool?: string
  outputKey: string
  outputJsonKey?: string
  verdict?: boolean
}

export const PRODUCT_SCHEMAS: ProductSchema[] = manifest.schemas as ProductSchema[]

export const GRANTABLE_TOOLS: GrantableTool[] = manifest.tools as GrantableTool[]

export const READ_ALL = '*'

export function schemaByName(name: string): ProductSchema | undefined {
  return PRODUCT_SCHEMAS.find((s) => s.name === name)
}

export function schemaByArtifact(artifactName: string): ProductSchema | undefined {
  return PRODUCT_SCHEMAS.find((s) => s.artifactName === artifactName)
}

export function isVerdictSchema(name: string): boolean {
  return !!schemaByName(name)?.verdict
}

type Caps = AgentCapabilities | null | undefined

export function isClarify(caps: Caps): boolean {
  return caps?.interaction === 'clarify'
}

/** Auto Agent that parks for human review after its run. */
export function reviewEnabled(caps: Caps): boolean {
  return !!caps && !isClarify(caps) && !!caps.review
}

/** Holds a human conversation at all (clarify dialogue or post-run review). */
export function isInteractive(caps: Caps): boolean {
  return isClarify(caps) || reviewEnabled(caps)
}

export function hasTool(caps: Caps, tool: GrantableTool | string): boolean {
  return !!caps?.tools?.includes(tool)
}

/** May register a running app preview (set_preview). */
export function canPreview(caps: Caps): boolean {
  return hasTool(caps, 'set_preview')
}

export function writesSchema(caps: Caps, schema: string): boolean {
  return !!caps?.writes?.some((w) => w.schema === schema)
}

/** Writes a verdict product (test_result / review), so the node gets pass / fail outlets. */
export function isGated(caps: Caps): boolean {
  return !!caps?.writes?.some((w) => isVerdictSchema(w.schema))
}

export function readsAll(caps: Caps): boolean {
  return !!caps?.reads?.some((r) => r.trim() === READ_ALL)
}

export type DeclaredProduct = ProductSchema & { required: boolean }

/** Products the Agent declares, in declaration order (unknown schemas skipped). */
export function declaredProducts(caps: Caps): DeclaredProduct[] {
  const out: DeclaredProduct[] = []
  for (const w of caps?.writes || []) {
    const s = schemaByName(w.schema)
    if (s) out.push({ ...s, required: !!w.required })
  }
  return out
}

export type CapabilityIssueCode =
  | 'missing'
  | 'interaction'
  | 'tool'
  | 'clarifyNeedsAskQuestion'
  | 'clarifyReview'
  | 'maxRounds'
  | 'unknownSchema'
  | 'duplicateSchema'

export type CapabilityIssue = { code: CapabilityIssueCode; value?: string }

/** i18n key for a capability issue (params: { value }). */
export function capabilityIssueKey(issue: CapabilityIssue): string {
  return `nodes.capabilities.errors.${issue.code}`
}

/** First problem in caps, or null. Same rules and order as the server. */
export function validateCapabilities(caps: Caps): CapabilityIssue | null {
  if (!caps) return { code: 'missing' }
  if (caps.interaction !== 'auto' && caps.interaction !== 'clarify') {
    return { code: 'interaction', value: String(caps.interaction ?? '') }
  }
  for (const tool of caps.tools || []) {
    if (!GRANTABLE_TOOLS.includes(tool as GrantableTool)) return { code: 'tool', value: tool }
  }
  if (isClarify(caps) && !hasTool(caps, 'ask_question')) return { code: 'clarifyNeedsAskQuestion' }
  if (isClarify(caps) && caps.review) return { code: 'clarifyReview' }
  if ((caps.maxRounds ?? 0) < 0) return { code: 'maxRounds' }
  const seen = new Set<string>()
  for (const w of caps.writes || []) {
    if (!schemaByName(w.schema)) return { code: 'unknownSchema', value: w.schema }
    if (seen.has(w.schema)) return { code: 'duplicateSchema', value: w.schema }
    seen.add(w.schema)
  }
  return null
}

/** Canonical payload: drops empty lists / false flags so dirty compare and saves stay stable. */
export function normalizeCapabilities(caps: AgentCapabilities): AgentCapabilities {
  const out: AgentCapabilities = { interaction: caps.interaction }
  if (caps.review && caps.interaction !== 'clarify') out.review = true
  const tools = GRANTABLE_TOOLS.filter((t) => caps.tools?.includes(t))
  const unknownTools = (caps.tools || []).filter((t) => !GRANTABLE_TOOLS.includes(t as GrantableTool))
  if (tools.length || unknownTools.length) out.tools = [...tools, ...unknownTools]
  const reads = (caps.reads || []).map((r) => r.trim()).filter(Boolean)
  if (reads.length) out.reads = reads.includes(READ_ALL) ? [READ_ALL] : Array.from(new Set(reads))
  const writes: ProductWrite[] = (caps.writes || [])
    .filter((w) => w.schema)
    .map((w) => (w.required ? { schema: w.schema, required: true } : { schema: w.schema }))
  if (writes.length) out.writes = writes
  if (caps.maxRounds && caps.maxRounds > 0) out.maxRounds = Math.floor(caps.maxRounds)
  return out
}

export type CapabilitySummary = {
  interaction: AgentCapabilities['interaction']
  review: boolean
  gated: boolean
  preview: boolean
  asksHuman: boolean
  tools: string[]
  readsAll: boolean
  reads: string[]
  writes: DeclaredProduct[]
}

/** Display-ready digest for cards, the canvas and template pickers. */
export function summarizeCapabilities(caps: Caps): CapabilitySummary | null {
  if (!caps) return null
  return {
    interaction: caps.interaction,
    review: reviewEnabled(caps),
    gated: isGated(caps),
    preview: canPreview(caps),
    asksHuman: hasTool(caps, 'ask_question') || hasTool(caps, 'ask_form'),
    tools: [...(caps.tools || [])],
    readsAll: readsAll(caps),
    reads: (caps.reads || []).filter((r) => r.trim() && r.trim() !== READ_ALL),
    writes: declaredProducts(caps),
  }
}

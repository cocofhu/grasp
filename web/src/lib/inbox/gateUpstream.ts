import { OUTPUT_KEY_TO_ARTIFACT } from '@/lib/run/structuredArtifacts'

export type GateUpstreamRef = { nodeId: string; key: string }

/** Used by gate editor primary-product resolution. */

/** One primary product resolved from a gate body_template. */
export type GatePrimaryProductRef = {
  name: string
  kind: 'json' | 'html' | 'markdown' | 'text' | 'image'
  /** Non-text (e.g. image): shown in tabs but not editable/saveable. */
  readonly?: boolean
  nodeId?: string
  outputKey?: string
}

export type NodeExecLike = {
  iteration?: number
  status?: string
  outputs?: Record<string, any>
}

export type ResolveUpstreamResult = {
  outputs: Record<string, any> | null
  /** Iteration used for the preview banner (pointer N; null without a pointer). */
  selectedIteration: number | null
  /** Gate has upstreamNodeId + upstreamIteration. */
  usedPointer: boolean
  /**
   * Pointer was set but the matching nodeExecutions row was missing.
   * Callers must fall back to the run artifact store (or empty), never the
   * equals-iteration heuristic.
   */
  pointerMiss: boolean
}

const BODY_OUTPUT_REF = /\{\{\s*nodes\.([^.}\s]+)\.outputs\.([a-z_]+)\s*\}\}/g
const BODY_ARTIFACT_REF =
  /\{\{\s*artifact\s*\(\s*["']([^"']+)["']\s*\)\s*\}\}|artifact\s*\(\s*["']([^"']+)["']\s*\)/g

export function isReadonlyArtifactKind(kind: string | undefined): boolean {
  return (kind || '').toLowerCase() === 'image'
}

export function inferArtifactKind(name: string): GatePrimaryProductRef['kind'] {
  const n = name.toLowerCase()
  if (n.endsWith('.json')) return 'json'
  if (n.endsWith('.html') || n.endsWith('.htm')) return 'html'
  if (n.endsWith('.md') || n.endsWith('.markdown')) return 'markdown'
  if (
    n.endsWith('.png') ||
    n.endsWith('.jpg') ||
    n.endsWith('.jpeg') ||
    n.endsWith('.webp') ||
    n.endsWith('.gif')
  ) {
    return 'image'
  }
  return 'text'
}

function withReadonly(p: GatePrimaryProductRef): GatePrimaryProductRef {
  const kind = p.kind || inferArtifactKind(p.name)
  return {
    ...p,
    kind,
    readonly: p.readonly ?? isReadonlyArtifactKind(kind),
  }
}

/** Pick product ref: page/page.html preferred, else first {{nodes.*.outputs.*}}. */
export function pickProductRef(bodyTemplate: string): GateUpstreamRef | null {
  const products = listPrimaryProducts(bodyTemplate)
  const page = products.find((p) => p.outputKey === 'page' && p.nodeId)
  if (page?.nodeId && page.outputKey) {
    return { nodeId: page.nodeId, key: page.outputKey }
  }
  const first = products.find((p) => p.nodeId && p.outputKey)
  if (first?.nodeId && first.outputKey) {
    return { nodeId: first.nodeId, key: first.outputKey }
  }
  return null
}

/**
 * List all primary products from body_template: nodes.*.outputs.* and
 * artifact("name"). Dedupes by artifact name; preserves discovery order
 * (output refs first, then artifact() refs).
 */
export function listPrimaryProducts(bodyTemplate: string): GatePrimaryProductRef[] {
  const seen = new Set<string>()
  const out: GatePrimaryProductRef[] = []

  const add = (p: GatePrimaryProductRef) => {
    const name = (p.name || '').trim()
    if (!name || seen.has(name)) return
    seen.add(name)
    out.push(withReadonly({ ...p, name, kind: p.kind || inferArtifactKind(name) }))
  }

  BODY_OUTPUT_REF.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = BODY_OUTPUT_REF.exec(bodyTemplate || '')) !== null) {
    const nodeId = m[1]
    const key = m[2]
    const name = OUTPUT_KEY_TO_ARTIFACT[key]
    if (!name) continue
    add({ name, nodeId, outputKey: key, kind: inferArtifactKind(name) })
  }

  BODY_ARTIFACT_REF.lastIndex = 0
  while ((m = BODY_ARTIFACT_REF.exec(bodyTemplate || '')) !== null) {
    const name = (m[1] || m[2] || '').trim()
    if (!name) continue
    const outputKey = Object.entries(OUTPUT_KEY_TO_ARTIFACT).find(([, v]) => v === name)?.[0]
    add({ name, outputKey, kind: inferArtifactKind(name) })
  }

  return out
}

export type NodeConfigLike = {
  id: string
  config?: Record<string, unknown>
}

/**
 * Names that appear only in upstream produces (not in primary whitelist).
 * Used for the "not editable" hint aligned with page.html prototype.
 * Scans the full run.nodes produces union so produces-only artifacts from
 * nodes not referenced by body_template (e.g. outputs+artifact mix) still surface.
 */
export function listExcludedProduces(
  bodyTemplate: string,
  nodes: NodeConfigLike[] | undefined,
): string[] {
  const products = listPrimaryProducts(bodyTemplate)
  const primary = new Set(products.map((p) => p.name))
  const excluded: string[] = []
  const seen = new Set<string>()

  const addProduces = (raw: unknown) => {
    if (typeof raw !== 'string' || !raw.trim()) return
    for (const part of raw.split(',')) {
      const name = part.trim()
      if (!name || seen.has(name) || primary.has(name)) continue
      seen.add(name)
      excluded.push(name)
    }
  }

  for (const n of nodes || []) {
    addProduces(n.config?.produces)
  }

  return excluded
}

/**
 * Resolve upstream outputs for a gate preview: exact pointer iteration match,
 * else a pointer miss (no equals fallback). Gates without a pointer have no
 * upstream snapshot.
 */
export function resolveUpstreamOutputs(opts: {
  execsByNode: Record<string, NodeExecLike[] | undefined>
  upstreamNodeId?: string
  upstreamIteration?: number
}): ResolveUpstreamResult {
  const { execsByNode, upstreamNodeId, upstreamIteration } = opts

  const hasPointer =
    !!upstreamNodeId &&
    upstreamIteration != null &&
    Number.isFinite(upstreamIteration) &&
    upstreamIteration > 0

  if (hasPointer) {
    const execs = execsByNode[upstreamNodeId!] || []
    const hit = execs.find((e) => e.iteration === upstreamIteration)
    if (hit?.outputs) {
      return {
        outputs: hit.outputs,
        selectedIteration: upstreamIteration!,
        usedPointer: true,
        pointerMiss: false,
      }
    }
    return {
      outputs: null,
      selectedIteration: upstreamIteration!,
      usedPointer: true,
      pointerMiss: true,
    }
  }

  return { outputs: null, selectedIteration: null, usedPointer: false, pointerMiss: false }
}

/** Banner N: pointer iteration when set, else the resolved iteration. */
export function reviewingUpstreamN(opts: {
  upstreamIteration?: number
  selectedIteration: number | null
}): number | null {
  if (opts.upstreamIteration != null && opts.upstreamIteration > 0) {
    return opts.upstreamIteration
  }
  return opts.selectedIteration
}

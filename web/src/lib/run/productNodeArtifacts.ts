// Single source for StructuredProductPanel renderable node types ↔ artifact names.
// Manifest products cover Agent deliverables; proposal_select is the confirmed pick.

import manifest from '@/data/nodeManifest.generated.json'

type ManifestArtifact = {
  artifactName: string
  outputKey?: string
  required?: boolean
}

type ProductEntry = {
  type: string
  artifactName?: string
  outputKey?: string
  artifacts?: ManifestArtifact[]
}

const products = manifest.products as ProductEntry[]

/** Canonical registry type: historical approve graphs resolve to grasp. */
function canonicalProductType(nodeType: string): string {
  return nodeType === 'approve' ? 'grasp' : nodeType
}

function primaryArtifactName(p: ProductEntry): string | undefined {
  if (p.artifactName) return p.artifactName
  const required = p.artifacts?.find((a) => a.required)
  return required?.artifactName || p.artifacts?.[0]?.artifactName
}

const fromManifest = Object.fromEntries(
  products
    .map((p) => [p.type, primaryArtifactName(p)] as const)
    .filter((entry): entry is [string, string] => !!entry[1]),
)

/** Reserved product artifact per node type (generic `agent` intentionally absent). */
export const PRODUCT_ARTIFACT_BY_TYPE: Record<string, string> = {
  ...fromManifest,
  // Confirmed single proposal — same card as proposals.json, highlighted as selected.
  proposal_select: 'proposal.json',
}

export const PRODUCT_NODE_TYPES: string[] = Object.keys(PRODUCT_ARTIFACT_BY_TYPE)

export function productArtifactName(nodeType: string): string | undefined {
  return PRODUCT_ARTIFACT_BY_TYPE[canonicalProductType(nodeType)]
}

export type ProductArtifactSpec = { name: string; required: boolean; outputKey?: string }

/** All reserved deliverables for a node type (Grasp lists several). */
export function productArtifactsForType(nodeType: string): ProductArtifactSpec[] {
  const entry = products.find((p) => p.type === canonicalProductType(nodeType))
  if (entry?.artifacts?.length) {
    return entry.artifacts
      .filter((a) => a.artifactName)
      .map((a) => ({
        name: a.artifactName,
        required: !!a.required,
        outputKey: a.outputKey,
      }))
  }
  if (entry?.artifactName) {
    return [
      {
        name: entry.artifactName,
        required: true,
        outputKey: entry.outputKey,
      },
    ]
  }
  const name = productArtifactName(nodeType)
  return name ? [{ name, required: true }] : []
}

/** Run-scoped reserved names: one copy per run; later status writes must not hide it. */
export const RUN_SCOPED_PRODUCT_NAMES = new Set(['plan.json'])

const FINISHED_NODE_STATUSES = new Set(['completed', 'failed', 'cancelled'])

/** Pick the store row a structured product panel should bind. */
export function resolveStructuredProductArtifact<T extends { name: string; nodeId?: string }>(opts: {
  name: string
  nodeId: string
  nodeType: string
  nodeStatus?: string
  hasSnapshot?: boolean
  artifacts: T[]
}): T | null {
  const { name, nodeId, nodeType, artifacts } = opts
  if (!name) return null
  const owned = artifacts.find((a) => a.name === name && a.nodeId === nodeId)
  if (owned) return owned
  const listed = productArtifactsForType(nodeType)
  if (listed.length <= 1) {
    return artifacts.find((a) => a.name === name) || null
  }
  // plan.json is run-global. implement update_plan_status used to rewrite
  // nodeId to the implement node; after this node has finished (or already
  // snapshotted the plan), still bind the current run copy. In-flight
  // Approve without a snapshot must not pick up an upstream leftover.
  const finished = FINISHED_NODE_STATUSES.has(String(opts.nodeStatus || ''))
  if (RUN_SCOPED_PRODUCT_NAMES.has(name) && (opts.hasSnapshot || finished)) {
    return artifacts.find((a) => a.name === name) || null
  }
  return null
}

/** Inspector / editor output rows derived from the nodereg manifest contract. */
export function productOutputDefs(
  nodeType: string,
  extras: { key: string; desc: string }[] = [],
): { key: string; desc: string }[] {
  const outs: { key: string; desc: string }[] = []
  const seen = new Set<string>()
  for (const a of productArtifactsForType(nodeType)) {
    if (!a.outputKey || seen.has(a.outputKey)) continue
    seen.add(a.outputKey)
    outs.push({
      key: a.outputKey,
      desc: `nodes.${nodeType}.outputs.${a.outputKey}.desc`,
    })
    if (a.name !== 'page.html') {
      const jsonKey = `${a.outputKey}_json`
      outs.push({
        key: jsonKey,
        desc: `nodes.${nodeType}.outputs.${jsonKey}.desc`,
      })
    }
  }
  for (const e of extras) {
    if (seen.has(e.key)) continue
    seen.add(e.key)
    outs.push(e)
  }
  return outs
}

import { describe, expect, it } from 'vitest'
import { PRODUCT_ARTIFACT_BY_TYPE, PRODUCT_NODE_TYPES, productArtifactName, productArtifactsForType, productOutputDefs, resolveStructuredProductArtifact } from './productNodeArtifacts'

describe('productNodeArtifacts', () => {
  it('covers StructuredProductPanel node types including proposal_select', () => {
    expect(PRODUCT_NODE_TYPES).toEqual(expect.arrayContaining(['react', 'research', 'plan', 'proposal_select', 'visual']))
    expect(productArtifactName('research')).toBe('research.json')
    expect(productArtifactName('proposal_select')).toBe('proposal.json')
    expect(PRODUCT_ARTIFACT_BY_TYPE.react).toBe('clarified_requirement.json')
  })

  it('lists Approve required + optional products', () => {
    expect(PRODUCT_NODE_TYPES).toContain('grasp')
    expect(productArtifactName('grasp')).toBe('clarified_requirement.json')
    const arts = productArtifactsForType('grasp')
    expect(arts.filter((a) => a.required).map((a) => a.name)).toEqual([
      'clarified_requirement.json',
      'plan.json',
    ])
    expect(arts.filter((a) => !a.required).map((a) => a.name)).toEqual(
      expect.arrayContaining(['research.json', 'proposals.json', 'page.html']),
    )
  })

  it('includes outputKey for single-product and multi-product types', () => {
    expect(productArtifactsForType('plan')[0]?.outputKey).toBe('plan')
    expect(productArtifactsForType('research')[0]?.outputKey).toBe('research')
    expect(productArtifactsForType('grasp').map((a) => a.outputKey)).toEqual([
      'clarified_requirement',
      'plan',
      'research',
      'proposals',
      'page',
    ])
  })

  it('builds inspector output defs from the manifest', () => {
    expect(productOutputDefs('plan')).toEqual([
      { key: 'plan', desc: 'nodes.plan.outputs.plan.desc' },
      { key: 'plan_json', desc: 'nodes.plan.outputs.plan_json.desc' },
    ])
    expect(productOutputDefs('visual', [{ key: 'artifact_id', desc: 'nodes.visual.outputs.artifact_id.desc' }])).toEqual([
      { key: 'page', desc: 'nodes.visual.outputs.page.desc' },
      { key: 'artifact_id', desc: 'nodes.visual.outputs.artifact_id.desc' },
    ])
  })

  it('keeps Approve plan.json visible after implement steals the store nodeId', () => {
    const plan = { name: 'plan.json', nodeId: 'implement' }
    expect(
      resolveStructuredProductArtifact({
        name: 'plan.json',
        nodeId: 'approve',
        nodeType: 'approve',
        nodeStatus: 'completed',
        artifacts: [plan],
      }),
    ).toEqual(plan)
    expect(
      resolveStructuredProductArtifact({
        name: 'plan.json',
        nodeId: 'approve',
        nodeType: 'approve',
        nodeStatus: 'waiting_human',
        artifacts: [plan],
      }),
    ).toBeNull()
    expect(
      resolveStructuredProductArtifact({
        name: 'research.json',
        nodeId: 'approve',
        nodeType: 'approve',
        nodeStatus: 'completed',
        artifacts: [{ name: 'research.json', nodeId: 'research' }],
      }),
    ).toBeNull()
  })
})

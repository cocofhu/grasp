import { describe, expect, it } from 'vitest'
import {
  isProductNode,
  productArtifactName,
  productArtifactsForNode,
  resolveStructuredProductArtifact,
} from './productNodeArtifacts'
import { AUTO_CAPS, CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'

const clarify = { type: 'agent' as const, caps: CLARIFY_CAPS }

describe('productNodeArtifacts', () => {
  it('derives products from the agent caps snapshot', () => {
    expect(productArtifactName(clarify)).toBe('clarified_requirement.json')
    const arts = productArtifactsForNode(clarify)
    expect(arts.filter((a) => a.required).map((a) => a.name)).toEqual(['clarified_requirement.json', 'plan.json'])
    expect(arts.filter((a) => !a.required).map((a) => a.name)).toEqual(
      expect.arrayContaining(['research.json', 'root_cause.json', 'proposals.json', 'page.html']),
    )
    expect(arts.map((a) => a.outputKey)).toEqual(['clarified_requirement', 'plan', 'research', 'proposals', 'root_cause', 'page'])
    expect(productArtifactName({ type: 'agent', caps: IMPLEMENT_CAPS })).toBe('implementation_result.json')
    expect(productArtifactsForNode({ type: 'agent', caps: TEST_REVIEW_CAPS }).map((a) => a.name)).toEqual([
      'test_result.json',
      'review.json',
    ])
  })

  it('covers proposal_select and skips nodes without products', () => {
    expect(productArtifactName({ type: 'proposal_select' })).toBe('proposal.json')
    expect(isProductNode({ type: 'agent', caps: AUTO_CAPS })).toBe(false)
    expect(isProductNode({ type: 'agent' })).toBe(false)
    expect(isProductNode({ type: 'human_gate' })).toBe(false)
    expect(isProductNode(null)).toBe(false)
  })

  it('keeps plan.json visible after a later node steals the store nodeId', () => {
    const plan = { name: 'plan.json', nodeId: 'implement' }
    const base = { name: 'plan.json', nodeId: 'clarify', node: clarify, artifacts: [plan] }
    expect(resolveStructuredProductArtifact({ ...base, nodeStatus: 'completed' })).toEqual(plan)
    expect(resolveStructuredProductArtifact({ ...base, nodeStatus: 'waiting_human' })).toBeNull()
    expect(resolveStructuredProductArtifact({ ...base, nodeStatus: 'running', hasSnapshot: true })).toEqual(plan)
    expect(
      resolveStructuredProductArtifact({
        name: 'research.json',
        nodeId: 'clarify',
        node: clarify,
        nodeStatus: 'completed',
        artifacts: [{ name: 'research.json', nodeId: 'research' }],
      }),
    ).toBeNull()
    expect(
      resolveStructuredProductArtifact({
        name: 'implementation_result.json',
        nodeId: 'impl',
        node: { type: 'agent', caps: IMPLEMENT_CAPS },
        artifacts: [{ name: 'implementation_result.json', nodeId: 'other' }],
      }),
    ).toEqual({ name: 'implementation_result.json', nodeId: 'other' })
    expect(resolveStructuredProductArtifact({ ...base, name: '' })).toBeNull()
  })
})

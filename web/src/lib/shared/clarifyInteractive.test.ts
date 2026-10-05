import { describe, expect, it } from 'vitest'
import {
  findGraphNode,
  isClarifyNode,
  isInteractiveNode,
  isPreviewNode,
  isReviewNode,
} from './clarifyInteractive'
import { AUTO_CAPS, CLARIFY_CAPS, IMPLEMENT_CAPS } from '@/test/capsFixtures'
import type { WFNode } from './types'

const node = (id: string, caps?: WFNode['caps'], type = 'agent'): WFNode =>
  ({ id, type, position: { x: 0, y: 0 }, data: { label: id, config: {} }, caps }) as WFNode

describe('clarifyInteractive', () => {
  it('derives interactivity from the agent caps snapshot', () => {
    const clarify = node('c', CLARIFY_CAPS)
    const impl = node('i', IMPLEMENT_CAPS)
    const auto = node('a', AUTO_CAPS)
    expect(isClarifyNode(clarify)).toBe(true)
    expect(isReviewNode(clarify)).toBe(false)
    expect(isInteractiveNode(clarify)).toBe(true)
    expect(isClarifyNode(impl)).toBe(false)
    expect(isReviewNode(impl)).toBe(true)
    expect(isPreviewNode(impl)).toBe(true)
    expect(isInteractiveNode(auto)).toBe(false)
    expect(isPreviewNode(auto)).toBe(false)
  })

  it('ignores caps on non-agent nodes and missing nodes', () => {
    expect(isClarifyNode(node('h', CLARIFY_CAPS, 'human_gate'))).toBe(false)
    expect(isInteractiveNode(null)).toBe(false)
    expect(isReviewNode(undefined)).toBe(false)
  })

  it('finds graph nodes by id', () => {
    const nodes = [node('a'), node('b')]
    expect(findGraphNode(nodes, 'b')?.id).toBe('b')
    expect(findGraphNode(nodes, '')).toBeUndefined()
    expect(findGraphNode(null, 'a')).toBeUndefined()
  })
})

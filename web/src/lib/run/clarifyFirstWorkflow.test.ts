import { describe, expect, it } from 'vitest'
import {
  clarifyFirstNodeId,
  isClarifyFirstWorkflow,
  isPublishedClarifyFirst,
  withPublishedSnapshot,
} from './clarifyFirstWorkflow'
import { AUTO_CAPS, CLARIFY_CAPS } from '@/test/capsFixtures'
import type { WFEdge, WFNode } from '@/lib/shared/types'

const AGENTS = {
  clarifier: { capabilities: CLARIFY_CAPS },
  worker: { capabilities: AUTO_CAPS },
}

type NodeSpec = { id: string; type?: WFNode['type']; profile?: string; caps?: WFNode['caps'] }

function graph(nodes: NodeSpec[], edges: Partial<WFEdge>[]) {
  return {
    nodes: nodes.map((n) => ({
      id: n.id,
      type: n.type || 'agent',
      label: n.id,
      position: { x: 0, y: 0 },
      config: n.profile ? { agent_profile: n.profile } : {},
      caps: n.caps,
    })) as WFNode[],
    edges: edges.map((e, i) => ({
      id: e.id || `e${i}`,
      source: String(e.source),
      target: String(e.target),
      when: e.when,
      kind: e.kind,
    })) as WFEdge[],
  }
}

describe('isClarifyFirstWorkflow', () => {
  it('matches input → clarify Agent (caps from the agent list)', () => {
    const g = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'clarifier' },
        { id: 'out', type: 'output' },
      ],
      [
        { source: 'in', target: 'ask' },
        { source: 'ask', target: 'out' },
      ],
    )
    expect(isClarifyFirstWorkflow(g, AGENTS)).toBe(true)
    expect(isClarifyFirstWorkflow(g)).toBe(false)
  })

  it('prefers the node caps snapshot over the agent list', () => {
    const g = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'worker', caps: CLARIFY_CAPS },
      ],
      [{ source: 'in', target: 'ask' }],
    )
    expect(isClarifyFirstWorkflow(g, AGENTS)).toBe(true)
  })

  it('rejects input → auto Agent', () => {
    const g = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'w', profile: 'worker' },
      ],
      [{ source: 'in', target: 'w' }],
    )
    expect(isClarifyFirstWorkflow(g, AGENTS)).toBe(false)
  })

  it('rejects graphs without an input node', () => {
    const g = graph([{ id: 'ask', profile: 'clarifier' }, { id: 'out', type: 'output' }], [
      { source: 'ask', target: 'out' },
    ])
    expect(isClarifyFirstWorkflow(g, AGENTS)).toBe(false)
  })

  it('uses the unguarded success edge when a when-guarded sibling exists', () => {
    const g = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'clarifier' },
        { id: 'w', profile: 'worker' },
      ],
      [
        { source: 'in', target: 'w', when: 'skip' },
        { source: 'in', target: 'ask' },
      ],
    )
    expect(isClarifyFirstWorkflow(g, AGENTS)).toBe(true)
  })

  it('accepts a single success edge even when it has when', () => {
    const g = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'clarifier' },
      ],
      [{ source: 'in', target: 'ask', when: 'ok' }],
    )
    expect(isClarifyFirstWorkflow(g, AGENTS)).toBe(true)
  })

  it('ignores failure edges leaving input', () => {
    const g = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'clarifier' },
      ],
      [{ source: 'in', target: 'ask', kind: 'failure' }],
    )
    expect(isClarifyFirstWorkflow(g, AGENTS)).toBe(false)
  })

  it('treats a published head as the snapshot and ignores a never-published draft', () => {
    const g = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'clarifier' },
      ],
      [{ source: 'in', target: 'ask' }],
    )
    expect(isPublishedClarifyFirst({ status: 'draft', version: 1, publishedVersion: 0, ...g }, AGENTS)).toBe(false)
    expect(isPublishedClarifyFirst({ status: 'published', version: 1, publishedVersion: 1, ...g }, AGENTS)).toBe(true)
    expect(clarifyFirstNodeId(g, AGENTS)).toBe('ask')
  })

  it('uses the published snapshot even when the draft head is no longer clarify-first', () => {
    const head = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'work', profile: 'worker' },
      ],
      [{ source: 'in', target: 'work' }],
    )
    const snap = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'clarifier' },
      ],
      [{ source: 'in', target: 'ask' }],
    )
    const wf = {
      status: 'draft' as const,
      version: 2,
      publishedVersion: 1,
      name: '草稿名',
      description: '草稿说明',
      ...head,
      publishedSnapshot: {
        version: 1,
        name: '已发布名',
        description: '已发布说明',
        nodes: snap.nodes!,
        edges: snap.edges!,
      },
    }
    expect(isPublishedClarifyFirst(wf, AGENTS)).toBe(true)
    const card = withPublishedSnapshot({ id: 'wf', updatedAt: '', needsRepo: false, ...wf })
    expect(card.name).toBe('已发布名')
    expect(card.description).toBe('已发布说明')
    expect(clarifyFirstNodeId(card, AGENTS)).toBe('ask')
  })

  it('rejects a clarify-first draft whose published snapshot is not clarify-first', () => {
    const head = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'ask', profile: 'clarifier' },
      ],
      [{ source: 'in', target: 'ask' }],
    )
    const snap = graph(
      [
        { id: 'in', type: 'input' },
        { id: 'work', profile: 'worker' },
      ],
      [{ source: 'in', target: 'work' }],
    )
    expect(
      isPublishedClarifyFirst(
        {
          status: 'draft',
          version: 2,
          publishedVersion: 1,
          ...head,
          publishedSnapshot: { version: 1, name: '旧版', description: '', nodes: snap.nodes!, edges: snap.edges! },
        },
        AGENTS,
      ),
    ).toBe(false)
  })
})

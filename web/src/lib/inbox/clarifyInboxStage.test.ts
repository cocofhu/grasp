import { describe, expect, it } from 'vitest'
import {
  defaultClarifyProductId,
  listClarifyProductNodes,
  resolveClarifyProductStage,
} from './clarifyInboxStage'
import type { Run, WFNode } from '../shared/types'
import { CLARIFY_CAPS, IMPLEMENT_CAPS } from '@/test/capsFixtures'

function node(id: string, type: WFNode['type'], caps?: WFNode['caps']): WFNode {
  return { id, type, label: id, position: { x: 0, y: 0 }, config: {}, caps }
}

function run(partial: Partial<Run>): Run {
  return {
    id: 'r1',
    workflowId: '',
    workflowName: '',
    status: 'waiting_human',
    trigger: '',
    startedAt: '',
    durationSec: 0,
    progress: 0,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

describe('listClarifyProductNodes / defaultClarifyProductId', () => {
  it('lists PRODUCT nodes with executions and defaults to current', () => {
    const products = listClarifyProductNodes(
      run({
        nodes: [node('research_1', 'agent', CLARIFY_CAPS), node('plan', 'agent', IMPLEMENT_CAPS), node('gate', 'human_gate')],
        nodeExecutions: {
          research_1: [{ nodeId: 'research_1', status: 'waiting_human', iteration: 1 }],
          plan: [{ nodeId: 'plan', status: 'completed', iteration: 1 }],
        },
      }),
    )
    expect(products.map((n) => n.id)).toEqual(['research_1', 'plan'])
    expect(defaultClarifyProductId('research_1', products)).toBe('research_1')
    expect(defaultClarifyProductId('other', products)).toBe('research_1')
  })
})

describe('resolveClarifyProductStage', () => {
  it('returns loadFailed when context lacks product fields', () => {
    expect(
      resolveClarifyProductStage({
        loadError: false,
        run: run({}),
        inboxNodeId: 'react',
        inboxIteration: 1,
        selectedNode: null,
        selectedNodeRun: null,
      }),
    ).toBe('loadFailed')
  })

  it('returns panel when a product node/run is selected', () => {
    const n = node('research_1', 'research')
    const nr = { nodeId: 'research_1', status: 'waiting_human' as const, iteration: 1 }
    expect(
      resolveClarifyProductStage({
        loadError: false,
        run: run({ nodes: [n], nodeExecutions: { research_1: [nr] } }),
        inboxNodeId: 'research_1',
        inboxIteration: 1,
        selectedNode: n,
        selectedNodeRun: nr,
      }),
    ).toBe('panel')
  })

  it('distinguishes pending vs executed-empty', () => {
    const base = {
      loadError: false,
      selectedNode: null,
      selectedNodeRun: null,
      inboxNodeId: 'agent_1',
      inboxIteration: 1,
    }
    expect(
      resolveClarifyProductStage({
        ...base,
        run: run({
          nodes: [node('agent_1', 'agent')],
          nodeExecutions: { agent_1: [{ nodeId: 'agent_1', status: 'pending', iteration: 1 }] },
        }),
      }),
    ).toBe('pending')
    expect(
      resolveClarifyProductStage({
        ...base,
        run: run({
          nodes: [node('agent_1', 'agent')],
          nodeExecutions: {
            agent_1: [{ nodeId: 'agent_1', status: 'waiting_human', iteration: 1 }],
          },
        }),
      }),
    ).toBe('executedEmpty')
  })
})

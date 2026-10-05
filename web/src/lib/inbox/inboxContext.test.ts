import { describe, expect, it } from 'vitest'
import { adaptInboxContextToRun } from './inboxContext'
import { CLARIFY_CAPS, IMPLEMENT_CAPS } from '@/test/capsFixtures'

describe('adaptInboxContextToRun', () => {
  it('maps gate context to Run subset', () => {
    const run = adaptInboxContextToRun(
      {
        type: 'gate',
        nodes: [{ id: 'gate', type: 'human_gate', label: 'G', position: { x: 0, y: 0 }, config: {} }],
        artifacts: [{ id: 'a1', name: 'plan.json', kind: 'json', nodeId: 'plan', runId: 'r1', workflowName: 'wf', sizeBytes: 1, createdAt: '' }],
        nodeExecutions: { visual: [{ nodeId: 'visual', status: 'completed', iteration: 1, outputs: { page: '<p/>' } }] },
      },
      'r1',
    )
    expect(run.id).toBe('r1')
    expect(run.nodes).toHaveLength(1)
    expect(run.artifacts[0].name).toBe('plan.json')
    expect(run.nodeExecutions?.visual[0].outputs?.page).toBe('<p/>')
  })

  it('maps clarify context to clarifyByNode + status', () => {
    const run = adaptInboxContextToRun(
      {
        type: 'clarify',
        status: 'waiting_human',
        clarify: {
          nodeId: 'react',
          iteration: 2,
          turns: [{ role: 'agent', text: 'hi', at: '2026-01-01T00:00:00Z' }],
          done: false,
          label: '澄清',
          previewArtifact: 'page.html',
        },
      },
      'r2',
    )
    expect(run.status).toBe('waiting_human')
    expect(run.clarifyByNode?.react.turns).toHaveLength(1)
    expect(run.clarifyByNode?.react.previewArtifact).toBe('page.html')
    expect(run.artifacts).toEqual([])
  })

  it('maps clarify product fields so StructuredProductPanel can mount', () => {
    const run = adaptInboxContextToRun(
      {
        type: 'clarify',
        status: 'waiting_human',
        nodes: [
          { id: 'research_1', type: 'agent', caps: IMPLEMENT_CAPS, label: '调研', position: { x: 0, y: 0 }, config: {} },
          { id: 'react', type: 'agent', caps: CLARIFY_CAPS, label: '澄清', position: { x: 1, y: 0 }, config: {} },
        ],
        artifacts: [
          {
            id: 'a1',
            name: 'research.json',
            kind: 'json',
            nodeId: 'research_1',
            runId: 'r3',
            workflowName: 'wf',
            sizeBytes: 12,
            createdAt: '',
          },
        ],
        nodeExecutions: {
          research_1: [
            {
              nodeId: 'research_1',
              status: 'waiting_human',
              iteration: 1,
              outputs: { research: '{"summary":"ok"}' },
            },
          ],
        },
        clarify: {
          nodeId: 'research_1',
          iteration: 1,
          turns: [{ role: 'agent', text: '请复审', at: '2026-01-01T00:00:00Z' }],
          done: false,
          label: '调研结论',
        },
      },
      'r3',
    )
    expect(run.nodes?.find((n) => n.id === 'research_1')?.type).toBe('agent')
    expect(run.artifacts[0].name).toBe('research.json')
    expect(run.nodeExecutions?.research_1[0].outputs?.research).toContain('ok')
    expect(run.clarifyByNode?.research_1.done).toBe(false)
  })

  it('maps clarify reactSessions for refresh-resume', () => {
    const run = adaptInboxContextToRun(
      {
        type: 'clarify',
        status: 'waiting_human',
        reactSessions: {
          react: {
            kind: 'clarify',
            waiting: 1,
            busy: true,
            items: [{ id: 'q2', text: '乙' }],
            activeItem: { id: 'q1', text: '甲' },
          },
        },
        clarify: {
          nodeId: 'react',
          iteration: 1,
          turns: [],
          done: false,
          label: '澄清',
        },
      },
      'r-sess',
    )
    expect(run.reactSessions?.react?.busy).toBe(true)
    expect(run.reactSessions?.react?.items?.[0]?.id).toBe('q2')
    expect(run.reactSessions?.react?.activeItem?.text).toBe('甲')
  })

  it('normalizes null clarify turns to an empty array', () => {
    const run = adaptInboxContextToRun(
      {
        type: 'clarify',
        status: 'waiting_human',
        nodes: [{ id: 'predev', type: 'agent', caps: CLARIFY_CAPS, label: 'Approve', position: { x: 0, y: 0 }, config: {} }],
        artifacts: [],
        nodeExecutions: {},
        clarify: {
          nodeId: 'predev',
          iteration: 1,
          turns: null as unknown as [],
          done: false,
          label: 'Approve',
        },
      },
      'r-empty',
    )
    expect(run.clarifyByNode?.predev.turns).toEqual([])
  })
})

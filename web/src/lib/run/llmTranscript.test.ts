import { describe, expect, it } from 'vitest'
import type { AcpEvent, Run } from '@/lib/shared/types'
import type { LlmTranscriptResponse } from '@/lib/api/apiTypes'
import { buildLlmTranscript, carryInflight, splitTurns, summarizeLlmTranscript } from './llmTranscript'

const nodeInfo = (id: string) => ({ label: `L-${id}`, type: 'agent' })

function baseRun(over: Partial<Run> = {}): Run {
  return {
    id: 'r1',
    workflowId: 'wf',
    workflowName: 'WF',
    status: 'completed',
    trigger: 'manual',
    startedAt: '2026-10-01T00:00:00Z',
    durationSec: 60,
    progress: 100,
    nodeRuns: {},
    artifacts: [],
    ...over,
  }
}

const ev = (e: Partial<AcpEvent> & { kind: AcpEvent['kind'] }): AcpEvent => ({ t: 0, ...e })

describe('carryInflight', () => {
  const at = '2026-10-01T00:00:05Z'
  const prev: LlmTranscriptResponse = {
    executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'running' }],
    inflight: { a: { prompt: 'Q', at } },
  }

  it('keeps a finished turn visible until its prompt is fetched', () => {
    const next: LlmTranscriptResponse = { executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'running' }] }
    expect(carryInflight(prev, next).inflight?.a?.prompt).toBe('Q')
  })

  it('drops the carried prompt once persisted, superseded or the execution ended', () => {
    const persisted: LlmTranscriptResponse = {
      executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'running', events: [{ kind: 'prompt', text: 'Q', at }] }],
    }
    expect(carryInflight(prev, persisted).inflight).toBeUndefined()
    const ended: LlmTranscriptResponse = { executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'completed' }] }
    expect(carryInflight(prev, ended).inflight).toBeUndefined()
    const newer: LlmTranscriptResponse = { ...prev, inflight: { a: { prompt: 'Q2', at: '2026-10-01T00:00:09Z' } } }
    expect(carryInflight(prev, newer).inflight?.a?.prompt).toBe('Q2')
  })
})

describe('splitTurns', () => {
  it('pairs each prompt with its reply and turn_end usage', () => {
    const turns = splitTurns([
      ev({ kind: 'prompt', text: 'Q1', at: '2026-10-01T00:00:01Z', title: '2 images' }),
      ev({ kind: 'thought', text: 'thinking' }),
      ev({ kind: 'tool_call', title: 'write_artifact prd.md', status: 'completed', artifact: { name: 'prd.md', kind: 'markdown' } }),
      ev({ kind: 'message', text: 'A1' }),
      ev({ kind: 'turn_end', at: '2026-10-01T00:00:09Z', usage: { inputTokens: 5, outputTokens: 1, cacheReadTokens: 0, cacheWriteTokens: 0 } }),
      ev({ kind: 'prompt', text: 'Q2' }),
      ev({ kind: 'message', text: 'part 1' }),
      ev({ kind: 'segment' }),
      ev({ kind: 'message', text: 'part 2' }),
      ev({ kind: 'turn_end', status: 'failed', text: 'quota' }),
    ])
    expect(turns).toHaveLength(2)
    expect(turns[0]!.prompt).toMatchObject({ text: 'Q1', imageCount: 2 })
    expect(turns[0]!.answers).toHaveLength(1)
    expect(turns[0]!.answers[0]).toMatchObject({ thought: 'thinking', text: 'A1' })
    expect(turns[0]!.answers[0]!.tools[0]!.artifact?.name).toBe('prd.md')
    expect(turns[0]!.usage?.inputTokens).toBe(5)
    expect(turns[1]!.answers.map((a) => a.text)).toEqual(['part 1', 'part 2'])
    expect(turns[1]!).toMatchObject({ failed: true, error: 'quota' })
  })

  it('attaches the ordered timeline to the reply it belongs to', () => {
    const parts = [{ kind: 'thought' as const, text: 't' }, { kind: 'tool' as const, title: 'Read' }, { kind: 'message' as const, text: 'm' }]
    const turns = splitTurns([
      ev({ kind: 'prompt', text: 'Q' }),
      ev({ kind: 'message', text: 'first' }),
      ev({ kind: 'segment' }),
      ev({ kind: 'thought', text: 't' }),
      ev({ kind: 'message', text: 'm' }),
      ev({ kind: 'timeline', parts: [] }),
      ev({ kind: 'timeline', parts }),
      ev({ kind: 'turn_end' }),
    ])
    expect(turns[0]!.answers).toHaveLength(2)
    expect(turns[0]!.answers[0]!.parts).toBeUndefined()
    expect(turns[0]!.answers[1]!.parts).toEqual(parts)
  })

  it('keeps legacy events (no prompt) as a single reply-only turn', () => {
    const turns = splitTurns([ev({ kind: 'thought', text: 't' }), ev({ kind: 'message', text: 'm' })])
    expect(turns).toHaveLength(1)
    expect(turns[0]!.prompt).toBeUndefined()
    expect(turns[0]!.answers[0]).toMatchObject({ thought: 't', text: 'm' })
  })
})

describe('buildLlmTranscript', () => {
  it('orders executions, turns and transitions chronologically and labels prompt sources', () => {
    const transcript: LlmTranscriptResponse = {
      executions: [
        {
          id: 1,
          nodeId: 'clarify',
          iteration: 1,
          status: 'completed',
          startedAt: '2026-10-01T00:00:00Z',
          durationSec: 30,
          usage: { inputTokens: 10, outputTokens: 2, cacheReadTokens: 0, cacheWriteTokens: 0 },
          usageByModel: { 'claude-x': { inputTokens: 10, outputTokens: 2, cacheReadTokens: 0, cacheWriteTokens: 0 } },
          events: [
            ev({ kind: 'prompt', text: 'You are a PM', at: '2026-10-01T00:00:01Z' }),
            ev({ kind: 'message', text: 'What platform?' }),
            ev({ kind: 'turn_end', at: '2026-10-01T00:00:05Z' }),
            ev({ kind: 'prompt', text: 'Web only', at: '2026-10-01T00:00:20Z' }),
            ev({ kind: 'message', text: 'Got it' }),
            ev({ kind: 'turn_end', at: '2026-10-01T00:00:25Z' }),
          ],
          mcpCalls: [
            { at: '2026-10-01T00:00:03Z', tool: 'ask_question' },
            { at: '2026-10-01T00:00:22Z', tool: 'write_artifact' },
          ],
        },
        { id: 2, nodeId: 'gate', iteration: 1, status: 'completed', startedAt: '2026-10-01T00:00:40Z' },
      ],
    }
    const run = baseRun({
      clarifyByNode: {
        clarify: { nodeId: 'clarify', done: true, turns: [{ role: 'human', text: 'Web only', at: '' }] },
      },
      trace: [
        { at: '2026-10-01T00:00:00Z', nodeId: 'clarify', event: 'enter' },
        { at: '2026-10-01T00:00:10Z', nodeId: 'clarify', event: 'pause', detail: 'waiting' },
        { at: '2026-10-01T00:00:19Z', nodeId: 'clarify', event: 'resume', detail: 'react 完成' },
        { at: '2026-10-01T00:00:31Z', nodeId: 'clarify', event: 'exit' },
        { at: '2026-10-01T00:00:31Z', nodeId: 'clarify', event: 'transition', to: 'gate', kind: 'success' },
      ],
    })
    const items = buildLlmTranscript({ run, transcript, nodeInfo })
    expect(items.map((i) => (i.type === 'trace' ? `trace:${i.event}` : i.type))).toEqual([
      'node',
      'turn',
      'trace:pause',
      'trace:resume',
      'turn',
      'trace:transition',
      'node',
    ])
    const turns = items.filter((i) => i.type === 'turn')
    expect(turns.map((t) => t.type === 'turn' && t.turn.prompt?.source)).toEqual(['instruction', 'human'])
    expect(turns.map((t) => t.type === 'turn' && t.turn.mcpCalls.map((c) => c.tool))).toEqual([
      ['ask_question'],
      ['write_artifact'],
    ])
    const transition = items.find((i) => i.type === 'trace' && i.event === 'transition')
    expect(transition).toMatchObject({ toLabel: 'L-gate' })
    const node = items[0]
    expect(node).toMatchObject({ type: 'node', label: 'L-clarify', models: ['claude-x'], turnCount: 2 })
    expect(summarizeLlmTranscript(items)).toEqual({ turns: 2, nodes: 2, totalTokens: 12 })
  })

  it('adds a legacy notice for executions recorded before prompts were persisted', () => {
    const run = baseRun({
      nodeExecutions: {
        a: [{ nodeId: 'a', status: 'completed', startedAt: '2026-10-01T00:00:00Z', events: [ev({ kind: 'message', text: 'old' })] }],
      },
    })
    const items = buildLlmTranscript({ run, transcript: null, nodeInfo })
    expect(items.map((i) => i.type)).toEqual(['node', 'legacy', 'turn'])
  })

  it('appends a live turn from inflight prompt and WS events for the running execution', () => {
    const run = baseRun({ status: 'running' })
    const transcript: LlmTranscriptResponse = {
      executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'running', startedAt: '2026-10-01T00:00:00Z', events: [] }],
      inflight: { a: { prompt: 'build it', at: '2026-10-01T00:00:02Z' } },
    }
    const items = buildLlmTranscript({
      run,
      transcript,
      nodeInfo,
      liveEvents: { a: [ev({ kind: 'message', text: 'working…' })] },
    })
    const turn = items.find((i) => i.type === 'turn')
    expect(turn?.type === 'turn' && turn.turn).toMatchObject({
      live: true,
      prompt: { text: 'build it', source: 'instruction' },
      answers: [{ text: 'working…' }],
    })
  })

  it('ignores inflight prompts once the execution is no longer active', () => {
    const transcript: LlmTranscriptResponse = {
      executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'completed', startedAt: '2026-10-01T00:00:00Z' }],
      inflight: { a: { prompt: 'stale', at: '2026-10-01T00:00:02Z' } },
    }
    const items = buildLlmTranscript({ run: baseRun(), transcript, nodeInfo })
    expect(items.some((i) => i.type === 'turn')).toBe(false)
  })

  it('puts a transition before the divider of the node it starts in the same second', () => {
    const transcript: LlmTranscriptResponse = {
      executions: [
        { id: 1, nodeId: 'in', iteration: 1, status: 'completed', startedAt: '2026-10-01T00:00:00Z' },
        { id: 2, nodeId: 'b', iteration: 1, status: 'waiting_human', startedAt: '2026-10-01T00:00:01Z' },
      ],
    }
    const run = baseRun({
      trace: [
        { at: '2026-10-01T00:00:01Z', nodeId: 'in', event: 'transition', to: 'b' },
        { at: '2026-10-01T00:00:01Z', nodeId: 'b', event: 'pause' },
      ],
    })
    const items = buildLlmTranscript({ run, transcript, nodeInfo })
    expect(items.map((i) => (i.type === 'trace' ? i.event : `${i.type}:${i.nodeId}`))).toEqual([
      'node:in',
      'transition',
      'node:b',
      'pause',
    ])
  })

  it('surfaces execution errors after the turns', () => {
    const transcript: LlmTranscriptResponse = {
      executions: [
        {
          id: 1,
          nodeId: 'a',
          iteration: 1,
          status: 'failed',
          startedAt: '2026-10-01T00:00:00Z',
          durationSec: 5,
          error: 'sandbox setup failed',
          events: [ev({ kind: 'prompt', text: 'q', at: '2026-10-01T00:00:01Z' })],
        },
      ],
    }
    const items = buildLlmTranscript({ run: baseRun(), transcript, nodeInfo })
    expect(items.map((i) => i.type)).toEqual(['node', 'turn', 'error'])
  })
})

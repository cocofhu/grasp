import { describe, expect, it } from 'vitest'
import type { AcpEvent, Run } from '@/lib/shared/types'
import type { LlmTranscriptResponse } from '@/lib/api/apiTypes'
import { buildLlmTranscript } from './llmTranscript'
import { clarifyTurnsFromLlm, llmTranscriptThread, notesAfterVisit, notesBesideTurn } from './llmTranscriptThread'

const nodeInfo = (id: string) => ({ label: `L-${id}`, type: id === 'gate' ? 'human_gate' : 'agent' })

function baseRun(over: Partial<Run> = {}): Run {
  return {
    id: 'r1',
    workflowId: 'wf',
    workflowName: 'WF',
    status: 'waiting_human',
    trigger: 'manual',
    startedAt: '2026-10-01T00:00:00Z',
    durationSec: 60,
    progress: 50,
    nodeRuns: {},
    artifacts: [],
    ...over,
  }
}

const ev = (e: Partial<AcpEvent> & { kind: AcpEvent['kind'] }): AcpEvent => ({ t: 0, ...e })

describe('llmTranscriptThread', () => {
  it('carries a prompt’s reported usage once onto its final sealed reply without guessing execution metadata', () => {
    const usage = { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 0, cacheWriteTokens: 0 }
    const rows = clarifyTurnsFromLlm([{
      prompt: { text: 'Continue', at: '2026-10-28T08:00:00Z', truncated: false, imageCount: 0, source: 'human' },
      answers: [{ text: 'Reading first', tools: [] }, { text: 'Complete', tools: [] }],
      endedAt: '2026-10-28T08:00:25Z', usage, mcpCalls: [], failed: false,
    }]).filter(row => row.role === 'agent')
    expect(rows[0].usage).toBeUndefined()
    expect(rows[1].usage).toEqual(usage)
    expect(rows[1].execution).toBeUndefined()
    expect(rows[1].startedAt).toBe('2026-10-28T08:00:00Z')
  })
  it('slots pause and resume between turns and keeps a bare exit after the visit', () => {
    const transcript: LlmTranscriptResponse = {
      executions: [
        {
          id: 1,
          nodeId: 'clarify',
          iteration: 1,
          nodeType: 'agent',
          status: 'completed',
          startedAt: '2026-10-01T00:00:00Z',
          durationSec: 30,
          events: [
            ev({ kind: 'prompt', text: 'You are a PM', at: '2026-10-01T00:00:01Z' }),
            ev({ kind: 'message', text: 'What platform?' }),
            ev({ kind: 'turn_end', at: '2026-10-01T00:00:05Z' }),
            ev({ kind: 'prompt', text: 'Web only', at: '2026-10-01T00:00:20Z' }),
            ev({ kind: 'message', text: 'Got it' }),
            ev({ kind: 'turn_end', at: '2026-10-01T00:00:25Z' }),
          ],
        },
        {
          id: 2,
          nodeId: 'gate',
          iteration: 1,
          nodeType: 'human_gate',
          status: 'waiting_human',
          startedAt: '2026-10-01T00:00:40Z',
        },
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
        { at: '2026-10-01T00:00:40Z', nodeId: 'gate', event: 'pause', detail: 'waiting' },
      ],
    })
    const items = buildLlmTranscript({ run, transcript, nodeInfo })
    expect(items.map((item) => (item.type === 'trace' ? `trace:${item.event}` : item.type))).toEqual([
      'node',
      'turn',
      'trace:pause',
      'trace:resume',
      'turn',
      'trace:exit',
      'trace:transition',
      'node',
      'trace:pause',
    ])

    const thread = llmTranscriptThread(run, items)
    const prior = thread.priors[0]!
    expect(prior.notes?.map((note) => `${note.event}:${note.afterTurn}`)).toEqual([
      'pause:1',
      'resume:1',
      'exit:3',
      'transition:3',
    ])
    expect(notesBesideTurn(prior.notes, 1, prior.turns.length).map((note) => note.event)).toEqual(['pause', 'resume'])
    expect(notesAfterVisit(prior.notes, prior.turns.length).map((note) => note.event)).toEqual(['exit', 'transition'])
    expect(notesBesideTurn(prior.notes, -1, prior.turns.length)).toEqual([])
    expect(thread.currentNotes.map((note) => `${note.event}:${note.afterTurn}`)).toEqual(['pause:-1'])
    expect(thread.current?.nodeId).toBe('gate')
  })
})

import { describe, expect, it } from 'vitest'
import { applyAcp, partsOf, type AcpReply } from './acpReply'

const label = (key: string) => (key === 'read_file' ? '读取文件' : '')
const chunk = (kind: string, text: string) => ({ op: 'event', data: { type: 'session_update', update: { sessionUpdate: kind, content: { type: 'text', text } } } })
const update = (u: Record<string, unknown>) => ({ op: 'event', data: { type: 'session_update', update: u } })
const reply = (): AcpReply => ({ text: '', tools: [], plan: [] })
const fold = (turn: AcpReply, ...frames: unknown[]) => frames.forEach((f) => applyAcp(f, turn, label, 'Tool'))

describe('acpReply', () => {
  it('folds message, thought and tool frames in arrival order', () => {
    const turn = reply()
    fold(
      turn,
      chunk('agent_thought_chunk', 'think '),
      chunk('agent_thought_chunk', 'more'),
      { data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 't1', title: 'run_shell', status: 'in_progress', rawInput: { command: 'ls -la', token: 'x' } } } },
      { data: { type: 'session_update', update: { sessionUpdate: 'tool_call_update', toolCallId: 't1', status: 'completed', rawOutput: { stdout: 'ok' } } } },
      chunk('agent_message_chunk', 'Hello'),
      chunk('agent_message_chunk', ' world'),
    )
    expect(turn.text).toBe('Hello world')
    expect(turn.thought).toBe('think more')
    expect(turn.tools).toHaveLength(1)
    expect(turn.tools[0]).toMatchObject({ id: 't1', title: 'Run Shell', status: 'completed', summary: 'ls -la', output: 'ok' })
    expect(turn.tools[0]!.input).toContain('****')
    expect(partsOf(turn).map((p) => p.kind)).toEqual(['thought', 'tool', 'message'])
    expect(partsOf(turn)[0]!.text).toBe('think more')
  })

  it('reads tool output from ACP tool call content and skips non-text blocks', () => {
    const turn = reply()
    fold(
      turn,
      update({ sessionUpdate: 'tool_call', toolCallId: 't', title: 'read_file', kind: 'read' }),
      update({
        sessionUpdate: 'tool_call_update',
        toolCallId: 't',
        status: 'completed',
        content: [{ type: 'content', content: { type: 'text', text: '42 passed' } }, { type: 'content', content: { type: 'image', data: 'binary' } }],
      }),
    )
    expect(turn.tools[0]).toMatchObject({ title: '读取文件', status: 'completed', output: '42 passed' })
  })

  it('starts a new paragraph when reply text resumes after a tool, thought or steer', () => {
    const turn = reply()
    fold(
      turn,
      chunk('agent_message_chunk', 'Plan:\n'),
      update({ sessionUpdate: 'tool_call', toolCallId: 't', title: 'x' }),
      chunk('agent_message_chunk', 'a'),
      update({ sessionUpdate: 'usage_update', used: 1 }),
      chunk('agent_message_chunk', 'b'),
      { op: 'event', data: { type: 'user_steer', text: 'one' } },
      { data: { type: 'user_steer', text: 'two' } },
      { data: { type: 'user_steer', text: '  ' } },
      chunk('agent_message_chunk', 'done'),
    )
    expect(turn.text).toBe('Plan:\n\nab\n\ndone')
    expect(partsOf(turn)).toEqual([
      { kind: 'message', text: 'Plan:\n' },
      { kind: 'tool', title: 'X', status: 'pending', summary: undefined, input: undefined, output: undefined },
      { kind: 'message', text: 'ab' },
      { kind: 'steer', text: 'one' },
      { kind: 'steer', text: 'two' },
      { kind: 'message', text: 'done' },
    ])
  })

  it('keeps provider diagnostics and partial output when the turn fails', () => {
    const turn = reply()
    fold(turn, chunk('agent_message_chunk', 'Partial reply'))
    for (const text of ['Quota exhausted', 'Check the selected model']) fold(turn, { op: 'event', data: { type: 'error_text', text } })
    fold(turn, { data: { type: 'prompt_done', stopReason: 'failed' } })
    expect(turn.status).toBe('failed')
    expect(turn.error).toBe('Quota exhausted\nCheck the selected model')
    expect(partsOf(turn)).toEqual([{ kind: 'message', text: 'Partial reply' }])
  })

  it('records context commands, plans and humanized tool titles', () => {
    const turn = reply()
    fold(
      turn,
      { op: 'event', data: { type: 'context_command', command: '/compact' } },
      update({ sessionUpdate: 'plan', entries: [{ content: 'step', status: 'in_progress' }, { title: '' }] }),
      update({ sessionUpdate: 'tool_call', toolCallId: 'a', title: '执行命令' }),
      update({ sessionUpdate: 'tool_call', toolCallId: 'b', title: 'tool_12345678' }),
      {},
      { type: 'other' },
    )
    expect(turn.contextCommand).toBe('compact')
    expect(turn.plan).toEqual([{ content: 'step', status: 'in_progress' }])
    expect(turn.tools.map((t) => t.title)).toEqual(['执行命令', 'Tool'])
    fold(turn, update({ sessionUpdate: 'plan', steps: 'bad' }))
    expect(turn.plan).toEqual([])
  })
})

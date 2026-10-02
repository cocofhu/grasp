import { describe, expect, it } from 'vitest'
import {
  contentText,
  extractAgentMessageDelta,
  flattenUpdate,
  normalizeKind,
  unwrapFrame,
} from './acpUnpack'

describe('acpUnpack', () => {
  it('unwrapFrame peels op:event', () => {
    expect(unwrapFrame({ op: 'event', data: { type: 'session_update' } })).toEqual({
      type: 'session_update',
    })
    expect(unwrapFrame({ type: 'session_update' })).toEqual({ type: 'session_update' })
  })

  it('normalizeKind handles camelCase and kebab', () => {
    expect(normalizeKind('agentMessageChunk')).toBe('agent_message_chunk')
    expect(normalizeKind('agent-message-chunk')).toBe('agent_message_chunk')
  })

  it('contentText covers string / array / parts', () => {
    expect(contentText('hello')).toBe('hello')
    expect(contentText([{ text: 'a' }, { text: 'b' }])).toBe('ab')
    expect(contentText({ text: 'x' })).toBe('x')
    expect(contentText({ parts: [{ text: 'p' }, 'q'] })).toBe('pq')
    expect(contentText(null)).toBe('')
    expect(contentText({ parts: ['a', { text: 'b' }] })).toBe('ab')
    expect(contentText({ nope: true })).toBe('')
    expect(normalizeKind('toolCall-Update')).toBe('tool_call_update')
    expect(unwrapFrame(null)).toBeNull()
  })

  it('flattenUpdate lifts nested sessionUpdate fields without overriding', () => {
    expect(flattenUpdate({ kind: 'k', sessionUpdate: { kind: 'x', content: 'c' } })).toEqual({ kind: 'k', content: 'c' })
    expect(flattenUpdate({ session_update: 'plan', entries: [] })).toEqual({ session_update: 'plan', entries: [] })
    expect(flattenUpdate('raw')).toBe('raw')
  })

  it('extractAgentMessageDelta accumulates nested op:event agent_message_chunk', () => {
    const nested = {
      op: 'event',
      data: {
        type: 'session_update',
        update: {
          sessionUpdate: 'agent_message_chunk',
          content: { type: 'text', text: 'Hello' },
        },
      },
    }
    expect(extractAgentMessageDelta(nested)).toEqual({
      kind: 'agent_message_chunk',
      text: 'Hello',
    })
  })

  it('extractAgentMessageDelta ignores non-session_update frames', () => {
    expect(extractAgentMessageDelta({ op: 'event', data: { type: 'prompt_begin' } })).toBeNull()
    expect(
      extractAgentMessageDelta({
        type: 'session_update',
        update: { sessionUpdate: 'agent_thought_chunk', content: { text: 'think' } },
      }),
    ).toBeNull()
  })

  it('extractAgentMessageDelta reads array content parts', () => {
    const frame = {
      type: 'session_update',
      update: {
        sessionUpdate: 'agent_message_chunk',
        content: [{ text: 'A' }, { parts: [{ text: 'B' }] }],
      },
    }
    expect(extractAgentMessageDelta(frame)?.text).toBe('AB')
  })
})

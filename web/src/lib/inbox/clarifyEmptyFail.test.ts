import { describe, expect, it } from 'vitest'
import {
  emptyFailDisplayText,
  emptyFailHeadline,
  failureDetailText,
  turnProcessParts,
  isRetryableFailedAgent,
} from './clarifyEmptyFail'
import type { ClarifyTurn } from '@/lib/shared/types'

describe('clarifyEmptyFail', () => {
  it('retries only a reply the server ended empty or failed (plan g1.2)', () => {
    expect(isRetryableFailedAgent({ role: 'agent', text: '', at: 't', outcome: 'empty' })).toBe(true)
    expect(isRetryableFailedAgent({ role: 'agent', text: '(澄清回复失败:x)', at: 't', outcome: 'failed' })).toBe(true)
    // The text alone never makes a failure.
    expect(isRetryableFailedAgent({ role: 'agent', text: '(澄清回复失败:x)', at: 't', outcome: 'ok' })).toBe(false)
    expect(isRetryableFailedAgent({ role: 'agent', text: '', at: 't' })).toBe(false)
    expect(isRetryableFailedAgent({ role: 'agent', text: '(已中断)', at: 't', interrupted: true, outcome: 'interrupted' })).toBe(false)
    expect(isRetryableFailedAgent({ role: 'agent', text: '', at: 't', outcome: 'empty', streaming: true })).toBe(false)
    expect(isRetryableFailedAgent({ role: 'human', text: '', at: 't', outcome: 'empty' })).toBe(false)
  })

  it('prefers cleaned error text for failure card copy', () => {
    const empty: ClarifyTurn = { role: 'agent', text: '', at: 't', outcome: 'empty' }
    expect(emptyFailDisplayText(empty, '本轮没有输出')).toBe('本轮没有输出')
    expect(emptyFailHeadline(empty, '本轮没有输出', '这一轮没有完成')).toBe('本轮没有输出')
    const failed: ClarifyTurn = { role: 'agent', text: '(澄清回复失败:boom)', at: 't', outcome: 'failed' }
    expect(emptyFailDisplayText(failed, '本轮没有输出')).toBe('boom')
    expect(emptyFailHeadline(failed, '本轮没有输出', '这一轮没有完成')).toBe('这一轮没有完成')
    expect(
      failureDetailText(
        "(澄清回复失败:exit status 1; stderr: NonRetriableError: Provider Error We're having trouble.)",
      ),
    ).toBe("We're having trouble.")
  })

  it('draws a failed turn from its own steps, not the capped tool list', () => {
    const failed: ClarifyTurn = {
      role: 'agent',
      text: '(澄清回复失败:boom)',
      at: 't',
      outcome: 'failed',
      tools: Array.from({ length: 50 }, () => ({ title: 'Read', status: 'completed' })),
      parts: [
        { kind: 'tool', title: 'Glob', status: 'completed' },
        { kind: 'message', text: '(澄清回复失败:boom)' },
      ],
    }
    const parts = turnProcessParts(failed)
    expect(parts.map((p) => p.kind)).toEqual(['tool'])
    expect(parts[0]?.title).toBe('Glob')
  })
})

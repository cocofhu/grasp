import { describe, expect, it } from 'vitest'
import { replyTail } from './replyTail'

describe('replyTail', () => {
  it('appends only the arguments that are set', () => {
    expect(replyTail()).toEqual([])
    expect(replyTail('cm-1')).toEqual(['cm-1'])
    expect(replyTail('cm-1', [])).toEqual(['cm-1'])
    expect(replyTail(undefined, [{ kind: 'solo', target: 's1' }])).toEqual([undefined, [{ kind: 'solo', target: 's1' }]])
  })
})

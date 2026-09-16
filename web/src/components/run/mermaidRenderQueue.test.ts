// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enqueueMermaidTask, flushMermaidQueue } from './mermaidRenderQueue'

describe('mermaidRenderQueue (g1.1)', () => {
  beforeEach(async () => {
    await flushMermaidQueue()
  })

  afterEach(async () => {
    await flushMermaidQueue()
  })

  it('runs tasks strictly one after another', async () => {
    const order: string[] = []
    const slow = enqueueMermaidTask(async () => {
      order.push('a-start')
      await new Promise((r) => setTimeout(r, 20))
      order.push('a-end')
      return 'a'
    })
    const fast = enqueueMermaidTask(async () => {
      order.push('b-start')
      order.push('b-end')
      return 'b'
    })
    const [ra, rb] = await Promise.all([slow, fast])
    expect(ra).toBe('a')
    expect(rb).toBe('b')
    expect(order).toEqual(['a-start', 'a-end', 'b-start', 'b-end'])
  })

  it('continues the queue after a rejected task', async () => {
    const failing = enqueueMermaidTask(async () => {
      throw new Error('boom')
    })
    const ok = enqueueMermaidTask(async () => 'ok')
    await expect(failing).rejects.toThrow('boom')
    await expect(ok).resolves.toBe('ok')
  })
})

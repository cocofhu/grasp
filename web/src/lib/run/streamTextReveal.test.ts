import { describe, expect, it, vi } from 'vitest'
import { charsPerTickForLag, createStreamTextReveal, STREAM_REVEAL_MAX_LAG } from './streamTextReveal'

describe('charsPerTickForLag', () => {
  it('matches Demo lag tiers', () => {
    expect(charsPerTickForLag(81, false)).toBe(8)
    expect(charsPerTickForLag(41, false)).toBe(5)
    expect(charsPerTickForLag(17, false)).toBe(3)
    expect(charsPerTickForLag(1, false)).toBe(2)
  })

  it('uses fast stride under reduced motion', () => {
    expect(charsPerTickForLag(1, true)).toBe(40)
    expect(charsPerTickForLag(200, true)).toBe(40)
  })
})

describe('createStreamTextReveal', () => {
  function makeHarness(opts: Parameters<typeof createStreamTextReveal>[0] = {}) {
    const frames: Array<() => void> = []
    const timeouts: Array<{ cb: () => void; ms: number }> = []
    const revealed: string[] = []
    const reveal = createStreamTextReveal({
      onReveal: (t) => revealed.push(t),
      schedule: (cb) => {
        frames.push(cb)
        return frames.length
      },
      cancel: () => {
        frames.length = 0
      },
      delay: (cb, ms) => {
        timeouts.push({ cb, ms })
        return timeouts.length
      },
      clearDelay: () => {
        timeouts.length = 0
      },
      prefersReducedMotion: () => false,
      delayMs: 22,
      ...opts,
    })
    return {
      reveal,
      revealed,
      stepFrame() {
        const cb = frames.shift()
        cb?.()
      },
      stepDelay() {
        const item = timeouts.shift()
        item?.cb()
      },
      pendingFrames: () => frames.length,
      pendingDelays: () => timeouts.length,
    }
  }

  it('reveals monotonically and accelerates on large lag', () => {
    const h = makeHarness()
    h.reveal.setTarget('abcdefghij') // 10 chars
    expect(h.revealed[0] ?? h.reveal.getRevealed()).toBe('')
    // first rAF tick
    expect(h.pendingFrames()).toBe(1)
    h.stepFrame()
    // lag=10 → 2 chars/tick
    expect(h.reveal.getRevealed()).toBe('ab')
    expect(h.reveal.getRevealed().length).toBe(2)
    // schedule delay then next frame
    expect(h.pendingDelays()).toBe(1)
    h.stepDelay()
    h.stepFrame()
    expect(h.reveal.getRevealed()).toBe('abcd')

    // sudden large absolute snapshot
    h.reveal.setTarget('x'.repeat(100))
    // divergent → restart from empty then catch up
    expect(h.reveal.getRevealed()).toBe('')
    h.stepFrame()
    // lag=100 → 8 chars
    expect(h.reveal.getRevealed().length).toBe(8)
  })

  it('flush aligns revealed to target and stops timers', () => {
    const h = makeHarness()
    h.reveal.setTarget('hello world')
    h.stepFrame()
    expect(h.reveal.getRevealed().length).toBeLessThan(11)
    h.reveal.flush()
    expect(h.reveal.getRevealed()).toBe('hello world')
    expect(h.pendingFrames()).toBe(0)
    expect(h.pendingDelays()).toBe(0)
  })

  it('reduced-motion uses large strides and zero delay', () => {
    const h = makeHarness({ prefersReducedMotion: () => true })
    h.reveal.setTarget('a'.repeat(50))
    h.stepFrame()
    expect(h.reveal.getRevealed().length).toBe(40)
    expect(h.pendingDelays()).toBe(1)
    // delay ms should be 0 under reduce — harness still queues; step it
    h.stepDelay()
    h.stepFrame()
    expect(h.reveal.getRevealed().length).toBe(50)
  })

  it('sync mode reveals full target on setTarget', () => {
    const onReveal = vi.fn()
    const reveal = createStreamTextReveal({ sync: true, onReveal })
    reveal.setTarget('full text now')
    expect(reveal.getRevealed()).toBe('full text now')
    expect(onReveal).toHaveBeenCalledWith('full text now')
  })

  it('jumps ahead so at most maxLag characters are left to animate', () => {
    const h = makeHarness()
    h.reveal.setTarget('a'.repeat(STREAM_REVEAL_MAX_LAG + 1000))
    h.stepFrame()
    expect(h.reveal.getRevealed().length).toBe(1000 + 8)
    h.reveal.setTarget(h.reveal.getTarget() + 'b'.repeat(5000))
    h.stepDelay()
    h.stepFrame()
    expect(h.reveal.getTarget().length - h.reveal.getRevealed().length).toBe(STREAM_REVEAL_MAX_LAG - 8)
    expect(h.reveal.getTarget().startsWith(h.reveal.getRevealed())).toBe(true)
  })

  it('maxLag 0 reveals each update whole', () => {
    const h = makeHarness({ maxLag: 0 })
    h.reveal.setTarget('hello world')
    h.stepFrame()
    expect(h.reveal.getRevealed()).toBe('hello world')
    expect(h.pendingDelays()).toBe(0)
  })

  it('reset clears both target and revealed', () => {
    const h = makeHarness()
    h.reveal.setTarget('abc')
    h.reveal.flush()
    h.reveal.reset()
    expect(h.reveal.getTarget()).toBe('')
    expect(h.reveal.getRevealed()).toBe('')
  })
})

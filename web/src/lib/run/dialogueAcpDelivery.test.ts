import { describe, expect, it, vi } from 'vitest'
import { deliverOrBufferGateAcp } from './dialogueAcpDelivery'
import type { AcpEvent } from '../shared/types'

const sample: AcpEvent[] = [
  { kind: 'thought', text: 't1', t: 1 },
  { kind: 'message', text: 'm1', t: 2 },
]

describe('deliverOrBufferGateAcp', () => {
  it('buffers when the frame is not for the gate', () => {
    const applyGate = vi.fn(() => true)
    expect(deliverOrBufferGateAcp({ forGate: false, events: sample, applyGate })).toBe('buffer')
    expect(applyGate).not.toHaveBeenCalled()
  })

  it('applies to a mounted gate panel (void/true)', () => {
    const applyGate = vi.fn(() => undefined)
    expect(deliverOrBufferGateAcp({ forGate: true, events: sample, applyGate })).toBe('applied')
    expect(applyGate).toHaveBeenCalledWith(sample)
  })

  it('buffers while the gate panel is not ready', () => {
    expect(deliverOrBufferGateAcp({ forGate: true, events: sample, applyGate: () => false })).toBe('buffer')
    expect(deliverOrBufferGateAcp({ forGate: true, events: sample })).toBe('buffer')
  })
})

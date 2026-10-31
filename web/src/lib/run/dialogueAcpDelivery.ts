import type { AcpEvent } from '../shared/types'

export type DialogueAcpApplyResult = 'applied' | 'buffer'

/**
 * Deliver cumulative ACP to the gate's visual-edit stream. ClarifyChat draws
 * the server dialogue state and takes no ACP. `applyGate` returns false while
 * the gate panel is not mounted yet; undefined/true = ok.
 */
export function deliverOrBufferGateAcp(opts: {
  forGate: boolean
  events: AcpEvent[]
  applyGate?: (events: AcpEvent[]) => boolean | void
}): DialogueAcpApplyResult {
  if (!opts.forGate || !opts.applyGate) return 'buffer'
  return opts.applyGate(opts.events) !== false ? 'applied' : 'buffer'
}

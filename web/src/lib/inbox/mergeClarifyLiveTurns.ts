import type { ClarifyTurn } from '@/lib/shared/types'
import { isEmptyFailedAgent, isRetryableFailedAgent } from '@/lib/inbox/clarifyEmptyFail'
import { sameParts, sameTools } from '@/lib/run/acpTools'

/**
 * True when `persisted` already has a completed agent reply after the in-flight
 * live human (no later human). Public poll / refresh can persist the finished
 * pair while liveTurns still hold a streaming placeholder — that must not
 * double-render the human bubble.
 *
 * Empty / failure agent rows count as settled for live teardown once persisted,
 * but cover-retry may still replace them via merge (see below).
 */
export function persistedCompletedLiveHuman(
  persisted: ClarifyTurn[],
  liveHumanText: string,
): boolean {
  const want = (liveHumanText || '').trim()
  if (!want || persisted.length < 2) return false
  for (let i = persisted.length - 2; i >= 0; i--) {
    const human = persisted[i]
    const agent = persisted[i + 1]
    if (
      human.role === 'human' &&
      (human.text || '').trim() === want &&
      agent.role === 'agent' &&
      !agent.streaming &&
      (!!(agent.text || agent.thought) || isRetryableFailedAgent(agent))
    ) {
      return !persisted.slice(i + 2).some((t) => t.role === 'human')
    }
  }
  return false
}

/**
 * Merge persisted transcript with in-flight liveTurns without duplicating
 * human/agent bubbles that softRefresh/loadRun already wrote into props.turns.
 *
 * Finds the longest prefix of `live` that matches the suffix of `persisted`
 * by role+text (streaming turns never match persisted), then appends only the
 * unmatched live tail (typically the streaming agent).
 *
 * If persisted already completed the live human's turn, live is dropped —
 * except when the persisted agent is an empty/failure card and live is
 * re-streaming the same human (cover retry): drop the failed agent and use live.
 */
export function mergePersistedAndLiveTurns(
  persisted: ClarifyTurn[],
  live: ClarifyTurn[],
): ClarifyTurn[] {
  if (!live.length) return persisted
  if (!persisted.length) return [...persisted, ...live]

  const liveHuman = live[0]?.role === 'human' ? live[0] : null
  const liveAgent = live.length >= 2 && live[1]?.role === 'agent' ? live[1] : null
  if (liveHuman && liveAgent?.streaming && persisted.length >= 2) {
    const ph = persisted[persisted.length - 2]
    const pa = persisted[persisted.length - 1]
    if (
      ph.role === 'human' &&
      pa.role === 'agent' &&
      (ph.text || '').trim() === (liveHuman.text || '').trim() &&
      isRetryableFailedAgent(pa)
    ) {
      // Cover retry: replace trailing empty/failure agent with live stream.
      return [...persisted.slice(0, -1), ...live.slice(1)]
    }
  }

  if (liveHuman && persistedCompletedLiveHuman(persisted, liveHuman.text || '')) {
    // The slot may already be closed while a longer snapshot is still arriving.
    // Keep that longer snapshot. A shorter or different live must not cover
    // the reply already on disk.
    const fresher = replaceWithFresherLiveAgent(persisted, live)
    if (fresher) return fresher
    return persisted
  }

  const maxK = Math.min(live.length, persisted.length)
  let matched = 0
  for (let candidate = maxK; candidate >= 1; candidate--) {
    let ok = true
    for (let i = 0; i < candidate; i++) {
      const lt = live[i]
      if (lt.streaming) {
        ok = false
        break
      }
      const pt = persisted[persisted.length - candidate + i]
      // Empty failed agent matches empty live agent by role only (both texts '').
      if (pt.role !== lt.role || (pt.text || '') !== (lt.text || '')) {
        if (
          pt.role === 'agent' &&
          lt.role === 'agent' &&
          isEmptyFailedAgent(pt) &&
          isEmptyFailedAgent(lt)
        ) {
          continue
        }
        ok = false
        break
      }
    }
    if (ok) {
      matched = candidate
      break
    }
  }
  if (matched === 0) return [...persisted, ...live]
  return [...persisted, ...live.slice(matched)]
}

/** True when live carries a strictly longer reply than the persisted agent. */
function liveAgentRicher(persisted: ClarifyTurn, live: ClarifyTurn): boolean {
  const pText = (persisted.text || '').trim()
  const lText = (live.text || '').trim()
  if (lText.length < pText.length) return false
  if (lText.length > pText.length) return true
  if (lText !== pText) return false
  const pThought = (persisted.thought || '').trim()
  const lThought = (live.thought || '').trim()
  if (lThought.length > pThought.length) return true
  if ((live.tools?.length || 0) > (persisted.tools?.length || 0)) return true
  if ((live.parts?.length || 0) > (persisted.parts?.length || 0)) return true
  return false
}

/**
 * When the same human turn is already persisted, a longer live agent is the
 * newer snapshot (slot closed, bridge still producing). A shorter or different
 * live must not cover the reply already on disk. Returns null when live has
 * nothing longer to show.
 */
function replaceWithFresherLiveAgent(persisted: ClarifyTurn[], live: ClarifyTurn[]): ClarifyTurn[] | null {
  const liveHuman = live.find((t) => t.role === 'human')
  const liveAgent = [...live].reverse().find((t) => t.role === 'agent')
  if (!liveHuman || !liveAgent) return null
  const text = (liveAgent.text || '').trim()
  const thought = (liveAgent.thought || '').trim()
  if (!text && !thought && !liveAgent.tools?.length && !liveAgent.parts?.length) return null
  const want = (liveHuman.text || '').trim()
  for (let i = persisted.length - 2; i >= 0; i--) {
    const human = persisted[i]
    const agent = persisted[i + 1]
    if (!human || !agent) continue
    if (human.role !== 'human' || (human.text || '').trim() !== want || agent.role !== 'agent') continue
    if (persisted.slice(i + 2).some((t) => t.role === 'human')) return null
    const same =
      (agent.text || '').trim() === text &&
      (agent.thought || '').trim() === thought &&
      sameTools(agent.tools, liveAgent.tools) &&
      sameParts(agent.parts, liveAgent.parts)
    if (same || !liveAgentRicher(agent, liveAgent)) return null
    const replaced: ClarifyTurn = {
      ...agent,
      text: liveAgent.text || '',
      thought: liveAgent.thought,
      streaming: !!liveAgent.streaming,
    }
    if (liveAgent.tools?.length) replaced.tools = liveAgent.tools
    else delete replaced.tools
    if (liveAgent.parts?.length) replaced.parts = liveAgent.parts
    else delete replaced.parts
    return [...persisted.slice(0, i + 1), replaced]
  }
  return null
}

/**
 * Acceptance checklist (g5.2): Clarify / Gate / Run × 硬刷新 / 重进 / WS / 完成态.
 * Source-contract + unit helpers — full browser matrix is for e2e/manual.
 *
 * Soft-refresh semantics and「思考中…」copy must remain unchanged (g2.3).
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it, vi } from 'vitest'
import { deliverOrBufferDialogueAcp } from './dialogueAcpDelivery'
import { runBusySeedRetry } from './busySeedRetry'
import { createWsReconnectController } from './wsReconnect'
import type { AcpEvent } from '@/lib/shared/types'

const here = dirname(fileURLToPath(import.meta.url))

function readSrc(rel: string): string {
  return readFileSync(join(here, '../..', rel), 'utf8')
}

const sample: AcpEvent[] = [
  { kind: 'thought', text: 't', t: 1 },
  { kind: 'message', text: 'm', t: 2 },
]

describe('stream resume acceptance (g5.2 three surfaces × four scenarios)', () => {
  it('g1: false-applied → buffer on clarify/gate delivery helpers', () => {
    expect(
      deliverOrBufferDialogueAcp({
        forClarify: true,
        forGate: false,
        events: sample,
        nodeId: 'c1',
        applyClarify: () => false,
      }),
    ).toBe('buffer')
    expect(
      deliverOrBufferDialogueAcp({
        forClarify: false,
        forGate: true,
        events: sample,
        nodeId: 'g1',
        applyGate: () => false,
      }),
    ).toBe('buffer')
  })

  it('g2.1: Inbox/Run restore order is queue_state → seed → flush', () => {
    // Inbox restore orchestration lives in useGatesInbox after structure sink.
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    // Run detail: seed/flush live in useRunDetailWs (entry only assembles the composable).
    const runWs = readSrc('lib/run/useRunDetailWs.ts')
    const runEntry = readSrc('views/RunDetailView.vue')
    const projectStart = inbox.indexOf('async function projectClarifySessionAfterLoad')
    expect(projectStart).toBeGreaterThan(-1)
    const projectSlice = inbox.slice(projectStart, projectStart + 1200)
    const inboxIdx = [
      projectSlice.indexOf('restoreReactSessions(r)'),
      projectSlice.indexOf('seedClarifyAcpFromNodeEventsOnce'),
      projectSlice.indexOf('flushPendingAcpFrames()'),
    ]
    expect(inboxIdx.every((i) => i >= 0)).toBe(true)
    expect(inboxIdx[0]).toBeLessThan(inboxIdx[1]!)
    expect(inboxIdx[1]).toBeLessThan(inboxIdx[2]!)
    expect(runEntry).toMatch(/useRunDetailWs|useRunDetail/)
    expect(runWs).toMatch(/seedDialogueNodeOnce/)
    expect(runWs.indexOf('seedDialogueAcpAfterRestore')).toBeGreaterThan(-1)
    expect(runWs.indexOf('flushPendingDialogueAcp')).toBeGreaterThan(-1)
  })

  it('g2.3: soft refresh path and thinkingBusy copy untouched', () => {
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    const clarify = [readSrc('components/run/ClarifyChat.vue'), readSrc('lib/inbox/useClarifyChat.ts')].join('\n')
    const gatePanel = readSrc('components/run/GateReactStreamPanel.vue')
    expect(inbox).toMatch(/isClarifySoftRefreshBlocked/)
    expect(inbox).toMatch(/Soft-refresh semantics unchanged/)
    expect(clarify).toMatch(/pages\.clarify\.thinkingBusy/)
    expect(gatePanel).toMatch(/pages\.clarify\.thinkingBusy/)
    expect(clarify).not.toMatch(/正在恢复对话/)
    expect(gatePanel).not.toMatch(/正在恢复对话/)
  })

  it('g2.1 multi-turn: seed writes absolute rails into live bubble only', () => {
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    const runWs = readSrc('lib/run/useRunDetailWs.ts')
    const clarify = readSrc('lib/inbox/useClarifyChat.ts')
    expect(inbox).toMatch(/current turn only/)
    expect(runWs).toMatch(/current-turn only/)
    // Absolute snapshot assignment (not += / append onto prior text).
    expect(clarify).toMatch(/if \(ev\.kind === 'message' && ev\.text\) msg = ev\.text/)
    expect(clarify).toMatch(/if \(ev\.kind === 'thought' && ev\.text\) thought = ev\.text/)
    expect(clarify).not.toMatch(/msg \+= /)
    expect(clarify).not.toMatch(/thought \+= /)
  })

  it('g3: busy seed retry stops on content / idle', async () => {
    let content = false
    const reason = await runBusySeedRetry({
      isBusy: () => true,
      hasContent: () => content,
      liveIncrementalReceived: () => false,
      intervalMs: 1,
      maxAttempts: 5,
      seed: async () => {
        content = true
        return true
      },
    })
    expect(reason).toBe('content')
    const idle = await runBusySeedRetry({
      isBusy: () => false,
      hasContent: () => false,
      seed: async () => false,
    })
    expect(idle).toBe('idle')
  })

  it('g4: WS reconnect controller schedules connect when wanted', () => {
    const connect = vi.fn()
    const ctrl = createWsReconnectController({
      connect,
      shouldReconnect: () => true,
      baseDelayMs: 1,
      maxDelayMs: 1,
      schedule: (fn) => {
        fn()
        return 1 as unknown as ReturnType<typeof setTimeout>
      },
      clearSchedule: () => undefined,
    })
    expect(ctrl.onClose()).toBe(true)
    expect(connect).toHaveBeenCalled()
  })

  it('Inbox + Run wire busySeedRetry and wsReconnect (g3/g4)', () => {
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    const runWs = readSrc('lib/run/useRunDetailWs.ts')
    const runEntry = [readSrc('views/RunDetailView.vue'), readSrc('lib/run/useRunDetail.ts')].join('\n')
    for (const src of [inbox, runWs]) {
      expect(src).toMatch(/createBusySeedRetryController/)
      expect(src).toMatch(/createWsReconnectController/)
      expect(src).toMatch(/fromReconnect/)
    }
    expect(inbox).toMatch(/projectClarifySessionAfterLoad/)
    expect(runEntry).toMatch(/useRunDetailWs/)
    expect(runEntry).toMatch(/projectDialogueAfterLoad/)
    expect(runWs).toMatch(/projectDialogueAfterLoad/)
  })

  it('g1.2: ACP busy=false must not stop busySeedRetry or clear platform session busy', () => {
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    const runWs = readSrc('lib/run/useRunDetailWs.ts')
    // Inbox: acp branch must not assign clarifyLiveBusy or stop retry on !busy.
    const acpInbox = inbox.slice(inbox.indexOf("if (m.type === 'acp')"))
    const acpInboxBlock = acpInbox.slice(0, acpInbox.indexOf('return') + 20)
    expect(acpInboxBlock).not.toMatch(/clarifyLiveBusy\.value = !!m\.busy/)
    expect(acpInboxBlock).not.toMatch(/if \(!m\.busy\) busySeedRetry\.stop\(\)/)
    expect(acpInboxBlock).toMatch(/observational only|must not overwrite platform|g1\.2/)
    // Run: ACP may update liveBusy for LiveLog, but must not stop dialogue retry.
    const acpRun = runWs.slice(runWs.indexOf("if (m.type === 'acp' && m.nodeId)"))
    const acpRunBlock = acpRun.slice(0, 600)
    expect(acpRunBlock).not.toMatch(/if \(!m\.busy\) busySeedRetry\.stop\(\)/)
    expect(runWs).toMatch(/dialoguePlatformBusy/)
    expect(runWs).toMatch(/Platform session busy only/)
  })

  it('g1.3: ReviewComposer exposes isSessionBusy', () => {
    const composer = readSrc('components/run/ReviewComposer.vue')
    expect(composer).toMatch(/isSessionBusy:\s*\(\)\s*=>/)
    expect(composer).toMatch(/chatRef\.value\?\.isSessionBusy/)
  })

  it('g2.2: Public page wires busySeedRetry + foreground re-seed', () => {
    const pub = readSrc('views/PublicGateApprovalView.vue')
    expect(pub).toMatch(/createBusySeedRetryController/)
    expect(pub).toMatch(/maybeStartPublicBusySeedRetry|startPublicBusySeedRetry/)
    expect(pub).toMatch(/resumeFromForeground/)
    const resumeIdx = pub.indexOf('async function resumeFromForeground')
    expect(resumeIdx).toBeGreaterThan(-1)
    expect(pub.slice(resumeIdx, resumeIdx + 500)).toMatch(/resyncChat\(/)
    // Foreground return and events-socket reconnect share the same re-seed.
    const resyncIdx = pub.indexOf('async function resyncChat')
    expect(resyncIdx).toBeGreaterThan(-1)
    const resyncSlice = pub.slice(resyncIdx, resyncIdx + 500)
    expect(resyncSlice).toMatch(/publicRailsFilled = false/)
    expect(resyncSlice).toMatch(/publicBusySeedRetry\.stop/)
  })

  it('g2.3: applyAcpEvents false must buffer on all three surfaces', () => {
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    const runWs = readSrc('lib/run/useRunDetailWs.ts')
    const pub = readSrc('views/PublicGateApprovalView.vue')
    expect(inbox).toMatch(/deliverOrBufferDialogueAcp/)
    expect(runWs).toMatch(/deliverOrBufferDialogueAcp/)
    expect(pub).toMatch(/applyAcpEvents\(events\) === false/)
    expect(pub).toMatch(/pendingPublicAcp = events/)
  })
})

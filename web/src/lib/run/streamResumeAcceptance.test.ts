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
import { deliverOrBufferGateAcp } from './dialogueAcpDelivery'
import { runBusySeedRetry } from './busySeedRetry'
import { createWsReconnectController } from './wsReconnect'
import { createReviewSessionState } from '@/lib/chat/reviewSessionState'
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
  it('g1: false-applied → buffer on the gate delivery helper', () => {
    expect(deliverOrBufferGateAcp({ forGate: true, events: sample, applyGate: () => false })).toBe('buffer')
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
    const clarifyChat = readSrc('components/run/ClarifyChat.vue')
    const clarify = [clarifyChat, readSrc('components/run/ClarifyThinkingRow.vue'), readSrc('lib/inbox/useClarifyChat.ts')].join('\n')
    const gatePanel = readSrc('components/run/GateReactStreamPanel.vue')
    expect(inbox).toMatch(/isClarifySoftRefreshBlocked/)
    expect(inbox).toMatch(/Soft-refresh semantics unchanged/)
    expect(clarifyChat).toMatch(/ClarifyThinkingRow/)
    expect(clarify).toMatch(/pages\.clarify\.thinkingBusy/)
    expect(gatePanel).toMatch(/pages\.clarify\.thinkingBusy/)
    expect(clarify).not.toMatch(/正在恢复对话/)
    expect(gatePanel).not.toMatch(/正在恢复对话/)
  })

  it('g2.1 multi-turn: the clarify chat draws only the server dialogue', () => {
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    const runWs = readSrc('lib/run/useRunDetailWs.ts')
    const clarify = readSrc('lib/inbox/useClarifyChat.ts')
    expect(inbox).toMatch(/current turn only/)
    expect(runWs).toMatch(/current-turn only/)
    expect(clarify).toMatch(/from '@\/lib\/chat\/dialogueStore'/)
    expect(clarify).not.toMatch(/applyAcpEvents|applyReviewFrame|applyQueueState/)
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
    // Session socket documents g1.2. The follow socket's acp branch is earlier
    // and only draws the current visit; it must not be the block under test.
    const sessionMark = inbox.indexOf('ACP/sandbox busy is observational only')
    expect(sessionMark).toBeGreaterThan(-1)
    const acpInbox = inbox.slice(inbox.lastIndexOf("if (m.type === 'acp')", sessionMark))
    const acpInboxBlock = acpInbox.slice(0, acpInbox.indexOf('return') + 20)
    expect(acpInboxBlock).not.toMatch(/clarifyLiveBusy\.value = !!m\.busy/)
    expect(acpInboxBlock).not.toMatch(/if \(!m\.busy\) busySeedRetry\.stop\(\)/)
    expect(acpInboxBlock).toMatch(/observational only|must not overwrite platform|g1\.2/)
    const followFn = inbox.slice(inbox.indexOf('function onFollowAcp'))
    const followEnd = followFn.indexOf('\nasync function ')
    const followBody = followFn.slice(0, followEnd > 0 ? followEnd : 1200)
    expect(followBody).not.toMatch(/clarifyLiveBusy/)
    expect(followBody).not.toMatch(/busySeedRetry/)
    // Run: ACP may update liveBusy for LiveLog, but must not stop dialogue retry.
    const acpRun = runWs.slice(runWs.indexOf("if (m.type === 'acp' && m.nodeId)"))
    const acpRunBlock = acpRun.slice(0, 600)
    expect(acpRunBlock).not.toMatch(/if \(!m\.busy\) busySeedRetry\.stop\(\)/)
    const state = createReviewSessionState()
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 1, revision: 1 })
    state.noteAcp('chat')
    expect(state.session('chat')?.busy).toBe(true)
    state.acceptServer({ event: 'queue_state', nodeId: 'chat', sessionGeneration: 1, revision: 2, busy: false })
    expect(state.session('chat')?.busy).toBe(false)
  })

  it('g1.3: ReviewComposer exposes isSessionBusy', () => {
    const composer = readSrc('components/run/ReviewComposer.vue')
    expect(composer).toMatch(/isSessionBusy:\s*\(\)\s*=>/)
    expect(composer).toMatch(/chatRef\.value\?\.isSessionBusy/)
  })

  it('g2.2: Public page re-reads the dialogue state on foreground return and reconnect', () => {
    const pub = readSrc('views/PublicGateApprovalView.vue')
    expect(pub).toMatch(/ingestDialogue\(/)
    expect(pub).toMatch(/setDialogueFetcher\(/)
    const resumeIdx = pub.indexOf('async function resumeFromForeground')
    expect(resumeIdx).toBeGreaterThan(-1)
    expect(pub.slice(resumeIdx, resumeIdx + 500)).toMatch(/resyncChat\(/)
    const resyncIdx = pub.indexOf('async function resyncChat')
    expect(resyncIdx).toBeGreaterThan(-1)
    expect(pub.slice(resyncIdx, resyncIdx + 300)).toMatch(/loadPreview\(/)
  })

  it('g2.3: applyAcpEvents false must buffer where ACP is still delivered', () => {
    const inbox = readSrc('lib/inbox/useGatesInbox.ts')
    const runWs = readSrc('lib/run/useRunDetailWs.ts')
    const pub = readSrc('views/PublicGateApprovalView.vue')
    expect(inbox).toMatch(/deliverOrBufferGateAcp/)
    expect(runWs).toMatch(/deliverOrBufferGateAcp/)
    expect(pub).not.toMatch(/applyAcpEvents/)
  })
})

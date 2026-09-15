// @vitest-environment node
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * plan g1.1 / g1.2 / g1.3: pin click-before-await ordering in source.
 * Overlay + inbox leave must not wait for reactReply / resumeGate / done.
 */
describe('confirm-flow click timing (plan g1)', () => {
  const root = resolve(__dirname, '../..')

  it('g1.1/g1.2: inbox force plays overlay and removes card before await reactReply', () => {
    const src = readFileSync(resolve(root, 'lib/inbox/useGatesInbox.ts'), 'utf8')
    const forceBlock = src.slice(src.indexOf('async function onClarifySend'), src.indexOf('function onClarifyFinish'))
    expect(forceBlock).toMatch(/void playConfirmFlowCeremony\(reviewChatRef\.value\)/)
    expect(forceBlock).toMatch(/removeListItemLocally\(submittedKey\)/)
    const playAt = forceBlock.indexOf('void playConfirmFlowCeremony(reviewChatRef.value)')
    const removeAt = forceBlock.indexOf('removeListItemLocally(submittedKey)')
    const awaitAt = forceBlock.indexOf('await api.reactReply')
    expect(playAt).toBeGreaterThanOrEqual(0)
    expect(removeAt).toBeGreaterThan(playAt)
    expect(awaitAt).toBeGreaterThan(removeAt)
    // Failure must restore the card (g2.2).
    expect(forceBlock).toMatch(/restoreListItemLocally\(submittedItem, prevList!\)/)
  })

  it('g1.1: run detail plays overlay before await; not after forceOk', () => {
    const src = readFileSync(resolve(root, 'lib/run/useRunDetail.ts'), 'utf8')
    const block = src.slice(src.indexOf('async function onClarifySend'), src.indexOf('async function onClarifyRetryLast'))
    expect(block).toMatch(/if \(force\) void playConfirmFlowCeremony\(reviewChatRef\.value\)/)
    expect(block).not.toMatch(/if \(forceOk\) await playConfirmFlowCeremony/)
    const playAt = block.indexOf('void playConfirmFlowCeremony(reviewChatRef.value)')
    const awaitAt = block.indexOf('await api.reactReply')
    expect(playAt).toBeGreaterThanOrEqual(0)
    expect(awaitAt).toBeGreaterThan(playAt)
  })

  it('g1.1: clarify chat does not auto-play on done; finishEarly plays on click', () => {
    const src = readFileSync(resolve(root, 'lib/inbox/useClarifyChat.ts'), 'utf8')
    expect(src).not.toMatch(/Success path: play overlay once/)
    expect(src).toMatch(/void playConfirmCeremony\(\)/)
    expect(src).toMatch(/Overlay is owned by the click path/)
  })

  it('g1.3: prototype demo contrasts click timing vs node_complete timing', () => {
    const demo = readFileSync(
      resolve(__dirname, '../../../../docs/prototypes/confirm-flow-click-timing.html'),
      'utf8',
    )
    expect(demo).toMatch(/正确：点击即动画，待办消失/)
    expect(demo).toMatch(/错误（现状）：等 node_complete 才动画/)
    expect(demo).toMatch(/playOverlay\(\);leaveInbox\(\)/)
  })
})

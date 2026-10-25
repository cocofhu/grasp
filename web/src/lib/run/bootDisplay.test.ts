import { describe, expect, it } from 'vitest'
import { resolveBootDisplay } from './bootDisplay'

const pending = { hasBoot: true, live: false }
const live = { hasBoot: true, live: true }
const none = { hasBoot: false, live: false }

describe('resolveBootDisplay', () => {
  it('hides the checklist and this boot’s new messages while the window is visible', () => {
    expect(resolveBootDisplay({
      windowVisible: true, seal: true, committed: false, nextAuto: null, ...pending,
    })).toEqual({ showStarting: false, showReady: false, holdMessages: true })
    expect(resolveBootDisplay({
      windowVisible: true, seal: true, committed: true, nextAuto: true, ...live,
    })).toEqual({ showStarting: false, showReady: false, holdMessages: true })
  })

  it('keeps an already-connected step’s ready row and messages while the window is open', () => {
    expect(resolveBootDisplay({
      windowVisible: true, seal: false, committed: false, nextAuto: null, ...live,
    })).toEqual({ showStarting: false, showReady: true, holdMessages: false })
  })

  it('plays the real in-progress checklist only for an automatic next process', () => {
    expect(resolveBootDisplay({
      windowVisible: false, seal: false, committed: true, nextAuto: true, ...pending,
    })).toEqual({ showStarting: true, showReady: false, holdMessages: true })
    expect(resolveBootDisplay({
      windowVisible: false, seal: false, committed: true, nextAuto: false, ...pending,
    })).toEqual({ showStarting: false, showReady: false, holdMessages: false })
  })

  it('shows 环境已就绪 immediately when the boot is already live, without a starting checklist', () => {
    expect(resolveBootDisplay({
      windowVisible: false, seal: false, committed: true, nextAuto: true, ...live,
    })).toEqual({ showStarting: false, showReady: true, holdMessages: false })
  })

  it('resumes the current step after cancel', () => {
    expect(resolveBootDisplay({
      windowVisible: false, seal: false, committed: false, nextAuto: null, ...pending,
    })).toEqual({ showStarting: true, showReady: false, holdMessages: true })
  })

  it('does not invent a checklist when there is no boot record', () => {
    expect(resolveBootDisplay({
      windowVisible: false, seal: false, committed: false, nextAuto: null, ...none,
    })).toEqual({ showStarting: false, showReady: false, holdMessages: false })
  })
})

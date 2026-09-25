// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PAGE_CONTROL_HELLO_MS, usePageControl } from './embedPageControl'
import { savePageControl } from './embedChat'

function setup(visible = true) {
  const posted: Record<string, unknown>[] = []
  const sent: Record<string, unknown>[] = []
  let vis = visible
  const pc = usePageControl({
    runId: 'r1',
    nodeId: 'n1',
    post: (m) => posted.push(m),
    send: (f) => {
      sent.push(f)
      return true
    },
    isVisible: () => vis,
  })
  return { pc, posted, sent, setVisible: (v: boolean) => (vis = v) }
}

const hello = { caps: ['page-control'], tab: 'tab-a' }

describe('usePageControl', () => {
  beforeEach(() => sessionStorage.clear())
  afterEach(() => vi.useRealTimers())

  it('stays unsupported when the page never announces page control', () => {
    vi.useFakeTimers()
    const { pc } = setup()
    pc.awaitHello()
    expect(pc.supported.value).toBeNull()
    vi.advanceTimersByTime(PAGE_CONTROL_HELLO_MS)
    expect(pc.supported.value).toBe(false)
    pc.setEnabled(true)
    expect(pc.enabled.value).toBe(false)
    pc.dispose()
  })

  it('reports the toggle with visibility and tells the page', () => {
    const { pc, posted, sent, setVisible } = setup()
    pc.onPageControl(hello)
    expect(pc.supported.value).toBe(true)
    expect(sent.at(-1)).toEqual({ type: 'page_control', on: false, visible: true })
    pc.setEnabled(true)
    expect(sent.at(-1)).toEqual({ type: 'page_control', on: true, visible: true })
    expect(posted.at(-1)).toEqual({ type: 'grasp-embed:control', on: true })
    setVisible(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(sent.at(-1)).toEqual({ type: 'page_control', on: true, visible: false })
    pc.onEventsReady()
    expect(sent.at(-1)).toEqual({ type: 'page_control', on: true, visible: false })
    pc.dispose()
  })

  it('restores the toggle after a reload in the same tab only', () => {
    savePageControl('r1', 'n1', 'tab-a', true)
    const same = setup()
    same.pc.onPageControl(hello)
    expect(same.pc.enabled.value).toBe(true)
    same.pc.dispose()

    // A tab opened from this one inherits sessionStorage but has its own tab id.
    const copy = setup()
    copy.pc.onPageControl({ caps: ['page-control'], tab: 'tab-b' })
    expect(copy.pc.enabled.value).toBe(false)
    copy.pc.dispose()
    const back = setup()
    back.pc.onPageControl(hello)
    expect(back.pc.enabled.value).toBe(false)
    back.pc.dispose()
  })

  it('relays commands with a fresh nonce and maps results back to the server id', () => {
    const { pc, posted, sent } = setup()
    pc.onPageControl(hello)
    pc.setEnabled(true)
    pc.onServerFrame({ type: 'page_cmd', id: 'c7', action: 'click', args: { index: 2, stateId: 'p:1' } })
    const cmd = posted.at(-1) as { type: string; nonce: string; action: string; args: unknown }
    expect(cmd.type).toBe('grasp-embed:cmd')
    expect(cmd.action).toBe('click')
    expect(cmd.args).toEqual({ index: 2, stateId: 'p:1' })
    expect(cmd.nonce).not.toBe('c7')

    pc.onPageResult({ nonce: 'forged', ok: true })
    expect(sent.some((f) => f.type === 'page_result')).toBe(false)

    pc.onPageResult({ nonce: cmd.nonce, ok: true, state: { stateId: 'p:2' } })
    expect(sent.at(-1)).toEqual({ type: 'page_result', id: 'c7', ok: true, error: undefined, note: undefined, state: { stateId: 'p:2' } })
    pc.onPageResult({ nonce: cmd.nonce, ok: true })
    expect(sent.filter((f) => f.type === 'page_result')).toHaveLength(1)
    pc.dispose()
  })

  it('refuses commands while the toggle is off', () => {
    const { pc, posted, sent } = setup()
    pc.onPageControl(hello)
    pc.onServerFrame({ type: 'page_cmd', id: 'c1', action: 'state' })
    expect(posted.some((m) => m.type === 'grasp-embed:cmd')).toBe(false)
    expect(sent.at(-1)).toMatchObject({ type: 'page_result', id: 'c1', ok: false })
    pc.dispose()
  })

  it('forwards server cancel and stops everything when the page banner says stop', () => {
    const { pc, posted, sent } = setup()
    pc.onPageControl(hello)
    pc.setEnabled(true)
    pc.onServerFrame({ type: 'page_cmd', id: 'c1', action: 'click', args: {} })
    const nonce = (posted.at(-1) as { nonce: string }).nonce
    pc.onServerFrame({ type: 'page_cmd_cancel', id: 'c1' })
    expect(posted.at(-1)).toEqual({ type: 'grasp-embed:cmd', nonce, action: 'cancel' })

    pc.onServerFrame({ type: 'page_cmd', id: 'c2', action: 'click', args: {} })
    const n2 = (posted.at(-1) as { nonce: string }).nonce
    pc.onPageControl({ stop: true })
    expect(pc.enabled.value).toBe(false)
    expect(posted).toContainEqual({ type: 'grasp-embed:cmd', nonce: n2, action: 'cancel' })
    expect(sent.at(-1)).toEqual({ type: 'page_control', on: false, visible: true })
    expect(sessionStorage.length).toBe(0)
    pc.dispose()
  })

  it('tracks server state frames and goes offline with the socket', () => {
    const { pc } = setup()
    pc.onServerFrame({ type: 'page_control_state', state: 'online', active: false })
    expect(pc.state.value).toBe('online')
    expect(pc.active.value).toBe(false)
    pc.onServerFrame({ type: 'page_control_state', state: 'paused' })
    expect(pc.state.value).toBe('paused')
    expect(pc.active.value).toBe(true)
    pc.onEventsClosed()
    expect(pc.state.value).toBe('offline')
    pc.dispose()
  })

  it('reset turns control off when the session ends', () => {
    const { pc } = setup()
    pc.onPageControl(hello)
    pc.setEnabled(true)
    pc.reset()
    expect(pc.enabled.value).toBe(false)
    expect(sessionStorage.length).toBe(0)
    pc.dispose()
  })
})

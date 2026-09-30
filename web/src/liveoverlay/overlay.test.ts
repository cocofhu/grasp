// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LIVE_ACK, LIVE_CMD, LIVE_MSG, LIVE_SESSIONS, createOverlay, type LiveOverlay } from './overlay'
import { strings } from './i18n'

const T = strings('zh-CN')

type Posted = Record<string, unknown>

function wrapperHtml(sid = 'sid001') {
  return (
    `<div id="w" data-grasp-live="${sid}" style="display:contents">` +
    '<section data-grasp-variant="0" hidden>orig</section>' +
    '<section data-grasp-variant="1" data-grasp-variant-label="层级">one</section>' +
    `<section data-grasp-variant="2" data-grasp-variant-label="紧凑" data-grasp-params='[{"id":"tone","kind":"steps","options":[{"value":"soft"},{"value":"strong"}]}]' hidden>two</section>` +
    '</div>'
  )
}

let overlay: LiveOverlay | null = null
let posted: Posted[] = []
let postOk = true
const notices: string[] = []

function make() {
  posted = []
  notices.length = 0
  overlay = createOverlay(
    {
      post: (m) => {
        if (!postOk) return false
        posted.push(m)
        return true
      },
      theme: () => 'dark',
      notice: (t) => void notices.push(t),
      stopPick: vi.fn(),
      changed: vi.fn(),
      isOwnUi: () => false,
    },
    T,
  )
  overlay.setEnabled(true)
  return overlay
}

const shadow = () => document.querySelector('grasp-live-overlay')!.shadowRoot!
const q = (sel: string) => shadow().querySelector(sel) as HTMLElement | null
const flush = () => new Promise((r) => setTimeout(r, 90))

function sessions(list: unknown[], replace = true) {
  overlay!.onDrawer({ type: LIVE_SESSIONS, replace, sessions: list })
}

beforeEach(() => {
  postOk = true
  sessionStorage.clear()
  history.replaceState(null, '', '/pricing')
})

afterEach(() => {
  overlay?.dispose()
  overlay = null
  document.body.innerHTML = ''
})

describe('Live overlay', () => {
  it('opens the bar, picks an element and sends generate', async () => {
    document.body.innerHTML = '<main><section id="card" class="c">Dispatch</section></main>'
    make()
    overlay!.toggle()
    expect(overlay!.isOpen()).toBe(true)
    expect(q('.bar')).not.toBeNull()
    ;(q('[data-act="pick"]') as HTMLButtonElement).click()
    expect(document.documentElement.hasAttribute('data-grasp-live-picking')).toBe(true)
    const card = document.getElementById('card')!
    card.getBoundingClientRect = () => new DOMRect(10, 10, 300, 180)
    card.dispatchEvent(new MouseEvent('mousemove', { bubbles: true }))
    expect(card.hasAttribute('data-grasp-live-hover')).toBe(true)
    card.click()
    expect(document.documentElement.hasAttribute('data-grasp-live-picking')).toBe(false)
    expect(q('.panel')).not.toBeNull()
    ;(q('[data-act="action"][data-v="colorize"]') as HTMLButtonElement).click()
    ;(q('[data-act="count"][data-v="4"]') as HTMLButtonElement).click()
    ;(q('[data-act="pmode"][data-v="compare"]') as HTMLButtonElement).click()
    const prompt = q('[data-input="prompt"]') as HTMLTextAreaElement
    prompt.value = '更醒目'
    prompt.dispatchEvent(new Event('input', { bubbles: true }))
    ;(q('[data-act="go"]') as HTMLButtonElement).click()
    const gen = posted.find((m) => m.op === 'generate')!
    expect(gen).toMatchObject({ type: LIVE_MSG, action: 'colorize', count: 4, prompt: '更醒目' })
    expect((gen.element as { selector: string }).selector).toBe('section#card')
    expect(notices).toContain(T.sent)
    expect(q('.panel')).toBeNull()
    // Optimistic session: shimmer over the picked element while generating.
    expect(q('[data-shimmer-sel]')).not.toBeNull()
    // A failed ack drops the optimistic session.
    overlay!.onDrawer({ type: LIVE_ACK, reqId: gen.reqId, ok: false, error: '还有一个未完成的 Live 变体' })
    expect(notices).toContain('还有一个未完成的 Live 变体')
    expect(q('[data-shimmer-sel]')).toBeNull()
  })

  it('requires a prompt for freeform and insert, and supports Esc', () => {
    document.body.innerHTML = '<main><section id="card">x</section></main>'
    document.getElementById('card')!.getBoundingClientRect = () => new DOMRect(10, 10, 300, 180)
    make()
    overlay!.toggle()
    ;(q('[data-act="insert"]') as HTMLButtonElement).click()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(document.documentElement.hasAttribute('data-grasp-live-picking')).toBe(false)
    ;(q('[data-act="insert"]') as HTMLButtonElement).click()
    document.getElementById('card')!.click()
    ;(q('[data-act="pos"][data-v="before"]') as HTMLButtonElement).click()
    ;(q('[data-act="go"]') as HTMLButtonElement).click()
    expect(notices).toContain(T.promptRequired)
    const notes = q('[data-input="notes"]') as HTMLTextAreaElement
    notes.value = '加一个 FAQ'
    notes.dispatchEvent(new Event('input', { bubbles: true }))
    ;(q('[data-act="go"]') as HTMLButtonElement).click()
    expect(posted.find((m) => m.op === 'insert')).toMatchObject({ position: 'before', notes: ['加一个 FAQ'], action: 'freeform' })
    ;(q('[data-act="pick"]') as HTMLButtonElement).click()
    document.getElementById('card')!.click()
    expect(notices).toContain(T.openOther)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  })

  it('shows a switcher for wrappers, switches, accepts with params and syncs state', async () => {
    document.body.innerHTML = wrapperHtml()
    make()
    sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', url: 'http://localhost/pricing', variants: [{ n: 1 }, { n: 2 }] }])
    expect(q('[data-sw="sid001"] .count')?.textContent).toBe('1 / 2')
    ;(q('[data-act="next"]') as HTMLButtonElement).click()
    const [v1, v2] = document.querySelectorAll<HTMLElement>('[data-grasp-variant="1"],[data-grasp-variant="2"]')
    expect(v1.hidden).toBe(true)
    expect(v2.hidden).toBe(false)
    expect(posted.at(-1)).toMatchObject({ op: 'state', sid: 'sid001', current: 2, mode: 'inplace' })
    // Params row for variant 2 (steps).
    ;(q('[data-param="sid001|2|tone"][data-v="strong"]') as HTMLButtonElement).click()
    expect(v2.getAttribute('data-gp-tone')).toBe('strong')
    ;(q('[data-sw="sid001"] [data-act="accept"]') as HTMLButtonElement).click()
    expect(posted.at(-1)).toMatchObject({ op: 'accept', variant: 2, params: { tone: 'strong' } })
    expect(q('[data-sw="sid001"] .state')?.textContent).toBe(T.accepting)
    // Second accept while busy is ignored.
    const n = posted.length
    ;(q('[data-sw="sid001"] [data-act="accept"]') as HTMLButtonElement).click()
    expect(posted.length).toBe(n)
    // Keyboard switching (after the accept resolves back to ready).
    overlay!.onDrawer({ type: LIVE_ACK, reqId: posted.at(-1)!.reqId, ok: false, error: 'x' })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft' }))
    expect(posted.at(-1)).toMatchObject({ op: 'state', current: 1 })
  })

  it('compare mode shows every variant with choose buttons and keep-original', () => {
    document.body.innerHTML = wrapperHtml()
    make()
    sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] }])
    ;(q('[data-act="compare"]') as HTMLButtonElement).click()
    const w = document.getElementById('w')!
    expect(w.hasAttribute('data-grasp-compare')).toBe(true)
    expect([...w.children].every((c) => !(c as HTMLElement).hidden)).toBe(true)
    expect(shadow().querySelectorAll('[data-badge="sid001"]').length).toBe(3)
    ;(q('[data-badge="sid001"][data-n="2"] [data-act="accept"]') as HTMLButtonElement).click()
    expect(posted.at(-1)).toMatchObject({ op: 'accept', variant: 2 })
    overlay!.onDrawer({ type: LIVE_ACK, reqId: posted.at(-1)!.reqId, ok: true, session: { sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }], updatedAt: '9' } })
    ;(q('[data-badge="sid001"][data-n="1"] [data-act="inplace"]') as HTMLButtonElement).click()
    expect(w.hasAttribute('data-grasp-compare')).toBe(false)
    expect(posted.at(-1)).toMatchObject({ op: 'state', current: 1, mode: 'inplace' })
    ;(q('[data-act="compare"]') as HTMLButtonElement).click()
    ;(q('[data-badge="sid001"][data-n="0"] [data-act="discard"]') as HTMLButtonElement).click()
    expect(posted.at(-1)).toMatchObject({ op: 'discard', sid: 'sid001' })
  })

  it('restores the view after a reload and follows drawer commands', () => {
    sessionStorage.setItem('__grasp_live', JSON.stringify({ sid001: { current: 2, mode: 'inplace', params: {}, at: 1 } }))
    document.body.innerHTML = wrapperHtml()
    make()
    sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] }])
    expect(q('[data-sw="sid001"] .count')?.textContent).toBe('2 / 2')
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'goto', variant: 1 })
    expect(overlay!.isOpen()).toBe(true)
    expect(q('[data-sw="sid001"] .count')?.textContent).toBe('1 / 2')
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'compare' })
    expect(document.getElementById('w')!.hasAttribute('data-grasp-compare')).toBe(true)
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'inplace' })
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'accept' })
    expect(posted.at(-1)).toMatchObject({ op: 'accept', variant: 1 })
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'discard' })
    overlay!.onDrawer({ type: LIVE_CMD, cmd: 'discard' })
    overlay!.onDrawer(null)
    // Ended sessions drop their saved view.
    sessions([{ sid: 'sid001', state: 'accepted', mode: 'replace' }])
    expect(sessionStorage.getItem('__grasp_live')).not.toContain('sid001')
  })

  it('is view-only without a known session and reports failed mounts', async () => {
    vi.useFakeTimers()
    try {
      document.body.innerHTML = wrapperHtml('orphan1')
      make()
      sessions([])
      const discard = q('[data-sw="orphan1"] [data-act="discard"]') as HTMLButtonElement
      expect(discard.disabled).toBe(true)
      document.body.innerHTML = ''
      sessions([{ sid: 'sid009', state: 'ready', mode: 'replace', url: `${location.origin}/pricing`, variants: [{ n: 1 }], updatedAt: '1' }])
      vi.advanceTimersByTime(6500)
      const mf = posted.find((m) => m.op === 'mount_failed')
      expect(mf).toMatchObject({ sid: 'sid009' })
      const count = posted.filter((m) => m.op === 'mount_failed').length
      vi.advanceTimersByTime(3000)
      expect(posted.filter((m) => m.op === 'mount_failed').length).toBe(count)
    } finally {
      vi.useRealTimers()
    }
  })

  it('reports candidates missing from the rendered page even when some candidates mounted', () => {
    vi.useFakeTimers()
    try {
      document.body.innerHTML = wrapperHtml()
      make()
      sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', url: `${location.origin}/pricing`, variants: [{ n: 1 }, { n: 2 }, { n: 3 }], updatedAt: '1' }])
      vi.advanceTimersByTime(6500)
      expect(posted.filter((message) => message.op === 'mount_failed')).toEqual([
        expect.objectContaining({ sid: 'sid001', error: 'reported variants not rendered: 3' }),
      ])
    } finally {
      vi.useRealTimers()
    }
  })

  it('allows HMR to finish rendering all reported candidates during the grace period', async () => {
    vi.useFakeTimers()
    try {
      document.body.innerHTML = wrapperHtml()
      make()
      sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', url: `${location.origin}/pricing`, variants: [{ n: 1 }, { n: 2 }, { n: 3 }], updatedAt: '1' }])
      await vi.advanceTimersByTimeAsync(2000)
      document.getElementById('w')!.insertAdjacentHTML('beforeend', '<section data-grasp-variant="3" hidden>three</section>')
      // Let the mutation observer rescan markup arriving after the ready frame.
      await vi.advanceTimersByTimeAsync(6500)
      expect(posted.some((message) => message.op === 'mount_failed')).toBe(false)
      expect(q('[data-sw="sid001"] .count')?.textContent).toBe('1 / 3')
    } finally {
      vi.useRealTimers()
    }
  })

  it('points at sessions on other routes, steers, peeks and hides', () => {
    document.body.innerHTML = wrapperHtml()
    make()
    overlay!.toggle()
    sessions([
      { sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] },
      { sid: 'sid002', state: 'ready', mode: 'replace', url: `${location.origin}/about`, variants: [{ n: 1 }] },
    ])
    expect(q('.hint')?.textContent).toContain('/about')
    const steer = q('[data-input="steer"]') as HTMLInputElement
    steer.value = '整体再紧凑一些'
    steer.dispatchEvent(new Event('input', { bubbles: true }))
    steer.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(posted.at(-1)).toMatchObject({ op: 'steer', prompt: '整体再紧凑一些' })
    const eye = q('[data-act="eye"]') as HTMLButtonElement
    eye.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, composed: true }))
    const orig = document.querySelector<HTMLElement>('[data-grasp-variant="0"]')!
    expect(orig.hidden).toBe(false)
    eye.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, composed: true }))
    expect(orig.hidden).toBe(true)
    eye.click()
    expect(q('[data-sw="sid001"]')).toBeNull()
    ;(q('[data-act="close"]') as HTMLButtonElement).click()
    expect(overlay!.isOpen()).toBe(false)
    postOk = false
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'discard' })
    expect(notices).toContain(T.noDrawer)
    overlay!.setEnabled(false)
    expect(q('[data-sw="sid001"]')).toBeNull()
  })

  it('rescans when HMR swaps the markup in', async () => {
    document.body.innerHTML = '<main id="m"></main>'
    make()
    sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] }])
    expect(q('[data-sw="sid001"]')).toBeNull()
    document.getElementById('m')!.innerHTML = wrapperHtml()
    await flush()
    expect(q('[data-sw="sid001"]')).not.toBeNull()
  })

  it('offers retry and non-editing dismissal for a failed whole-page adjustment', () => {
    document.body.innerHTML = '<main>partial adjustment</main>'
    make()
    overlay!.toggle()
    sessions([{ sid: 'steer01', mode: 'steer', state: 'failed', prompt: 'Make it quieter', url: `${location.origin}/pricing`, error: 'compile failed' }])
    expect(q('.hint')?.textContent).toContain(T.steerPartial)
    ;(q('[data-act="retry"]') as HTMLButtonElement).click()
    const retry = posted.find((m) => m.op === 'steer')!
    expect(retry).toMatchObject({ sid: 'steer01', prompt: 'Make it quieter' })
    expect(q('[data-act="retry"]')).toBeNull()
    overlay!.onDrawer({ type: LIVE_ACK, reqId: retry.reqId, ok: false, error: 'still failed' })
    expect(q('[data-act="retry"]')).not.toBeNull()
    ;(q('[data-act="discard"]') as HTMLButtonElement).click()
    const discard = posted.find((m) => m.op === 'discard')!
    expect(discard).toMatchObject({ sid: 'steer01' })
    overlay!.onDrawer({ type: LIVE_ACK, reqId: discard.reqId, ok: true, session: { sid: 'steer01', mode: 'steer', state: 'discarded' } })
    expect(q('.hint')).toBeNull()
    expect(document.querySelector('main')!.textContent).toBe('partial adjustment')
  })

  it('retries interrupted adoption without a wrapper or replacing persisted params', () => {
    document.body.innerHTML = '<main>adopted markup pending cleanup</main>'
    make()
    overlay!.toggle()
    sessions([{ sid: 'sid001', mode: 'replace', state: 'failed', selected: 2, retryAccept: true, error: 'cleanup interrupted' }])
    expect(q('[data-act="retry-accept"]')?.textContent).toBe(T.retryAccept)
    // Ordinary acceptance stays locked without a ready, mounted candidate.
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'accept', variant: 1 })
    expect(posted.some((m) => m.op === 'accept')).toBe(false)
    ;(q('[data-act="retry-accept"]') as HTMLButtonElement).click()
    const first = posted.find((m) => m.op === 'accept')!
    expect(first).toMatchObject({ sid: 'sid001', variant: 2 })
    expect(first).not.toHaveProperty('params')
    // An HTTP failure must leave the same explicit recovery available.
    overlay!.onDrawer({ type: LIVE_ACK, reqId: first.reqId, ok: false, error: 'offline' })
    expect(q('[data-act="retry-accept"]')).not.toBeNull()
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'retry-accept', variant: 1 })
    const second = posted.filter((m) => m.op === 'accept').at(-1)!
    expect(second).toMatchObject({ variant: 2 })
    expect(second).not.toHaveProperty('params')
    overlay!.onDrawer({ type: LIVE_ACK, reqId: second.reqId, ok: true, session: { sid: 'sid001', mode: 'replace', state: 'accepted', selected: 2 } })
    expect(q('[data-act="retry-accept"]')).toBeNull()
    expect(document.querySelector('main')?.textContent).toBe('adopted markup pending cleanup')
    sessions([{ sid: 'sid002', mode: 'replace', state: 'failed', selected: 1 }])
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid002', cmd: 'retry-accept' })
    expect(posted.filter((m) => m.op === 'accept')).toHaveLength(2)
  })
})

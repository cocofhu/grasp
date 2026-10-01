// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LIVE_ACK, LIVE_CMD, LIVE_MSG, LIVE_SESSIONS, createOverlay, type LiveOverlay } from './overlay'
import { strings } from './i18n'
import { OVERLAY_CSS } from './styles'

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
let toChat: Element[] = []
let stopPick = vi.fn()
let changed = vi.fn()

function make() {
  posted = []
  notices.length = 0
  toChat = []
  stopPick = vi.fn()
  changed = vi.fn()
  overlay = createOverlay(
    {
      post: (m) => {
        if (!postOk) return false
        posted.push(m)
        return true
      },
      theme: () => 'dark',
      notice: (t) => void notices.push(t),
      stopPick,
      sendToChat: (el) => void toChat.push(el),
      changed,
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
  it('keeps the primary chip text light on the purple background', () => {
    expect(OVERLAY_CSS).toContain('.chip.go,.chip.go:hover,.chip.go:focus,.chip.go:focus-visible{background:var(--acc);color:#fff}')
    expect(OVERLAY_CSS).toContain('.chip.go:hover,.chip.go:focus:hover,.chip.go:focus-visible:hover{background:var(--acc-hover);color:#fff}')
    expect(OVERLAY_CSS).toContain('.light .chip.go:hover,.light .chip.go:focus:hover,.light .chip.go:focus-visible:hover{background:var(--acc-hover);color:#fff}')
  })

  it('keeps the action card, dock, frames and tags under the chat drawer', () => {
    const layers = ['.dock', '.panel', '.sw', '.params', '.frame', '.shimmer', '.cframe', '.tag']
    for (const sel of layers) {
      const z = OVERLAY_CSS.match(new RegExp(sel.replace('.', '\\.') + '\\{[^}]*z-index:(\\d+)'))
      expect(z, sel).not.toBeNull()
      expect(Number(z![1]), sel).toBeLessThan(2147483647)
    }
  })

  it('has no toolbar of its own and offers chat or design for a picked element', () => {
    document.body.innerHTML = '<main><section id="card" class="c">Dispatch</section></main>'
    make()
    expect(q('.bar')).toBeNull()
    expect(q('[data-act="pick"]')).toBeNull()
    const card = document.getElementById('card')!
    card.getBoundingClientRect = () => new DOMRect(10, 10, 300, 180)
    overlay!.offer(card)
    expect(q('.panel.choose .target')?.textContent).toContain('section')
    expect(q('[data-act="go"]')).toBeNull()
    ;(q('[data-act="to-chat"]') as HTMLButtonElement).click()
    expect(toChat).toEqual([card])
    expect(q('.panel')).toBeNull()
    expect(posted).toEqual([])
    overlay!.offer(card)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(q('.panel')).toBeNull()
    // A disabled overlay falls back to a plain chat pick.
    overlay!.setEnabled(false)
    overlay!.offer(card)
    expect(toChat).toEqual([card, card])
    expect(q('.panel')).toBeNull()
  })

  it('designs a picked element and sends generate', async () => {
    document.body.innerHTML = '<main><section id="card" class="c">Dispatch</section></main>'
    make()
    const card = document.getElementById('card')!
    card.getBoundingClientRect = () => new DOMRect(10, 10, 300, 180)
    overlay!.offer(card)
    ;(q('[data-act="to-design"]') as HTMLButtonElement).click()
    expect(q('.panel.choose')).toBeNull()
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
    const card = document.getElementById('card')!
    card.getBoundingClientRect = () => new DOMRect(10, 10, 300, 180)
    make()
    overlay!.startInsert()
    expect(overlay!.isInserting()).toBe(true)
    expect(stopPick).toHaveBeenCalled()
    expect(changed).toHaveBeenCalled()
    expect(overlay!.isPickMode()).toBe(true)
    expect(q('.pickbar .pickhint')?.textContent).toBe(T.insertPicking)
    expect(q('[data-act="mode-insert"]')?.getAttribute('aria-pressed')).toBe('true')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(document.documentElement.hasAttribute('data-grasp-live-picking')).toBe(false)
    expect(overlay!.isInserting()).toBe(false)
    expect(overlay!.isPickMode()).toBe(false)
    expect(q('.pickbar')).toBeNull()
    overlay!.startInsert()
    overlay!.cancelPick()
    expect(overlay!.isInserting()).toBe(false)
    overlay!.startInsert()
    expect(document.documentElement.hasAttribute('data-grasp-live-picking')).toBe(true)
    card.dispatchEvent(new MouseEvent('mousemove', { bubbles: true }))
    expect(card.hasAttribute('data-grasp-live-hover')).toBe(true)
    card.click()
    // Insert skips the chat-or-design card, ends pick mode and has no action chips.
    expect(q('.panel.choose')).toBeNull()
    expect(overlay!.isPickMode()).toBe(false)
    expect(q('[data-act="action"]')).toBeNull()
    expect(q('[data-act="go"]')?.textContent).toBe(T.go)
    ;(q('[data-act="pos"][data-v="before"]') as HTMLButtonElement).click()
    ;(q('[data-act="go"]') as HTMLButtonElement).click()
    expect(notices).toContain(T.promptRequired)
    // Notes and marks live in a collapsed section.
    expect(q('[data-input="notes"]')).toBeNull()
    ;(q('[data-act="marks-toggle"]') as HTMLButtonElement).click()
    expect(q('[data-act="mark-undo"]')).toBeNull()
    const notes = q('[data-input="notes"]') as HTMLTextAreaElement
    notes.value = '加一个 FAQ'
    notes.dispatchEvent(new Event('input', { bubbles: true }))
    ;(q('[data-act="go"]') as HTMLButtonElement).click()
    expect(posted.find((m) => m.op === 'insert')).toMatchObject({ position: 'before', notes: ['加一个 FAQ'], action: 'freeform' })
    // Another set is open: design is blocked, chat still works.
    overlay!.offer(card)
    const design = q('[data-act="to-design"]') as HTMLButtonElement
    expect(design.disabled).toBe(true)
    expect(q('.panel.choose')?.textContent).toContain(T.openOther)
    design.click()
    expect(q('.panel.choose')).not.toBeNull()
    ;(q('[data-act="to-chat"]') as HTMLButtonElement).click()
    expect(toChat).toEqual([card])
  })

  it('ignores clicks on the host bar while inserting', () => {
    document.body.innerHTML = '<main><section id="card">x</section></main><div id="hostbar"><button id="plus">+</button></div>'
    const hostBar = document.getElementById('hostbar')!
    overlay = createOverlay(
      {
        post: () => true,
        theme: () => 'dark',
        notice: () => {},
        stopPick: () => {},
        sendToChat: () => {},
        changed: () => {},
        isOwnUi: (el) => el === hostBar || hostBar.contains(el),
      },
      T,
    )
    overlay.setEnabled(true)
    overlay.startInsert()
    document.getElementById('plus')!.click()
    expect(overlay.isInserting()).toBe(true)
    expect(q('.panel')).toBeNull()
  })

  it('switches between select and insert from the pick bar', () => {
    document.body.innerHTML = '<main><section id="card">x</section></main>'
    const startPick = vi.fn()
    overlay = createOverlay(
      { post: () => true, theme: () => 'dark', notice: () => {}, stopPick, startPick, sendToChat: () => {}, changed, isOwnUi: () => false },
      T,
    )
    overlay.setEnabled(true)
    overlay.setPickMode(true)
    ;(q('[data-act="mode-insert"]') as HTMLButtonElement).click()
    expect(overlay.isInserting()).toBe(true)
    expect(stopPick).toHaveBeenCalled()
    ;(q('[data-act="mode-select"]') as HTMLButtonElement).click()
    expect(overlay.isInserting()).toBe(false)
    expect(startPick).toHaveBeenCalled()
    expect(overlay.isPickMode()).toBe(true)
    overlay.setPickMode(false)
    expect(q('.pickbar')).toBeNull()
  })

  it('re-renders the pick bar and the panel when the language changes', () => {
    document.body.innerHTML = '<main><section id="card">x</section></main>'
    const card = document.getElementById('card')!
    card.getBoundingClientRect = () => new DOMRect(10, 10, 300, 180)
    overlay = createOverlay({ post: () => true, theme: () => 'dark', notice: () => {}, stopPick: () => {}, sendToChat: () => {}, changed: () => {}, isOwnUi: () => false, lang: 'en' })
    overlay.setEnabled(true)
    const en = strings('en')
    overlay.setPickMode(true)
    expect(q('[data-act="mode-select"]')?.textContent).toBe(en.modeSelect)
    overlay.offer(card)
    ;(q('[data-act="to-design"]') as HTMLButtonElement).click()
    expect(q('[data-act="go"]')?.textContent).toBe(en.go)
    overlay.setLang('zh-CN')
    expect(q('[data-act="go"]')?.textContent).toBe(T.go)
    expect(q('[data-act="action"][data-v="bolder"]')?.textContent).toBe(T.actions.bolder)
    overlay.setPickMode(true)
    expect(q('[data-act="mode-select"]')?.textContent).toBe(T.modeSelect)
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

  it('compare mode keeps the page layout, labels each candidate and has one toolbar to leave it', () => {
    document.body.innerHTML = wrapperHtml()
    make()
    sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] }])
    ;(q('[data-act="compare"]') as HTMLButtonElement).click()
    const w = document.getElementById('w')!
    expect(w.getAttribute('data-grasp-compare')).toBe('')
    expect([...w.children].every((c) => !(c as HTMLElement).hidden)).toBe(true)
    expect(document.querySelector('style[data-grasp-live-overlay]')?.textContent).not.toMatch(/grid|margin-top/)
    // One label and frame per candidate; the labels only select.
    const tags = [...shadow().querySelectorAll<HTMLElement>('[data-tag="sid001"]')]
    expect(tags.map((t) => t.textContent)).toEqual([T.original, '1 · 层级', '2 · 紧凑'])
    expect(tags.every((t) => t.dataset.act === 'select')).toBe(true)
    expect(shadow().querySelectorAll('[data-cframe="sid001"]').length).toBe(3)
    expect(shadow().querySelectorAll('[data-sw="sid001"]').length).toBe(1)
    const bar = () => q('[data-sw="sid001"][data-compare]')!
    expect(bar().querySelector('[data-act="accept"]')?.textContent).toBe('采用 1')
    ;(q('[data-tag="sid001"][data-n="2"]') as HTMLButtonElement).click()
    expect(q('[data-tag="sid001"][data-n="2"]')?.getAttribute('aria-pressed')).toBe('true')
    expect(q('[data-cframe="sid001"][data-n="2"]')?.classList.contains('sel')).toBe(true)
    ;(bar().querySelector('[data-act="accept"]') as HTMLButtonElement).click()
    expect(posted.at(-1)).toMatchObject({ op: 'accept', variant: 2 })
    overlay!.onDrawer({ type: LIVE_ACK, reqId: posted.at(-1)!.reqId, ok: true, session: { sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }], updatedAt: '9' } })
    // Clicking the original in the page selects it; accept is then disabled.
    ;(w.querySelector('[data-grasp-variant="0"]') as HTMLElement).click()
    expect(bar().querySelector('.lab')?.textContent).toBe(T.selectedOriginal)
    expect((bar().querySelector('[data-act="accept"]') as HTMLButtonElement).disabled).toBe(true)
    // Back in place from the original lands on the first variant.
    ;(bar().querySelector('[data-act="inplace"]') as HTMLButtonElement).click()
    expect(w.hasAttribute('data-grasp-compare')).toBe(false)
    expect(posted.at(-1)).toMatchObject({ op: 'state', current: 1, mode: 'inplace' })
    // Esc also leaves compare, keeping the selection.
    ;(q('[data-act="compare"]') as HTMLButtonElement).click()
    ;(q('[data-tag="sid001"][data-n="2"]') as HTMLButtonElement).click()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(w.hasAttribute('data-grasp-compare')).toBe(false)
    expect(q('[data-sw="sid001"] .count')?.textContent).toBe('2 / 2')
    ;(q('[data-act="compare"]') as HTMLButtonElement).click()
    ;(bar().querySelector('[data-act="discard"]') as HTMLButtonElement).click()
    expect(posted.at(-1)).toMatchObject({ op: 'discard', sid: 'sid001' })
  })

  it('writes the theme when the action card opens and when the theme changes', async () => {
    document.body.innerHTML = '<main><section id="card">Hi</section></main>'
    document.body.style.backgroundColor = 'rgb(15, 23, 42)'
    let mode = 'light'
    overlay = createOverlay(
      { post: () => true, theme: () => mode, notice: () => {}, stopPick: () => {}, sendToChat: () => {}, changed: () => {}, isOwnUi: () => false },
      T,
    )
    overlay.setEnabled(true)
    const card = document.getElementById('card')!
    card.getBoundingClientRect = () => new DOMRect(10, 10, 200, 40)
    overlay.offer(card)
    expect(q('.root')?.classList.contains('light')).toBe(false)
    expect(q('[data-act="to-chat"]')?.textContent).toBe('引用')
    expect(q('[data-act="to-design"]')?.textContent).toBe('修改')
    expect(strings('en').toChat).toBe('Quote')
    expect(strings('en').toDesign).toBe('Edit')
    expect(strings('en').pickHint).toBe('Click an element to quote or edit')
    expect(T.pickHint).toBe('点选元素：引用或修改')
    document.body.style.backgroundColor = 'rgb(250, 250, 250)'
    await flush()
    expect(q('.root')?.classList.contains('light')).toBe(true)
    document.body.style.backgroundColor = ''
    mode = 'dark'
    overlay.syncTheme()
    expect(q('.root')?.classList.contains('light')).toBe(false)
  })

  it('follows the page background for its theme', () => {
    document.body.innerHTML = wrapperHtml()
    document.body.style.backgroundColor = 'rgb(15, 23, 42)'
    overlay = createOverlay(
      { post: () => true, theme: () => 'light', notice: () => {}, stopPick: () => {}, sendToChat: () => {}, changed: () => {}, isOwnUi: () => false },
      T,
    )
    overlay.setEnabled(true)
    expect(q('.root')?.classList.contains('light')).toBe(false)
    document.body.style.backgroundColor = 'rgb(250, 250, 250)'
    overlay.setEnabled(false)
    overlay.setEnabled(true)
    expect(q('.root')?.classList.contains('light')).toBe(true)
    document.body.style.backgroundColor = ''
    overlay.dispose()
    overlay = createOverlay(
      { post: () => true, theme: () => 'light', notice: () => {}, stopPick: () => {}, sendToChat: () => {}, changed: () => {}, isOwnUi: () => false },
      T,
    )
    overlay.setEnabled(true)
    expect(q('.root')?.classList.contains('light')).toBe(true)
  })

  it('restores the view after a reload and follows drawer commands', () => {
    sessionStorage.setItem('__grasp_live', JSON.stringify({ sid001: { current: 2, mode: 'inplace', params: {}, at: 1 } }))
    document.body.innerHTML = wrapperHtml()
    make()
    sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] }])
    expect(q('[data-sw="sid001"] .count')?.textContent).toBe('2 / 2')
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'goto', variant: 1 })
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

  it('compares with the original in place: toggle, cycle and drawer goto', () => {
    document.body.innerHTML = wrapperHtml()
    make()
    sessions([{ sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] }])
    const shown = () => [...document.querySelectorAll<HTMLElement>('[data-grasp-variant]')].filter((el) => !el.hidden).map((el) => el.dataset.graspVariant)
    const btn = (act: string) => q(`[data-sw="sid001"] [data-act="${act}"]`) as HTMLButtonElement
    const lastState = () => posted.filter((m) => m.op === 'state').at(-1)

    expect(btn('original').getAttribute('aria-pressed')).toBe('false')
    btn('next').click()
    expect(shown()).toEqual(['2'])
    btn('original').click()
    expect(shown()).toEqual(['0'])
    expect(q('[data-sw="sid001"] .count')?.textContent).toBe(T.original)
    expect(btn('original').getAttribute('aria-pressed')).toBe('true')
    expect(btn('accept').disabled).toBe(true)
    expect(lastState()).toMatchObject({ current: 0, original: true })
    // Toggling back returns to the candidate being compared, not the first one.
    btn('original').click()
    expect(shown()).toEqual(['2'])
    expect(lastState()).toMatchObject({ current: 2 })
    expect(lastState()).not.toHaveProperty('original')

    // The original sits at the start of the in-place cycle.
    btn('prev').click()
    btn('prev').click()
    expect(shown()).toEqual(['0'])
    btn('next').click()
    expect(shown()).toEqual(['1'])

    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'goto', variant: 0 })
    expect(shown()).toEqual(['0'])
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'sid001', cmd: 'goto', variant: 2 })
    expect(shown()).toEqual(['2'])
  })

  it('insert wrappers have no original to compare', () => {
    document.body.innerHTML =
      '<div data-grasp-live="ins001" style="display:contents"><section data-grasp-variant="1">a</section><section data-grasp-variant="2" hidden>b</section></div>'
    make()
    sessions([{ sid: 'ins001', state: 'ready', mode: 'insert', variants: [{ n: 1 }, { n: 2 }] }])
    expect(q('[data-sw="ins001"] [data-act="original"]')).toBeNull()
    overlay!.onDrawer({ type: LIVE_CMD, sid: 'ins001', cmd: 'goto', variant: 0 })
    expect(posted.filter((m) => m.op === 'state').at(-1)).toMatchObject({ current: 1 })
  })

  it('auto-reports a missing wrapper once, then leaves it to the person', () => {
    vi.useFakeTimers()
    try {
      make()
      const ready = (updatedAt: string, extra: Record<string, unknown> = {}) =>
        sessions([{ sid: 'sid009', state: 'ready', mode: 'replace', url: `${location.origin}/pricing`, variants: [{ n: 1 }], updatedAt, ...extra }])
      const reports = () => posted.filter((m) => m.op === 'mount_failed')

      ready('1')
      vi.advanceTimersByTime(6500)
      expect(reports()).toEqual([expect.objectContaining({ sid: 'sid009', auto: true, error: 'no [data-grasp-live="sid009"] on /pricing' })])
      overlay!.onDrawer({ type: LIVE_ACK, reqId: reports()[0].reqId, ok: true })

      // The agent "fixes" and reports ready again; the page must not loop.
      ready('2', { mountAutoReported: true })
      vi.advanceTimersByTime(6500)
      expect(reports()).toHaveLength(1)
      expect(q('.hint')?.textContent).toContain(T.notMounted)
      expect(q('[data-act="reload"]')).not.toBeNull()

      ;(q('[data-act="report-mount"]') as HTMLButtonElement).click()
      expect(reports()).toHaveLength(2)
      expect(reports()[1]).not.toHaveProperty('auto')
      expect(q('[data-act="report-mount"]')).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not auto-report from a hidden tab', () => {
    vi.useFakeTimers()
    const vis = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    try {
      make()
      sessions([{ sid: 'sid009', state: 'ready', mode: 'replace', url: `${location.origin}/pricing`, variants: [{ n: 1 }], updatedAt: '1' }])
      vi.advanceTimersByTime(6500)
      expect(posted.some((m) => m.op === 'mount_failed')).toBe(false)
      vis.mockReturnValue('visible')
      vi.advanceTimersByTime(1500)
      expect(posted.filter((m) => m.op === 'mount_failed')).toHaveLength(1)
    } finally {
      vis.mockRestore()
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
    document.body.innerHTML = '<main id="m"></main>'
    make()
    expect(overlay!.hasCandidates()).toBe(false)
    document.getElementById('m')!.innerHTML = wrapperHtml()
    changed.mockClear()
    sessions([
      { sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] },
      { sid: 'sid002', state: 'ready', mode: 'replace', url: `${location.origin}/about`, variants: [{ n: 1 }] },
    ])
    expect(overlay!.hasCandidates()).toBe(true)
    expect(changed).toHaveBeenCalled()
    expect(q('.dock .hint')?.textContent).toContain('/about')
    expect(q('[data-input="steer"]')).toBeNull()
    overlay!.setPickMode(true)
    expect(overlay!.isPickMode()).toBe(true)
    expect(q('.pickbar .pickhint')?.textContent).toBe(T.pickHint)
    expect(q('[data-act="mode-select"]')?.getAttribute('aria-pressed')).toBe('true')
    const steer = q('[data-input="steer"]') as HTMLInputElement
    steer.focus()
    steer.value = '整体再紧凑一些'
    steer.dispatchEvent(new Event('input', { bubbles: true }))
    steer.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(posted.at(-1)).toMatchObject({ op: 'steer', prompt: '整体再紧凑一些' })
    expect(stopPick).toHaveBeenCalled()
    expect(overlay!.isPickMode()).toBe(false)
    expect(q('[data-input="steer"]')).toBeNull()
    overlay!.setPeek(true)
    const orig = document.querySelector<HTMLElement>('[data-grasp-variant="0"]')!
    expect(orig.hidden).toBe(false)
    overlay!.setPeek(false)
    expect(orig.hidden).toBe(true)
    overlay!.toggleHidden()
    expect(overlay!.isHidden()).toBe(true)
    expect(q('[data-sw="sid001"]')).toBeNull()
    // Esc still closes a card while candidates are hidden.
    overlay!.offer(document.querySelector('main')!)
    expect(q('.panel.choose')).not.toBeNull()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(q('.panel')).toBeNull()
    overlay!.toggleHidden()
    expect(q('[data-sw="sid001"]')).not.toBeNull()
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

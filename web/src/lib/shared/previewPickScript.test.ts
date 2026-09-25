import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { Window } from 'happy-dom'
import { afterEach, describe, expect, it } from 'vitest'
import {
  EMBED_CMD_MESSAGE,
  EMBED_CMD_RESULT_MESSAGE,
  EMBED_CONTROL_MESSAGE,
  EMBED_PICK_MESSAGE,
  EMBED_READY_MESSAGE,
  EMBED_THEME_MESSAGE,
  PAGE_CONTROL_CAP,
} from '@/lib/inbox/embedChat'
import { PICK_HTML_MAX, PICK_TEXT_MAX } from './previewPickUrl'

const repo = resolve(__dirname, '../../../..')
const COPIES = [
  'web/public/preview-pick.js',
  'server/internal/handlers/preview-pick.js',
  'sandbox-gateway/sandbox/internal/previewinject/preview-pick.js',
]
const SCRIPT = readFileSync(resolve(repo, COPIES[0]), 'utf8')
const GRASP = 'https://grasp.example'

type Msg = Record<string, unknown> & { type: string }
type Page = {
  win: Window
  shadow: ShadowRoot
  fetched: string[]
  toggle: () => void
  click: (selector: string) => void
  listItems: () => string[]
  frame: () => HTMLIFrameElement | null
  chatButton: () => HTMLButtonElement
  drawerOpen: () => boolean
  /** Messages the drawer iframe received, after it reports ready. */
  drawerReady: (origin?: string) => Msg[]
}

const opened: Window[] = []
const channels: BroadcastChannel[] = []

/** Node's BroadcastChannel, shared by every test window, closed after each test. */
class TestChannel extends BroadcastChannel {
  constructor(name: string) {
    super(name)
    ;(this as unknown as { unref?: () => void }).unref?.()
    channels.push(this)
  }
}

type EmbedReply = { origin: string; runId: string; nodeId: string } | null

function openPage(
  body: string,
  opts: {
    stored?: string | null
    hash?: string
    embedReply?: EmbedReply
    savedEmbed?: string
    tab?: string
    broadcast?: boolean
  } = {},
): Page {
  const win = new Window({
    url: `http://10.0.0.5:5173/pricing${opts.hash || ''}`,
    settings: { navigator: { userAgent: 'test' }, disableIframePageLoading: true, disableJavaScriptFileLoading: true },
  })
  opened.push(win)
  win.document.body.innerHTML = body
  if (opts.stored) win.sessionStorage.setItem('__grasp_preview_picks', opts.stored)
  if (opts.savedEmbed) win.sessionStorage.setItem('__grasp_embed', opts.savedEmbed)
  if (opts.tab) win.sessionStorage.setItem('__grasp_tab', opts.tab)
  if (opts.broadcast) (win as unknown as Record<string, unknown>).BroadcastChannel = TestChannel
  const fetched: string[] = []
  ;(win as unknown as { fetch: (u: string) => Promise<unknown> }).fetch = async (u: string) => {
    fetched.push(u)
    const reply = opts.embedReply
    return { ok: !!reply, json: async () => reply }
  }
  win.eval(SCRIPT)
  const host = win.document.querySelector('grasp-preview-pick')
  if (!host?.shadowRoot) throw new Error('pick bar not mounted')
  const shadow = host.shadowRoot as unknown as ShadowRoot
  const inbox: Msg[] = []
  const frame = () => shadow.querySelector('iframe') as HTMLIFrameElement | null
  return {
    win,
    shadow,
    fetched,
    toggle() {
      ;(shadow.querySelector('[data-role="toggle"]') as HTMLButtonElement).click()
    },
    click(selector) {
      ;(win.document.querySelector(selector) as unknown as HTMLElement).click()
    },
    listItems() {
      return Array.from(shadow.querySelectorAll('[data-role="list"] code')).map((c) => c.textContent || '')
    },
    frame,
    chatButton: () => shadow.querySelector('[data-role="chat"]') as HTMLButtonElement,
    drawerOpen: () => !(shadow.querySelector('[data-role="drawer"]') as HTMLElement).hidden,
    drawerReady(origin = GRASP) {
      const f = frame()
      if (!f) throw new Error('no drawer')
      const fake = { postMessage: (m: Msg, target: string) => void inbox.push({ ...m, target }) }
      Object.defineProperty(f, 'contentWindow', { value: fake, configurable: true })
      win.dispatchEvent(
        new win.MessageEvent('message', { data: { type: EMBED_READY_MESSAGE }, origin, source: fake as never }),
      )
      return inbox
    },
  }
}

const settle = () => new Promise((r) => setTimeout(r, 0))

afterEach(async () => {
  for (const c of channels.splice(0)) c.close()
  for (const w of opened.splice(0)) await w.happyDOM.close()
})

describe('preview-pick.js copies', () => {
  it('sandbox injector, server and web serve the same bytes', () => {
    for (const rel of COPIES.slice(1)) {
      expect(readFileSync(resolve(repo, rel), 'utf8'), rel).toBe(SCRIPT)
    }
  })
})

describe('preview-pick.js standalone window', () => {
  const body = '<main><h2 class="t">Choose   your\n plan</h2><p>a</p><p>b</p><a id="go" href="/about">about</a></main>'

  it('mounts its own Pick bar and stages several picks without leaving the page', () => {
    const p = openPage(body)
    p.toggle()
    p.click('h2')
    p.click('main > p:nth-of-type(2)')
    p.click('#go')

    expect(p.listItems()).toEqual([
      'h2 · Choose your plan',
      'p · b',
      'a · about',
    ])
    expect(p.win.location.pathname).toBe('/pricing')
  })

  it('removes a staged pick and ignores duplicates', () => {
    const p = openPage(body)
    p.toggle()
    p.click('h2')
    p.click('h2')
    p.click('#go')
    expect(p.listItems()).toHaveLength(2)
    ;(p.shadow.querySelector('button[data-index="0"]') as HTMLButtonElement).click()
    expect(p.listItems()).toEqual(['a · about'])
  })

  it('keeps staged picks across a full page load in the same tab', () => {
    const first = openPage(body)
    first.toggle()
    first.click('#go')
    const stored = first.win.sessionStorage.getItem('__grasp_preview_picks')
    expect(stored).toContain('"selector":"#go"')

    const next = openPage(body, { stored })
    expect(next.listItems()).toEqual(['a · about'])
    ;(next.shadow.querySelector('button[data-index="0"]') as HTMLButtonElement).click()
    expect(next.win.sessionStorage.getItem('__grasp_preview_picks')).toBeNull()
  })

  it('ignores corrupt stored picks', () => {
    const p = openPage(body, { stored: '{not json' })
    expect(p.listItems()).toEqual([])
  })

  it('caps staged picks and says so', () => {
    const many = Array.from({ length: 25 }, (_, i) => `<p id="p${i}">${i}</p>`).join('')
    const p = openPage(many)
    p.toggle()
    for (let i = 0; i < 25; i++) p.click(`#p${i}`)
    expect(p.listItems()).toHaveLength(20)
    const notice = p.shadow.querySelector('[data-role="notice"]') as HTMLElement
    expect(notice.hidden).toBe(false)
    expect(notice.textContent).toMatch(/20/)
  })

  it('does not treat clicks on the bar itself as picks', () => {
    const p = openPage(body)
    p.toggle()
    ;(p.win.document.querySelector('grasp-preview-pick') as unknown as HTMLElement).click()
    expect(p.listItems()).toHaveLength(0)
  })

  it('Escape leaves pick mode', () => {
    const p = openPage(body)
    p.toggle()
    p.win.document.dispatchEvent(new p.win.KeyboardEvent('keydown', { key: 'Escape' }))
    p.click('h2')
    expect(p.listItems()).toHaveLength(0)
    expect(p.shadow.querySelector('[data-role="toggle"]')?.getAttribute('aria-pressed')).toBe('false')
  })

  it('clips visible text and outerHTML', () => {
    const long = 'x'.repeat(PICK_HTML_MAX + 200)
    const p = openPage(`<div id="big">${long}</div>`)
    p.toggle()
    p.click('#big')
    const [item] = JSON.parse(p.win.sessionStorage.getItem('__grasp_preview_picks') || '[]')
    expect(item.text).toBe('x'.repeat(PICK_TEXT_MAX) + '…')
    expect((item.outerHTML as string).length).toBe(PICK_HTML_MAX + 1)
  })
})

describe('preview-pick.js chat drawer', () => {
  const body = '<main><h2>Plan</h2><button id="buy">Buy</button></main>'
  const hash = '#__grasp_embed&run=run-1&node=ap1&ticket=tk1'
  const reply = { origin: GRASP, runId: 'run-1', nodeId: 'ap1' }

  it('has no chat without a ticket or a saved drawer', async () => {
    const p = openPage(body)
    await settle()
    expect(p.fetched).toEqual([])
    expect(p.chatButton().hidden).toBe(true)
    expect(p.frame()).toBeNull()
  })

  it('checks the ticket with Grasp, strips it from the URL and opens the drawer', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    expect(p.win.location.hash).toBe('')
    await settle()
    expect(p.fetched).toEqual(['/__grasp/embed-origin?ticket=tk1&node=ap1'])
    expect(p.frame()?.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/chat#ticket=tk1&theme=dark`)
    expect(p.chatButton().hidden).toBe(false)
    expect(p.drawerOpen()).toBe(true)
    expect(JSON.parse(p.win.sessionStorage.getItem('__grasp_embed') || '{}')).toMatchObject({ origin: GRASP, run: 'run-1', node: 'ap1' })
    expect(p.win.sessionStorage.getItem('__grasp_embed')).not.toContain('tk1')
  })

  it('stays without a drawer when Grasp does not vouch for the ticket', async () => {
    for (const bad of [null, { ...reply, runId: 'run-2' }, { ...reply, origin: 'https://grasp.example/x' }]) {
      const p = openPage(body, { hash, embedReply: bad })
      await settle()
      expect(p.frame(), JSON.stringify(bad)).toBeNull()
      expect(p.win.sessionStorage.getItem('__grasp_embed')).toBeNull()
    }
  })

  it('reopens the saved drawer on a later page load without a ticket', async () => {
    const saved = JSON.stringify({ origin: GRASP, run: 'run-1', node: 'ap1', open: false })
    const p = openPage(body, { savedEmbed: saved })
    await settle()
    expect(p.fetched).toEqual([])
    expect(p.frame()?.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/chat#theme=dark`)
    expect(p.drawerOpen()).toBe(false)
    p.chatButton().click()
    expect(p.drawerOpen()).toBe(true)
    expect(JSON.parse(p.win.sessionStorage.getItem('__grasp_embed') || '{}').open).toBe(true)
  })

  it('holds picks until the drawer is ready, then sends them to the Grasp origin only', async () => {
    const stored = JSON.stringify([{ selector: '#old', tagName: 'a', text: 'old', outerHTML: '<a>', url: 'http://x/' }])
    const p = openPage(body, { hash, embedReply: reply, stored })
    await settle()
    expect(p.win.sessionStorage.getItem('__grasp_preview_picks')).toBeNull()
    p.toggle()
    p.click('#buy')
    expect(p.listItems()).toEqual([])

    expect(p.drawerReady('https://evil.example')).toEqual([])
    const inbox = p.drawerReady()
    expect(inbox.map((m) => [m.type, (m.payload as { selector: string } | undefined)?.selector, m.target])).toEqual([
      [EMBED_THEME_MESSAGE, undefined, GRASP],
      [EMBED_PICK_MESSAGE, '#old', GRASP],
      [EMBED_PICK_MESSAGE, '#buy', GRASP],
    ])
    p.click('h2')
    expect(inbox.at(-1)?.payload).toEqual({
      selector: 'body > main > h2',
      tagName: 'h2',
      text: 'Plan',
      outerHTML: '<h2>Plan</h2>',
      url: 'http://10.0.0.5:5173/pricing',
    })
    const notice = p.shadow.querySelector('[data-role="notice"]') as HTMLElement
    expect(notice.textContent).toBe('Added to the Grasp chat')
  })

  it('starts in the Grasp theme and switches the drawer and the chat together', async () => {
    const p = openPage(body, { hash: `${hash}&theme=light`, embedReply: reply })
    await settle()
    const drawerEl = p.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    const themeBtn = p.shadow.querySelector('[data-role="drawer-theme"]') as HTMLButtonElement
    expect(p.frame()?.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/chat#ticket=tk1&theme=light`)
    expect(drawerEl.classList.contains('light')).toBe(true)
    expect(themeBtn.getAttribute('aria-label')).toBe('Switch to dark')
    const inbox = p.drawerReady()
    expect(inbox).toEqual([{ type: EMBED_THEME_MESSAGE, theme: 'light', target: GRASP }])

    themeBtn.click()
    expect(drawerEl.classList.contains('light')).toBe(false)
    expect(themeBtn.getAttribute('aria-label')).toBe('Switch to light')
    expect(inbox.at(-1)).toEqual({ type: EMBED_THEME_MESSAGE, theme: 'dark', target: GRASP })
    expect(JSON.parse(p.win.sessionStorage.getItem('__grasp_embed') || '{}').theme).toBe('dark')
  })
})

describe('preview-pick.js floating chat window', () => {
  const body = '<main><h2>Plan</h2><button id="buy">Buy</button></main>'
  const hash = '#__grasp_embed&run=run-1&node=ap1&ticket=tk1'
  const reply = { origin: GRASP, runId: 'run-1', nodeId: 'ap1' }
  const savedBase = { origin: GRASP, run: 'run-1', node: 'ap1', open: true, theme: 'dark' }

  function geom(p: Page) {
    const el = p.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    return {
      el,
      x: Number.parseInt(el.style.left, 10),
      y: Number.parseInt(el.style.top, 10),
      w: Number.parseInt(el.style.width, 10),
      h: Number.parseInt(el.style.height, 10),
    }
  }

  function view(p: Page) {
    const de = p.win.document.documentElement
    const win = p.win as unknown as { innerWidth: number; innerHeight: number }
    return {
      vw: de.clientWidth || win.innerWidth,
      vh: de.clientHeight || win.innerHeight,
    }
  }

  function fire(p: Page, type: string, target: EventTarget, x: number, y: number) {
    const Ev = (p.win as unknown as { PointerEvent: typeof PointerEvent }).PointerEvent
    target.dispatchEvent(
      new Ev(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, pointerId: 1, button: 0 }),
    )
  }

  function drag(p: Page, target: EventTarget, from: { x: number; y: number }, to: { x: number; y: number }) {
    fire(p, 'pointerdown', target, from.x, from.y)
    fire(p, 'pointermove', p.win, to.x, to.y)
    fire(p, 'pointerup', p.win, to.x, to.y)
  }

  it('opens a rounded Grasp card on the right at the default size', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const { vw, vh } = view(p)
    expect(vw).toBeGreaterThan(420)
    expect(vh).toBeGreaterThan(400)
    const g = geom(p)
    const css = p.shadow.querySelector('style')?.textContent || ''
    expect(p.shadow.querySelector('[data-role="drawer-title"]')?.textContent).toBe('Grasp')
    expect(css).toContain('border-radius:22px')
    expect(g.w).toBe(420)
    expect(g.h).toBe(Math.round(vh * 0.7))
    expect(g.x).toBe(vw - 420 - 28)
    expect(g.y).toBe(72)
    expect(g.x + g.w).toBeLessThanOrEqual(vw)
    expect(g.y + g.h).toBeLessThanOrEqual(vh)
    expect(p.drawerOpen()).toBe(true)
  })

  it('drags from the title bar and keeps the window inside the viewport', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const head = p.shadow.querySelector('[data-role="drawer-head"]') as HTMLElement
    const before = geom(p)
    drag(p, head, { x: 800, y: 100 }, { x: 700, y: 160 })
    const moved = geom(p)
    expect(moved.x).toBe(before.x - 100)
    expect(moved.y).toBe(before.y + 60)
    expect(moved.w).toBe(before.w)
    expect(moved.h).toBe(before.h)

    drag(p, head, { x: 0, y: 0 }, { x: -5000, y: -5000 })
    const pinned = geom(p)
    expect(pinned.x).toBe(0)
    expect(pinned.y).toBe(0)

    const { vw, vh } = view(p)
    drag(p, head, { x: 0, y: 0 }, { x: 8000, y: 8000 })
    const far = geom(p)
    expect(far.x).toBe(vw - far.w)
    expect(far.y).toBe(vh - far.h)
    expect(far.el.classList.contains('light')).toBe(false)
    expect(p.drawerOpen()).toBe(true)
  })

  it('does not treat title-bar drags as theme or collapse clicks', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const theme = p.shadow.querySelector('[data-role="drawer-theme"]') as HTMLButtonElement
    const close = p.shadow.querySelector('[data-role="drawer-close"]') as HTMLButtonElement
    const before = geom(p)
    fire(p, 'pointerdown', theme, before.x + before.w - 20, before.y + 32)
    fire(p, 'pointermove', p.win, before.x, before.y + 120)
    fire(p, 'pointerup', p.win, before.x, before.y + 120)
    expect(geom(p)).toMatchObject({ x: before.x, y: before.y, w: before.w, h: before.h })
    fire(p, 'pointerdown', close, before.x + before.w - 20, before.y + 32)
    fire(p, 'pointermove', p.win, 10, 10)
    fire(p, 'pointerup', p.win, 10, 10)
    expect(p.drawerOpen()).toBe(true)
    expect(geom(p).el.classList.contains('light')).toBe(false)
  })

  it('resizes from a corner and stops at 320 by 240', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const se = p.shadow.querySelector('[data-dir="se"]') as HTMLElement
    const west = p.shadow.querySelector('[data-dir="w"]') as HTMLElement
    const north = p.shadow.querySelector('[data-dir="n"]') as HTMLElement
    const before = geom(p)
    drag(p, se, { x: 900, y: 400 }, { x: 880, y: 370 })
    const shrunk = geom(p)
    expect(shrunk.w).toBe(before.w - 20)
    expect(shrunk.h).toBe(before.h - 30)
    expect(shrunk.x).toBe(before.x)
    expect(shrunk.y).toBe(before.y)

    const mid = geom(p)
    drag(p, west, { x: mid.x, y: mid.y + 40 }, { x: mid.x + 5000, y: mid.y + 40 })
    const narrow = geom(p)
    expect(narrow.w).toBe(320)
    expect(narrow.x + narrow.w).toBe(mid.x + mid.w)
    drag(p, north, { x: narrow.x + 40, y: narrow.y }, { x: narrow.x + 40, y: narrow.y + 5000 })
    const short = geom(p)
    expect(short.h).toBe(240)
    expect(short.y + short.h).toBe(narrow.y + narrow.h)
  })

  it('stops an outward resize on the viewport edge without moving the opposite side', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const before = geom(p)
    const { vw, vh } = view(p)
    expect(before.x).toBeGreaterThan(0)
    expect(before.y).toBeGreaterThan(0)
    const east = p.shadow.querySelector('[data-dir="e"]') as HTMLElement
    const south = p.shadow.querySelector('[data-dir="s"]') as HTMLElement
    drag(p, east, { x: before.x + before.w, y: before.y + 40 }, { x: before.x + before.w + 4000, y: before.y + 40 })
    const wide = geom(p)
    expect(wide.x).toBe(before.x)
    expect(wide.y).toBe(before.y)
    expect(wide.w).toBe(vw - before.x)
    expect(wide.h).toBe(before.h)
    expect(wide.x + wide.w).toBeLessThanOrEqual(vw)

    drag(p, south, { x: wide.x + 40, y: wide.y + wide.h }, { x: wide.x + 40, y: wide.y + wide.h + 4000 })
    const tall = geom(p)
    expect(tall.x).toBe(wide.x)
    expect(tall.y).toBe(wide.y)
    expect(tall.w).toBe(wide.w)
    expect(tall.h).toBe(vh - wide.y)
    expect(tall.y + tall.h).toBeLessThanOrEqual(vh)

    const placed = openPage(body, {
      savedEmbed: JSON.stringify({ ...savedBase, x: 120, y: 80, width: 400, height: 360 }),
    })
    await settle()
    const origin = geom(placed)
    const west = placed.shadow.querySelector('[data-dir="w"]') as HTMLElement
    const north = placed.shadow.querySelector('[data-dir="n"]') as HTMLElement
    drag(placed, west, { x: origin.x, y: origin.y + 40 }, { x: origin.x - 4000, y: origin.y + 40 })
    const left = geom(placed)
    expect(left.x).toBe(0)
    expect(left.x + left.w).toBe(origin.x + origin.w)
    expect(left.y).toBe(origin.y)
    expect(left.h).toBe(origin.h)
    drag(placed, north, { x: left.x + 40, y: left.y }, { x: left.x + 40, y: left.y - 4000 })
    const up = geom(placed)
    expect(up.y).toBe(0)
    expect(up.y + up.h).toBe(left.y + left.h)
    expect(up.x).toBe(left.x)
    expect(up.w).toBe(left.w)
  })

  it('keeps resize inside the viewport and follows the pointer across the iframe until release', async () => {
    const saved = JSON.stringify({ ...savedBase, x: 0, y: 0, width: 400, height: 400 })
    const p = openPage(body, { savedEmbed: saved })
    await settle()
    const east = p.shadow.querySelector('[data-dir="e"]') as HTMLElement
    const frame = p.frame()
    expect(frame).not.toBeNull()
    fire(p, 'pointerdown', east, 400, 200)
    expect(frame?.style.pointerEvents).toBe('none')
    fire(p, 'pointermove', p.win, 400 + 20000, 200)
    const { vw, vh } = view(p)
    const grown = geom(p)
    expect(grown.x).toBe(0)
    expect(grown.w).toBe(vw)
    expect(grown.x + grown.w).toBeLessThanOrEqual(vw)
    expect(grown.y + grown.h).toBeLessThanOrEqual(vh)
    fire(p, 'pointerup', p.win, 400 + 20000, 200)
    expect(frame?.style.pointerEvents).toBe('')
    expect(geom(p).w).toBe(vw)
  })

  it('remembers position and size in the tab session and restores them', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const head = p.shadow.querySelector('[data-role="drawer-head"]') as HTMLElement
    const before = geom(p)
    drag(p, head, { x: 500, y: 100 }, { x: 420, y: 140 })
    const placed = geom(p)
    const stored = JSON.parse(p.win.sessionStorage.getItem('__grasp_embed') || '{}')
    expect(stored).toMatchObject({ x: placed.x, y: placed.y, width: placed.w, height: placed.h })
    expect(placed.x).toBe(before.x - 80)
    expect(placed.y).toBe(before.y + 40)

    ;(p.shadow.querySelector('[data-role="drawer-close"]') as HTMLButtonElement).click()
    expect(p.drawerOpen()).toBe(false)
    const afterClose = JSON.parse(p.win.sessionStorage.getItem('__grasp_embed') || '{}')
    expect(afterClose).toMatchObject({ open: false, x: placed.x, y: placed.y, width: placed.w, height: placed.h })

    const again = openPage(body, { savedEmbed: JSON.stringify(afterClose) })
    await settle()
    expect(again.drawerOpen()).toBe(false)
    expect(geom(again)).toMatchObject({ x: placed.x, y: placed.y, w: placed.w, h: placed.h })
    again.chatButton().click()
    expect(again.drawerOpen()).toBe(true)
    expect(geom(again)).toMatchObject({ x: placed.x, y: placed.y, w: placed.w, h: placed.h })
  })

  it('pulls the window into a smaller viewport without overwriting the saved size', async () => {
    const saved = { ...savedBase, x: 100, y: 40, width: 500, height: 400 }
    const p = openPage(body, { savedEmbed: JSON.stringify(saved) })
    await settle()
    expect(geom(p)).toMatchObject({ x: 100, y: 40, w: 500, h: 400 })
    const stored = p.win.sessionStorage.getItem('__grasp_embed')
    const de = p.win.document.documentElement
    Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => 360 })
    Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => 280 })
    p.win.dispatchEvent(new p.win.Event('resize'))
    const shrunk = geom(p)
    expect(shrunk.x).toBe(0)
    expect(shrunk.y).toBe(0)
    expect(shrunk.w).toBe(360)
    expect(shrunk.h).toBe(280)
    expect(p.win.sessionStorage.getItem('__grasp_embed')).toBe(stored)

    Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => 1280 })
    Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => 800 })
    p.win.dispatchEvent(new p.win.Event('resize'))
    expect(geom(p)).toMatchObject({ x: 100, y: 40, w: 500, h: 400 })
    expect(p.win.sessionStorage.getItem('__grasp_embed')).toBe(stored)
  })

  it('uses the default place and size when session fields are missing or illegal', async () => {
    const fresh = openPage(body, { hash, embedReply: reply })
    await settle()
    const defaults = geom(fresh)

    const missing = openPage(body, { savedEmbed: JSON.stringify(savedBase) })
    await settle()
    expect(geom(missing)).toMatchObject({ x: defaults.x, y: defaults.y, w: defaults.w, h: defaults.h })

    const illegal = openPage(body, {
      savedEmbed: JSON.stringify({ ...savedBase, x: '12', y: false, width: 'wide', height: -5 }),
    })
    await settle()
    expect(geom(illegal)).toMatchObject({ x: defaults.x, y: defaults.y, w: defaults.w, h: defaults.h })

    const tiny = openPage(body, {
      savedEmbed: JSON.stringify({ ...savedBase, x: 16, y: 20, width: 100, height: 80 }),
    })
    await settle()
    expect(geom(tiny)).toMatchObject({ x: 16, y: 20, w: 320, h: 240 })
  })
})

describe('preview-pick.js page control', () => {
  const body = '<main><button id="buy">Buy</button></main>'
  const saved = JSON.stringify({ origin: GRASP, run: 'run-1', node: 'ap1', open: true, theme: 'dark' })
  type Run = (cmd: { action: string; args: Record<string, unknown> }, signal?: AbortSignal) => Promise<unknown>

  async function ready(opts: { run?: Run; tab?: string; broadcast?: boolean } = {}) {
    const p = openPage(body, { savedEmbed: saved, tab: opts.tab, broadcast: opts.broadcast })
    const created: { hideOwnUi: (h: boolean) => void }[] = []
    const armed: boolean[] = []
    if (opts.run) {
      const run = opts.run
      ;(p.win as unknown as Record<string, unknown>).__graspPageControl = {
        version: 1,
        create: (o: { hideOwnUi: (h: boolean) => void }) => {
          created.push(o)
          return { run, setArmed: (on: boolean) => armed.push(on) }
        },
      }
    }
    await settle()
    const inbox = p.drawerReady()
    await new Promise((r) => setTimeout(r, 200))
    const send = (data: Msg) =>
      p.win.dispatchEvent(
        new p.win.MessageEvent('message', { data, origin: GRASP, source: p.frame()?.contentWindow as never }),
      )
    const banner = () => p.shadow.querySelector('[data-role="agent"]') as HTMLElement
    return { p, inbox, send, banner, created, armed }
  }

  const results = (inbox: Msg[]) => inbox.filter((m) => m.type === EMBED_CMD_RESULT_MESSAGE)

  it('announces page control with a tab id after the drawer is ready', async () => {
    const { inbox, p } = await ready()
    const hello = inbox.find((m) => m.type === EMBED_CONTROL_MESSAGE)
    expect(hello).toMatchObject({ caps: [PAGE_CONTROL_CAP], target: GRASP })
    expect(hello?.tab).toMatch(/^[0-9a-f]{16}$/)
    expect(p.win.sessionStorage.getItem('__grasp_tab')).toBe(hello?.tab)
  })

  it('keeps the tab id across reloads of the same tab', async () => {
    const { inbox } = await ready({ tab: 'abcdabcdabcdabcd' })
    expect(inbox.find((m) => m.type === EMBED_CONTROL_MESSAGE)?.tab).toBe('abcdabcdabcdabcd')
  })

  it('gives a tab opened from a live tab (copied session) its own id', async () => {
    const first = await ready({ tab: 'abcdabcdabcdabcd', broadcast: true })
    expect(first.inbox.find((m) => m.type === EMBED_CONTROL_MESSAGE)?.tab).toBe('abcdabcdabcdabcd')
    const copy = await ready({ tab: 'abcdabcdabcdabcd', broadcast: true })
    const tab = copy.inbox.find((m) => m.type === EMBED_CONTROL_MESSAGE)?.tab
    expect(tab).toMatch(/^[0-9a-f]{16}$/)
    expect(tab).not.toBe('abcdabcdabcdabcd')
    expect(copy.p.win.sessionStorage.getItem('__grasp_tab')).toBe(tab)
  })

  it('refuses commands until the drawer turns control on', async () => {
    const { inbox, send, banner } = await ready({ run: async () => ({ ok: true }) })
    expect(banner().hidden).toBe(true)
    send({ type: EMBED_CMD_MESSAGE, nonce: 'n1', action: 'state', args: {} })
    await settle()
    expect(results(inbox)).toEqual([
      expect.objectContaining({ nonce: 'n1', ok: false, error: '用户没有开启页面操作' }),
    ])
  })

  it('runs commands through the executor, shows the banner and blocks picking meanwhile', async () => {
    let release: (v: unknown) => void = () => {}
    const seen: unknown[] = []
    const { p, inbox, send, banner, created } = await ready({
      run: (cmd) => {
        seen.push(cmd)
        return new Promise((r) => (release = r))
      },
    })
    send({ type: EMBED_CONTROL_MESSAGE, on: true })
    await settle()
    expect(banner().hidden).toBe(false)
    p.toggle()
    expect(p.shadow.querySelector('[data-role="toggle"]')?.getAttribute('aria-pressed')).toBe('true')

    send({ type: EMBED_CMD_MESSAGE, nonce: 'n2', action: 'click', args: { index: 3, stateId: 's1' } })
    await settle()
    expect(seen).toEqual([{ action: 'click', args: { index: 3, stateId: 's1' } }])
    expect(banner().className).toContain('busy')
    const toggle = p.shadow.querySelector('[data-role="toggle"]') as HTMLButtonElement
    expect(toggle.getAttribute('aria-pressed')).toBe('false')
    expect(toggle.disabled).toBe(true)

    created[0].hideOwnUi(true)
    expect((p.win.document.querySelector('grasp-preview-pick') as unknown as HTMLElement).style.display).toBe('none')
    created[0].hideOwnUi(false)

    release({ ok: true, note: '已点击', state: { stateId: 's2', content: '[0]<button >Buy />' } })
    await settle()
    await settle()
    expect(results(inbox)).toEqual([
      expect.objectContaining({ nonce: 'n2', ok: true, note: '已点击', state: expect.objectContaining({ stateId: 's2' }) }),
    ])
    expect(banner().className).toBe('agent')
    expect(toggle.disabled).toBe(false)
  })

  it('aborts a command on cancel and on stop, and tells the drawer about stop', async () => {
    const signals: AbortSignal[] = []
    const { p, inbox, send, banner } = await ready({
      run: (_cmd, signal) => {
        if (signal) signals.push(signal)
        return new Promise((r) => signal?.addEventListener('abort', () => r({ ok: false, error: '操作已取消' })))
      },
    })
    send({ type: EMBED_CONTROL_MESSAGE, on: true })
    send({ type: EMBED_CMD_MESSAGE, nonce: 'a', action: 'state', args: {} })
    await settle()
    send({ type: EMBED_CMD_MESSAGE, nonce: 'a', action: 'cancel' })
    expect(signals[0].aborted).toBe(true)

    send({ type: EMBED_CMD_MESSAGE, nonce: 'b', action: 'state', args: {} })
    await settle()
    ;(p.shadow.querySelector('[data-role="agent-stop"]') as HTMLButtonElement).click()
    expect(signals[1].aborted).toBe(true)
    expect(banner().hidden).toBe(true)
    expect(inbox).toContainEqual(expect.objectContaining({ type: EMBED_CONTROL_MESSAGE, stop: true }))
  })

  it('shows the agent pointer while control is on and hides it on off and stop', async () => {
    const { p, send, armed } = await ready({ run: async () => ({ ok: true }) })
    send({ type: EMBED_CONTROL_MESSAGE, on: true })
    await settle()
    expect(armed).toEqual([true])
    send({ type: EMBED_CONTROL_MESSAGE, on: false })
    expect(armed).toEqual([true, false])
    send({ type: EMBED_CONTROL_MESSAGE, on: true })
    await settle()
    ;(p.shadow.querySelector('[data-role="agent-stop"]') as HTMLButtonElement).click()
    expect(armed).toEqual([true, false, true, false])
  })

  it('keeps the pointer hidden when control turns off before the executor loads', async () => {
    const { send, armed } = await ready({ run: async () => ({ ok: true }) })
    send({ type: EMBED_CONTROL_MESSAGE, on: true })
    send({ type: EMBED_CONTROL_MESSAGE, on: false })
    await settle()
    expect(armed).toEqual([false])
  })

  it('loads the executor next to itself and reports a blocked load', async () => {
    const { p, inbox, send } = await ready()
    send({ type: EMBED_CONTROL_MESSAGE, on: true })
    const script = p.win.document.querySelector('script[data-grasp-page-control]') as unknown as HTMLScriptElement
    expect(script.getAttribute('src')).toBe('/__grasp/page-control.js')
    send({ type: EMBED_CMD_MESSAGE, nonce: 'c', action: 'state', args: {} })
    script.dispatchEvent(new p.win.Event('error') as unknown as Event)
    await settle()
    await settle()
    expect(results(inbox)).toEqual([expect.objectContaining({ nonce: 'c', ok: false, error: expect.stringContaining('CSP') })])
  })

  it('ignores control messages from other origins', async () => {
    const { p, banner } = await ready({ run: async () => ({ ok: true }) })
    p.win.dispatchEvent(
      new p.win.MessageEvent('message', {
        data: { type: EMBED_CONTROL_MESSAGE, on: true },
        origin: 'https://evil.example',
        source: p.frame()?.contentWindow as never,
      }),
    )
    expect(banner().hidden).toBe(true)
  })
})

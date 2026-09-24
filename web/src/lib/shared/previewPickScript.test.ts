import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { Window } from 'happy-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { EMBED_PICK_MESSAGE, EMBED_READY_MESSAGE, EMBED_THEME_MESSAGE } from '@/lib/inbox/embedChat'
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

type EmbedReply = { origin: string; runId: string; nodeId: string } | null

function openPage(
  body: string,
  opts: { stored?: string | null; hash?: string; embedReply?: EmbedReply; savedEmbed?: string } = {},
): Page {
  const win = new Window({
    url: `http://10.0.0.5:5173/pricing${opts.hash || ''}`,
    settings: { navigator: { userAgent: 'test' }, disableIframePageLoading: true },
  })
  opened.push(win)
  win.document.body.innerHTML = body
  if (opts.stored) win.sessionStorage.setItem('__grasp_preview_picks', opts.stored)
  if (opts.savedEmbed) win.sessionStorage.setItem('__grasp_embed', opts.savedEmbed)
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

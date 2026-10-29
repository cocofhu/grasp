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
  EMBED_SESSION_MESSAGE,
  EMBED_THEME_MESSAGE,
  PAGE_CONTROL_CAP,
} from '@/lib/inbox/embedChat'
import { PICK_HTML_MAX, PICK_TEXT_MAX } from './previewPickUrl'
import { EMBED_LIVE_CONTEXT_REQUEST, EMBED_LIVE_CONTEXT_RESULT } from '../inbox/embedLiveContext'

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
  frame: () => HTMLIFrameElement | null
  chatButton: () => HTMLButtonElement
  artifactButton: () => HTMLButtonElement
  artifactMask: () => HTMLElement
  artifactFrame: () => HTMLIFrameElement | null
  drawerOpen: () => boolean
  artifactOpen: () => boolean
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

type EmbedReply = { origin: string; runId: string; nodeId: string; ticket?: string } | null

function openPage(
  body: string,
  opts: {
    stored?: string | null
    hash?: string
    embedReply?: EmbedReply
    savedEmbed?: string
    tab?: string
    pending?: string
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
  if (opts.savedEmbed) win.localStorage.setItem('__grasp_embed', opts.savedEmbed)
  if (opts.tab) win.sessionStorage.setItem('__grasp_tab', opts.tab)
  if (opts.pending) win.sessionStorage.setItem('__grasp_embed_pending', opts.pending)
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
  const frame = () =>
    shadow.querySelector('[data-role="drawer"] iframe') as HTMLIFrameElement | null
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
    frame,
    chatButton: () => shadow.querySelector('[data-role="chat"]') as HTMLButtonElement,
    artifactButton: () => shadow.querySelector('[data-role="artifact"]') as HTMLButtonElement,
    artifactMask: () => shadow.querySelector('[data-role="artifact-mask"]') as HTMLElement,
    artifactFrame: () => shadow.querySelector('[data-role="artifact-modal"] iframe') as HTMLIFrameElement | null,
    drawerOpen: () => !(shadow.querySelector('[data-role="drawer"]') as HTMLElement).hidden,
    artifactOpen: () => !(shadow.querySelector('[data-role="artifact-mask"]') as HTMLElement).hidden,
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

  it('keeps light chat hover purple with white text above the generic light button hover', () => {
    expect(SCRIPT).toContain(
      '.light .chat:hover{background:linear-gradient(135deg,#7c6cff,#9b6cff);border-color:#a78bfa;color:#fff}',
    )
    expect(SCRIPT).toContain(
      '.light .chat[aria-expanded="true"]:hover{background:linear-gradient(135deg,#5b4bd6,#7c3aed);color:#fff}',
    )
    expect(SCRIPT).toContain('.light .chat{background:linear-gradient(135deg,#6d5dfc,#8b5cf6);border-color:#8b5cf6;color:#fff}')
    expect(SCRIPT).toContain('.light button:hover{background:#f4f4f5}')
    expect(SCRIPT).toContain('.chat:hover{background:linear-gradient(135deg,#7c6cff,#9b6cff);border-color:#a78bfa}')
    expect(SCRIPT).toContain('.light .live:hover{background:#fff;border-color:#c4b5fd;color:#5b21b6}')
  })

  it('puts the chat drawer above the live action card', () => {
    const drawer = SCRIPT.match(/\.drawer\{[^}]*z-index:(\d+)/)
    expect(drawer?.[1]).toBe('2147483647')
    expect(SCRIPT).toContain('live.api.syncTheme')
    expect(SCRIPT).toContain("if (!drawer) return '';")
    expect(SCRIPT).toContain('.live[data-tooltip]::after')
    expect(SCRIPT).toContain('var ICON_EYE_OFF')
    expect(SCRIPT).toContain('点选元素引用或修改，也可以插入区块或整页调整')
    expect(SCRIPT).toContain('Pick an element to quote or edit, insert a block, or adjust the whole page')
  })
})

describe('preview-pick.js without a ticket', () => {
  const body = '<main><h2 class="t">Choose   your\n plan</h2><p>a</p></main>'
  const saved = JSON.stringify({ origin: GRASP, run: 'run-1', node: 'ap1', open: false })

  it('shows Pick and Chat disabled, with a hint to reopen from the preview page', async () => {
    const p = openPage(body)
    await settle()
    const pick = p.shadow.querySelector('[data-role="toggle"]') as HTMLButtonElement
    const gate = p.shadow.querySelector('[data-role="gate"]') as HTMLElement
    const tip = p.shadow.querySelector('[data-role="ticket-tip"]') as HTMLElement
    const hint = 'Reopen from the preview page in Grasp to get a new ticket.'
    expect(pick.hidden).toBe(false)
    expect(pick.disabled).toBe(true)
    expect(p.artifactButton().hidden).toBe(false)
    expect(p.artifactButton().disabled).toBe(true)
    expect(p.chatButton().hidden).toBe(false)
    expect(p.chatButton().disabled).toBe(true)
    expect(gate.title).toBe('')
    expect(gate.getAttribute('aria-describedby')).toBe('grasp-ticket-tip')
    expect(tip.getAttribute('role')).toBe('tooltip')
    expect(tip.textContent).toBe(hint)
    expect(tip.hidden).toBe(true)
    gate.dispatchEvent(new p.win.MouseEvent('mouseenter', { bubbles: true }))
    expect(tip.hidden).toBe(false)
    expect(tip.textContent).toBe(hint)
    expect(tip.className).toBe('tip on')
    gate.dispatchEvent(new p.win.MouseEvent('mouseleave', { bubbles: true }))
    expect(tip.hidden).toBe(true)
    gate.dispatchEvent(new p.win.FocusEvent('focusin', { bubbles: true }))
    expect(tip.hidden).toBe(false)
    gate.dispatchEvent(new p.win.FocusEvent('focusout', { bubbles: true }))
    expect(tip.hidden).toBe(true)
    p.toggle()
    p.click('h2')
    expect(pick.getAttribute('aria-pressed')).toBe('false')
    expect(p.win.sessionStorage.getItem('__grasp_preview_picks')).toBeNull()
  })

  it('greys out both again when the drawer reports its session gone', async () => {
    const p = openPage(body, { savedEmbed: saved })
    await settle()
    const pick = p.shadow.querySelector('[data-role="toggle"]') as HTMLButtonElement
    const gate = p.shadow.querySelector('[data-role="gate"]') as HTMLElement
    const tip = p.shadow.querySelector('[data-role="ticket-tip"]') as HTMLElement
    p.chatButton().click()
    p.drawerReady()
    expect(pick.disabled).toBe(false)
    expect(gate.title).toBe('')
    expect(tip.hidden).toBe(true)
    expect(tip.textContent).toBe('')
    gate.dispatchEvent(new p.win.MouseEvent('mouseenter', { bubbles: true }))
    expect(tip.hidden).toBe(true)
    expect(tip.textContent).toBe('')
    gate.dispatchEvent(new p.win.MouseEvent('mouseleave', { bubbles: true }))
    p.toggle()
    expect(pick.getAttribute('aria-pressed')).toBe('true')

    const f = p.frame()
    p.win.dispatchEvent(
      new p.win.MessageEvent('message', {
        data: { type: EMBED_SESSION_MESSAGE, ok: false },
        origin: GRASP,
        source: f?.contentWindow as never,
      }),
    )
    expect(pick.disabled).toBe(true)
    expect(pick.getAttribute('aria-pressed')).toBe('false')
    expect(p.chatButton().disabled).toBe(true)
    expect(p.drawerOpen()).toBe(false)
    expect(gate.title).toBe('')
    expect(tip.hidden).toBe(true)
    expect(tip.textContent).toMatch(/new ticket/)
    gate.dispatchEvent(new p.win.MouseEvent('mouseenter', { bubbles: true }))
    expect(tip.hidden).toBe(false)
    expect(tip.textContent).toBe('Reopen from the preview page in Grasp to get a new ticket.')
    gate.dispatchEvent(new p.win.MouseEvent('mouseleave', { bubbles: true }))
    expect(tip.hidden).toBe(true)
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
    expect(p.chatButton().hidden).toBe(false)
    expect(p.chatButton().disabled).toBe(true)
    expect(p.frame()).toBeNull()
  })

  it('never asks for a ticket by itself on the bare preview address', async () => {
    const p = openPage(body, { embedReply: { ...reply, ticket: 'tk-x' } })
    await settle()
    expect(p.fetched).toEqual([])
    expect(p.frame()).toBeNull()
    expect(p.chatButton().disabled).toBe(true)
  })

  it('checks the ticket with Grasp, strips it from the URL and opens the drawer', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    expect(p.win.location.hash).toBe('')
    expect(JSON.parse(p.win.sessionStorage.getItem('__grasp_embed_pending') || '{}')).toMatchObject({
      run: 'run-1',
      node: 'ap1',
      ticket: 'tk1',
    })
    await settle()
    expect(p.fetched).toEqual(['/__grasp/embed-origin?ticket=tk1&node=ap1'])
    expect(p.frame()?.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/chat#ticket=tk1&theme=dark`)
    expect(p.chatButton().hidden).toBe(false)
    expect(p.drawerOpen()).toBe(true)
    expect(JSON.parse(p.win.localStorage.getItem('__grasp_embed') || '{}')).toMatchObject({ origin: GRASP, run: 'run-1', node: 'ap1' })
    expect(p.win.localStorage.getItem('__grasp_embed')).not.toContain('tk1')
    expect(p.win.sessionStorage.getItem('__grasp_embed_pending')).toContain('tk1')
    p.drawerReady()
    expect(p.win.sessionStorage.getItem('__grasp_embed_pending')).toBeNull()
  })

  it('finishes the handshake after the preview app navigates to a new page', async () => {
    const first = openPage(body, { hash, embedReply: reply })
    const pending = first.win.sessionStorage.getItem('__grasp_embed_pending')
    expect(first.win.location.hash).toBe('')
    expect(pending).toContain('tk1')
    const next = openPage(body, { pending: pending || '', embedReply: reply })
    await settle()
    expect(next.win.location.hash).toBe('')
    expect(next.fetched).toEqual(['/__grasp/embed-origin?ticket=tk1&node=ap1'])
    expect(next.frame()?.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/chat#ticket=tk1&theme=dark`)
    expect(next.drawerOpen()).toBe(true)
  })

  it('stays without a drawer when Grasp does not vouch for the ticket', async () => {
    for (const bad of [null, { ...reply, runId: 'run-2' }, { ...reply, origin: 'https://grasp.example/x' }]) {
      const p = openPage(body, { hash, embedReply: bad })
      await settle()
      expect(p.frame(), JSON.stringify(bad)).toBeNull()
      expect(p.win.localStorage.getItem('__grasp_embed')).toBeNull()
      expect(p.win.sessionStorage.getItem('__grasp_embed_pending')).toBeNull()
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
    expect(JSON.parse(p.win.localStorage.getItem('__grasp_embed') || '{}').open).toBe(true)
  })

  it('holds picks until the drawer is ready, then sends them to the Grasp origin only', async () => {
    const stored = JSON.stringify([{ selector: '#old', tagName: 'a', text: 'old', outerHTML: '<a>', url: 'http://x/' }])
    const p = openPage(body, { hash, embedReply: reply, stored })
    await settle()
    expect(p.win.sessionStorage.getItem('__grasp_preview_picks')).toBeNull()
    p.toggle()
    p.click('#buy')

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

  it('ignores clicks on the bar, leaves pick mode on Escape and clips long picks', async () => {
    const long = 'x'.repeat(PICK_HTML_MAX + 200)
    const p = openPage(`${body}<div id="big">${long}</div>`, { hash, embedReply: reply })
    await settle()
    const inbox = p.drawerReady()
    const picks = () => inbox.filter((m) => m.type === EMBED_PICK_MESSAGE)
    p.toggle()
    ;(p.win.document.querySelector('grasp-preview-pick') as unknown as HTMLElement).click()
    expect(picks()).toEqual([])
    p.click('#big')
    const item = picks()[0].payload as { text: string; outerHTML: string }
    expect(item.text).toBe('x'.repeat(PICK_TEXT_MAX) + '…')
    expect(item.outerHTML.length).toBe(PICK_HTML_MAX + 1)
    p.win.document.dispatchEvent(new p.win.KeyboardEvent('keydown', { key: 'Escape' }))
    p.click('h2')
    expect(picks()).toHaveLength(1)
    expect(p.shadow.querySelector('[data-role="toggle"]')?.getAttribute('aria-pressed')).toBe('false')
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
    expect(JSON.parse(p.win.localStorage.getItem('__grasp_embed') || '{}').theme).toBe('dark')
  })
})

describe('preview-pick.js language', () => {
  const body = '<main><h2>Plan</h2></main>'
  const reply = { origin: GRASP, runId: 'run-1', nodeId: 'ap1' }
  const toggleText = (p: Page) => (p.shadow.querySelector('[data-role="toggle"]') as HTMLElement).textContent

  it('uses the Grasp language from the ticket fragment over the browser language and keeps it', async () => {
    const p = openPage(body, { hash: '#__grasp_embed&run=run-1&node=ap1&ticket=tk1&lang=zh-CN', embedReply: reply })
    expect(toggleText(p)).toBe('取点')
    await settle()
    expect(JSON.parse(p.win.localStorage.getItem('__grasp_embed') || '{}').lang).toBe('zh-CN')
  })

  it('falls back to the browser language without a Grasp language', () => {
    const p = openPage(body)
    expect(toggleText(p)).toBe('Pick')
  })

  it('switches the toolbar when the drawer reports the Grasp language', async () => {
    const p = openPage(body, { hash: '#__grasp_embed&run=run-1&node=ap1&ticket=tk1', embedReply: reply })
    await settle()
    p.drawerReady()
    expect(toggleText(p)).toBe('Pick')
    p.win.dispatchEvent(
      new p.win.MessageEvent('message', { data: { type: 'grasp-embed:lang', lang: 'zh-CN' }, origin: GRASP, source: p.frame()?.contentWindow as never }),
    )
    expect(toggleText(p)).toBe('取点')
    expect(p.chatButton().textContent).toBe('对话')
    // Other origins cannot switch it.
    p.win.dispatchEvent(new p.win.MessageEvent('message', { data: { type: 'grasp-embed:lang', lang: 'en' }, origin: 'https://evil.example', source: p.frame()?.contentWindow as never }))
    expect(toggleText(p)).toBe('取点')
  })
})

describe('preview-pick.js artifact modal', () => {
  const body = '<main><h2>Plan</h2><button id="buy">Buy</button></main>'
  const hash = '#__grasp_embed&run=run-1&node=ap1&ticket=tk1'
  const reply = { origin: GRASP, runId: 'run-1', nodeId: 'ap1' }

  it('keeps Artifact disabled until the chat drawer session is ready', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    expect(p.artifactButton().disabled).toBe(true)
    expect(p.artifactOpen()).toBe(false)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(false)
    p.drawerReady()
    expect(p.artifactButton().disabled).toBe(false)
  })

  it('opens an artifacts iframe modal without replacing the chat drawer', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    expect(p.drawerOpen()).toBe(true)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    expect(p.artifactButton().getAttribute('aria-expanded')).toBe('true')
    expect(p.artifactFrame()?.getAttribute('src')).toBe(
      `${GRASP}/embed/runs/run-1/nodes/ap1/artifacts#theme=dark`,
    )
    expect(p.drawerOpen()).toBe(true)
    expect(p.frame()?.getAttribute('src')).toContain('/chat#')
  })

  it('closes the artifact modal and leaves Pick/Chat state intact', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    expect(p.drawerOpen()).toBe(true)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    ;(p.shadow.querySelector('[data-role="artifact-close"]') as HTMLButtonElement).click()
    expect(p.artifactOpen()).toBe(false)
    expect(p.artifactButton().getAttribute('aria-expanded')).toBe('false')
    expect(p.drawerOpen()).toBe(true)
    expect(p.chatButton().getAttribute('aria-expanded')).toBe('true')
    const pick = p.shadow.querySelector('[data-role="toggle"]') as HTMLButtonElement
    expect(pick.disabled).toBe(false)
    p.toggle()
    expect(pick.getAttribute('aria-pressed')).toBe('true')
  })

  // plan g1.1 / g3.1: clicking the dimming mask must not hide the Artifacts window
  it('keeps the artifact modal open when the mask outside the window is clicked (g1.1 / g3.1)', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    expect(p.drawerOpen()).toBe(true)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    expect(p.artifactButton().getAttribute('aria-expanded')).toBe('true')
    p.artifactMask().click()
    expect(p.artifactOpen()).toBe(true)
    expect(p.artifactButton().getAttribute('aria-expanded')).toBe('true')
    expect(p.drawerOpen()).toBe(true)
  })

  // plan g1.2 / g3.1: title-bar close and bar toggle still dismiss the window
  it('still closes via title-bar close and artifact bar toggle (g1.2 / g3.1)', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    ;(p.shadow.querySelector('[data-role="artifact-close"]') as HTMLButtonElement).click()
    expect(p.artifactOpen()).toBe(false)
    expect(p.artifactButton().getAttribute('aria-expanded')).toBe('false')

    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(false)
    expect(p.artifactButton().getAttribute('aria-expanded')).toBe('false')
  })

  function artifactHeadColors(p: Page) {
    const head = p.shadow.querySelector('[data-role="artifact-head"]') as HTMLElement
    const title = p.shadow.querySelector('[data-role="artifact-title"]') as HTMLElement
    const close = p.shadow.querySelector('[data-role="artifact-close"]') as HTMLButtonElement
    const cs = (el: Element) => p.win.getComputedStyle(el as unknown as Element)
    return {
      headBg: cs(head).backgroundColor,
      headColor: cs(head).color,
      titleColor: cs(title).color,
      closeColor: cs(close).color,
      maskClass: p.artifactMask().className,
    }
  }

  // plan g2.1 / g3.2: dark theme title bar matches drawer dark surface
  it('uses a dark artifact title bar in dark theme (g2.1 / g3.2)', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    const c = artifactHeadColors(p)
    expect(c.maskClass).toBe('mask')
    // happy-dom returns stylesheet hex as-written
    expect(c.headBg).toBe('#0b0b0c')
    expect(c.headColor).toBe('#e5e7eb')
    expect(c.titleColor).toBe('#e5e7eb')
    expect(c.closeColor).toBe('#9ca3af')
  })

  // plan g2.2 / g3.2: light keeps white bar; live theme switch updates without closing
  it('keeps a light title bar in light theme and updates live on theme toggle (g2.2 / g3.2)', async () => {
    const p = openPage(body, { hash: `${hash}&theme=light`, embedReply: reply })
    await settle()
    p.drawerReady()
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    let c = artifactHeadColors(p)
    expect(c.maskClass).toBe('mask light')
    expect(c.headBg).toBe('#fff')
    expect(c.headColor).toBe('#18181b')
    expect(c.closeColor).toBe('#71717a')

    const themeBtn = p.shadow.querySelector('[data-role="drawer-theme"]') as HTMLButtonElement
    themeBtn.click()
    expect(p.artifactOpen()).toBe(true)
    c = artifactHeadColors(p)
    expect(c.maskClass).toBe('mask')
    expect(c.headBg).toBe('#0b0b0c')
    expect(c.headColor).toBe('#e5e7eb')

    themeBtn.click()
    expect(p.artifactOpen()).toBe(true)
    c = artifactHeadColors(p)
    expect(c.maskClass).toBe('mask light')
    expect(c.headBg).toBe('#fff')
  })

  // plan g3.3: after close, drawer + pick remain usable
  it('after closing artifacts, drawer and pick still work (g3.3)', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(false)
    expect(p.drawerOpen()).toBe(true)
    expect(p.chatButton().getAttribute('aria-expanded')).toBe('true')
    const pick = p.shadow.querySelector('[data-role="toggle"]') as HTMLButtonElement
    expect(pick.disabled).toBe(false)
    p.toggle()
    expect(pick.getAttribute('aria-pressed')).toBe('true')
    p.chatButton().click()
    expect(p.drawerOpen()).toBe(false)
    p.chatButton().click()
    expect(p.drawerOpen()).toBe(true)
  })

  function artifactGeom(p: Page) {
    const el = p.shadow.querySelector('[data-role="artifact-modal"]') as HTMLElement
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

  it('opens at 920×640 top-left on a large viewport (g1.1 / g3.1)', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    const { vw, vh } = view(p)
    expect(vw).toBeGreaterThan(920 + 56)
    expect(vh).toBeGreaterThan(640 + 56)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    const g = artifactGeom(p)
    expect(g.w).toBe(920)
    expect(g.h).toBe(640)
    expect(g.x).toBe(28)
    expect(g.y).toBe(28)
    expect(g.x + g.w).toBeLessThanOrEqual(vw)
    expect(g.y + g.h).toBeLessThanOrEqual(vh)
  })

  it('clamps the default box inside a smaller viewport (g1.2 / g3.2)', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    const de = p.win.document.documentElement
    Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => 800 })
    Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => 500 })
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    const g = artifactGeom(p)
    const { vw, vh } = view(p)
    expect(vw).toBe(800)
    expect(vh).toBe(500)
    expect(g.w).toBeLessThanOrEqual(vw)
    expect(g.h).toBeLessThanOrEqual(vh)
    expect(g.x).toBeGreaterThanOrEqual(0)
    expect(g.y).toBeGreaterThanOrEqual(0)
    expect(g.x + g.w).toBeLessThanOrEqual(vw)
    expect(g.y + g.h).toBeLessThanOrEqual(vh)
  })

  it('drags and enlarges past 920×640, remembering size in-session (g2.1 / g3.3)', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    p.drawerReady()
    const de = p.win.document.documentElement
    Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => 1400 })
    Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => 900 })
    p.artifactButton().click()
    const head = p.shadow.querySelector('[data-role="artifact-head"]') as HTMLElement
    const se = p.shadow.querySelector('[data-role="artifact-modal"] .edge.se') as HTMLElement
    const close = p.shadow.querySelector('[data-role="artifact-close"]') as HTMLButtonElement
    expect(head).not.toBeNull()
    expect(se).not.toBeNull()

    // 920×640 is a default, not a hard cap — enlarge past it.
    const before = artifactGeom(p)
    expect(before.w).toBe(920)
    expect(before.h).toBe(640)
    const rightEdge = before.x + before.w
    const bottomEdge = before.y + before.h
    drag(p, se, { x: rightEdge - 2, y: bottomEdge - 2 }, { x: rightEdge + 98, y: bottomEdge + 78 })
    const resized = artifactGeom(p)
    expect(resized.x).toBe(before.x)
    expect(resized.y).toBe(before.y)
    expect(resized.w).toBe(before.w + 100)
    expect(resized.h).toBe(before.h + 80)
    expect(resized.w).toBeGreaterThan(920)
    expect(resized.h).toBeGreaterThan(640)

    drag(p, head, { x: resized.x + 40, y: resized.y + 10 }, { x: resized.x + 80, y: resized.y + 50 })
    const moved = artifactGeom(p)
    expect(moved.x).toBe(resized.x + 40)
    expect(moved.y).toBe(resized.y + 40)
    expect(moved.w).toBe(resized.w)
    expect(moved.h).toBe(resized.h)

    // Close button must not start a drag.
    const mid = artifactGeom(p)
    fire(p, 'pointerdown', close, mid.x + mid.w - 10, mid.y + 10)
    fire(p, 'pointermove', p.win, mid.x + mid.w + 40, mid.y + 50)
    fire(p, 'pointerup', p.win, mid.x + mid.w + 40, mid.y + 50)
    expect(p.artifactOpen()).toBe(true)
    const afterBtn = artifactGeom(p)
    expect(afterBtn.x).toBe(mid.x)
    expect(afterBtn.y).toBe(mid.y)
    expect(afterBtn.w).toBe(mid.w)
    expect(afterBtn.h).toBe(mid.h)

    close.click()
    expect(p.artifactOpen()).toBe(false)

    p.artifactButton().click()
    const reopened = artifactGeom(p)
    expect(reopened.x).toBe(moved.x)
    expect(reopened.y).toBe(moved.y)
    expect(reopened.w).toBe(moved.w)
    expect(reopened.h).toBe(moved.h)
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

  it('opens a rounded Page Harness CoCo card on the right at the default size', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const { vw, vh } = view(p)
    expect(vw).toBeGreaterThan(420)
    expect(vh).toBeGreaterThan(400)
    const g = geom(p)
    const css = p.shadow.querySelector('style')?.textContent || ''
    expect(p.shadow.querySelector('[data-role="drawer-title"]')?.textContent).toBe('Page Harness CoCo')
    expect(css).toContain('border-radius:22px')
    expect(g.w).toBe(420)
    expect(g.h).toBe(Math.round(vh * 0.7))
    expect(g.x).toBe(vw - 420 - 28)
    expect(g.y).toBe(72)
    expect(g.x + g.w).toBeLessThanOrEqual(vw)
    expect(g.y + g.h).toBeLessThanOrEqual(vh)
    expect(p.drawerOpen()).toBe(true)
  })

  it('renders the title bar as a compact Page Harness CoCo toolbar', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const css = p.shadow.querySelector('style')?.textContent || ''
    const title = p.shadow.querySelector('[data-role="drawer-title"]') as HTMLElement
    const grip = p.shadow.querySelector('[data-role="drawer-grip"]')
    const mark = p.shadow.querySelector('.mark')
    const buttons = p.shadow.querySelectorAll('.dhead button')
    expect(title.textContent).toBe('Page Harness CoCo')
    expect(title.getAttribute('title')).toBe('Page Harness CoCo')
    expect(grip).not.toBeNull()
    const markRule = css.match(/\.dhead \.mark\{([^}]+)\}/)?.[1] || ''
    expect(mark?.textContent ?? '').not.toContain('PH')
    // The injected, dependency-free toolbar must match the canonical Grasp lockup.
    const canonical = p.win.document.createElement('div')
    canonical.innerHTML = readFileSync(resolve(repo, 'web/src/components/shell/BrandCharacterMark.vue'), 'utf8')
      .split('<template>')[1]!.split('</template>')[0]!
    const svg = mark?.querySelector('svg')
    expect(svg?.getAttribute('viewBox')).toBe(canonical.querySelector('svg')?.getAttribute('viewBox'))
    const shapes = (root: Element | null | undefined) => Array.from(root?.querySelectorAll('path,ellipse,circle') || [],
      (part) => [part.tagName, Object.fromEntries(Array.from(part.attributes, (attr) => [attr.name, attr.value]))])
    expect(shapes(svg)).toEqual(shapes(canonical))
    expect(mark?.querySelectorAll('.brand-character__eye')).toHaveLength(2)
    expect(markRule).toContain('width:24px')
    expect(markRule).toContain('height:30px')
    expect(markRule).not.toContain('color:')
    expect(markRule).not.toContain('linear-gradient')
    expect(markRule).not.toContain('#6366f1')
    expect(markRule).not.toContain('#8b5cf6')
    expect(markRule).not.toContain('#fff')
    expect(css).toMatch(/\.drawer\{[^}]*color:#e5e7eb/)
    expect(css).toContain('.drawer.light{color:#18181b')
    const drawer = p.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    expect(drawer.classList.contains('light')).toBe(false)
    expect(p.win.getComputedStyle(mark as Element).color).toBe('#e5e7eb')
    expect(p.win.getComputedStyle(title).color).toBe('#e5e7eb')
    expect(buttons.length).toBe(2)
    expect(css).toContain('.dhead{display:flex;align-items:center;gap:9px;height:48px')
    expect(css).toContain('.dhead [data-role="drawer-title"]{flex:1;min-width:0;font-size:14px;font-weight:600')
    expect(css).toContain('line-height:20px')
    expect(css).toContain('text-overflow:ellipsis')
    expect(css).toContain('.dhead button{width:30px;height:30px')
    expect(css).not.toContain('height:64px')
    expect(css).not.toContain('font-size:28px')
    expect(css).not.toContain('width:36px')
  })

  // g2.1: Page Harness CoCo ink stays inside the light title box; long names still ellipsize.
  it('keeps the g in Page Harness CoCo inside the light title box', async () => {
    const p = openPage(body, { hash: `${hash}&theme=light`, embedReply: reply })
    await settle()
    const drawer = p.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    const title = p.shadow.querySelector('[data-role="drawer-title"]') as HTMLElement
    const css = p.shadow.querySelector('style')?.textContent || ''
    const rule = css.match(/\.dhead \[data-role="drawer-title"\]\{([^}]+)\}/)?.[1] || ''
    expect(drawer.classList.contains('light')).toBe(true)
    const mark = p.shadow.querySelector('.mark') as HTMLElement
    expect(mark.querySelector('svg')).not.toBeNull()
    expect(p.win.getComputedStyle(mark).color).toBe('#18181b')
    expect(p.win.getComputedStyle(title).color).toBe('#18181b')
    expect(title.textContent).toBe('Page Harness CoCo')
    expect(title.getAttribute('title')).toBe('Page Harness CoCo')
    expect(rule).toContain('font-size:14px')
    expect(rule).toContain('font-weight:600')
    expect(rule).toContain('line-height:20px')
    expect(rule).toContain('overflow:hidden')
    expect(rule).toContain('white-space:nowrap')
    expect(rule).toContain('text-overflow:ellipsis')
    expect(css).toContain('.dhead{display:flex;align-items:center;gap:9px;height:48px')

    const fontSize = Number(rule.match(/font-size:(\d+)px/)?.[1])
    const lineHeight = Number(rule.match(/line-height:(\d+)px/)?.[1])
    // At line-height equal to the 14px font size, "Page Harness CoCo" ink was 16.5px
    // and the g descender sat 1px below the title box (root cause). Half-leading
    // grows the line box equally above and below that em square.
    const inkHeight = 16.5
    const descenderPastBox = 1
    const ascenderPastBox = 1.5
    const halfLeading = (lineHeight - fontSize) / 2
    const boxBottom = lineHeight
    const inkBottom = fontSize + descenderPastBox + halfLeading
    expect(lineHeight).toBeGreaterThanOrEqual(inkHeight)
    expect(inkBottom).toBeLessThanOrEqual(boxBottom)
    expect(ascenderPastBox - halfLeading).toBeLessThanOrEqual(0)

    title.textContent = 'Page Harness CoCo with a long harness name that must stay on one line'
    expect(title.textContent.startsWith('Page Harness CoCo')).toBe(true)
    expect(rule).toContain('white-space:nowrap')
    expect(rule).toContain('text-overflow:ellipsis')
    expect(rule).toContain('overflow:hidden')
    expect(css).not.toContain('height:64px')
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
    const se = p.shadow.querySelector('[data-role="drawer"] [data-dir="se"]') as HTMLElement
    const west = p.shadow.querySelector('[data-role="drawer"] [data-dir="w"]') as HTMLElement
    const north = p.shadow.querySelector('[data-role="drawer"] [data-dir="n"]') as HTMLElement
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
    const east = p.shadow.querySelector('[data-role="drawer"] [data-dir="e"]') as HTMLElement
    const south = p.shadow.querySelector('[data-role="drawer"] [data-dir="s"]') as HTMLElement
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
    const west = placed.shadow.querySelector('[data-role="drawer"] [data-dir="w"]') as HTMLElement
    const north = placed.shadow.querySelector('[data-role="drawer"] [data-dir="n"]') as HTMLElement
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
    const east = p.shadow.querySelector('[data-role="drawer"] [data-dir="e"]') as HTMLElement
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
    const stored = JSON.parse(p.win.localStorage.getItem('__grasp_embed') || '{}')
    expect(stored).toMatchObject({ x: placed.x, y: placed.y, width: placed.w, height: placed.h })
    expect(placed.x).toBe(before.x - 80)
    expect(placed.y).toBe(before.y + 40)

    ;(p.shadow.querySelector('[data-role="drawer-close"]') as HTMLButtonElement).click()
    expect(p.drawerOpen()).toBe(false)
    const afterClose = JSON.parse(p.win.localStorage.getItem('__grasp_embed') || '{}')
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
    const stored = p.win.localStorage.getItem('__grasp_embed')
    const de = p.win.document.documentElement
    Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => 360 })
    Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => 280 })
    p.win.dispatchEvent(new p.win.Event('resize'))
    const shrunk = geom(p)
    expect(shrunk.x).toBe(0)
    expect(shrunk.y).toBe(0)
    expect(shrunk.w).toBe(360)
    expect(shrunk.h).toBe(280)
    expect(p.win.localStorage.getItem('__grasp_embed')).toBe(stored)

    Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => 1280 })
    Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => 800 })
    p.win.dispatchEvent(new p.win.Event('resize'))
    expect(geom(p)).toMatchObject({ x: 100, y: 40, w: 500, h: 400 })
    expect(p.win.localStorage.getItem('__grasp_embed')).toBe(stored)
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

describe('preview-pick.js Live overlay hook', () => {
  const body = '<main><section id="card">Dispatch</section></main>'
  const saved = JSON.stringify({ origin: GRASP, run: 'run-1', node: 'ap1', open: true, theme: 'dark' })

  type HostOpts = {
    post: (m: Record<string, unknown>) => boolean
    stopPick: () => void
    startPick: () => void
    sendToChat: (el: Element) => void
    theme: () => string
    isOwnUi: (el: Element) => boolean
    changed: () => void
    lang: string
  }
  type FakeOverlay = {
    received: unknown[]
    enabled: boolean[]
    offered: Element[]
    inserting: boolean
    cancels: number
    pickMode: boolean
    langs: string[]
    candidates: boolean
    hidden: boolean
    peeks: boolean[]
    opts: null | HostOpts
  }

  async function ready(install = true) {
    const p = openPage(body, { savedEmbed: saved })
    const fake: FakeOverlay = {
      received: [],
      enabled: [],
      offered: [],
      inserting: false,
      cancels: 0,
      pickMode: false,
      langs: [],
      candidates: false,
      hidden: false,
      peeks: [],
      opts: null,
    }
    if (install) {
      ;(p.win as unknown as Record<string, unknown>).__graspLiveOverlay = {
        version: 3,
        create: (o: HostOpts) => {
          fake.opts = o
          return {
            onDrawer: (m: unknown) => fake.received.push(m),
            offer: (el: Element) => {
              fake.pickMode = false
              fake.offered.push(el)
            },
            startInsert: () => {
              o.stopPick()
              fake.pickMode = true
              fake.inserting = true
              o.changed()
            },
            cancelPick: () => {
              fake.cancels++
              fake.inserting = false
            },
            isInserting: () => fake.inserting,
            setPickMode: (on: boolean) => {
              fake.pickMode = on
              if (!on) fake.inserting = false
              o.changed()
            },
            isPickMode: () => fake.pickMode,
            setLang: (l: string) => void fake.langs.push(l),
            hasCandidates: () => fake.candidates,
            setPeek: (on: boolean) => fake.peeks.push(on),
            toggleHidden: () => {
              fake.hidden = !fake.hidden
              o.changed()
            },
            isHidden: () => fake.hidden,
            setEnabled: (on: boolean) => fake.enabled.push(on),
          }
        },
      }
    }
    await settle()
    const inbox = p.drawerReady()
    const send = (data: Msg) =>
      p.win.dispatchEvent(new p.win.MessageEvent('message', { data, origin: GRASP, source: p.frame()?.contentWindow as never }))
    const btn = (role: string) => p.shadow.querySelector(`[data-role="${role}"]`) as HTMLButtonElement
    return { p, inbox, send, fake, btn }
  }

  it('stays hidden until the drawer says Live is on', async () => {
    const { btn, fake } = await ready()
    expect(btn('insert')).toBeNull()
    expect(btn('steer')).toBeNull()
    expect(btn('eye').hidden).toBe(true)
    expect(btn('live')).toBeNull()
    expect(fake.opts).toBeNull()
  })

  it('loads the overlay, forwards drawer messages and posts through the drawer', async () => {
    const { p, send, fake, btn, inbox } = await ready()
    send({ type: 'grasp-embed:live-sessions', replace: true, sessions: [] })
    send({ type: 'grasp-embed:live-caps', enabled: true })
    await settle()
    await settle()
    expect(fake.opts).not.toBeNull()
    expect(fake.received).toEqual([{ type: 'grasp-embed:live-sessions', replace: true, sessions: [] }])
    // Insert and whole-page adjustment live in the overlay's pick bar; the eye waits for candidates.
    expect(btn('eye').hidden).toBe(true)
    expect(btn('toggle').title).toContain('quote or edit')
    fake.candidates = true
    fake.opts!.changed()
    expect(btn('eye').hidden).toBe(false)
    expect(btn('eye').innerHTML).toContain('button-icon')
    expect(btn('eye').getAttribute('title')).toBe(null)
    expect(btn('eye').getAttribute('data-tooltip')).toContain('Hold to see the original')
    expect(fake.opts!.post({ type: 'grasp-embed:live', op: 'discard', sid: 'sid001' })).toBe(true)
    expect(inbox.find((m) => m.type === 'grasp-embed:live')).toMatchObject({ op: 'discard', sid: 'sid001', target: GRASP })
    send({ type: 'grasp-embed:live-cmd', sid: 'sid001', cmd: 'goto', variant: 2 })
    expect(fake.received).toHaveLength(2)
    expect(fake.opts!.theme()).toBe('dark')
    expect(fake.opts!.isOwnUi(p.win.document.querySelector('grasp-preview-pick') as unknown as Element)).toBe(true)
    fake.opts!.stopPick()

    send({ type: 'grasp-embed:live-caps', enabled: false })
    expect(fake.enabled).toEqual([false])
    expect(btn('eye').hidden).toBe(true)
    send({ type: 'grasp-embed:live-caps', enabled: true })
    expect(fake.enabled).toEqual([false, true])

    send({ type: EMBED_SESSION_MESSAGE, ok: false })
    expect(fake.enabled).toEqual([false, true, false])
    expect(fake.opts!.post({ type: 'grasp-embed:live', op: 'discard', sid: 'sid001' })).toBe(false)
  })

  it('hands the overlay the Grasp language and forwards later switches', async () => {
    const { send, fake } = await ready()
    send({ type: 'grasp-embed:live-caps', enabled: true })
    await settle()
    expect(fake.opts!.lang).toBe('en')
    send({ type: 'grasp-embed:lang', lang: 'zh-CN' })
    expect(fake.langs).toEqual(['zh-CN'])
  })

  it('injects the overlay script next to preview-pick.js when it is not loaded yet', async () => {
    const { p, send } = await ready(false)
    send({ type: 'grasp-embed:live-caps', enabled: true })
    await settle()
    const s = p.win.document.querySelector('script[data-grasp-live-overlay]') as unknown as HTMLScriptElement | null
    expect(s?.getAttribute('src')).toBe('/__grasp/live-overlay.js')
  })

  it('acknowledges the current page to chat once Live controls load without opening the picker', async () => {
    const { p, send, inbox, fake } = await ready()
    send({ type: 'grasp-embed:live-caps', enabled: true })
    p.win.history.pushState({}, '', '/login?tab=design')
    send({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce: 'chat-start-1' })
    await settle()
    expect(inbox.filter((m) => m.type === EMBED_LIVE_CONTEXT_RESULT)).toEqual([
      { type: EMBED_LIVE_CONTEXT_RESULT, nonce: 'chat-start-1', ok: true, url: 'http://10.0.0.5:5173/login?tab=design', target: GRASP },
    ])
    expect(fake.opts).not.toBeNull()
    expect(fake.offered).toEqual([])
    expect(fake.inserting).toBe(false)
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(false)
  })

  async function liveReady() {
    const r = await ready()
    r.send({ type: 'grasp-embed:live-caps', enabled: true })
    await settle()
    return r
  }

  it('hands a picked element to the Live action card instead of sending it straight to chat', async () => {
    const { p, fake, inbox } = await liveReady()
    p.toggle()
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(true)
    p.click('#card')
    expect(fake.offered).toEqual([p.win.document.getElementById('card')])
    expect(inbox.filter((m) => m.type === EMBED_PICK_MESSAGE)).toEqual([])
    // One pick opens the card and leaves Pick mode.
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(false)
    // The card's "Quote" action uses the ordinary pick path.
    fake.opts!.sendToChat(p.win.document.getElementById('card') as unknown as Element)
    expect(inbox.filter((m) => m.type === EMBED_PICK_MESSAGE)).toEqual([
      expect.objectContaining({ payload: expect.objectContaining({ selector: '#card', tagName: 'section' }), target: GRASP }),
    ])
    expect(p.drawerOpen()).toBe(true)
  })

  it('opens one pick mode that spans select and insert, and drives the eye', async () => {
    const { p, fake, btn } = await liveReady()
    p.toggle()
    expect(fake.pickMode).toBe(true)
    expect(btn('toggle').getAttribute('aria-pressed')).toBe('true')
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(true)
    // The pick bar's insert segment turns off element picking but keeps Pick pressed.
    fake.opts!.stopPick()
    fake.inserting = true
    fake.opts!.changed()
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(false)
    expect(btn('toggle').getAttribute('aria-pressed')).toBe('true')
    // The select segment turns element picking back on.
    fake.opts!.startPick()
    fake.inserting = false
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(true)
    // Pick again leaves pick mode entirely.
    p.toggle()
    expect(fake.pickMode).toBe(false)
    expect(btn('toggle').getAttribute('aria-pressed')).toBe('false')
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(false)
    // Esc leaves pick mode too, also while inserting.
    p.toggle()
    fake.opts!.stopPick()
    fake.inserting = true
    p.win.document.dispatchEvent(new p.win.KeyboardEvent('keydown', { key: 'Escape' }) as unknown as Event)
    expect(fake.pickMode).toBe(false)
    expect(btn('toggle').getAttribute('aria-pressed')).toBe('false')

    fake.candidates = true
    fake.opts!.changed()
    const eye = btn('eye')
    eye.dispatchEvent(new p.win.PointerEvent('pointerdown') as unknown as Event)
    eye.dispatchEvent(new p.win.PointerEvent('pointerup') as unknown as Event)
    expect(fake.peeks).toEqual([true, false])
    eye.click()
    expect(fake.hidden).toBe(true)
    expect(eye.getAttribute('aria-pressed')).toBe('true')
    // plan g2.1: the corner eye is the restore control and shares the hide switch.
    expect(eye.getAttribute('aria-label')).toBe('Show the selection layer')
    eye.click()
    expect(fake.hidden).toBe(false)
    expect(eye.getAttribute('aria-pressed')).toBe('false')
    expect(eye.getAttribute('aria-label')).toBe('Hold to see the original, click to hide the selection layer')
  })

  it('does not expose the page URL to other origins, other frames, or invalid request nonces', async () => {
    const { p, send, inbox } = await ready()
    send({ type: 'grasp-embed:live-caps', enabled: true })
    await settle()
    for (const [origin, source] of [
      ['https://evil.example', p.frame()?.contentWindow],
      [GRASP, p.win],
      [GRASP, null],
    ] as const) {
      p.win.dispatchEvent(new p.win.MessageEvent('message', { data: { type: EMBED_LIVE_CONTEXT_REQUEST, nonce: 'spoofed' }, origin, source: source as never }))
    }
    for (const nonce of [undefined, null, 123, '', 'x'.repeat(65)]) send({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce })
    await settle()
    expect(inbox.filter((m) => m.type === EMBED_LIVE_CONTEXT_RESULT)).toEqual([])
    send({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce: 'x'.repeat(64) })
    await settle()
    expect(inbox.filter((m) => m.type === EMBED_LIVE_CONTEXT_RESULT)).toEqual([
      expect.objectContaining({ nonce: 'x'.repeat(64), ok: true, target: GRASP }),
    ])
  })

  it('refuses chat generation while Live capability is disabled', async () => {
    const { send, inbox, fake } = await ready()
    send({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce: 'disabled' })
    await settle()
    expect(inbox.filter((m) => m.type === EMBED_LIVE_CONTEXT_RESULT)).toEqual([
      { type: EMBED_LIVE_CONTEXT_RESULT, nonce: 'disabled', ok: false, url: undefined, target: GRASP },
    ])
    expect(fake.opts).toBeNull()
  })

  it('reports a failed overlay load to chat so a request cannot silently become ordinary text', async () => {
    const { p, send, inbox } = await ready(false)
    send({ type: 'grasp-embed:live-caps', enabled: true })
    send({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce: 'blocked-load' })
    const script = p.win.document.querySelector('script[data-grasp-live-overlay]')
    expect(script).not.toBeNull()
    script!.dispatchEvent(new p.win.Event('error'))
    await settle()
    expect(inbox.filter((m) => m.type === EMBED_LIVE_CONTEXT_RESULT)).toEqual([
      { type: EMBED_LIVE_CONTEXT_RESULT, nonce: 'blocked-load', ok: false, url: undefined, target: GRASP },
    ])
  })

  it.each([
    { type: 'grasp-embed:live-caps', enabled: false },
    { type: EMBED_SESSION_MESSAGE, ok: false },
  ])('does not acknowledge a pending load after capability or session loss: %j', async (revocation) => {
    const { p, send, inbox } = await ready(false)
    send({ type: 'grasp-embed:live-caps', enabled: true })
    send({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce: 'pending-load' })
    send(revocation)
    ;(p.win as unknown as Record<string, unknown>).__graspLiveOverlay = {
      version: 3,
      create: () => ({ onDrawer: () => {}, setEnabled: () => {}, isPickMode: () => false }),
    }
    const script = p.win.document.querySelector('script[data-grasp-live-overlay]')
    script!.dispatchEvent(new p.win.Event('load'))
    await settle()
    expect(inbox.filter((m) => m.type === EMBED_LIVE_CONTEXT_RESULT)).toEqual([
      { type: EMBED_LIVE_CONTEXT_RESULT, nonce: 'pending-load', ok: false, url: undefined, target: GRASP },
    ])
  })

  it('never picks the Live overlay itself', async () => {
    const { p } = await ready()
    const host = p.win.document.createElement('grasp-live-overlay')
    host.setAttribute('data-grasp-live-overlay', '')
    p.win.document.body.appendChild(host)
    p.toggle()
    ;(host as unknown as HTMLElement).click()
    await settle()
    expect(p.win.document.documentElement.classList.contains('__hp-inspecting')).toBe(true)
  })
})

describe('preview-pick.js image preview window', () => {
  const body = '<main><h2>Plan</h2><button id="buy">Buy</button></main>'
  const hash = '#__grasp_embed&run=run-1&node=ap1&ticket=tk1&lang=zh-CN'
  const reply = { origin: GRASP, runId: 'run-1', nodeId: 'ap1' }
  const secret = 'https://secret.example/pixel.png?token=s3cret'

  function setView(p: Page, w: number, h: number) {
    const de = p.win.document.documentElement
    Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => w })
    Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => h })
    p.win.dispatchEvent(new p.win.Event('resize'))
  }

  function modalGeom(el: HTMLElement) {
    return {
      x: Number.parseInt(el.style.left, 10),
      y: Number.parseInt(el.style.top, 10),
      w: Number.parseInt(el.style.width, 10),
      h: Number.parseInt(el.style.height, 10),
    }
  }

  function imageCloseRect(g: { x: number; y: number; w: number; h: number }) {
    return { x: g.x + g.w - 36, y: g.y + 6, w: 28, h: 28 }
  }

  function rectHits(
    a: { x: number; y: number; w: number; h: number },
    b: { x: number; y: number; w: number; h: number },
  ) {
    return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
  }

  function imageModal(p: Page) {
    return p.shadow.querySelector('[data-role="image-modal"]') as HTMLElement
  }

  function imageShown(p: Page) {
    return !imageModal(p).hidden
  }

  function imageGeom(p: Page) {
    const el = imageModal(p)
    return {
      x: Number.parseInt(el.style.left, 10),
      y: Number.parseInt(el.style.top, 10),
      w: Number.parseInt(el.style.width, 10),
      h: Number.parseInt(el.style.height, 10),
    }
  }

  function fromChat(p: Page, data: Record<string, unknown>, origin = GRASP) {
    const frame = p.frame()
    if (!frame?.contentWindow) throw new Error('no drawer')
    p.win.dispatchEvent(
      new p.win.MessageEvent('message', { data, origin, source: frame.contentWindow as never }),
    )
  }

  function openImage(p: Page, label: string, extra: Record<string, unknown> = {}) {
    fromChat(p, { type: 'grasp-embed:image', action: 'open', label, ...extra })
  }

  async function ready(pHash = hash) {
    const p = openPage(body, { hash: pHash, embedReply: reply })
    await settle()
    p.drawerReady()
    return p
  }

  it('opens beside the chat card, not inside it, and ignores the picture address', async () => {
    const p = await ready()
    setView(p, 1600, 900)
    expect(imageShown(p)).toBe(false)
    openImage(p, 'image.png', { src: secret })
    expect(imageShown(p)).toBe(true)
    const drawer = p.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    const modal = imageModal(p)
    expect(drawer.contains(modal)).toBe(false)
    expect(modal.parentElement).toBe(p.artifactMask())
    expect(p.shadow.querySelectorAll('[data-role="image-modal"]')).toHaveLength(1)
    expect(p.shadow.querySelector('[data-role="image-title"]')?.textContent).toBe('图片预览 · image.png')
    const frame = p.shadow.querySelector('[data-role="image-modal"] iframe') as HTMLIFrameElement
    expect(frame.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/image#theme=dark`)
    expect(frame.getAttribute('src')).not.toContain('secret')
    expect(p.shadow.textContent || '').not.toContain('s3cret')
    expect(p.shadow.textContent || '').not.toContain('secret.example')
    const cs = (el: Element) => p.win.getComputedStyle(el as unknown as Element)
    expect(Number(cs(p.artifactMask()).zIndex)).toBeLessThan(Number(cs(drawer).zIndex))
    expect(p.drawerOpen()).toBe(true)
    const g = imageGeom(p)
    const drawerLeft = 1600 - 420 - 28
    expect(g.x).toBe(28)
    expect(g.y).toBe(28)
    expect(g.w).toBe(920)
    expect(g.h).toBe(640)
    expect(g.x + g.w).toBeLessThanOrEqual(drawerLeft - 16)
    expect(g.y + g.h).toBeLessThanOrEqual(900)
  })

  it('keeps the chat usable: mask clicks do nothing, close leaves the drawer, hiding chat leaves the picture', async () => {
    const p = await ready()
    openImage(p, 'image.png')
    p.artifactMask().click()
    expect(imageShown(p)).toBe(true)
    expect(p.drawerOpen()).toBe(true)
    p.chatButton().click()
    expect(p.drawerOpen()).toBe(false)
    expect(imageShown(p)).toBe(true)
    p.chatButton().click()
    expect(p.drawerOpen()).toBe(true)
    ;(p.shadow.querySelector('[data-role="image-close"]') as HTMLButtonElement).click()
    expect(imageShown(p)).toBe(false)
    expect(p.drawerOpen()).toBe(true)
    expect(p.artifactOpen()).toBe(false)
  })

  it('replaces the same window and leaves the artifact window alone', async () => {
    const p = await ready()
    setView(p, 1600, 900)
    p.artifactButton().click()
    expect(p.artifactOpen()).toBe(true)
    const artifactSrc = p.artifactFrame()?.getAttribute('src')
    openImage(p, 'image.png', { src: secret })
    openImage(p, 'other.png', { src: secret })
    expect(p.shadow.querySelectorAll('[data-role="image-modal"]')).toHaveLength(1)
    expect(p.shadow.querySelector('[data-role="image-title"]')?.textContent).toBe('图片预览 · other.png')
    expect(imageShown(p)).toBe(true)
    expect(p.artifactOpen()).toBe(true)
    expect((p.shadow.querySelector('[data-role="artifact-modal"]') as HTMLElement).hidden).toBe(false)
    expect(p.artifactFrame()?.getAttribute('src')).toBe(artifactSrc)
    expect(p.shadow.querySelector('[data-role="artifact-title"]')?.textContent).toBe('产物')
    const imageFrame = p.shadow.querySelector('[data-role="image-modal"] iframe') as HTMLIFrameElement
    expect(imageFrame.getAttribute('src')).toContain('/image#')
    expect(imageFrame.getAttribute('src')).not.toContain('other.png')
    const artifactModal = p.shadow.querySelector('[data-role="artifact-modal"]') as HTMLElement
    expect(artifactModal.contains(imageModal(p))).toBe(false)
    const g = imageGeom(p)
    expect(g.x).toBe(64)
    expect(g.y).toBe(64)
    expect(modalGeom(artifactModal)).toMatchObject({ x: 28, y: 28, w: 920, h: 640 })
    expect(Number(imageModal(p).style.zIndex)).toBeGreaterThan(Number(artifactModal.style.zIndex))
    ;(p.shadow.querySelector('[data-role="image-close"]') as HTMLButtonElement).click()
    expect(imageShown(p)).toBe(false)
    expect(p.artifactOpen()).toBe(true)
    expect(p.drawerOpen()).toBe(true)
  })

  it('clamps a narrow viewport and still shows the close button', async () => {
    const p = await ready()
    setView(p, 700, 400)
    openImage(p, 'image.png')
    const g = imageGeom(p)
    expect(g.w).toBeLessThanOrEqual(700)
    expect(g.h).toBeLessThanOrEqual(400)
    expect(g.x).toBeGreaterThanOrEqual(0)
    expect(g.y).toBeGreaterThanOrEqual(0)
    expect(g.x + g.w).toBeLessThanOrEqual(700)
    expect(g.y + g.h).toBeLessThanOrEqual(400)
    const close = p.shadow.querySelector('[data-role="image-close"]') as HTMLButtonElement
    expect(close.hidden).toBe(false)
    expect(close.getAttribute('aria-label')).toBe('关闭')
  })

  it('drags and resizes inside the viewport', async () => {
    const p = await ready()
    setView(p, 1600, 900)
    openImage(p, 'image.png')
    const head = p.shadow.querySelector('[data-role="image-head"]') as HTMLElement
    const se = p.shadow.querySelector('[data-role="image-modal"] .edge.se') as HTMLElement
    const before = imageGeom(p)
    const fire = (type: string, target: EventTarget, x: number, y: number) => {
      const Ev = (p.win as unknown as { PointerEvent: typeof PointerEvent }).PointerEvent
      target.dispatchEvent(new Ev(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, pointerId: 1, button: 0 }))
    }
    const drag = (target: EventTarget, from: { x: number; y: number }, to: { x: number; y: number }) => {
      fire('pointerdown', target, from.x, from.y)
      fire('pointermove', p.win, to.x, to.y)
      fire('pointerup', p.win, to.x, to.y)
    }
    drag(head, { x: before.x + 40, y: before.y + 12 }, { x: before.x + 80, y: before.y + 42 })
    const moved = imageGeom(p)
    expect(moved.x).toBe(before.x + 40)
    expect(moved.y).toBe(before.y + 30)
    expect(moved.w).toBe(before.w)
    drag(se, { x: moved.x + moved.w - 2, y: moved.y + moved.h - 2 }, { x: moved.x + moved.w + 40, y: moved.y + moved.h + 20 })
    const resized = imageGeom(p)
    expect(resized.w).toBe(moved.w + 42)
    expect(resized.h).toBe(moved.h + 22)
    expect(resized.x + resized.w).toBeLessThanOrEqual(1600)
    expect(resized.y + resized.h).toBeLessThanOrEqual(900)
  })

  it('offsets the artifact window when the picture is already open', async () => {
    const p = await ready()
    setView(p, 1440, 900)
    openImage(p, 'image.png')
    const imageBefore = imageGeom(p)
    expect(imageBefore).toEqual({ x: 28, y: 28, w: 920, h: 640 })
    p.artifactButton().click()
    const image = imageModal(p)
    const artifact = p.shadow.querySelector('[data-role="artifact-modal"]') as HTMLElement
    const artifactBox = modalGeom(artifact)
    expect(artifact.hidden).toBe(false)
    expect(image.hidden).toBe(false)
    expect(artifactBox).toEqual({ x: 64, y: 64, w: 920, h: 640 })
    expect(imageGeom(p)).toEqual(imageBefore)
    expect(Number(artifact.style.zIndex)).toBeGreaterThan(Number(image.style.zIndex))
    expect(artifactBox.y).toBeGreaterThan(imageBefore.y)
    expect(imageBefore.y + 40).toBeGreaterThan(artifactBox.y)
    expect(p.artifactFrame()?.getAttribute('src')).toContain('/artifacts')
    expect(p.shadow.querySelector('[data-role="artifact-title"]')?.textContent).toBe('产物')
    const drawer = p.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    const cs = (el: Element) => p.win.getComputedStyle(el as unknown as Element)
    expect(Number(cs(drawer).zIndex)).toBeGreaterThan(Number(cs(p.artifactMask()).zIndex))
  })

  it('stops a drag before the close button slides under the chat card', async () => {
    const p = await ready()
    setView(p, 1440, 900)
    openImage(p, 'image.png')
    const drawer = modalGeom(p.shadow.querySelector('[data-role="drawer"]') as HTMLElement)
    expect(drawer.x).toBe(1440 - 420 - 28)
    const before = imageGeom(p)
    const head = p.shadow.querySelector('[data-role="image-head"]') as HTMLElement
    const fire = (type: string, target: EventTarget, x: number, y: number) => {
      const Ev = (p.win as unknown as { PointerEvent: typeof PointerEvent }).PointerEvent
      target.dispatchEvent(new Ev(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, pointerId: 1, button: 0 }))
    }
    const drag = (target: EventTarget, from: { x: number; y: number }, to: { x: number; y: number }) => {
      fire('pointerdown', target, from.x, from.y)
      fire('pointermove', p.win, to.x, to.y)
      fire('pointerup', p.win, to.x, to.y)
    }
    drag(head, { x: before.x + 40, y: before.y + 12 }, { x: before.x + 800, y: before.y + 40 })
    const moved = imageGeom(p)
    const btn = imageCloseRect(moved)
    expect(rectHits(btn, drawer)).toBe(false)
    expect(btn.x + btn.w).toBe(drawer.x)
    expect(moved.w).toBe(before.w)
    expect(moved.h).toBe(before.h)
    expect(moved.y).toBe(before.y + 28)
    expect(moved.x).toBeGreaterThan(before.x)
  })

  it('follows the chat card theme and language without closing', async () => {
    const p = await ready('#__grasp_embed&run=run-1&node=ap1&ticket=tk1&theme=light')
    openImage(p, 'image.png')
    const head = p.shadow.querySelector('[data-role="image-head"]') as HTMLElement
    const cs = () => p.win.getComputedStyle(head as unknown as Element)
    expect(p.artifactMask().className).toBe('mask light')
    expect(cs().backgroundColor).toBe('#fff')
    expect(p.shadow.querySelector('[data-role="image-title"]')?.textContent).toBe('Image preview · image.png')
    ;(p.shadow.querySelector('[data-role="drawer-theme"]') as HTMLButtonElement).click()
    expect(imageShown(p)).toBe(true)
    expect(p.artifactMask().className).toBe('mask')
    expect(cs().backgroundColor).toBe('#0b0b0c')
    fromChat(p, { type: 'grasp-embed:lang', lang: 'zh-CN' })
    expect(imageShown(p)).toBe(true)
    expect(p.shadow.querySelector('[data-role="image-title"]')?.textContent).toBe('图片预览 · image.png')
    expect(p.drawerOpen()).toBe(true)
  })

  it('ignores an image request until the chat session is ready, and closes with the session', async () => {
    const p = openPage(body, { hash, embedReply: reply })
    await settle()
    const frame = p.frame()
    expect(frame).toBeTruthy()
    p.win.dispatchEvent(
      new p.win.MessageEvent('message', {
        data: { type: 'grasp-embed:image', action: 'open', label: 'image.png' },
        origin: GRASP,
        source: frame?.contentWindow as never,
      }),
    )
    expect(imageShown(p)).toBe(false)
    p.drawerReady()
    openImage(p, 'image.png')
    expect(imageShown(p)).toBe(true)
    fromChat(p, { type: 'grasp-embed:session', ok: false })
    expect(imageShown(p)).toBe(false)
    expect(p.drawerOpen()).toBe(false)
  })

  function demoModal(p: Page) {
    return p.shadow.querySelector('[data-role="demo-modal"]') as HTMLElement
  }

  function openDemo(p: Page, label: string, extra: Record<string, unknown> = {}) {
    fromChat(p, { type: 'grasp-embed:demo', action: 'open', label, ...extra })
  }

  it('opens one demo window outside the chat card and drops the HTML', async () => {
    const p = await ready()
    setView(p, 1600, 900)
    expect(demoModal(p).hidden).toBe(true)
    openDemo(p, '方案 B', { html: '<p>secret-demo</p>' })
    const drawer = p.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    const modal = demoModal(p)
    expect(modal.hidden).toBe(false)
    expect(drawer.contains(modal)).toBe(false)
    expect(modal.parentElement).toBe(p.artifactMask())
    const box = modalGeom(modal)
    const card = modalGeom(drawer)
    expect(box.x + box.w).toBeLessThanOrEqual(card.x)
    expect(rectHits(box, card)).toBe(false)
    expect(p.shadow.querySelector('[data-role="demo-title"]')?.textContent).toBe('窗口放大查看 · 方案 B')
    expect(p.shadow.textContent || '').not.toContain('secret-demo')
    const frame = modal.querySelector('iframe') as HTMLIFrameElement
    expect(frame.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/demo#theme=dark`)
    expect(frame.getAttribute('src')).not.toContain('secret')
    openDemo(p, '方案 C', { html: '<p>other-secret</p>' })
    expect(p.shadow.querySelectorAll('[data-role="demo-modal"]')).toHaveLength(1)
    expect(p.shadow.querySelector('[data-role="demo-title"]')?.textContent).toBe('窗口放大查看 · 方案 C')
    expect(rectHits(modalGeom(demoModal(p)), modalGeom(drawer))).toBe(false)
    expect(p.drawerOpen()).toBe(true)
  })

  it('closes the demo window with the chat card and leaves an image window open', async () => {
    const p = await ready()
    setView(p, 1600, 900)
    openImage(p, 'image.png')
    openDemo(p, '方案 A')
    expect(imageShown(p)).toBe(true)
    expect(demoModal(p).hidden).toBe(false)
    ;(p.shadow.querySelector('[data-role="drawer-close"]') as HTMLButtonElement).click()
    expect(p.drawerOpen()).toBe(false)
    expect(demoModal(p).hidden).toBe(true)
    expect(imageShown(p)).toBe(true)
    p.chatButton().click()
    expect(p.drawerOpen()).toBe(true)
    expect(demoModal(p).hidden).toBe(true)
    ;(p.shadow.querySelector('[data-role="demo-close"]') as HTMLButtonElement).click()
    expect(p.drawerOpen()).toBe(true)
  })
})

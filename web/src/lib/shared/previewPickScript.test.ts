import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { Window } from 'happy-dom'
import { afterEach, describe, expect, it } from 'vitest'
import {
  DIRECT_PREVIEW_HOST,
  DIRECT_PREVIEW_INSPECT,
  DIRECT_PREVIEW_INSPECT_STATE,
  DIRECT_PREVIEW_PICKED,
} from './directPreviewPick'
import { PICK_HTML_MAX, PICK_TEXT_MAX } from './previewPickUrl'

const repo = resolve(__dirname, '../../../..')
const COPIES = [
  'web/public/preview-pick.js',
  'server/internal/handlers/preview-pick.js',
  'sandbox-gateway/sandbox/internal/previewinject/preview-pick.js',
]
const SCRIPT = readFileSync(resolve(repo, COPIES[0]), 'utf8')

type Msg = Record<string, unknown> & { type: string }
type Page = {
  win: Window
  sent: Msg[]
  parent: { postMessage: (m: Msg) => void }
  shadow: ShadowRoot
  fromParent: (data: Msg, source?: unknown) => void
  toggle: () => void
  click: (selector: string) => void
  listItems: () => string[]
}

const opened: Window[] = []

function openPage(body: string, opts: { embeddedFrame?: boolean; stored?: string | null } = {}): Page {
  const win = new Window({ url: 'http://10.0.0.5:5173/pricing', settings: { navigator: { userAgent: 'test' } } })
  opened.push(win)
  win.document.body.innerHTML = body
  if (opts.stored) win.sessionStorage.setItem('__grasp_preview_picks', opts.stored)
  const sent: Msg[] = []
  const parent = { postMessage: (m: Msg) => void sent.push(m) }
  if (opts.embeddedFrame !== false) Object.defineProperty(win, 'parent', { value: parent, configurable: true })
  win.eval(SCRIPT)
  const host = win.document.querySelector('grasp-preview-pick')
  if (!host?.shadowRoot) throw new Error('pick bar not mounted')
  const shadow = host.shadowRoot as unknown as ShadowRoot
  return {
    win,
    sent,
    parent,
    shadow,
    fromParent(data, source = parent) {
      win.dispatchEvent(new win.MessageEvent('message', { data, source: source as never }))
    },
    toggle() {
      ;(shadow.querySelector('[data-role="toggle"]') as HTMLButtonElement).click()
    },
    click(selector) {
      ;(win.document.querySelector(selector) as unknown as HTMLElement).click()
    },
    listItems() {
      return Array.from(shadow.querySelectorAll('[data-role="list"] code')).map((c) => c.textContent || '')
    },
  }
}

const picks = (p: Page) => p.sent.filter((m) => m.type === DIRECT_PREVIEW_PICKED)

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
    expect(picks(p)).toHaveLength(0)
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

  it('hands stored picks to Grasp and forgets them once embedded', () => {
    const stored = JSON.stringify([{ selector: '#go', tagName: 'a', text: 'about', outerHTML: '<a>', url: 'http://x/' }])
    const p = openPage(body, { stored })
    p.fromParent({ type: DIRECT_PREVIEW_HOST })
    expect(picks(p)).toEqual([expect.objectContaining({ selector: '#go' })])
    expect(p.win.sessionStorage.getItem('__grasp_preview_picks')).toBeNull()
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
    p.fromParent({ type: DIRECT_PREVIEW_HOST })
    p.fromParent({ type: DIRECT_PREVIEW_INSPECT, on: true })
    p.click('#big')
    const [msg] = picks(p)
    expect(msg.text).toBe('x'.repeat(PICK_TEXT_MAX) + '…')
    expect((msg.outerHTML as string).length).toBe(PICK_HTML_MAX + 1)
  })
})

describe('preview-pick.js inside the Grasp frame', () => {
  const body = '<main><h2>Plan</h2><button id="buy">Buy</button></main>'

  it('stays standalone until the parent says hello', () => {
    const p = openPage(body)
    p.toggle()
    p.click('#buy')
    expect(picks(p)).toHaveLength(0)
    expect(p.listItems()).toEqual(['button · Buy'])
  })

  it('ignores a hello that does not come from its parent', () => {
    const p = openPage(body)
    p.fromParent({ type: DIRECT_PREVIEW_HOST }, { postMessage() {} })
    p.toggle()
    p.click('#buy')
    expect(picks(p)).toHaveLength(0)
  })

  it('never embeds when opened as a top-level window', () => {
    const p = openPage(body, { embeddedFrame: false })
    p.fromParent({ type: DIRECT_PREVIEW_HOST }, p.win)
    p.toggle()
    p.click('#buy')
    expect(p.listItems()).toEqual(['button · Buy'])
  })

  it('posts every pick to the parent, keeps pick mode on and keeps no local list', () => {
    const p = openPage(body)
    p.fromParent({ type: DIRECT_PREVIEW_HOST })
    p.toggle()
    p.click('h2')
    p.click('#buy')

    expect(picks(p)).toEqual([
      {
        type: DIRECT_PREVIEW_PICKED,
        selector: 'body > main > h2',
        tagName: 'h2',
        text: 'Plan',
        outerHTML: '<h2>Plan</h2>',
        url: 'http://10.0.0.5:5173/pricing',
      },
      expect.objectContaining({ selector: '#buy', tagName: 'button', text: 'Buy' }),
    ])
    expect(p.listItems()).toHaveLength(0)
    expect(p.sent.filter((m) => m.type === DIRECT_PREVIEW_INSPECT_STATE)).toEqual([
      { type: DIRECT_PREVIEW_INSPECT_STATE, on: true },
    ])
  })

  it('hands picks staged before the hello over to the parent', () => {
    const p = openPage(body)
    p.toggle()
    p.click('#buy')
    p.fromParent({ type: DIRECT_PREVIEW_HOST })
    expect(picks(p)).toEqual([expect.objectContaining({ selector: '#buy' })])
    expect(p.listItems()).toHaveLength(0)
  })

  it('outer inspect toggle drives pick mode without echoing state back', () => {
    const p = openPage(body)
    p.fromParent({ type: DIRECT_PREVIEW_HOST })
    p.fromParent({ type: DIRECT_PREVIEW_INSPECT, on: true })
    p.click('#buy')
    expect(picks(p)).toHaveLength(1)
    expect(p.sent.some((m) => m.type === DIRECT_PREVIEW_INSPECT_STATE)).toBe(false)
  })
})

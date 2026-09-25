// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  MAX_CONTENT_BYTES,
  MAX_ELEMENTS,
  createExecutor,
  scrubPasswords,
  truncateContent,
  type PageControllerLike,
} from './executor'

function fakeController(content = '[0]<button >Buy />\n[1]<input type=password value=hunter2 />') {
  const els = new Map<number, HTMLElement>()
  const calls: string[] = []
  const ok = (message: string) => Promise.resolve({ success: true, message })
  const c: PageControllerLike & { els: typeof els; calls: string[]; content: string } = {
    els,
    calls,
    content,
    getBrowserState: () =>
      Promise.resolve({ url: 'http://app/', title: 'App', header: '[Start of page]', content: c.content, footer: '[End of page]' }),
    cleanUpHighlights: () => Promise.resolve(),
    clickElement: (i) => (calls.push(`click ${i}`), ok('clicked')),
    inputText: (i, t) => (calls.push(`input ${i} ${t}`), ok(`Input text (${t})`)),
    selectOption: (i, o) => (calls.push(`select ${i} ${o}`), ok('selected')),
    scroll: (o) => (calls.push(`scroll ${JSON.stringify(o)}`), ok('scrolled')),
    elementAt: (i) => els.get(i) ?? null,
    dispose: () => {},
  }
  return c
}

function setup(content?: string) {
  document.body.innerHTML = '<button id="b">Buy</button><input id="p" type="password" value="hunter2"><a id="n" target="_blank" href="/x">x</a>'
  const c = fakeController(content)
  c.els.set(0, document.getElementById('b') as HTMLElement)
  c.els.set(1, document.getElementById('p') as HTMLElement)
  c.els.set(2, document.getElementById('n') as HTMLElement)
  const hidden: boolean[] = []
  const ex = createExecutor({ controller: c, settleIdleMs: 1, settleMaxMs: 20, hideOwnUi: (h) => hidden.push(h) })
  return { c, ex, hidden }
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('page-control executor', () => {
  it('returns page state with a fresh stateId and hides its own UI while reading', async () => {
    const { ex, hidden } = setup()
    const a = await ex.run({ action: 'state' })
    const b = await ex.run({ action: 'state' })
    expect(a.ok).toBe(true)
    expect(a.state).toMatchObject({ url: 'http://app/', title: 'App', truncated: false })
    expect(a.state?.content).toContain('[0]<button >Buy />')
    expect(a.state?.stateId).toMatch(new RegExp(`^${ex.pageId}-1$`))
    expect(b.state?.stateId).toBe(`${ex.pageId}-2`)
    expect(hidden.slice(0, 2)).toEqual([true, false])
    expect(hidden.at(-1)).toBe(false)
  })

  it('never reports password values', async () => {
    const { ex } = setup()
    const r = await ex.run({ action: 'state' })
    expect(r.state?.content).not.toContain('hunter2')
    expect(document.getElementById('p')?.getAttribute('value')).toBe('hunter2')
  })

  it('rejects indexes from stale or foreign states', async () => {
    const { ex, c } = setup()
    const none = await ex.run({ action: 'click', args: { index: 0, stateId: 'x-1' } })
    expect(none).toMatchObject({ ok: false, error: expect.stringContaining('page_state') })
    const first = (await ex.run({ action: 'state' })).state!.stateId
    await ex.run({ action: 'state' })
    const stale = await ex.run({ action: 'click', args: { index: 0, stateId: first } })
    expect(stale).toMatchObject({ ok: false, error: expect.stringContaining('已过期') })
    const other = await ex.run({ action: 'click', args: { index: 0, stateId: 'otherpage-9' } })
    expect(other).toMatchObject({ ok: false, error: expect.stringContaining('已失效') })
    expect(c.calls).toEqual([])
  })

  it('clicks, then waits for the page and returns the new state', async () => {
    const { ex, c } = setup()
    const s = (await ex.run({ action: 'state' })).state!.stateId
    const r = await ex.run({ action: 'click', args: { index: 0, stateId: s } })
    expect(c.calls).toEqual(['click 0'])
    expect(r).toMatchObject({ ok: true, note: '已点击' })
    expect(r.state?.stateId).not.toBe(s)
  })

  it('flags links that open a new tab', async () => {
    const { ex } = setup()
    const s = (await ex.run({ action: 'state' })).state!.stateId
    const r = await ex.run({ action: 'click', args: { index: 2, stateId: s } })
    expect(r.note).toContain('新标签页')
  })

  it('reports elements that left the page', async () => {
    const { ex, c } = setup()
    const s = (await ex.run({ action: 'state' })).state!.stateId
    document.getElementById('b')?.remove()
    const r = await ex.run({ action: 'click', args: { index: 0, stateId: s } })
    expect(r).toMatchObject({ ok: false, error: expect.stringContaining('不在页面上') })
    const missing = await ex.run({ action: 'click', args: { index: 9, stateId: s } })
    expect(missing).toMatchObject({ ok: false, error: expect.stringContaining('9') })
    expect(c.calls).toEqual([])
  })

  it('does not echo typed passwords', async () => {
    const { ex } = setup()
    const s = (await ex.run({ action: 'state' })).state!.stateId
    const r = await ex.run({ action: 'input', args: { index: 1, stateId: s, text: 's3cret' } })
    expect(r.ok).toBe(true)
    expect(JSON.stringify(r)).not.toContain('s3cret')
    const s2 = r.state!.stateId
    const t = await ex.run({ action: 'input', args: { index: 0, stateId: s2, text: '你好' } })
    expect(t.note).toBe('已输入 2 个字符')
  })

  it('selects and scrolls', async () => {
    const { ex, c } = setup()
    const s = (await ex.run({ action: 'scroll', args: { down: false, pages: 2 } })).state!.stateId
    const r = await ex.run({ action: 'select', args: { index: 0, stateId: s, option: 'Pro' } })
    expect(r.note).toBe('已选择「Pro」')
    await ex.run({ action: 'scroll', args: { index: 0, stateId: r.state!.stateId, down: true, pages: 1 } })
    expect(c.calls).toEqual([
      'scroll {"down":false,"numPages":2}',
      'select 0 Pro',
      'scroll {"down":true,"numPages":1,"index":0}',
    ])
  })

  it('stops waiting when cancelled', async () => {
    const { ex } = setup()
    const s = (await ex.run({ action: 'state' })).state!.stateId
    const ac = new AbortController()
    const p = ex.run({ action: 'click', args: { index: 0, stateId: s } }, ac.signal)
    ac.abort()
    expect(await p).toMatchObject({ ok: false, error: '操作已取消' })
  })

  it('runs commands one at a time and turns controller errors into results', async () => {
    const { ex, c } = setup()
    const order: string[] = []
    c.getBrowserState = vi.fn(async () => {
      order.push('read')
      await new Promise((r) => setTimeout(r, 5))
      return { url: '', title: '', header: '', content: '', footer: '' }
    })
    await Promise.all([ex.run({ action: 'state' }), ex.run({ action: 'state' })])
    expect(order).toEqual(['read', 'read'])
    c.getBrowserState = () => Promise.reject(new Error('boom'))
    expect(await ex.run({ action: 'state' })).toEqual({ ok: false, error: 'boom' })
    expect(await ex.run({ action: 'hover' })).toMatchObject({ ok: false })
  })
})

describe('content limits', () => {
  it('caps element count', () => {
    const lines = Array.from({ length: MAX_ELEMENTS + 5 }, (_, i) => `\t[${i}]<a >x />`)
    const r = truncateContent(lines.join('\n'))
    expect(r.truncated).toBe(true)
    expect(r.content.split('\n')).toHaveLength(MAX_ELEMENTS)
  })

  it('caps bytes', () => {
    const r = truncateContent('中'.repeat(MAX_CONTENT_BYTES))
    expect(r.truncated).toBe(true)
    const small = truncateContent('a\n[0]<b />')
    expect(small).toEqual({ content: 'a\n[0]<b />', truncated: false })
  })

  it('scrubs password values', () => {
    expect(scrubPasswords('[1]<input type=password value=abc />\n[2]<input value=keep />')).toBe(
      '[1]<input type=password  />\n[2]<input value=keep />',
    )
  })
})

describe('page-control.js copies', () => {
  it('web, server and sandbox injector serve the same bundle', () => {
    const repo = resolve(__dirname, '../../..')
    const copies = [
      'web/public/page-control.js',
      'server/internal/handlers/page-control.js',
      'sandbox-gateway/sandbox/internal/previewinject/page-control.js',
    ].map((rel) => readFileSync(resolve(repo, rel), 'utf8'))
    expect(copies[0]).toContain('__graspPageControl')
    expect(copies[1]).toBe(copies[0])
    expect(copies[2]).toBe(copies[0])
  })
})

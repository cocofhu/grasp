import { PageController } from '@page-agent/page-controller'

export type PageCommand = { action: string; args?: Record<string, unknown> }

export type PageState = {
  stateId: string
  url: string
  title: string
  content: string
  truncated: boolean
}

export type PageResult = {
  ok: boolean
  error?: string
  note?: string
  state?: PageState
}

export type ExecutorOptions = {
  /** Hides Grasp's own UI while the page is read so it never shows up as page content. */
  hideOwnUi?: (hidden: boolean) => void
  settleIdleMs?: number
  settleMaxMs?: number
  controller?: PageControllerLike
}

/** The subset of PageController the executor relies on (tests supply a fake). */
export type PageControllerLike = {
  getBrowserState(): Promise<{ url: string; title: string; header: string; content: string; footer: string }>
  cleanUpHighlights(): Promise<void>
  clickElement(index: number): Promise<{ success: boolean; message: string }>
  inputText(index: number, text: string): Promise<{ success: boolean; message: string }>
  selectOption(index: number, option: string): Promise<{ success: boolean; message: string }>
  scroll(o: { down: boolean; numPages: number; index?: number }): Promise<{ success: boolean; message: string }>
  elementAt(index: number): HTMLElement | null
  dispose(): void
}

export const MAX_ELEMENTS = 300
export const MAX_CONTENT_BYTES = 20 * 1024
const ELEMENT_LINE = /^\t*\*?\[\d+\]</

function randomId(): string {
  const b = new Uint8Array(6)
  globalThis.crypto?.getRandomValues?.(b)
  const s = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
  return s === '000000000000' ? Math.random().toString(16).slice(2, 14) : s
}

function utf8Len(s: string): number {
  return new TextEncoder().encode(s).length
}

/** Caps the element listing at MAX_ELEMENTS entries and MAX_CONTENT_BYTES bytes. */
export function truncateContent(content: string): { content: string; truncated: boolean } {
  const lines = content.split('\n')
  const out: string[] = []
  let elements = 0
  let bytes = 0
  for (const line of lines) {
    if (ELEMENT_LINE.test(line)) {
      if (elements >= MAX_ELEMENTS) return { content: out.join('\n'), truncated: true }
      elements++
    }
    const n = utf8Len(line) + 1
    if (bytes + n > MAX_CONTENT_BYTES) return { content: out.join('\n'), truncated: true }
    bytes += n
    out.push(line)
  }
  return { content: out.join('\n'), truncated: false }
}

/** Drops any value= that made it into a password field's line. */
export function scrubPasswords(content: string): string {
  return content
    .split('\n')
    .map((line) => (/\btype=password\b/i.test(line) ? line.replace(/\bvalue=\S*/g, '') : line))
    .join('\n')
}

function isPasswordField(el: Element | null): boolean {
  return !!el && el.tagName === 'INPUT' && (el as HTMLInputElement).type.toLowerCase() === 'password'
}

/** Removes value attributes from password inputs until the returned restore runs. */
function hidePasswordAttrs(): () => void {
  const saved: [Element, string][] = []
  document.querySelectorAll('input[value]').forEach((el) => {
    if (!isPasswordField(el)) return
    saved.push([el, el.getAttribute('value') || ''])
    el.removeAttribute('value')
  })
  return () => {
    for (const [el, v] of saved.splice(0)) el.setAttribute('value', v)
  }
}

function abortError(signal?: AbortSignal): PageResult | null {
  return signal?.aborted ? { ok: false, error: '操作已取消' } : null
}

/** Resolves once the DOM has been quiet for idleMs, or after maxMs. */
export function waitForSettle(idleMs: number, maxMs: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    let idle: ReturnType<typeof setTimeout>
    const done = () => {
      clearTimeout(idle)
      clearTimeout(cap)
      obs?.disconnect()
      signal?.removeEventListener('abort', done)
      resolve()
    }
    const bump = () => {
      clearTimeout(idle)
      idle = setTimeout(done, idleMs)
    }
    const cap = setTimeout(done, maxMs)
    let obs: MutationObserver | undefined
    if (typeof MutationObserver !== 'undefined' && document.documentElement) {
      obs = new MutationObserver(bump)
      obs.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true })
    }
    signal?.addEventListener('abort', done)
    bump()
  })
}

function wrapController(pc: PageController): PageControllerLike {
  const map = () => (pc as unknown as { selectorMap: Map<number, { ref?: HTMLElement }> }).selectorMap
  return {
    getBrowserState: () => pc.getBrowserState(),
    cleanUpHighlights: () => pc.cleanUpHighlights(),
    clickElement: (i) => pc.clickElement(i),
    inputText: (i, t) => pc.inputText(i, t),
    selectOption: (i, o) => pc.selectOption(i, o),
    scroll: (o) => pc.scroll(o),
    elementAt: (i) => map()?.get(i)?.ref ?? null,
    dispose: () => pc.dispose(),
  }
}

function num(v: unknown): number | null {
  return typeof v === 'number' && Number.isInteger(v) && v >= 0 ? v : null
}

/**
 * Runs page_* commands against the current document. Element indexes are only
 * valid for the stateId they came from; every action returns a fresh state.
 */
export function createExecutor(opts: ExecutorOptions = {}) {
  const pageId = randomId()
  let seq = 0
  let current = ''
  let queue: Promise<unknown> = Promise.resolve()
  const idleMs = opts.settleIdleMs ?? 500
  const maxMs = opts.settleMaxMs ?? 3000
  let pc = opts.controller ?? null
  const controller = (): PageControllerLike => {
    if (!pc) {
      pc = wrapController(
        new PageController({ viewportExpansion: 400, highlightOpacity: 0, highlightLabelOpacity: 0 }),
      )
    }
    return pc
  }

  async function capture(): Promise<PageState> {
    const c = controller()
    const restore = hidePasswordAttrs()
    opts.hideOwnUi?.(true)
    let bs
    try {
      // The DOM walk inside getBrowserState runs synchronously, so the UI is
      // back before the next paint.
      const p = c.getBrowserState()
      opts.hideOwnUi?.(false)
      restore()
      bs = await p
    } finally {
      opts.hideOwnUi?.(false)
      restore()
    }
    await c.cleanUpHighlights()
    document.getElementById('playwright-highlight-container')?.remove()
    const body = scrubPasswords([bs.header, bs.content, bs.footer].filter(Boolean).join('\n'))
    const { content, truncated } = truncateContent(body)
    seq++
    current = `${pageId}-${seq}`
    return { stateId: current, url: bs.url, title: bs.title, content, truncated }
  }

  function checkState(args: Record<string, unknown>): string | null {
    const id = typeof args.stateId === 'string' ? args.stateId : ''
    if (!current) return '页面还没有读取过状态,请先调用 page_state'
    if (id === current) return null
    if (!id.startsWith(pageId + '-')) return '页面已刷新或跳转,这个 stateId 已失效;请先调用 page_state'
    return '页面状态已更新,这个 stateId 已过期;请用最近一次返回的 stateId(或先调用 page_state)'
  }

  function target(index: number): { el: HTMLElement } | { error: string } {
    const el = controller().elementAt(index)
    if (!el) return { error: `当前状态里没有序号 ${index} 的元素` }
    if (!el.isConnected) return { error: `序号 ${index} 的元素已不在页面上;请先调用 page_state` }
    return { el }
  }

  async function after(signal: AbortSignal | undefined, res: PageResult): Promise<PageResult> {
    await waitForSettle(idleMs, maxMs, signal)
    const aborted = abortError(signal)
    if (aborted) return aborted
    return { ...res, state: await capture() }
  }

  async function exec(cmd: PageCommand, signal?: AbortSignal): Promise<PageResult> {
    const args = cmd.args ?? {}
    const aborted = abortError(signal)
    if (aborted) return aborted
    if (cmd.action === 'state') return { ok: true, state: await capture() }
    if (cmd.action === 'scroll' && args.index === undefined) {
      if (!current) await capture()
      const pages = typeof args.pages === 'number' && args.pages > 0 ? args.pages : 1
      const r = await controller().scroll({ down: args.down !== false, numPages: pages })
      if (!r.success) return { ok: false, error: r.message }
      return after(signal, { ok: true, note: r.message })
    }
    const stale = checkState(args)
    if (stale) return { ok: false, error: stale }
    const index = num(args.index)
    if (index === null) return { ok: false, error: '缺少有效的 index' }
    const t = target(index)
    if ('error' in t) return { ok: false, error: t.error }
    const c = controller()
    switch (cmd.action) {
      case 'click': {
        const blank = t.el.closest('a[target=_blank]') !== null
        let opened = false
        const open = window.open
        window.open = function (this: Window, ...a: Parameters<typeof window.open>) {
          opened = true
          return open.apply(this, a)
        } as typeof window.open
        let r
        try {
          r = await c.clickElement(index)
        } finally {
          window.open = open
        }
        if (!r.success) return { ok: false, error: r.message }
        const note =
          blank || opened
            ? '已点击;页面打开了新标签页,新标签页不受 Agent 控制,需要时请用户在新标签页里操作'
            : '已点击'
        return after(signal, { ok: true, note })
      }
      case 'input': {
        const text = typeof args.text === 'string' ? args.text : ''
        const secret = isPasswordField(t.el)
        const r = await c.inputText(index, text)
        if (!r.success) return { ok: false, error: secret ? '无法在该密码框中输入' : r.message }
        const note = secret ? '已输入密码(内容不回显)' : `已输入 ${[...text].length} 个字符`
        return after(signal, { ok: true, note })
      }
      case 'select': {
        const option = typeof args.option === 'string' ? args.option : ''
        const r = await c.selectOption(index, option)
        if (!r.success) return { ok: false, error: r.message }
        return after(signal, { ok: true, note: `已选择「${option}」` })
      }
      case 'scroll': {
        const pages = typeof args.pages === 'number' && args.pages > 0 ? args.pages : 1
        const r = await c.scroll({ down: args.down !== false, numPages: pages, index })
        if (!r.success) return { ok: false, error: r.message }
        return after(signal, { ok: true, note: r.message })
      }
    }
    return { ok: false, error: `不支持的页面操作: ${cmd.action}` }
  }

  /** Commands run one at a time in arrival order. */
  function run(cmd: PageCommand, signal?: AbortSignal): Promise<PageResult> {
    const p = queue.then(() =>
      exec(cmd, signal).catch((e: unknown) => ({ ok: false, error: e instanceof Error ? e.message : String(e) })),
    )
    queue = p
    return p
  }

  function dispose() {
    pc?.dispose()
    pc = null
    current = ''
  }

  return { run, dispose, pageId }
}

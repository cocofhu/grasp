// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { Window } from 'happy-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyTurn, ReactOption, ReactQuestion } from '@/lib/shared/types'
import { CLARIFY_DEMO_ESCAPE } from '@/lib/inbox/clarifyDemoWindow'
import { EMBED_DEMO_PREVIEW_KEY } from '@/lib/inbox/embedDemoPreview'
import ClarifyChat from './ClarifyChat.vue'

const here = dirname(fileURLToPath(import.meta.url))
const PICK = readFileSync(join(here, '../../../public/preview-pick.js'), 'utf8')
const GRASP = 'https://grasp.example'

const HtmlPreviewStub = defineComponent({
  name: 'HtmlPreview',
  props: { html: String, mode: String, enlargeable: Boolean, modalTitle: String },
  template:
    '<div data-testid="html-preview" :data-mode="mode" :data-enlargeable="enlargeable ? \'1\' : \'0\'">{{ html }}</div>',
})

function mountChat(turns: ClarifyTurn[], provide?: Record<symbol, unknown>) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ClarifyChat, {
    props: {
      runId: 'run-1',
      nodeId: 'react-1',
      iteration: 1,
      turns,
      done: false,
      active: true,
    },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: { Icon: true, HtmlPreview: HtmlPreviewStub },
      provide,
    },
  })
}

function option(id: string, label: string, demoHtml?: string, recommended = false): ReactOption {
  return { id, label, ...(demoHtml === undefined ? {} : { demoHtml }), ...(recommended ? { recommended: true } : {}) }
}

function question(partial: Partial<ReactQuestion> & Pick<ReactQuestion, 'id' | 'prompt' | 'options'>): ReactQuestion {
  return { allowMultiple: false, ...partial }
}

function agent(questions: ReactQuestion[], at = '2026-07-18T00:00:00Z'): ClarifyTurn {
  return { role: 'agent', text: '请选择', at, questions }
}

function selectedOptionLabels(wrapper: ReturnType<typeof mountChat>): string[] {
  return wrapper
    .findAll('[data-testid="clarify-option-btn"]')
    .filter((btn) => btn.attributes('aria-pressed') === 'true')
    .map((btn) => btn.get('[data-testid="clarify-option-label"]').text())
}

function wallPreviews(): HTMLElement[] {
  return [...document.body.querySelectorAll('[data-testid="clarify-preview-wall"] [data-testid="html-preview"]')] as HTMLElement[]
}

function dialog(): HTMLElement | null {
  return document.body.querySelector('[role="dialog"]')
}

function scrollerPreviews(wrapper: ReturnType<typeof mountChat>): HTMLElement[] {
  const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
  return [...scroller.querySelectorAll('[data-testid="html-preview"]')] as HTMLElement[]
}

const threeUp = question({
  id: 'layout3',
  prompt: '三选一',
  options: [
    option('a', '方案 A', '<p>preview-a</p>', true),
    option('b', '方案 B', '<p>preview-b</p>'),
    option('c', '方案 C', '<p>preview-c</p>'),
    option('blank', '空白', '   '),
    option('text', '纯文字'),
  ],
})

const fourUp = question({
  id: 'layout',
  prompt: '用哪种布局？',
  options: [
    option('a', '方案 A', '<p>preview-a</p>', true),
    option('b', '方案 B', '<p>preview-b</p>'),
    option('c', '方案 C', '<p>preview-c</p>'),
    option('d', '方案 D', '<p>preview-d</p>'),
    option('blank', '空白', '   '),
    option('text', '纯文字'),
  ],
})

const hosts: Window[] = []

afterEach(async () => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  for (const win of hosts.splice(0)) await win.happyDOM.close()
})

describe('ClarifyChat option preview window', () => {
  it('shows every valid preview under the options when there are 1–3', async () => {
    const wrapper = mountChat([agent([threeUp])])
    await flushPromises()
    const previews = scrollerPreviews(wrapper)
    expect(previews).toHaveLength(3)
    expect(previews.map((el) => el.textContent)).toEqual([
      expect.stringContaining('preview-a'),
      expect.stringContaining('preview-b'),
      expect.stringContaining('preview-c'),
    ])
    const wall = wrapper.get('[data-testid="clarify-inline-demos"]')
    expect(wall.classes()).toContain('mt-2.5')
    expect(wall.element.querySelector('.grid')?.className).toContain('grid-cols-1')
    expect(wall.element.querySelector('.grid')?.className).toContain('sm:grid-cols-3')
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
    const other = wrapper.get('[data-testid="clarify-other-input"]').element
    const confirm = wrapper.findAll('button').find((btn) => btn.text().includes('确认选择'))!.element
    expect(other.compareDocumentPosition(wall.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(wall.element.compareDocumentPosition(confirm) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(scroller.contains(wall.element)).toBe(true)
    expect(wrapper.findAll('[data-testid="clarify-option-enlarge"]')).toHaveLength(3)
    expect(wrapper.findAll('[data-testid="clarify-demo-enlarge"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('纯文字')
    expect(wrapper.text()).toContain('空白')
    expect(dialog()).toBeNull()
    wrapper.unmount()
  })

  it('highlights the selected inline frame without opening a window', async () => {
    const wrapper = mountChat([agent([threeUp])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[1]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 B'])
    const frames = wrapper.findAll('[data-testid="clarify-demo-frame"]')
    expect(frames[1]!.classes()).toContain('border-accent')
    expect(frames[0]!.classes()).not.toContain('border-accent')
    expect(dialog()).toBeNull()
    wrapper.unmount()
  })

  it('shows only the first selected preview when there are more than 3', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    expect(scrollerPreviews(wrapper)).toHaveLength(0)
    expect(wrapper.find('[data-testid="clarify-inline-demos"]').find('[data-testid="clarify-demo-frame"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="clarify-option-enlarge"]')).toHaveLength(4)

    await wrapper.findAll('[data-testid="clarify-option-btn"]')[3]!.trigger('click')
    await flushPromises()
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-d')])
    expect(wrapper.get('[data-testid="clarify-demo-frame"]').classes()).toContain('border-accent')

    const multi = question({
      id: 'multi',
      prompt: '可以多选',
      allowMultiple: true,
      options: [
        option('a', '甲', '<p>pa</p>'),
        option('b', '乙', '<p>pb</p>'),
        option('c', '丙', '<p>pc</p>'),
        option('d', '丁', '<p>pd</p>'),
      ],
    })
    const many = mountChat([agent([multi])])
    await flushPromises()
    const buttons = many.findAll('[data-testid="clarify-option-btn"]')
    await buttons[3]!.trigger('click')
    await buttons[0]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(many)).toEqual(['甲', '丁'])
    expect(scrollerPreviews(many).map((el) => el.textContent)).toEqual([expect.stringContaining('pa')])
    many.unmount()
    wrapper.unmount()
  })

  it('opens only the clicked frame outside the scroller and does not change the selection', async () => {
    const wrapper = mountChat([agent([threeUp])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()

    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[2]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(dialog()?.textContent).toContain('窗口放大查看')
    expect(dialog()?.textContent).toContain('方案 C')
    expect(wallPreviews()).toHaveLength(1)
    expect(wallPreviews()[0]?.textContent).toContain('preview-c')
    expect(wallPreviews()[0]?.textContent).not.toContain('preview-a')
    expect(wallPreviews()[0]?.getAttribute('data-enlargeable')).toBe('0')
    expect(document.body.querySelector('[data-testid="clarify-preview-pick"]')).toBeNull()
    expect(dialog()?.querySelector('[data-testid="clarify-demo-enlarge"]')).toBeNull()
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
    expect(scroller.contains(dialog())).toBe(false)
    expect(scrollerPreviews(wrapper)).toHaveLength(3)
    expect(wallPreviews()[0]?.textContent).toContain(CLARIFY_DEMO_ESCAPE)
    expect(scrollerPreviews(wrapper).some((el) => el.textContent?.includes(CLARIFY_DEMO_ESCAPE))).toBe(false)
    expect(document.body.querySelector('.fixed.inset-0')).toBeNull()

    await wrapper.get('[data-testid="clarify-inline-demos"]').findAll('[data-testid="clarify-demo-enlarge"]')[1]!.trigger('click')
    await flushPromises()
    expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(wallPreviews()[0]?.textContent).toContain('preview-b')
    expect(wallPreviews()[0]?.textContent).not.toContain('preview-c')
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    wrapper.unmount()
  })

  it('keeps a history window read-only and shows the same count rule below the rows', async () => {
    const past = question({
      id: 'past',
      prompt: '历史布局',
      options: [
        option('a', '旧 A', '<p>old-a</p>'),
        option('b', '旧 B', '<p>old-b</p>'),
        option('c', '旧 C', '<p>old-c</p>'),
      ],
    })
    const crowded = question({
      id: 'crowded',
      prompt: '历史四选',
      options: [
        option('a', '旧甲', '<p>old-1</p>'),
        option('b', '旧乙', '<p>old-2</p>'),
        option('c', '旧丙', '<p>old-3</p>'),
        option('d', '旧丁', '<p>old-4</p>'),
      ],
    })
    const current = question({
      id: 'now',
      prompt: '当前问题',
      options: [option('x', '现在', '<p>now</p>')],
    })
    const wrapper = mountChat([
      agent([past, crowded], '2026-07-18T00:00:00Z'),
      { role: 'human', text: '我的选择:\n- 历史布局 → 旧 A\n- 历史四选 → 旧丙', at: '2026-07-18T00:01:00Z' },
      agent([current], '2026-07-18T00:02:00Z'),
    ])
    await flushPromises()
    await wrapper.get('[data-testid="clarify-option-btn"]').trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['现在'])

    const history = wrapper.findAll('[data-testid="clarify-history-option"]')
    expect(history).toHaveLength(7)
    expect(history[0]!.text()).toContain('已选')
    const inline = wrapper.findAll('[data-testid="clarify-inline-demos"]')
    expect(inline[0]!.findAll('[data-testid="html-preview"]').map((el) => el.text())).toEqual([
      expect.stringContaining('old-a'),
      expect.stringContaining('old-b'),
      expect.stringContaining('old-c'),
    ])
    expect(inline[0]!.findAll('[data-testid="clarify-demo-frame"]')[0]!.text()).toContain('已选')
    expect(inline[0]!.findAll('[data-testid="clarify-demo-frame"]')[0]!.classes()).toContain('border-accent')
    expect(inline[1]!.findAll('[data-testid="html-preview"]').map((el) => el.text())).toEqual([
      expect.stringContaining('old-3'),
    ])
    expect(inline[1]!.text()).toContain('已选')

    await history[2]!.get('[data-testid="clarify-option-enlarge"]').trigger('click')
    await flushPromises()
    expect(wallPreviews()).toHaveLength(1)
    expect(wallPreviews()[0]?.textContent).toContain('old-c')
    expect(document.body.querySelector('[data-testid="clarify-preview-pick"]')).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['现在'])
    expect(wrapper.get('[data-testid="clarify-choice-summary"]').text()).toContain('旧 A')
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
    expect(scroller.contains(dialog())).toBe(false)
    wrapper.unmount()
  })

  it('closes when the deck moves and keeps the choice already made', async () => {
    const second = question({
      id: 'next',
      prompt: '下一题',
      options: [option('n', '继续')],
    })
    const wrapper = mountChat([agent([fourUp, second])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[1]!.trigger('click')
    await flushPromises()
    expect(dialog()).toBeTruthy()

    const next = wrapper.findAll('button').find((btn) => btn.text().includes('下一个'))
    await next!.trigger('click')
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual([])

    const prev = wrapper.findAll('button').find((btn) => btn.text().includes('上一题'))
    await prev!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-a')])
    expect(dialog()).toBeNull()
    wrapper.unmount()
  })

  it('closes when the round is submitted', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="clarify-option-enlarge"]').trigger('click')
    await flushPromises()
    expect(dialog()).toBeTruthy()
    const confirm = wrapper.findAll('button').find((btn) => btn.text().includes('确认选择'))
    await confirm!.trigger('click')
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(String(wrapper.emitted('send')![0][0])).toContain('方案 A')
    wrapper.unmount()
  })

  it('closes from the header and Escape without clearing the choice or covering the chat', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()
    const root = wrapper.get('[data-review-composer]').element
    vi.spyOn(root, 'getBoundingClientRect').mockReturnValue({
      x: 860,
      y: 0,
      width: 420,
      height: 900,
      left: 860,
      top: 0,
      right: 1280,
      bottom: 900,
      toJSON() {
        return {}
      },
    } as DOMRect)
    vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(1280)
    vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(900)

    async function open() {
      await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[1]!.trigger('click')
      await flushPromises()
      expect(dialog()).toBeTruthy()
    }

    await open()
    const box = dialog()!
    expect(box.className).not.toContain('inset-0')
    expect(document.body.querySelector('.fixed.inset-0')).toBeNull()
    const left = Number.parseFloat(box.style.left)
    const width = Number.parseFloat(box.style.width)
    expect(left + width).toBeLessThanOrEqual(860)
    expect(box.contains(wrapper.get('[data-testid="clarify-inline-demos"]').element)).toBe(false)
    ;(document.body.querySelector('[data-testid="clarify-demo-window-close"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(scrollerPreviews(wrapper)).toHaveLength(1)

    await open()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    await open()
    const frame = document.createElement('iframe')
    dialog()!.appendChild(frame)
    window.dispatchEvent(
      new MessageEvent('message', { data: { type: CLARIFY_DEMO_ESCAPE }, source: frame.contentWindow }),
    )
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    await open()
    const owned = document.createElement('iframe')
    dialog()!.appendChild(owned)
    const stranger = document.createElement('iframe')
    document.body.appendChild(stranger)
    window.dispatchEvent(
      new MessageEvent('message', { data: { type: CLARIFY_DEMO_ESCAPE }, source: stranger.contentWindow }),
    )
    await flushPromises()
    expect(dialog()).toBeTruthy()
    window.dispatchEvent(
      new MessageEvent('message', { data: { type: CLARIFY_DEMO_ESCAPE }, source: owned.contentWindow }),
    )
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    wrapper.unmount()
  })

  it('places the window above the drawer and outside the chat scroller', async () => {
    const chatSrc = readFileSync(join(here, 'ClarifyChat.vue'), 'utf8')
    const drawerSrc = readFileSync(join(here, '../ui/AppDrawer.vue'), 'utf8')
    const windowZ = Number(chatSrc.match(/data-testid="clarify-demo-window"[\s\S]{0,240}\bz-(\d+)\b/)?.[1]
      ?? chatSrc.match(/\bz-(\d+)\b[\s\S]{0,240}data-testid="clarify-demo-window"/)?.[1])
    const drawerZ = Number(drawerSrc.match(/\bz-(\d+)\b/)?.[1])
    expect(windowZ).toBeGreaterThan(drawerZ)

    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    await wrapper.find('[data-testid="clarify-option-enlarge"]').trigger('click')
    await flushPromises()
    const box = dialog()
    expect(box?.closest('.z-50')).toBeTruthy()
    expect(box?.closest('.fixed.inset-0')).toBeNull()
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
    expect(scroller.contains(box)).toBe(false)
    expect(document.body.contains(box)).toBe(true)
    wrapper.unmount()
  })

  it('reuses the same chat window for clarify, review, and the public gate drawer', () => {
    const clarify = readFileSync(join(here, 'RunClarifyPanel.vue'), 'utf8')
    const review = readFileSync(join(here, 'ReviewComposer.vue'), 'utf8')
    const gate = readFileSync(join(here, '../../views/PublicGateApprovalView.vue'), 'utf8')
    expect(clarify).toContain('<ClarifyChat')
    expect(review).toContain('<ClarifyChat')
    expect(gate).toContain('<ReviewComposer')
    expect(gate).toContain('demoHtml: o.demoHtml')
    expect(clarify).not.toContain('ClarifyDemoFrame')
    expect(review).not.toContain('ClarifyDemoFrame')
    expect(gate).not.toContain('ClarifyDemoFrame')
  })

  it('gives a plain question no enlarge control', async () => {
    const plain = question({
      id: 'plain',
      prompt: '纯文字题',
      options: [option('a', '左'), option('b', '右')],
    })
    const wrapper = mountChat([agent([plain])])
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-option-enlarge"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="clarify-demo-frame"]').exists()).toBe(false)
    expect(dialog()).toBeNull()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['左'])
    wrapper.unmount()
  })

  it('asks the CoCo host to open one frame and keeps that window outside the chat card', async () => {
    const open = vi.fn()
    const close = vi.fn()
    const wrapper = mountChat([agent([threeUp])], {
      [EMBED_DEMO_PREVIEW_KEY as symbol]: { open, close },
    })
    await flushPromises()
    expect(scrollerPreviews(wrapper)).toHaveLength(3)
    await wrapper.findAll('[data-testid="clarify-demo-enlarge"]')[1]!.trigger('click')
    await flushPromises()
    expect(open).toHaveBeenCalledTimes(1)
    expect(open).toHaveBeenCalledWith('<p>preview-b</p>', '方案 B')
    expect(dialog()).toBeNull()
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
    expect(scroller.querySelector('[role="dialog"]')).toBeNull()
    expect(wrapper.element.querySelector('[role="dialog"]')).toBeNull()

    const host = await openHostPage()
    host.setView(1600, 900)
    host.fromChat({ type: 'grasp-embed:demo', action: 'open', label: '方案 B', html: '<p>secret-demo</p>' })
    const drawer = host.shadow.querySelector('[data-role="drawer"]') as HTMLElement
    const modal = host.shadow.querySelector('[data-role="demo-modal"]') as HTMLElement
    expect(modal.hidden).toBe(false)
    expect(drawer.contains(modal)).toBe(false)
    expect(modal.parentElement).toBe(host.shadow.querySelector('[data-role="artifact-mask"]'))
    const box = geom(modal)
    const card = geom(drawer)
    expect(box.x + box.w).toBeLessThanOrEqual(card.x)
    expect(overlaps(box, card)).toBe(false)
    expect(host.shadow.querySelector('[data-role="demo-title"]')?.textContent).toBe('窗口放大查看 · 方案 B')
    expect(host.shadow.textContent || '').not.toContain('secret-demo')
    const frame = modal.querySelector('iframe')
    expect(frame?.getAttribute('src')).toBe(`${GRASP}/embed/runs/run-1/nodes/ap1/demo#theme=dark`)
    expect(frame?.getAttribute('src')).not.toContain('secret-demo')

    host.fromChat({ type: 'grasp-embed:demo', action: 'open', label: '方案 C', html: '<p>other-secret</p>' })
    expect(host.shadow.querySelectorAll('[data-role="demo-modal"]')).toHaveLength(1)
    expect(host.shadow.querySelector('[data-role="demo-title"]')?.textContent).toBe('窗口放大查看 · 方案 C')
    expect(overlaps(geom(modal), geom(drawer))).toBe(false)

    ;(host.shadow.querySelector('[data-role="drawer-close"]') as HTMLButtonElement).click()
    expect(modal.hidden).toBe(true)
    expect(host.drawerOpen()).toBe(false)
    wrapper.unmount()
    expect(close).toHaveBeenCalled()
  })
})

function overlaps(
  a: { x: number; y: number; w: number; h: number },
  b: { x: number; y: number; w: number; h: number },
) {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
}

function geom(el: HTMLElement) {
  return {
    x: Number.parseInt(el.style.left, 10),
    y: Number.parseInt(el.style.top, 10),
    w: Number.parseInt(el.style.width, 10),
    h: Number.parseInt(el.style.height, 10),
  }
}

async function openHostPage() {
  const win = new Window({
    url: 'http://10.0.0.5:5173/pricing#__grasp_embed&run=run-1&node=ap1&ticket=tk1&lang=zh-CN',
    settings: { navigator: { userAgent: 'test' }, disableIframePageLoading: true, disableJavaScriptFileLoading: true },
  })
  hosts.push(win)
  win.document.body.innerHTML = '<main><h2>Plan</h2></main>'
  ;(win as unknown as { fetch: (u: string) => Promise<unknown> }).fetch = async () => ({
    ok: true,
    json: async () => ({ origin: GRASP, runId: 'run-1', nodeId: 'ap1' }),
  })
  win.eval(PICK)
  await new Promise((resolve) => setTimeout(resolve, 0))
  const shadow = win.document.querySelector('grasp-preview-pick')?.shadowRoot as ShadowRoot
  if (!shadow) throw new Error('pick bar not mounted')
  const frame = () => shadow.querySelector('[data-role="drawer"] iframe') as HTMLIFrameElement | null
  const f = frame()
  if (!f) throw new Error('no drawer')
  const fake = { postMessage: () => {} }
  Object.defineProperty(f, 'contentWindow', { value: fake, configurable: true })
  win.dispatchEvent(new win.MessageEvent('message', { data: { type: 'grasp-embed:ready' }, origin: GRASP, source: fake as never }))
  return {
    win,
    shadow,
    drawerOpen: () => !(shadow.querySelector('[data-role="drawer"]') as HTMLElement).hidden,
    setView(w: number, h: number) {
      const de = win.document.documentElement
      Object.defineProperty(de, 'clientWidth', { configurable: true, get: () => w })
      Object.defineProperty(de, 'clientHeight', { configurable: true, get: () => h })
      win.dispatchEvent(new win.Event('resize'))
    },
    fromChat(data: Record<string, unknown>) {
      const drawerFrame = frame()
      if (!drawerFrame?.contentWindow) throw new Error('no drawer')
      win.dispatchEvent(
        new win.MessageEvent('message', { data, origin: GRASP, source: drawerFrame.contentWindow as never }),
      )
    },
  }
}

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

const IconStub = defineComponent({
  name: 'Icon',
  props: { name: String, size: [Number, String] },
  template: '<svg class="app-icon" :data-icon="name" :width="size" :height="size" />',
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
      stubs: { Icon: IconStub, HtmlPreview: HtmlPreviewStub },
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

function mockViewport(w: number, h: number) {
  vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(w)
  vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(h)
}

function pointer(target: EventTarget, type: 'pointerdown' | 'pointermove' | 'pointerup', x: number, y: number) {
  target.dispatchEvent(new PointerEvent(type, { clientX: x, clientY: y, bubbles: true, button: 0, pointerId: 1 }))
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
  it('shows one full-width preview for the selected option among 2–3 demos', async () => {
    const wrapper = mountChat([agent([threeUp])])
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-a')])
    expect(wrapper.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="clarify-inline-demos"]').find('.grid').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="clarify-option-enlarge"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('纯文字')
    expect(wrapper.text()).toContain('空白')

    await wrapper.findAll('[data-testid="clarify-option-btn"]')[1]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 B'])
    const previews = scrollerPreviews(wrapper)
    expect(previews).toHaveLength(1)
    expect(previews[0]?.textContent).toContain('preview-b')
    const wall = wrapper.get('[data-testid="clarify-inline-demos"]')
    expect(wall.classes()).toContain('mt-2.5')
    expect(wall.find('.grid').exists()).toBe(false)
    expect(wall.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(wall.get('[data-testid="clarify-demo-frame"]').classes()).toContain('border-accent')
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
    const other = wrapper.get('[data-testid="clarify-other-input"]').element
    const confirm = wrapper.findAll('button').find((btn) => btn.text().includes('确认选择'))!.element
    expect(other.compareDocumentPosition(wall.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(wall.element.compareDocumentPosition(confirm) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(scroller.contains(wall.element)).toBe(true)
    expect(wrapper.findAll('[data-testid="clarify-demo-enlarge"]')).toHaveLength(1)
    for (const enlarge of [
      ...wrapper.findAll('[data-testid="clarify-option-enlarge"]'),
      ...wrapper.findAll('[data-testid="clarify-demo-enlarge"]'),
    ]) {
      expect(enlarge.text()).not.toContain('放大')
      expect(enlarge.text()).not.toContain('Enlarge')
      expect(enlarge.attributes('aria-label')).toBe('放大')
      expect(enlarge.attributes('title')).toBe('放大')
      expect(enlarge.get('[data-icon="expand"]').attributes('width')).toBe('11')
    }
    expect(dialog()).toBeNull()

    await wrapper.findAll('[data-testid="clarify-option-btn"]')[2]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 C'])
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-c')])
    expect(wrapper.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)

    await wrapper.findAll('[data-testid="clarify-option-btn"]').find((btn) => btn.text().includes('纯文字'))!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['纯文字'])
    expect(wrapper.find('[data-testid="clarify-inline-demos"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('preselects the recommended preview when the question is already present on mount', async () => {
    const wrapper = mountChat([agent([threeUp])])
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(wrapper.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-a')])
    expect(wrapper.get('[data-testid="clarify-inline-demos"]').find('.grid').exists()).toBe(false)

    const plain = question({
      id: 'plain',
      prompt: '没有推荐',
      options: [
        option('a', '甲', '<p>pa</p>'),
        option('b', '乙', '<p>pb</p>'),
      ],
    })
    const none = mountChat([agent([plain])])
    await flushPromises()
    expect(selectedOptionLabels(none)).toEqual([])
    expect(none.find('[data-testid="clarify-inline-demos"]').exists()).toBe(false)
    none.unmount()
    wrapper.unmount()
  })

  it('shows the recommended preview once that question set arrives', async () => {
    const wrapper = mountChat([])
    await flushPromises()
    await wrapper.setProps({ turns: [agent([threeUp])] })
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-a')])
    expect(wrapper.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="clarify-inline-demos"]').find('.grid').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps the first still-checked preview when several of three are selected', async () => {
    const multi = question({
      id: 'multi3',
      prompt: '三选多',
      allowMultiple: true,
      options: [
        option('a', '甲', '<p>pa</p>'),
        option('b', '乙', '<p>pb</p>'),
        option('c', '丙', '<p>pc</p>'),
      ],
    })
    const wrapper = mountChat([agent([multi])])
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-inline-demos"]').exists()).toBe(false)
    const buttons = wrapper.findAll('[data-testid="clarify-option-btn"]')
    await buttons[2]!.trigger('click')
    await buttons[0]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['甲', '丙'])
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('pa')])
    expect(wrapper.get('[data-testid="clarify-inline-demos"]').find('.grid').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows only the first selected preview when there are more than 3', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-a')])
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
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

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
    expect(scrollerPreviews(wrapper)).toHaveLength(1)
    expect(wallPreviews()[0]?.textContent).toContain(CLARIFY_DEMO_ESCAPE)
    expect(scrollerPreviews(wrapper).some((el) => el.textContent?.includes(CLARIFY_DEMO_ESCAPE))).toBe(false)
    expect(document.body.querySelector('.fixed.inset-0')).toBeNull()

    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[1]!.trigger('click')
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
    ])
    expect(inline[0]!.find('.grid').exists()).toBe(false)
    expect(inline[0]!.findAll('[data-testid="clarify-demo-frame"]')[0]!.text()).toContain('已选')
    expect(inline[0]!.findAll('[data-testid="clarify-demo-frame"]')[0]!.classes()).toContain('border-accent')
    expect(inline[1]!.findAll('[data-testid="html-preview"]').map((el) => el.text())).toEqual([
      expect.stringContaining('old-3'),
    ])
    expect(inline[1]!.text()).toContain('已选')

    const historyEnlarge = history[2]!.get('[data-testid="clarify-option-enlarge"]')
    expect(history[2]!.findAll('button')).toHaveLength(1)
    expect(historyEnlarge.text()).toContain('旧 C')
    expect(historyEnlarge.text()).not.toContain('放大')
    expect(historyEnlarge.text()).not.toContain('Enlarge')
    expect(historyEnlarge.text()).not.toContain('old-c')
    expect(historyEnlarge.attributes('aria-label')).toContain('旧 C')
    expect(historyEnlarge.attributes('aria-label')).toContain('放大')
    expect(historyEnlarge.attributes('aria-label')).not.toContain('已选')
    expect(historyEnlarge.classes()).not.toContain('is-selected')
    expect(historyEnlarge.find('[data-icon="expand"]').exists()).toBe(false)
    expect(historyEnlarge.get('svg[viewBox="0 0 36 26"]').attributes('aria-hidden')).toBe('true')
    const historyMark = historyEnlarge.get('[data-icon="preview-expand"]')
    expect(historyMark.attributes('width')).toBe('14')
    expect(historyMark.attributes('height')).toBe('14')
    const pickedRow = history[0]!.get('[data-testid="clarify-option-enlarge"]')
    expect(pickedRow.classes()).toContain('is-selected')
    expect(pickedRow.text()).toContain('已选')
    expect(pickedRow.attributes('aria-label')).toContain('旧 A')
    expect(pickedRow.attributes('aria-label')).toContain('已选')
    expect(pickedRow.attributes('aria-label')).toContain('放大')
    expect(history[1]!.text()).not.toContain('已选')
    const liveEnlarge = wrapper
      .findAll('[data-testid="clarify-option-enlarge"]')
      .find((btn) => !btn.element.closest('[data-testid="clarify-history-option"]'))
    expect(liveEnlarge!.get('[data-icon="expand"]').attributes('width')).toBe('11')
    expect(inline[0]!.get('[data-testid="clarify-demo-enlarge"]').get('[data-icon="expand"]').attributes('width')).toBe('11')
    await historyEnlarge.trigger('click')
    await flushPromises()
    expect(wallPreviews()).toHaveLength(1)
    expect(wallPreviews()[0]?.textContent).toContain('old-c')
    expect(document.body.querySelector('[data-testid="clarify-preview-pick"]')).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['现在'])
    expect(history[0]!.text()).toContain('已选')
    expect(history[2]!.text()).not.toContain('已选')
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
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
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
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
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

  it('closes from the header and Escape without clearing the choice or adding a page mask', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
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
    expect(box.style.left).toBe('280px')
    expect(box.style.top).toBe('170px')
    expect(box.style.width).toBe('720px')
    expect(box.style.height).toBe('560px')
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

  it('drags from the title bar, resizes from the edges, and restores the default box on reopen', async () => {
    const wrapper = mountChat([agent([threeUp])])
    await flushPromises()
    mockViewport(1280, 900)
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[2]!.trigger('click')
    await flushPromises()

    const box = dialog()!
    expect(box.style.left).toBe('280px')
    expect(box.style.top).toBe('170px')
    expect(box.style.width).toBe('720px')
    expect(box.style.height).toBe('560px')
    expect(box.textContent).toContain('窗口放大查看')
    expect(box.textContent).toContain('方案 C')
    expect(box.querySelector('[data-testid="clarify-demo-frame"]')?.className).toContain('clarify-demo-expanded')
    expect(box.querySelector('[data-testid="clarify-preview-wall"]')?.className).toContain('flex-1')
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    const title = box.querySelector('[data-testid="clarify-demo-window-titlebar"]') as HTMLElement
    expect(title.className).toContain('cursor-grab')
    pointer(title, 'pointerdown', 40, 40)
    pointer(window, 'pointermove', 60, 60)
    pointer(window, 'pointerup', 60, 60)
    await flushPromises()
    expect(box.style.left).toBe('300px')
    expect(box.style.top).toBe('190px')
    expect(box.style.width).toBe('720px')
    expect(box.style.height).toBe('560px')
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    pointer(window, 'pointermove', 900, 900)
    await flushPromises()
    expect(box.style.left).toBe('300px')
    expect(box.style.width).toBe('720px')

    pointer(title, 'pointerdown', 200, 80)
    pointer(window, 'pointermove', 900, 700)
    pointer(window, 'pointerup', 900, 700)
    await flushPromises()
    expect(box.style.left).toBe('560px')
    expect(box.style.top).toBe('340px')
    expect(box.style.width).toBe('720px')
    expect(box.style.height).toBe('560px')

    const close = box.querySelector('[data-testid="clarify-demo-window-close"]') as HTMLElement
    pointer(close, 'pointerdown', 1200, 40)
    pointer(window, 'pointermove', 400, 400)
    pointer(window, 'pointerup', 400, 400)
    await flushPromises()
    expect(box.style.left).toBe('560px')
    expect(box.style.top).toBe('340px')
    expect(dialog()).toBeTruthy()

    const east = box.querySelector('[data-testid="clarify-demo-window-resize-e"]') as HTMLElement
    pointer(east, 'pointerdown', 1280, 200)
    pointer(window, 'pointermove', 1180, 200)
    pointer(window, 'pointerup', 1180, 200)
    await flushPromises()
    expect(box.style.left).toBe('560px')
    expect(box.style.width).toBe('620px')
    expect(box.style.height).toBe('560px')

    const south = box.querySelector('[data-testid="clarify-demo-window-resize-s"]') as HTMLElement
    pointer(south, 'pointerdown', 400, 900)
    pointer(window, 'pointermove', 400, 780)
    pointer(window, 'pointerup', 400, 780)
    await flushPromises()
    expect(box.style.top).toBe('340px')
    expect(box.style.height).toBe('440px')
    expect(box.style.width).toBe('620px')

    const west = box.querySelector('[data-testid="clarify-demo-window-resize-w"]') as HTMLElement
    const right = 560 + 620
    pointer(west, 'pointerdown', 56, 200)
    pointer(window, 'pointermove', 156, 200)
    pointer(window, 'pointerup', 156, 200)
    await flushPromises()
    expect(Number.parseFloat(box.style.left) + Number.parseFloat(box.style.width)).toBe(right)
    expect(box.style.left).toBe('660px')
    expect(box.style.width).toBe('520px')

    const north = box.querySelector('[data-testid="clarify-demo-window-resize-n"]') as HTMLElement
    const bottom = 340 + 440
    pointer(north, 'pointerdown', 400, 56)
    pointer(window, 'pointermove', 400, 136)
    pointer(window, 'pointerup', 400, 136)
    await flushPromises()
    expect(Number.parseFloat(box.style.top) + Number.parseFloat(box.style.height)).toBe(bottom)
    expect(box.style.top).toBe('420px')
    expect(box.style.height).toBe('360px')

    const corner = box.querySelector('[data-testid="clarify-demo-window-resize-se"]') as HTMLElement
    pointer(corner, 'pointerdown', 1180, 780)
    pointer(window, 'pointermove', 200, 200)
    pointer(window, 'pointerup', 200, 200)
    await flushPromises()
    expect(box.style.left).toBe('660px')
    expect(box.style.top).toBe('420px')
    expect(Number.parseFloat(box.style.width)).toBeGreaterThanOrEqual(480)
    expect(Number.parseFloat(box.style.height)).toBeGreaterThanOrEqual(320)
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    const held = {
      left: box.style.left,
      top: box.style.top,
      width: box.style.width,
      height: box.style.height,
    }
    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[1]!.trigger('click')
    await flushPromises()
    expect(wallPreviews()[0]?.textContent).toContain('preview-b')
    expect(box.style.left).toBe(held.left)
    expect(box.style.top).toBe(held.top)
    expect(box.style.width).toBe(held.width)
    expect(box.style.height).toBe(held.height)
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    mockViewport(1000, 700)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(box.style.width).not.toBe('944px')
    expect(Number.parseFloat(box.style.left) + Number.parseFloat(box.style.width)).toBeLessThanOrEqual(1000)
    expect(Number.parseFloat(box.style.top) + Number.parseFloat(box.style.height)).toBeLessThanOrEqual(700)
    expect(Number.parseFloat(box.style.left)).toBeGreaterThanOrEqual(0)
    expect(Number.parseFloat(box.style.top)).toBeGreaterThanOrEqual(0)

    ;(box.querySelector('[data-testid="clarify-demo-window-close"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(dialog()).toBeNull()

    mockViewport(1280, 900)
    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[2]!.trigger('click')
    await flushPromises()
    const reopened = dialog()!
    expect(reopened.style.left).toBe('280px')
    expect(reopened.style.top).toBe('170px')
    expect(reopened.style.width).toBe('720px')
    expect(reopened.style.height).toBe('560px')
    expect(reopened.textContent).toContain('窗口放大查看')
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(scrollerPreviews(wrapper)).toHaveLength(1)
    wrapper.unmount()
  })

  it('recomputes the centered default when the viewport changes before a drag', async () => {
    const wrapper = mountChat([agent([threeUp])])
    await flushPromises()
    mockViewport(1280, 900)
    await wrapper.find('[data-testid="clarify-option-enlarge"]').trigger('click')
    await flushPromises()
    expect(dialog()?.style.width).toBe('720px')

    mockViewport(390, 900)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(dialog()?.style.left).toBe('28px')
    expect(dialog()?.style.top).toBe('170px')
    expect(dialog()?.style.width).toBe('334px')
    expect(dialog()?.style.height).toBe('560px')
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
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(scrollerPreviews(wrapper).map((el) => el.textContent)).toEqual([expect.stringContaining('preview-a')])
    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[1]!.trigger('click')
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

  it('echoes a one-line choice as the selected preview inside the bubble', async () => {
    const prompt = '工作流放在哪？三种都已经带上这个资源按钮。'
    const picked = '产物抽屉顶部加一条只读工作流，资源抽屉不放'
    const q = question({
      id: 'flow',
      prompt,
      options: [
        option('flow', picked, '<p>preview-flow</p>'),
        option('button', '再加一个「工作流」按钮，和资源、产物并列', '<p>preview-button</p>'),
        option('split', '右侧不加工作流，只把资源和产物分开', '<p>preview-split</p>'),
      ],
    })
    const wrapper = mountChat([
      agent([q]),
      { role: 'human', text: `我的选择:\n- ${prompt} → ${picked}`, at: '2026-07-18T00:01:00Z' },
    ])
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-human-message"]').exists()).toBe(false)
    const inline = wrapper.get('[data-testid="clarify-inline-demos"]')
    const frames = inline.findAll('[data-testid="clarify-demo-frame"]')
    expect(frames).toHaveLength(1)
    expect(inline.find('.grid').exists()).toBe(false)
    expect(frames[0]!.classes()).toContain('border-accent')
    expect(frames[0]!.get('[data-testid="clarify-demo-selected"]').text()).toContain('已选')
    expect(wrapper.findAll('[data-testid="clarify-history-option"]')).toHaveLength(3)
    const name = frames[0]!.get('[data-testid="clarify-demo-label"]')
    expect(name.classes()).toContain('truncate')
    expect(name.element.contains(frames[0]!.get('[data-testid="clarify-demo-selected"]').element)).toBe(false)

    const summary = wrapper.get('[data-testid="clarify-choice-summary"]')
    expect(summary.classes()).toContain('chat-user-message')
    expect(summary.find('.chat-response-question').exists()).toBe(false)
    expect(summary.text()).not.toContain(prompt)
    const bubbleFrames = summary.findAll('[data-testid="clarify-demo-frame"]')
    expect(bubbleFrames).toHaveLength(1)
    expect(bubbleFrames[0]!.get('[data-testid="clarify-demo-label"]').text()).toBe(picked)
    expect(bubbleFrames[0]!.get('[data-testid="clarify-demo-selected"]').text()).toContain('已选')
    expect(bubbleFrames[0]!.get('[data-testid="html-preview"]').attributes('data-mode')).toBe('demo')
    expect(bubbleFrames[0]!.get('[data-testid="html-preview"]').text()).toContain('preview-flow')
    expect(summary.get('[data-testid="clarify-choice-demos"]').classes()).toContain('flex-col')
    expect(summary.find('.grid').exists()).toBe(false)
    expect(summary.element.parentElement?.className).toContain('w-[88%]')
    wrapper.unmount()
  })

  it('joins a wrapped prompt and enlarges the same read-only window', async () => {
    const prompt = '资源按你的说法加在对话栏右侧。\n\n工作流放哪？'
    const picked = '产物抽屉顶部'
    const other = '再加一个工作流按钮'
    const third = '右侧不加工作流'
    const q = question({
      id: 'flow',
      prompt,
      options: [
        option('flow', picked, '<p>preview-flow</p>'),
        option('button', other, '<p>preview-button</p>'),
        option('split', third, '<p>preview-split</p>'),
      ],
    })
    const wrapper = mountChat([
      agent([q]),
      { role: 'human', text: `我的选择:\n- ${prompt} → ${picked}`, at: '2026-07-18T00:01:00Z' },
    ])
    await flushPromises()
    expect(wrapper.find('[data-testid="clarify-human-message"]').exists()).toBe(false)
    const frames = wrapper.get('[data-testid="clarify-inline-demos"]').findAll('[data-testid="clarify-demo-frame"]')
    expect(frames).toHaveLength(1)
    expect(frames[0]!.text()).toContain('已选')
    const history = wrapper.findAll('[data-testid="clarify-history-option"]')
    expect(history).toHaveLength(3)
    expect(history[1]!.text()).not.toContain('已选')

    const summary = wrapper.get('[data-testid="clarify-choice-summary"]')
    expect(summary.text()).not.toContain('工作流放哪')
    expect(summary.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(summary.get('[data-testid="html-preview"]').text()).toContain('preview-flow')

    vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(1280)
    vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(900)

    await summary.get('[data-testid="clarify-demo-enlarge"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(dialog()?.textContent).toContain('窗口放大查看')
    expect(dialog()?.textContent).toContain(picked)
    expect(dialog()?.querySelector('[data-testid="clarify-demo-selected"]')).toBeTruthy()
    expect(dialog()?.querySelector('[data-testid="clarify-demo-frame"]')?.className).toContain('clarify-demo-expanded')
    expect(dialog()?.querySelector('[data-testid="clarify-demo-enlarge"]')).toBeNull()
    expect(document.body.querySelector('.fixed.inset-0')).toBeNull()
    const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
    expect(scroller.contains(dialog())).toBe(false)
    const box = dialog()!
    expect(box.style.left).toBe('280px')
    expect(box.style.top).toBe('170px')
    expect(box.style.width).toBe('720px')
    expect(box.style.height).toBe('560px')

    await history[1]!.get('[data-testid="clarify-option-enlarge"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(dialog()?.textContent).toContain(other)
    expect(dialog()?.querySelector('[data-testid="clarify-demo-selected"]')).toBeNull()
    expect(dialog()?.querySelector('[data-testid="clarify-demo-enlarge"]')).toBeNull()

    await frames[0]!.trigger('click')
    expect(dialog()?.textContent).toContain(other)
    expect(frames[0]!.text()).toContain('已选')
    expect(history[1]!.text()).not.toContain('已选')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(frames[0]!.text()).toContain('已选')
    expect(history[1]!.text()).not.toContain('已选')
    expect(summary.get('[data-testid="clarify-demo-selected"]').text()).toContain('已选')
    wrapper.unmount()
  })

  it('keeps more than three history previews on the selected card only', async () => {
    const q = question({
      id: 'many',
      prompt: '四种布局',
      options: [
        option('a', '方案 A', '<p>preview-a</p>'),
        option('b', '方案 B', '<p>preview-b</p>'),
        option('c', '方案 C', '<p>preview-c</p>'),
        option('d', '方案 D', '<p>preview-d</p>'),
      ],
    })
    const wrapper = mountChat([
      agent([q]),
      { role: 'human', text: '我的选择:\n- 四种布局 → 方案 C', at: '2026-07-18T00:01:00Z' },
    ])
    await flushPromises()
    const inline = wrapper.get('[data-testid="clarify-inline-demos"]')
    expect(inline.find('.grid').exists()).toBe(false)
    expect(inline.findAll('[data-testid="html-preview"]').map((el) => el.text())).toEqual([
      expect.stringContaining('preview-c'),
    ])
    const summary = wrapper.get('[data-testid="clarify-choice-summary"]')
    expect(summary.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(summary.get('[data-testid="html-preview"]').text()).toContain('preview-c')
    expect(summary.find('.chat-response-question').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps text answers when a choice has no preview, a custom note, or several questions', async () => {
    const plain = question({
      id: 'plain',
      prompt: '纯文字题',
      options: [option('l', '左'), option('r', '右')],
    })
    const mixed = question({
      id: 'mixed',
      prompt: '可以多选并自填',
      allowMultiple: true,
      options: [
        option('a', '方案 A', '<p>preview-a</p>'),
        option('b', '方案 B', '<p>preview-b</p>'),
        option('c', '纯文字项'),
      ],
    })
    const missed = question({
      id: 'missed',
      prompt: '对不上的题',
      options: [option('a', '方案 A', '<p>preview-a</p>')],
    })
    const textOnly = mountChat([
      agent([plain]),
      { role: 'human', text: '我的选择:\n- 纯文字题 → 左、右', at: '1' },
    ])
    await flushPromises()
    const plainSummary = textOnly.get('[data-testid="clarify-choice-summary"]')
    expect(plainSummary.find('[data-testid="clarify-demo-frame"]').exists()).toBe(false)
    expect(plainSummary.get('.chat-response-question').text()).toContain('纯文字题')
    expect(plainSummary.findAll('[data-testid="clarify-choice-answer"]').map((el) => el.text())).toEqual(['左', '右'])
    textOnly.unmount()

    const both = mountChat([
      agent([mixed]),
      { role: 'human', text: '我的选择:\n- 可以多选并自填 → 方案 A、方案 B、自己写的', at: '1' },
    ])
    await flushPromises()
    const mixedSummary = both.get('[data-testid="clarify-choice-summary"]')
    expect(mixedSummary.find('.chat-response-question').exists()).toBe(false)
    expect(mixedSummary.findAll('[data-testid="clarify-demo-label"]').map((el) => el.text())).toEqual(['方案 A'])
    expect(mixedSummary.get('[data-testid="clarify-choice-demos"]').classes()).toContain('flex-col')
    expect(mixedSummary.find('.grid').exists()).toBe(false)
    expect(mixedSummary.findAll('[data-testid="clarify-choice-answer"]').map((el) => el.text())).toEqual(['方案 B', '自己写的'])
    const inline = both.get('[data-testid="clarify-inline-demos"]')
    expect(inline.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(inline.findAll('[data-testid="clarify-demo-selected"]')).toHaveLength(1)
    expect(inline.get('[data-testid="clarify-demo-label"]').text()).toBe('方案 A')
    const historyRows = both.findAll('[data-testid="clarify-history-option"]')
    expect(historyRows).toHaveLength(2)
    expect(historyRows[0]!.text()).toContain('已选')
    expect(historyRows[1]!.text()).toContain('已选')
    expect(historyRows[0]!.get('[data-testid="clarify-option-enlarge"]').attributes('aria-label')).toContain('方案 A')
    expect(historyRows[1]!.get('[data-testid="clarify-option-enlarge"]').attributes('aria-label')).toContain('方案 B')
    both.unmount()

    const grouped = mountChat([
      agent([
        question({
          id: 'card',
          prompt: '有预览的题',
          options: [
            option('a', '方案 A', '<p>preview-a</p>'),
            option('b', '方案 B', '<p>preview-b</p>'),
          ],
        }),
        plain,
      ]),
      { role: 'human', text: '我的选择:\n- 有预览的题 → 方案 A\n- 纯文字题 → 右', at: '1' },
    ])
    await flushPromises()
    const rows = grouped.findAll('[data-testid="clarify-choice-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.get('.chat-response-question').text()).toContain('有预览的题')
    expect(rows[0]!.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(rows[1]!.get('.chat-response-question').text()).toContain('纯文字题')
    expect(rows[1]!.find('[data-testid="clarify-demo-frame"]').exists()).toBe(false)
    expect(rows[1]!.get('[data-testid="clarify-choice-answer"]').text()).toBe('右')
    grouped.unmount()

    const unmatched = mountChat([
      agent([missed]),
      { role: 'human', text: '我的选择:\n- 对不上的题 → 没有这个选项', at: '1' },
    ])
    await flushPromises()
    const missedSummary = unmatched.get('[data-testid="clarify-choice-summary"]')
    expect(missedSummary.find('[data-testid="clarify-demo-frame"]').exists()).toBe(false)
    expect(missedSummary.get('[data-testid="clarify-choice-answer"]').text()).toBe('没有这个选项')
    expect(unmatched.find('[data-testid="clarify-option-enlarge"]').exists()).toBe(true)
    expect(unmatched.find('[data-testid="clarify-inline-demos"]').exists()).toBe(false)
    unmatched.unmount()
  })

  it('echoes a separator-bearing preview once and keeps the plain label whole', async () => {
    const chip = '留在输入框上方，改成和项目标签、文件芯片一样的单行芯片'
    const scope = '输入区、已发送消息、反馈记录一起改'
    const place = question({
      id: 'place',
      prompt: '芯片放哪',
      options: [
        option('chip', chip, '<p>preview-chip</p>'),
        option('above', '放在消息列表上方', '<p>preview-above</p>'),
        option('menu', '收进输入框里的菜单', '<p>preview-menu</p>'),
      ],
    })
    const range = question({
      id: 'range',
      prompt: '改哪些',
      options: [option('all', scope), option('input', '只改输入区')],
    })
    const wrapper = mountChat([
      agent([place, range]),
      {
        role: 'human',
        text: `我的选择:\n- 芯片放哪 → ${chip}\n- 改哪些 → ${scope}`,
        at: '1',
      },
    ])
    await flushPromises()

    const inline = wrapper.get('[data-testid="clarify-inline-demos"]')
    const frames = inline.findAll('[data-testid="clarify-demo-frame"]')
    expect(frames).toHaveLength(1)
    expect(frames[0]!.get('[data-testid="clarify-demo-selected"]').text()).toContain('已选')
    expect(frames[0]!.text()).toContain(chip)

    const rows = wrapper.findAll('[data-testid="clarify-choice-row"]')
    expect(rows).toHaveLength(2)
    const bubbleFrames = rows[0]!.findAll('[data-testid="clarify-demo-frame"]')
    expect(bubbleFrames).toHaveLength(1)
    expect(bubbleFrames[0]!.get('[data-testid="clarify-demo-label"]').text()).toBe(chip)
    expect(bubbleFrames[0]!.get('[data-testid="html-preview"]').text()).toContain('preview-chip')
    expect(rows[0]!.find('[data-testid="clarify-choice-answer"]').exists()).toBe(false)
    expect(rows[1]!.find('[data-testid="clarify-demo-frame"]').exists()).toBe(false)
    expect(rows[1]!.findAll('[data-testid="clarify-choice-answer"]').map((el) => el.text())).toEqual([scope])
    wrapper.unmount()
  })

  it('restores a separator-bearing label under the English choice prefix', async () => {
    const chip = '留在输入框上方，改成和项目标签、文件芯片一样的单行芯片'
    const place = question({
      id: 'place',
      prompt: 'Where should the chip sit?',
      options: [
        option('chip', chip, '<p>preview-chip</p>'),
        option('above', 'Above the list', '<p>preview-above</p>'),
      ],
    })
    const wrapper = mountChat([
      agent([place]),
      { role: 'human', text: `My choices:\n- Where should the chip sit? → ${chip}`, at: '1' },
    ])
    await flushPromises()
    const frames = wrapper.get('[data-testid="clarify-inline-demos"]').findAll('[data-testid="clarify-demo-frame"]')
    expect(frames).toHaveLength(1)
    expect(frames[0]!.text()).toContain('已选')
    expect(frames[0]!.text()).toContain(chip)
    const summary = wrapper.get('[data-testid="clarify-choice-summary"]')
    expect(summary.findAll('[data-testid="clarify-demo-frame"]')).toHaveLength(1)
    expect(summary.get('[data-testid="clarify-demo-label"]').text()).toBe(chip)
    expect(summary.find('[data-testid="clarify-choice-answer"]').exists()).toBe(false)
    wrapper.unmount()
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

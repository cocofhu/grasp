// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyTurn, ReactOption, ReactQuestion } from '@/lib/shared/types'
import ClarifyChat from './ClarifyChat.vue'

const here = dirname(fileURLToPath(import.meta.url))

const HtmlPreviewStub = defineComponent({
  name: 'HtmlPreview',
  props: { html: String, mode: String, enlargeable: Boolean, modalTitle: String },
  template:
    '<div data-testid="html-preview" :data-mode="mode" :data-enlargeable="enlargeable ? \'1\' : \'0\'">{{ html }}</div>',
})

function mountChat(turns: ClarifyTurn[]) {
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
    .filter((btn) => btn.classes().includes('border-accent'))
    .map((btn) => btn.get('[data-testid="clarify-option-label"]').text())
}

function wallPreviews(): HTMLElement[] {
  return [...document.body.querySelectorAll('[data-testid="clarify-preview-wall"] [data-testid="html-preview"]')] as HTMLElement[]
}

function dialog(): HTMLElement | null {
  return document.body.querySelector('[role="dialog"]')
}

function scrollerHasPreview(wrapper: ReturnType<typeof mountChat>): boolean {
  const scroller = wrapper.get('[data-testid="clarify-scroller"]').element
  return !!scroller.querySelector('[data-testid="html-preview"], iframe')
}

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

afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('ClarifyChat option preview window', () => {
  it('keeps previews out of the chat and offers enlarge only for real previews', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    expect(scrollerHasPreview(wrapper)).toBe(false)
    expect(wrapper.get('[data-testid="clarify-scroller"]').text()).not.toContain('preview-a')
    const enlarges = wrapper.findAll('[data-testid="clarify-option-enlarge"]')
    expect(enlarges).toHaveLength(4)
    expect(enlarges.map((btn) => btn.element.parentElement?.textContent)).toEqual([
      expect.stringContaining('方案 A'),
      expect.stringContaining('方案 B'),
      expect.stringContaining('方案 C'),
      expect.stringContaining('方案 D'),
    ])
    expect(wrapper.text()).toContain('纯文字')
    expect(wrapper.get('[data-testid="clarify-other-input"]').attributes('placeholder')).toContain('其他')
    expect(wrapper.text()).toContain('确认选择')
    wrapper.unmount()
  })

  it('opens every valid preview without changing the selection, and scrolls the clicked card into view', async () => {
    const scroll = vi.spyOn(HTMLElement.prototype, 'scrollIntoView').mockImplementation(() => {})
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    const enlarges = wrapper.findAll('[data-testid="clarify-option-enlarge"]')
    await enlarges[3]!.trigger('click')
    await flushPromises()

    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    expect(dialog()?.textContent).toContain('窗口放大查看')
    expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(scrollerHasPreview(wrapper)).toBe(false)
    expect(wallPreviews()).toHaveLength(4)
    expect(wallPreviews().map((el) => el.textContent)).toEqual([
      expect.stringContaining('preview-a'),
      expect.stringContaining('preview-b'),
      expect.stringContaining('preview-c'),
      expect.stringContaining('preview-d'),
    ])
    expect(wallPreviews().every((el) => el.getAttribute('data-enlargeable') === '0')).toBe(true)
    expect(wallPreviews().every((el) => el.getAttribute('data-mode') === 'demo')).toBe(true)
    const wall = document.body.querySelector('[data-testid="clarify-preview-wall"]')
    expect(wall?.textContent).not.toContain('放大')
    expect(wall?.className).toContain('grid-cols-1')
    expect(wall?.className).toContain('sm:grid-cols-3')
    const focused = document.body.querySelector('[data-preview-option="d"]')
    expect(scroll.mock.instances).toContain(focused)
    wrapper.unmount()
  })

  it('syncs single-select between the window and the option row', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[0]!.trigger('click')
    await flushPromises()

    const picks = () => [...document.body.querySelectorAll('[data-testid="clarify-preview-pick"]')] as HTMLButtonElement[]
    picks()[1]!.click()
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 B'])
    const frames = () => [...document.body.querySelectorAll('[data-testid="clarify-demo-frame"]')] as HTMLElement[]
    expect(frames()[1]?.className).toContain('border-accent')
    expect(frames()[0]?.className).not.toContain('border-accent')

    await wrapper.findAll('[data-testid="clarify-option-btn"]')[2]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 C'])
    expect(frames()[2]?.className).toContain('border-accent')
    expect(frames()[1]?.className).not.toContain('border-accent')
    wrapper.unmount()
  })

  it('toggles multi-select from the window', async () => {
    const multi = question({
      id: 'multi',
      prompt: '可以多选',
      allowMultiple: true,
      options: [option('a', '甲', '<p>pa</p>'), option('b', '乙', '<p>pb</p>'), option('c', '丙')],
    })
    const wrapper = mountChat([agent([multi])])
    await flushPromises()
    expect(wrapper.findAll('[data-testid="clarify-option-enlarge"]')).toHaveLength(2)
    await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[0]!.trigger('click')
    await flushPromises()
    const picks = () => [...document.body.querySelectorAll('[data-testid="clarify-preview-pick"]')] as HTMLButtonElement[]
    picks()[0]!.click()
    picks()[1]!.click()
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['甲', '乙'])
    picks()[0]!.click()
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['乙'])
    wrapper.unmount()
  })

  it('keeps a history window read-only and out of the current draft', async () => {
    const past = question({
      id: 'past',
      prompt: '历史布局',
      options: [option('a', '旧 A', '<p>old-a</p>'), option('b', '旧 B', '<p>old-b</p>'), option('c', '旧 C', '<p>old-c</p>'), option('d', '旧 D', '<p>old-d</p>')],
    })
    const current = question({
      id: 'now',
      prompt: '当前问题',
      options: [option('x', '现在', '<p>now</p>')],
    })
    const wrapper = mountChat([
      agent([past], '2026-07-18T00:00:00Z'),
      { role: 'human', text: '我的选择:\n- 历史布局 → 旧 A', at: '2026-07-18T00:01:00Z' },
      agent([current], '2026-07-18T00:02:00Z'),
    ])
    await flushPromises()
    await wrapper.get('[data-testid="clarify-option-btn"]').trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['现在'])

    const history = wrapper.findAll('[data-testid="clarify-history-option"]')
    expect(history).toHaveLength(4)
    expect(history[0]!.text()).toContain('已选')
    expect(scrollerHasPreview(wrapper)).toBe(false)
    await history[2]!.get('[data-testid="clarify-option-enlarge"]').trigger('click')
    await flushPromises()

    expect(wallPreviews()).toHaveLength(4)
    expect(document.body.querySelector('[data-testid="clarify-preview-pick"]')).toBeNull()
    const frame = document.body.querySelector('[data-preview-option="c"]') as HTMLElement
    frame.click()
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['现在'])
    expect(wrapper.get('[data-testid="clarify-choice-summary"]').text()).toContain('旧 A')
    expect(scrollerHasPreview(wrapper)).toBe(false)
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

  it('closes from the header, the backdrop, and Escape without clearing the choice', async () => {
    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()

    async function open() {
      await wrapper.findAll('[data-testid="clarify-option-enlarge"]')[1]!.trigger('click')
      await flushPromises()
      expect(dialog()).toBeTruthy()
    }

    await open()
    ;(document.body.querySelector('.fixed.inset-0.z-50 .h-14 button') as HTMLButtonElement).click()
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    await open()
    ;(document.body.querySelector('.fixed.inset-0.z-50 > .absolute.inset-0') as HTMLElement).click()
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])

    await open()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(dialog()).toBeNull()
    expect(selectedOptionLabels(wrapper)).toEqual(['方案 A'])
    wrapper.unmount()
  })

  it('places the window above the drawer and outside the chat scroller', async () => {
    const modalSrc = readFileSync(join(here, '../ui/AppModal.vue'), 'utf8')
    const drawerSrc = readFileSync(join(here, '../ui/AppDrawer.vue'), 'utf8')
    const modalZ = Number(modalSrc.match(/\bz-(\d+)\b/)?.[1])
    const drawerZ = Number(drawerSrc.match(/\bz-(\d+)\b/)?.[1])
    expect(modalZ).toBeGreaterThan(drawerZ)

    const wrapper = mountChat([agent([fourUp])])
    await flushPromises()
    await wrapper.find('[data-testid="clarify-option-enlarge"]').trigger('click')
    await flushPromises()
    const box = dialog()
    expect(box?.closest('.z-50')).toBeTruthy()
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
    expect(dialog()).toBeNull()
    await wrapper.findAll('[data-testid="clarify-option-btn"]')[0]!.trigger('click')
    await flushPromises()
    expect(selectedOptionLabels(wrapper)).toEqual(['左'])
    wrapper.unmount()
  })
})

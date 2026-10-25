// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import zhCommon from '@/locales/zh-CN/common.json'
import zhPages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import type { Run } from '@/lib/shared/types'

const push = vi.hoisted(() => vi.fn())

vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

import RunBoardPreviewDrawer from './RunBoardPreviewDrawer.vue'

function sampleRun(partial: Partial<Run> = {}): Run {
  return {
    id: 'run-4d5cb057',
    title: '在流转过程中更新会有BUG',
    workflowId: 'wf-1',
    workflowName: '自拟迭代',
    status: 'waiting_human',
    priority: 'normal',
    startedAt: '2026-10-25T11:48:00Z',
    durationSec: 157,
    createdAt: '2026-10-25T11:48:00Z',
    progress: 0.16666666666666666,
    currentNodeLabel: '需求澄清',
    trigger: 'manual',
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

function mountDrawer(run: Run | null, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const messages = locale === 'en'
    ? { en: { ...enCommon, ...enPages } }
    : { 'zh-CN': { ...zhCommon, ...zhPages } }
  const i18n = createI18n({ legacy: false, locale, messages })
  return mount(RunBoardPreviewDrawer, {
    props: { open: true, run },
    global: {
      plugins: [i18n],
      stubs: { Icon: { template: '<span />' } },
      directives: { 'hover-ink': vHoverInk },
    },
    attachTo: document.body,
  })
}

function sheet(): HTMLElement {
  const el = document.body.querySelector('.fixed.inset-0 > div:last-child')
  if (!(el instanceof HTMLElement)) throw new Error('drawer sheet missing')
  return el
}

function actions(): HTMLElement {
  const el = document.body.querySelector('.run-summary-actions')
  if (!(el instanceof HTMLElement)) throw new Error('summary actions missing')
  return el
}

function footerButtons(): HTMLButtonElement[] {
  return [...actions().querySelectorAll('button')]
}

describe('RunBoardPreviewDrawer', () => {
  it('shows run details when open', () => {
    const wrapper = mountDrawer(sampleRun())
    expect(document.body.textContent).toContain('在流转过程中更新会有BUG')
    wrapper.unmount()
  })

  it('uses the flush shell at 420px and keeps fields in order without duplicate rows', () => {
    const wrapper = mountDrawer(sampleRun())
    const panel = sheet()
    expect(panel.className).not.toMatch(/\bapp-sidebar-card\b/)
    expect(panel.className).toMatch(/\binset-y-0\b/)
    expect(panel.className).toMatch(/\bright-0\b/)
    expect(panel.className).toMatch(/\bborder-l\b/)
    expect(panel.style.width).toBe('420px')
    expect(panel.style.maxWidth).toBe('100vw')

    const text = document.body.textContent || ''
    const titleAt = text.indexOf('在流转过程中更新会有BUG')
    const statusAt = text.indexOf('等待人工')
    const priorityAt = text.indexOf('普通')
    const progressAt = text.indexOf('进度')
    expect(titleAt).toBeGreaterThanOrEqual(0)
    expect(titleAt).toBeLessThan(statusAt)
    expect(statusAt).toBeLessThan(priorityAt)
    expect(priorityAt).toBeLessThan(progressAt)
    expect(text.match(/等待人工/g)).toHaveLength(1)
    expect(text.match(/普通/g)).toHaveLength(1)
    expect(text).toContain('17%')
    expect(text).not.toContain('0.16666666666666666')
    expect(text).not.toContain('点击卡片先预览摘要')

    const labels = [...document.body.querySelectorAll('dt')].map((el) => el.textContent)
    expect(labels).toEqual(['工作流', '当前节点', '开始', '耗时', 'Run ID'])
    expect(labels).not.toContain('状态')
    expect(labels).not.toContain('优先级')

    const values = [...document.body.querySelectorAll('dd')].map((el) => el.textContent)
    expect(values[0]).toBe('自拟迭代')
    expect(values[1]).toBe('需求澄清')
    expect(values[4]).toBe('#4d5cb057')
    expect(document.body.querySelector('.run-summary-fill')?.getAttribute('style')).toContain('width: 17%')
    expect(document.body.querySelector('.col-span-full')).toBeTruthy()
    wrapper.unmount()
  })

  it('shows an em dash and a zero-width bar when progress is missing', () => {
    const wrapper = mountDrawer(sampleRun({
      progress: Number.NaN,
      title: '',
      workflowName: '',
      currentNodeLabel: '',
      startedAt: '',
      durationSec: undefined as unknown as number,
    }))
    const text = document.body.textContent || ''
    expect(document.body.querySelector('h3')?.textContent).toBe('#4d5cb057')
    expect(text).toContain('普通')
    expect(text).not.toContain('%')
    expect(document.body.querySelector('.run-summary-fill')?.getAttribute('style')).toContain('width: 0%')
    const values = [...document.body.querySelectorAll('dd')].map((el) => el.textContent)
    expect(values).toEqual(['—', '—', '—', '—', '#4d5cb057'])
    wrapper.unmount()
  })

  it('draws 0% and 100% from the clamped ratio', async () => {
    const wrapper = mountDrawer(sampleRun({ progress: 0 }))
    expect(document.body.querySelector('.tabular-nums')?.textContent).toBe('0%')
    expect(document.body.querySelector('.run-summary-fill')?.getAttribute('style')).toContain('width: 0%')
    await wrapper.setProps({ run: sampleRun({ progress: 2 }) })
    expect(document.body.querySelector('.tabular-nums')?.textContent).toBe('100%')
    expect(document.body.querySelector('.run-summary-fill')?.getAttribute('style')).toContain('width: 100%')
    wrapper.unmount()
  })

  it('places both footer buttons on the right without stretching them', () => {
    const wrapper = mountDrawer(sampleRun())
    const bar = actions()
    expect(bar.className).toMatch(/\bjustify-end\b/)
    expect(bar.className).not.toMatch(/\bflex-1\b/)
    const [dismiss, open] = footerButtons()
    expect(dismiss.textContent).toContain('继续浏览')
    expect(open.textContent).toContain('进入运行详情')
    for (const button of [dismiss, open]) {
      expect(button.className).not.toMatch(/\bflex-1\b/)
      expect(button.className).not.toMatch(/\bw-full\b/)
      expect(button.className).toMatch(/\bh-9\b/)
      expect(button.className).toMatch(/\btext-sm\b/)
      expect(button.className).toMatch(/\brounded-md\b/)
      expect(button.classList.contains('hover-ink-host')).toBe(true)
      expect(button.querySelector(':scope > .hover-ink')).toBeTruthy()
    }
    expect(dismiss.className).toMatch(/\bborder\b/)
    expect(open.className).toMatch(/\bbg-accent\b/)
    expect(open.className).not.toMatch(/\bshadow-glow\b/)
    expect(dismiss.style.getPropertyValue('--hover-ink-color')).toContain('--c-elevated')
    expect(open.style.getPropertyValue('--hover-ink-color')).toContain('--c-accent-hover')
    wrapper.unmount()
  })

  it('does not spread ink on a disabled open-detail button', () => {
    const wrapper = mountDrawer(null)
    const open = footerButtons()[1]
    expect(open.disabled).toBe(true)
    expect(open.classList.contains('is-ink-suppressed')).toBe(true)
    expect(open.style.getPropertyValue('--hover-ink-color')).toBe('')
    open.dispatchEvent(new PointerEvent('pointerenter', { clientX: 8, clientY: 8, bubbles: true }))
    expect(open.classList.contains('is-hover-ink')).toBe(false)
    wrapper.unmount()
  })

  it('keeps browsing on the board and opens the run from the detail button', async () => {
    push.mockClear()
    const wrapper = mountDrawer(sampleRun())
    const [dismiss, open] = footerButtons()
    await dismiss.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(push).not.toHaveBeenCalled()

    await open.click()
    expect(wrapper.emitted('close')).toHaveLength(2)
    expect(push).toHaveBeenCalledWith('/runs/run-4d5cb057')
    wrapper.unmount()
  })

  it('follows the English labels and hides the summary hint', () => {
    const wrapper = mountDrawer(sampleRun({ title: 'Keep this title' }), 'en')
    const text = document.body.textContent || ''
    expect(text).toContain('Run summary')
    expect(text).toContain('Keep this title')
    expect(text).toContain('Progress')
    expect(text).toContain('Workflow')
    expect(text).toContain('Current node')
    expect(text).toContain('Started')
    expect(text).toContain('Duration')
    expect(text).toContain('Run ID')
    expect(text).toContain('Keep browsing')
    expect(text).toContain('Open run detail')
    expect(text).toContain('Normal')
    expect(text).not.toContain('Cards open a summary first')
    wrapper.unmount()
  })

  it('declares a single-column facts grid under 360px and no flex-1 footer', () => {
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'RunBoardPreviewDrawer.vue'), 'utf8')
    expect(src).toContain('flush')
    expect(src).not.toContain('pages.board.preview.hint')
    expect(src).not.toMatch(/\bflex-1\b/)
    expect(src).toContain(':glow="false"')
    expect(src).toMatch(/container-name:\s*run-summary/)
    expect(src).toMatch(/@container run-summary \(max-width:\s*359px\)\s*\{[\s\S]*grid-template-columns:\s*1fr/)
    expect(src).toMatch(/grid-template-columns:\s*1fr 1fr/)
    expect(src).toMatch(/\.run-summary-actions :deep\(button\)\s*\{[^}]*flex:\s*none/)
  })
})

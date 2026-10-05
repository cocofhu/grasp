// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import InboxPendingCard from './InboxPendingCard.vue'
import type { ClarifyInboxItem, GateInboxItem } from '@/lib/shared/types'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...pages } },
})

function gate(over: Partial<GateInboxItem> = {}): GateInboxItem {
  return {
    type: 'gate',
    nodeType: 'human_gate',
    runId: 'run-1',
    nodeId: 'hg1',
    workflowName: 'wf',
    title: '审阅视觉稿',
    bodyMd: '请审阅',
    actions: [{ id: 'approve', label: '批准' }],
    requestedAt: '2026-08-01T00:00:00Z',
    shareLink: { state: 'none', canCreate: true },
    ...over,
  }
}

function clarify(over: Partial<ClarifyInboxItem> = {}): ClarifyInboxItem {
  return {
    type: 'clarify',
    kind: 'clarify',
    runId: 'run-2',
    nodeId: 'react1',
    workflowName: 'wf',
    label: '澄清需求',
    done: false,
    requestedAt: '2026-08-01T00:00:00Z',
    updatedAt: '2026-08-01T00:00:00Z',
    ...over,
  }
}

describe('InboxPendingCard share entry', () => {
  it('shows copy button and status for human_gate, kind=clarify and kind=review', async () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate() },
      global: { plugins: [i18n] },
    })
    expect(w.get('[data-testid="gate-share-copy-btn"]').text()).toContain('复制临时链接')
    expect(w.get('[data-testid="gate-share-status"]').text()).toContain('尚未创建')
    await w.get('[data-testid="gate-share-copy-btn"]').trigger('click')
    expect(w.emitted('open-share')).toHaveLength(1)

    const review = mount(InboxPendingCard, {
      props: {
        item: clarify({
          kind: 'review',
          nodeId: 'research1',
          label: '调研复审',
          shareLink: { state: 'none', canCreate: true },
        }),
      },
      global: { plugins: [i18n] },
    })
    expect(review.get('[data-testid="gate-share-copy-btn"]').text()).toContain('复制临时链接')
    expect(review.get('[data-testid="gate-share-status"]').text()).toContain('尚未创建')

    const explicitClarify = mount(InboxPendingCard, {
      props: {
        item: clarify({
          kind: 'clarify',
          shareLink: { state: 'none', canCreate: true },
        }),
      },
      global: { plugins: [i18n] },
    })
    expect(explicitClarify.get('[data-testid="gate-share-copy-btn"]').text()).toContain('复制临时链接')

    const ps = mount(InboxPendingCard, {
      props: { item: gate({ nodeId: 'ps1', nodeType: 'proposal_select', title: '选择方案' }) },
      global: { plugins: [i18n] },
    })
    expect(ps.find('[data-testid="gate-share-copy-btn"]').exists()).toBe(false)
    expect(ps.find('[data-testid="gate-share-status"]').exists()).toBe(false)
  })

  it('copy button uses chip-sized classes at all breakpoints and is not taller than the status chip', () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate() },
      global: { plugins: [i18n] },
    })
    const btn = w.get('[data-testid="gate-share-copy-btn"]')
    const chip = w.get('[data-testid="gate-share-status"]')
    const row = w.get('[data-testid="gate-share-row"]')
    const cls = btn.classes().join(' ')
    expect(cls).toContain('min-h-6')
    expect(cls).toContain('text-[10px]')
    expect(cls).toContain('px-1.5')
    expect(cls).toContain('py-0.5')
    expect(cls).toContain('gap-1')
    expect(cls).toContain('border-accent/40')
    expect(cls).toContain('bg-accent/10')
    expect(cls).toContain('text-accent-2')
    expect(cls).not.toContain('min-h-11')
    expect(cls).not.toContain('min-w-[44px]')
    expect(cls).not.toContain('text-xs')
    expect(cls).not.toContain('md:min-h-6')
    expect(cls).not.toContain('md:text-[10px]')
    expect(btn.text()).toContain('复制临时链接')
    expect(row.classes()).toEqual(expect.arrayContaining(['flex-wrap', 'items-center', 'gap-2']))
    const btnH = (btn.element as HTMLElement).getBoundingClientRect().height
    const chipH = (chip.element as HTMLElement).getBoundingClientRect().height
    if (btnH > 0 && chipH > 0) {
      expect(btnH).toBeLessThanOrEqual(chipH + 8)
    }
  })

  it('max-md hit wrap is vertical-only 44px and has no debug outline', () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate() },
      global: { plugins: [i18n] },
    })
    const wrap = w.get('[data-testid="gate-share-hit-wrap"]')
    const wrapCls = wrap.classes().join(' ')
    expect(wrapCls).toContain('max-md:min-h-[44px]')
    expect(wrapCls).toContain('max-md:min-w-[44px]')
    expect(wrapCls).toContain('max-md:py-2.5')
    expect(wrapCls).toContain('max-md:-my-2.5')
    expect(wrapCls).not.toMatch(/dashed|outline-dashed|border-dashed/)
    expect(wrap.html()).not.toMatch(/hit-ghost|show-hit/)
    const box = (wrap.element as HTMLElement).getBoundingClientRect()
    if (box.width > 0 && box.height > 0) {
      expect(box.width).toBeGreaterThanOrEqual(44)
      expect(box.height).toBeGreaterThanOrEqual(44)
    }
  })

  it('hit wrap click opens share when enabled', async () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate() },
      global: { plugins: [i18n] },
    })
    await w.get('[data-testid="gate-share-hit-wrap"]').trigger('click')
    expect(w.emitted('open-share')).toHaveLength(1)
    expect(w.emitted('select')).toBeFalsy()
  })

  it('chip click does not open share panel', async () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate() },
      global: { plugins: [i18n] },
    })
    await w.get('[data-testid="gate-share-status"]').trigger('click')
    expect(w.emitted('open-share')).toBeFalsy()
  })

  it('disables copy when used', async () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate({ shareLink: { state: 'used', canCreate: false } }) },
      global: { plugins: [i18n] },
    })
    expect(w.get('[data-testid="gate-share-status"]').text()).toContain('已使用')
    expect((w.get('[data-testid="gate-share-copy-btn"]').element as HTMLButtonElement).disabled).toBe(true)
    await w.get('[data-testid="gate-share-hit-wrap"]').trigger('click')
    expect(w.emitted('open-share')).toBeFalsy()
  })

  it('hit wrap does not open share when card is disabled', async () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate(), disabled: true },
      global: { plugins: [i18n] },
    })
    expect((w.get('[data-testid="gate-share-copy-btn"]').element as HTMLButtonElement).disabled).toBe(true)
    await w.get('[data-testid="gate-share-hit-wrap"]').trigger('click')
    expect(w.emitted('open-share')).toBeFalsy()
  })

  it('marks a booting sandbox and keeps its share row disabled (plan g1.2)', async () => {
    const w = mount(InboxPendingCard, {
      props: {
        item: clarify({
          state: 'starting',
          label: '把登录做清楚',
          shareLink: { state: 'none', canCreate: true },
        }),
      },
      global: { plugins: [i18n] },
    })
    const card = w.get('[data-testid="inbox-item-card"]')
    expect(card.attributes('data-starting')).toBe('true')
    expect(card.text()).toContain('启动中')
    expect(card.text()).not.toContain('待澄清')
    expect(w.find('[data-testid="gate-share-row"]').exists()).toBe(true)
    expect(w.get('[data-testid="gate-share-status"]').text()).toContain('尚未创建')
    expect((w.get('[data-testid="gate-share-copy-btn"]').element as HTMLButtonElement).disabled).toBe(true)
    await w.get('[data-testid="gate-share-copy-btn"]').trigger('click')
    await w.get('[data-testid="gate-share-hit-wrap"]').trigger('click')
    expect(w.emitted('open-share')).toBeFalsy()
    const spinner = w.findComponent({ name: 'AppSpinner' })
    expect(spinner.exists()).toBe(true)
    expect(spinner.props('size')).toBe(18)
    const svg = spinner.find('svg')
    expect(svg.classes()).toContain('app-spinner')
    expect(svg.classes().join(' ')).not.toContain('animate-spin')
    // Not a rotating chat bubble (plan g2.2).
    expect(svg.html()).not.toMatch(/M21 12a8 8 0 0 1-11\.3 7\.3/)
    expect(svg.html()).not.toMatch(/L4 20/)
    expect(svg.html()).toMatch(/<circle[^>]*r="9"/)
  })

  it('marks a busy parked session with the replying badge, AppSpinner, and share row (plan g2.1)', () => {
    const w = mount(InboxPendingCard, {
      props: {
        item: clarify({
          state: 'replying',
          kind: 'clarify',
          label: '把登录做清楚',
          shareLink: { state: 'none', canCreate: true },
        }),
      },
      global: { plugins: [i18n] },
    })
    const card = w.get('[data-testid="inbox-item-card"]')
    expect(card.attributes('data-replying')).toBe('true')
    expect(card.attributes('data-starting')).toBeUndefined()
    expect(card.text()).toContain('正在回复中')
    expect(card.text()).not.toContain('待澄清')
    expect(card.text()).not.toContain('启动中')
    expect(w.find('[data-testid="gate-share-row"]').exists()).toBe(true)
    expect(w.get('[data-testid="gate-share-copy-btn"]').text()).toContain('复制临时链接')
    const spinner = w.findComponent({ name: 'AppSpinner' })
    expect(spinner.exists()).toBe(true)
    expect(spinner.props('size')).toBe(18)
    const svg = spinner.find('svg')
    expect(svg.classes()).toContain('app-spinner')
    expect(svg.classes().join(' ')).not.toContain('animate-spin')
    expect(svg.html()).not.toMatch(/L4 20/)
    expect(svg.html()).toMatch(/M21 12a9 9 0 0 1-9 9/)
    expect(w.find('.text-n-artifact').exists()).toBe(true)
  })

  it('idle clarify card keeps static chat icon without AppSpinner (plan g2.1 idle)', () => {
    const w = mount(InboxPendingCard, {
      props: {
        item: clarify({
          kind: 'clarify',
          label: '澄清需求',
          shareLink: { state: 'none', canCreate: true },
        }),
      },
      global: { plugins: [i18n] },
    })
    expect(w.findComponent({ name: 'AppSpinner' }).exists()).toBe(false)
    expect(w.findComponent({ name: 'Icon' }).props('name')).toBe('chat')
    expect(w.find('svg.app-spinner').exists()).toBe(false)
  })
  it('renders English Replying for state=replying', () => {
    const en = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { ...enCommon, ...enPages } },
    })
    const w = mount(InboxPendingCard, {
      props: { item: clarify({ state: 'replying', kind: 'clarify' }) },
      global: { plugins: [en] },
    })
    expect(w.get('[data-testid="inbox-item-card"]').text()).toContain('Replying')
    expect(w.get('[data-testid="inbox-item-card"]').text()).not.toContain('Needs clarify')
  })

  it('keeps starting above replying and still shows the disabled share row', () => {
    const w = mount(InboxPendingCard, {
      props: {
        item: clarify({
          state: 'starting',
          kind: 'clarify',
          shareLink: { state: 'none', canCreate: true },
        }),
      },
      global: { plugins: [i18n] },
    })
    expect(w.get('[data-testid="inbox-item-card"]').text()).toContain('启动中')
    expect(w.get('[data-testid="inbox-item-card"]').text()).not.toContain('正在回复中')
    expect(w.find('[data-testid="gate-share-row"]').exists()).toBe(true)
    expect((w.get('[data-testid="gate-share-copy-btn"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows remaining time for active links', () => {
    const w = mount(InboxPendingCard, {
      props: { item: gate({ shareLink: { state: 'active', remainingSec: 3600, canManage: true } }) },
      global: { plugins: [i18n] },
    })
    expect(w.get('[data-testid="gate-share-status"]').text()).toMatch(/有效/)
  })
})

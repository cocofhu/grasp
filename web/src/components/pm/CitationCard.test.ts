// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import { provideConversationArtifacts } from '@/lib/chat/conversationArtifacts'
import { describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import CitationCard from './CitationCard.vue'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

function mountCard(citation: Record<string, unknown>) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(CitationCard, {
    props: { citation: citation as any },
    global: { plugins: [i18n] },
  })
}

describe('CitationCard', () => {
  it('renders valid RUN card with Run #shortId and optional subtitle', () => {
    const w = mountCard({
      type: 'run',
      targetId: 'run-a1b2c3d4',
      summarySnippet: '需求澄清 · 进行中',
    })
    expect(w.get('[data-testid="citation-card-valid"]').text()).toContain('Run #a1b2c3d4')
    expect(w.text()).toContain('需求澄清 · 进行中')
    expect(w.text()).not.toContain('run:run-a1b2c3d4')
    const jump = w.get('[data-testid="citation-open-source"]')
    expect(jump.attributes('disabled')).toBeUndefined()
    jump.trigger('click')
    expect(push).toHaveBeenCalledWith('/runs/run-a1b2c3d4')
  })

  it('greys out historical fake citation and blocks jump', async () => {
    push.mockClear()
    const w = mountCard({
      type: 'run',
      targetId: 'trigger',
      summarySnippet: 'run:trigger',
    })
    expect(w.find('[data-testid="citation-card-invalid"]').exists()).toBe(true)
    expect(w.text()).toContain('run:trigger')
    expect(w.get('[data-testid="citation-invalid-note"]').text()).toBe('引用无效或目标不存在')
    const jump = w.get('[data-testid="citation-open-source"]')
    expect(jump.attributes('disabled')).toBeDefined()
    await jump.trigger('click')
    expect(push).not.toHaveBeenCalled()
  })

  it('emits a precise artifact target instead of leaving the conversation', async () => {
    push.mockClear()
    const wrapper = mountCard({ type: 'artifact', targetId: 'art-a1b2c3d4', summarySnippet: '交付报告' })
    expect(wrapper.get('[data-testid="citation-open-source"]').text()).toBe('打开产物')
    await wrapper.get('[data-testid="citation-open-source"]').trigger('click')
    expect(wrapper.emitted('open-artifact')).toEqual([[{ id: 'art-a1b2c3d4' }]])
    expect(push).not.toHaveBeenCalled()
  })

  it.each([
    ['art-a1b2c3d4', { id: 'art-a1b2c3d4' }],
    ['research.json', { name: 'research.json' }],
  ])('opens %s in the host artifact pane', async (targetId, target) => {
    const open = vi.fn()
    const citation = { type: 'artifact', targetId }
    const wrapper = mount({ setup() { provideConversationArtifacts({ open }); return () => h(CitationCard, { citation }) } }, { global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })] } })
    await wrapper.get('[data-testid="citation-open-source"]').trigger('click')
    expect(open).toHaveBeenCalledWith(target)
    expect(wrapper.findComponent(CitationCard).emitted('open-artifact')).toBeUndefined()
  })

  it('continues to navigate workflow references normally', async () => {
    push.mockClear()
    const wrapper = mountCard({ type: 'workflow', targetId: 'wf-a1b2c3d4', summarySnippet: '交付流程' })
    await wrapper.get('[data-testid="citation-open-source"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/workflows/wf-a1b2c3d4')
  })

})

// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zhPages from '@/locales/zh-CN/pages.json'
import StructuredArtifactView, { isFeedbackArtifactName, isStructuredArtifactName } from './StructuredArtifactView.vue'

describe('isStructuredArtifactName', () => {
  it('matches the reserved structured JSON artifact names', () => {
    const names = [
      'clarified_requirement.json',
      'research.json',
      'root_cause.json',
      'plan.json',
      'implementation_result.json',
      'test_result.json',
      'review.json',
      'merge_request.json',
      'preflight.json',
    ]
    for (const name of names) {
      expect(isStructuredArtifactName(name)).toBe(true)
    }
  })

  it('does not match markdown, html, or other artifact names', () => {
    expect(isStructuredArtifactName('clarified_requirement.md')).toBe(false)
    expect(isStructuredArtifactName('page.html')).toBe(false)
    expect(isStructuredArtifactName('design.md')).toBe(false)
    expect(isStructuredArtifactName('result.json')).toBe(false)
  })

  // Round names are generated per node and iteration, so the ledger can only be
  // recognized by prefix — an exact-name whitelist would drop every round into
  // the raw JSON fallback.
  it('recognizes the ledger by prefix, not by an exact name', () => {
    const names = [
      'feedback_index.json',
      'feedback.review.research-1.i2r3.json',
      'feedback.gate.approve.i1r1.json',
    ]
    for (const name of names) {
      expect(isFeedbackArtifactName(name)).toBe(true)
      expect(isStructuredArtifactName(name)).toBe(true)
    }
    expect(isFeedbackArtifactName('feedbackish.json')).toBe(false)
    expect(isFeedbackArtifactName('research.json')).toBe(false)
  })
})

describe('merge_request.json card', () => {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhPages } })

  it('renders one row per repo with branches, state badge and MR link', () => {
    const wrapper = mount(StructuredArtifactView, {
      global: { plugins: [i18n], stubs: { Icon: true } },
      props: {
        name: 'merge_request.json',
        doc: {
          summary: '两个仓库已交付',
          items: [
            {
              repo: 'web',
              sourceBranch: 'feature/login',
              targetBranch: 'main',
              url: 'https://github.com/acme/web/pull/12',
              provider: 'github',
              state: 'created',
            },
            {
              repo: 'server',
              sourceBranch: 'feature/login',
              targetBranch: 'develop',
              provider: 'other',
              state: 'unsupported',
              note: '自建 Git 服务,请手动创建',
            },
          ],
        },
      },
    })
    expect(wrapper.text()).toContain('两个仓库已交付')
    const rows = wrapper.findAll('[data-testid="merge-request-item"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('web')
    expect(rows[0]!.find('[data-testid="merge-request-branches"]').text()).toBe('feature/login→main')
    expect(rows[0]!.find('[data-testid="merge-request-state"]').text()).toBe('已创建')
    const link = rows[0]!.find('[data-testid="merge-request-link"]')
    expect(link.attributes('href')).toBe('https://github.com/acme/web/pull/12')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.text()).toContain('打开 PR')
    expect(rows[1]!.find('[data-testid="merge-request-state"]').text()).toBe('不支持')
    expect(rows[1]!.find('[data-testid="merge-request-link"]').exists()).toBe(false)
    expect(rows[1]!.text()).toContain('自建 Git 服务')
  })
})

describe('structured report content', () => {
  it.each([
    ['research.json', { title: '调研结论', summary: '选择现有接口', questions: [{ question: '如何接入？', answer: '复用现有入口' }], findings: [{ title: '接口稳定', detail: '已有回归覆盖' }], recommendation: '按现有契约推进' }, ['调研结论', '选择现有接口', '如何接入？', '复用现有入口', '接口稳定', '已有回归覆盖', '按现有契约推进']],
    ['implementation_result.json', { summary: '实现已完成', changed_areas: [{ title: '运行入口', detail: '保留历史输入' }], tests: ['验证手动运行'], breaking_changes: ['旧入口已迁移'], follow_ups: ['跟踪执行结果'] }, ['实现已完成', '运行入口', '保留历史输入', '验证手动运行', '旧入口已迁移', '跟踪执行结果']],
  ])('keeps every report section when rendering %s as a document', (name, doc, expected) => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhPages } })
    const wrapper = mount(StructuredArtifactView, { props: { name: name as string, doc }, global: { plugins: [i18n], stubs: { Icon: true } } })
    expect(wrapper.find('article').exists()).toBe(true)
    for (const text of expected as string[]) expect(wrapper.text()).toContain(text)
    wrapper.unmount()
  })
})

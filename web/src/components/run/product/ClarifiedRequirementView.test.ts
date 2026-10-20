// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { provideReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import ClarifiedRequirementView, { type ClarifiedRequirementDoc } from './ClarifiedRequirementView.vue'

function mountView(doc: ClarifiedRequirementDoc, withAnnotate = false) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  if (!withAnnotate) {
    return mount(ClarifiedRequirementView, {
      props: { doc, accent: '#818CF8' },
      global: { plugins: [i18n], stubs: { Icon: true } },
    })
  }
  const Host = defineComponent({
    setup() {
      provideReviewAnnotate({ enabled: true, annotate: () => {} })
      return () => h(ClarifiedRequirementView, { doc, accent: '#818CF8' })
    },
  })
  return mount(Host, {
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('ClarifiedRequirementView', () => {
  it('renders summary and functional requirements', () => {
    const doc: ClarifiedRequirementDoc = {
      title: '用户登录',
      summary: '支持 OIDC 登录',
      background: '现有系统无 SSO',
      goals: ['提升登录体验'],
      functional_requirements: [
        {
          title: 'OIDC 登录',
          detail: '接入 Synology SSO',
          priority: 'must',
          acceptance_criteria: ['可完成登录跳转'],
        },
      ],
      in_scope: ['Web 登录'],
      out_of_scope: ['移动端'],
    }
    const wrapper = mountView(doc)
    expect(wrapper.text()).toContain('用户登录')
    expect(wrapper.text()).toContain('支持 OIDC 登录')
    expect(wrapper.text()).toContain('OIDC 登录')
    expect(wrapper.text()).toContain('Web 登录')
    wrapper.unmount()
  })

  it('renders personas and scenarios when present', () => {
    const doc: ClarifiedRequirementDoc = {
      summary: '摘要',
      personas: [{ name: '开发者', description: '日常使用者', goals: ['快速部署'] }],
      user_scenarios: [{ name: '部署应用', actor: '开发者', flow: '提交 MR', outcome: '自动预览' }],
    }
    const wrapper = mountView(doc)
    expect(wrapper.text()).toContain('开发者')
    expect(wrapper.text()).toContain('部署应用')
    wrapper.unmount()
  })

  it('renders secondary list fields', () => {
    const doc: ClarifiedRequirementDoc = {
      summary: '摘要',
      assumptions: ['用户已登录'],
      dependencies: ['OIDC Provider'],
      constraints: ['不改后端协议'],
      risks: [{ id: 'r1', description: '会话过期', mitigation: '刷新令牌' }],
      glossary: [{ term: 'Gate', definition: '人工审批节点' }],
    }
    const wrapper = mountView(doc)
    expect(wrapper.text()).toContain('用户已登录')
    expect(wrapper.text()).toContain('OIDC Provider')
    expect(wrapper.text()).toContain('不改后端协议')
    expect(wrapper.text()).toContain('会话过期')
    expect(wrapper.text()).toContain('Gate')
    wrapper.unmount()
  })

  const LONG_CJK_DETAIL =
    'NFR说明与指标在常见手机宽度下可连续扫读，中文不得被压成逐字竖排。'
  const LONG_CJK_METRIC =
    '与单次 Tab 紧凑主值同一套格式；芯片含开始时间与当前列KPI主值在常见桌面宽度下可扫读，不被七位数字撑破；芯片对比态不另起视觉体系。'
  const UNBROKEN_TOKEN = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyzToken'

  function nfrCard(wrapper: ReturnType<typeof mountView>, id: string) {
    return wrapper.get(`[data-json-path="non_functional_requirements[${id}]"]`)
  }

  function nfrBlocks(wrapper: ReturnType<typeof mountView>, id: string) {
    const card = nfrCard(wrapper, id).element as HTMLElement
    return Array.from(card.children) as HTMLElement[]
  }

  it('renders long CJK NFR as full-width blocks without shrink-0 metric (g2.1)', () => {
    const doc: ClarifiedRequirementDoc = {
      summary: '摘要',
      non_functional_requirements: [
        {
          id: 'n1',
          category: 'usability',
          detail: LONG_CJK_DETAIL,
          metric: LONG_CJK_METRIC,
        },
      ],
    }
    const wrapper = mountView(doc)
    expect(wrapper.text()).toContain('非功能需求')
    expect(wrapper.text()).toContain('usability')
    expect(wrapper.text()).toContain(LONG_CJK_DETAIL)
    expect(wrapper.text()).toContain(LONG_CJK_METRIC)
    expect(wrapper.text()).toContain('指标:')

    const card = nfrCard(wrapper, 'n1')
    expect(card.classes()).not.toContain('flex')

    const [head, detailEl, metricEl] = nfrBlocks(wrapper, 'n1')
    expect(head.className).toMatch(/\bflex\b/)
    expect(head.className).toMatch(/\bflex-wrap\b/)
    expect(head.className).toMatch(/\bitems-center\b/)
    expect(head.textContent).toContain('usability')
    expect(head.textContent).not.toContain(LONG_CJK_DETAIL)
    expect(head.textContent).not.toContain(LONG_CJK_METRIC)
    expect(head.textContent).not.toContain('指标:')

    expect(detailEl.textContent).toBe(LONG_CJK_DETAIL)
    expect(detailEl.className).toMatch(/overflow-wrap:anywhere/)
    expect(detailEl.className).toMatch(/\bbreak-words\b/)
    expect(detailEl.className).toMatch(/\bw-full\b/)
    expect(detailEl.className).toMatch(/\bmin-w-0\b/)

    expect(metricEl.getAttribute('data-json-path')).toBe('non_functional_requirements[n1].metric')
    expect(metricEl.textContent).toContain(`指标: ${LONG_CJK_METRIC}`)
    expect(metricEl.className).not.toMatch(/\bshrink-0\b/)
    expect(metricEl.className).toMatch(/overflow-wrap:anywhere/)
    expect(metricEl.className).toMatch(/\bbreak-words\b/)
    expect(metricEl.className).toMatch(/\bw-full\b/)
    expect(metricEl.className).toMatch(/\bmin-w-0\b/)
    wrapper.unmount()
  })

  it('hides empty NFR fields and keeps short/long metric structure consistent (g2.2)', () => {
    const doc: ClarifiedRequirementDoc = {
      summary: '摘要',
      non_functional_requirements: [
        { id: 'a', category: 'usability', detail: '只有说明没有指标' },
        { id: 'b', detail: '无类别只有说明与短指标', metric: 'p95' },
        { id: 'c', category: 'performance', metric: 'p95' },
        {
          id: 'd',
          category: 'reliability',
          detail: `超长无空格 ${UNBROKEN_TOKEN}`,
          metric: LONG_CJK_METRIC,
        },
      ],
    }
    const wrapper = mountView(doc)
    expect(wrapper.text()).toContain('只有说明没有指标')
    expect(wrapper.text()).toContain('无类别只有说明与短指标')
    expect(wrapper.text()).toContain(UNBROKEN_TOKEN)

    const aKids = nfrBlocks(wrapper, 'a')
    expect(aKids).toHaveLength(2)
    expect(aKids[0].textContent).toContain('usability')
    expect(aKids[1].textContent).toBe('只有说明没有指标')
    expect(wrapper.get('[data-json-path="non_functional_requirements[a]"]').text()).not.toContain('指标:')

    const bKids = nfrBlocks(wrapper, 'b')
    expect(bKids).toHaveLength(3)
    expect(bKids[0].textContent?.trim()).toBe('')
    expect(bKids[0].querySelector('.bg-elevated')).toBeNull()
    expect(bKids[1].textContent).toBe('无类别只有说明与短指标')
    expect(bKids[2].textContent).toContain('指标: p95')
    expect(bKids[2].className).not.toMatch(/\bshrink-0\b/)
    expect(bKids[2].className).toMatch(/\bw-full\b/)

    const cKids = nfrBlocks(wrapper, 'c')
    expect(cKids).toHaveLength(2)
    expect(cKids[0].textContent).toContain('performance')
    expect(cKids[1].textContent).toContain('指标: p95')
    expect(cKids[1].className).not.toMatch(/\bshrink-0\b/)
    expect(cKids[1].className).toMatch(/overflow-wrap:anywhere/)
    expect(wrapper.get('[data-json-path="non_functional_requirements[c]"]').text()).not.toContain('只有说明')

    const dKids = nfrBlocks(wrapper, 'd')
    expect(dKids).toHaveLength(3)
    expect(dKids[2].textContent).toContain(`指标: ${LONG_CJK_METRIC}`)
    expect(dKids[1].className).toMatch(/overflow-wrap:anywhere/)
    expect(dKids[2].className).toMatch(/overflow-wrap:anywhere/)
    expect(dKids[2].className).not.toMatch(/\bshrink-0\b/)
    expect(dKids[2].className.split(/\s+/).sort().join(' ')).toBe(cKids[1].className.split(/\s+/).sort().join(' '))

    const cards = wrapper.findAll('[data-json-path^="non_functional_requirements["]').filter((n) => {
      const path = n.attributes('data-json-path') || ''
      return /non_functional_requirements\[[^\]]+\]$/.test(path)
    })
    expect(cards.length).toBeGreaterThanOrEqual(4)
    wrapper.unmount()
  })

  it('keeps annotate control on the NFR chip row when channel is enabled (g2.3)', () => {
    const doc: ClarifiedRequirementDoc = {
      summary: '摘要',
      non_functional_requirements: [
        {
          id: 'n1',
          category: 'usability',
          detail: LONG_CJK_DETAIL,
          metric: LONG_CJK_METRIC,
        },
      ],
    }
    const wrapper = mountView(doc, true)
    const card = nfrCard(wrapper, 'n1')
    const btn = card.find('button[title="标注 non_functional_requirements[n1]"]')
    expect(btn.exists()).toBe(true)

    const [head, detailEl, metricEl] = nfrBlocks(wrapper, 'n1')
    expect(head.contains(btn.element)).toBe(true)
    expect(head.textContent).toContain('usability')
    expect(head.textContent).not.toContain(LONG_CJK_DETAIL)
    expect(head.textContent).not.toContain(LONG_CJK_METRIC)
    expect(detailEl.contains(btn.element)).toBe(false)
    expect(metricEl.contains(btn.element)).toBe(false)
    expect(metricEl.className).not.toMatch(/\bshrink-0\b/)
    expect(detailEl.className).toMatch(/\bw-full\b/)
    expect(metricEl.className).toMatch(/\bw-full\b/)
    wrapper.unmount()
  })

  it('exposes AnnotateBtn for personas/interfaces/entities/business_rules', () => {
    const doc: ClarifiedRequirementDoc = {
      summary: '摘要',
      personas: [{ id: 'u1', name: '开发者' }],
      external_interfaces: [{ id: 'i1', name: 'OIDC' }],
      data_entities: [{ id: 'e1', name: 'User' }],
      business_rules: ['仅管理员可审批'],
    }
    const wrapper = mountView(doc, true)
    const titles = wrapper.findAll('button').map((b) => b.attributes('title') || '')
    expect(titles.some((t) => t.includes('personas[u1]'))).toBe(true)
    expect(titles.some((t) => t.includes('external_interfaces[i1]'))).toBe(true)
    expect(titles.some((t) => t.includes('data_entities[e1]'))).toBe(true)
    expect(titles.some((t) => t.includes('business_rules[0]'))).toBe(true)
    wrapper.unmount()
  })
})

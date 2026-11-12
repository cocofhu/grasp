// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import type { ReactForm, ReactQuestion } from '@/lib/shared/types'
import SoloAskCard from './SoloAskCard.vue'

function mountCard(props: { questions?: ReactQuestion[]; forms?: ReactForm[]; disabled?: boolean }) {
  return mount(SoloAskCard, {
    props,
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': pages } })] },
  })
}

const db: ReactQuestion = { id: 'db', prompt: '用哪个数据库?', options: [{ id: 'pg', label: 'Postgres', recommended: true }, { id: 'my', label: 'MySQL' }] }
const langs: ReactQuestion = { id: 'lang', prompt: '支持哪些语言?', allowMultiple: true, options: [{ id: 'zh', label: '中文' }, { id: 'en', label: 'English' }] }

describe('SoloAskCard', () => {
  it('preselects the recommendation and answers in the clarify choice format', async () => {
    const wrapper = mountCard({ questions: [db, langs] })
    expect(wrapper.get('[data-testid="solo-ask-option-db-pg"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-testid="solo-ask-option-db-my"]').trigger('click')
    await wrapper.get('[data-testid="solo-ask-option-lang-zh"]').trigger('click')
    await wrapper.get('[data-testid="solo-ask-option-lang-en"]').trigger('click')
    await wrapper.get('[data-testid="solo-ask-other-lang"]').setValue('日本語')
    await wrapper.get('[data-testid="solo-ask-submit"]').trigger('click')
    expect(wrapper.emitted('answer')).toEqual([['我的选择:\n- 用哪个数据库? → MySQL\n- 支持哪些语言? → 中文、English、日本語']])
  })

  it('lets free text replace a single choice and skips unanswered questions', async () => {
    const wrapper = mountCard({ questions: [db, langs] })
    await wrapper.get('[data-testid="solo-ask-other-db"]').setValue('SQLite')
    await wrapper.get('[data-testid="solo-ask-submit"]').trigger('click')
    expect(wrapper.emitted('answer')).toEqual([['我的选择:\n- 用哪个数据库? → SQLite']])
  })

  it('submits a form only when every field is filled', async () => {
    const wrapper = mountCard({ forms: [{ fields: [{ name: 'url', label: '地址', type: 'url' }, { name: 'user', label: '账号' }] }] })
    const submit = wrapper.get('[data-testid="solo-ask-form-submit"]')
    await wrapper.get('[data-testid="solo-ask-field-url"]').setValue('https://example.com')
    expect(submit.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="solo-ask-field-user"]').setValue('ada')
    await submit.trigger('click')
    expect(wrapper.emitted('answer')).toEqual([['我的填写:\n- 地址 → https://example.com\n- 账号 → ada']])
  })

  it('does not answer while disabled', async () => {
    const wrapper = mountCard({ questions: [db], disabled: true })
    await wrapper.get('[data-testid="solo-ask-submit"]').trigger('click')
    expect(wrapper.emitted('answer')).toBeUndefined()
  })
})

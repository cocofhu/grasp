// @vitest-environment happy-dom
import { beforeAll, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import OnboardingTeamCard from './OnboardingTeamCard.vue'
import OnboardingWorkflowPreview from './OnboardingWorkflowPreview.vue'
import { i18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import {
  ONBOARDING_AGENT_NAMES,
  applyDefaultTeamNames,
  buildOnboardingWorkflowPreview,
  freshOnboardingTeam,
} from '@/lib/pm/onboardingWizard'

beforeAll(async () => {
  const [zh, en] = await Promise.all([loadLocaleMessages('zh-CN'), loadLocaleMessages('en')])
  i18n.global.setLocaleMessage('zh-CN', zh)
  i18n.global.setLocaleMessage('en', en)
})

function mountCard(templateId: 'clarify' | 'test_review', locale: 'zh-CN' | 'en') {
  i18n.global.locale.value = locale
  const member = freshOnboardingTeam().find((m) => m.templateId === templateId)!
  return mount(OnboardingTeamCard, {
    props: {
      member,
      capabilities:
        templateId === 'clarify'
          ? {
              interaction: 'clarify',
              tools: ['ask_question', 'set_preview'],
              writes: [{ schema: 'plan', required: true }, { schema: 'research' }],
            }
          : { interaction: 'auto', review: true, writes: [{ schema: 'test_result', required: true }] },
      modelPlaceholder: 'm',
    },
    global: { plugins: [i18n] },
  })
}

describe('OnboardingTeamCard', () => {
  it('renders localized title, tools, products and the preview badge', () => {
    const zh = mountCard('clarify', 'zh-CN')
    expect(zh.text()).toContain('需求澄清')
    expect(zh.text()).toContain('提问澄清')
    expect(zh.text()).toContain('应用预览')
    expect(zh.text()).toContain('可启动应用预览')
    expect(zh.text()).toContain('调研?')
    expect(zh.text()).toContain('必选')

    const en = mountCard('test_review', 'en')
    expect(en.text()).toContain('Test & review')
    expect(en.text()).toContain('Human review after run')
    expect(en.text()).toContain('No preview')
    expect(en.text()).toContain('Optional')
  })

  it('emits toggle, name and model edits', async () => {
    const w = mountCard('test_review', 'zh-CN')
    await w.find('[data-testid="onboarding-team-toggle-test_review"]').setValue(false)
    await w.find('[data-testid="onboarding-team-name-test_review"]').setValue('评审')
    await w.find('[data-testid="onboarding-team-model-test_review"]').setValue('gpt-5')
    expect(w.emitted('toggle')?.[0]).toEqual([false])
    expect(w.emitted('update:name')?.[0]).toEqual(['评审'])
    expect(w.emitted('update:model')?.[0]).toEqual(['gpt-5'])
  })
})

describe('OnboardingWorkflowPreview', () => {
  it('draws pass/fail edges with localized labels', () => {
    i18n.global.locale.value = 'zh-CN'
    const team = freshOnboardingTeam()
    applyDefaultTeamNames(team, [...ONBOARDING_AGENT_NAMES])
    const w = mount(OnboardingWorkflowPreview, {
      props: { preview: buildOnboardingWorkflowPreview(team) },
      global: { plugins: [i18n] },
    })
    expect(w.find('[data-testid="onboarding-preview-node-input"]').text()).toContain('开始')
    expect(w.find('[data-testid="onboarding-preview-node-output"]').text()).toContain('结束')
    expect(w.find('[data-testid="onboarding-preview-edge-test_review-output"]').text()).toBe('通过')
    expect(w.find('[data-testid="onboarding-preview-edge-test_review-implement-fail"]').text()).toBe('未通过')
    expect(w.findAll('path[stroke-dasharray]')).toHaveLength(1)
  })
})

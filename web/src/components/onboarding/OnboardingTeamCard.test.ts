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
      modelConfigurations: [
        {
          id: 'model-review',
          name: 'Review model',
          backend: 'codex',
          binding: { kind: 'connection', connectionId: 'connection', modelId: 'review' },
          state: 'active',
          revision: 1,
        },
      ],
      index: templateId === 'clarify' ? 0 : 2,
    },
    global: { plugins: [i18n], stubs: { teleport: true } },
  })
}

describe('OnboardingTeamCard', () => {
  it('renders localized title, stage, products and the preview line', () => {
    const zh = mountCard('clarify', 'zh-CN')
    expect(zh.text()).toContain('模型组')
    expect(zh.get('[data-test="app-select-trigger"]').attributes('aria-label')).toBe('模型组')
    expect(i18n.global.t('pages.onboarding.team.meta')).toContain('指定模型组')
    expect(i18n.global.t('pages.onboarding.team.modelPlaceholder')).toBe('留空沿用工作区默认')
    expect(i18n.global.t('pages.onboarding.solo.description')).toBe(
      '默认创建一个空白 Solo Agent，使用上面选择的模型与 Git 配置，供 Solo 对话使用。',
    )
    expect(zh.text()).toContain('需求澄清')
    expect(zh.text()).toContain('阶段 1')
    expect(zh.text()).toContain('可启动应用预览')
    expect(zh.text()).toContain('计划')
    expect(zh.text()).toContain('等 1 项可选')
    expect(zh.text()).toContain('必选')
    expect(zh.find('[data-testid="onboarding-team-toggle-clarify"]').exists()).toBe(false)

    const en = mountCard('test_review', 'en')
    expect(en.text()).toContain('Model group')
    expect(en.get('[data-test="app-select-trigger"]').attributes('aria-label')).toBe('Model group')
    expect(i18n.global.t('pages.onboarding.team.meta')).toContain('model group')
    expect(i18n.global.t('pages.onboarding.solo.description')).toBe(
      'An empty Solo Agent is created by default for Solo conversations, using the model and Git settings selected above.',
    )
    expect(en.text()).toContain('Test & review')
    expect(en.text()).toContain('Human review')
    expect(en.text()).toContain('No preview')
    expect(en.text()).toContain('Stage 3')
  })

  it('keeps the name read-only, shows required, and still emits model edits', async () => {
    const w = mountCard('test_review', 'zh-CN')
    expect(w.find('[data-testid="onboarding-team-toggle-test_review"]').exists()).toBe(false)
    expect(w.text()).toContain('必选')
    const name = w.get('[data-testid="onboarding-team-name-test_review"]')
    expect(name.attributes('readonly')).toBeDefined()
    await name.setValue('评审')
    expect(w.emitted('update:name')).toBeUndefined()
    await w.get('[data-testid="onboarding-team-model-test_review"] [data-test="app-select-trigger"]').trigger('click')
    await w.get('[data-test="app-select-option-model-review"]').trigger('click')
    expect(w.emitted('update:model')?.[0]).toEqual(['model-review'])
  })

  it('lists a single selected Tencent group and hides a disabled group', async () => {
    i18n.global.locale.value = 'zh-CN'
    const member = freshOnboardingTeam().find((m) => m.templateId === 'test_review')!
    expect(member.modelConfigurationId).toBe('')
    const w = mount(OnboardingTeamCard, {
      props: {
        member,
        defaultModelId: 'tencent',
        defaultModelBackend: 'cursor',
        modelConfigurations: [
          {
            id: 'tencent',
            name: 'Tencent',
            backend: 'cursor',
            binding: { kind: 'connection', connectionId: 'connection', modelId: 'grok' },
            state: 'active',
            revision: 1,
          },
          {
            id: 'tencent-off',
            name: 'Tencent',
            backend: 'cursor',
            binding: { kind: 'connection', connectionId: 'old', modelId: 'old' },
            state: 'disabled',
            revision: 1,
          },
        ],
        index: 2,
      },
      global: { plugins: [i18n], stubs: { teleport: true } },
    })
    const trigger = w.get('[data-testid="onboarding-team-model-test_review"] [data-test="app-select-trigger"]')
    expect(trigger.text()).toContain('Tencent')
    await trigger.trigger('click')
    const options = w.findAll('[role="option"]')
    expect(options).toHaveLength(1)
    expect(options[0]?.text()).toContain('Tencent')
    expect(options[0]?.attributes('aria-selected')).toBe('true')
    expect(w.find('[data-test="app-select-option-tencent-off"]').exists()).toBe(false)
    await w.get('[data-test="app-select-search"]').setValue('Tencent')
    expect(w.findAll('[role="option"]')).toHaveLength(1)
    await w.get('[data-test="app-select-search"]').setValue('nomatch')
    expect(w.find('[data-test="app-select-empty"]').exists()).toBe(true)
    await w.get('[data-test="app-select-search"]').setValue('')
    await w.get('[data-test="app-select-option-tencent"]').trigger('click')
    expect(w.emitted('update:model')?.[0]).toEqual([''])
    expect(member.modelConfigurationId).toBe('')
  })
})

describe('OnboardingWorkflowPreview', () => {
  it('draws pass/fail edges with localized labels', () => {
    i18n.global.locale.value = 'zh-CN'
    const team = freshOnboardingTeam()
    applyDefaultTeamNames(team, [...ONBOARDING_AGENT_NAMES])
    const w = mount(OnboardingWorkflowPreview, {
      props: { preview: buildOnboardingWorkflowPreview(team) },
      global: { plugins: [i18n], stubs: { teleport: true } },
    })
    expect(w.find('[data-testid="onboarding-preview-node-input"]').text()).toContain('开始')
    expect(w.find('[data-testid="onboarding-preview-node-output"]').text()).toContain('结束')
    expect(w.find('[data-testid="onboarding-preview-edge-test_review-deliver"]').text()).toBe('通过')
    expect(w.find('[data-testid="onboarding-preview-node-deliver"]').text()).toContain('交付')
    expect(w.find('[data-testid="onboarding-preview-edge-test_review-implement-fail"]').text()).toBe('未通过')
    expect(w.findAll('path[stroke-dasharray]')).toHaveLength(1)
  })
})

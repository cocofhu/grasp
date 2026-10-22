// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import ReactConnectingState from './ReactConnectingState.vue'

function mountSidebar(locale: 'zh-CN' | 'en') {
  const i18n = locale === 'zh-CN'
    ? createI18n({
        legacy: false,
        locale,
        messages: { 'zh-CN': { ...common, ...pages } },
      })
    : createI18n({
        legacy: false,
        locale,
        messages: { en: { ...enCommon, ...enPages } },
      })
  return mount(ReactConnectingState, {
    global: {
      plugins: [i18n],
      stubs: { Icon: true },
    },
  })
}

function mountStage(
  locale: 'zh-CN' | 'en' = 'zh-CN',
  props: { sandboxPhase?: string | null } = {},
) {
  const i18n = locale === 'zh-CN'
    ? createI18n({
        legacy: false,
        locale,
        messages: { 'zh-CN': { ...common, ...pages } },
      })
    : createI18n({
        legacy: false,
        locale,
        messages: { en: { ...enCommon, ...enPages } },
      })
  return mount(ReactConnectingState, {
    props: { mode: 'stage', ...props },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('ReactConnectingState', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('shows the Chinese staged loader with all actions disabled', async () => {
    vi.useFakeTimers()
    const wrapper = mountSidebar('zh-CN')
    // No meta row above the transcript (item 13): the boot loader is the only status.
    expect(wrapper.find('[data-testid="react-connecting-pill"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('连接中…')
    expect(wrapper.text()).not.toContain('多轮 ReAct')
    expect(wrapper.text()).not.toContain('共 0 条')
    expect(wrapper.find('icon-stub[name="chat"]').exists()).toBe(false)
    // Status sits in the transcript column as the chat's folded tool row (spinner + checklist), not a centered loader.
    expect(wrapper.get('[data-testid="clarify-boot-loader"]').attributes('data-variant')).toBe('inline')
    expect(wrapper.get('[data-testid="clarify-boot-head"]').find('icon-stub[name="arc-spinner"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    // No boot record: the checklist does not pace itself — only 启动沙箱 is in progress, no times.
    expect(wrapper.text()).toContain('正在准备工作环境…')
    vi.advanceTimersByTime(2600 * 5)
    await nextTick()
    const steps = wrapper.findAll('[data-testid="clarify-boot-step"]')
    expect(steps.map((s) => s.attributes('data-state'))).toEqual(['running', 'pending', 'pending'])
    expect(steps.map((s) => s.text())).toEqual(['启动沙箱', '准备工作区', '连接 Agent'])
    expect(wrapper.find('[data-testid="clarify-boot-elapsed"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="clarify-boot-title"]').text()).toBe('正在启动 Agent')
    const input = wrapper.get('[data-testid="react-connecting-input"]')
    expect(input.attributes('placeholder')).toBe('连接中，暂不可输入')
    expect((input.element as HTMLTextAreaElement).disabled).toBe(true)
    expect((wrapper.get('[data-testid="react-connecting-send"]').element as HTMLButtonElement).disabled).toBe(true)
    // State #1: exactly one primary action, a disabled 发送 (no 确认并流转 while connecting).
    expect(wrapper.findAll('.composer-primary')).toHaveLength(1)
    expect(wrapper.find('[data-testid="react-connecting-confirm"]').exists()).toBe(false)
  })

  it('shows the English connecting copy', () => {
    const wrapper = mountSidebar('en')
    expect(wrapper.find('[data-testid="react-connecting-pill"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="clarify-boot-title"]').text()).toBeTruthy()
    expect(wrapper.text()).not.toContain('Multi-turn ReAct')
    expect(wrapper.text()).not.toMatch(/\d+ messages/)
    expect(wrapper.get('[data-testid="react-connecting-input"]').attributes('placeholder')).toContain('Connecting')
  })

  it('keeps the first step static when reduced motion is preferred', async () => {
    vi.useFakeTimers()
    vi.spyOn(window, 'matchMedia').mockReturnValue({
      matches: true,
      media: '(prefers-reduced-motion: reduce)',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })
    const wrapper = mountSidebar('zh-CN')
    vi.advanceTimersByTime(10_400)
    await nextTick()
    expect(wrapper.text()).toContain('正在准备工作环境…')
    expect(wrapper.text()).not.toContain('正在启动 Agent…')
  })

  it('pins pull-image copy when sandboxPhase is pulling (g3.1)', async () => {
    vi.useFakeTimers()
    const wrapper = mount(ReactConnectingState, {
      props: { sandboxPhase: 'pulling' },
      global: {
        plugins: [createI18n({
          legacy: false,
          locale: 'zh-CN',
          messages: { 'zh-CN': { ...common, ...pages } },
        })],
        stubs: { Icon: true },
      },
    })
    expect(wrapper.find('[data-testid="react-connecting-pill"]').exists()).toBe(false)
    await nextTick()
    // Pulling is shown once, as the checklist head's current phase (sandbox step in progress).
    expect(wrapper.get('[data-testid="clarify-boot-context"]').text()).toBe('正在拉取镜像…')
    // Visible once; the sr-only live region announces the same phase.
    const status = wrapper.get('[data-testid="clarify-boot-status"]')
    expect(status.attributes('role')).toBe('status')
    expect(status.classes()).toContain('sr-only')
    expect(status.text()).toBe('正在拉取镜像…')
    expect(wrapper.get('[data-testid="clarify-boot-head"]').attributes('role')).toBeUndefined()
    expect(wrapper.text().replace(status.text(), '').split('正在拉取镜像…')).toHaveLength(2)
    vi.advanceTimersByTime(10_400)
    await nextTick()
    expect(wrapper.get('[data-testid="clarify-boot-context"]').text()).toBe('正在拉取镜像…')
    expect(wrapper.text()).not.toContain('正在连接 Agent…')
  })

  // g1.1 / g1.2 / g2.1 / g3.2 / g4.1 / g4.2 — stage centered status (no skeleton)
  it('stage chrome shows centered spinner and two-line copy without placeholders (g1.1 g1.2 g2.1 g3.2 g4.1 g4.2)', () => {
    const wrapper = mountStage('zh-CN')
    expect(wrapper.get('[data-testid="react-connecting-stage"]').attributes('aria-busy')).toBe('true')
    const workflow = wrapper.get('[data-testid="react-connecting-tab-workflow"]')
    expect(workflow.element.tagName).toBe('BUTTON')
    expect(workflow.attributes('role')).toBe('tab')
    expect(workflow.attributes('aria-selected')).toBe('true')
    expect(workflow.text()).toContain('工作流产物')
    expect(wrapper.find('[data-testid="react-connecting-tab-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-connecting-workflow-skeleton"]').exists()).toBe(false)
    expect(wrapper.find('.animate-pulse').exists()).toBe(false)

    const spinner = wrapper.get('[data-testid="react-connecting-stage-spinner"]')
    expect(spinner.attributes('name')).toBe('spinner')
    expect(spinner.attributes('size')).toBe('28')
    expect(spinner.classes()).toContain('animate-spin')
    expect(spinner.classes()).toContain('text-accent')

    expect(wrapper.get('[data-testid="react-connecting-stage-title"]').text()).toBe('正在准备会话')
    const hint = wrapper.get('[data-testid="react-connecting-stage-hint"]')
    expect(hint.text()).toBe('产物会在会话就绪后出现在这里')
    expect(hint.attributes('role')).toBe('status')
    expect(wrapper.text()).not.toContain('正在连接 Agent…')
  })

  // g2.2 — pulling only swaps title
  it('stage pulling swaps title only and keeps shared hint (g2.2)', () => {
    const wrapper = mountStage('zh-CN', { sandboxPhase: 'pulling' })
    expect(wrapper.get('[data-testid="react-connecting-stage-title"]').text()).toBe('正在拉取镜像')
    expect(wrapper.get('[data-testid="react-connecting-stage-hint"]').text()).toBe(
      '产物会在会话就绪后出现在这里',
    )
    expect(wrapper.text()).not.toContain('正在启动 Agent…')
    expect(wrapper.text()).not.toContain('正在连接 Agent…')
    expect(wrapper.text()).not.toContain('正在整理第一轮问题…')
  })

  it('stage chrome English shows Preparing / Pulling titles (g2.1 g2.2)', () => {
    const ready = mountStage('en')
    expect(ready.get('[data-testid="react-connecting-tab-workflow"]').attributes('aria-selected')).toBe('true')
    expect(ready.get('[data-testid="react-connecting-tab-workflow"]').text()).toContain('Workflow artifacts')
    expect(ready.find('[data-testid="react-connecting-tab-preview"]').exists()).toBe(false)
    expect(ready.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(ready.get('[data-testid="react-connecting-stage-title"]').text()).toBe('Preparing the session')
    expect(ready.get('[data-testid="react-connecting-stage-hint"]').text()).toBe(
      'Artifacts will appear here when the session is ready',
    )

    const pulling = mountStage('en', { sandboxPhase: 'pulling' })
    expect(pulling.get('[data-testid="react-connecting-stage-title"]').text()).toBe('Pulling the image')
    expect(pulling.get('[data-testid="react-connecting-stage-hint"]').text()).toBe(
      'Artifacts will appear here when the session is ready',
    )
  })

  // g3.1 — reduced motion stops stage spinner rotation
  it('stage spinner does not rotate when reduced motion is preferred (g3.1)', async () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({
      matches: true,
      media: '(prefers-reduced-motion: reduce)',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })
    const wrapper = mountStage('zh-CN')
    await nextTick()
    const spinner = wrapper.get('[data-testid="react-connecting-stage-spinner"]')
    expect(spinner.classes()).not.toContain('animate-spin')
    expect(wrapper.get('[data-testid="react-connecting-stage-title"]').text()).toBe('正在准备会话')
    expect(wrapper.get('[data-testid="react-connecting-stage-hint"]').text()).toBe(
      '产物会在会话就绪后出现在这里',
    )
    expect(wrapper.get('[data-testid="react-connecting-stage"]').attributes('aria-busy')).toBe('true')
  })

  it('sidebar connecting stays ClarifyBootLoader without preview tabs (f6)', () => {
    const wrapper = mountSidebar('zh-CN')
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-connecting-tab-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="clarify-boot-checklist"]').exists()).toBe(true)
  })

  it('sidebar connecting uses the chat skeleton: transcript column + disabled ComposerShell', () => {
    const wrapper = mountSidebar('zh-CN')
    const root = wrapper.get('[data-testid="react-connecting-sidebar"]')
    expect(root.classes()).toContain('chat-thread')
    expect(wrapper.find('.react-connecting-transcript [data-testid="clarify-boot-loader"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-connecting-actions"] [data-testid="composer-shell"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-connecting-input-row"]').classes()).toContain('chat-composer')
    const input = wrapper.get('[data-testid="react-connecting-input"]')
    expect(input.classes()).toContain('chat-input')
    expect((input.element as HTMLTextAreaElement).disabled).toBe(true)
    expect(wrapper.get('[data-testid="react-connecting-send"]').classes()).toContain('chat-action-primary')
    expect(wrapper.get('[data-testid="react-connecting-send"]').attributes('data-kind')).toBe('send')
    expect(wrapper.findAll('.composer-primary')).toHaveLength(1)
    expect((wrapper.get('[data-testid="react-connecting-attach"]').element as HTMLButtonElement).disabled).toBe(true)
  })
})

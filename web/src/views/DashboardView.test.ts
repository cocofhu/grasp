// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Workflow } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  listWorkflows: vi.fn(),
  listProjects: vi.fn(),
  patchWorkflowHomeVisibility: vi.fn(),
  startRun: vi.fn(),
  getRun: vi.fn(),
  reactReply: vi.fn(),
  createWorkflowFromBaseline: vi.fn(),
  readStoredProjectId: vi.fn(() => 'proj-1'),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.push }),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listWorkflows: mocks.listWorkflows,
      listProjects: mocks.listProjects,
      patchWorkflowHomeVisibility: mocks.patchWorkflowHomeVisibility,
      startRun: mocks.startRun,
      getRun: mocks.getRun,
      reactReply: mocks.reactReply,
      createWorkflowFromBaseline: mocks.createWorkflowFromBaseline,
    },
  }
})

vi.mock('@/lib/composables/useProjectContext', () => ({
  readStoredProjectId: () => mocks.readStoredProjectId(),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ warn: vi.fn(), error: vi.fn(), success: vi.fn() }),
}))

import { HOME_COMPOSER_DRAFT_KEY } from '@/lib/run/homeComposerDraft'
import { HOME_PRIORITY_MEMORY_KEY } from '@/lib/run/useHomeApproveChat'
import { setBrandSettings } from '@/lib/composables/useBrandSettings'
import DashboardView from './DashboardView.vue'
import dashboardSource from './DashboardView.vue?raw'

const HomePreviewAppModalStub = {
  props: ['open', 'title', 'width'],
  emits: ['close'],
  template: `
    <div v-if="open" data-testid="home-image-preview-modal">
      <div data-testid="home-image-preview-title">{{ title }}</div>
      <button type="button" data-testid="home-image-preview-close" @click="$emit('close')">×</button>
      <button type="button" data-testid="home-image-preview-backdrop" @click="$emit('close')">backdrop</button>
      <slot />
      <slot name="footer" />
    </div>
  `,
}

const approveWf: Workflow = {
  id: 'wf-ap',
  name: '自我迭代PRO',
  description: '开发前澄清 + 计划',
  status: 'published',
  version: 1,
  updatedAt: '',
  needsRepo: false,
  projectId: 'proj-1',
  showOnHome: true,
  nodes: [
    { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
    { id: 'ap', type: 'approve', label: '澄清', position: { x: 0, y: 0 }, config: {} },
  ],
  edges: [{ id: 'e1', source: 'in', target: 'ap' }],
}

function mountDashboard() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(DashboardView, {
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        RunLaunchModal: true,
        AppModal: HomePreviewAppModalStub,
        Teleport: false,
      },
    },
  })
}

/** HomePipelineSelect teleports its panel to document.body. */
function teleported(testid: string) {
  const el = document.querySelector(`[data-testid="${testid}"]`)
  if (!el) {
    throw new Error(`Unable to get [data-testid="${testid}"] (teleported to body)`)
  }
  return new DOMWrapper(el)
}

function teleportedExists(testid: string) {
  return document.querySelector(`[data-testid="${testid}"]`) != null
}

function stubReducedMotion(matches: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: query.includes('prefers-reduced-motion') ? matches : false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  )
}

describe('DashboardView home composer', () => {
  beforeEach(() => {
    mocks.push.mockReset()
    mocks.listWorkflows.mockReset()
    mocks.listProjects.mockReset()
    mocks.patchWorkflowHomeVisibility.mockReset()
    mocks.startRun.mockReset()
    mocks.getRun.mockReset()
    mocks.reactReply.mockReset()
    mocks.createWorkflowFromBaseline.mockReset()
    mocks.readStoredProjectId.mockReturnValue('proj-1')
    mocks.listWorkflows.mockResolvedValue([approveWf])
    mocks.listProjects.mockResolvedValue([{ id: 'proj-1', name: '综合项目组', description: '', variables: [] }])
    mocks.patchWorkflowHomeVisibility.mockResolvedValue({ ...approveWf, showOnHome: false })
    mocks.startRun.mockResolvedValue({ id: 'run-9', status: 'queued' })
    mocks.getRun.mockResolvedValue({
      id: 'run-9',
      status: 'waiting_human',
      nodes: [{ id: 'ap', type: 'approve', label: '', position: { x: 0, y: 0 }, config: {} }],
      nodeRuns: { ap: { nodeId: 'ap', status: 'waiting_human' } },
    })
    mocks.reactReply.mockResolvedValue({ status: 'ok' })
    stubReducedMotion(false)
    setBrandSettings(null)
    localStorage.removeItem(HOME_COMPOSER_DRAFT_KEY)
    localStorage.removeItem(HOME_PRIORITY_MEMORY_KEY)
    vi.useFakeTimers()
  })

  afterEach(() => {
    localStorage.removeItem(HOME_COMPOSER_DRAFT_KEY)
    localStorage.removeItem(HOME_PRIORITY_MEMORY_KEY)
    vi.useRealTimers()
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('renders composer and approve-first cards without a project gate', async () => {
    mocks.readStoredProjectId.mockReturnValue('')
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-title"]').text()).toContain('从一句话开始一次开发前澄清')
    expect(wrapper.find('[data-testid="home-composer"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-no-project"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').text()).toContain('自我迭代PRO')
    expect(wrapper.get('[data-testid="home-pipeline-card-project-wf-ap"]').text()).toBe('综合项目组')
    expect(mocks.listWorkflows).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const call = mocks.listWorkflows.mock.calls[0]?.[0] || {}
    expect(call).not.toHaveProperty('projectId')
    wrapper.unmount()
  })

  // plan g2.1 — card title = workflow name, next line = project name; same workflow name, different projects
  it('shows workflow name above project name and distinguishes same-named workflows', async () => {
    const other: Workflow = {
      ...approveWf,
      id: 'wf-ap-b',
      name: '自我迭代PRO',
      projectId: 'proj-2',
      description: '另一项目的同名工作流',
    }
    mocks.listWorkflows.mockResolvedValue([approveWf, other])
    mocks.listProjects.mockResolvedValue([
      { id: 'proj-1', name: '综合项目组', description: '', variables: [] },
      { id: 'proj-2', name: 'SkillHub', description: '', variables: [] },
    ])
    const wrapper = mountDashboard()
    await flushPromises()
    const a = wrapper.get('[data-testid="home-pipeline-card-wf-ap"]')
    const b = wrapper.get('[data-testid="home-pipeline-card-wf-ap-b"]')
    expect(a.get('[data-testid="home-pipeline-card-name"]').text()).toBe('自我迭代PRO')
    expect(b.get('[data-testid="home-pipeline-card-name"]').text()).toBe('自我迭代PRO')
    expect(wrapper.get('[data-testid="home-pipeline-card-project-wf-ap"]').text()).toBe('综合项目组')
    expect(wrapper.get('[data-testid="home-pipeline-card-project-wf-ap-b"]').text()).toBe('SkillHub')
    expect(a.get('[data-testid="home-pipeline-card-name"]').attributes('title')).toBe('自我迭代PRO')
    expect(wrapper.get('[data-testid="home-pipeline-card-project-wf-ap"]').attributes('title')).toBe(
      '综合项目组',
    )
    wrapper.unmount()
  })

  it('omits the project name row when the pipeline has no projectId', async () => {
    mocks.listWorkflows.mockResolvedValue([{ ...approveWf, projectId: undefined }])
    mocks.listProjects.mockResolvedValue([{ id: 'proj-1', name: '综合项目组', description: '', variables: [] }])
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').text()).toContain('自我迭代PRO')
    expect(wrapper.find('[data-testid="home-pipeline-card-project-wf-ap"]').exists()).toBe(false)
    wrapper.unmount()
  })

  // plan g1.1 — no full-bleed purple stage layer; particle mesh bg instead
  it('does not render full-screen purple stage atmosphere', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="home-stage-bg"]').exists()).toBe(false)
    expect(wrapper.find('.home-stage__wash').exists()).toBe(false)
    expect(wrapper.find('.home-stage__glow').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders particle mesh background layer behind content', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const bg = wrapper.find('[data-testid="home-particle-mesh-bg"]')
    expect(bg.exists()).toBe(true)
    expect(bg.classes()).toContain('home-particle-mesh')
    expect(wrapper.find('[data-testid="home-composer"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // plan g2 — Grasp mono brand + Chinese hint；无筛选说明句
  it('renders monospace Grasp brand and Chinese hint', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-brand"]').classes()).toContain('home-brand')
    expect(wrapper.get('[data-testid="home-title"]').text()).toBe('从一句话开始一次开发前澄清')
    expect(wrapper.find('[data-testid="home-subtitle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-filter-hint"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('仅显示开始后是 Approve 的已发布流水线')
    wrapper.unmount()
  })

  // plan g1 — pipeline cards use card-role 12px via scoped CSS (not global .card)
  it('renders rounded pipeline cards via home-shell__card', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const card = wrapper.get('[data-testid="home-pipeline-card-wf-ap"]')
    expect(card.classes()).toContain('home-shell__card')
    expect(card.classes()).not.toContain('card')
    expect(card.classes()).toContain('border')
    wrapper.unmount()
  })

  it('opens the icon menu on contextmenu and prevents the browser menu', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const card = wrapper.get('[data-testid="home-pipeline-card-wf-ap"]')
    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 48,
      clientY: 72,
    })
    card.element.dispatchEvent(event)
    await flushPromises()

    expect(event.defaultPrevented).toBe(true)
    const menu = teleported('home-pipeline-menu')
    expect(menu.text()).toContain('隐藏')
    expect(menu.text()).toContain('编辑')
    expect(teleported('home-pipeline-menu-hide').find('svg').exists()).toBe(true)
    expect(teleported('home-pipeline-menu-edit').find('svg').exists()).toBe(true)
    wrapper.unmount()
  })

  it('opens the same menu from more without changing the selected pipeline and edits the target', async () => {
    const second: Workflow = { ...approveWf, id: 'wf-lite', name: '快速澄清 Lite' }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    const wrapper = mountDashboard()
    await flushPromises()

    await wrapper.get('[data-testid="home-pipeline-more-wf-lite"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').classes()).toContain(
      'home-shell__card--selected',
    )
    expect(teleportedExists('home-pipeline-menu')).toBe(true)
    await teleported('home-pipeline-menu-edit').trigger('click')
    expect(mocks.push).toHaveBeenCalledWith('/workflows/wf-lite/edit')
    expect(teleportedExists('home-pipeline-menu')).toBe(false)
    wrapper.unmount()
  })

  it('opens on a 500ms touch hold and cancels when the finger moves', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const card = wrapper.get('[data-testid="home-pipeline-card-wf-ap"]')

    await card.trigger('pointerdown', { pointerType: 'touch', clientX: 20, clientY: 20 })
    await card.trigger('pointermove', { pointerType: 'touch', clientX: 40, clientY: 20 })
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    expect(teleportedExists('home-pipeline-menu')).toBe(false)

    await card.trigger('pointerdown', { pointerType: 'touch', clientX: 20, clientY: 20 })
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    expect(teleportedExists('home-pipeline-menu')).toBe(true)
    await card.trigger('click')
    await flushPromises()
    expect(card.classes()).toContain('home-shell__card--selected')
    wrapper.unmount()
  })

  it('hides a pipeline through home-visibility and falls back to the next card', async () => {
    const second: Workflow = { ...approveWf, id: 'wf-lite', name: '快速澄清 Lite' }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    const wrapper = mountDashboard()
    await flushPromises()

    await wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').trigger('contextmenu')
    await teleported('home-pipeline-menu-hide').trigger('click')
    await flushPromises()
    expect(mocks.patchWorkflowHomeVisibility).toHaveBeenCalledWith('wf-ap', false)
    expect(wrapper.find('[data-testid="home-pipeline-card-wf-ap"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-lite"]').classes()).toContain(
      'home-shell__card--selected',
    )
    wrapper.unmount()
  })

  // plan g3 — one-shot typewriter then opacity-hide caret (keep layout box)
  it('types Grasp once then settles without looping', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-brand-text"]').text()).toBe('')
    await vi.advanceTimersByTimeAsync(220 + 78 * 9 + 50)
    expect(wrapper.get('[data-testid="home-brand-text"]').text()).toBe('Grasp')
    const caret = wrapper.get('[data-testid="home-brand-cursor"]')
    expect(caret.classes()).not.toContain('home-brand__cursor--gone')
    await vi.advanceTimersByTimeAsync(850 * 3 + 50)
    expect(wrapper.get('[data-testid="home-brand-text"]').text()).toBe('Grasp')
    expect(wrapper.get('[data-testid="home-brand-cursor"]').classes()).toContain('home-brand__cursor--gone')
    expect(wrapper.get('[data-testid="home-brand-cursor"]').classes()).not.toContain('home-brand__cursor--blink')
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.get('[data-testid="home-brand-text"]').text()).toBe('Grasp')
    expect(wrapper.get('[data-testid="home-brand-cursor"]').classes()).toContain('home-brand__cursor--gone')
    wrapper.unmount()
  })

  // plan g3 — reduced-motion shows static brand; caret stays in layout but gone
  it('shows full Grasp immediately under reduced-motion', async () => {
    stubReducedMotion(true)
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-brand-text"]').text()).toBe('Grasp')
    expect(wrapper.get('[data-testid="home-brand-cursor"]').classes()).toContain('home-brand__cursor--gone')
    wrapper.unmount()
  })

  it('uses the same configured product name for typewriter, static mode, and aria label', async () => {
    setBrandSettings({ product_name: 'Acme Flow' })
    stubReducedMotion(true)
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-brand-text"]').text()).toBe('Acme Flow')
    expect(wrapper.get('[data-testid="home-brand"]').attributes('aria-label')).toBe('Acme Flow')
    wrapper.unmount()
  })

  it('uses a configured home subtitle and falls back to the locale message when blank', async () => {
    setBrandSettings({ home_subtitle: 'Clarify together' })
    const custom = mountDashboard()
    await flushPromises()
    expect(custom.get('[data-testid="home-title"]').text()).toBe('Clarify together')
    custom.unmount()

    setBrandSettings({ home_subtitle: '　 ' })
    const fallback = mountDashboard()
    await flushPromises()
    expect(fallback.get('[data-testid="home-title"]').text()).toBe('从一句话开始一次开发前澄清')
    fallback.unmount()
  })

  // plan g1.2 — placeholder typewriter when idle/empty
  it('shows placeholder typewriter when empty and unfocused', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="home-composer-placeholder"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(80 + 70 * 9 + 50)
    expect(wrapper.get('[data-testid="home-composer-placeholder"]').text()).toContain('快速开启你的迭代')
    await wrapper.get('[data-testid="home-composer-input"]').trigger('focus')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-composer-placeholder"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('configures multiple dashboard placeholder lines in i18n', () => {
    const lines = pages.pages.dashboard.placeholders as string[]
    expect(lines.length).toBe(5)
    expect(lines[0]).toBe('快速开启你的迭代')
    expect(lines[1]).toContain('说一个功能')
  })

  it('cycles placeholder typewriter to the next configured line', async () => {
    const lines = pages.pages.dashboard.placeholders as string[]
    const wrapper = mountDashboard()
    await flushPromises()
    const firstLen = lines[0].length
    const advanceMs = 80 + 70 * firstLen + 1800 + 32 * firstLen + 400 + 70 * 5
    await vi.advanceTimersByTimeAsync(advanceMs)
    expect(wrapper.get('[data-testid="home-composer-placeholder"]').text()).toContain(lines[1].slice(0, 4))
    wrapper.unmount()
  })

  it('shows static first placeholder under reduced-motion', async () => {
    stubReducedMotion(true)
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-composer-placeholder"]').text()).toContain('快速开启你的迭代')
    expect(wrapper.get('label.sr-only').text()).toContain('快速开启你的迭代')
    wrapper.unmount()
  })

  // plan g2.1 — no project gate; still loads cross-project pipelines
  it('loads pipelines without a stored project and does not show project empty state', async () => {
    mocks.readStoredProjectId.mockReturnValue('')
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="home-no-project"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-pipeline-cards"]').exists()).toBe(true)
    expect(mocks.listWorkflows).toHaveBeenCalled()
    wrapper.unmount()
  })

  // plan g2.2 — combobox and card selection stay in sync
  it('keeps pipeline combobox and card selection in sync', async () => {
    const second: Workflow = {
      ...approveWf,
      id: 'wf-lite',
      name: '快速澄清 Lite',
      description: '轻量 Approve 入口',
    }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    const wrapper = mountDashboard()
    await flushPromises()
    const trigger = wrapper.get('[data-testid="home-pipeline-select-trigger"]')
    expect(trigger.text()).toContain('自我迭代PRO')
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').classes()).toContain(
      'home-shell__card--selected',
    )
    await wrapper.get('[data-testid="home-pipeline-card-wf-lite"]').trigger('click')
    await flushPromises()
    expect(trigger.text()).toContain('快速澄清 Lite')
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-lite"]').classes()).toContain(
      'home-shell__card--selected',
    )
    await trigger.trigger('click')
    await flushPromises()
    await teleported('home-pipeline-select-option-wf-ap').trigger('click')
    await flushPromises()
    expect(trigger.text()).toContain('自我迭代PRO')
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').classes()).toContain(
      'home-shell__card--selected',
    )
    wrapper.unmount()
  })

  it('filters pipelines by keyword in the combobox search', async () => {
    const second: Workflow = {
      ...approveWf,
      id: 'wf-lite',
      name: '快速澄清 Lite',
      description: '轻量 Approve 入口',
    }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.get('[data-testid="home-pipeline-select-trigger"]').trigger('click')
    await flushPromises()
    const search = teleported('home-pipeline-select-search')
    await search.setValue('Lite')
    await flushPromises()
    expect(teleportedExists('home-pipeline-select-option-wf-lite')).toBe(true)
    expect(teleportedExists('home-pipeline-select-option-wf-ap')).toBe(false)
    await search.setValue('nomatch-xyz')
    await flushPromises()
    expect(teleportedExists('home-pipeline-select-empty')).toBe(true)
    expect(teleported('home-pipeline-select-empty').text()).toContain('无匹配流水线')
    await search.setValue('')
    await flushPromises()
    expect(teleportedExists('home-pipeline-select-option-wf-ap')).toBe(true)
    wrapper.unmount()
  })

  it('filters the home combobox by project name as well as workflow name (plan g2.1/g2.2)', async () => {
    const second: Workflow = {
      ...approveWf,
      id: 'wf-lite',
      name: '默认工作流',
      projectId: 'proj-2',
      description: '轻量 Approve 入口',
    }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    mocks.listProjects.mockResolvedValue([
      { id: 'proj-1', name: '综合项目组', description: '', variables: [] },
      { id: 'proj-2', name: 'SkillHub', description: '', variables: [] },
    ])
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-pipeline-select-trigger"]').text()).toContain(
      '综合项目组 · 自我迭代PRO',
    )
    await wrapper.get('[data-testid="home-pipeline-select-trigger"]').trigger('click')
    await flushPromises()
    const search = teleported('home-pipeline-select-search')
    await search.setValue('Skill')
    await flushPromises()
    expect(teleportedExists('home-pipeline-select-option-wf-lite')).toBe(true)
    expect(teleportedExists('home-pipeline-select-option-wf-ap')).toBe(false)
    wrapper.unmount()
  })

  it('selects pipeline from combobox via Enter after keyword filter', async () => {
    const second: Workflow = {
      ...approveWf,
      id: 'wf-lite',
      name: '快速澄清 Lite',
      description: '轻量 Approve 入口',
    }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.get('[data-testid="home-pipeline-select-trigger"]').trigger('click')
    await flushPromises()
    const search = teleported('home-pipeline-select-search')
    await search.setValue('Lite')
    await flushPromises()
    await search.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.get('[data-testid="home-pipeline-select-trigger"]').text()).toContain('快速澄清 Lite')
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-lite"]').classes()).toContain(
      'home-shell__card--selected',
    )
    wrapper.unmount()
  })

  // plan g1.3 / g3.2 — empty list: trigger stays openable for create (not disabled)
  it('keeps pipeline combobox openable when no pipelines are available', async () => {
    mocks.listWorkflows.mockResolvedValue([])
    const wrapper = mountDashboard()
    await flushPromises()
    const triggerEl = wrapper.get('[data-testid="home-pipeline-select-trigger"]').element as HTMLButtonElement
    expect(triggerEl.disabled).toBe(false)
    expect(wrapper.get('[data-testid="home-pipeline-select-trigger"]').text()).toContain('未选择流水线')
    await wrapper.get('[data-testid="home-pipeline-select-trigger"]').trigger('click')
    await flushPromises()
    expect(teleportedExists('home-pipeline-select-panel')).toBe(true)
    expect(teleportedExists('home-pipeline-select-create')).toBe(true)
    wrapper.unmount()
  })

  it('shows pipeline empty state when none are approve-first', async () => {
    mocks.listWorkflows.mockResolvedValue([
      {
        ...approveWf,
        id: 'wf-react',
        name: '实现',
        nodes: [
          { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
          { id: 'r', type: 'react', label: '实现', position: { x: 0, y: 0 }, config: {} },
        ],
        edges: [{ id: 'e1', source: 'in', target: 'r' }],
      },
    ])
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="home-pipelines-empty"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="home-pipelines-empty"]').text()).not.toContain('选择项目')
    expect(wrapper.find('[data-testid="home-go-projects"]').exists()).toBe(true)
    await wrapper.get('[data-testid="home-go-projects"]').trigger('click')
    expect(mocks.push).toHaveBeenCalledWith('/projects')
    wrapper.unmount()
  })

  it('shows empty state prompting project Show on Home when pipelines are hidden (g3.2 / g3.3)', async () => {
    mocks.listWorkflows.mockResolvedValue([{ ...approveWf, showOnHome: false }])
    const wrapper = mountDashboard()
    await flushPromises()
    const empty = wrapper.get('[data-testid="home-pipelines-empty"]')
    expect(empty.text()).toContain('首页可见')
    expect(empty.text()).not.toContain('丢失')
    // plan g1.3 — zero visible pipelines: select still openable for create
    expect(wrapper.get('[data-testid="home-pipeline-select-trigger"]').element).toHaveProperty(
      'disabled',
      false,
    )
    wrapper.unmount()
  })

  // plan g1.1 — toolbar chip to the right of pipeline select, default 普通
  it('renders a compact priority chip next to the pipeline select defaulting to 普通', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const trigger = wrapper.get('[data-testid="home-priority-select-trigger"]')
    expect(trigger.text()).toContain('普通')
    expect(trigger.attributes('aria-label')).toBe('优先级')
    expect(wrapper.get('[data-testid="home-priority-select"]').element.compareDocumentPosition(
      wrapper.get('[data-testid="home-pipeline-select"]').element,
    ) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy()
    wrapper.unmount()
  })

  it('disables the priority chip when no home pipelines are visible (plan g1.1)', async () => {
    mocks.listWorkflows.mockResolvedValue([{ ...approveWf, showOnHome: false }])
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[data-testid="home-priority-select-trigger"]').element).toHaveProperty('disabled', true)
    wrapper.unmount()
  })

  it('sends startRun with high after choosing 高 in the toolbar (plan g2.1 / g3.3)', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.get('[data-testid="home-priority-select-trigger"]').trigger('click')
    await flushPromises()
    await teleported('home-priority-select-option-high').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="home-priority-select-trigger"]').text()).toContain('高')
    expect(wrapper.getComponent({ name: 'RunLaunchModal' }).props('initialPriority')).toBe('high')
    await wrapper.get('[data-testid="home-composer-input"]').setValue('紧急登录')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit')
    await flushPromises()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'high', [], {
      title: '紧急登录',
      firstMessage: { text: '紧急登录', images: [] },
    })
    wrapper.unmount()
  })

  it('sending the first message starts the run and opens inbox', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.get('[data-testid="home-composer-input"]').setValue('把登录做清楚')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit')
    await flushPromises()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: '把登录做清楚',
      firstMessage: { text: '把登录做清楚', images: [] },
    })
    expect(mocks.reactReply).not.toHaveBeenCalled()
    expect(mocks.push).toHaveBeenCalledWith({ path: '/gates', query: { run: 'run-9', node: 'ap', projectId: 'proj-1' } })
    wrapper.unmount()
  })

  // plan g1.1 / g2.2 — Ctrl/⌘+Enter submits; bare Enter / Shift+Enter do not
  it('Ctrl/Meta+Enter submits; bare Enter and Shift+Enter keep the draft for a new line', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const input = wrapper.get('[data-testid="home-composer-input"]')
    await input.setValue('一行需求')

    await input.trigger('keydown', { key: 'Enter', shiftKey: true })
    await flushPromises()
    expect(mocks.startRun).not.toHaveBeenCalled()

    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(mocks.startRun).not.toHaveBeenCalled()

    await input.trigger('keydown', { key: 'Enter', metaKey: true })
    await flushPromises()
    expect(mocks.startRun).toHaveBeenCalledTimes(1)
    mocks.startRun.mockClear()

    await input.setValue('再发一条')
    await input.trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(mocks.startRun).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  // plan g1.2 — IME composing ignores Ctrl/⌘+Enter
  it('does not submit while IME is composing even with Ctrl/Meta+Enter', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const input = wrapper.get('[data-testid="home-composer-input"]')
    await input.setValue('组合输入中')
    await input.trigger('compositionstart')
    await input.trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(mocks.startRun).not.toHaveBeenCalled()
    await input.trigger('keydown', { key: 'Enter', metaKey: true, isComposing: true })
    await flushPromises()
    expect(mocks.startRun).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  // plan g2.1 — send button exposes Ctrl/⌘+Enter shortcut title
  it('send button title advertises Ctrl/⌘+Enter shortcut', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const send = wrapper.get('[data-testid="home-composer-send"]')
    expect(send.attributes('title')).toMatch(/Ctrl\/⌘\s*\+\s*Enter/)
    wrapper.unmount()
  })

  it('plus opens the file picker; paste and attach-only send work', async () => {
    class FakeReader {
      result: string | ArrayBuffer | null = null
      onload: null | (() => void) = null
      readAsDataURL(file: File) {
        this.result = `data:${file.type || 'application/octet-stream'};base64,QUJD`
        queueMicrotask(() => this.onload?.())
      }
    }
    vi.stubGlobal('FileReader', FakeReader as unknown as typeof FileReader)

    const wrapper = mountDashboard()
    await flushPromises()
    const plus = wrapper.get('[data-testid="home-composer-plus"]')
    expect((plus.element as HTMLButtonElement).disabled).toBe(false)
    const fileInput = wrapper.get('[data-testid="home-composer-file"]').element as HTMLInputElement
    expect(fileInput.multiple).toBe(true)
    expect(fileInput.getAttribute('accept')).toBeNull()
    const clickSpy = vi.spyOn(fileInput, 'click')
    await plus.trigger('click')
    expect(clickSpy).toHaveBeenCalled()

    const note = new File(['ABC'], 'note.txt', { type: 'text/plain' })
    const dt = new DataTransfer()
    dt.items.add(note)
    Object.defineProperty(fileInput, 'files', { configurable: true, value: dt.files })
    await wrapper.get('[data-testid="home-composer-file"]').trigger('change')
    await flushPromises()
    expect(wrapper.get('[data-testid="home-pending-file-chip"]').text()).toContain('note.txt')

    const img = new File(['ABC'], 'clip.png', { type: 'image/png' })
    const pasteDt = new DataTransfer()
    pasteDt.items.add(img)
    const pasteEv = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(pasteEv, 'clipboardData', { value: pasteDt })
    wrapper.get('[data-testid="home-composer-input"]').element.dispatchEvent(pasteEv)
    await flushPromises()
    expect(wrapper.find('[data-testid="home-draft-image-thumb"]').exists()).toBe(true)

    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  it('opens draft image preview, close keeps attachment, no unavailable overlay (g1.3)', async () => {
    class FakeReader {
      result: string | ArrayBuffer | null = null
      onload: null | (() => void) = null
      readAsDataURL(file: File) {
        this.result = `data:${file.type || 'application/octet-stream'};base64,QUJD`
        queueMicrotask(() => this.onload?.())
      }
    }
    vi.stubGlobal('FileReader', FakeReader as unknown as typeof FileReader)

    const wrapper = mountDashboard()
    await flushPromises()
    const img = new File(['ABC'], '首页截图.png', { type: 'image/png' })
    const pasteDt = new DataTransfer()
    pasteDt.items.add(img)
    const pasteEv = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(pasteEv, 'clipboardData', { value: pasteDt })
    wrapper.get('[data-testid="home-composer-input"]').element.dispatchEvent(pasteEv)
    await flushPromises()

    const thumb = wrapper.find('[data-testid="home-draft-image-thumb"]')
    expect(thumb.exists()).toBe(true)
    expect(thumb.text()).toContain('点击放大')
    expect(thumb.text()).not.toContain('不可预览')

    await thumb.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-image-preview-title"]').text()).toBe('图片预览 · 首页截图.png')
    expect(wrapper.find('[data-testid="home-image-preview-img"]').attributes('src')).toContain('base64')

    await wrapper.find('[data-testid="home-image-preview-img"]').trigger('error')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-image-preview-failed"]').text()).toContain('图片加载失败')
    await wrapper.find('[data-testid="home-image-preview-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-image-preview-modal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-draft-image-thumb"]').exists()).toBe(true)

    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  it('sends with attachments only and uses the first filename as title', async () => {
    class FakeReader {
      result: string | ArrayBuffer | null = null
      onload: null | (() => void) = null
      readAsDataURL(file: File) {
        this.result = `data:${file.type || 'application/octet-stream'};base64,QUJD`
        queueMicrotask(() => this.onload?.())
      }
    }
    vi.stubGlobal('FileReader', FakeReader as unknown as typeof FileReader)

    const wrapper = mountDashboard()
    await flushPromises()
    const fileInput = wrapper.get('[data-testid="home-composer-file"]').element as HTMLInputElement
    const note = new File(['ABC'], 'brief.pdf', { type: 'application/pdf' })
    const dt = new DataTransfer()
    dt.items.add(note)
    Object.defineProperty(fileInput, 'files', { configurable: true, value: dt.files })
    await wrapper.get('[data-testid="home-composer-file"]').trigger('change')
    await flushPromises()
    await wrapper.get('[data-testid="home-composer"]').trigger('submit')
    await flushPromises()
    expect(mocks.startRun).toHaveBeenCalledWith('wf-ap', {}, 'manual', 'normal', [], {
      title: 'brief.pdf',
      firstMessage: {
        text: '',
        images: expect.arrayContaining([
          expect.objectContaining({ name: 'brief.pdf', mimeType: 'application/pdf' }),
        ]),
      },
    })
    expect(mocks.reactReply).not.toHaveBeenCalled()
    expect(mocks.push).toHaveBeenCalledWith({ path: '/gates', query: { run: 'run-9', node: 'ap', projectId: 'proj-1' } })
    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  // plan g1 — pipeline rail scroll: hidden scrollbar + edge arrows
  it('renders pipeline scroll arrows and hides horizontal scrollbar on cards rail', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="home-pipeline-rail-wrap"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-pipeline-scroll-prev"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-pipeline-scroll-next"]').exists()).toBe(true)
    const rail = wrapper.get('[data-testid="home-pipeline-cards"]')
    expect(rail.classes()).toContain('home-pipeline-rail')
    expect(rail.classes()).not.toContain('overflow-x-auto')
    wrapper.unmount()
  })

  it('disables scroll arrows when pipeline list does not overflow', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const prev = wrapper.get('[data-testid="home-pipeline-scroll-prev"]').element as HTMLButtonElement
    const next = wrapper.get('[data-testid="home-pipeline-scroll-next"]').element as HTMLButtonElement
    expect(prev.disabled).toBe(true)
    expect(next.disabled).toBe(true)
    wrapper.unmount()
  })

  it('syncs scroll arrow disabled state and edge fades when rail overflows', async () => {
    const many = Array.from({ length: 8 }, (_, i) => ({
      ...approveWf,
      id: `wf-${i}`,
      name: `流水线 ${i}`,
    }))
    mocks.listWorkflows.mockResolvedValue(many)
    const wrapper = mountDashboard()
    await flushPromises()

    const rail = wrapper.get('[data-testid="home-pipeline-cards"]').element as HTMLDivElement
    Object.defineProperty(rail, 'clientWidth', { configurable: true, value: 400 })
    Object.defineProperty(rail, 'scrollWidth', { configurable: true, value: 1600 })
    let scrollLeft = 0
    Object.defineProperty(rail, 'scrollLeft', {
      configurable: true,
      get: () => scrollLeft,
      set: (v: number) => {
        scrollLeft = v
      },
    })

    await rail.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.get('[data-testid="home-pipeline-cards"]').classes()).toContain(
      'home-pipeline-rail--overflow',
    )
    const prev = wrapper.get('[data-testid="home-pipeline-scroll-prev"]').element as HTMLButtonElement
    const next = wrapper.get('[data-testid="home-pipeline-scroll-next"]').element as HTMLButtonElement
    expect(prev.disabled).toBe(true)
    expect(next.disabled).toBe(false)
    expect(wrapper.find('.home-pipeline-rail-wrap--has-right').exists()).toBe(true)
    expect(wrapper.find('.home-pipeline-rail-wrap--has-left').exists()).toBe(false)

    scrollLeft = 1200
    await rail.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(prev.disabled).toBe(false)
    expect(next.disabled).toBe(true)
    expect(wrapper.find('.home-pipeline-rail-wrap--has-left').exists()).toBe(true)
    expect(wrapper.find('.home-pipeline-rail-wrap--has-right').exists()).toBe(false)

    scrollLeft = 0
    await rail.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(prev.disabled).toBe(true)
    expect(wrapper.find('.home-pipeline-rail-wrap--has-left').exists()).toBe(false)

    wrapper.unmount()
  })

  it('left-aligns pipeline rail when overflowing so first card is not clipped', async () => {
    const many = Array.from({ length: 6 }, (_, i) => ({
      ...approveWf,
      id: `wf-${i}`,
      name: `流水线 ${i}`,
    }))
    mocks.listWorkflows.mockResolvedValue(many)
    const wrapper = mountDashboard()
    await flushPromises()

    const rail = wrapper.get('[data-testid="home-pipeline-cards"]')
    const railEl = rail.element as HTMLDivElement
    Object.defineProperty(railEl, 'clientWidth', { configurable: true, value: 400 })
    Object.defineProperty(railEl, 'scrollWidth', { configurable: true, value: 1200 })
    Object.defineProperty(railEl, 'scrollLeft', { configurable: true, value: 0, writable: true })

    await railEl.dispatchEvent(new Event('scroll'))
    await flushPromises()

    expect(rail.classes()).toContain('home-pipeline-rail--overflow')
    expect(rail.classes()).not.toContain('justify-center')
    wrapper.unmount()
  })

  it('keeps pipeline rail centered when cards do not overflow', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    const rail = wrapper.get('[data-testid="home-pipeline-cards"]')
    const railEl = rail.element as HTMLDivElement
    Object.defineProperty(railEl, 'clientWidth', { configurable: true, value: 800 })
    Object.defineProperty(railEl, 'scrollWidth', { configurable: true, value: 200 })
    Object.defineProperty(railEl, 'scrollLeft', { configurable: true, value: 0, writable: true })

    await railEl.dispatchEvent(new Event('scroll'))
    await flushPromises()

    expect(rail.classes()).not.toContain('home-pipeline-rail--overflow')
    wrapper.unmount()
  })

  it('arrow click does not change pipeline card selection', async () => {
    const second: Workflow = {
      ...approveWf,
      id: 'wf-lite',
      name: '快速澄清 Lite',
      description: '轻量 Approve 入口',
    }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    const wrapper = mountDashboard()
    await flushPromises()

    const rail = wrapper.get('[data-testid="home-pipeline-cards"]').element as HTMLDivElement
    Object.defineProperty(rail, 'clientWidth', { configurable: true, value: 200 })
    Object.defineProperty(rail, 'scrollWidth', { configurable: true, value: 800 })
    Object.defineProperty(rail, 'scrollLeft', {
      configurable: true,
      get: () => 0,
      set: () => {},
    })
    await rail.dispatchEvent(new Event('scroll'))
    await flushPromises()

    await wrapper.get('[data-testid="home-pipeline-scroll-next"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').classes()).toContain(
      'home-shell__card--selected',
    )
    wrapper.unmount()
  })

  // plan g1.1 — plus card at the end of the rail; not a selectable pipeline
  it('appends a new-workflow card that opens the baseline modal and ignores context menu', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const rail = wrapper.get('[data-testid="home-pipeline-cards"]')
    const add = wrapper.get('[data-testid="home-new-workflow"]')
    expect(rail.element.lastElementChild).toBe(add.element)
    expect(add.text()).toContain('新建工作流')
    await add.trigger('contextmenu')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-pipeline-menu"]').exists()).toBe(false)
    await add.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-create-workflow-name"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-create-project-list"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('从零开始')
    wrapper.unmount()
  })

  // plan g3.1 — the plus is an SVG icon whose ink is geometrically centered in the square,
  // so flex centering no longer depends on the text glyph baseline (font-independent).
  it('renders the new-workflow plus as a centered svg icon instead of a text glyph', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const plus = wrapper.get('[data-testid="home-new-workflow"] .home-shell__card-plus')
    // no text node => no font baseline to push the glyph off-center
    expect(plus.text().trim()).toBe('')

    const svg = plus.get('svg')
    // size matches sibling plus icons (e.g. home-composer-plus) so the ink is not enlarged
    expect(svg.attributes('width')).toBe('16')
    expect(svg.attributes('height')).toBe('16')
    const viewBox = svg.attributes('viewBox') ?? ''
    const [vbX, vbY, vbW, vbH] = viewBox.split(/\s+/).map(Number)
    expect(vbW).toBe(vbH)
    expect(vbX + vbW / 2).toBe(vbY + vbH / 2)

    // each stroke of the plus is centered on the viewBox center
    const d = svg.get('path').attributes('d') ?? ''
    const vertical = d.match(/M(\d+) (\d+)v(\d+)/)
    const horizontal = d.match(/M(\d+) (\d+)h(\d+)/)
    expect(vertical).not.toBeNull()
    expect(horizontal).not.toBeNull()
    const vMid = { x: Number(vertical![1]), y: Number(vertical![2]) + Number(vertical![3]) / 2 }
    const hMid = { x: Number(horizontal![1]) + Number(horizontal![3]) / 2, y: Number(horizontal![2]) }
    expect(vMid).toEqual({ x: vbX + vbW / 2, y: vbY + vbH / 2 })
    expect(hMid).toEqual(vMid)

    // the flex box still centers its svg child geometrically (no font baseline involved)
    expect(svg.element.parentElement).toBe(plus.element)
    const css = dashboardSource
    expect(css).toMatch(/\.home-shell__card-plus\s*\{[^}]*display:\s*flex[^}]*\}/)
    expect(css).toMatch(/\.home-shell__card-plus\s*\{[^}]*align-items:\s*center[^}]*\}/)
    expect(css).toMatch(/\.home-shell__card-plus\s*\{[^}]*justify-content:\s*center[^}]*\}/)
    expect(css).toMatch(/\.home-shell__card-plus\s*>\s*svg\s*\{[^}]*display:\s*block[^}]*\}/)
    wrapper.unmount()
  })

  // plan g1.2 — empty pipeline list still offers the same plus card
  it('keeps the new-workflow card when the home pipeline list is empty', async () => {
    mocks.listWorkflows.mockResolvedValue([])
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="home-pipelines-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-new-workflow"]').exists()).toBe(true)
    await wrapper.get('[data-testid="home-new-workflow"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-create-workflow-name"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // plan g2.1 / g2.2 — successful home create reloads cards and selects the new pipeline
  it('reloads home cards after a successful baseline create', async () => {
    const created: Workflow = { ...approveWf, id: 'wf-new', name: '首页新建' }
    mocks.createWorkflowFromBaseline.mockResolvedValue(created)
    const wrapper = mountDashboard()
    await flushPromises()
    const callsAfterMount = mocks.listWorkflows.mock.calls.length
    await wrapper.get('[data-testid="home-new-workflow"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="home-create-workflow-name"]').setValue('首页新建')
    const url = wrapper.find('input[placeholder*="https"]')
    expect(url.exists()).toBe(true)
    await url.setValue('https://github.com/org/repo')
    await flushPromises()
    mocks.listWorkflows.mockResolvedValue([approveWf, created])
    await wrapper.get('[data-testid="home-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.createWorkflowFromBaseline).toHaveBeenCalled()
    expect(mocks.listWorkflows.mock.calls.length).toBeGreaterThan(callsAfterMount)
    expect(wrapper.find('[data-testid="home-pipeline-card-wf-new"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-new"]').classes()).toContain(
      'home-shell__card--selected',
    )
    wrapper.unmount()
  })

  // plan g2.2 — failed create must not refresh the home pipeline list
  it('does not reload home cards when baseline create fails', async () => {
    mocks.createWorkflowFromBaseline.mockRejectedValue(new Error('create failed'))
    const wrapper = mountDashboard()
    await flushPromises()
    const callsAfterMount = mocks.listWorkflows.mock.calls.length
    await wrapper.get('[data-testid="home-new-workflow"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="home-create-workflow-name"]').setValue('X')
    const url = wrapper.find('input[placeholder*="https"]')
    await url.setValue('https://example.com/r.git')
    await flushPromises()
    await wrapper.get('[data-testid="home-create-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="home-create-error"]').text()).toContain('create failed')
    expect(mocks.listWorkflows.mock.calls.length).toBe(callsAfterMount)
    expect(wrapper.find('[data-testid="home-pipeline-card-wf-ap"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // plan g2.1 / g3.2 — dropdown create opens the same HomeCreateBaselineModal
  it('opens the baseline modal from the pipeline select create footer', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.get('[data-testid="home-pipeline-select-trigger"]').trigger('click')
    await flushPromises()
    expect(teleportedExists('home-pipeline-select-create')).toBe(true)
    await teleported('home-pipeline-select-create').trigger('click')
    await flushPromises()
    expect(teleportedExists('home-pipeline-select-panel')).toBe(false)
    expect(wrapper.find('[data-testid="home-create-workflow-name"]').exists()).toBe(true)
    // plan g2.2 — rail card and composer + unchanged
    expect(wrapper.find('[data-testid="home-new-workflow"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-composer-plus"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // plan g3.2 — empty list can create from dropdown; composer + still attaches files only
  it('allows create from dropdown when home pipelines are empty without changing plus', async () => {
    mocks.listWorkflows.mockResolvedValue([])
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.get('[data-testid="home-pipeline-select-trigger"]').trigger('click')
    await flushPromises()
    await teleported('home-pipeline-select-create').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-create-workflow-name"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="home-composer-plus"]').attributes('title')).toBeTruthy()
    wrapper.unmount()
  })

  // plan g2.1 / g3.2 — select create success path shares reloadAfterCreate
  it('selects the new pipeline after create started from the dropdown footer', async () => {
    const created: Workflow = { ...approveWf, id: 'wf-from-select', name: '下拉新建' }
    mocks.createWorkflowFromBaseline.mockResolvedValue(created)
    const wrapper = mountDashboard()
    await flushPromises()
    await wrapper.get('[data-testid="home-pipeline-select-trigger"]').trigger('click')
    await flushPromises()
    await teleported('home-pipeline-select-create').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="home-create-workflow-name"]').setValue('下拉新建')
    const url = wrapper.find('input[placeholder*="https"]')
    await url.setValue('https://github.com/org/from-select')
    await flushPromises()
    mocks.listWorkflows.mockResolvedValue([approveWf, created])
    await wrapper.get('[data-testid="home-create-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-pipeline-card-wf-from-select"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="home-pipeline-card-wf-from-select"]').classes()).toContain(
      'home-shell__card--selected',
    )
    wrapper.unmount()
  })

  // plan g1.1 — wait blank: no loading copy, cards, or add card
  it('plan g1.1 — while pipelines load, composer stays and rail stays blank', async () => {
    let resolveList!: (value: Workflow[]) => void
    mocks.listWorkflows.mockImplementation(
      () => new Promise<Workflow[]>((resolve) => { resolveList = resolve }),
    )
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="home-composer"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-pipelines-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-pipeline-enter"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-new-workflow"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-pipeline-cards"]').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/加载中/)

    resolveList([approveWf])
    await flushPromises()
    // plan g1.2 — same settle: enter group ready with cards + add
    const enter = wrapper.get('[data-testid="home-pipeline-enter"]')
    expect(enter.classes()).toContain('home-pipeline-enter--ready')
    expect(enter.find('[data-testid="home-pipeline-card-wf-ap"]').exists()).toBe(true)
    expect(enter.find('[data-testid="home-new-workflow"]').exists()).toBe(true)
    expect(dashboardSource).not.toMatch(/setTimeout\([^)]*pipelineRail|minVisible|SHOW_AFTER/)
    wrapper.unmount()
  })

  // plan g1.3 — many cards share the same group enter (no per-card delay in source)
  it('plan g1.3 — many pipeline cards share one enter group without nth-child delays', async () => {
    const many = Array.from({ length: 8 }, (_, i) => ({
      ...approveWf,
      id: `wf-many-${i}`,
      name: `流水线 ${i + 1}`,
    }))
    mocks.listWorkflows.mockResolvedValue(many)
    const wrapper = mountDashboard()
    await flushPromises()
    const enter = wrapper.get('[data-testid="home-pipeline-enter"]')
    expect(enter.findAll('[data-testid^="home-pipeline-card-wf-many-"]').filter(
      (n) => /^home-pipeline-card-wf-many-\d+$/.test(n.attributes('data-testid') || ''),
    ).length).toBe(8)
    expect(enter.find('[data-testid="home-new-workflow"]').exists()).toBe(true)
    expect(dashboardSource).not.toMatch(/nth-child\([^)]+\)[^{]*\{[^}]*animation-delay/)
    expect(dashboardSource).toMatch(/home-pipeline-rail-enter 420ms/)
    wrapper.unmount()
  })

  // plan g2.1 — empty list: empty copy + add card in the same enter group
  it('plan g2.1 — empty pipelines reveal empty state and add card together', async () => {
    mocks.listWorkflows.mockResolvedValue([])
    const wrapper = mountDashboard()
    await flushPromises()
    const enter = wrapper.get('[data-testid="home-pipeline-enter"]')
    expect(enter.find('[data-testid="home-pipelines-empty"]').exists()).toBe(true)
    expect(enter.find('[data-testid="home-new-workflow"]').exists()).toBe(true)
    expect(enter.classes()).toContain('home-pipeline-enter--ready')
    wrapper.unmount()
  })

  // plan g2.1 — failure then retry plays enter once on success
  it('plan g2.1 — load error hides rail; retry success reveals enter group', async () => {
    mocks.listWorkflows.mockRejectedValueOnce(new Error('network down'))
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-load-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-pipeline-enter"]').exists()).toBe(false)

    mocks.listWorkflows.mockResolvedValue([approveWf])
    await wrapper.get('[data-testid="dashboard-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-load-error"]').exists()).toBe(false)
    const enter = wrapper.get('[data-testid="home-pipeline-enter"]')
    expect(enter.classes()).toContain('home-pipeline-enter--ready')
    expect(enter.find('[data-testid="home-pipeline-card-wf-ap"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // plan g2.2 — reloadAfterCreate keeps revealed rail (no reset of pipelineRailRevealed)
  it('plan g2.2 — reloadAfterCreate keeps enter group mounted without resetting reveal', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const enterBefore = wrapper.get('[data-testid="home-pipeline-enter"]').element
    expect(dashboardSource).not.toMatch(/pipelineRailRevealed\.value = false/)

    const created = {
      ...approveWf,
      id: 'wf-reload-keep',
      name: '刷新保持',
    }
    mocks.createWorkflowFromBaseline.mockResolvedValue(created)
    mocks.listWorkflows.mockResolvedValue([approveWf, created])
    await wrapper.get('[data-testid="home-new-workflow"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="home-create-workflow-name"]').setValue('刷新保持')
    const url = wrapper.find('input[placeholder*="https"]')
    await url.setValue('https://github.com/org/reload-keep')
    await flushPromises()
    await wrapper.get('[data-testid="home-create-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-pipeline-enter"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="home-pipeline-enter"]').element).toBe(enterBefore)
    expect(wrapper.find('[data-testid="home-pipeline-card-wf-reload-keep"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // plan g2.2 — hide does not remount enter group
  it('plan g2.2 — hidePipelineFromHome updates cards without remounting enter group', async () => {
    const second = {
      ...approveWf,
      id: 'wf-keep',
      name: '保留卡',
      projectId: 'proj-2',
    }
    mocks.listWorkflows.mockResolvedValue([approveWf, second])
    mocks.listProjects.mockResolvedValue([
      { id: 'proj-1', name: '综合项目组', description: '', variables: [] },
      { id: 'proj-2', name: 'SkillHub', description: '', variables: [] },
    ])
    const wrapper = mountDashboard()
    await flushPromises()
    const enterBefore = wrapper.get('[data-testid="home-pipeline-enter"]').element
    await wrapper.get('[data-testid="home-pipeline-card-wf-ap"]').trigger('contextmenu')
    await teleported('home-pipeline-menu-hide').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="home-pipeline-card-wf-ap"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="home-pipeline-enter"]').element).toBe(enterBefore)
    wrapper.unmount()
  })

  // plan g2.3 — reduced-motion rules cover the enter classes (source)
  it('plan g2.3 — prefers-reduced-motion disables pipeline enter animation', () => {
    expect(dashboardSource).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[\s\S]*\.home-pipeline-enter--ready[\s\S]*animation:\s*none/,
    )
  })
})

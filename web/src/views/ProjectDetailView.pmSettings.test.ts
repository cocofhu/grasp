// @vitest-environment happy-dom
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { PmLeaderBinding } from '@/lib/shared/types'

const apiMocks = vi.hoisted(() => ({
  getProject: vi.fn(),
  listWorkflows: vi.fn(),
  getPmLeader: vi.fn(),
  updatePmLeader: vi.fn(),
  listAgents: vi.fn(),
  listProjectCronJobs: vi.fn(),
  listProjectChannels: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProject: apiMocks.getProject,
      listWorkflows: apiMocks.listWorkflows,
      getPmLeader: apiMocks.getPmLeader,
      updatePmLeader: apiMocks.updatePmLeader,
      listAgents: apiMocks.listAgents,
      listProjectCronJobs: apiMocks.listProjectCronJobs,
      listProjectChannels: apiMocks.listProjectChannels,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return {
    useBreakpoint: () => ({ isMobile: ref(false) }),
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

import ProjectDetailView from './ProjectDetailView.vue'

const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
  description: '',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const MOCK_PROJECT_B = {
  ...MOCK_PROJECT,
  id: 'proj-2',
  name: 'Project B',
}

const DISABLED_BINDING: PmLeaderBinding = {
  enabled: false,
  agentAvailable: false,
  agentConfigRef: '',
  aclNote: '记忆请在 Agent Studio「数据 → 记忆」中管理；任意已登录用户可编辑。',
}

const ENABLED_BINDING: PmLeaderBinding = {
  enabled: true,
  agentAvailable: true,
  agentConfigRef: 'agent-1',
  aclNote: '记忆请在 Agent Studio「数据 → 记忆」中管理；任意已登录用户可编辑。',
}

/** Opens PM Leader settings via the disabled empty state's「前往设置」entry. */
async function mountSettings() {
  const mounted = await mountDetail('pmLeader')
  await flushPromises()
  const go = mounted.wrapper.findAll('button').find((b) => b.text().includes('前往设置'))
  await go!.trigger('click')
  await nextTick()
  return mounted
}

async function mountDetail(tabQuery?: string, binding: PmLeaderBinding = DISABLED_BINDING) {
  apiMocks.getProject.mockImplementation(async (id: string) =>
    id === MOCK_PROJECT_B.id ? MOCK_PROJECT_B : MOCK_PROJECT,
  )
  apiMocks.listWorkflows.mockResolvedValue([])
  apiMocks.getPmLeader.mockResolvedValue(binding)
  apiMocks.listAgents.mockResolvedValue([{ name: 'agent-1' }])
  apiMocks.listProjectCronJobs.mockResolvedValue({ items: [] })
  apiMocks.listProjectChannels.mockResolvedValue({ items: [], freeAgents: [], secretsKeyConfigured: true })
  apiMocks.updatePmLeader.mockImplementation(async (_id: string, body: Partial<PmLeaderBinding>) => ({
    ...ENABLED_BINDING,
    ...body,
    agentAvailable: !!body.enabled,
    aclNote: ENABLED_BINDING.aclNote,
  }))

  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const path = tabQuery
    ? `/projects/proj-1?tab=${encodeURIComponent(tabQuery)}`
    : '/projects/proj-1'
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/projects/:id', component: ProjectDetailView },
      { path: '/agents', component: { render: () => h('div') } },
      { path: '/projects', component: { render: () => h('div') } },
    ],
  })
  await router.push(path)
  await router.isReady()

  const wrapper = mount(
    defineComponent({
      setup() {
        return () => h(RouterView)
      },
    }),
    {
      global: {
        plugins: [i18n, router],
        stubs: {
          BoardView: true,
          Icon: true,
          AppModal: true,
          EmptyState: true,
          StatusPill: true,
          RunLaunchModal: true,
          CopyWorkflowModal: true,
          ExportVersionModal: true,
          CitationCard: true,
        },
      },
    },
  )
  await flushPromises()
  return { wrapper, router }
}

describe('ProjectDetailView PM Leader settings inline', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('tabs use a compact single-row horizontal scroller', async () => {
    const { wrapper } = await mountDetail('board')
    const tabs = wrapper.find('[data-testid="project-detail-tabs"]')
    expect(tabs.exists()).toBe(true)
    expect(tabs.classes()).toEqual(
      expect.arrayContaining(['flex-nowrap', 'overflow-x-auto', 'overflow-y-hidden']),
    )
    expect(tabs.classes()).not.toContain('flex-wrap')

    const boardTab = wrapper.find('[data-testid="project-tab-board"]')
    expect(boardTab.classes()).toEqual(
      expect.arrayContaining(['shrink-0', 'whitespace-nowrap', 'border-b-2', 'py-1.5']),
    )
    expect(boardTab.classes()).not.toContain('min-h-11')
    expect(tabs.classes()).not.toContain('mb-4')
    expect(boardTab.classes()).toContain('text-accent-2')
    const indicator = wrapper.find('[data-testid="project-tabs-indicator"]')
    expect(indicator.exists()).toBe(true)
    expect(indicator.classes()).toEqual(expect.arrayContaining(['bg-accent', 'app-tabs-indicator']))
    expect(wrapper.find('[data-testid="project-board-panel"]').classes()).toContain('min-w-0')
  })

  it('hides top-bar pmSettings and pmMemory tabs while keeping cron', async () => {
    const { wrapper } = await mountDetail('pmLeader')
    const tabIds = wrapper
      .findAll('[data-testid^="project-tab-"]')
      .map((el) => el.attributes('data-testid'))
    expect(tabIds).toContain('project-tab-pmLeader')
    expect(tabIds).toContain('project-tab-cronJobs')
    expect(tabIds).not.toContain('project-tab-pmSettings')
    expect(tabIds).not.toContain('project-tab-pmMemory')
    expect(wrapper.text()).toContain('定时任务')
    // Permanently removed: PM Leader no longer shows the Studio memory migration guide.
    expect(wrapper.find('[data-testid="pm-studio-memory-guide"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pm-open-studio-memory"]').exists()).toBe(false)
  })

  it('opens cron jobs panel from tab', async () => {
    const { wrapper } = await mountDetail('cronJobs')
    expect(wrapper.find('[data-testid="project-cron-jobs-panel"]').exists()).toBe(true)
    expect(apiMocks.listProjectCronJobs).toHaveBeenCalledWith('proj-1')
    expect(wrapper.text()).not.toContain('本项目下全部 Agent 的定时任务')
    expect(wrapper.text()).not.toContain('可查看与删除')
    expect(wrapper.text()).not.toContain('任意已登录用户开关是否推送到项目绑定渠道')
  })

  it('opens inline settings from disabled empty state without top-bar tab', async () => {
    const { wrapper } = await mountDetail('pmLeader')
    expect(wrapper.text()).toContain('项目管理未启用')
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(false)
    const go = wrapper.findAll('button').find((b) => b.text().includes('前往设置'))
    expect(go).toBeTruthy()
    await go!.trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-tab-pmSettings"]').exists()).toBe(false)
  })

  it('resets to chat after leaving PM Leader top-bar tab', async () => {
    const { wrapper } = await mountSettings()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(true)

    await wrapper.get('[data-testid="project-tab-cronJobs"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(false)

    await wrapper.get('[data-testid="project-tab-pmLeader"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('项目管理未启用')
  })

  it('returns to chat after successful save and refreshes binding', async () => {
    const { wrapper } = await mountSettings()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(true)

    // Enable + select agent + save
    const enable = wrapper.find('button[role="switch"]')
    await enable.trigger('click')
    const select = wrapper.find('#pm-bind-agent')
    await select.setValue('agent-1')
    const saveBtn = wrapper.find('[data-testid="pm-leader-save"]')
    expect(saveBtn).toBeTruthy()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(apiMocks.updatePmLeader).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(false)
    // Enabled chat shows settings gear entry
    expect(wrapper.find('[data-testid="pm-chat-open-settings"]').exists()).toBe(true)
  })

  it('stays on settings when save fails', async () => {
    apiMocks.updatePmLeader.mockRejectedValueOnce(new Error('save failed'))
    const { wrapper } = await mountSettings()

    const enable = wrapper.find('button[role="switch"]')
    await enable.trigger('click')
    await wrapper.find('#pm-bind-agent').setValue('agent-1')
    const saveBtn = wrapper.find('[data-testid="pm-leader-save"]')
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(true)
  })

  it('back button returns to chat without saving', async () => {
    const { wrapper } = await mountSettings()
    await wrapper.get('[data-testid="pm-settings-back"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('项目管理未启用')
    expect(apiMocks.updatePmLeader).not.toHaveBeenCalled()
  })

  it('shows gear+settings entry when enabled', async () => {
    const { wrapper } = await mountDetail('pmLeader', ENABLED_BINDING)
    await flushPromises()
    const btn = wrapper.find('[data-testid="pm-chat-open-settings"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('设置')
    await btn.trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(true)
  })

  it('resets pmView when navigating to another project', async () => {
    const { wrapper, router } = await mountSettings()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(true)

    await router.push('/projects/proj-2?tab=pmLeader')
    await flushPromises()

    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('项目管理未启用')
  })

  it('does not retain settings sub-view after remount (refresh)', async () => {
    const { wrapper } = await mountDetail('pmLeader')
    const go = wrapper.findAll('button').find((b) => b.text().includes('前往设置'))
    await go!.trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="project-pm-settings-view"]').exists()).toBe(true)

    wrapper.unmount()
    const { wrapper: reloaded } = await mountDetail('pmLeader')
    await flushPromises()
    expect(reloaded.find('[data-testid="project-pm-settings-view"]').exists()).toBe(false)
    expect(reloaded.text()).toContain('项目管理未启用')
  })
})

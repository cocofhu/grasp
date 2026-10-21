// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return { workspace: ref<any>({ name: 'Studio' }), getPmLeader: vi.fn(), reload: vi.fn(), requestLeave: vi.fn(), dirty: ref(false) }
})
vi.mock('@/lib/api/api', () => ({ api: { getPmLeader: mocks.getPmLeader } }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace, reload: mocks.reload }) }))
import WorkspaceToolView from './WorkspaceToolView.vue'
const panel = (name: string) => defineComponent({ name, props: ['workspace', 'binding'], emits: ['open-settings', 'changed', 'updated', 'workspace-updated'], setup(props, { emit }) {
  return () => h('div', { 'data-panel': name, 'data-workspace-name': props.workspace?.name, 'data-binding': props.binding?.agentConfigRef }, [
    h('button', { onClick: () => emit('open-settings') }, 'settings'),
    h('button', { 'data-emit': 'changed', onClick: () => emit('changed') }, 'changed'),
    h('button', { 'data-emit': 'updated', onClick: () => emit('updated', { name: 'Renamed' }) }, 'updated'),
  ])
} })
async function setup(path: string) {
  const specs = [['/board', 'board'], ['/requirements', 'requirements'], ['/assistant', 'assistant'], ['/settings/assistant', 'assistantSettings'], ['/settings/credentials', 'credentials'], ['/settings/notification-policy', 'notificationPolicy'], ['/settings/schedules', 'schedules'], ['/settings/audit', 'audit'], ['/settings/external-mcp', 'externalMcp']] as const
  const router = createRouter({ history: createMemoryHistory(), routes: specs.map(([route, tool]) => ({ path: route, component: WorkspaceToolView, meta: { workspaceTool: tool, workspaceTitle: `nav.${tool}` } })) })
  await router.push(path)
  const drafts = defineComponent({ setup(_props, { expose }) { expose({ get isDirty() { return mocks.dirty.value }, requestLeave: mocks.requestLeave }); return () => h('div', { 'data-panel': 'drafts' }) } })
  const stubs: Record<string, any> = { WorkspacePage: { template: '<section class="h-full min-h-0 flex flex-col"><slot name="actions"/><slot/></section>' }, RequirementDraftsPanel: drafts }
  for (const name of ['BoardView', 'PmLeaderChat', 'PmSettingsPanel', 'PmCronJobsPanel', 'WorkspaceNotifyPanel', 'WorkspaceExternalMcpPanel', 'WorkspaceCredentialsPanel', 'WorkspaceAuditPanel']) stubs[name] = panel(name)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': nav } })
  const wrapper = mount({ render: () => h(RouterView) }, { global: { plugins: [router, i18n], directives: { 'hover-ink': {} }, stubs } })
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => { vi.clearAllMocks(); mocks.dirty.value = false; mocks.workspace.value = { name: 'Studio' }; mocks.getPmLeader.mockResolvedValue({ enabled: true, agentConfigRef: 'manager' }); mocks.requestLeave.mockResolvedValue(false) })
describe('every workspace tool page', () => {
  it.each([
    ['/board', 'BoardView', 'workspace'], ['/requirements', 'drafts', 'workspace'], ['/assistant', 'PmLeaderChat', 'workspace'],
    ['/settings/assistant', 'PmSettingsPanel', 'settings'], ['/settings/schedules', 'PmCronJobsPanel', 'workspace'],
    ['/settings/notification-policy', 'WorkspaceNotifyPanel', 'settings'], ['/settings/external-mcp', 'WorkspaceExternalMcpPanel', 'settings'],
    ['/settings/credentials', 'WorkspaceCredentialsPanel', 'workspace'],
    ['/settings/audit', 'WorkspaceAuditPanel', 'workspace'],
  ])('%s renders only %s in the %s layout', async (path, component, layout) => {
    const { wrapper } = await setup(path)
    expect(wrapper.findAll('[data-panel]').map((node) => node.attributes('data-panel'))).toEqual([component])
    const root = wrapper.get('[data-testid^="workspace-tool-"]')
    expect(root.classes().includes('gap-8')).toBe(layout === 'settings')
    expect(mocks.getPmLeader).toHaveBeenCalledTimes(path === '/assistant' ? 1 : 0)
    wrapper.unmount()
  })
  it('passes the workspace and assistant binding to the panels that need them', async () => {
    const { wrapper } = await setup('/assistant')
    expect(wrapper.get('[data-panel="PmLeaderChat"]').attributes('data-binding')).toBe('manager')
    wrapper.unmount()
    for (const [path, component] of [['/settings/assistant', 'PmSettingsPanel'], ['/settings/notification-policy', 'WorkspaceNotifyPanel']]) {
      const { wrapper } = await setup(path)
      expect(wrapper.get(`[data-panel="${component}"]`).attributes('data-workspace-name')).toBe('Studio')
      wrapper.unmount()
    }
  })
  it('renders nothing until the workspace is loaded', async () => {
    mocks.workspace.value = null
    const { wrapper } = await setup('/board')
    expect(wrapper.find('[data-testid^="workspace-tool-"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('shows the assistant loading state while the binding request is pending', async () => {
    let resolve!: (value: unknown) => void
    mocks.getPmLeader.mockReturnValueOnce(new Promise((done) => { resolve = done }))
    const { wrapper } = await setup('/assistant')
    expect(wrapper.get('[role="status"]').text()).toBe('正在加载…')
    expect(wrapper.find('[data-panel="PmLeaderChat"]').exists()).toBe(false)
    resolve({ enabled: true, agentConfigRef: 'manager' })
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.find('[data-panel="PmLeaderChat"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('shows the assistant error and recovers on retry', async () => {
    mocks.getPmLeader.mockRejectedValueOnce(new Error('binding unavailable'))
    const { wrapper } = await setup('/assistant')
    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('binding unavailable')
    await alert.get('button').trigger('click')
    await flushPromises()
    expect(mocks.getPmLeader).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-panel="PmLeaderChat"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('reloads or replaces the workspace when settings panels report changes', async () => {
    const settings = await setup('/settings/assistant')
    await settings.wrapper.get('[data-panel="PmSettingsPanel"] [data-emit="changed"]').trigger('click')
    expect(mocks.reload).toHaveBeenCalledTimes(1)
    settings.wrapper.unmount()
    const notify = await setup('/settings/notification-policy')
    await notify.wrapper.get('[data-panel="WorkspaceNotifyPanel"] [data-emit="updated"]').trigger('click')
    expect(mocks.workspace.value.name).toBe('Renamed')
    notify.wrapper.unmount()
  })
})
describe('independent workspace tools', () => {
  it.each([['/board', 'BoardView'], ['/settings/credentials', 'WorkspaceCredentialsPanel'], ['/settings/audit', 'WorkspaceAuditPanel'], ['/settings/schedules', 'PmCronJobsPanel']])('mounts %s directly without workspace props', async (path, component) => {
    const { wrapper } = await setup(path)
    expect(wrapper.get(`[data-panel="${component}"]`).attributes('data-workspace-name')).toBeUndefined()
    expect(wrapper.get('[data-testid^="workspace-tool-"]').classes()).toEqual(expect.arrayContaining(['min-h-0', 'flex-1']))
    wrapper.unmount()
  })
  it('blocks leaving an unsaved requirement draft and permits an explicit confirmed leave', async () => {
    const { wrapper, router } = await setup('/requirements')
    mocks.dirty.value = true
    await router.push('/board')
    expect(mocks.requestLeave).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/requirements')
    mocks.requestLeave.mockResolvedValueOnce(true)
    await router.push('/board')
    expect(router.currentRoute.value.path).toBe('/board')
    wrapper.unmount()
  })
  it('keeps PM conversation and its configuration reachable as separate pages', async () => {
    const { wrapper, router } = await setup('/assistant')
    expect(mocks.getPmLeader).toHaveBeenCalledWith()
    expect(wrapper.find('[data-panel="PmLeaderChat"]').exists()).toBe(true)
    await wrapper.get('[data-panel="PmLeaderChat"] button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/settings/assistant')
    expect(wrapper.find('[data-panel="PmSettingsPanel"]').exists()).toBe(true)
    await wrapper.findAll('button').find((button) => button.text() === '返回助手')!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/assistant')
    expect(wrapper.find('[data-panel="PmLeaderChat"]').exists()).toBe(true)
    wrapper.unmount()
  })
})

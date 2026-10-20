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
  return { workspace: ref<any>({ id: 'kept-id', name: 'Studio' }), workspaceId: ref('kept-id'), getPmLeader: vi.fn(), reload: vi.fn(), requestLeave: vi.fn(), dirty: ref(false) }
})
vi.mock('@/lib/api/api', () => ({ api: { getPmLeader: mocks.getPmLeader } }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace, workspaceId: mocks.workspaceId, reload: mocks.reload }) }))
import WorkspaceToolView from './WorkspaceToolView.vue'
const panel = (name: string) => defineComponent({ name, props: ['projectId', 'project', 'binding'], emits: ['open-settings', 'changed', 'updated', 'project-updated'], setup(props, { emit }) { return () => h('div', { 'data-panel': name, 'data-workspace': props.projectId }, [h('button', { onClick: () => emit('open-settings') }, 'settings')]) } })
async function setup(path: string) {
  const specs = [['/board', 'board'], ['/requirements', 'requirements'], ['/assistant', 'assistant'], ['/settings/assistant', 'assistantSettings'], ['/settings/credentials', 'credentials'], ['/settings/notification-policy', 'notificationPolicy'], ['/settings/schedules', 'schedules'], ['/settings/audit', 'audit']] as const
  const router = createRouter({ history: createMemoryHistory(), routes: specs.map(([route, tool]) => ({ path: route, component: WorkspaceToolView, meta: { workspaceTool: tool, workspaceTitle: `nav.${tool}` } })) })
  await router.push(path)
  const drafts = defineComponent({ props: ['projectId'], setup(_props, { expose }) { expose({ get isDirty() { return mocks.dirty.value }, requestLeave: mocks.requestLeave }); return () => h('div', { 'data-panel': 'drafts' }) } })
  const stubs: Record<string, any> = { WorkspacePage: { template: '<section class="h-full min-h-0 flex flex-col"><slot name="actions"/><slot/></section>' }, RequirementDraftsPanel: drafts }
  for (const name of ['BoardView', 'PmLeaderChat', 'PmSettingsPanel', 'PmCronJobsPanel', 'ProjectNotifyPanel', 'ProjectExternalMcpPanel', 'ProjectSharedAgentPanel', 'ProjectCredentialsPanel', 'ProjectAuditPanel']) stubs[name] = panel(name)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': nav } })
  const wrapper = mount({ render: () => h(RouterView) }, { global: { plugins: [router, i18n], directives: { 'hover-ink': {} }, stubs } })
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => { vi.clearAllMocks(); mocks.dirty.value = false; mocks.getPmLeader.mockResolvedValue({ enabled: true, agentConfigRef: 'manager' }); mocks.requestLeave.mockResolvedValue(false) })
describe('independent workspace tools', () => {
  it.each([['/board', 'BoardView'], ['/settings/credentials', 'ProjectCredentialsPanel'], ['/settings/audit', 'ProjectAuditPanel'], ['/settings/schedules', 'PmCronJobsPanel']])('mounts %s directly with the preserved internal ID', async (path, component) => {
    const { wrapper } = await setup(path)
    expect(wrapper.get(`[data-panel="${component}"]`).attributes('data-workspace')).toBe('kept-id')
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
    expect(mocks.getPmLeader).toHaveBeenCalledWith('kept-id')
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

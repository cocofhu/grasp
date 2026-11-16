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
  return { workspace: ref<any>({ name: 'Studio' }) }
})
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace }) }))
import WorkspaceToolView from './WorkspaceToolView.vue'
const panel = (name: string) => defineComponent({ name, props: ['workspace'], setup(props) {
  return () => h('div', { 'data-panel': name, 'data-workspace-name': props.workspace?.name })
} })
async function setup(path: string) {
  const specs = [['/board', 'board'], ['/settings/credentials', 'credentials'], ['/settings/audit', 'audit']] as const
  const router = createRouter({ history: createMemoryHistory(), routes: specs.map(([route, tool]) => ({ path: route, component: WorkspaceToolView, meta: { workspaceTool: tool, workspaceTitle: `nav.${tool}` } })) })
  await router.push(path)
  const stubs: Record<string, any> = { WorkspacePage: { template: '<section class="h-full min-h-0 flex flex-col"><slot name="actions"/><slot/></section>' } }
  for (const name of ['BoardView', 'WorkspaceCredentialsPanel', 'WorkspaceAuditPanel']) stubs[name] = panel(name)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': nav } })
  const wrapper = mount({ render: () => h(RouterView) }, { global: { plugins: [router, i18n], directives: { 'hover-ink': {} }, stubs } })
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => { mocks.workspace.value = { name: 'Studio' } })
describe('every workspace tool page', () => {
  it.each([
    ['/board', 'BoardView'],
    ['/settings/credentials', 'WorkspaceCredentialsPanel'],
    ['/settings/audit', 'WorkspaceAuditPanel'],
  ])('%s mounts only %s without workspace props', async (path, component) => {
    const { wrapper } = await setup(path)
    expect(wrapper.findAll('[data-panel]').map((node) => node.attributes('data-panel'))).toEqual([component])
    expect(wrapper.get(`[data-panel="${component}"]`).attributes('data-workspace-name')).toBeUndefined()
    expect(wrapper.get('[data-testid^="workspace-tool-"]').classes()).toEqual(expect.arrayContaining(['min-h-0', 'flex-1']))
    wrapper.unmount()
  })
  it('renders nothing until the workspace is loaded', async () => {
    mocks.workspace.value = null
    const { wrapper } = await setup('/board')
    expect(wrapper.find('[data-testid^="workspace-tool-"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

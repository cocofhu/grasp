// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(), listAgents: vi.fn(), putConfig: vi.fn(), createTest: vi.fn(),
  success: vi.fn(), error: vi.fn(),
}))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api,
    getProjectSharedAgentConfig: mocks.getConfig, listAgents: mocks.listAgents,
    putProjectSharedAgentConfig: mocks.putConfig, createProjectSharedAgentTest: mocks.createTest,
  } }
})
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))
import ProjectSharedAgentPanel from './ProjectSharedAgentPanel.vue'

const cfg = {
  projectId: 'p1', acpBackend: 'cursor', gitCredentialType: '', files: [], mcp: [], env: {},
  layout: { configRoot: '~/.cursor', workspaceDir: '/workspace' },
}
const FilesStub = { name: 'AgentFilesPanel', props: ['draft', 'save'], methods: { openPathOrCreate: vi.fn() }, template: '<div data-testid="files"/>' }
function mountPanel(projectId = 'p1') {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(ProjectSharedAgentPanel, {
    props: { projectId },
    global: { plugins: [i18n], stubs: {
      Icon: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' },
      AgentFilesPanel: FilesStub, AgentMcpPanel: { template: '<div data-testid="mcp"/>' },
      AgentEnvPanel: { emits: ['open-settings-file'], template: '<button data-testid="env-settings" @click="$emit(\'open-settings-file\')"/>' },
    } },
  })
}

describe('ProjectSharedAgentPanel interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getConfig.mockResolvedValue(cfg)
    mocks.listAgents.mockResolvedValue([{ name: 'a1', projectId: 'p1' }, { name: 'other', projectId: 'p2' }])
    mocks.putConfig.mockImplementation(async (_: string, body: any) => ({ ...cfg, ...body }))
    mocks.createTest.mockResolvedValue({ id: 'sandbox' })
  })

  it('edits metadata, switches backend/region and saves normalized payload', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(vm.derivedPaths[0].path).toContain('mcp.json')
    vm.selectAcpBackend('claude_code')
    await flushPromises()
    expect(vm.draft.acpBackend).toBe('claude_code')
    if (vm.metaRegionOptions.length) vm.selectRegion(vm.metaRegionOptions[0].id)
    vm.draft.layout.workspaceDir = '/srv/work'
    expect(vm.dirty).toBe(true)
    expect(await vm.save()).toBe(true)
    expect(mocks.putConfig).toHaveBeenCalledWith('p1', expect.objectContaining({
      layout: expect.objectContaining({ workspaceDir: '/srv/work' }),
      files: [],
    }))
    expect(mocks.success).toHaveBeenCalled()
    expect(vm.dirty).toBe(false)
    w.unmount()
  })

  it('surfaces load/save failures and retries/discards', async () => {
    mocks.getConfig.mockRejectedValueOnce(new Error('load failed'))
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    expect(w.text()).toContain('load failed')
    mocks.getConfig.mockResolvedValue(cfg)
    await vm.load()
    vm.draft.layout.workspaceDir = '/dirty'
    mocks.putConfig.mockRejectedValueOnce(new Error('save failed'))
    expect(await vm.save()).toBe(false)
    expect(mocks.error).toHaveBeenCalledWith('save failed')
    vm.discard()
    await flushPromises()
    expect(vm.draft.layout.workspaceDir).toBe('/workspace')
    vm.draft = null
    expect(await vm.save()).toBe(false)
    vm.discard()
    vm.selectAcpBackend('cursor')
    vm.selectRegion('global')
    vm.openSettingsInFiles()
    w.unmount()
  })

  it('opens settings via env without a dialogue-test subtab', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    await w.get('[data-testid="shared-agent-subtab-env"]').trigger('click')
    await w.get('[data-testid="env-settings"]').trigger('click')
    await flushPromises()
    expect(vm.subTab).toBe('files')
    expect(w.find('[data-testid="shared-agent-subtab-test"]').exists()).toBe(false)
    w.unmount()
  })

  it('reloads when project id changes and resets the selected tab', async () => {
    const w = mountPanel()
    await flushPromises()
    ;(w.vm as any).subTab = 'meta'
    await w.setProps({ projectId: 'p2' })
    await flushPromises()
    expect(mocks.getConfig).toHaveBeenCalledWith('p2')
    expect((w.vm as any).subTab).toBe('files')
    w.unmount()
  })

  it('renders every remaining subpanel plus special-region state, without an SSH block', async () => {
    mocks.getConfig.mockResolvedValueOnce({ ...cfg, env: { CURSOR_REGION: 'legacy-special' } })
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    for (const tab of ['mcp', 'env', 'meta']) {
      await w.get(`[data-testid="shared-agent-subtab-${tab}"]`).trigger('click')
      await flushPromises()
    }
    vm.subTab = 'meta'; await w.vm.$nextTick()
    expect(w.find('[data-test="shared-ssh-private-key"]').exists()).toBe(false)
    vm.draft.layout.configRoot = ''
    await w.vm.$nextTick()
    expect(vm.derivedPaths[0].path).toContain('mcp.json')
    w.unmount()
  })
})

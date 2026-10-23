// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Workspace } from '@/lib/shared/types'
import WorkspaceNotifyPanel from './WorkspaceNotifyPanel.vue'

const apiMocks = vi.hoisted(() => ({
  listChannels: vi.fn(),
  updateWorkspace: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listChannels: apiMocks.listChannels,
      updateWorkspace: apiMocks.updateWorkspace,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: apiMocks.toastSuccess, error: apiMocks.toastError }),
}))

const SAMPLE_WORKSPACE: Workspace = {
  name: 'Demo',
  description: '',
  variables: [],
  notifyPolicy: {
    enabled: true,
    defaultEvents: ['waiting_human', 'failed'],
    waitingHumanTemplate: 'wait {{run}}',
    failedTemplate: 'fail {{run}}',
    completedTemplate: '',
    channelIds: ['ch-1'],
  },
}

function mountPanel(workspace: Workspace = SAMPLE_WORKSPACE) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(WorkspaceNotifyPanel, {
    props: { workspace },
    global: {
      plugins: [i18n],
    },
  })
}

describe('WorkspaceNotifyPanel', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    apiMocks.listChannels.mockResolvedValue({
      items: [{ id: 'ch-1', name: 'QQ', enabled: true, cronDeliverTarget: 'group:1' }],
      freeAgents: [],
      secretsKeyConfigured: true,
    })
    apiMocks.updateWorkspace.mockImplementation(async (body: Partial<Workspace>) => ({
      ...SAMPLE_WORKSPACE,
      ...body,
    }))
  })

  it('saves selected events and all templates from the grouped settings', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(apiMocks.listChannels).toHaveBeenCalledWith()
    expect(w.findAll('h2').map((heading) => heading.text())).toEqual([
      '通知发送', '默认通知时机', '消息文案模板',
    ])
    expect(w.get('[data-testid="notify-status-badge"]').text()).toBe('已启用')
    expect(w.find('[data-testid="notify-no-channel-hint"]').exists()).toBe(false)

    await w.get('[data-testid="notify-master-toggle"]').trigger('click')
    await w.get('[data-testid="notify-ev-waiting-human"]').trigger('click')
    await w.get('[data-testid="notify-ev-completed"]').trigger('click')
    await w.get('[data-testid="notify-tpl-input"]').setValue('Waiting {run_id}')
    await w.get('[data-testid="notify-tpl-seg-failed"]').trigger('click')
    expect(w.get<HTMLTextAreaElement>('[data-testid="notify-tpl-input"]').element.value).toBe('fail {{run}}')
    await w.get('[data-testid="notify-tpl-input"]').setValue('Failed {workflow}')
    await w.get('[data-testid="notify-tpl-seg-completed"]').trigger('click')
    await w.get('[data-testid="notify-tpl-input"]').setValue('Completed {workspace}')
    await w.get('[data-testid="notify-save"]').trigger('click')
    await flushPromises()

    const policy = {
      enabled: false,
      defaultEvents: ['failed', 'completed'],
      waitingHumanTemplate: 'Waiting {run_id}',
      failedTemplate: 'Failed {workflow}',
      completedTemplate: 'Completed {workspace}',
    }
    expect(apiMocks.updateWorkspace).toHaveBeenCalledWith({ notifyPolicy: policy })
    expect(w.emitted('updated')?.[0]?.[0]).toMatchObject({ notifyPolicy: policy })
    expect(apiMocks.toastSuccess).toHaveBeenCalledWith('通知设置已保存')
    w.unmount()
  })

  it('inserts placeholders at the selection and keeps default and custom template previews', async () => {
    const w = mountPanel()
    await flushPromises()
    const input = w.get<HTMLTextAreaElement>('[data-testid="notify-tpl-input"]')
    await input.setValue('Run details')
    input.element.setSelectionRange(4, 11)
    await w.get('[data-testid="notify-ph-run_id"]').trigger('click')
    expect(input.element.value).toBe('Run {run_id}')
    expect(input.element.selectionStart).toBe('Run {run_id}'.length)
    expect(w.get('[data-testid="notify-preview-mode"]').text()).toBe('自定义模板渲染')
    expect(w.get('[data-testid="notify-preview-body"]').text()).toContain('Run ')
    expect(w.get('[data-testid="notify-preview-body"]').text()).not.toContain('{run_id}')

    await w.get('[data-testid="notify-tpl-seg-completed"]').trigger('click')
    expect(input.element.value).toBe('')
    expect(w.get('[data-testid="notify-preview-mode"]').text()).toBe('使用系统默认')
    await w.get('[data-testid="notify-tpl-fill-default"]').trigger('click')
    expect(input.element.value).toContain('{workspace}')
    await w.get('[data-testid="notify-tpl-clear"]').trigger('click')
    expect(input.element.value).toBe('')
    expect(w.get('[data-testid="notify-preview-body"]').text().length).toBeGreaterThan(0)
    await w.get('[data-testid="notify-tpl-seg-waiting"]').trigger('click')
    expect(input.element.value).toBe('Run {run_id}')
    w.unmount()
  })

  it('locks the policy and template editors during save and retains edits after failure', async () => {
    let rejectSave!: (error: Error) => void
    apiMocks.updateWorkspace.mockImplementationOnce(() => new Promise<Workspace>((_, reject) => {
      rejectSave = reject
    }))
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="notify-tpl-input"]').setValue('Unsaved {run_id}')
    await w.get('[data-testid="notify-ev-completed"]').trigger('click')
    await w.get('[data-testid="notify-save"]').trigger('click')

    expect(w.get('[data-testid="workspace-notify-panel"]').attributes('aria-busy')).toBe('true')
    for (const control of w.findAll('button[role="switch"], button[role="tab"], [data-testid="notify-ph-run_id"], [data-testid="notify-tpl-fill-default"], [data-testid="notify-tpl-clear"], [data-testid="notify-save"], textarea')) {
      expect(control.attributes('disabled')).toBeDefined()
    }
    rejectSave(new Error('Save unavailable'))
    await flushPromises()
    expect(apiMocks.toastError).toHaveBeenCalledWith('Save unavailable')
    expect(w.get<HTMLTextAreaElement>('[data-testid="notify-tpl-input"]').element.value).toBe('Unsaved {run_id}')
    expect(w.get('[data-testid="notify-ev-completed"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-testid="notify-save"]').attributes('disabled')).toBeUndefined()
    expect(w.emitted('updated')).toBeUndefined()
    w.unmount()
  })

  it('handles missing channel ids without fetching and links to channel settings', async () => {
    const w = mountPanel({
      ...SAMPLE_WORKSPACE,
      notifyPolicy: { enabled: true, defaultEvents: ['failed'] },
    })
    await flushPromises()
    expect(apiMocks.listChannels).not.toHaveBeenCalled()
    expect(w.get('[data-testid="notify-status-badge"]').text()).toBe('已开启，但未配置通知渠道')
    await w.get('[data-testid="notify-go-channel"]').trigger('click')
    expect(w.emitted('openChannelSettings')).toHaveLength(1)
    expect(w.get('[data-testid="notify-save"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('keeps channel load failures explicit and restores the form after retry', async () => {
    apiMocks.listChannels.mockRejectedValueOnce(new Error('Channel unavailable'))
    const w = mountPanel()
    expect(w.find('[data-testid="notify-channel-skeleton"]').exists()).toBe(true)
    expect(w.find('[data-testid="notify-save"]').exists()).toBe(false)
    await flushPromises()
    expect(w.get('[data-testid="notify-channel-error"]').text()).toContain('Channel unavailable')
    expect(w.find('[data-testid="notify-master-toggle"]').exists()).toBe(false)
    await w.get('[data-testid="app-inline-error-retry"]').trigger('click')
    await flushPromises()
    expect(apiMocks.listChannels).toHaveBeenCalledTimes(2)
    expect(w.find('[data-testid="notify-channel-error"]').exists()).toBe(false)
    expect(w.get('[data-testid="notify-master-toggle"]').attributes('aria-checked')).toBe('true')
    w.unmount()
  })

  it('deepens unselected template segments one step without the selected text color', async () => {
    const w = mountPanel()
    await flushPromises()
    const selected = w.get('[data-testid="notify-tpl-seg-waiting"]')
    const idle = w.get('[data-testid="notify-tpl-seg-failed"]')
    expect(selected.attributes('aria-selected')).toBe('true')
    expect(selected.classes()).toContain('text-txt')
    expect(selected.classes()).not.toContain('hover:text-txt')
    expect(selected.classes()).not.toContain('hover:text-txt2')
    expect(idle.classes()).toEqual(expect.arrayContaining(['text-txt3', 'hover:text-txt2']))
    expect(idle.classes()).not.toContain('hover:text-txt')
    w.unmount()
  })
})

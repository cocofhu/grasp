// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import snapshots from '@/locales/zh-CN/workspaceSnapshots.json'
import type { RunWorkspaceSave } from '@/lib/shared/types'

const retry = vi.fn()
vi.mock('@/lib/run/workspaceSave', async (load) => ({ ...(await load<object>()), retryWorkspaceSave: (...a: unknown[]) => retry(...a) }))

import WorkspaceSaveBanner from './WorkspaceSaveBanner.vue'

function mountBanner(save: RunWorkspaceSave) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...snapshots } } })
  return mount(WorkspaceSaveBanner, {
    props: { run: { id: 'run-1', controlVersion: 2 }, save, nodeLabel: '编码' },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

const failed: RunWorkspaceSave = { checkpointId: 'cp', nodeId: 'code', state: 'failed', reason: 'snapshot_failed', detail: 'git_unsupported', action: 'retry' }

describe('WorkspaceSaveBanner', () => {
  it('says the run is saving, not paused', () => {
    const wrapper = mountBanner({ ...failed, state: 'saving', reason: '', detail: '', action: undefined })
    expect(wrapper.get('[data-testid="workspace-save-banner"]').attributes('data-state')).toBe('saving')
    expect(wrapper.text()).toContain('正在保存工作区')
    expect(wrapper.text()).toContain('「编码」已完成')
    expect(wrapper.find('[data-testid="workspace-save-retry"]').exists()).toBe(false)
  })

  it('shows the failure cause and retries the save', async () => {
    retry.mockResolvedValueOnce(undefined)
    const wrapper = mountBanner(failed)
    expect(wrapper.get('[data-testid="workspace-save-banner"]').attributes('role')).toBe('alert')
    expect(wrapper.text()).toContain('工作区保存失败')
    expect(wrapper.text()).toContain('重试保存只会重新保存')
    expect(wrapper.text()).toContain('无法安全保存的 Git 元数据')
    await wrapper.get('[data-testid="workspace-save-retry"]').trigger('click')
    await flushPromises()
    expect(retry).toHaveBeenCalledWith({ id: 'run-1', controlVersion: 2 }, failed)
    expect(wrapper.emitted('retried')).toHaveLength(1)
  })

  it('keeps a failed retry visible', async () => {
    retry.mockRejectedValueOnce(new Error('任务状态已变化'))
    const wrapper = mountBanner(failed)
    await wrapper.get('[data-testid="workspace-save-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="workspace-save-retry-error"]').text()).toContain('任务状态已变化')
    expect(wrapper.emitted('retried')).toBeUndefined()
  })

  it('says when no automatic retry exists', () => {
    const wrapper = mountBanner({ ...failed, reason: 'binding_unavailable', detail: '', action: undefined, outcomeSealed: true })
    expect(wrapper.find('[data-testid="workspace-save-retry"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('无法自动重试')
    expect(wrapper.text()).toContain('失败重试')
  })

  it('does not point at failure retry when the node result is unsealed', () => {
    const wrapper = mountBanner({ ...failed, reason: 'provider_outcome_unconfirmed', detail: '', action: undefined, outcomeSealed: false })
    expect(wrapper.find('[data-testid="workspace-save-retry"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-save-no-action"]').text()).toContain('尚未确认')
    expect(wrapper.get('[data-testid="workspace-save-no-action"]').text()).not.toContain('失败重试')
  })
})

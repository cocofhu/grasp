// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({ chat: vi.fn(), continueWorkflow: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { workflowChat: mocks.chat, continueWorkflow: mocks.continueWorkflow } }))
import RunView from './RunView.vue'

const wrappers: ReturnType<typeof mount>[] = []
async function render(path = '/runs/old') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/runs/:id', component: RunView }] })
  await router.push(path)
  const wrapper = mount(RunView, { global: {
    plugins: [router, createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })],
    stubs: {
      SoloSessionPane: { props: ['sessionId'], template: '<div data-testid="owned-solo">{{ sessionId }}</div>' },
      RunLiveView: { template: '<div data-testid="run-history">工作流对话记录</div>' },
      RunDetailView: { template: '<div data-testid="run-canvas">执行详情</div>' },
      SoloCreateModal: { props: ['open', 'continuationRunId'], template: '<div v-if="open" data-testid="continue-config">{{ continuationRunId }}</div>' },
    },
  } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => { vi.clearAllMocks(); mocks.chat.mockResolvedValue({ soloId: '', title: '历史任务', status: 'completed' }) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('chat-first Run history', () => {
  it('opens the existing caller-owned Solo without launching an environment', async () => {
    mocks.chat.mockResolvedValue({ soloId: 'origin', title: '任务', status: 'completed' })
    const { wrapper } = await render()
    expect(wrapper.get('[data-testid="owned-solo"]').text()).toBe('origin')
    expect(wrapper.find('[data-testid="run-canvas"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="run-chat-continue"]').exists()).toBe(false)
    expect(mocks.continueWorkflow).not.toHaveBeenCalled()
  })
  it.each(['completed', 'failed', 'cancelled'])('shows authorized Run chat for %s without an accessible origin', async status => {
    mocks.chat.mockResolvedValue({ soloId: '', title: '历史任务', status })
    const { wrapper } = await render()
    expect(wrapper.find('[data-testid="run-history"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="owned-solo"]').exists()).toBe(false)
    expect(mocks.continueWorkflow).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="run-chat-continue"]').trigger('click')
    expect(wrapper.get('[data-testid="continue-config"]').text()).toBe('old')
    expect(mocks.continueWorkflow).not.toHaveBeenCalled()
  })
  it('keeps execution details and old node deep links accessible', async () => {
    const { wrapper, router } = await render('/runs/old?view=details')
    expect(wrapper.find('[data-testid="run-canvas"]').exists()).toBe(true)
    expect(mocks.chat).not.toHaveBeenCalled()
    await router.push('/runs/old?node=implementation&tab=output'); await flushPromises()
    expect(wrapper.find('[data-testid="run-canvas"]').exists()).toBe(true)
    await router.push('/runs/old'); await flushPromises()
    expect(wrapper.find('[data-testid="run-history"]').exists()).toBe(true)
  })
  it('ignores a late response from a previously opened Run', async () => {
    let finish!: (value: { soloId: string; title: string; status: string }) => void
    mocks.chat.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { wrapper, router } = await render('/runs/old')
    await router.push('/runs/new'); await flushPromises()
    finish({ soloId: 'private-old', title: 'Old', status: 'completed' }); await flushPromises()
    expect(wrapper.find('[data-testid="owned-solo"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="run-chat-details"]').attributes('href')).toContain('/runs/new?view=details')
  })
})
